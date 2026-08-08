package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/r2l332/arteria.app/backend/pkg/delivery"
	"github.com/r2l332/arteria.app/backend/pkg/engine"
	"github.com/r2l332/arteria.app/backend/pkg/hl7"
	"github.com/r2l332/arteria.app/backend/pkg/logging"
	"github.com/r2l332/arteria.app/backend/pkg/metrics"
	"github.com/r2l332/arteria.app/backend/pkg/natsutil"
	"github.com/r2l332/arteria.app/backend/pkg/plugin"
	"github.com/r2l332/arteria.app/backend/pkg/plugin/routesjs"
	"github.com/r2l332/arteria.app/backend/pkg/scyllautil"
)

const (
	subjectRaw   = "arteria.ingest.raw"
	subjectRoute = "arteria.route"
	subjectDLQ   = "arteria.dlq"
	consumerName = "processing-service"
)

var log *logging.Logger
var met *metrics.Counters
var registry *plugin.Registry
var nc *nats.Conn
var deliverer *delivery.Deliverer

func main() {
	var err error
	log, err = logging.FromEnv("processing")
	if err != nil {
		panic("failed to init logger: " + err.Error())
	}
	defer log.Close()

	natsURL := envOrDefault("NATS_URL", nats.DefaultURL)
	scyllaHost := envOrDefault("SCYLLA_HOST", "127.0.0.1")
	enabledPlugins := envOrDefault("ENABLE_PLUGINS", "routes-js") // comma-separated

	log.Info("starting processing service", logging.Fields{
		"nats_url":    natsURL,
		"scylla_host": scyllaHost,
		"plugins":     enabledPlugins,
	})

	// Connect to NATS
	natsCfg := natsutil.DefaultConfig()
	natsCfg.URL = natsURL
	var js nats.JetStreamContext
	nc, js, err = natsutil.Connect(natsCfg)
	if err != nil {
		log.Fatal("failed to connect to NATS", logging.Fields{"error": err.Error()})
	}
	defer nc.Close()

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

	// Initialize delivery (delivers to output CPs directly from processing)
	deliverer = delivery.New(nc, session)

	// Serve metrics via NATS request-reply
	nc.Subscribe("arteria.metrics.processing", func(msg *nats.Msg) {
		snap := met.Snap()
		data, _ := json.Marshal(snap)
		msg.Respond(data)
	})

	// --- Plugin Registry ---
	registry = plugin.NewRegistry()
	pluginSet := parsePluginList(enabledPlugins)

	if pluginSet["routes-js"] {
		registry.Register(routesjs.New())
		log.Info("plugin registered", logging.Fields{"plugin": "routes-js"})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := &plugin.Dependencies{
		Session: session,
		NC:      nc,
		JS:      js,
	}
	if err := registry.InitAll(ctx, deps); err != nil {
		log.Fatal("plugin init failed", logging.Fields{"error": err.Error()})
	}
	defer registry.CloseAll()

	// Register plugin NATS handlers
	registry.RegisterAllNATSHandlers(nc)

	// Subscribe to raw ingest
	sub, err := js.QueueSubscribe(subjectRaw, consumerName, func(m *nats.Msg) {
		handleMessage(ctx, m, js, session, registry)
	}, nats.Durable(consumerName), nats.ManualAck(), nats.AckWait(30*time.Second))
	if err != nil {
		log.Fatal("failed to subscribe", logging.Fields{"error": err.Error()})
	}
	defer sub.Unsubscribe()

	log.Info("subscribed to ingest stream", logging.Fields{"subject": subjectRaw, "consumer": consumerName})

	// Reload delivery output CPs periodically
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for { select { case <-ctx.Done(): return; case <-ticker.C: deliverer.LoadOutputCPs() } }
	}()

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("received shutdown signal", logging.Fields{"signal": sig.String()})

	cancel()
	sub.Drain()
	nc.Drain()
	log.Info("shutdown complete")
}

// toGocqlUUID converts a google/uuid.UUID to gocql.UUID.
func toGocqlUUID(id uuid.UUID) gocql.UUID {
	return gocql.UUID(id)
}

