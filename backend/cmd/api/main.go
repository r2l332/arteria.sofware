package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gocql/gocql"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/r2l332/arteria.app/backend/pkg/auth"
	"github.com/r2l332/arteria.app/backend/pkg/connectors"
	"github.com/r2l332/arteria.app/backend/pkg/logging"
	"github.com/r2l332/arteria.app/backend/pkg/metrics"
	"github.com/r2l332/arteria.app/backend/pkg/natsutil"
	"github.com/r2l332/arteria.app/backend/pkg/scyllautil"
	"github.com/r2l332/arteria.app/backend/pkg/security"
)

func main() {
	log, err := logging.FromEnv("api")
	if err != nil {
		panic("failed to init logger: " + err.Error())
	}
	defer log.Close()

	scyllaHost := envOrDefault("SCYLLA_HOST", "127.0.0.1")
	apiPort := envOrDefault("API_PORT", "8080")
	natsURL := envOrDefault("NATS_URL", nats.DefaultURL)

	// Connect to NATS for metrics aggregation
	natsCfg := natsutil.DefaultConfig()
	natsCfg.URL = natsURL
	nc, _, err := natsutil.Connect(natsCfg)
	if err != nil {
		log.Fatal("failed to connect to NATS", logging.Fields{"error": err.Error()})
	}
	defer nc.Close()

	scyllaCfg := scyllautil.DefaultConfig()
	scyllaCfg.Hosts = strings.Split(scyllaHost, ",")
	session, err := scyllautil.Connect(scyllaCfg)
	if err != nil {
		log.Fatal("failed to connect to ScyllaDB", logging.Fields{"error": err.Error()})
	}
	defer session.Close()

	// Auth setup
	jwtSecret := envOrDefault("JWT_SECRET", "")
	if jwtSecret == "" {
		jwtSecret = auth.GenerateSecret()
		log.Info("generated ephemeral JWT secret (set JWT_SECRET env for persistence)")
	}
	authCfg := auth.Config{
		JWTSecret:   jwtSecret,
		TokenExpiry: 24 * time.Hour,
		DefaultUser: envOrDefault("ADMIN_USER", "admin"),
		DefaultPass: envOrDefault("ADMIN_PASS", "arteria"),
	}

	// Create default admin user if none exist
	if err := auth.EnsureDefaultUser(session, authCfg.DefaultUser, authCfg.DefaultPass); err != nil {
		log.Warn("could not ensure default user", logging.Fields{"error": err.Error()})
	}

	app := fiber.New(fiber.Config{AppName: "Arteria API"})
	app.Use(fiberlogger.New())
	app.Use(security.Headers())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     envOrDefault("CORS_ORIGINS", "*"),
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowMethods:     "GET, POST, PUT, DELETE, OPTIONS",
		AllowCredentials: false,
	}))

	// Rate limiter for login
	loginLimiter := auth.NewRateLimiter(5, 5*time.Minute, 15*time.Minute)

	// Audit logger
	auditLog := security.NewAuditLogger(session)
	_ = auditLog // Used in login handler and audit middleware

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// --- Public Auth Endpoints (no middleware) ---
	app.Post("/api/v1/auth/login", loginHandler(session, authCfg, loginLimiter, auditLog))

	// --- Auth Middleware for all /api/v1/* routes ---
	api := app.Group("/api/v1", authMiddleware(authCfg.JWTSecret))

	// --- Auth Management ---
	api.Get("/auth/me", func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*auth.Claims)
		return c.JSON(fiber.Map{"user_id": claims.UserID, "username": claims.Username, "role": claims.Role})
	})
	api.Post("/auth/change-password", changePasswordHandler(session, authCfg))

	// --- Runtime Config ---
	api.Get("/config/log-level", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"level": logging.ParseLevel(os.Getenv("LOG_LEVEL")).String()})
	})
	api.Put("/config/log-level", func(c *fiber.Ctx) error {
		var body struct{ Level string `json:"level"` }
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		newLevel := logging.ParseLevel(body.Level)
		log.SetLevel(newLevel)
		log.Info("log level changed", logging.Fields{"new_level": body.Level})
		return c.JSON(fiber.Map{"status": "updated", "level": body.Level})
	})

	// --- Module Configuration ---
	api.Get("/config/modules", func(c *fiber.Ctx) error {
		modules := fiber.Map{
			"hl7":    envOrDefault("MODULE_HL7", "true") == "true",
			"fhir":   envOrDefault("MODULE_FHIR", "false") == "true",
			"dicom":  envOrDefault("MODULE_DICOM", "false") == "true",
			"tunnel": envOrDefault("MODULE_TUNNEL", "true") == "true",
		}
		return c.JSON(fiber.Map{"modules": modules})
	})

	// --- Communication Points ---
	api.Get("/comm-points", auth.RequirePermission(auth.PermCPView), listCommPoints(session))
	api.Post("/comm-points", auth.RequirePermission(auth.PermCPManage), createCommPoint(session, nc))
	api.Put("/comm-points/:id", auth.RequirePermission(auth.PermCPManage), updateCommPoint(session, nc))
	api.Delete("/comm-points/:id", auth.RequirePermission(auth.PermCPManage), deleteCommPoint(session))

	// --- Routes ---
	api.Get("/routes", auth.RequirePermission(auth.PermRouteView), listRoutes(session))
	api.Get("/routes/:id", auth.RequirePermission(auth.PermRouteView), getRoute(session))
	api.Post("/routes", auth.RequirePermission(auth.PermRouteManage), createRoute(session))
	api.Put("/routes/:id", auth.RequirePermission(auth.PermRouteManage), updateRoute(session))
	api.Delete("/routes/:id", auth.RequirePermission(auth.PermRouteManage), deleteRoute(session))

	// --- Filters ---
	api.Get("/routes/:id/filters", auth.RequirePermission(auth.PermRouteView), listFilters(session))
	api.Post("/routes/:id/filters", auth.RequirePermission(auth.PermRouteManage), createFilter(session))
	api.Put("/filters/:id", auth.RequirePermission(auth.PermRouteManage), updateFilter(session))
	api.Delete("/routes/:routeId/filters/:order", auth.RequirePermission(auth.PermRouteManage), deleteFilter(session))

	// --- Lookup Tables ---
	api.Get("/lookups", auth.RequirePermission(auth.PermRouteView), listLookupTables(session))
	api.Post("/lookups", auth.RequirePermission(auth.PermRouteManage), createLookupTable(session))
	api.Get("/lookups/:id/entries", auth.RequirePermission(auth.PermRouteView), listLookupEntries(session))
	api.Put("/lookups/:id/entries", auth.RequirePermission(auth.PermRouteManage), upsertLookupEntry(session))

	// --- Messages (PHI - restricted) ---
	api.Get("/messages", auth.RequireAnyPermission(auth.PermMessageView, auth.PermMessageViewSandbox), listMessages(session))
	api.Get("/messages/:id", auth.RequireAnyPermission(auth.PermMessageView, auth.PermMessageViewSandbox), getMessage(session))
	api.Get("/messages/status/:status", auth.RequireAnyPermission(auth.PermMessageView, auth.PermMessageViewSandbox), listMessagesByStatus(session))
	api.Get("/messages/patient/:patientId", auth.RequirePermission(auth.PermMessageView), listMessagesByPatient(session))

	// --- Errors / DLQ ---
	api.Get("/errors", auth.RequirePermission(auth.PermErrorView), listErrors(session))

	// --- Stats ---
	api.Get("/stats", auth.RequirePermission(auth.PermMetricsView), getStats(session))

	// --- Config Backup & Restore ---
	api.Get("/config/export", auth.RequirePermission(auth.PermConfigManage), configExport(session))
	api.Post("/config/import", auth.RequirePermission(auth.PermConfigManage), configImport(session))
	api.Get("/config/backups", auth.RequirePermission(auth.PermConfigView), listBackups(session))
	api.Post("/config/backups", auth.RequirePermission(auth.PermConfigManage), createBackup(session))
	api.Get("/config/history", auth.RequirePermission(auth.PermConfigView), configHistory(session))

	// --- Retention ---
	api.Get("/config/retention", auth.RequirePermission(auth.PermConfigView), getRetentionConfig(session))
	api.Put("/config/retention", auth.RequirePermission(auth.PermConfigManage), updateRetentionConfig(session))

	// --- Live Metrics ---
	api.Get("/metrics", auth.RequirePermission(auth.PermMetricsView), getLiveMetrics(nc))
	api.Get("/metrics/comm-points", auth.RequirePermission(auth.PermMetricsView), getCPMetrics(nc))
	api.Get("/metrics/comm-points/:id/logs", auth.RequirePermission(auth.PermMetricsView), getCPLogs(nc))

	// --- Tunnel Mesh ---
	api.Get("/tunnel/nodes", auth.RequirePermission(auth.PermTunnelView), listTunnelNodes(session))
	api.Post("/tunnel/nodes", auth.RequirePermission(auth.PermTunnelManage), createTunnelNode(session))
	api.Delete("/tunnel/nodes/:id", auth.RequirePermission(auth.PermTunnelManage), deleteTunnelNode(session))
	api.Post("/tunnel/nodes/:id/push-config", auth.RequirePermission(auth.PermTunnelManage), pushTunnelConfigHandler(session, nc))

	// --- JS Filter Playground ---
	api.Post("/playground/execute", auth.RequirePermission(auth.PermPlayground), executePlayground(nc))

	// --- User Management (security role) ---
	api.Get("/users", auth.RequirePermission(auth.PermUserView), listUsers(session))
	api.Post("/users", auth.RequirePermission(auth.PermUserManage), createUser(session))
	api.Put("/users/:id/role", auth.RequirePermission(auth.PermUserManage), updateUserRole(session))
	api.Delete("/users/:id", auth.RequirePermission(auth.PermUserManage), deleteUser(session))
	api.Get("/roles", auth.RequirePermission(auth.PermUserView), listRoles())

	// --- Connector Types (reference for CP creation) ---
	api.Get("/connector-types", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"connector_types": connectors.AllConnectorTypes()})
	})

	// --- Audit Log ---
	api.Get("/audit-log", auth.RequireAnyPermission(auth.PermConfigManage, auth.PermUserManage), func(c *fiber.Ctx) error {
		username := c.Query("username", "")
		limit := c.QueryInt("limit", 50)
		if username == "" {
			return c.JSON(fiber.Map{"audit_log": []fiber.Map{}, "hint": "specify ?username=admin"})
		}
		var items []fiber.Map
		iter := session.Query(`SELECT event_id, timestamp, action, resource, client_ip FROM arteria.audit_log WHERE username = ? LIMIT ?`, username, limit).Iter()
		var eID gocql.UUID
		var ts time.Time
		var action, resource, ip string
		for iter.Scan(&eID, &ts, &action, &resource, &ip) {
			items = append(items, fiber.Map{"event_id": eID.String(), "timestamp": ts, "action": action, "resource": resource, "client_ip": ip})
		}
		iter.Close()
		if items == nil { items = []fiber.Map{} }
		return c.JSON(fiber.Map{"audit_log": items, "count": len(items)})
	})

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("shutting down API")
		app.Shutdown()
	}()

	log.Info("API starting", logging.Fields{"port": apiPort})
	if err := app.Listen(":" + apiPort); err != nil {
		log.Fatal("server error", logging.Fields{"error": err.Error()})
	}
}

