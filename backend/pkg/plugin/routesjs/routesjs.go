package routesjs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/r2l332/arteria.app/backend/pkg/auth"
	"github.com/r2l332/arteria.app/backend/pkg/engine"
	"github.com/r2l332/arteria.app/backend/pkg/plugin"
	"github.com/r2l332/arteria.app/backend/pkg/v8pool"
)

// Plugin implements the routes, filters, lookup tables, and JS playground.
// It wraps the existing engine + v8pool and exposes them as a modular plugin.
type Plugin struct {
	eng  *engine.Engine
	pool *v8pool.Pool
	deps *plugin.Dependencies
}

// New creates the routes+JS plugin.
func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string {
	return "routes-js"
}

func (p *Plugin) Init(ctx context.Context, deps *plugin.Dependencies) error {
	p.deps = deps

	pool, err := v8pool.New(v8pool.Config{
		PoolSize: 8,
		Timeout:  50 * time.Millisecond,
	})
	if err != nil {
		return fmt.Errorf("v8 pool: %w", err)
	}
	p.pool = pool

	p.eng = engine.New(pool, deps.Session)
	if err := p.eng.LoadConfig(); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	p.eng.StartConfigReloader(ctx, 30*time.Second)
	return nil
}

func (p *Plugin) Process(ctx context.Context, envelope *plugin.MessageEnvelope) (plugin.ProcessResult, bool) {
	// Convert plugin envelope to engine envelope
	engEnvelope := &engine.MessageEnvelope{
		MessageID:       envelope.MessageID,
		MessageType:     envelope.MessageType,
		TriggerEvent:    envelope.TriggerEvent,
		SendingFacility: envelope.SendingFacility,
		PatientID:       envelope.PatientID,
		RawPayload:      envelope.RawPayload,
		Properties:      envelope.Properties,
	}

	destTopic, transformed, err := p.eng.ProcessMessage(ctx, engEnvelope)
	if err != nil {
		return plugin.ProcessResult{Err: err}, false
	}

	return plugin.ProcessResult{
		DestinationTopic:   destTopic,
		TransformedPayload: transformed,
	}, false
}

func (p *Plugin) RegisterNATSHandlers(nc *nats.Conn) {
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
		result := p.pool.Execute(ctx, req.Script, req.Payload)

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
}

func (p *Plugin) RegisterAPIRoutes(router fiber.Router) {
	// --- Routes CRUD ---
	router.Get("/routes", auth.RequirePermission(auth.PermRouteView), p.listRoutes)
	router.Get("/routes/:id", auth.RequirePermission(auth.PermRouteView), p.getRoute)
	router.Post("/routes", auth.RequirePermission(auth.PermRouteManage), p.createRoute)
	router.Put("/routes/:id", auth.RequirePermission(auth.PermRouteManage), p.updateRoute)
	router.Delete("/routes/:id", auth.RequirePermission(auth.PermRouteManage), p.deleteRoute)

	// --- Filters ---
	router.Get("/routes/:id/filters", auth.RequirePermission(auth.PermRouteView), p.listFilters)
	router.Post("/routes/:id/filters", auth.RequirePermission(auth.PermRouteManage), p.createFilter)
	router.Put("/filters/:id", auth.RequirePermission(auth.PermRouteManage), p.updateFilter)
	router.Delete("/routes/:routeId/filters/:order", auth.RequirePermission(auth.PermRouteManage), p.deleteFilter)

	// --- Lookup Tables ---
	router.Get("/lookups", auth.RequirePermission(auth.PermRouteView), p.listLookupTables)
	router.Post("/lookups", auth.RequirePermission(auth.PermRouteManage), p.createLookupTable)
	router.Get("/lookups/:id/entries", auth.RequirePermission(auth.PermRouteView), p.listLookupEntries)
	router.Put("/lookups/:id/entries", auth.RequirePermission(auth.PermRouteManage), p.upsertLookupEntry)

	// --- JS Playground ---
	router.Post("/playground/execute", auth.RequirePermission(auth.PermPlayground), p.executePlayground)
}

func (p *Plugin) Close() error {
	if p.pool != nil {
		p.pool.Close()
	}
	return nil
}

// ==================== Route Handlers ====================

func (p *Plugin) listRoutes(c *fiber.Ctx) error {
	routes := p.eng.GetRoutes()
	return c.JSON(routes)
}

func (p *Plugin) getRoute(c *fiber.Ctx) error {
	id := c.Params("id")
	uid, err := uuid.Parse(id)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid route id"})
	}

	routes := p.eng.GetRoutes()
	for _, r := range routes {
		if r.RouteID.String() == uid.String() {
			return c.JSON(r)
		}
	}
	return c.Status(404).JSON(fiber.Map{"error": "route not found"})
}

