package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gocql/gocql"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/nats-io/nats.go"
)

// --- WebSocket Hub: streams NATS events to connected browsers ---

type WSHub struct {
	clients    map[*websocket.Conn]bool
	mu         sync.RWMutex
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
}

func newWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

func (h *WSHub) run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
		case conn := <-h.unregister:
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.clients {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					conn.Close()
					delete(h.clients, conn)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// StreamEvent is pushed to browsers via WebSocket
type StreamEvent struct {
	Type      string      `json:"type"`      // "message", "error", "metric", "trace"
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// MessageEvent represents a message flowing through the pipeline
type MessageEvent struct {
	MessageID       string `json:"message_id"`
	MessageType     string `json:"message_type"`
	TriggerEvent    string `json:"trigger_event"`
	PatientID       string `json:"patient_id"`
	SendingFacility string `json:"sending_facility"`
	Status          string `json:"status"`
	RouteName       string `json:"route_name,omitempty"`
	Stage           string `json:"stage"` // "received", "processing", "routed", "delivered", "error"
	RawPayload      string `json:"raw_payload,omitempty"`
	Transformed     string `json:"transformed,omitempty"`
	SizeBytes       int    `json:"size_bytes"`
}

// TraceEvent shows a message's journey through the pipeline
type TraceEvent struct {
	MessageID string      `json:"message_id"`
	Steps     []TraceStep `json:"steps"`
}

type TraceStep struct {
	Stage     string `json:"stage"`
	Timestamp string `json:"timestamp"`
	Component string `json:"component"` // CP name, route name, filter name
	Input     string `json:"input,omitempty"`
	Output    string `json:"output,omitempty"`
	Duration  int64  `json:"duration_ms"`
	Error     string `json:"error,omitempty"`
}

// startNATSBridge subscribes to pipeline NATS subjects and pushes events to the WebSocket hub
func startNATSBridge(nc *nats.Conn, hub *WSHub) {
	// Subscribe to pipeline events published by processing/egress services
	nc.Subscribe("arteria.events.>", func(msg *nats.Msg) {
		event := StreamEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		}

		// Parse the subject to determine event type
		parts := subjectParts(msg.Subject)
		switch {
		case len(parts) >= 3 && parts[2] == "received":
			event.Type = "message"
			var me MessageEvent
			json.Unmarshal(msg.Data, &me)
			me.Stage = "received"
			event.Data = me
		case len(parts) >= 3 && parts[2] == "routed":
			event.Type = "message"
			var me MessageEvent
			json.Unmarshal(msg.Data, &me)
			me.Stage = "routed"
			event.Data = me
		case len(parts) >= 3 && parts[2] == "delivered":
			event.Type = "message"
			var me MessageEvent
			json.Unmarshal(msg.Data, &me)
			me.Stage = "delivered"
			event.Data = me
		case len(parts) >= 3 && parts[2] == "error":
			event.Type = "error"
			var me MessageEvent
			json.Unmarshal(msg.Data, &me)
			me.Stage = "error"
			event.Data = me
		case len(parts) >= 3 && parts[2] == "trace":
			event.Type = "trace"
			var te TraceEvent
			json.Unmarshal(msg.Data, &te)
			event.Data = te
		default:
			event.Type = "event"
			event.Data = json.RawMessage(msg.Data)
		}

		payload, _ := json.Marshal(event)
		select {
		case hub.broadcast <- payload:
		default:
			// Drop if buffer full — don't block NATS
		}
	})

	// Also bridge metrics at 1s intervals
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			resp, err := nc.Request("arteria.metrics.processing", nil, 2*time.Second)
			if err != nil {
				continue
			}
			event := StreamEvent{
				Type:      "metric",
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
				Data:      json.RawMessage(resp.Data),
			}
			payload, _ := json.Marshal(event)
			select {
			case hub.broadcast <- payload:
			default:
			}
		}
	}()

	log.Println("[WS] NATS→WebSocket bridge started")
}