// ==================== Communication Points ====================

func listCommPoints(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var items []fiber.Map
		iter := session.Query(`SELECT comm_point_id, name, direction, protocol, host, port, is_active, max_retries, retry_delay_ms, timeout_ms, tunnel_enabled, tunnel_node_id, tunnel_local_port FROM arteria.communication_points`).Iter()
		var id, tunnelNodeID gocql.UUID
		var name, direction, protocol, host string
		var port, maxRetries, retryDelay, timeout, tunnelLocalPort int
		var isActive, tunnelEnabled bool
		for iter.Scan(&id, &name, &direction, &protocol, &host, &port, &isActive, &maxRetries, &retryDelay, &timeout, &tunnelEnabled, &tunnelNodeID, &tunnelLocalPort) {
			item := fiber.Map{
				"comm_point_id": id.String(), "name": name, "direction": direction,
				"protocol": protocol, "host": host, "port": port, "is_active": isActive,
				"max_retries": maxRetries, "retry_delay_ms": retryDelay, "timeout_ms": timeout,
				"tunnel_enabled": tunnelEnabled, "tunnel_local_port": tunnelLocalPort,
			}
			if tunnelNodeID.String() != "00000000-0000-0000-0000-000000000000" {
				item["tunnel_node_id"] = tunnelNodeID.String()
			}
			items = append(items, item)
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"communication_points": items, "count": len(items)})
	}
}

