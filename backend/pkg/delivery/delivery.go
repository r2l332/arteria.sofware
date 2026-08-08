package delivery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"
	"github.com/nats-io/nats.go"
)

const (
	startBlock = 0x0B
	endBlock   = 0x1C
	cr         = 0x0D
)

type OutputCP struct {
	CommPointID     string
	Name            string
	Protocol        string
	Host            string
	Port            int
	IsActive        bool
	TimeoutMs       int
	DestTopic       string
	TunnelEnabled   bool
	TunnelNodeID    string
	TunnelLocalPort int
}

type Deliverer struct {
	nc      *nats.Conn
	session *gocql.Session
	mu      sync.RWMutex
	cps     []OutputCP
}

func New(nc *nats.Conn, session *gocql.Session) *Deliverer {
	d := &Deliverer{nc: nc, session: session}
	d.LoadOutputCPs()
	return d
}

func (d *Deliverer) LoadOutputCPs() {
	cpTopicMap := make(map[string]string)
	routeIter := d.session.Query(`SELECT dest_comm_point_id, destination_topic FROM arteria.routes WHERE is_active = true ALLOW FILTERING`).Iter()
	var destCPID gocql.UUID
	var destTopic string
	for routeIter.Scan(&destCPID, &destTopic) {
		cpTopicMap[destCPID.String()] = destTopic
	}
	routeIter.Close()

	var cps []OutputCP
	iter := d.session.Query(`SELECT comm_point_id, name, direction, protocol, host, port, is_active, timeout_ms, tunnel_enabled, tunnel_node_id, tunnel_local_port FROM arteria.communication_points`).Iter()
	var id, tunnelNodeID gocql.UUID
	var name, direction, protocol, host string
	var port, timeout, tunnelLocalPort int
	var isActive, tunnelEnabled bool
	for iter.Scan(&id, &name, &direction, &protocol, &host, &port, &isActive, &timeout, &tunnelEnabled, &tunnelNodeID, &tunnelLocalPort) {
		if direction == "OUTPUT" && isActive {
			topic := "*"
			if t, ok := cpTopicMap[id.String()]; ok { topic = t }
			cps = append(cps, OutputCP{CommPointID: id.String(), Name: name, Protocol: protocol, Host: host, Port: port, IsActive: isActive, TimeoutMs: timeout, DestTopic: topic, TunnelEnabled: tunnelEnabled, TunnelNodeID: tunnelNodeID.String(), TunnelLocalPort: tunnelLocalPort})
		}
	}
	iter.Close()
	d.mu.Lock()
	d.cps = cps
	d.mu.Unlock()
	log.Printf("[DELIVERY] loaded %d output CPs", len(cps))
}

// DeliverToTargets delivers a message to the specified CP IDs. Returns (delivered, failed).
func (d *Deliverer) DeliverToTargets(payload []byte, destCPIDs []string, destTopic string) (int, int) {
	d.mu.RLock()
	var targets []OutputCP
	if len(destCPIDs) > 0 {
		for _, cp := range d.cps {
			if !cp.IsActive { continue }
			for _, tid := range destCPIDs {
				if cp.CommPointID == tid { targets = append(targets, cp); break }
			}
		}
	} else {
		for _, cp := range d.cps {
			if cp.IsActive && (cp.DestTopic == destTopic || cp.DestTopic == "*") {
				targets = append(targets, cp)
			}
		}
	}
	d.mu.RUnlock()

	delivered, failed := 0, 0
	for _, cp := range targets {
		if err := d.deliver(cp, payload); err != nil {
			log.Printf("[DELIVERY] failed to %s (%s:%d): %v", cp.Name, cp.Host, cp.Port, err)
			failed++
		} else {
			delivered++
		}
	}
	return delivered, failed
}

func (d *Deliverer) deliver(cp OutputCP, payload []byte) error {
	if cp.TunnelEnabled && cp.TunnelNodeID != "" && cp.TunnelNodeID != "00000000-0000-0000-0000-000000000000" {
		return d.deliverViaTunnel(cp, payload)
	}
	timeout := time.Duration(cp.TimeoutMs) * time.Millisecond
	if timeout == 0 { timeout = 30 * time.Second }

	switch strings.ToUpper(cp.Protocol) {
	case "MLLP":
		return deliverMLLP(cp.Host, cp.Port, payload, timeout)
	case "HTTP", "REST", "WEBHOOK":
		return deliverHTTP(cp.Host, cp.Port, payload, timeout)
	case "DISCARD", "NULL", "SINK":
		return nil
	default:
		return fmt.Errorf("unsupported protocol: %s", cp.Protocol)
	}
}

func (d *Deliverer) deliverViaTunnel(cp OutputCP, payload []byte) error {
	targetPort := cp.TunnelLocalPort
	if targetPort == 0 { targetPort = cp.Port }

	wirePayload := payload
	if strings.ToUpper(cp.Protocol) == "MLLP" {
		var buf bytes.Buffer
		buf.WriteByte(startBlock)
		buf.Write(payload)
		buf.WriteByte(endBlock)
		buf.WriteByte(cr)
		wirePayload = buf.Bytes()
	}

	req := struct {
		NodeID     string `json:"node_id"`
		TargetPort int    `json:"target_port"`
		Protocol   string `json:"protocol"`
		Payload    []byte `json:"payload"`
	}{NodeID: cp.TunnelNodeID, TargetPort: targetPort, Protocol: cp.Protocol, Payload: wirePayload}
	data, _ := json.Marshal(req)

	msg, err := d.nc.Request("arteria.tunnel.deliver", data, 10*time.Second)
	if err != nil { return fmt.Errorf("tunnel to node %s port %d: %w", cp.TunnelNodeID, targetPort, err) }

	var resp struct { Success bool `json:"success"`; Error string `json:"error,omitempty"` }
	if json.Unmarshal(msg.Data, &resp) != nil || !resp.Success {
		return fmt.Errorf("tunnel delivery failed: %s", resp.Error)
	}
	return nil
}

func deliverMLLP(host string, port int, payload []byte, timeout time.Duration) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil { return fmt.Errorf("mllp connect %s: %w", addr, err) }
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	hl7 := extractHL7(payload)
	var buf bytes.Buffer
	buf.WriteByte(startBlock)
	buf.Write(hl7)
	buf.WriteByte(endBlock)
	buf.WriteByte(cr)
	_, err = conn.Write(buf.Bytes())
	if err != nil { return fmt.Errorf("mllp write %s: %w", addr, err) }

	ackBuf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	conn.Read(ackBuf)
	return nil
}

func deliverHTTP(host string, port int, payload []byte, timeout time.Duration) error {
	url := fmt.Sprintf("http://%s:%d", host, port)
	if port == 443 { url = fmt.Sprintf("https://%s", host) }
	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil { return fmt.Errorf("http post %s: %w", url, err) }
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 { return fmt.Errorf("http %s returned %d", url, resp.StatusCode) }
	return nil
}

func extractHL7(data []byte) []byte {
	var envelope struct { RawPayload string `json:"rawPayload"` }
	if json.Unmarshal(data, &envelope) == nil && envelope.RawPayload != "" {
		return []byte(envelope.RawPayload)
	}
	return data
}
