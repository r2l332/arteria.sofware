package tunnel

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// Agent connects outbound to the broker and manages local port listeners.
type Agent struct {
	brokerAddr string
	nodeID     string
	tlsCfg     *tls.Config
	session    *yamux.Session
	control    net.Conn
	listeners  map[int]net.Listener
	mu         sync.Mutex
	quit       chan struct{}
	configDir  string
}

// AgentConfig holds agent configuration.
type AgentConfig struct {
	BrokerAddr string
	ConfigDir  string // Directory to store certs and config
}

// NewAgent creates and connects a tunnel agent.
func NewAgent(cfg AgentConfig) *Agent {
	return &Agent{
		brokerAddr: cfg.BrokerAddr,
		configDir:  cfg.ConfigDir,
		listeners:  make(map[int]net.Listener),
		quit:       make(chan struct{}),
	}
}

// Enroll performs first-time enrollment with the broker using a token.
func (a *Agent) Enroll(token string) error {
	// Connect without client cert for enrollment
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, // Will be replaced after enrollment
		MinVersion:         tls.VersionTLS13,
	}

	conn, err := tls.Dial("tcp", a.brokerAddr, tlsCfg)
	if err != nil {
		return fmt.Errorf("connect to broker: %w", err)
	}

	session, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		conn.Close()
		return fmt.Errorf("yamux client: %w", err)
	}

	controlStream, err := session.Open()
	if err != nil {
		session.Close()
		return fmt.Errorf("open control stream: %w", err)
	}

	hostname, _ := os.Hostname()
	req := EnrollRequest{
		Token:    token,
		Hostname: hostname,
		AgentVer: "0.1.0",
	}
	reqPayload, _ := json.Marshal(req)
	msg := ControlMessage{Type: "enroll", Payload: reqPayload}

	if err := json.NewEncoder(controlStream).Encode(msg); err != nil {
		session.Close()
		return fmt.Errorf("send enroll: %w", err)
	}

	// Read response
	var respMsg ControlMessage
	if err := json.NewDecoder(controlStream).Decode(&respMsg); err != nil {
		session.Close()
		return fmt.Errorf("read enroll response: %w", err)
	}

	var resp EnrollResponse
	json.Unmarshal(respMsg.Payload, &resp)

	if !resp.Success {
		session.Close()
		return fmt.Errorf("enrollment failed: %s", resp.Error)
	}

	// Save certificates
	os.MkdirAll(a.configDir, 0700)
	os.WriteFile(a.configDir+"/ca.pem", []byte(resp.CACert), 0644)
	os.WriteFile(a.configDir+"/node.pem", []byte(resp.Cert), 0644)
	os.WriteFile(a.configDir+"/node-key.pem", []byte(resp.Key), 0600)
	os.WriteFile(a.configDir+"/node-id", []byte(resp.NodeID), 0644)

	a.nodeID = resp.NodeID
	log.Printf("[AGENT] enrolled as node %s", a.nodeID)

	session.Close()
	return nil
}

// Connect establishes the mTLS tunnel to the broker.
func (a *Agent) Connect() error {
	// Load saved certs
	caCert, err := os.ReadFile(a.configDir + "/ca.pem")
	if err != nil {
		return fmt.Errorf("load CA cert: %w (run 'enroll' first)", err)
	}
	clientCert, err := os.ReadFile(a.configDir + "/node.pem")
	if err != nil {
		return fmt.Errorf("load node cert: %w", err)
	}
	clientKey, err := os.ReadFile(a.configDir + "/node-key.pem")
	if err != nil {
		return fmt.Errorf("load node key: %w", err)
	}
	nodeIDBytes, _ := os.ReadFile(a.configDir + "/node-id")
	a.nodeID = string(nodeIDBytes)

	a.tlsCfg, err = AgentTLSConfig(caCert, clientCert, clientKey)
	if err != nil {
		return fmt.Errorf("TLS config: %w", err)
	}

	return a.connectWithRetry()
}

func (a *Agent) connectWithRetry() error {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-a.quit:
			return nil
		default:
		}

		err := a.dial()
		if err == nil {
			backoff = 1 * time.Second
			a.runLoop()
			log.Printf("[AGENT] connection lost, reconnecting in %v...", backoff)
		} else {
			log.Printf("[AGENT] connect error: %v, retrying in %v...", err, backoff)
		}

		select {
		case <-time.After(backoff):
		case <-a.quit:
			return nil
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (a *Agent) dial() error {
	conn, err := tls.Dial("tcp", a.brokerAddr, a.tlsCfg)
	if err != nil {
		return fmt.Errorf("dial broker: %w", err)
	}

	session, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		conn.Close()
		return fmt.Errorf("yamux: %w", err)
	}

	controlStream, err := session.Open()
	if err != nil {
		session.Close()
		return fmt.Errorf("control stream: %w", err)
	}

	// Send connect message
	msg := ControlMessage{Type: "connect", Payload: json.RawMessage(`{}`)}
	if err := json.NewEncoder(controlStream).Encode(msg); err != nil {
		session.Close()
		return fmt.Errorf("send connect: %w", err)
	}

	a.session = session
	a.control = controlStream
	log.Printf("[AGENT] connected to broker as node %s", a.nodeID)
	return nil
}