func createCommPoint(session *gocql.Session, nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var p struct {
			Name            string `json:"name"`
			Direction       string `json:"direction"`
			Protocol        string `json:"protocol"`
			Host            string `json:"host"`
			Port            int    `json:"port"`
			IsActive        bool   `json:"is_active"`
			MaxRetries      int    `json:"max_retries"`
			RetryDelay      int    `json:"retry_delay_ms"`
			Timeout         int    `json:"timeout_ms"`
			TunnelEnabled   bool   `json:"tunnel_enabled"`
			TunnelNodeID    string `json:"tunnel_node_id"`
			TunnelLocalPort int    `json:"tunnel_local_port"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		id := gocql.TimeUUID()
		now := time.Now()
		tunnelNodeID, _ := gocql.ParseUUID(p.TunnelNodeID)
		session.Query(`INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, max_retries, retry_delay_ms, timeout_ms, tunnel_enabled, tunnel_node_id, tunnel_local_port, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, p.Name, p.Direction, p.Protocol, p.Host, p.Port, p.IsActive, p.MaxRetries, p.RetryDelay, p.Timeout, p.TunnelEnabled, tunnelNodeID, p.TunnelLocalPort, now, now).Exec()

		// If tunnel is enabled, push config to the node
		if p.TunnelEnabled && p.TunnelNodeID != "" {
			pushTunnelConfig(nc, p.TunnelNodeID)
		}

		return c.Status(201).JSON(fiber.Map{"comm_point_id": id.String()})
	}
}

func updateCommPoint(session *gocql.Session, nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		var p struct {
			Name            string `json:"name"`
			Direction       string `json:"direction"`
			Protocol        string `json:"protocol"`
			Host            string `json:"host"`
			Port            int    `json:"port"`
			IsActive        bool   `json:"is_active"`
			MaxRetries      int    `json:"max_retries"`
			RetryDelay      int    `json:"retry_delay_ms"`
			Timeout         int    `json:"timeout_ms"`
			TunnelEnabled   bool   `json:"tunnel_enabled"`
			TunnelNodeID    string `json:"tunnel_node_id"`
			TunnelLocalPort int    `json:"tunnel_local_port"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		tunnelNodeID, _ := gocql.ParseUUID(p.TunnelNodeID)
		session.Query(`UPDATE arteria.communication_points SET name=?, direction=?, protocol=?, host=?, port=?, is_active=?, max_retries=?, retry_delay_ms=?, timeout_ms=?, tunnel_enabled=?, tunnel_node_id=?, tunnel_local_port=?, updated_at=? WHERE comm_point_id=?`,
			p.Name, p.Direction, p.Protocol, p.Host, p.Port, p.IsActive, p.MaxRetries, p.RetryDelay, p.Timeout, p.TunnelEnabled, tunnelNodeID, p.TunnelLocalPort, time.Now(), id).Exec()

		// Push config to the node whenever tunnel settings change
		if p.TunnelNodeID != "" {
			pushTunnelConfig(nc, p.TunnelNodeID)
		}

		return c.JSON(fiber.Map{"status": "updated"})
	}
}

func deleteCommPoint(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		session.Query(`DELETE FROM arteria.communication_points WHERE comm_point_id=?`, id).Exec()
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

// ==================== Routes ====================

func listRoutes(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var items []fiber.Map
		iter := session.Query(`SELECT route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active FROM arteria.routes`).Iter()
		var id, srcCP, dstCP gocql.UUID
		var name, desc, srcTopic, dstTopic string
		var isActive bool
		for iter.Scan(&id, &name, &desc, &srcCP, &dstCP, &srcTopic, &dstTopic, &isActive) {
			items = append(items, fiber.Map{
				"route_id": id.String(), "name": name, "description": desc,
				"source_comm_point_id": srcCP.String(), "dest_comm_point_id": dstCP.String(),
				"source_topic": srcTopic, "destination_topic": dstTopic, "is_active": isActive,
			})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"routes": items, "count": len(items)})
	}
}

func getRoute(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		var name, desc, srcTopic, dstTopic string
		var srcCP, dstCP gocql.UUID
		var isActive bool
		err = session.Query(`SELECT name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active FROM arteria.routes WHERE route_id=?`, id).
			Scan(&name, &desc, &srcCP, &dstCP, &srcTopic, &dstTopic, &isActive)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "route not found"})
		}
		return c.JSON(fiber.Map{
			"route_id": id.String(), "name": name, "description": desc,
			"source_comm_point_id": srcCP.String(), "dest_comm_point_id": dstCP.String(),
			"source_topic": srcTopic, "destination_topic": dstTopic, "is_active": isActive,
		})
	}
}

func createRoute(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var p struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			SourceCP     string `json:"source_comm_point_id"`
			DestCP       string `json:"dest_comm_point_id"`
			SourceTopic  string `json:"source_topic"`
			DestTopic    string `json:"destination_topic"`
			IsActive     bool   `json:"is_active"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		srcCP, _ := gocql.ParseUUID(p.SourceCP)
		dstCP, _ := gocql.ParseUUID(p.DestCP)
		id := gocql.TimeUUID()
		now := time.Now()
		session.Query(`INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			id, p.Name, p.Description, srcCP, dstCP, p.SourceTopic, p.DestTopic, p.IsActive, now, now).Exec()
		return c.Status(201).JSON(fiber.Map{"route_id": id.String()})
	}
}

func updateRoute(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		var p struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			SourceCP     string `json:"source_comm_point_id"`
			DestCP       string `json:"dest_comm_point_id"`
			SourceTopic  string `json:"source_topic"`
			DestTopic    string `json:"destination_topic"`
			IsActive     bool   `json:"is_active"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		srcCP, _ := gocql.ParseUUID(p.SourceCP)
		dstCP, _ := gocql.ParseUUID(p.DestCP)
		session.Query(`UPDATE arteria.routes SET name=?, description=?, source_comm_point_id=?, dest_comm_point_id=?, source_topic=?, destination_topic=?, is_active=?, updated_at=? WHERE route_id=?`,
			p.Name, p.Description, srcCP, dstCP, p.SourceTopic, p.DestTopic, p.IsActive, time.Now(), id).Exec()
		return c.JSON(fiber.Map{"status": "updated"})
	}
}

func deleteRoute(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		session.Query(`DELETE FROM arteria.routes WHERE route_id=?`, id).Exec()
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

// ==================== Filters ====================

func listFilters(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var items []fiber.Map
		iter := session.Query(`SELECT filter_id, name, filter_type, execution_order, js_script, config_json, is_active FROM arteria.filters WHERE route_id=?`, routeID).Iter()
		var fID gocql.UUID
		var name, fType, jsScript, configJSON string
		var order int
		var isActive bool
		for iter.Scan(&fID, &name, &fType, &order, &jsScript, &configJSON, &isActive) {
			items = append(items, fiber.Map{
				"filter_id": fID.String(), "name": name, "filter_type": fType,
				"execution_order": order, "js_script": jsScript, "config_json": configJSON,
				"is_active": isActive,
			})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"filters": items, "count": len(items)})
	}
}

