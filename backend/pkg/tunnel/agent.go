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
	brokerAddr   string
	nodeID       string
	tlsCfg       *tls.Config
	session      *yamux.Session
	control      net.Conn
	controlEnc   *json.Encoder // for sending log messages back to broker
	listeners    map[int]net.Listener
	mu           sync.Mutex
	quit         chan struct{}
	configDir    string
	ACKMode      bool   // If true, send MLLP ACK to sender after forwarding
	ForwardHost  string // Host for outbound forwarding (default: 127.0.0.1, use host.docker.internal in Docker)
}

// AgentConfig holds agent configuration.
type AgentConfig struct {
	BrokerAddr  string
	ConfigDir   string // Directory to store certs and config
	ACKMode     bool   // Send MLLP ACK after forwarding
	ForwardHost string // Host for outbound forwarding (default: 127.0.0.1, use host.docker.internal in Docker)
}

// NewAgent creates and connects a tunnel agent.
func NewAgent(cfg AgentConfig) *Agent {
	fwdHost := cfg.ForwardHost
	if fwdHost == "" {
		fwdHost = "127.0.0.1"
	}
	return &Agent{
		brokerAddr:  cfg.BrokerAddr,
		configDir:   cfg.ConfigDir,
		listeners:   make(map[int]net.Listener),
		quit:        make(chan struct{}),
		ACKMode:     cfg.ACKMode,
		ForwardHost: fwdHost,
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
	a.controlEnc = json.NewEncoder(controlStream)
	log.SetOutput(io.MultiWriter(os.Stderr, &agentLogForwarder{agent: a}))
	log.Printf("[AGENT] connected to broker as node %s", a.nodeID)
	return nil
}

// agentLogForwarder sends log lines back to the broker via the control stream.
type agentLogForwarder struct{ agent *Agent }

func (w *agentLogForwarder) Write(p []byte) (int, error) {
	line := string(p)
	if w.agent.controlEnc != nil {
		payload, _ := json.Marshal(map[string]string{"line": line})
		w.agent.controlEnc.Encode(ControlMessage{Type: "log", Payload: payload})
	}
	return len(p), nil
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

	addr := fmt.Sprintf("%s:%d", a.ForwardHost, targetPort)
	target, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Printf("[AGENT] forward to %s failed: %v", addr, err)
		return
	}
	defer target.Close()

	log.Printf("[AGENT] forwarding outbound stream to %s", addr)
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

			go a.acceptAndForward(ln, m.TargetPort, m.Protocol)
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

func (a *Agent) acceptAndForward(ln net.Listener, targetPort int, protocol string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go a.tunnelToHost(conn, targetPort, protocol)
	}
}

func (a *Agent) tunnelToHost(local net.Conn, targetPort int, protocol string) {
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

	// MLLP ACK mode: read one message, forward, send ACK. Only for MLLP protocol.
	if a.ACKMode && protocol == "MLLP" {
		buf := make([]byte, 65536)
		n, err := local.Read(buf)
		if err != nil || n == 0 {
			return
		}
		data := buf[:n]

		if _, err := stream.Write(data); err != nil {
			return
		}

		ack := buildMLLPAck(data)
		local.Write(ack)
	} else {
		// Transparent relay: any protocol, bidirectional
		done := make(chan struct{}, 2)
		go func() { io.Copy(stream, local); done <- struct{}{} }()
		go func() { io.Copy(local, stream); done <- struct{}{} }()
		<-done
	}
}

// buildMLLPAck generates a minimal MLLP ACK response for the given HL7 message.
func buildMLLPAck(data []byte) []byte {
	// Extract message control ID from MSH-10 (field 10, pipe-delimited)
	msgID := "0"
	if len(data) > 1 {
		raw := data
		// Skip start block 0x0B if present
		if raw[0] == 0x0B {
			raw = raw[1:]
		}
		fields := splitHL7Field(raw, '|')
		if len(fields) > 9 {
			msgID = string(fields[9])
		}
	}

	ack := fmt.Sprintf("MSH|^~\\&|ARTERIA|ACK|||%s||ACK|%s|P|2.3\rMSA|AA|%s\r",
		time.Now().Format("20060102150405"), msgID, msgID)

	// Wrap in MLLP framing
	var buf []byte
	buf = append(buf, 0x0B)
	buf = append(buf, []byte(ack)...)
	buf = append(buf, 0x1C, 0x0D)
	return buf
}

// splitHL7Field splits on a delimiter, stopping at \r
func splitHL7Field(data []byte, delim byte) [][]byte {
	var fields [][]byte
	start := 0
	for i, b := range data {
		if b == delim {
			fields = append(fields, data[start:i])
			start = i + 1
		} else if b == '\r' || b == 0x1C {
			fields = append(fields, data[start:i])
			break
		}
	}
	return fields
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
