package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/r2l332/arteria.app/backend/pkg/natsutil"
	"github.com/r2l332/arteria.app/backend/pkg/scyllautil"
	"github.com/r2l332/arteria.app/backend/pkg/tunnel"
)

// Broker metrics
var (
	brokerBytesIn      atomic.Int64
	brokerBytesOut     atomic.Int64
	brokerMsgsRouted   atomic.Int64
	brokerConnections  atomic.Int64
	brokerDisconnects  atomic.Int64
	brokerActiveConns  atomic.Int64
	brokerEnrollments  atomic.Int64
	brokerStartTime    time.Time
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	listenAddr := envOr("BROKER_LISTEN", ":9443")
	scyllaHost := envOr("SCYLLA_HOST", "127.0.0.1")
	natsURL := envOr("NATS_URL", nats.DefaultURL)
	caDir := envOr("CA_DIR", "/etc/arteria-broker/ca")

	// Connect to ScyllaDB
	scyllaCfg := scyllautil.DefaultConfig()
	scyllaCfg.Hosts = strings.Split(scyllaHost, ",")
	session, err := scyllautil.Connect(scyllaCfg)
	if err != nil {
		log.Fatalf("ScyllaDB connect: %v", err)
	}
	defer session.Close()

	// Connect to NATS — the mesh backbone. Broker can be anywhere that reaches NATS.
	natsCfg := natsutil.DefaultConfig()
	natsCfg.URL = natsURL
	nc, js, err := natsutil.Connect(natsCfg)
	if err != nil {
		log.Fatalf("NATS connect: %v", err)
	}
	defer nc.Close()
	_ = js

	// Load or create CA
	os.MkdirAll(caDir, 0700)
	ca, err := tunnel.LoadOrCreateCA(caDir+"/ca.pem", caDir+"/ca-key.pem")
	if err != nil {
		log.Fatalf("CA init: %v", err)
	}
	log.Println("[BROKER] CA initialized")

	brokerStartTime = time.Now()

	// Issue broker's own server cert
	brokerCert, brokerKey, err := ca.IssueCert("broker", "arteria-broker")
	if err != nil {
		log.Fatalf("broker cert: %v", err)
	}

	// Create broker
	broker, err := tunnel.NewBroker(tunnel.BrokerConfig{
		ListenAddr: listenAddr,
		CA:         ca,
		BrokerCert: brokerCert,
		BrokerKey:  brokerKey,

		GetConfig: func(nodeID string) *tunnel.NodeConfig {
			return loadNodeConfig(session, nodeID)
		},

		EnrollNode: func(req tunnel.EnrollRequest) (*tunnel.EnrollResponse, error) {
			return enrollNode(session, ca, req)
		},

		OnConnect: func(nodeID string) {
			log.Printf("[BROKER] node connected: %s", nodeID)
			updateNodeStatus(session, nodeID, "CONNECTED")
			brokerConnections.Add(1)
			brokerActiveConns.Add(1)
		},

		OnDisconn: func(nodeID string) {
			log.Printf("[BROKER] node disconnected: %s", nodeID)
			updateNodeStatus(session, nodeID, "DISCONNECTED")
			brokerDisconnects.Add(1)
			brokerActiveConns.Add(-1)
		},

		// Route inbound traffic through NATS — broker can be anywhere in the world
		OnInbound: func(nodeID string, targetPort int, data []byte) {
			subject := "arteria.ingest.raw"
			log.Printf("[BROKER] routing %d bytes from node %s via NATS subject %s", len(data), nodeID, subject)
			brokerBytesIn.Add(int64(len(data)))
			brokerMsgsRouted.Add(1)
			if _, err := js.Publish(subject, data); err != nil {
				log.Printf("[BROKER] NATS publish error: %v", err)
			}
		},
	})
	if err != nil {
		log.Fatalf("broker start: %v", err)
	}
	_ = broker

	// NATS metrics responder — allows platform health check to query broker stats
	nc.Subscribe("arteria.metrics.tunnel-broker", func(msg *nats.Msg) {
		uptime := time.Since(brokerStartTime).Seconds()
		metrics := map[string]interface{}{
			"bytes_in":          brokerBytesIn.Load(),
			"bytes_out":         brokerBytesOut.Load(),
			"messages_routed":   brokerMsgsRouted.Load(),
			"total_connections": brokerConnections.Load(),
			"total_disconnects": brokerDisconnects.Load(),
			"active_connections": brokerActiveConns.Load(),
			"enrollments":       brokerEnrollments.Load(),
			"uptime_seconds":    int64(uptime),
			"bandwidth_kbps":    float64(brokerBytesIn.Load()+brokerBytesOut.Load()) / uptime * 8 / 1024,
		}
		data, _ := json.Marshal(metrics)
		msg.Respond(data)
	})

	// Listen for config-push notifications from the API
	nc.Subscribe("arteria.tunnel.config-push", func(msg *nats.Msg) {
		nodeID := string(msg.Data)
		log.Printf("[BROKER] config push requested for node %s", nodeID)
		cfg := loadNodeConfig(session, nodeID)
		if cfg != nil {
			if err := broker.PushConfig(nodeID, cfg); err != nil {
				log.Printf("[BROKER] push config failed for %s: %v", nodeID, err)
			} else {
				log.Printf("[BROKER] config pushed to node %s (%d mappings)", nodeID, len(cfg.Mappings))
			}
		}
	})

	// Listen for tunnel delivery requests from the egress service
	nc.Subscribe("arteria.tunnel.deliver", func(msg *nats.Msg) {
		var req struct {
			NodeID     string          `json:"node_id"`
			TargetPort int             `json:"target_port"`
			Protocol   string          `json:"protocol"`
			Payload    json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			resp, _ := json.Marshal(map[string]interface{}{"success": false, "error": "invalid request"})
			msg.Respond(resp)
			return
		}

		log.Printf("[BROKER] tunnel deliver: node=%s port=%d protocol=%s size=%d", req.NodeID, req.TargetPort, req.Protocol, len(req.Payload))
		brokerBytesOut.Add(int64(len(req.Payload)))

		if err := broker.OpenStream(req.NodeID, req.TargetPort, req.Payload); err != nil {
			log.Printf("[BROKER] tunnel deliver failed: %v", err)
			resp, _ := json.Marshal(map[string]interface{}{"success": false, "error": err.Error()})
			msg.Respond(resp)
			return
		}

		brokerMsgsRouted.Add(1)
		resp, _ := json.Marshal(map[string]interface{}{"success": true})
		msg.Respond(resp)
	})

	log.Printf("[BROKER] tunnel broker started on %s", listenAddr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("[BROKER] shutting down...")
	broker.Stop()
}

func enrollNode(session *gocql.Session, ca *tunnel.CA, req tunnel.EnrollRequest) (*tunnel.EnrollResponse, error) {
	// Validate enrollment token
	var nodeID gocql.UUID
	var name, siteName string
	iter := session.Query(`SELECT node_id, name, site_name FROM arteria.tunnel_nodes WHERE enrollment_token = ? ALLOW FILTERING`, req.Token).Iter()
	found := iter.Scan(&nodeID, &name, &siteName)
	iter.Close()

	if !found {
		return &tunnel.EnrollResponse{Success: false, Error: "invalid enrollment token"}, nil
	}

	// Check if already enrolled
	var status string
	session.Query(`SELECT status FROM arteria.tunnel_nodes WHERE node_id = ?`, nodeID).Scan(&status)
	if status == "ENROLLED" || status == "CONNECTED" {
		return &tunnel.EnrollResponse{Success: false, Error: "node already enrolled"}, nil
	}

	// Issue certificate
	cn := "node-" + nodeID.String()
	certPEM, keyPEM, err := ca.IssueCert(nodeID.String(), cn)
	if err != nil {
		return nil, err
	}

	// Update node status
	now := time.Now()
	session.Query(`UPDATE arteria.tunnel_nodes SET status = ?, agent_version = ?, last_seen = ?, updated_at = ? WHERE node_id = ?`,
		"ENROLLED", req.AgentVer, now, now, nodeID).Exec()

	return &tunnel.EnrollResponse{
		NodeID:  nodeID.String(),
		CACert:  string(ca.CACertPEM()),
		Cert:    string(certPEM),
		Key:     string(keyPEM),
		Success: true,
	}, nil
}

func loadNodeConfig(session *gocql.Session, nodeID string) *tunnel.NodeConfig {
	id, err := gocql.ParseUUID(nodeID)
	if err != nil {
		return nil
	}

	cfg := &tunnel.NodeConfig{NodeID: nodeID}

	// Derive mappings from communication_points where tunnel_node_id matches this node
	iter := session.Query(`SELECT direction, protocol, host, port, tunnel_enabled, tunnel_node_id, tunnel_local_port, is_active FROM arteria.communication_points`).Iter()
	var tunnelNodeID gocql.UUID
	var direction, protocol, host string
	var cpPort, tunnelLocalPort int
	var isActive, tunnelEnabled bool

	for iter.Scan(&direction, &protocol, &host, &cpPort, &tunnelEnabled, &tunnelNodeID, &tunnelLocalPort, &isActive) {
		if !tunnelEnabled || tunnelNodeID != id {
			continue
		}

		m := tunnel.Mapping{
			LocalPort:  tunnelLocalPort,
			Protocol:   protocol,
			IsActive:   isActive,
		}

		if direction == "INPUT" {
			// INBOUND: hospital sends to agent's local port → tunnel → Arteria ingestion port
			m.Direction = "INBOUND"
			m.TargetHost = "ingestion" // Docker service name
			m.TargetPort = cpPort
		} else {
			// OUTBOUND: Arteria sends through tunnel → agent delivers to hospital system
			m.Direction = "OUTBOUND"
			m.TargetHost = host
			m.TargetPort = cpPort
		}

		cfg.Mappings = append(cfg.Mappings, m)
	}
	iter.Close()

	return cfg
}

func updateNodeStatus(session *gocql.Session, nodeIDStr, status string) {
	nodeID, err := gocql.ParseUUID(nodeIDStr)
	if err != nil {
		return
	}
	session.Query(`UPDATE arteria.tunnel_nodes SET status = ?, last_seen = ?, updated_at = ? WHERE node_id = ?`,
		status, time.Now(), time.Now(), nodeID).Exec()
}

// createEnrollmentToken is a helper used by the API to generate tokens.
func createEnrollmentToken() string {
	return uuid.New().String()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