func createFilter(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		var p struct {
			Name       string `json:"name"`
			FilterType string `json:"filter_type"`
			Order      int    `json:"execution_order"`
			JSScript   string `json:"js_script"`
			ConfigJSON string `json:"config_json"`
			IsActive   bool   `json:"is_active"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		fID := gocql.TimeUUID()
		now := time.Now()

		// Insert into both tables
		session.Query(`INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			fID, routeID, p.Name, p.FilterType, p.Order, p.JSScript, p.ConfigJSON, p.IsActive, now).Exec()
		session.Query(`INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			fID, routeID, p.Name, p.FilterType, p.Order, p.JSScript, p.ConfigJSON, p.IsActive, now).Exec()

		return c.Status(201).JSON(fiber.Map{"filter_id": fID.String()})
	}
}

func updateFilter(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		fID, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid filter ID"})
		}

		// Look up the filter to get route_id and execution_order
		var routeID gocql.UUID
		var order int
		err = session.Query(`SELECT route_id, execution_order FROM arteria.filters_by_id WHERE filter_id=?`, fID).Scan(&routeID, &order)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "filter not found"})
		}

		var p struct {
			Name       string `json:"name"`
			FilterType string `json:"filter_type"`
			JSScript   string `json:"js_script"`
			ConfigJSON string `json:"config_json"`
			IsActive   bool   `json:"is_active"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		session.Query(`UPDATE arteria.filters SET filter_id=?, name=?, filter_type=?, js_script=?, config_json=?, is_active=? WHERE route_id=? AND execution_order=?`,
			fID, p.Name, p.FilterType, p.JSScript, p.ConfigJSON, p.IsActive, routeID, order).Exec()
		session.Query(`UPDATE arteria.filters_by_id SET name=?, filter_type=?, js_script=?, config_json=?, is_active=? WHERE filter_id=?`,
			p.Name, p.FilterType, p.JSScript, p.ConfigJSON, p.IsActive, fID).Exec()

		return c.JSON(fiber.Map{"status": "updated"})
	}
}

func deleteFilter(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		routeID, err := gocql.ParseUUID(c.Params("routeId"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid route ID"})
		}
		order := c.QueryInt("order", -1)
		if order < 0 {
			return c.Status(400).JSON(fiber.Map{"error": "execution_order required"})
		}
		session.Query(`DELETE FROM arteria.filters WHERE route_id=? AND execution_order=?`, routeID, order).Exec()
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

// ==================== Lookup Tables ====================

func listLookupTables(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var items []fiber.Map
		iter := session.Query(`SELECT table_id, name, description FROM arteria.lookup_tables`).Iter()
		var id gocql.UUID
		var name, desc string
		for iter.Scan(&id, &name, &desc) {
			items = append(items, fiber.Map{"table_id": id.String(), "name": name, "description": desc})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"lookup_tables": items, "count": len(items)})
	}
}

func createLookupTable(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var p struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		id := gocql.TimeUUID()
		now := time.Now()
		session.Query(`INSERT INTO arteria.lookup_tables (table_id, name, description, created_at, updated_at) VALUES (?,?,?,?,?)`,
			id, p.Name, p.Description, now, now).Exec()
		return c.Status(201).JSON(fiber.Map{"table_id": id.String()})
	}
}

func listLookupEntries(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		var items []fiber.Map
		iter := session.Query(`SELECT lookup_key, lookup_value FROM arteria.lookup_entries WHERE table_id=?`, id).Iter()
		var key, val string
		for iter.Scan(&key, &val) {
			items = append(items, fiber.Map{"key": key, "value": val})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"entries": items, "count": len(items)})
	}
}

func upsertLookupEntry(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		var p struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		session.Query(`INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (?,?,?)`, id, p.Key, p.Value).Exec()
		return c.JSON(fiber.Map{"status": "upserted"})
	}
}

// ==================== Messages ====================

func listMessages(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		limit := c.QueryInt("limit", 50)
		if limit > 200 {
			limit = 200
		}
		var items []fiber.Map
		iter := session.Query(`SELECT message_id, patient_id, message_type, trigger_event, sending_facility, status, created_at FROM arteria.messages LIMIT ?`, limit).Iter()
		var id gocql.UUID
		var pid, mt, te, sf, st string
		var ca time.Time
		for iter.Scan(&id, &pid, &mt, &te, &sf, &st, &ca) {
			items = append(items, fiber.Map{
				"message_id": id.String(), "patient_id": pid, "message_type": mt,
				"trigger_event": te, "sending_facility": sf, "status": st, "created_at": ca,
			})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"messages": items, "count": len(items)})
	}
}

func getMessage(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		var pid, mt, te, sf, raw, transformed, props, st, errDet string
		var ca, ua time.Time
		var retries int
		err = session.Query(`SELECT patient_id, message_type, trigger_event, sending_facility, raw_payload, transformed_payload, properties, status, error_details, created_at, updated_at, retry_count FROM arteria.messages WHERE message_id=?`, id).
			Scan(&pid, &mt, &te, &sf, &raw, &transformed, &props, &st, &errDet, &ca, &ua, &retries)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		return c.JSON(fiber.Map{
			"message_id": id.String(), "patient_id": pid, "message_type": mt,
			"trigger_event": te, "sending_facility": sf, "raw_payload": raw,
			"transformed_payload": transformed, "properties": props, "status": st,
			"error_details": errDet, "created_at": ca, "updated_at": ua, "retry_count": retries,
		})
	}
}

func listMessagesByStatus(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		status := c.Params("status")
		limit := c.QueryInt("limit", 50)
		var items []fiber.Map
		iter := session.Query(`SELECT message_id, message_type, patient_id, created_at FROM arteria.messages_by_status WHERE status=? LIMIT ?`, status, limit).Iter()
		var id gocql.UUID
		var mt, pid string
		var ca time.Time
		for iter.Scan(&id, &mt, &pid, &ca) {
			items = append(items, fiber.Map{"message_id": id.String(), "message_type": mt, "patient_id": pid, "created_at": ca})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"messages": items, "count": len(items)})
	}
}

func listMessagesByPatient(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		pid := c.Params("patientId")
		limit := c.QueryInt("limit", 50)
		var items []fiber.Map
		iter := session.Query(`SELECT message_id, created_at FROM arteria.messages_by_patient WHERE patient_id=? LIMIT ?`, pid, limit).Iter()
		var id gocql.UUID
		var ca time.Time
		for iter.Scan(&id, &ca) {
			items = append(items, fiber.Map{"message_id": id.String(), "created_at": ca})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"messages": items, "count": len(items)})
	}
}

