package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gocql/gocql"
	"github.com/nats-io/nats.go"
	"github.com/r2l332/arteria.app/backend/pkg/logging"
	"github.com/r2l332/arteria.app/backend/pkg/metrics"
	"github.com/r2l332/arteria.app/backend/pkg/natsutil"
	"github.com/r2l332/arteria.app/backend/pkg/scyllautil"
)

// Egress service: subscribes to arteria.route.* and delivers messages to output CPs.
// Supports: MLLP, HTTP/Webhook, and Discard (null sink for testing).

const (
	subjectRoute = "arteria.route.>"
	consumerName = "egress-service"
	startBlock   = 0x0B
	endBlock     = 0x1C
	cr           = 0x0D
)

// OutputCP represents a configured output communication point.
type OutputCP struct {
	CommPointID      string
	Name             string
	Protocol         string // MLLP, HTTP, WEBHOOK, REST, DISCARD
	Host             string
	Port             int
	MaxRetries       int
	RetryDelayMs     int
	TimeoutMs        int
	IsActive         bool
	DestTopic        string // which route topic this CP handles
	TunnelEnabled    bool
	TunnelNodeID     string
	TunnelLocalPort  int
}

var (
	log *logging.Logger
	met *metrics.Counters

	natsConn    *nats.Conn
	outputCPs   []OutputCP
	outputCPsMu sync.RWMutex
)

func main() {
	var err error
	log, err = logging.FromEnv("egress")
	if err != nil {
		panic("failed to init logger: " + err.Error())
	}
	defer log.Close()

	natsURL := envOrDefault("NATS_URL", nats.DefaultURL)
	scyllaHost := envOrDefault("SCYLLA_HOST", "127.0.0.1")

	log.Info("starting egress service", logging.Fields{
		"nats_url":    natsURL,
		"scylla_host": scyllaHost,
	})

	// Connect to NATS
	natsCfg := natsutil.DefaultConfig()
	natsCfg.URL = natsURL
	nc, js, err := natsutil.Connect(natsCfg)
	if err != nil {
		log.Fatal("failed to connect to NATS", logging.Fields{"error": err.Error()})
	}
	defer nc.Close()
	natsConn = nc

	// Connect to ScyllaDB
	scyllaCfg := scyllautil.DefaultConfig()
	scyllaCfg.Hosts = strings.Split(scyllaHost, ",")
	session, err := scyllautil.Connect(scyllaCfg)
	if err != nil {
		log.Fatal("failed to connect to ScyllaDB", logging.Fields{"error": err.Error()})
	}
	defer session.Close()

	// Initialize metrics
	met = metrics.New()

	// Serve metrics via NATS
	nc.Subscribe("arteria.metrics.egress", func(msg *nats.Msg) {
		snap := met.Snap()
		data, _ := json.Marshal(snap)
		msg.Respond(data)
	})

	// Load output CPs from ScyllaDB
	loadOutputCPs(session)

	// Periodically reload output CPs (pick up config changes)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				loadOutputCPs(session)
			}
		}
	}()

	// Subscribe to all routed messages
	sub, err := js.QueueSubscribe(subjectRoute, consumerName, func(m *nats.Msg) {
		handleRoutedMessage(m, session)
	}, nats.Durable(consumerName), nats.ManualAck(), nats.AckWait(30*time.Second))
	if err != nil {
		log.Fatal("failed to subscribe to route stream", logging.Fields{"error": err.Error()})
	}
	defer sub.Unsubscribe()

	log.Info("subscribed to route stream", logging.Fields{
		"subject":  subjectRoute,
		"consumer": consumerName,
	})

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("received shutdown signal", logging.Fields{"signal": sig.String()})
	cancel()
	sub.Drain()
	nc.Drain()
	log.Info("egress shutdown complete")
}