func (p *Plugin) createRoute(c *fiber.Ctx) error {
	var req struct {
		Name             string `json:"name"`
		SourceTopic      string `json:"source_topic"`
		DestinationTopic string `json:"destination_topic"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.Name == "" || req.SourceTopic == "" || req.DestinationTopic == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name, source_topic, and destination_topic required"})
	}

	routeID := uuid.New()
	if err := p.deps.Session.Query(
		`INSERT INTO arteria.routes (route_id, name, source_topic, destination_topic, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		toGocqlUUID(routeID), req.Name, req.SourceTopic, req.DestinationTopic, true, time.Now(), time.Now(),
	).Exec(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	p.eng.LoadConfig()
	return c.Status(201).JSON(fiber.Map{"route_id": routeID.String(), "name": req.Name})
}

func (p *Plugin) updateRoute(c *fiber.Ctx) error {
	id := c.Params("id")
	uid, err := parseUUID(id)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid route id"})
	}

	var req struct {
		Name             *string `json:"name"`
		SourceTopic      *string `json:"source_topic"`
		DestinationTopic *string `json:"destination_topic"`
		IsActive         *bool   `json:"is_active"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if req.Name != nil {
		p.deps.Session.Query(`UPDATE arteria.routes SET name = ?, updated_at = ? WHERE route_id = ?`, *req.Name, time.Now(), uid).Exec()
	}
	if req.SourceTopic != nil {
		p.deps.Session.Query(`UPDATE arteria.routes SET source_topic = ?, updated_at = ? WHERE route_id = ?`, *req.SourceTopic, time.Now(), uid).Exec()
	}
	if req.DestinationTopic != nil {
		p.deps.Session.Query(`UPDATE arteria.routes SET destination_topic = ?, updated_at = ? WHERE route_id = ?`, *req.DestinationTopic, time.Now(), uid).Exec()
	}
	if req.IsActive != nil {
		p.deps.Session.Query(`UPDATE arteria.routes SET is_active = ?, updated_at = ? WHERE route_id = ?`, *req.IsActive, time.Now(), uid).Exec()
	}

	p.eng.LoadConfig()
	return c.JSON(fiber.Map{"status": "updated"})
}

func (p *Plugin) deleteRoute(c *fiber.Ctx) error {
	id := c.Params("id")
	uid, err := parseUUID(id)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid route id"})
	}

	if err := p.deps.Session.Query(`DELETE FROM arteria.routes WHERE route_id = ?`, uid).Exec(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	p.eng.LoadConfig()
	return c.JSON(fiber.Map{"status": "deleted"})
}

// ==================== Filter Handlers ====================

func (p *Plugin) listFilters(c *fiber.Ctx) error {
	id := c.Params("id")
	uid, err := parseUUID(id)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid route id"})
	}

	var filters []map[string]interface{}
	iter := p.deps.Session.Query(`SELECT filter_id, name, filter_type, execution_order, js_script, config_json, is_active FROM arteria.filters WHERE route_id = ?`, uid).Iter()
	var filterID interface{}
	var name, filterType, jsScript, configJSON string
	var order int
	var isActive bool
	for iter.Scan(&filterID, &name, &filterType, &order, &jsScript, &configJSON, &isActive) {
		filters = append(filters, map[string]interface{}{
			"filter_id":       filterID,
			"name":            name,
			"filter_type":     filterType,
			"execution_order": order,
			"js_script":       jsScript,
			"config_json":     configJSON,
			"is_active":       isActive,
		})
	}
	iter.Close()
	if filters == nil {
		filters = []map[string]interface{}{}
	}
	return c.JSON(filters)
}

func (p *Plugin) createFilter(c *fiber.Ctx) error {
	routeID := c.Params("id")
	uid, err := parseUUID(routeID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid route id"})
	}

	var req struct {
		Name           string `json:"name"`
		FilterType     string `json:"filter_type"`
		ExecutionOrder int    `json:"execution_order"`
		JSScript       string `json:"js_script"`
		ConfigJSON     string `json:"config_json"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.Name == "" || req.FilterType == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name and filter_type required"})
	}

	filterID := uuid.New()
	if err := p.deps.Session.Query(
		`INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		toGocqlUUID(filterID), uid, req.Name, req.FilterType, req.ExecutionOrder, req.JSScript, req.ConfigJSON, true, time.Now(),
	).Exec(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	p.eng.LoadConfig()
	return c.Status(201).JSON(fiber.Map{"filter_id": filterID.String()})
}

func (p *Plugin) updateFilter(c *fiber.Ctx) error {
	id := c.Params("id")
	uid, err := parseUUID(id)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid filter id"})
	}

	var req struct {
		Name       *string `json:"name"`
		JSScript   *string `json:"js_script"`
		ConfigJSON *string `json:"config_json"`
		IsActive   *bool   `json:"is_active"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if req.Name != nil {
		p.deps.Session.Query(`UPDATE arteria.filters SET name = ? WHERE filter_id = ?`, *req.Name, uid).Exec()
	}
	if req.JSScript != nil {
		p.deps.Session.Query(`UPDATE arteria.filters SET js_script = ? WHERE filter_id = ?`, *req.JSScript, uid).Exec()
	}
	if req.ConfigJSON != nil {
		p.deps.Session.Query(`UPDATE arteria.filters SET config_json = ? WHERE filter_id = ?`, *req.ConfigJSON, uid).Exec()
	}
	if req.IsActive != nil {
		p.deps.Session.Query(`UPDATE arteria.filters SET is_active = ? WHERE filter_id = ?`, *req.IsActive, uid).Exec()
	}

	p.eng.LoadConfig()
	return c.JSON(fiber.Map{"status": "updated"})
}

func (p *Plugin) deleteFilter(c *fiber.Ctx) error {
	routeID := c.Params("routeId")
	uid, err := parseUUID(routeID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid route id"})
	}

	order := c.Params("order")
	if err := p.deps.Session.Query(`DELETE FROM arteria.filters WHERE route_id = ? AND execution_order = ?`, uid, order).Exec(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	p.eng.LoadConfig()
	return c.JSON(fiber.Map{"status": "deleted"})
}

// ==================== Lookup Table Handlers ====================

func (p *Plugin) listLookupTables(c *fiber.Ctx) error {
	var tables []map[string]interface{}
	iter := p.deps.Session.Query(`SELECT table_id, name, description, created_at FROM arteria.lookup_tables`).Iter()
	var tableID interface{}
	var name, description string
	var createdAt time.Time
	for iter.Scan(&tableID, &name, &description, &createdAt) {
		tables = append(tables, map[string]interface{}{
			"table_id":    tableID,
			"name":        name,
			"description": description,
			"created_at":  createdAt,
		})
	}
	iter.Close()
	if tables == nil {
		tables = []map[string]interface{}{}
	}
	return c.JSON(tables)
}

func (p *Plugin) createLookupTable(c *fiber.Ctx) error {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name required"})
	}

	tableID := uuid.New()
	if err := p.deps.Session.Query(
		`INSERT INTO arteria.lookup_tables (table_id, name, description, created_at) VALUES (?, ?, ?, ?)`,
		toGocqlUUID(tableID), req.Name, req.Description, time.Now(),
	).Exec(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"table_id": tableID.String(), "name": req.Name})
}

func (p *Plugin) listLookupEntries(c *fiber.Ctx) error {
	id := c.Params("id")
	uid, err := parseUUID(id)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid table id"})
	}

	var entries []map[string]string
	iter := p.deps.Session.Query(`SELECT lookup_key, lookup_value FROM arteria.lookup_entries WHERE table_id = ?`, uid).Iter()
	var key, value string
	for iter.Scan(&key, &value) {
		entries = append(entries, map[string]string{"key": key, "value": value})
	}
	iter.Close()
	if entries == nil {
		entries = []map[string]string{}
	}
	return c.JSON(entries)
}

func (p *Plugin) upsertLookupEntry(c *fiber.Ctx) error {
	id := c.Params("id")
	uid, err := parseUUID(id)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid table id"})
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.Key == "" {
		return c.Status(400).JSON(fiber.Map{"error": "key required"})
	}

	if err := p.deps.Session.Query(
		`INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (?, ?, ?)`,
		uid, req.Key, req.Value,
	).Exec(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "upserted"})
}

// ==================== Playground Handler ====================

func (p *Plugin) executePlayground(c *fiber.Ctx) error {
	var req struct {
		Script     string `json:"script"`
		FilterType string `json:"filter_type"`
		Payload    string `json:"payload"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.Script == "" {
		return c.Status(400).JSON(fiber.Map{"error": "script required"})
	}

	ctx := context.Background()
	result := p.pool.Execute(ctx, req.Script, req.Payload)
	if result.Error != nil {
		return c.JSON(fiber.Map{"success": false, "error": result.Error.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "output": result.Output})
}

// ==================== Helpers ====================

func parseUUID(s string) (interface{}, error) {
	uid, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return toGocqlUUID(uid), nil
}

func toGocqlUUID(id uuid.UUID) [16]byte {
	return [16]byte(id)
}