// ==================== Errors / DLQ ====================

func listErrors(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		limit := c.QueryInt("limit", 50)
		var items []fiber.Map
		iter := session.Query(`SELECT message_id, error_type, error_details, retry_count, max_retries, created_at FROM arteria.error_messages LIMIT ?`, limit).Iter()
		var id gocql.UUID
		var errType, errDet string
		var retries, maxRetries int
		var ca time.Time
		for iter.Scan(&id, &errType, &errDet, &retries, &maxRetries, &ca) {
			items = append(items, fiber.Map{
				"message_id": id.String(), "error_type": errType, "error_details": errDet,
				"retry_count": retries, "max_retries": maxRetries, "created_at": ca,
			})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"errors": items, "count": len(items)})
	}
}

// ==================== Stats ====================

func getStats(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var msgCount, routeCount, errCount int
		session.Query(`SELECT COUNT(*) FROM arteria.messages`).Scan(&msgCount)
		session.Query(`SELECT COUNT(*) FROM arteria.routes`).Scan(&routeCount)
		session.Query(`SELECT COUNT(*) FROM arteria.error_messages`).Scan(&errCount)
		return c.JSON(fiber.Map{
			"total_messages": msgCount,
			"total_routes":   routeCount,
			"total_errors":   errCount,
		})
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ==================== Config Export/Import ====================

func configExport(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		export := fiber.Map{}

		// Export comm points
		var cps []fiber.Map
		iter := session.Query(`SELECT comm_point_id, name, direction, protocol, host, port, is_active, max_retries, retry_delay_ms, timeout_ms, tunnel_enabled, tunnel_node_id, tunnel_local_port FROM arteria.communication_points`).Iter()
		var id, tnID gocql.UUID
		var name, dir, proto, host string
		var port, mr, rd, to, tlp int
		var active, te bool
		for iter.Scan(&id, &name, &dir, &proto, &host, &port, &active, &mr, &rd, &to, &te, &tnID, &tlp) {
			cps = append(cps, fiber.Map{"comm_point_id": id.String(), "name": name, "direction": dir, "protocol": proto, "host": host, "port": port, "is_active": active, "max_retries": mr, "retry_delay_ms": rd, "timeout_ms": to, "tunnel_enabled": te, "tunnel_node_id": tnID.String(), "tunnel_local_port": tlp})
		}
		iter.Close()
		export["communication_points"] = cps

		// Export routes
		var routes []fiber.Map
		rIter := session.Query(`SELECT route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, retention_days FROM arteria.routes`).Iter()
		var rID, srcCP, dstCP gocql.UUID
		var rName, desc, srcT, dstT string
		var rActive bool
		var retDays int
		for rIter.Scan(&rID, &rName, &desc, &srcCP, &dstCP, &srcT, &dstT, &rActive, &retDays) {
			routes = append(routes, fiber.Map{"route_id": rID.String(), "name": rName, "description": desc, "source_comm_point_id": srcCP.String(), "dest_comm_point_id": dstCP.String(), "source_topic": srcT, "destination_topic": dstT, "is_active": rActive, "retention_days": retDays})
		}
		rIter.Close()
		export["routes"] = routes

		// Export filters
		var filters []fiber.Map
		fIter := session.Query(`SELECT filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active FROM arteria.filters_by_id`).Iter()
		var fID, fRouteID gocql.UUID
		var fName, fType, fJS, fCfg string
		var fOrder int
		var fActive bool
		for fIter.Scan(&fID, &fRouteID, &fName, &fType, &fOrder, &fJS, &fCfg, &fActive) {
			filters = append(filters, fiber.Map{"filter_id": fID.String(), "route_id": fRouteID.String(), "name": fName, "filter_type": fType, "execution_order": fOrder, "js_script": fJS, "config_json": fCfg, "is_active": fActive})
		}
		fIter.Close()
		export["filters"] = filters

		// Export lookup tables + entries
		var lookups []fiber.Map
		lIter := session.Query(`SELECT table_id, name, description FROM arteria.lookup_tables`).Iter()
		var lID gocql.UUID
		var lName, lDesc string
		for lIter.Scan(&lID, &lName, &lDesc) {
			// Get entries for this table
			var entries []fiber.Map
			eIter := session.Query(`SELECT lookup_key, lookup_value FROM arteria.lookup_entries WHERE table_id = ?`, lID).Iter()
			var k, v string
			for eIter.Scan(&k, &v) {
				entries = append(entries, fiber.Map{"key": k, "value": v})
			}
			eIter.Close()
			lookups = append(lookups, fiber.Map{"table_id": lID.String(), "name": lName, "description": lDesc, "entries": entries})
		}
		lIter.Close()
		export["lookup_tables"] = lookups

		// Export tunnel nodes
		var nodes []fiber.Map
		nIter := session.Query(`SELECT node_id, name, site_name, status FROM arteria.tunnel_nodes`).Iter()
		var nID gocql.UUID
		var nName, nSite, nStatus string
		for nIter.Scan(&nID, &nName, &nSite, &nStatus) {
			nodes = append(nodes, fiber.Map{"node_id": nID.String(), "name": nName, "site_name": nSite, "status": nStatus})
		}
		nIter.Close()
		export["tunnel_nodes"] = nodes

		export["exported_at"] = time.Now().UTC().Format(time.RFC3339)
		export["version"] = "1.0"

		return c.JSON(export)
	}
}