func subjectParts(s string) []string {
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// --- Message Control API: flush, drop, queue, retry ---

// MessageControlAction represents an action on a message
type MessageControlAction struct {
	Action    string `json:"action"`     // "drop", "retry", "flush", "hold", "release"
	MessageID string `json:"message_id"` // specific message (for drop/retry)
	Route     string `json:"route"`      // route filter (for flush)
	Reason    string `json:"reason"`     // audit reason
}

func registerStreamingRoutes(app *fiber.App, nc *nats.Conn, js nats.JetStreamContext, session *gocql.Session, hub *WSHub) {
	// WebSocket endpoint for live streaming
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws/flow", websocket.New(func(c *websocket.Conn) {
		hub.register <- c
		defer func() { hub.unregister <- c }()

		// Keep connection alive, read client messages (for subscriptions/filters)
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				break
			}
			// Client can send filter commands like {"subscribe": "route_id"}
			var cmd struct {
				Subscribe string `json:"subscribe"`
				Action    string `json:"action"`
			}
			json.Unmarshal(msg, &cmd)
			// Future: per-client filtering
		}
	}))

	// Message control endpoints
	api := app.Group("/api/v1")

	// Drop a message (remove from queue, mark as DROPPED)
	api.Post("/messages/:id/drop", func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		var body struct {
			Reason string `json:"reason"`
		}
		c.BodyParser(&body)

		now := time.Now()
		session.Query(`UPDATE arteria.messages SET status = ?, error_details = ?, updated_at = ? WHERE message_id = ?`,
			"DROPPED", fmt.Sprintf("Manually dropped: %s", body.Reason), now, id).Exec()

		// Publish event
		evt, _ := json.Marshal(MessageEvent{MessageID: id.String(), Status: "DROPPED", Stage: "dropped"})
		nc.Publish("arteria.events.dropped", evt)

		return c.JSON(fiber.Map{"status": "dropped", "message_id": id.String()})
	})

	// Retry a failed message
	api.Post("/messages/:id/retry", func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}

		// Get the raw payload
		var raw string
		session.Query(`SELECT raw_payload FROM arteria.messages WHERE message_id = ?`, id).Scan(&raw)
		if raw == "" {
			return c.Status(404).JSON(fiber.Map{"error": "message not found or no payload"})
		}

		// Re-publish to ingestion
		_, err = js.Publish("arteria.ingest.raw", []byte(raw))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "republish failed: " + err.Error()})
		}

		now := time.Now()
		session.Query(`UPDATE arteria.messages SET status = ?, updated_at = ? WHERE message_id = ?`,
			"RETRYING", now, id).Exec()

		return c.JSON(fiber.Map{"status": "retrying", "message_id": id.String()})
	})

	// Flush all messages for a route (clear the queue)
	api.Post("/routes/:id/flush", func(c *fiber.Ctx) error {
		routeID := c.Params("id")
		var body struct {
			Reason string `json:"reason"`
		}
		c.BodyParser(&body)

		// Get route info
		id, _ := gocql.ParseUUID(routeID)
		var destTopic string
		session.Query(`SELECT destination_topic FROM arteria.routes WHERE route_id = ?`, id).Scan(&destTopic)

		if destTopic == "" {
			return c.Status(404).JSON(fiber.Map{"error": "route not found"})
		}

		// Purge the NATS stream for this topic
		streamName := "arteria"
		subject := "arteria.route." + destTopic
		err := js.PurgeStream(streamName, &nats.StreamPurgeRequest{Subject: subject})
		if err != nil {
			// Non-fatal — subject may not exist
			log.Printf("[CONTROL] purge %s: %v", subject, err)
		}

		return c.JSON(fiber.Map{"status": "flushed", "route_id": routeID, "topic": destTopic, "reason": body.Reason})
	})

	// Hold a message (pause delivery, keep in queue)
	api.Post("/messages/:id/hold", func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		now := time.Now()
		session.Query(`UPDATE arteria.messages SET status = ?, updated_at = ? WHERE message_id = ?`,
			"HELD", now, id).Exec()
		return c.JSON(fiber.Map{"status": "held", "message_id": id.String()})
	})

	// Release a held message
	api.Post("/messages/:id/release", func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}

		var raw string
		session.Query(`SELECT raw_payload FROM arteria.messages WHERE message_id = ?`, id).Scan(&raw)
		if raw == "" {
			return c.Status(404).JSON(fiber.Map{"error": "message not found"})
		}

		_, err = js.Publish("arteria.ingest.raw", []byte(raw))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "release failed: " + err.Error()})
		}

		now := time.Now()
		session.Query(`UPDATE arteria.messages SET status = ?, updated_at = ? WHERE message_id = ?`,
			"RECEIVED", now, id).Exec()
		return c.JSON(fiber.Map{"status": "released", "message_id": id.String()})
	})

	// Get message trace (full journey through pipeline)
	api.Get("/messages/:id/trace", func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}

		var pid, mt, te, sf, raw, transformed, props, st, errDet string
		var ca, ua time.Time
		err = session.Query(`SELECT patient_id, message_type, trigger_event, sending_facility, raw_payload, transformed_payload, properties, status, error_details, created_at, updated_at FROM arteria.messages WHERE message_id=?`, id).
			Scan(&pid, &mt, &te, &sf, &raw, &transformed, &props, &st, &errDet, &ca, &ua)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}

		// Build trace from what we know
		steps := []TraceStep{
			{Stage: "received", Timestamp: ca.Format(time.RFC3339), Component: "Ingestion", Input: truncPayload(raw, 500), Duration: 0},
		}

		if transformed != "" {
			steps = append(steps, TraceStep{
				Stage: "processed", Timestamp: ca.Add(50 * time.Millisecond).Format(time.RFC3339),
				Component: "Processing Engine", Input: truncPayload(raw, 500), Output: truncPayload(transformed, 500),
				Duration: int64(ua.Sub(ca).Milliseconds()),
			})
		}

		if st == "ROUTED" || st == "DELIVERED" {
			steps = append(steps, TraceStep{
				Stage: "routed", Timestamp: ua.Format(time.RFC3339),
				Component: "Router", Output: truncPayload(transformed, 500),
			})
		}
		if st == "DELIVERED" {
			steps = append(steps, TraceStep{
				Stage: "delivered", Timestamp: ua.Add(10 * time.Millisecond).Format(time.RFC3339),
				Component: "Egress",
			})
		}
		if st == "ERROR" {
			steps = append(steps, TraceStep{
				Stage: "error", Timestamp: ua.Format(time.RFC3339),
				Component: "Processing Engine", Error: errDet,
			})
		}

		return c.JSON(fiber.Map{
			"message_id": id.String(),
			"status":     st,
			"steps":      steps,
			"message": fiber.Map{
				"patient_id": pid, "message_type": mt, "trigger_event": te,
				"sending_facility": sf, "raw_payload": raw, "transformed_payload": transformed,
			},
		})
	})

	// Test a filter with a specific payload (live debugging)
	api.Post("/filters/:id/test", func(c *fiber.Ctx) error {
		var body struct {
			Payload string `json:"payload"` // JSON MessageEnvelope to test with
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		filterID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid filter ID"})
		}

		// Load the filter script
		var filterType, script string
		session.Query(`SELECT filter_type, js_script FROM arteria.filters_by_id WHERE filter_id = ?`, filterID).Scan(&filterType, &script)
		if script == "" {
			return c.Status(404).JSON(fiber.Map{"error": "filter not found"})
		}

		// Execute via NATS request to processing service
		testReq, _ := json.Marshal(map[string]string{
			"filter_type": filterType,
			"script":      script,
			"payload":     body.Payload,
		})

		resp, err := nc.Request("arteria.filter.test", testReq, 5*time.Second)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "filter test timeout: " + err.Error()})
		}

		var result map[string]interface{}
		json.Unmarshal(resp.Data, &result)
		return c.JSON(result)
	})

	// Get last N messages that passed through a specific route (for live replay)
	api.Get("/routes/:id/recent", func(c *fiber.Ctx) error {
		routeID := c.Params("id")
		limit := c.QueryInt("limit", 10)

		id, _ := gocql.ParseUUID(routeID)
		var destTopic string
		session.Query(`SELECT destination_topic FROM arteria.routes WHERE route_id = ?`, id).Scan(&destTopic)

		// Query recent messages that were routed to this topic
		var messages []fiber.Map
		iter := session.Query(`SELECT message_id, patient_id, message_type, trigger_event, sending_facility, status, created_at FROM arteria.messages LIMIT ?`, limit*5).Iter()
		var msgID gocql.UUID
		var pid, mt, te, sf, st string
		var ca time.Time
		for iter.Scan(&msgID, &pid, &mt, &te, &sf, &st, &ca) {
			if st == "ROUTED" || st == "DELIVERED" {
				messages = append(messages, fiber.Map{
					"message_id": msgID.String(), "patient_id": pid, "message_type": mt,
					"trigger_event": te, "sending_facility": sf, "status": st, "created_at": ca,
				})
				if len(messages) >= limit {
					break
				}
			}
		}
		iter.Close()

		return c.JSON(fiber.Map{"messages": messages, "count": len(messages), "route_id": routeID})
	})
}