func handleMessage(ctx context.Context, m *nats.Msg, js nats.JetStreamContext, session *gocql.Session, reg *plugin.Registry) {
	parsed := hl7.Parse(m.Data)

	msgID, err := uuid.NewRandom()
	if err != nil {
		log.Error("uuid generation failed", logging.Fields{"error": err.Error()})
		m.Nak()
		return
	}

	gocqlID := toGocqlUUID(msgID)
	now := time.Now()

	log.Trace("message received from NATS", logging.Fields{
		"message_id":   msgID.String(),
		"message_type": parsed.MessageType,
		"trigger_event": parsed.TriggerEvent,
		"patient_id":   parsed.PatientID,
		"facility":     parsed.SendingFacility,
		"size_bytes":   len(m.Data),
	})
	met.Received.Add(1)
	met.BytesIn.Add(int64(len(m.Data)))

	// Build the message envelope
	envelope := &plugin.MessageEnvelope{
		MessageID:       msgID.String(),
		MessageType:     parsed.MessageType,
		TriggerEvent:    parsed.TriggerEvent,
		SendingFacility: parsed.SendingFacility,
		PatientID:       parsed.PatientID,
		RawPayload:      parsed.RawPayload,
		Properties:      make(map[string]string),
	}

	// Record message as RECEIVED
	engEnvelope := &engine.MessageEnvelope{
		MessageID:       envelope.MessageID,
		MessageType:     envelope.MessageType,
		TriggerEvent:    envelope.TriggerEvent,
		SendingFacility: envelope.SendingFacility,
		PatientID:       envelope.PatientID,
		RawPayload:      envelope.RawPayload,
		Properties:      envelope.Properties,
		Segments:        hl7.ParseSegments(envelope.RawPayload),
	}
	insertMessage(session, gocqlID, engEnvelope, "", "RECEIVED", now, nil)

	// Emit event for WebSocket streaming
	nc.Publish("arteria.events.received", mustJSON(map[string]interface{}{
		"message_id": msgID.String(), "message_type": parsed.MessageType,
		"trigger_event": parsed.TriggerEvent, "patient_id": parsed.PatientID,
		"sending_facility": parsed.SendingFacility, "size_bytes": len(m.Data), "status": "RECEIVED",
	}))

	// Run through the plugin registry
	result := reg.Process(ctx, envelope)

	// Resolve org_id for this message
	var orgID *gocql.UUID
	if result.OrgID != "" {
		if parsed, err := gocql.ParseUUID(result.OrgID); err == nil {
			orgID = &parsed
		}
	}
	var routeUUID *gocql.UUID
	if result.RouteID != "" {
		if parsed, err := gocql.ParseUUID(result.RouteID); err == nil {
			routeUUID = &parsed
		}
	}
	var sourceCPUUID *gocql.UUID
	if result.SourceCPID != "" {
		if parsed, err := gocql.ParseUUID(result.SourceCPID); err == nil {
			sourceCPUUID = &parsed
		}
	}

	if result.Err != nil {
		log.Warn("plugin chain rejected message", logging.Fields{
			"message_id":   msgID.String(),
			"message_type": parsed.MessageType + "^" + parsed.TriggerEvent,
			"error":        result.Err.Error(),
		})

		updateMessageOrgAndStatus(session, gocqlID, "ERROR", result.Err.Error(), orgID, now)

		dlqPayload, _ := json.Marshal(map[string]interface{}{
			"message_id":  msgID.String(),
			"error":       result.Err.Error(),
			"raw_payload": parsed.RawPayload,
			"timestamp":   now,
		})
		js.Publish(subjectDLQ+"."+parsed.MessageType, dlqPayload)

		session.Query(`INSERT INTO arteria.error_messages (message_id, error_type, error_details, raw_payload, retry_count, max_retries, org_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			gocqlID, "FILTER_ERROR", result.Err.Error(), parsed.RawPayload, 0, 3, orgID, now).Exec()

		met.Errors.Add(1)
		met.Rejected.Add(1)
		met.DLQ.Add(1)

		// Emit error event for WebSocket streaming
		nc.Publish("arteria.events.error", mustJSON(map[string]interface{}{
			"message_id": msgID.String(), "message_type": parsed.MessageType,
			"trigger_event": parsed.TriggerEvent, "patient_id": parsed.PatientID,
			"sending_facility": parsed.SendingFacility, "status": "ERROR",
			"route_id": result.RouteID, "error": result.Err.Error(),
		}))

		m.Ack()
		return
	}

	destTopic := result.DestinationTopic
	wirePayload := result.TransformedPayload  // rawPayload content for delivery
	destCPIDs := result.DestCommPointIDs

	// Store full envelope JSON in DB for audit (includes properties, metadata)
	dbPayload := wirePayload
	if result.Envelope != nil {
		envelopeJSON, _ := json.Marshal(result.Envelope)
		dbPayload = string(envelopeJSON)
	}
	updateMessageTransformed(session, gocqlID, dbPayload, "ROUTED", now, orgID, routeUUID, sourceCPUUID)

	if parsed.PatientID != "" {
		session.Query(`INSERT INTO arteria.messages_by_patient (patient_id, created_at, message_id) VALUES (?, ?, ?)`,
			parsed.PatientID, now, gocqlID).Exec()
	}

	session.Query(`INSERT INTO arteria.messages_by_status (status, created_at, message_id, message_type, patient_id) VALUES (?, ?, ?, ?, ?)`,
		"ROUTED", now, gocqlID, parsed.MessageType, parsed.PatientID).Exec()

	routeSubject := subjectRoute + "." + destTopic
	msg := nats.NewMsg(routeSubject)
	msg.Data = []byte(wirePayload)
	if len(destCPIDs) > 0 {
		msg.Header = nats.Header{}
		msg.Header.Set("X-Dest-CP", strings.Join(destCPIDs, ","))
	}
	_, err = js.PublishMsg(msg, nats.MsgId(msgID.String()))
	if err != nil {
		log.Error("failed to publish routed message", logging.Fields{
			"message_id":    msgID.String(),
			"route_subject": routeSubject,
			"error":         err.Error(),
		})
		updateMessageStatus(session, gocqlID, "ERROR", "publish failed: "+err.Error(), now)
		m.Nak()
		return
	}

	m.Ack()
	met.Processed.Add(1)
	met.Routed.Add(1)

	// Deliver directly to output CPs (no separate egress service needed)
	go func() {
		delivered, failed := deliverer.DeliverToTargets([]byte(wirePayload), destCPIDs, destTopic)
		if delivered > 0 {
			updateMessageStatus(session, gocqlID, "DELIVERED", "", now)
			nc.Publish("arteria.events.delivered", mustJSON(map[string]interface{}{
				"message_id": msgID.String(), "message_type": parsed.MessageType,
				"trigger_event": parsed.TriggerEvent, "patient_id": parsed.PatientID,
				"status": "DELIVERED", "delivered_to": delivered,
			}))
		}
		if failed > 0 {
			log.Warn("some deliveries failed", logging.Fields{"message_id": msgID.String(), "delivered": delivered, "failed": failed})
		}
	}()

	// Emit routed event for WebSocket streaming
	nc.Publish("arteria.events.routed", mustJSON(map[string]interface{}{
		"message_id": msgID.String(), "message_type": parsed.MessageType,
		"trigger_event": parsed.TriggerEvent, "patient_id": parsed.PatientID,
		"sending_facility": parsed.SendingFacility, "status": "ROUTED",
		"route_name": destTopic, "route_id": result.RouteID, "size_bytes": len(wirePayload),
	}))

	log.Info("message processed and routed", logging.Fields{
		"message_id":    msgID.String(),
		"message_type":  parsed.MessageType + "^" + parsed.TriggerEvent,
		"patient_id":    parsed.PatientID,
		"route_subject": routeSubject,
	})
}

func insertMessage(session *gocql.Session, msgID gocql.UUID, env *engine.MessageEnvelope, transformed, status string, now time.Time, orgID *gocql.UUID) {
	propsJSON, _ := json.Marshal(env.Properties)
	if err := session.Query(`INSERT INTO arteria.messages 
		(message_id, patient_id, message_type, trigger_event, sending_facility, raw_payload, transformed_payload, properties, status, org_id, created_at, updated_at, retry_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msgID, env.PatientID, env.MessageType, env.TriggerEvent, env.SendingFacility,
		env.RawPayload, transformed, string(propsJSON), status, orgID, now, now, 0,
	).Exec(); err != nil {
		log.Error("scylla insert failed", logging.Fields{"table": "messages", "message_id": msgID.String(), "error": err.Error()})
	}
}

func updateMessageStatus(session *gocql.Session, msgID gocql.UUID, status, errDetails string, now time.Time) {
	if err := session.Query(`UPDATE arteria.messages SET status = ?, error_details = ?, updated_at = ? WHERE message_id = ?`,
		status, errDetails, now, msgID).Exec(); err != nil {
		log.Error("scylla update status failed", logging.Fields{"message_id": msgID.String(), "error": err.Error()})
	}
}

func updateMessageOrgAndStatus(session *gocql.Session, msgID gocql.UUID, status, errDetails string, orgID *gocql.UUID, now time.Time) {
	if err := session.Query(`UPDATE arteria.messages SET status = ?, error_details = ?, org_id = ?, updated_at = ? WHERE message_id = ?`,
		status, errDetails, orgID, now, msgID).Exec(); err != nil {
		log.Error("scylla update status failed", logging.Fields{"message_id": msgID.String(), "error": err.Error()})
	}
}

func updateMessageTransformed(session *gocql.Session, msgID gocql.UUID, transformed, status string, now time.Time, orgID, routeID, commPointID *gocql.UUID) {
	if err := session.Query(`UPDATE arteria.messages SET transformed_payload = ?, status = ?, org_id = ?, route_id = ?, comm_point_id = ?, updated_at = ? WHERE message_id = ?`,
		transformed, status, orgID, routeID, commPointID, now, msgID).Exec(); err != nil {
		log.Error("scylla update transformed failed", logging.Fields{"message_id": msgID.String(), "error": err.Error()})
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parsePluginList(s string) map[string]bool {
	m := make(map[string]bool)
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			m[p] = true
		}
	}
	return m
}

func mustJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