func configImport(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var body map[string]interface{}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid JSON body"})
		}

		imported := 0

		// Import comm points
		if cps, ok := body["communication_points"].([]interface{}); ok {
			for _, cp := range cps {
				m := cp.(map[string]interface{})
				cpID, _ := gocql.ParseUUID(m["comm_point_id"].(string))
				tnID, _ := gocql.ParseUUID(strVal(m, "tunnel_node_id"))
				session.Query(`INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, max_retries, retry_delay_ms, timeout_ms, tunnel_enabled, tunnel_node_id, tunnel_local_port, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
					cpID, m["name"], m["direction"], m["protocol"], m["host"], intVal(m, "port"), boolVal(m, "is_active"), intVal(m, "max_retries"), intVal(m, "retry_delay_ms"), intVal(m, "timeout_ms"), boolVal(m, "tunnel_enabled"), tnID, intVal(m, "tunnel_local_port"), time.Now(), time.Now()).Exec()
				imported++
			}
		}

		// Import routes
		if routes, ok := body["routes"].([]interface{}); ok {
			for _, r := range routes {
				m := r.(map[string]interface{})
				rID, _ := gocql.ParseUUID(m["route_id"].(string))
				srcCP, _ := gocql.ParseUUID(strVal(m, "source_comm_point_id"))
				dstCP, _ := gocql.ParseUUID(strVal(m, "dest_comm_point_id"))
				session.Query(`INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, retention_days, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
					rID, m["name"], m["description"], srcCP, dstCP, m["source_topic"], m["destination_topic"], boolVal(m, "is_active"), intVal(m, "retention_days"), time.Now(), time.Now()).Exec()
				imported++
			}
		}

		// Import filters
		if filters, ok := body["filters"].([]interface{}); ok {
			for _, f := range filters {
				m := f.(map[string]interface{})
				fID, _ := gocql.ParseUUID(m["filter_id"].(string))
				rID, _ := gocql.ParseUUID(m["route_id"].(string))
				order := intVal(m, "execution_order")
				session.Query(`INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
					fID, rID, m["name"], m["filter_type"], order, m["js_script"], m["config_json"], boolVal(m, "is_active"), time.Now()).Exec()
				session.Query(`INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
					fID, rID, m["name"], m["filter_type"], order, m["js_script"], m["config_json"], boolVal(m, "is_active"), time.Now()).Exec()
				imported++
			}
		}

		return c.JSON(fiber.Map{"status": "imported", "items": imported})
	}
}

func listBackups(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var items []fiber.Map
		iter := session.Query(`SELECT backup_id, name, description, created_by, created_at FROM arteria.config_backups`).Iter()
		var bID gocql.UUID
		var bName, bDesc, bBy string
		var bAt time.Time
		for iter.Scan(&bID, &bName, &bDesc, &bBy, &bAt) {
			items = append(items, fiber.Map{"backup_id": bID.String(), "name": bName, "description": bDesc, "created_by": bBy, "created_at": bAt})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"backups": items, "count": len(items)})
	}
}

func createBackup(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*auth.Claims)

		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := c.BodyParser(&body); err != nil {
			body.Name = "Manual backup"
		}
		if body.Name == "" {
			body.Name = "Backup " + time.Now().Format("2006-01-02 15:04")
		}

		// Generate full export
		exportData := exportAllConfig(session)
		exportJSON, _ := json.Marshal(exportData)

		bID := gocql.TimeUUID()
		session.Query(`INSERT INTO arteria.config_backups (backup_id, name, description, backup_json, created_by, created_at) VALUES (?,?,?,?,?,?)`,
			bID, body.Name, body.Description, string(exportJSON), claims.Username, time.Now()).Exec()

		return c.Status(201).JSON(fiber.Map{"backup_id": bID.String(), "name": body.Name, "size_bytes": len(exportJSON)})
	}
}

func configHistory(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		entityType := c.Query("type", "")
		limit := c.QueryInt("limit", 50)

		var items []fiber.Map
		var iter *gocql.Iter
		if entityType != "" {
			iter = session.Query(`SELECT change_id, entity_type, entity_id, action, changed_by, created_at FROM arteria.config_history WHERE entity_type = ? LIMIT ?`, entityType, limit).Iter()
		} else {
			// Can't do cross-partition without ALLOW FILTERING, return empty with hint
			return c.JSON(fiber.Map{"history": []fiber.Map{}, "hint": "specify ?type=route|filter|comm_point|tunnel_node|lookup"})
		}

		var cID, eID gocql.UUID
		var eType, action, changedBy string
		var createdAt time.Time
		for iter.Scan(&cID, &eType, &eID, &action, &changedBy, &createdAt) {
			items = append(items, fiber.Map{"change_id": cID.String(), "entity_type": eType, "entity_id": eID.String(), "action": action, "changed_by": changedBy, "created_at": createdAt})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"history": items, "count": len(items)})
	}
}

func getRetentionConfig(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"global": fiber.Map{
				"messages_ttl_days":       30,
				"error_messages_ttl_days": 90,
				"config_history_ttl_days": 365,
			},
			"info": "Global TTL is set at the ScyllaDB table level. Per-route retention_days overrides at insert time.",
		})
	}
}