func (a *Agent) runLoop() {
	// Handle incoming data streams from broker (OUTBOUND direction — broker sending to hospital)
	go a.handleIncomingStreams()

	// Listen for control messages (config pushes, heartbeats)
	dec := json.NewDecoder(a.control)
	for {
		var msg ControlMessage
		if err := dec.Decode(&msg); err != nil {
			log.Printf("[AGENT] control stream error: %v", err)
			return
		}

		switch msg.Type {
		case "config_push":
			var cfg NodeConfig
			json.Unmarshal(msg.Payload, &cfg)
			a.applyConfig(&cfg)

		case "heartbeat":
			// No-op, keeps connection alive

		default:
			log.Printf("[AGENT] unknown control message: %s", msg.Type)
		}
	}
}

func (a *Agent) handleIncomingStreams() {
	for {
		stream, err := a.session.Accept()
		if err != nil {
			return
		}

		// Read 2-byte target port
		portBuf := make([]byte, 2)
		if _, err := io.ReadFull(stream, portBuf); err != nil {
			stream.Close()
			continue
		}
		targetPort := int(portBuf[0])<<8 | int(portBuf[1])

		// Forward to local target
		go a.forwardToLocal(stream, targetPort)
	}
}

func (a *Agent) forwardToLocal(stream net.Conn, targetPort int) {
	defer stream.Close()

	target, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", targetPort), 5*time.Second)
	if err != nil {
		log.Printf("[AGENT] forward to local :%d failed: %v", targetPort, err)
		return
	}
	defer target.Close()

	relay(stream, target)
}

// applyConfig updates local port listeners based on pushed config.
func (a *Agent) applyConfig(cfg *NodeConfig) {
	log.Printf("[AGENT] applying config: %d mappings", len(cfg.Mappings))

	a.mu.Lock()
	defer a.mu.Unlock()

	// Track which ports should be active
	activePorts := make(map[int]bool)

	for _, m := range cfg.Mappings {
		if !m.IsActive {
			continue
		}

		// INBOUND: hospital sends to agent local port → forwarded through tunnel to Arteria
		if m.Direction == "INBOUND" {
			activePorts[m.LocalPort] = true

			if _, exists := a.listeners[m.LocalPort]; exists {
				continue // Already listening
			}

			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", m.LocalPort))
			if err != nil {
				log.Printf("[AGENT] listen :%d failed: %v", m.LocalPort, err)
				continue
			}

			a.listeners[m.LocalPort] = ln
			log.Printf("[AGENT] listening on :%d → tunnel → broker:%d (%s)", m.LocalPort, m.TargetPort, m.Protocol)

			go a.acceptAndForward(ln, m.TargetPort)
		}
	}

	// Stop listeners for removed mappings
	for port, ln := range a.listeners {
		if !activePorts[port] {
			ln.Close()
			delete(a.listeners, port)
			log.Printf("[AGENT] stopped listening on :%d", port)
		}
	}
}

func (a *Agent) acceptAndForward(ln net.Listener, targetPort int) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go a.tunnelToHost(conn, targetPort)
	}
}

func (a *Agent) tunnelToHost(local net.Conn, targetPort int) {
	defer local.Close()

	if a.session == nil || a.session.IsClosed() {
		log.Printf("[AGENT] tunnel not connected, dropping connection")
		return
	}

	stream, err := a.session.Open()
	if err != nil {
		log.Printf("[AGENT] open tunnel stream: %v", err)
		return
	}
	defer stream.Close()

	// Send 2-byte target port header (big-endian uint16)
	portBuf := []byte{byte(targetPort >> 8), byte(targetPort & 0xff)}
	if _, err := stream.Write(portBuf); err != nil {
		return
	}

	// Relay bidirectionally
	done := make(chan struct{}, 2)
	go func() { io.Copy(stream, local); done <- struct{}{} }()
	go func() { io.Copy(local, stream); done <- struct{}{} }()
	<-done
}

// Stop shuts down the agent.
func (a *Agent) Stop() {
	close(a.quit)
	a.mu.Lock()
	for _, ln := range a.listeners {
		ln.Close()
	}
	a.mu.Unlock()
	if a.session != nil {
		a.session.Close()
	}
}
