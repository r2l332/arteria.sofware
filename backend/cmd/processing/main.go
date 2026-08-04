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
	"github.com/r2l332/arteria.app/backend/pkg/engine"
	"github.com/r2l332/arteria.app/backend/pkg/hl7"
	"github.com/r2l332/arteria.app/backend/pkg/logging"
	"github.com/r2l332/arteria.app/backend/pkg/metrics"
	"github.com/r2l332/arteria.app/backend/pkg/natsutil"
	"github.com/r2l332/arteria.app/backend/pkg/scyllautil"
	"github.com/r2l332/arteria.app/backend/pkg/v8pool"
)

const (
	subjectRaw   = "arteria.ingest.raw"
	subjectRoute = "arteria.route"
	subjectDLQ   = "arteria.dlq"
	consumerName = "processing-service"
)

var log *logging.Logger
var met *metrics.Counters

func main() {
	var err error
	log, err = logging.FromEnv("processing")
	if err != nil {
		panic("failed to init logger: " + err.Error())
	}
	defer log.Close()

	natsURL := envOrDefault("NATS_URL", nats.DefaultURL)
	scyllaHost := envOrDefault("SCYLLA_HOST", "127.0.0.1")
	v8PoolSize := 8

	log.Info("starting processing service", logging.Fields{
		"nats_url":    natsURL,
		"scylla_host": scyllaHost,
		"v8_pool_size": v8PoolSize,
	})

	// Connect to NATS
	natsCfg := natsutil.DefaultConfig()
	natsCfg.URL = natsURL
	nc, js, err := natsutil.Connect(natsCfg)
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

	// Initialize V8 pool
	pool, err := v8pool.New(v8pool.Config{
		PoolSize: v8PoolSize,
		Timeout:  50 * time.Millisecond,
	})
	if err != nil {
		log.Fatal("failed to create V8 pool", logging.Fields{"error": err.Error()})
	}
	defer pool.Close()
	log.Info("V8 pool initialized", logging.Fields{"pool_size": v8PoolSize})

	// Initialize metrics
	met = metrics.New()

	// Serve metrics via NATS request-reply
	nc.Subscribe("arteria.metrics.processing", func(msg *nats.Msg) {
		snap := met.Snap()
		data, _ := json.Marshal(snap)
		msg.Respond(data)
	})

	// JS Playground — execute ad-hoc scripts via NATS request-reply
	nc.Subscribe("arteria.playground.execute", func(msg *nats.Msg) {
		var req struct {
			Script     string `json:"script"`
			FilterType string `json:"filter_type"`
			Payload    string `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			resp, _ := json.Marshal(map[string]interface{}{"success": false, "error": "invalid request"})
			msg.Respond(resp)
			return
		}

		ctx := context.Background()
		result := pool.Execute(ctx, req.Script, req.Payload)

		if result.Error != nil {
			resp, _ := json.Marshal(map[string]interface{}{
				"success": false,
				"error":   result.Error.Error(),
			})
			msg.Respond(resp)
			return
		}

		resp, _ := json.Marshal(map[string]interface{}{
			"success": true,
			"output":  result.Output,
		})
		msg.Respond(resp)
	})

	// Initialize processing engine
	eng := engine.New(pool, session)
	if err := eng.LoadConfig(); err != nil {
		log.Fatal("failed to load engine config", logging.Fields{"error": err.Error()})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Periodically reload routes/filters/lookups
	eng.StartConfigReloader(ctx, 30*time.Second)

	// Subscribe to raw ingest
	sub, err := js.QueueSubscribe(subjectRaw, consumerName, func(m *nats.Msg) {
		handleMessage(ctx, m, js, session, eng)
	}, nats.Durable(consumerName), nats.ManualAck(), nats.AckWait(30*time.Second))
	if err != nil {
		log.Fatal("failed to subscribe", logging.Fields{"error": err.Error()})
	}
	defer sub.Unsubscribe()

	log.Info("subscribed to ingest stream", logging.Fields{"subject": subjectRaw, "consumer": consumerName})

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

func handleMessage(ctx context.Context, m *nats.Msg, js nats.JetStreamContext, session *gocql.Session, eng *engine.Engine) {
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

	// Build the message envelope (Rhapsody-style message object)
	envelope := &engine.MessageEnvelope{
		MessageID:       msgID.String(),
		MessageType:     parsed.MessageType,
		TriggerEvent:    parsed.TriggerEvent,
		SendingFacility: parsed.SendingFacility,
		PatientID:       parsed.PatientID,
		RawPayload:      parsed.RawPayload,
		Properties:      make(map[string]string),
	}

	// Record message as RECEIVED
	insertMessage(session, gocqlID, envelope, "", "RECEIVED", now)

	// Run through the engine's filter chain
	destTopic, transformedPayload, err := eng.ProcessMessage(ctx, envelope)
	if err != nil {
		log.Warn("filter chain rejected message", logging.Fields{
			"message_id":   msgID.String(),
			"message_type": parsed.MessageType + "^" + parsed.TriggerEvent,
			"error":        err.Error(),
		})

		// Update status to ERROR
		updateMessageStatus(session, gocqlID, "ERROR", err.Error(), now)

		// Send to Dead Letter Queue
		dlqPayload, _ := json.Marshal(map[string]interface{}{
			"message_id":  msgID.String(),
			"error":       err.Error(),
			"raw_payload": parsed.RawPayload,
			"timestamp":   now,
		})
		js.Publish(subjectDLQ+"."+parsed.MessageType, dlqPayload)

		// Record in error_messages table
		session.Query(`INSERT INTO arteria.error_messages (message_id, error_type, error_details, raw_payload, retry_count, max_retries, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			gocqlID, "FILTER_ERROR", err.Error(), parsed.RawPayload, 0, 3, now).Exec()

		log.Debug("message sent to DLQ", logging.Fields{
			"message_id": msgID.String(),
			"dlq_subject": subjectDLQ + "." + parsed.MessageType,
		})

		met.Errors.Add(1)
		met.Rejected.Add(1)
		met.DLQ.Add(1)
		m.Ack()
		return
	}

	// Update message with transformed payload and status
	updateMessageTransformed(session, gocqlID, transformedPayload, "ROUTED", now)

	// Insert into messages_by_patient
	if parsed.PatientID != "" {
		session.Query(`INSERT INTO arteria.messages_by_patient (patient_id, created_at, message_id) VALUES (?, ?, ?)`,
			parsed.PatientID, now, gocqlID).Exec()
	}

	// Insert into messages_by_status
	session.Query(`INSERT INTO arteria.messages_by_status (status, created_at, message_id, message_type, patient_id) VALUES (?, ?, ?, ?, ?)`,
		"ROUTED", now, gocqlID, parsed.MessageType, parsed.PatientID).Exec()

	// Publish to routing subject
	routeSubject := subjectRoute + "." + destTopic
	_, err = js.Publish(routeSubject, []byte(transformedPayload), nats.MsgId(msgID.String()))
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
	log.Info("message processed and routed", logging.Fields{
		"message_id":    msgID.String(),
		"message_type":  parsed.MessageType + "^" + parsed.TriggerEvent,
		"patient_id":    parsed.PatientID,
		"route_subject": routeSubject,
	})
}

func insertMessage(session *gocql.Session, msgID gocql.UUID, env *engine.MessageEnvelope, transformed, status string, now time.Time) {
	propsJSON, _ := json.Marshal(env.Properties)
	if err := session.Query(`INSERT INTO arteria.messages 
		(message_id, patient_id, message_type, trigger_event, sending_facility, raw_payload, transformed_payload, properties, status, created_at, updated_at, retry_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msgID, env.PatientID, env.MessageType, env.TriggerEvent, env.SendingFacility,
		env.RawPayload, transformed, string(propsJSON), status, now, now, 0,
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

func updateMessageTransformed(session *gocql.Session, msgID gocql.UUID, transformed, status string, now time.Time) {
	if err := session.Query(`UPDATE arteria.messages SET transformed_payload = ?, status = ?, updated_at = ? WHERE message_id = ?`,
		transformed, status, now, msgID).Exec(); err != nil {
		log.Error("scylla update transformed failed", logging.Fields{"message_id": msgID.String(), "error": err.Error()})
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