func handleRoutedMessage(m *nats.Msg, session *gocql.Session) {
	// Extract the destination topic from the NATS subject
	// Subject format: arteria.route.<topic>
	parts := strings.SplitN(m.Subject, ".", 3)
	destTopic := "default"
	if len(parts) >= 3 {
		destTopic = parts[2]
	}

	met.Received.Add(1)

	// Find matching output CPs for this topic
	outputCPsMu.RLock()
	var targets []OutputCP
	for _, cp := range outputCPs {
		if cp.IsActive && (cp.DestTopic == destTopic || cp.DestTopic == "*") {
			targets = append(targets, cp)
		}
	}
	outputCPsMu.RUnlock()

	if len(targets) == 0 {
		// No output CP configured for this topic — discard silently
		log.Debug("no output CP for topic, discarding", logging.Fields{
			"topic":   destTopic,
			"subject": m.Subject,
			"size":    len(m.Data),
		})
		met.Processed.Add(1)
		m.Ack()
		return
	}

	// Deliver to all matching output CPs
	var lastErr error
	for _, cp := range targets {
		err := deliver(cp, m.Data)
		if err != nil {
			log.Error("delivery failed", logging.Fields{
				"comm_point": cp.Name,
				"protocol":   cp.Protocol,
				"host":       cp.Host,
				"port":       cp.Port,
				"topic":      destTopic,
				"error":      err.Error(),
			})
			met.Errors.Add(1)
			lastErr = err

			// Update message status to DELIVERY_FAILED
			updateDeliveryStatus(session, m.Data, "DELIVERY_FAILED", err.Error())
		} else {
			log.Info("message delivered", logging.Fields{
				"comm_point": cp.Name,
				"protocol":   cp.Protocol,
				"host":       fmt.Sprintf("%s:%d", cp.Host, cp.Port),
				"topic":      destTopic,
				"size":       len(m.Data),
			})
			met.Routed.Add(1)

			// Update message status to DELIVERED
			updateDeliveryStatus(session, m.Data, "DELIVERED", "")
		}
	}

	if lastErr != nil {
		// At least one delivery failed — NAK for retry
		m.Nak()
	} else {
		met.Processed.Add(1)
		m.Ack()
	}
}

