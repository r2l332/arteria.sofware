package tunnel

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// Mapping defines a port-forwarding rule pushed from the broker to the agent.
type Mapping struct {
	LocalPort  int    `json:"local_port"`
	Direction  string `json:"direction"`   // "INBOUND" or "OUTBOUND"
	TargetHost string `json:"target_host"` // Where to forward traffic
	TargetPort int    `json:"target_port"`
	Protocol   string `json:"protocol"`    // "MLLP", "TCP", "HTTP"
	IsActive   bool   `json:"is_active"`
}

// NodeConfig is pushed from broker to agent over the control stream.
type NodeConfig struct {
	NodeID   string    `json:"node_id"`
	Mappings []Mapping `json:"mappings"`
}

// ControlMessage is the envelope for control-plane messages over yamux stream 0.
type ControlMessage struct {
	Type    string          `json:"type"` // "config_push", "heartbeat", "enroll_response", "log"
	Payload json.RawMessage `json:"payload"`
}

// EnrollRequest is sent by the agent to the broker during enrollment.
type EnrollRequest struct {
	Token     string `json:"token"`
	Hostname  string `json:"hostname"`
	AgentVer  string `json:"agent_version"`
}

// EnrollResponse is returned by the broker after enrollment.
type EnrollResponse struct {
	NodeID  string `json:"node_id"`
	CACert  string `json:"ca_cert"`
	Cert    string `json:"cert"`
	Key     string `json:"key"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// --- Broker ---

// Broker accepts inbound mTLS connections from agents and routes traffic via NATS.
// The broker can be deployed anywhere — it only needs NATS connectivity to reach Arteria services.
type Broker struct {
	listener   net.Listener
	ca         *CA
	sessions   map[string]*BrokerSession
	mu         sync.RWMutex
	quit       chan struct{}
	onConnect  func(nodeID string)
	onDisconn  func(nodeID string)
	getConfig  func(nodeID string) *NodeConfig
	enrollNode func(req EnrollRequest) (*EnrollResponse, error)
	onInbound  func(nodeID string, targetPort int, data []byte) // Called when data arrives from agent
	onAgentLog func(nodeID, line string) // Called when agent sends a log line
}

// BrokerSession represents one connected agent.
type BrokerSession struct {
	NodeID       string
	Session      *yamux.Session
	Control      net.Conn
	Connected    time.Time
	AgentVersion string
	AgentOS      string
	AgentArch    string
}

// BrokerConfig holds broker configuration.
type BrokerConfig struct {
	ListenAddr string
	CA         *CA
	BrokerCert []byte
	BrokerKey  []byte
	GetConfig  func(nodeID string) *NodeConfig
	EnrollNode func(req EnrollRequest) (*EnrollResponse, error)
	OnConnect  func(nodeID string)
	OnDisconn  func(nodeID string)
	OnInbound  func(nodeID string, targetPort int, data []byte) // Route inbound traffic via NATS
	OnAgentLog func(nodeID, line string) // Forward agent log lines
}

// NewBroker creates a new tunnel broker.
func NewBroker(cfg BrokerConfig) (*Broker, error) {
	tlsCfg, err := cfg.CA.BrokerTLSConfig(cfg.BrokerCert, cfg.BrokerKey)
	if err != nil {
		return nil, err
	}

	// Also accept unauthenticated connections for enrollment
	tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven

	ln, err := tls.Listen("tcp", cfg.ListenAddr, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("broker listen: %w", err)
	}

	b := &Broker{
		listener:   ln,
		ca:         cfg.CA,
		sessions:   make(map[string]*BrokerSession),
		quit:       make(chan struct{}),
		getConfig:  cfg.GetConfig,
		enrollNode: cfg.EnrollNode,
		onConnect:  cfg.OnConnect,
		onDisconn:  cfg.OnDisconn,
		onInbound:  cfg.OnInbound,
		onAgentLog: cfg.OnAgentLog,
	}

	go b.acceptLoop()
	log.Printf("[BROKER] listening on %s", cfg.ListenAddr)
	return b, nil
}

func (b *Broker) acceptLoop() {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			select {
			case <-b.quit:
				return
			default:
				log.Printf("[BROKER] accept error: %v", err)
				continue
			}
		}
		go b.handleConn(conn)
	}
}

func (b *Broker) handleConn(conn net.Conn) {
	tlsConn := conn.(*tls.Conn)
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("[BROKER] TLS handshake error: %v", err)
		conn.Close()
		return
	}

	// Create yamux session (broker is the server side)
	session, err := yamux.Server(conn, yamux.DefaultConfig())
	if err != nil {
		log.Printf("[BROKER] yamux session error: %v", err)
		conn.Close()
		return
	}

	// Accept the control stream (stream 0)
	controlStream, err := session.Accept()
	if err != nil {
		log.Printf("[BROKER] control stream error: %v", err)
		session.Close()
		return
	}

	// Read the first message to determine if this is enrollment or reconnect
	dec := json.NewDecoder(controlStream)
	var firstMsg ControlMessage
	if err := dec.Decode(&firstMsg); err != nil {
		log.Printf("[BROKER] control read error: %v", err)
		session.Close()
		return
	}

	var nodeID string
	var agentVersion, agentOS, agentArch string

	switch firstMsg.Type {
	case "enroll":
		var req EnrollRequest
		json.Unmarshal(firstMsg.Payload, &req)

		resp, err := b.enrollNode(req)
		if err != nil {
			resp = &EnrollResponse{Success: false, Error: err.Error()}
		}

		respPayload, _ := json.Marshal(resp)
		respMsg := ControlMessage{Type: "enroll_response", Payload: respPayload}
		json.NewEncoder(controlStream).Encode(respMsg)

		if !resp.Success {
			session.Close()
			return
		}
		nodeID = resp.NodeID
		log.Printf("[BROKER] enrolled new node %s", nodeID)

	case "connect":
		// Authenticated reconnect — extract node ID from client cert CN
		state := tlsConn.ConnectionState()
		if len(state.PeerCertificates) == 0 {
			log.Printf("[BROKER] no client cert on connect")
			session.Close()
			return
		}
		cn := state.PeerCertificates[0].Subject.CommonName
		// CN is "node-<uuid>", strip the prefix
		if len(cn) > 5 && cn[:5] == "node-" {
			nodeID = cn[5:]
		} else {
			nodeID = cn
		}
		// Parse version info from connect payload
		var connectInfo struct {
			AgentVersion string `json:"agent_version"`
			OS           string `json:"os"`
			Arch         string `json:"arch"`
		}
		json.Unmarshal(firstMsg.Payload, &connectInfo)
		agentVersion = connectInfo.AgentVersion
		agentOS = connectInfo.OS
		agentArch = connectInfo.Arch
		log.Printf("[BROKER] node reconnected: %s (v%s %s/%s)", nodeID, agentVersion, agentOS, agentArch)

	default:
		log.Printf("[BROKER] unknown first message type: %s", firstMsg.Type)
		session.Close()
		return
	}

	// Register session
	bs := &BrokerSession{
		NodeID:       nodeID,
		Session:      session,
		Control:      controlStream,
		Connected:    time.Now(),
		AgentVersion: agentVersion,
		AgentOS:      agentOS,
		AgentArch:    agentArch,
	}
	b.mu.Lock()
	b.sessions[nodeID] = bs
	b.mu.Unlock()

	if b.onConnect != nil {
		b.onConnect(nodeID)
	}

	// Push config to the agent
	if b.getConfig != nil {
		if cfg := b.getConfig(nodeID); cfg != nil {
			b.PushConfig(nodeID, cfg)
		}
	}

	// Handle incoming data streams from the agent (for INBOUND direction)
	go b.handleDataStreams(bs)

	// Read control messages from the agent (logs, etc.)
	go b.readControlMessages(bs)

	// Heartbeat loop
	go b.heartbeatLoop(bs)
}

func (b *Broker) readControlMessages(bs *BrokerSession) {
	dec := json.NewDecoder(bs.Control)
	for {
		var msg ControlMessage
		if err := dec.Decode(&msg); err != nil {
			return
		}
		if msg.Type == "log" && b.onAgentLog != nil {
			var entry struct{ Line string `json:"line"` }
			json.Unmarshal(msg.Payload, &entry)
			if entry.Line != "" {
				b.onAgentLog(bs.NodeID, entry.Line)
			}
		}
	}
}

func (b *Broker) handleDataStreams(bs *BrokerSession) {
	for {
		stream, err := bs.Session.Accept()
		if err != nil {
			log.Printf("[BROKER] node %s disconnected: %v", bs.NodeID, err)
			b.removeSession(bs.NodeID)
			return
		}

		// Read 2-byte target port (big-endian uint16)
		portBuf := make([]byte, 2)
		if _, err := io.ReadFull(stream, portBuf); err != nil {
			stream.Close()
			continue
		}
		targetPort := int(portBuf[0])<<8 | int(portBuf[1])

		// Route via NATS
		go b.routeInbound(bs.NodeID, stream, targetPort)
	}
}

func (b *Broker) routeInbound(nodeID string, stream net.Conn, targetPort int) {
	defer stream.Close()

	// Read in chunks — don't wait for EOF since MLLP streams may stay open
	buf := make([]byte, 64*1024)
	for {
		stream.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := stream.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			if b.onInbound != nil {
				b.onInbound(nodeID, targetPort, data)
			}
		}
		if err != nil {
			return
		}
	}
}

func (b *Broker) heartbeatLoop(bs *BrokerSession) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	enc := json.NewEncoder(bs.Control)
	beatCount := 0

	for {
		select {
		case <-ticker.C:
			beatCount++
			msg := ControlMessage{Type: "heartbeat", Payload: json.RawMessage(`{}`)}
			if err := enc.Encode(msg); err != nil {
				return
			}
			// Re-push config every 60s (every 4th heartbeat) to keep agent in sync
			if beatCount%4 == 0 && b.getConfig != nil {
				if cfg := b.getConfig(bs.NodeID); cfg != nil {
					b.PushConfig(bs.NodeID, cfg)
				}
			}
		case <-b.quit:
			return
		}
	}
}

// PushConfig sends a config update to a connected agent.
func (b *Broker) PushConfig(nodeID string, cfg *NodeConfig) error {
	b.mu.RLock()
	bs, ok := b.sessions[nodeID]
	b.mu.RUnlock()
	if !ok {
		return fmt.Errorf("node %s not connected", nodeID)
	}

	payload, _ := json.Marshal(cfg)
	msg := ControlMessage{Type: "config_push", Payload: payload}
	return json.NewEncoder(bs.Control).Encode(msg)
}

// OpenStream opens a new yamux stream to an agent (for OUTBOUND direction — sending to hospital).
func (b *Broker) OpenStream(nodeID string, targetPort int, data []byte) error {
	b.mu.RLock()
	bs, ok := b.sessions[nodeID]
	b.mu.RUnlock()
	if !ok {
		return fmt.Errorf("node %s not connected", nodeID)
	}

	stream, err := bs.Session.Open()
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer stream.Close()

	// Send 2-byte target port header
	portBuf := []byte{byte(targetPort >> 8), byte(targetPort & 0xff)}
	if _, err := stream.Write(portBuf); err != nil {
		return err
	}

	// Send data
	_, err = stream.Write(data)
	return err
}

func (b *Broker) removeSession(nodeID string) {
	b.mu.Lock()
	delete(b.sessions, nodeID)
	b.mu.Unlock()

	if b.onDisconn != nil {
		b.onDisconn(nodeID)
	}
}

// ConnectedNodes returns the list of currently connected node IDs.
func (b *Broker) ConnectedNodes() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	ids := make([]string, 0, len(b.sessions))
	for id := range b.sessions {
		ids = append(ids, id)
	}
	return ids
}

// GetSessionInfo returns version/os/arch for a connected node.
func (b *Broker) GetSessionInfo(nodeID string) (version, goos, goarch string, connected time.Time, ok bool) {
	b.mu.RLock()
	bs, ok := b.sessions[nodeID]
	b.mu.RUnlock()
	if !ok {
		return
	}
	return bs.AgentVersion, bs.AgentOS, bs.AgentArch, bs.Connected, true
}

// PushUpdate sends a new agent binary to a connected node.
func (b *Broker) PushUpdate(nodeID string, version string, binary []byte, sha256hex string) error {
	b.mu.RLock()
	bs, ok := b.sessions[nodeID]
	b.mu.RUnlock()
	if !ok {
		return fmt.Errorf("node %s not connected", nodeID)
	}

	// Send update control message with metadata
	payload, _ := json.Marshal(map[string]interface{}{
		"version": version,
		"size":    len(binary),
		"sha256":  sha256hex,
	})
	if err := json.NewEncoder(bs.Control).Encode(ControlMessage{Type: "update", Payload: payload}); err != nil {
		return fmt.Errorf("send update control: %w", err)
	}

	// Open a yamux stream and push the binary
	stream, err := bs.Session.Open()
	if err != nil {
		return fmt.Errorf("open update stream: %w", err)
	}
	defer stream.Close()

	// 4-byte magic header so agent can identify this stream
	if _, err := stream.Write([]byte("CUPD")); err != nil {
		return err
	}
	_, err = stream.Write(binary)
	return err
}

// Stop shuts down the broker.
func (b *Broker) Stop() {
	close(b.quit)
	b.listener.Close()
	b.mu.Lock()
	for _, bs := range b.sessions {
		bs.Session.Close()
	}
	b.mu.Unlock()
}

// relay copies data bidirectionally between two connections.
func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