func truncPayload(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// publishPipelineEvent is called by processing/egress to emit events for WebSocket streaming
func publishPipelineEvent(nc *nats.Conn, stage string, event MessageEvent) {
	event.Stage = stage
	data, _ := json.Marshal(event)
	nc.Publish("arteria.events."+stage, data)
}

// --- Rewire API: drag-and-drop route/filter reassignment ---

func registerRewireRoutes(app *fiber.App, session *gocql.Session) {
	api := app.Group("/api/v1")

	// Quick rewire: change source or dest CP on a route
	api.Patch("/routes/:id/rewire", func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		var body struct {
			SourceCP *string `json:"source_comm_point_id"`
			DestCP   *string `json:"dest_comm_point_id"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		now := time.Now()
		if body.SourceCP != nil {
			cpID, _ := gocql.ParseUUID(*body.SourceCP)
			session.Query(`UPDATE arteria.routes SET source_comm_point_id = ?, updated_at = ? WHERE route_id = ?`, cpID, now, id).Exec()
		}
		if body.DestCP != nil {
			cpID, _ := gocql.ParseUUID(*body.DestCP)
			session.Query(`UPDATE arteria.routes SET dest_comm_point_id = ?, updated_at = ? WHERE route_id = ?`, cpID, now, id).Exec()
		}

		return c.JSON(fiber.Map{"status": "rewired"})
	})

	// Reorder filters within a route
	api.Put("/routes/:id/filters/reorder", func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var body struct {
			Order []struct {
				FilterID string `json:"filter_id"`
				Position int    `json:"position"`
			} `json:"order"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		// Read all current filters for this route
		type filterRow struct {
			FilterID  gocql.UUID
			Name      string
			Type      string
			Order     int
			Script    string
			Config    string
			IsActive  bool
		}
		var existing []filterRow
		iter := session.Query(`SELECT filter_id, name, filter_type, execution_order, js_script, config_json, is_active FROM arteria.filters WHERE route_id = ?`, routeID).Iter()
		var fr filterRow
		for iter.Scan(&fr.FilterID, &fr.Name, &fr.Type, &fr.Order, &fr.Script, &fr.Config, &fr.IsActive) {
			existing = append(existing, fr)
		}
		iter.Close()

		// Delete all existing filters for this route
		for _, f := range existing {
			session.Query(`DELETE FROM arteria.filters WHERE route_id = ? AND execution_order = ?`, routeID, f.Order).Exec()
		}

		// Re-insert with new ordering
		filterMap := make(map[string]filterRow)
		for _, f := range existing {
			filterMap[f.FilterID.String()] = f
		}
		for _, item := range body.Order {
			f, ok := filterMap[item.FilterID]
			if !ok {
				continue
			}
			session.Query(`INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				f.FilterID, routeID, f.Name, f.Type, item.Position, f.Script, f.Config, f.IsActive, time.Now()).Exec()
		}

		return c.JSON(fiber.Map{"status": "reordered"})
	})

	// Move a filter from one route to another
	api.Post("/filters/:id/move", func(c *fiber.Ctx) error {
		filterID, _ := gocql.ParseUUID(c.Params("id"))
		var body struct {
			FromRouteID string `json:"from_route_id"`
			ToRouteID   string `json:"to_route_id"`
			Position    int    `json:"position"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		fromRoute, _ := gocql.ParseUUID(body.FromRouteID)
		toRoute, _ := gocql.ParseUUID(body.ToRouteID)

		// Read filter data
		var name, filterType, script, configJSON string
		var isActive bool
		var oldOrder int
		session.Query(`SELECT name, filter_type, execution_order, js_script, config_json, is_active FROM arteria.filters_by_id WHERE filter_id = ?`, filterID).
			Scan(&name, &filterType, &oldOrder, &script, &configJSON, &isActive)
		if name == "" {
			return c.Status(404).JSON(fiber.Map{"error": "filter not found"})
		}

		// Delete from old route
		session.Query(`DELETE FROM arteria.filters WHERE route_id = ? AND execution_order = ?`, fromRoute, oldOrder).Exec()

		// Insert into new route
		session.Query(`INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			filterID, toRoute, name, filterType, body.Position, script, configJSON, isActive, time.Now()).Exec()

		return c.JSON(fiber.Map{"status": "moved"})
	})

	// Configure conditional fan-out on a route
	api.Put("/routes/:id/fan-out", func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var body struct {
			Entries []struct {
				CPID          string `json:"cp_id"`
				Name          string `json:"name"`
				ConditionType string `json:"condition_type"` // "python", "javascript", "bash", "dotnet", "" (unconditional)
				Condition     string `json:"condition"`      // Script: reads envelope JSON from stdin, prints "true"/"false"
			} `json:"entries"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		configJSON, _ := json.Marshal(body.Entries)
		session.Query(`UPDATE arteria.routes SET fan_out_config = ?, updated_at = ? WHERE route_id = ?`,
			string(configJSON), time.Now(), routeID).Exec()

		return c.JSON(fiber.Map{"status": "configured", "entries": len(body.Entries)})
	})

	// Get conditional fan-out config for a route
	api.Get("/routes/:id/fan-out", func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var configRaw string
		session.Query(`SELECT fan_out_config FROM arteria.routes WHERE route_id = ?`, routeID).Scan(&configRaw)
		var entries []interface{}
		if configRaw != "" {
			json.Unmarshal([]byte(configRaw), &entries)
		}
		return c.JSON(fiber.Map{"entries": entries, "count": len(entries)})
	})

	// Set default properties on a route (injected into every message before the filter chain)
	api.Put("/routes/:id/properties", func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var body struct {
			Properties map[string]string `json:"properties"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		propsJSON, _ := json.Marshal(body.Properties)
		session.Query(`UPDATE arteria.routes SET default_properties = ?, updated_at = ? WHERE route_id = ?`,
			string(propsJSON), time.Now(), routeID).Exec()
		return c.JSON(fiber.Map{"status": "updated", "properties": body.Properties})
	})

	// Get default properties for a route
	api.Get("/routes/:id/properties", func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var propsRaw string
		session.Query(`SELECT default_properties FROM arteria.routes WHERE route_id = ?`, routeID).Scan(&propsRaw)
		var props map[string]string
		if propsRaw != "" {
			json.Unmarshal([]byte(propsRaw), &props)
		}
		return c.JSON(fiber.Map{"properties": props})
	})

	// Set the next route in a chain (route chaining)
	api.Put("/routes/:id/chain", func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var body struct {
			NextRouteID *string `json:"next_route_id"` // null to clear
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		now := time.Now()
		if body.NextRouteID == nil || *body.NextRouteID == "" {
			session.Query(`UPDATE arteria.routes SET next_route_id = null, updated_at = ? WHERE route_id = ?`, now, routeID).Exec()
			return c.JSON(fiber.Map{"status": "cleared"})
		}
		nextID, err := gocql.ParseUUID(*body.NextRouteID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid next_route_id"})
		}
		// Prevent self-referencing chains
		if nextID == routeID {
			return c.Status(400).JSON(fiber.Map{"error": "route cannot chain to itself"})
		}
		session.Query(`UPDATE arteria.routes SET next_route_id = ?, updated_at = ? WHERE route_id = ?`, nextID, now, routeID).Exec()
		return c.JSON(fiber.Map{"status": "chained", "next_route_id": nextID.String()})
	})

	// Get the chain configuration for a route
	api.Get("/routes/:id/chain", func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var nextRouteID *gocql.UUID
		session.Query(`SELECT next_route_id FROM arteria.routes WHERE route_id = ?`, routeID).Scan(&nextRouteID)
		if nextRouteID == nil {
			return c.JSON(fiber.Map{"next_route_id": nil})
		}
		// Get the next route name for display
		var name string
		session.Query(`SELECT name FROM arteria.routes WHERE route_id = ?`, *nextRouteID).Scan(&name)
		return c.JSON(fiber.Map{"next_route_id": nextRouteID.String(), "next_route_name": name})
	})
}

// --- Platform Admin: DLQ Management, Queue Operations ---

func registerPlatformAdminRoutes(app *fiber.App, nc *nats.Conn, js nats.JetStreamContext, session *gocql.Session) {
	api := app.Group("/api/v1")

	// Bulk retry DLQ errors — supports org_id scoping
	api.Post("/platform/dlq/retry-all", func(c *fiber.Ctx) error {
		var body struct {
			Limit int    `json:"limit"`
			OrgID string `json:"org_id"`
		}
		c.BodyParser(&body)
		if body.Limit <= 0 { body.Limit = 100 }

		var iter *gocql.Iter
		if body.OrgID != "" {
			uid, _ := gocql.ParseUUID(body.OrgID)
			iter = session.Query(`SELECT message_id, raw_payload FROM arteria.error_messages WHERE org_id = ? ALLOW FILTERING`, uid).Iter()
		} else {
			iter = session.Query(`SELECT message_id, raw_payload FROM arteria.error_messages LIMIT ?`, body.Limit).Iter()
		}
		var msgID gocql.UUID
		var rawPayload string
		retried, failed := 0, 0
		for iter.Scan(&msgID, &rawPayload) {
			if rawPayload == "" { continue }
			_, err := js.Publish("arteria.ingest.raw", []byte(rawPayload))
			if err != nil { failed++; continue }
			session.Query(`DELETE FROM arteria.error_messages WHERE message_id = ?`, msgID).Exec()
			session.Query(`UPDATE arteria.messages SET status = ?, updated_at = ? WHERE message_id = ?`, "RETRYING", time.Now(), msgID).Exec()
			retried++
			if retried+failed >= body.Limit { break }
		}
		iter.Close()
		return c.JSON(fiber.Map{"retried": retried, "failed": failed, "org_id": body.OrgID})
	})

	// Bulk drop DLQ errors — supports org_id scoping
	api.Post("/platform/dlq/drop-all", func(c *fiber.Ctx) error {
		var body struct {
			Reason string `json:"reason"`
			OrgID  string `json:"org_id"`
		}
		c.BodyParser(&body)

		var iter *gocql.Iter
		if body.OrgID != "" {
			uid, _ := gocql.ParseUUID(body.OrgID)
			iter = session.Query(`SELECT message_id FROM arteria.error_messages WHERE org_id = ? ALLOW FILTERING`, uid).Iter()
		} else {
			iter = session.Query(`SELECT message_id FROM arteria.error_messages`).Iter()
		}
		var msgID gocql.UUID
		dropped := 0
		for iter.Scan(&msgID) {
			session.Query(`DELETE FROM arteria.error_messages WHERE message_id = ?`, msgID).Exec()
			session.Query(`UPDATE arteria.messages SET status = ?, error_details = ?, updated_at = ? WHERE message_id = ?`,
				"DROPPED", fmt.Sprintf("Bulk dropped: %s", body.Reason), time.Now(), msgID).Exec()
			dropped++
		}
		iter.Close()
		return c.JSON(fiber.Map{"dropped": dropped, "reason": body.Reason, "org_id": body.OrgID})
	})

	// Get DLQ summary — optionally scoped by org_id query param
	api.Get("/platform/dlq/summary", func(c *fiber.Ctx) error {
		orgID := c.Query("org_id", "")

		var oldest, newest time.Time
		var errTypes = make(map[string]int)
		var count int

		var iter *gocql.Iter
		if orgID != "" {
			uid, _ := gocql.ParseUUID(orgID)
			iter = session.Query(`SELECT error_type, created_at FROM arteria.error_messages WHERE org_id = ? ALLOW FILTERING`, uid).Iter()
		} else {
			iter = session.Query(`SELECT error_type, created_at FROM arteria.error_messages`).Iter()
		}
		var errType string
		var createdAt time.Time
		for iter.Scan(&errType, &createdAt) {
			count++
			errTypes[errType]++
			if oldest.IsZero() || createdAt.Before(oldest) { oldest = createdAt }
			if newest.IsZero() || createdAt.After(newest) { newest = createdAt }
		}
		iter.Close()

		return c.JSON(fiber.Map{
			"count": count, "error_types": errTypes,
			"oldest": oldest, "newest": newest, "org_id": orgID,
		})
	})

	// NATS consumer lag per route
	api.Get("/platform/nats/consumers", func(c *fiber.Ctx) error {
		var consumers []fiber.Map
		for consumer := range js.ConsumerNames("ARTERIA") {
			ci, err := js.ConsumerInfo("ARTERIA", consumer)
			if err != nil { continue }
			consumers = append(consumers, fiber.Map{
				"name":           ci.Name,
				"stream":         ci.Stream,
				"pending":        ci.NumPending,
				"ack_pending":    ci.NumAckPending,
				"delivered":      ci.Delivered.Consumer,
				"redelivered":    ci.NumRedelivered,
				"waiting":        ci.NumWaiting,
				"last_delivered": ci.Delivered.Last,
			})
		}
		if consumers == nil { consumers = []fiber.Map{} }
		return c.JSON(fiber.Map{"consumers": consumers, "count": len(consumers)})
	})

	// Purge a NATS stream subject (queue management)
	api.Post("/platform/nats/purge", func(c *fiber.Ctx) error {
		var body struct {
			Subject string `json:"subject"`
		}
		if err := c.BodyParser(&body); err != nil || body.Subject == "" {
			return c.Status(400).JSON(fiber.Map{"error": "subject required"})
		}
		err := js.PurgeStream("ARTERIA", &nats.StreamPurgeRequest{Subject: body.Subject})
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "purged", "subject": body.Subject})
	})

	// System overview with per-org breakdown
	api.Get("/platform/overview", func(c *fiber.Ctx) error {
		// Message stats
		var totalMsgs, totalErrors int
		session.Query(`SELECT COUNT(*) FROM arteria.messages`).Scan(&totalMsgs)
		session.Query(`SELECT COUNT(*) FROM arteria.error_messages`).Scan(&totalErrors)

		// Route stats
		var totalRoutes int
		session.Query(`SELECT COUNT(*) FROM arteria.routes`).Scan(&totalRoutes)
		var activeRoutes int
		routeIter := session.Query(`SELECT is_active FROM arteria.routes`).Iter()
		var isActive bool
		for routeIter.Scan(&isActive) { if isActive { activeRoutes++ } }
		routeIter.Close()

		// CP stats
		var totalCPs, activeCPs, inputCPs, outputCPs int
		cpIter := session.Query(`SELECT direction, is_active FROM arteria.communication_points`).Iter()
		var dir string
		for cpIter.Scan(&dir, &isActive) {
			totalCPs++
			if isActive { activeCPs++ }
			if dir == "INPUT" { inputCPs++ } else { outputCPs++ }
		}
		cpIter.Close()

		// NATS stream state
		var streamMsgs uint64
		var streamBytes uint64
		var pendingMsgs uint64
		si, err := js.StreamInfo("ARTERIA")
		if err == nil {
			streamMsgs = si.State.Msgs
			streamBytes = si.State.Bytes
			for consumer := range js.ConsumerNames("ARTERIA") {
				ci, _ := js.ConsumerInfo("ARTERIA", consumer)
				if ci != nil { pendingMsgs += ci.NumPending }
			}
		}

		// Processing metrics
		var procMetrics map[string]interface{}
		resp, err := nc.Request("arteria.metrics.processing", nil, 2*time.Second)
		if err == nil { json.Unmarshal(resp.Data, &procMetrics) }

		// Per-org DLQ breakdown
		orgDLQ := make(map[string]int)
		orgNames := make(map[string]string)
		dlqIter := session.Query(`SELECT org_id FROM arteria.error_messages`).Iter()
		var dlqOrgID *gocql.UUID
		for dlqIter.Scan(&dlqOrgID) {
			key := "unassigned"
			if dlqOrgID != nil { key = dlqOrgID.String() }
			orgDLQ[key]++
		}
		dlqIter.Close()

		// Get org names
		orgIter := session.Query(`SELECT org_id, name FROM arteria.organisations`).Iter()
		var oID gocql.UUID
		var oName string
		for orgIter.Scan(&oID, &oName) { orgNames[oID.String()] = oName }
		orgIter.Close()

		// Per-org message counts
		orgMsgs := make(map[string]int)
		msgIter := session.Query(`SELECT org_id FROM arteria.messages`).Iter()
		var msgOrgID *gocql.UUID
		for msgIter.Scan(&msgOrgID) {
			key := "unassigned"
			if msgOrgID != nil { key = msgOrgID.String() }
			orgMsgs[key]++
		}
		msgIter.Close()

		// Build per-org summary
		var orgBreakdown []fiber.Map
		allOrgIDs := make(map[string]bool)
		for k := range orgDLQ { allOrgIDs[k] = true }
		for k := range orgMsgs { allOrgIDs[k] = true }
		for oid := range allOrgIDs {
			name := orgNames[oid]
			if name == "" { name = oid }
			orgBreakdown = append(orgBreakdown, fiber.Map{
				"org_id":   oid,
				"name":     name,
				"messages": orgMsgs[oid],
				"dlq":      orgDLQ[oid],
			})
		}

		return c.JSON(fiber.Map{
			"messages":      fiber.Map{"total": totalMsgs, "errors": totalErrors, "error_rate": func() float64 { if totalMsgs == 0 { return 0 }; return float64(totalErrors) / float64(totalMsgs) * 100 }()},
			"routes":        fiber.Map{"total": totalRoutes, "active": activeRoutes},
			"comm_points":   fiber.Map{"total": totalCPs, "active": activeCPs, "input": inputCPs, "output": outputCPs},
			"nats":          fiber.Map{"stream_msgs": streamMsgs, "stream_bytes": streamBytes, "pending": pendingMsgs},
			"processing":    procMetrics,
			"org_breakdown": orgBreakdown,
		})
	})
}