// deliver sends a message to an output CP based on its protocol.
func deliver(cp OutputCP, payload []byte) error {
	// If tunnel-enabled, route through the Aorta mesh instead of direct delivery
	if cp.TunnelEnabled && cp.TunnelNodeID != "" && cp.TunnelNodeID != "00000000-0000-0000-0000-000000000000" {
		return deliverViaTunnel(cp, payload)
	}

	timeout := time.Duration(cp.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	switch strings.ToUpper(cp.Protocol) {
	case "MLLP":
		return deliverMLLP(cp.Host, cp.Port, payload, timeout)
	case "HTTP", "REST", "WEBHOOK":
		return deliverHTTP(cp.Host, cp.Port, payload, timeout)
	case "DISCARD", "NULL", "SINK":
		// Null sink — acknowledge without sending anywhere (for testing)
		log.Debug("message discarded (null sink)", logging.Fields{"comm_point": cp.Name, "size": len(payload)})
		return nil
	default:
		return fmt.Errorf("unsupported protocol: %s", cp.Protocol)
	}
}

// deliverViaTunnel routes a message through the Aorta tunnel mesh to a remote Capillary agent.
// It publishes a request to NATS that the tunnel-broker picks up and forwards via yamux stream.
func deliverViaTunnel(cp OutputCP, payload []byte) error {
	targetPort := cp.TunnelLocalPort
	if targetPort == 0 {
		targetPort = cp.Port
	}

	// MLLP protocol requires framing around the payload
	wirePayload := payload
	if strings.ToUpper(cp.Protocol) == "MLLP" {
		var buf bytes.Buffer
		buf.WriteByte(startBlock)
		buf.Write(payload)
		buf.WriteByte(endBlock)
		buf.WriteByte(cr)
		wirePayload = buf.Bytes()
	}

	// Build the tunnel delivery request
	req := struct {
		NodeID     string `json:"node_id"`
		TargetPort int    `json:"target_port"`
		Protocol   string `json:"protocol"`
		Payload    []byte `json:"payload"`
	}{
		NodeID:     cp.TunnelNodeID,
		TargetPort: targetPort,
		Protocol:   cp.Protocol,
		Payload:    wirePayload,
	}

	data, _ := json.Marshal(req)

	// Request/reply to broker — wait up to 10s for delivery confirmation
	msg, err := natsConn.Request("arteria.tunnel.deliver", data, 10*time.Second)
	if err != nil {
		return fmt.Errorf("tunnel delivery to node %s port %d: %w", cp.TunnelNodeID, targetPort, err)
	}

	// Check response
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if json.Unmarshal(msg.Data, &resp) != nil || !resp.Success {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return fmt.Errorf("tunnel delivery failed: %s", errMsg)
	}

	log.Info("message delivered via tunnel", logging.Fields{
		"comm_point": cp.Name,
		"node_id":    cp.TunnelNodeID,
		"port":       targetPort,
		"size":       len(payload),
	})
	return nil
}

// deliverMLLP sends a message via MLLP (HL7 TCP framing).
func deliverMLLP(host string, port int, payload []byte, timeout time.Duration) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("mllp connect %s: %w", addr, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	// Extract raw HL7 from the JSON envelope if possible
	hl7Payload := extractHL7(payload)

	var buf bytes.Buffer
	buf.WriteByte(startBlock)
	buf.Write(hl7Payload)
	buf.WriteByte(endBlock)
	buf.WriteByte(cr)

	_, err = conn.Write(buf.Bytes())
	if err != nil {
		return fmt.Errorf("mllp write %s: %w", addr, err)
	}

	// Read ACK (optional, best-effort)
	ackBuf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	conn.Read(ackBuf) // Don't fail on missing ACK

	return nil
}

// deliverHTTP sends a message via HTTP POST.
func deliverHTTP(host string, port int, payload []byte, timeout time.Duration) error {
	url := fmt.Sprintf("http://%s:%d", host, port)
	if port == 443 {
		url = fmt.Sprintf("https://%s", host)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("http post %s: %w", url, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %s returned %d", url, resp.StatusCode)
	}
	return nil
}

// extractHL7 pulls the raw HL7 payload from a JSON message envelope.
func extractHL7(data []byte) []byte {
	var envelope struct {
		RawPayload string `json:"rawPayload"`
	}
	if json.Unmarshal(data, &envelope) == nil && envelope.RawPayload != "" {
		return []byte(envelope.RawPayload)
	}
	// Not JSON or no rawPayload — send as-is
	return data
}

// updateDeliveryStatus updates the message status in ScyllaDB.
func updateDeliveryStatus(session *gocql.Session, payload []byte, status, errDetails string) {
	var envelope struct {
		MessageID string `json:"messageId"`
	}
	if json.Unmarshal(payload, &envelope) != nil || envelope.MessageID == "" {
		return
	}
	msgID, err := gocql.ParseUUID(envelope.MessageID)
	if err != nil {
		return
	}
	session.Query(`UPDATE arteria.messages SET status = ?, error_details = ?, updated_at = ? WHERE message_id = ?`,
		status, errDetails, time.Now(), msgID).Exec()
}

// loadOutputCPs loads OUTPUT communication points from ScyllaDB.
func loadOutputCPs(session *gocql.Session) {
	var cps []OutputCP
	iter := session.Query(`SELECT comm_point_id, name, direction, protocol, host, port, is_active, max_retries, retry_delay_ms, timeout_ms, tunnel_enabled, tunnel_node_id, tunnel_local_port FROM arteria.communication_points`).Iter()

	var id, tunnelNodeID gocql.UUID
	var name, direction, protocol, host string
	var port, maxRetries, retryDelay, timeout, tunnelLocalPort int
	var isActive, tunnelEnabled bool

	for iter.Scan(&id, &name, &direction, &protocol, &host, &port, &isActive, &maxRetries, &retryDelay, &timeout, &tunnelEnabled, &tunnelNodeID, &tunnelLocalPort) {
		if direction == "OUTPUT" && isActive {
			cps = append(cps, OutputCP{
				CommPointID:     id.String(),
				Name:            name,
				Protocol:        protocol,
				Host:            host,
				Port:            port,
				MaxRetries:      maxRetries,
				RetryDelayMs:    retryDelay,
				TimeoutMs:       timeout,
				IsActive:        isActive,
				DestTopic:       "*", // TODO: add dest_topic field to CP config
				TunnelEnabled:   tunnelEnabled,
				TunnelNodeID:    tunnelNodeID.String(),
				TunnelLocalPort: tunnelLocalPort,
			})
		}
	}
	iter.Close()

	outputCPsMu.Lock()
	outputCPs = cps
	outputCPsMu.Unlock()

	log.Info("loaded output CPs", logging.Fields{"count": len(cps)})
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