func updateRetentionConfig(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var body struct {
			MessagesTTLDays int `json:"messages_ttl_days"`
			ErrorsTTLDays   int `json:"error_messages_ttl_days"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		if body.MessagesTTLDays > 0 {
			ttlSec := body.MessagesTTLDays * 86400
			session.Query(fmt.Sprintf("ALTER TABLE arteria.messages WITH default_time_to_live = %d", ttlSec)).Exec()
			session.Query(fmt.Sprintf("ALTER TABLE arteria.messages_by_patient WITH default_time_to_live = %d", ttlSec)).Exec()
			session.Query(fmt.Sprintf("ALTER TABLE arteria.messages_by_status WITH default_time_to_live = %d", ttlSec)).Exec()
		}
		if body.ErrorsTTLDays > 0 {
			ttlSec := body.ErrorsTTLDays * 86400
			session.Query(fmt.Sprintf("ALTER TABLE arteria.error_messages WITH default_time_to_live = %d", ttlSec)).Exec()
		}

		return c.JSON(fiber.Map{"status": "updated", "messages_ttl_days": body.MessagesTTLDays, "error_messages_ttl_days": body.ErrorsTTLDays})
	}
}

// Helper to export all config (used by backup and export endpoints)
func exportAllConfig(session *gocql.Session) map[string]interface{} {
	export := map[string]interface{}{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"version":     "1.0",
	}

	// Simplified — reuses the same logic as configExport
	var cps []map[string]interface{}
	iter := session.Query(`SELECT comm_point_id, name, direction, protocol, host, port, is_active FROM arteria.communication_points`).Iter()
	var id gocql.UUID
	var name, dir, proto, host string
	var port int
	var active bool
	for iter.Scan(&id, &name, &dir, &proto, &host, &port, &active) {
		cps = append(cps, map[string]interface{}{"comm_point_id": id.String(), "name": name, "direction": dir, "protocol": proto, "host": host, "port": port, "is_active": active})
	}
	iter.Close()
	export["communication_points"] = cps

	var routes []map[string]interface{}
	rIter := session.Query(`SELECT route_id, name, description, source_topic, destination_topic, is_active FROM arteria.routes`).Iter()
	var rID gocql.UUID
	var rName, desc, srcT, dstT string
	var rActive bool
	for rIter.Scan(&rID, &rName, &desc, &srcT, &dstT, &rActive) {
		routes = append(routes, map[string]interface{}{"route_id": rID.String(), "name": rName, "description": desc, "source_topic": srcT, "destination_topic": dstT, "is_active": rActive})
	}
	rIter.Close()
	export["routes"] = routes

	return export
}

// Type conversion helpers for import
func strVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func intVal(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok && v != nil {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		}
	}
	return 0
}

func boolVal(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok && v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// ==================== Authentication ====================

func authMiddleware(jwtSecret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Allow module config without auth (needed for sidebar before login)
		if c.Path() == "/api/v1/config/modules" {
			return c.Next()
		}

		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(401).JSON(fiber.Map{"error": "authorization header required"})
		}

		// Extract "Bearer <token>"
		tokenStr := ""
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			tokenStr = authHeader[7:]
		}
		if tokenStr == "" {
			return c.Status(401).JSON(fiber.Map{"error": "invalid authorization format"})
		}

		claims, err := auth.ValidateToken(tokenStr, jwtSecret)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "invalid or expired token"})
		}

		c.Locals("claims", claims)
		return c.Next()
	}
}

func loginHandler(session *gocql.Session, cfg auth.Config, limiter *auth.RateLimiter, auditLog *security.AuditLogger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		if body.Username == "" || body.Password == "" {
			return c.Status(400).JSON(fiber.Map{"error": "username and password required"})
		}

		// Rate limit check
		limitKey := c.IP() + ":" + body.Username
		if !limiter.Allow(limitKey) {
			auditLog.Log(body.Username, "LOGIN_BLOCKED", "/auth/login", "", c.IP(), c.Get("User-Agent"), map[string]string{"reason": "rate_limited"})
			return c.Status(429).JSON(fiber.Map{"error": "too many login attempts, try again later"})
		}

		// Look up user
		var userID gocql.UUID
		var passwordHash, role string
		var isActive bool
		err := session.Query(`SELECT user_id, password_hash, role, is_active FROM arteria.users WHERE username = ? ALLOW FILTERING`, body.Username).
			Scan(&userID, &passwordHash, &role, &isActive)
		if err != nil {
			limiter.Record(limitKey)
			auditLog.Log(body.Username, "LOGIN_FAILED", "/auth/login", "", c.IP(), c.Get("User-Agent"), nil)
			return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
		}

		if !isActive {
			auditLog.Log(body.Username, "LOGIN_DISABLED", "/auth/login", "", c.IP(), c.Get("User-Agent"), nil)
			return c.Status(401).JSON(fiber.Map{"error": "account disabled"})
		}

		if !auth.CheckPassword(body.Password, passwordHash) {
			limiter.Record(limitKey)
			auditLog.Log(body.Username, "LOGIN_FAILED", "/auth/login", "", c.IP(), c.Get("User-Agent"), nil)
			return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
		}

		token, err := auth.GenerateToken(cfg.JWTSecret, userID.String(), body.Username, role, cfg.TokenExpiry)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "token generation failed"})
		}

		limiter.Reset(limitKey)
		auditLog.Log(body.Username, "LOGIN_SUCCESS", "/auth/login", userID.String(), c.IP(), c.Get("User-Agent"), nil)

		return c.JSON(fiber.Map{
			"token":    token,
			"username": body.Username,
			"role":     role,
			"expires_in": int(cfg.TokenExpiry.Seconds()),
		})
	}
}

func changePasswordHandler(session *gocql.Session, cfg auth.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*auth.Claims)

		var body struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		if len(body.NewPassword) < 8 {
			return c.Status(400).JSON(fiber.Map{"error": "password must be at least 8 characters"})
		}

		// Verify current password
		userID, _ := gocql.ParseUUID(claims.UserID)
		var currentHash string
		session.Query(`SELECT password_hash FROM arteria.users WHERE user_id = ?`, userID).Scan(&currentHash)

		if !auth.CheckPassword(body.CurrentPassword, currentHash) {
			return c.Status(401).JSON(fiber.Map{"error": "current password is incorrect"})
		}

		newHash, _ := auth.HashPassword(body.NewPassword)
		session.Query(`UPDATE arteria.users SET password_hash = ? WHERE user_id = ?`, newHash, userID).Exec()

		return c.JSON(fiber.Map{"status": "password changed"})
	}
}

// ==================== Live Metrics ====================

func getLiveMetrics(nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		timeout := 2 * time.Second
		result := fiber.Map{}

		// Query ingestion metrics
		if resp, err := nc.Request("arteria.metrics.ingestion", nil, timeout); err == nil {
			var snap metrics.Snapshot
			if json.Unmarshal(resp.Data, &snap) == nil {
				result["ingestion"] = snap
			}
		}

		// Query processing metrics
		if resp, err := nc.Request("arteria.metrics.processing", nil, timeout); err == nil {
			var snap metrics.Snapshot
			if json.Unmarshal(resp.Data, &snap) == nil {
				result["processing"] = snap
			}
		}

		return c.JSON(result)
	}
}

func getCPMetrics(nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		timeout := 2 * time.Second
		allCPs := make(map[string]metrics.CPSnapshot)

		// Gather CP metrics from ingestion
		if resp, err := nc.Request("arteria.metrics.ingestion", nil, timeout); err == nil {
			var snap metrics.Snapshot
			if json.Unmarshal(resp.Data, &snap) == nil {
				for id, cp := range snap.CommPoints {
					allCPs[id] = cp
				}
			}
		}

		// Gather CP metrics from processing
		if resp, err := nc.Request("arteria.metrics.processing", nil, timeout); err == nil {
			var snap metrics.Snapshot
			if json.Unmarshal(resp.Data, &snap) == nil {
				for id, cp := range snap.CommPoints {
					allCPs[id] = cp
				}
			}
		}

		return c.JSON(fiber.Map{"comm_points": allCPs, "count": len(allCPs)})
	}
}

func getCPLogs(nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		cpID := c.Params("id")
		timeout := 2 * time.Second

		// Check ingestion service first
		if resp, err := nc.Request("arteria.metrics.ingestion", nil, timeout); err == nil {
			var snap metrics.Snapshot
			if json.Unmarshal(resp.Data, &snap) == nil {
				if cp, ok := snap.CommPoints[cpID]; ok {
					return c.JSON(fiber.Map{
						"comm_point_id": cpID,
						"name":          cp.Name,
						"direction":     cp.Direction,
						"received":      cp.Received,
						"errors":        cp.Errors,
						"logs":          cp.Logs,
						"log_count":     len(cp.Logs),
					})
				}
			}
		}

		// Check processing service
		if resp, err := nc.Request("arteria.metrics.processing", nil, timeout); err == nil {
			var snap metrics.Snapshot
			if json.Unmarshal(resp.Data, &snap) == nil {
				if cp, ok := snap.CommPoints[cpID]; ok {
					return c.JSON(fiber.Map{
						"comm_point_id": cpID,
						"name":          cp.Name,
						"direction":     cp.Direction,
						"received":      cp.Received,
						"errors":        cp.Errors,
						"logs":          cp.Logs,
						"log_count":     len(cp.Logs),
					})
				}
			}
		}

		return c.Status(404).JSON(fiber.Map{"error": "communication point not found or not active"})
	}
}

// ==================== Tunnel Mesh ====================

func listTunnelNodes(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var items []fiber.Map
		iter := session.Query(`SELECT node_id, name, site_name, enrollment_token, status, agent_version, last_seen FROM arteria.tunnel_nodes`).Iter()
		var id gocql.UUID
		var name, site, token, status, agentVer string
		var lastSeen time.Time
		for iter.Scan(&id, &name, &site, &token, &status, &agentVer, &lastSeen) {
			items = append(items, fiber.Map{
				"node_id": id.String(), "name": name, "site_name": site,
				"enrollment_token": token, "status": status,
				"agent_version": agentVer, "last_seen": lastSeen,
			})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"nodes": items, "count": len(items)})
	}
}

func createTunnelNode(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var p struct {
			Name     string `json:"name"`
			SiteName string `json:"site_name"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		nodeID := gocql.TimeUUID()
		token := uuid.New().String()
		now := time.Now()
		if err := session.Query(`INSERT INTO arteria.tunnel_nodes (node_id, name, site_name, enrollment_token, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			nodeID, p.Name, p.SiteName, token, "PENDING", now, now).Exec(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{
			"node_id":          nodeID.String(),
			"enrollment_token": token,
			"status":           "PENDING",
			"instructions":     "Run: arteria-agent enroll " + token,
		})
	}
}

func deleteTunnelNode(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		session.Query(`DELETE FROM arteria.tunnel_nodes WHERE node_id=?`, id).Exec()
		session.Query(`DELETE FROM arteria.tunnel_mappings WHERE node_id=?`, id).Exec()
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

// pushTunnelConfig notifies the broker to rebuild and push config to a tunnel node.
// The broker reads all CPs linked to this node and pushes the mappings.
func pushTunnelConfig(nc *nats.Conn, nodeID string) {
	nc.Publish("arteria.tunnel.config-push", []byte(nodeID))
}

func pushTunnelConfigHandler(session *gocql.Session, nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		nodeID := c.Params("id")
		pushTunnelConfig(nc, nodeID)
		return c.JSON(fiber.Map{"status": "config push triggered", "node_id": nodeID})
	}
}

// ==================== JS Filter Playground ====================

func executePlayground(nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var p struct {
			Script     string `json:"script"`
			FilterType string `json:"filter_type"`
			Payload    string `json:"payload"` // JSON string of a MessageEnvelope
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		if p.Script == "" || p.Payload == "" {
			return c.Status(400).JSON(fiber.Map{"error": "script and payload are required"})
		}

		// Send to processing service for V8 execution via NATS request
		req := map[string]string{
			"script":      p.Script,
			"filter_type": p.FilterType,
			"payload":     p.Payload,
		}
		data, _ := json.Marshal(req)

		resp, err := nc.Request("arteria.playground.execute", data, 5*time.Second)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "execution timeout or processing service unavailable"})
		}

		var result map[string]interface{}
		json.Unmarshal(resp.Data, &result)
		return c.JSON(result)
	}
}

// ==================== User Management ====================

func listUsers(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var items []fiber.Map
		iter := session.Query(`SELECT user_id, username, role, is_active, created_at FROM arteria.users`).Iter()
		var id gocql.UUID
		var username, role string
		var isActive bool
		var createdAt time.Time
		for iter.Scan(&id, &username, &role, &isActive, &createdAt) {
			items = append(items, fiber.Map{
				"user_id": id.String(), "username": username, "role": role,
				"is_active": isActive, "created_at": createdAt,
			})
		}
		iter.Close()
		if items == nil {
			items = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"users": items, "count": len(items)})
	}
}

func createUser(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.Username == "" || body.Password == "" || body.Role == "" {
			return c.Status(400).JSON(fiber.Map{"error": "username, password, and role required"})
		}
		if len(body.Password) < 8 {
			return c.Status(400).JSON(fiber.Map{"error": "password must be at least 8 characters"})
		}
		if !auth.HasPermission(body.Role, auth.PermMetricsView) && body.Role != "security" {
			// Validate role exists
			valid := false
			for _, r := range auth.AllRoles() {
				if r == body.Role {
					valid = true
					break
				}
			}
			if !valid {
				return c.Status(400).JSON(fiber.Map{"error": "invalid role", "valid_roles": auth.AllRoles()})
			}
		}

		hash, _ := auth.HashPassword(body.Password)
		userID := gocql.TimeUUID()
		if err := session.Query(`INSERT INTO arteria.users (user_id, username, password_hash, role, is_active, created_at) VALUES (?,?,?,?,?,?)`,
			userID, body.Username, hash, body.Role, true, time.Now()).Exec(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		return c.Status(201).JSON(fiber.Map{"user_id": userID.String(), "username": body.Username, "role": body.Role})
	}
}

func updateUserRole(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid user ID"})
		}
		var body struct {
			Role     string `json:"role"`
			IsActive *bool  `json:"is_active"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.Role != "" {
			session.Query(`UPDATE arteria.users SET role = ? WHERE user_id = ?`, body.Role, id).Exec()
		}
		if body.IsActive != nil {
			session.Query(`UPDATE arteria.users SET is_active = ? WHERE user_id = ?`, *body.IsActive, id).Exec()
		}
		return c.JSON(fiber.Map{"status": "updated"})
	}
}

func deleteUser(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := gocql.ParseUUID(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid user ID"})
		}
		session.Query(`DELETE FROM arteria.users WHERE user_id = ?`, id).Exec()
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}

func listRoles() fiber.Handler {
	return func(c *fiber.Ctx) error {
		roles := make([]fiber.Map, 0)
		for _, role := range auth.AllRoles() {
			perms := auth.GetPermissions(role)
			permStrings := make([]string, len(perms))
			for i, p := range perms {
				permStrings[i] = string(p)
			}
			roles = append(roles, fiber.Map{
				"role":        role,
				"permissions": permStrings,
			})
		}
		return c.JSON(fiber.Map{"roles": roles})
	}
}
