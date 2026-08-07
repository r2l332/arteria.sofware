package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sort"
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

var sessionTracker *auth.SessionTracker
var natsConn *nats.Conn // Package-level for config change events

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
	natsConn = nc

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
		DefaultPass: envOrDefault("ADMIN_PASS", "arteria123"),
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

	// Session tracker (5-minute idle timeout = considered offline)
	sessionTracker = auth.NewSessionTracker(5 * time.Minute)

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// --- Public Auth Endpoints (no middleware) ---
	app.Post("/api/v1/auth/login", loginHandler(session, authCfg, loginLimiter, auditLog))

	// --- Public Branding (no auth — loaded before login for whitelabel) ---
	app.Get("/api/v1/branding", func(c *fiber.Ctx) error {
		// Resolve tenant from Host header or query param
		host := c.Hostname()
		slug := c.Query("slug", "")

		var orgID gocql.UUID
		var name, brandSlug, logo, appName, primaryColor, accentColor, favicon, supportEmail string
		var found bool

		if slug != "" {
			err := session.Query(
				`SELECT org_id, name, slug, branding_logo_url, branding_app_name, branding_primary_color, branding_accent_color, branding_favicon_url, support_email FROM arteria.organisations WHERE slug = ? ALLOW FILTERING`, slug,
			).Scan(&orgID, &name, &brandSlug, &logo, &appName, &primaryColor, &accentColor, &favicon, &supportEmail)
			found = err == nil
		}

		if !found && host != "" {
			err := session.Query(
				`SELECT org_id, name, slug, branding_logo_url, branding_app_name, branding_primary_color, branding_accent_color, branding_favicon_url, support_email FROM arteria.organisations WHERE custom_domain = ? ALLOW FILTERING`, host,
			).Scan(&orgID, &name, &brandSlug, &logo, &appName, &primaryColor, &accentColor, &favicon, &supportEmail)
			found = err == nil
		}

		if !found {
			// Default branding
			return c.JSON(fiber.Map{
				"org_id":    nil,
				"app_name":  "Arteria",
				"subtitle":  "Integration Engine",
				"logo_url":  "",
				"primary_color": "#6366f1",
				"accent_color":  "#6366f1",
				"favicon_url":   "",
				"support_email": "",
			})
		}

		return c.JSON(fiber.Map{
			"org_id":        orgID.String(),
			"app_name":      orDefault(appName, "Arteria"),
			"subtitle":      "Integration Engine",
			"logo_url":      logo,
			"primary_color": orDefault(primaryColor, "#6366f1"),
			"accent_color":  orDefault(accentColor, "#6366f1"),
			"favicon_url":   favicon,
			"support_email": supportEmail,
			"slug":          brandSlug,
			"org_name":      name,
		})
	})

	// --- Auth Middleware for all /api/v1/* routes ---
	api := app.Group("/api/v1", authMiddleware(authCfg.JWTSecret, sessionTracker))

	// --- Auth Management ---
	api.Get("/auth/me", func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*auth.Claims)
		return c.JSON(fiber.Map{"user_id": claims.UserID, "username": claims.Username, "role": claims.Role})
	})
	api.Post("/auth/change-password", changePasswordHandler(session, authCfg))
	api.Get("/auth/password-policy", func(c *fiber.Ctx) error {
		return c.JSON(auth.DefaultPasswordPolicy())
	})

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
	enabledPlugins := envOrDefault("ENABLE_PLUGINS", "routes-js")
	api.Get("/config/modules", func(c *fiber.Ctx) error {
		modules := fiber.Map{
			"hl7":       envOrDefault("MODULE_HL7", "true") == "true",
			"fhir":      envOrDefault("MODULE_FHIR", "false") == "true",
			"dicom":     envOrDefault("MODULE_DICOM", "false") == "true",
			"tunnel":    envOrDefault("MODULE_TUNNEL", "true") == "true",
			"routes-js": strings.Contains(enabledPlugins, "routes-js"),
		}
		return c.JSON(fiber.Map{"modules": modules})
	})

	// --- Communication Points ---
	api.Get("/comm-points", auth.RequirePermission(auth.PermCPView), listCommPoints(session))
	api.Post("/comm-points", auth.RequirePermission(auth.PermCPManage), createCommPoint(session, nc))
	api.Put("/comm-points/:id", auth.RequirePermission(auth.PermCPManage), updateCommPoint(session, nc))
	api.Delete("/comm-points/:id", auth.RequirePermission(auth.PermCPManage), deleteCommPoint(session))

	// --- Routes, Filters, Lookups, Playground (plugin: routes-js) ---
	if strings.Contains(enabledPlugins, "routes-js") {
		api.Get("/routes", auth.RequirePermission(auth.PermRouteView), listRoutes(session))
		api.Get("/routes/:id", auth.RequirePermission(auth.PermRouteView), getRoute(session))
		api.Post("/routes", auth.RequirePermission(auth.PermRouteManage), createRoute(session))
		api.Put("/routes/:id", auth.RequirePermission(auth.PermRouteManage), updateRoute(session))
		api.Delete("/routes/:id", auth.RequirePermission(auth.PermRouteManage), deleteRoute(session))

		api.Get("/routes/:id/filters", auth.RequirePermission(auth.PermRouteView), listFilters(session))
		api.Post("/routes/:id/filters", auth.RequirePermission(auth.PermRouteManage), createFilter(session))
		api.Put("/filters/:id", auth.RequirePermission(auth.PermRouteManage), updateFilter(session))
		api.Delete("/routes/:routeId/filters/:order", auth.RequirePermission(auth.PermRouteManage), deleteFilter(session))

		api.Get("/lookups", auth.RequirePermission(auth.PermRouteView), listLookupTables(session))
		api.Post("/lookups", auth.RequirePermission(auth.PermRouteManage), createLookupTable(session))
		api.Get("/lookups/:id/entries", auth.RequirePermission(auth.PermRouteView), listLookupEntries(session))
		api.Put("/lookups/:id/entries", auth.RequirePermission(auth.PermRouteManage), upsertLookupEntry(session))

		api.Post("/playground/execute", auth.RequirePermission(auth.PermPlayground), executePlayground(nc))
		log.Info("plugin routes registered", logging.Fields{"plugin": "routes-js"})
	}

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

	// --- User Management (security role) ---
	api.Get("/users", auth.RequirePermission(auth.PermUserView), listUsers(session))
	api.Post("/users", auth.RequirePermission(auth.PermUserManage), createUser(session))
	api.Put("/users/:id/role", auth.RequirePermission(auth.PermUserManage), updateUserRole(session))
	api.Delete("/users/:id", auth.RequirePermission(auth.PermUserManage), deleteUser(session))
	api.Get("/roles", auth.RequirePermission(auth.PermUserView), listRoles())
	api.Get("/users/online", func(c *fiber.Ctx) error {
		online := sessionTracker.Online()
		return c.JSON(fiber.Map{"online_users": online, "count": len(online)})
	})

	// --- In-App Messaging ---
	api.Get("/messages/internal/inbox", getInbox(session))
	api.Get("/messages/internal/sent", getSentMessages(session))
	api.Post("/messages/internal", sendMessage(session))
	api.Put("/messages/internal/:id/read", markMessageRead(session))
	api.Get("/messages/internal/unread-count", getUnreadCount(session))

	// --- Organisations (Multi-Tenant) ---
	api.Get("/organisations", auth.RequirePermission(auth.PermOrgView), listOrganisations(session))
	api.Post("/organisations", auth.RequirePermission(auth.PermOrgManage), createOrganisation(session))
	api.Get("/organisations/:id", auth.RequirePermission(auth.PermOrgView), getOrganisation(session))
	api.Put("/organisations/:id/branding", auth.RequirePermission(auth.PermOrgManage), updateOrganisationBranding(session))

	// --- Platform Health & Monitoring ---
	api.Get("/platform/health", auth.RequirePermission(auth.PermPlatformManage), platformHealthCheck(session, nc))
	api.Get("/platform/tunnel-stats", auth.RequirePermission(auth.PermPlatformManage), getTunnelStats(nc))
	api.Get("/platform/usage", auth.RequirePermission(auth.PermPlatformManage), getPlatformUsage(session))
	api.Get("/platform/logs/:service", auth.RequirePermission(auth.PermPlatformManage), getServiceLogs())
	api.Get("/platform/nats-stats", auth.RequirePermission(auth.PermPlatformManage), getNATSStats(nc))
	api.Get("/platform/connections", auth.RequirePermission(auth.PermPlatformManage), getConnectionHistory(session))

	// --- Connector Types (reference for CP creation) ---
	api.Get("/connector-types", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"connector_types": connectors.AllConnectorTypes()})
	})

	// --- Audit Log ---
	api.Get("/audit-log", auth.RequireAnyPermission(auth.PermConfigManage, auth.PermUserManage), func(c *fiber.Ctx) error {
		username := c.Query("username", "")
		limit := c.QueryInt("limit", 50)
		var items []fiber.Map
		if username != "" {
			iter := session.Query(`SELECT event_id, timestamp, action, resource, client_ip FROM arteria.audit_log WHERE username = ? LIMIT ?`, username, limit).Iter()
			var eID gocql.UUID
			var ts time.Time
			var action, resource, ip string
			for iter.Scan(&eID, &ts, &action, &resource, &ip) {
				items = append(items, fiber.Map{"event_id": eID.String(), "timestamp": ts, "username": username, "action": action, "resource": resource, "client_ip": ip})
			}
			iter.Close()
		} else {
			// Scan all users' audit entries (limited)
			userIter := session.Query(`SELECT DISTINCT username FROM arteria.audit_log`).Iter()
			var uname string
			var allEntries []fiber.Map
			for userIter.Scan(&uname) {
				iter := session.Query(`SELECT event_id, timestamp, action, resource, client_ip FROM arteria.audit_log WHERE username = ? LIMIT ?`, uname, 20).Iter()
				var eID gocql.UUID
				var ts time.Time
				var action, resource, ip string
				for iter.Scan(&eID, &ts, &action, &resource, &ip) {
					allEntries = append(allEntries, fiber.Map{"event_id": eID.String(), "timestamp": ts, "username": uname, "action": action, "resource": resource, "client_ip": ip})
				}
				iter.Close()
			}
			userIter.Close()
			// Sort by timestamp desc and take limit
			sort.Slice(allEntries, func(i, j int) bool {
				ti, _ := allEntries[i]["timestamp"].(time.Time)
				tj, _ := allEntries[j]["timestamp"].(time.Time)
				return ti.After(tj)
			})
			if len(allEntries) > limit { allEntries = allEntries[:limit] }
			items = allEntries
		}
		if items == nil { items = []fiber.Map{} }
		return c.JSON(fiber.Map{"entries": items, "count": len(items)})
	})

	// --- Patient Journey ---
	api.Get("/patients/:id/journey", auth.RequirePermission(auth.PermMessageView), patientJourney(session))

	// --- AI Filter Generator ---
	api.Post("/ai/generate-filter", auth.RequirePermission(auth.PermRouteManage), generateFilter())

	// --- Compliance Timeline ---
	api.Get("/compliance/timeline", auth.RequirePermission(auth.PermMetricsView), complianceTimeline(session))

	// --- WebSocket streaming + message control ---
	js, _ := nc.JetStream()
	wsHub := newWSHub()
	go wsHub.run()
	startNATSBridge(nc, wsHub)
	registerStreamingRoutes(app, nc, js, session, wsHub)
	registerRewireRoutes(app, session)
	registerPlatformAdminRoutes(app, nc, js, session)

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
		// Pre-load tunnel node names for enrichment
		nodeNames := make(map[string]fiber.Map)
		nIter := session.Query(`SELECT node_id, name, site_name, status FROM arteria.tunnel_nodes`).Iter()
		var nID gocql.UUID
		var nName, nSite, nStatus string
		for nIter.Scan(&nID, &nName, &nSite, &nStatus) {
			nodeNames[nID.String()] = fiber.Map{"name": nName, "site_name": nSite, "status": nStatus}
		}
		nIter.Close()

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
			nodeIDStr := tunnelNodeID.String()
			if nodeIDStr != "00000000-0000-0000-0000-000000000000" {
				item["tunnel_node_id"] = nodeIDStr
				if node, ok := nodeNames[nodeIDStr]; ok {
					item["capillary_name"] = node["name"]
					item["capillary_site"] = node["site_name"]
					item["capillary_status"] = node["status"]
				}
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
		iter := session.Query(`SELECT route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, default_properties, next_route_id FROM arteria.routes`).Iter()
		var id, srcCP, dstCP gocql.UUID
		var name, desc, srcTopic, dstTopic, defaultPropsRaw string
		var isActive bool
		var nextRouteID *gocql.UUID
		for iter.Scan(&id, &name, &desc, &srcCP, &dstCP, &srcTopic, &dstTopic, &isActive, &defaultPropsRaw, &nextRouteID) {
			item := fiber.Map{
				"route_id": id.String(), "name": name, "description": desc,
				"source_comm_point_id": srcCP.String(), "dest_comm_point_id": dstCP.String(),
				"source_topic": srcTopic, "destination_topic": dstTopic, "is_active": isActive,
			}
			if defaultPropsRaw != "" {
				var props map[string]string
				json.Unmarshal([]byte(defaultPropsRaw), &props)
				item["default_properties"] = props
			}
			if nextRouteID != nil {
				item["next_route_id"] = nextRouteID.String()
			}
			items = append(items, item)
			nextRouteID = nil
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
		var name, desc, srcTopic, dstTopic, defaultPropsRaw string
		var srcCP, dstCP gocql.UUID
		var isActive bool
		var nextRouteID *gocql.UUID
		err = session.Query(`SELECT name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, default_properties, next_route_id FROM arteria.routes WHERE route_id=?`, id).
			Scan(&name, &desc, &srcCP, &dstCP, &srcTopic, &dstTopic, &isActive, &defaultPropsRaw, &nextRouteID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "route not found"})
		}
		result := fiber.Map{
			"route_id": id.String(), "name": name, "description": desc,
			"source_comm_point_id": srcCP.String(), "dest_comm_point_id": dstCP.String(),
			"source_topic": srcTopic, "destination_topic": dstTopic, "is_active": isActive,
		}
		if defaultPropsRaw != "" {
			var props map[string]string
			json.Unmarshal([]byte(defaultPropsRaw), &props)
			result["default_properties"] = props
		}
		if nextRouteID != nil {
			result["next_route_id"] = nextRouteID.String()
		}
		return c.JSON(result)
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
		publishConfigChange(natsConn, "route", "created", id.String())
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
			Name         string   `json:"name"`
			Description  string   `json:"description"`
			SourceCP     string   `json:"source_comm_point_id"`
			DestCP       string   `json:"dest_comm_point_id"`
			FanOutCPIDs  []string `json:"fan_out_cp_ids"`
			SourceTopic  string   `json:"source_topic"`
			DestTopic    string   `json:"destination_topic"`
			IsActive     bool     `json:"is_active"`
		}
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		srcCP, _ := gocql.ParseUUID(p.SourceCP)
		dstCP, _ := gocql.ParseUUID(p.DestCP)

		// Parse fan-out CP IDs
		var fanOutIDs []gocql.UUID
		for _, idStr := range p.FanOutCPIDs {
			if uid, err := gocql.ParseUUID(idStr); err == nil {
				fanOutIDs = append(fanOutIDs, uid)
			}
		}

		session.Query(`UPDATE arteria.routes SET name=?, description=?, source_comm_point_id=?, dest_comm_point_id=?, source_topic=?, destination_topic=?, is_active=?, fan_out_cp_ids=?, updated_at=? WHERE route_id=?`,
			p.Name, p.Description, srcCP, dstCP, p.SourceTopic, p.DestTopic, p.IsActive, fanOutIDs, time.Now(), id).Exec()
		publishConfigChange(natsConn, "route", "updated", id.String())
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
		publishConfigChange(natsConn, "route", "deleted", id.String())
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

		publishConfigChange(natsConn, "filter", "created", fID.String())
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

		// Query messages_by_status for ROUTED messages (newest first)
		// This table has created_at as clustering key = sorted by time
		var items []fiber.Map
		for _, status := range []string{"ROUTED", "DELIVERED", "RECEIVED", "ERROR"} {
			iter := session.Query(`SELECT message_id, created_at, message_type, patient_id FROM arteria.messages_by_status WHERE status = ? LIMIT ?`, status, limit).Iter()
			var id gocql.UUID
			var pid, mt string
			var ca time.Time
			for iter.Scan(&id, &ca, &mt, &pid) {
				items = append(items, fiber.Map{
					"message_id": id.String(), "patient_id": pid, "message_type": mt,
					"status": status, "created_at": ca,
				})
			}
			iter.Close()
		}

		// Sort by created_at descending (newest first)
		sortByTime(items)

		// Trim to limit
		if len(items) > limit {
			items = items[:limit]
		}

		// Enrich with full details from messages table
		for i, item := range items {
			msgID, _ := gocql.ParseUUID(item["message_id"].(string))
			var te, sf, st string
			session.Query(`SELECT trigger_event, sending_facility, status FROM arteria.messages WHERE message_id = ?`, msgID).Scan(&te, &sf, &st)
			items[i]["trigger_event"] = te
			items[i]["sending_facility"] = sf
			if st != "" {
				items[i]["status"] = st
			}
		}

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

func orDefault(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}

func sortByTime(items []fiber.Map) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			ti, _ := items[i]["created_at"].(time.Time)
			tj, _ := items[j]["created_at"].(time.Time)
			if tj.After(ti) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
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

func authMiddleware(jwtSecret string, tracker *auth.SessionTracker) fiber.Handler {
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

		// Track session activity
		if tracker != nil {
			tracker.Touch(claims.UserID, claims.Username, claims.Role, c.IP(), c.Get("User-Agent"))
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
		var isActive, mustChangePassword bool
		err := session.Query(`SELECT user_id, password_hash, role, is_active, must_change_password FROM arteria.users WHERE username = ? ALLOW FILTERING`, body.Username).
			Scan(&userID, &passwordHash, &role, &isActive, &mustChangePassword)
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
			"token":                token,
			"username":             body.Username,
			"role":                 role,
			"expires_in":           int(cfg.TokenExpiry.Seconds()),
			"must_change_password": mustChangePassword,
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

		// Validate against password policy
		policy := auth.DefaultPasswordPolicy()
		if err := policy.Validate(body.NewPassword); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error(), "policy": policy})
		}

		userID, _ := gocql.ParseUUID(claims.UserID)

		// Check if this is a forced password change (first login)
		var mustChange bool
		session.Query(`SELECT must_change_password FROM arteria.users WHERE user_id = ?`, userID).Scan(&mustChange)

		if !mustChange {
			// Normal change: verify current password
			var currentHash string
			session.Query(`SELECT password_hash FROM arteria.users WHERE user_id = ?`, userID).Scan(&currentHash)
			if !auth.CheckPassword(body.CurrentPassword, currentHash) {
				return c.Status(401).JSON(fiber.Map{"error": "current password is incorrect"})
			}
		}
		// If must_change_password=true, skip current password check (forced change)

		newHash, _ := auth.HashPassword(body.NewPassword)
		session.Query(`UPDATE arteria.users SET password_hash = ?, must_change_password = ? WHERE user_id = ?`, newHash, false, userID).Exec()

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

// publishConfigChange notifies WebSocket clients that config has changed.
func publishConfigChange(nc *nats.Conn, entity, action, id string) {
	data, _ := json.Marshal(map[string]string{"entity": entity, "action": action, "id": id})
	nc.Publish("arteria.events.config", data)
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
		claims := c.Locals("claims").(*auth.Claims)
		callerIsPlatform := auth.IsPlatformRole(claims.Role)

		var items []fiber.Map
		iter := session.Query(`SELECT user_id, username, role, is_active, org_id, created_at FROM arteria.users`).Iter()
		var id gocql.UUID
		var username, role string
		var isActive bool
		var orgID *gocql.UUID
		var createdAt time.Time
		for iter.Scan(&id, &username, &role, &isActive, &orgID, &createdAt) {
			if username == "" {
				continue
			}
			userHasOrg := orgID != nil && orgID.String() != "00000000-0000-0000-0000-000000000000"

			// Platform users only see platform users (no org_id)
			// Org users only see users in their own org
			if callerIsPlatform && userHasOrg {
				continue // super_admin doesn't see org-bound users
			}
			if !callerIsPlatform && !userHasOrg {
				continue // org admin doesn't see platform users
			}

			item := fiber.Map{
				"user_id": id.String(), "username": username, "role": role,
				"is_active": isActive, "created_at": createdAt,
			}
			if userHasOrg {
				item["org_id"] = orgID.String()
			}
			items = append(items, item)
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
		policy := auth.DefaultPasswordPolicy()
		if err := policy.Validate(body.Password); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error(), "policy": policy})
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

// ==================== In-App Messaging ====================

func getInbox(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*auth.Claims)
		var messages []fiber.Map
		iter := session.Query(`SELECT message_id, from_user, to_user, subject, body, is_read, created_at FROM arteria.messages_internal WHERE to_user = ?`, claims.Username).Iter()
		var msgID gocql.UUID
		var from, to, subject, body string
		var isRead bool
		var createdAt time.Time
		for iter.Scan(&msgID, &from, &to, &subject, &body, &isRead, &createdAt) {
			messages = append(messages, fiber.Map{
				"message_id": msgID.String(), "from": from, "to": to,
				"subject": subject, "body": body, "is_read": isRead, "created_at": createdAt,
			})
		}
		iter.Close()
		if messages == nil {
			messages = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"messages": messages, "count": len(messages)})
	}
}

func getSentMessages(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*auth.Claims)
		var messages []fiber.Map
		iter := session.Query(`SELECT message_id, from_user, to_user, subject, body, is_read, created_at FROM arteria.messages_internal WHERE from_user = ?`, claims.Username).Iter()
		var msgID gocql.UUID
		var from, to, subject, body string
		var isRead bool
		var createdAt time.Time
		for iter.Scan(&msgID, &from, &to, &subject, &body, &isRead, &createdAt) {
			messages = append(messages, fiber.Map{
				"message_id": msgID.String(), "from": from, "to": to,
				"subject": subject, "body": body, "is_read": isRead, "created_at": createdAt,
			})
		}
		iter.Close()
		if messages == nil {
			messages = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"messages": messages, "count": len(messages)})
	}
}

func sendMessage(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*auth.Claims)
		var body struct {
			To      string `json:"to"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.To == "" || body.Subject == "" {
			return c.Status(400).JSON(fiber.Map{"error": "to and subject required"})
		}

		// Validate recipient exists and is in same org (or both are platform users)
		var recipientOrgID *gocql.UUID
		var recipientRole string
		err := session.Query(`SELECT org_id, role FROM arteria.users WHERE username = ? ALLOW FILTERING`, body.To).Scan(&recipientOrgID, &recipientRole)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "recipient user not found"})
		}

		// Get sender's org
		var senderOrgID *gocql.UUID
		session.Query(`SELECT org_id FROM arteria.users WHERE username = ? ALLOW FILTERING`, claims.Username).Scan(&senderOrgID)

		senderIsPlatform := auth.IsPlatformRole(claims.Role)
		recipientIsPlatform := auth.IsPlatformRole(recipientRole)

		// Rules: platform users can message other platform users
		// Org users can only message users in the same org
		if !senderIsPlatform && !recipientIsPlatform {
			// Both are org users — must be same org
			if senderOrgID == nil || recipientOrgID == nil || senderOrgID.String() != recipientOrgID.String() {
				return c.Status(403).JSON(fiber.Map{"error": "can only message users in your organisation"})
			}
		}

		msgID := gocql.TimeUUID()
		if err := session.Query(
			`INSERT INTO arteria.messages_internal (message_id, from_user, to_user, subject, body, is_read, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			msgID, claims.Username, body.To, body.Subject, body.Body, false, time.Now(),
		).Exec(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		return c.Status(201).JSON(fiber.Map{"message_id": msgID.String(), "status": "sent"})
	}
}

func markMessageRead(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		msgID, err := gocql.ParseUUID(id)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid message id"})
		}
		session.Query(`UPDATE arteria.messages_internal SET is_read = ? WHERE message_id = ?`, true, msgID).Exec()
		return c.JSON(fiber.Map{"status": "read"})
	}
}

func getUnreadCount(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*auth.Claims)
		var count int
		session.Query(`SELECT COUNT(*) FROM arteria.messages_internal WHERE to_user = ? AND is_read = false ALLOW FILTERING`, claims.Username).Scan(&count)
		return c.JSON(fiber.Map{"unread": count})
	}
}

// ==================== Organisations (Multi-Tenant) ====================

func listOrganisations(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var orgs []fiber.Map
		iter := session.Query(`SELECT org_id, name, slug, custom_domain, is_active, created_at FROM arteria.organisations`).Iter()
		var orgID gocql.UUID
		var name, slug, domain string
		var isActive bool
		var createdAt time.Time
		for iter.Scan(&orgID, &name, &slug, &domain, &isActive, &createdAt) {
			orgs = append(orgs, fiber.Map{
				"org_id": orgID.String(), "name": name, "slug": slug,
				"custom_domain": domain, "is_active": isActive, "created_at": createdAt,
			})
		}
		iter.Close()
		if orgs == nil {
			orgs = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"organisations": orgs, "count": len(orgs)})
	}
}

func createOrganisation(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var body struct {
			Name         string `json:"name"`
			Slug         string `json:"slug"`
			CustomDomain string `json:"custom_domain"`
			SupportEmail string `json:"support_email"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.Name == "" || body.Slug == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name and slug required"})
		}

		orgID := gocql.TimeUUID()
		now := time.Now()
		if err := session.Query(
			`INSERT INTO arteria.organisations (org_id, name, slug, custom_domain, support_email, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			orgID, body.Name, body.Slug, body.CustomDomain, body.SupportEmail, true, now, now,
		).Exec(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		// Auto-create org admin user with default password
		adminUsername := body.Slug + ".admin"
		defaultPass := "arteria123"
		hash, _ := auth.HashPassword(defaultPass)
		adminUserID := gocql.TimeUUID()
		session.Query(
			`INSERT INTO arteria.users (user_id, username, password_hash, role, is_active, must_change_password, org_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			adminUserID, adminUsername, hash, "admin", true, true, orgID, now,
		).Exec()

		return c.Status(201).JSON(fiber.Map{
			"org_id":        orgID.String(),
			"name":          body.Name,
			"slug":          body.Slug,
			"admin_user":    adminUsername,
			"admin_password": defaultPass,
			"note":          "Admin user created with default password (must change on first login)",
		})
	}
}

func getOrganisation(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		orgID, err := gocql.ParseUUID(id)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid org id"})
		}

		var name, slug, domain, logo, appName, primaryColor, accentColor, favicon, supportEmail string
		var isActive bool
		var createdAt time.Time
		err = session.Query(
			`SELECT name, slug, custom_domain, branding_logo_url, branding_app_name, branding_primary_color, branding_accent_color, branding_favicon_url, support_email, is_active, created_at FROM arteria.organisations WHERE org_id = ?`, orgID,
		).Scan(&name, &slug, &domain, &logo, &appName, &primaryColor, &accentColor, &favicon, &supportEmail, &isActive, &createdAt)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "organisation not found"})
		}

		return c.JSON(fiber.Map{
			"org_id": orgID.String(), "name": name, "slug": slug, "custom_domain": domain,
			"branding": fiber.Map{
				"logo_url":      logo,
				"app_name":      appName,
				"primary_color": primaryColor,
				"accent_color":  accentColor,
				"favicon_url":   favicon,
			},
			"support_email": supportEmail, "is_active": isActive, "created_at": createdAt,
		})
	}
}

func updateOrganisationBranding(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		orgID, err := gocql.ParseUUID(id)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid org id"})
		}

		var body struct {
			LogoURL      *string `json:"logo_url"`
			AppName      *string `json:"app_name"`
			PrimaryColor *string `json:"primary_color"`
			AccentColor  *string `json:"accent_color"`
			FaviconURL   *string `json:"favicon_url"`
			CustomDomain *string `json:"custom_domain"`
			SupportEmail *string `json:"support_email"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		now := time.Now()
		if body.LogoURL != nil {
			session.Query(`UPDATE arteria.organisations SET branding_logo_url = ?, updated_at = ? WHERE org_id = ?`, *body.LogoURL, now, orgID).Exec()
		}
		if body.AppName != nil {
			session.Query(`UPDATE arteria.organisations SET branding_app_name = ?, updated_at = ? WHERE org_id = ?`, *body.AppName, now, orgID).Exec()
		}
		if body.PrimaryColor != nil {
			session.Query(`UPDATE arteria.organisations SET branding_primary_color = ?, updated_at = ? WHERE org_id = ?`, *body.PrimaryColor, now, orgID).Exec()
		}
		if body.AccentColor != nil {
			session.Query(`UPDATE arteria.organisations SET branding_accent_color = ?, updated_at = ? WHERE org_id = ?`, *body.AccentColor, now, orgID).Exec()
		}
		if body.FaviconURL != nil {
			session.Query(`UPDATE arteria.organisations SET branding_favicon_url = ?, updated_at = ? WHERE org_id = ?`, *body.FaviconURL, now, orgID).Exec()
		}
		if body.CustomDomain != nil {
			session.Query(`UPDATE arteria.organisations SET custom_domain = ?, updated_at = ? WHERE org_id = ?`, *body.CustomDomain, now, orgID).Exec()
		}
		if body.SupportEmail != nil {
			session.Query(`UPDATE arteria.organisations SET support_email = ?, updated_at = ? WHERE org_id = ?`, *body.SupportEmail, now, orgID).Exec()
		}

		return c.JSON(fiber.Map{"status": "updated"})
	}
}

// ==================== Platform Health & Monitoring ====================

func platformHealthCheck(dbSession *gocql.Session, nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		components := make(fiber.Map)

		// Check ScyllaDB
		start := time.Now()
		var count int
		err := dbSession.Query(`SELECT COUNT(*) FROM arteria.users`).Scan(&count)
		dbLatency := time.Since(start).Milliseconds()
		if err != nil {
			components["scylladb"] = fiber.Map{"status": "down", "error": err.Error()}
		} else {
			components["scylladb"] = fiber.Map{"status": "up", "latency_ms": dbLatency, "details": fmt.Sprintf("%d users", count)}
		}

		// Check NATS
		start = time.Now()
		_, err = nc.Request("arteria.metrics.ingestion", nil, 2*time.Second)
		natsLatency := time.Since(start).Milliseconds()
		if err != nil {
			components["nats"] = fiber.Map{"status": "down"}
		} else {
			components["nats"] = fiber.Map{"status": "up", "latency_ms": natsLatency}
		}

		// Check Ingestion
		start = time.Now()
		_, err = nc.Request("arteria.metrics.ingestion", nil, 2*time.Second)
		ingLatency := time.Since(start).Milliseconds()
		if err != nil {
			components["ingestion"] = fiber.Map{"status": "down"}
		} else {
			components["ingestion"] = fiber.Map{"status": "up", "latency_ms": ingLatency}
		}

		// Check Processing
		start = time.Now()
		_, err = nc.Request("arteria.metrics.processing", nil, 2*time.Second)
		procLatency := time.Since(start).Milliseconds()
		if err != nil {
			components["processing"] = fiber.Map{"status": "down"}
		} else {
			components["processing"] = fiber.Map{"status": "up", "latency_ms": procLatency}
		}

		// Check Egress
		start = time.Now()
		_, err = nc.Request("arteria.metrics.egress", nil, 2*time.Second)
		egressLatency := time.Since(start).Milliseconds()
		if err != nil {
			components["egress"] = fiber.Map{"status": "down"}
		} else {
			components["egress"] = fiber.Map{"status": "up", "latency_ms": egressLatency}
		}

		// Check Aorta Broker (try NATS metrics, fallback to TCP check)
		start = time.Now()
		_, err = nc.Request("arteria.metrics.tunnel-broker", nil, 2*time.Second)
		brokerLatency := time.Since(start).Milliseconds()
		if err != nil {
			// Broker doesn't have NATS metrics — check TCP connectivity instead
			conn, tcpErr := net.DialTimeout("tcp", "tunnel-broker:9443", 2*time.Second)
			if tcpErr == nil {
				conn.Close()
				components["aorta_broker"] = fiber.Map{"status": "up", "latency_ms": time.Since(start).Milliseconds(), "details": "TCP reachable"}
			} else {
				components["aorta_broker"] = fiber.Map{"status": "down", "details": "unreachable"}
			}
		} else {
			components["aorta_broker"] = fiber.Map{"status": "up", "latency_ms": brokerLatency}
		}

		// Count tunnel nodes
		var totalNodes, connectedNodes int
		nodeIter := dbSession.Query(`SELECT status FROM arteria.tunnel_nodes`).Iter()
		var nodeStatus string
		for nodeIter.Scan(&nodeStatus) {
			totalNodes++
			if nodeStatus == "CONNECTED" || nodeStatus == "connected" {
				connectedNodes++
			}
		}
		nodeIter.Close()

		// Count orgs
		var orgCount int
		dbSession.Query(`SELECT COUNT(*) FROM arteria.organisations`).Scan(&orgCount)

		// Count users
		var userCount int
		dbSession.Query(`SELECT COUNT(*) FROM arteria.users`).Scan(&userCount)

		// Overall status
		overallStatus := "healthy"
		for _, comp := range components {
			if m, ok := comp.(fiber.Map); ok {
				if m["status"] == "down" {
					overallStatus = "degraded"
					break
				}
			}
		}

		// System resources
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		resources := fiber.Map{
			"memory": fiber.Map{
				"used":    memStats.Alloc / (1024 * 1024),
				"total":   memStats.Sys / (1024 * 1024),
				"unit":    "MB",
				"percent": float64(memStats.Alloc) / float64(memStats.Sys) * 100,
			},
			"goroutines": fiber.Map{
				"used":    runtime.NumGoroutine(),
				"total":   1000,
				"unit":    "",
				"percent": float64(runtime.NumGoroutine()) / 10, // % of 1000 max
			},
			"gc_cycles": fiber.Map{
				"used":    memStats.NumGC,
				"total":   memStats.NumGC,
				"unit":    "",
				"percent": 0.0,
			},
			"heap": fiber.Map{
				"used":    memStats.HeapInuse / (1024 * 1024),
				"total":   memStats.HeapSys / (1024 * 1024),
				"unit":    "MB",
				"percent": float64(memStats.HeapInuse) / float64(memStats.HeapSys) * 100,
			},
		}

		return c.JSON(fiber.Map{
			"status":       overallStatus,
			"components":   components,
			"resources":    resources,
			"tunnel_nodes": fiber.Map{"total": totalNodes, "connected": connectedNodes},
			"orgs":         fiber.Map{"total": orgCount},
			"users":        fiber.Map{"total": userCount, "online": sessionTracker.Count()},
			"timestamp":    time.Now(),
		})
	}
}

func getTunnelStats(nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		resp, err := nc.Request("arteria.metrics.tunnel-broker", nil, 2*time.Second)
		if err != nil {
			return c.JSON(fiber.Map{"error": "broker unreachable", "status": "down"})
		}
		var brokerMetrics map[string]interface{}
		json.Unmarshal(resp.Data, &brokerMetrics)
		return c.JSON(fiber.Map{"status": "up", "metrics": brokerMetrics})
	}
}

func getPlatformUsage(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		orgUsage := make([]fiber.Map, 0)

		// Get time boundaries
		now := time.Now()
		t24h := now.Add(-24 * time.Hour)
		t7d := now.Add(-7 * 24 * time.Hour)
		t30d := now.Add(-30 * 24 * time.Hour)

		// Query organisations
		orgIter := session.Query(`SELECT org_id, name, slug FROM arteria.organisations`).Iter()
		var orgID gocql.UUID
		var orgName, orgSlug string
		for orgIter.Scan(&orgID, &orgName, &orgSlug) {
			// Count users in this org
			var userCount int
			session.Query(`SELECT COUNT(*) FROM arteria.users WHERE org_id = ? ALLOW FILTERING`, orgID).Scan(&userCount)

			// Count CPs in this org
			var cpCount int
			session.Query(`SELECT COUNT(*) FROM arteria.communication_points WHERE org_id = ? ALLOW FILTERING`, orgID).Scan(&cpCount)

			// Count tunnel nodes for this org (nodes linked to org CPs)
			var tunnelCount int
			session.Query(`SELECT COUNT(*) FROM arteria.tunnel_nodes`).Scan(&tunnelCount)

			// Count messages by time window (using messages_by_status table with timestamp)
			var msgs24h, msgs7d, msgs30d, msgsTotal int
			session.Query(`SELECT COUNT(*) FROM arteria.messages`).Scan(&msgsTotal)

			// Time-based counts from messages_by_status (has created_at)
			iter := session.Query(`SELECT created_at FROM arteria.messages_by_status WHERE status = 'ROUTED' ALLOW FILTERING`).Iter()
			var createdAt time.Time
			for iter.Scan(&createdAt) {
				if createdAt.After(t30d) {
					msgs30d++
				}
				if createdAt.After(t7d) {
					msgs7d++
				}
				if createdAt.After(t24h) {
					msgs24h++
				}
			}
			iter.Close()

			orgUsage = append(orgUsage, fiber.Map{
				"org_id":         orgID.String(),
				"name":           orgName,
				"slug":           orgSlug,
				"total_messages": msgsTotal,
				"messages_24h":   msgs24h,
				"messages_7d":    msgs7d,
				"messages_30d":   msgs30d,
				"users":          userCount,
				"comm_points":    cpCount,
				"tunnel_nodes":   tunnelCount,
			})
		}
		orgIter.Close()

		// Total platform stats
		var totalMessages int
		session.Query(`SELECT COUNT(*) FROM arteria.messages`).Scan(&totalMessages)

		return c.JSON(fiber.Map{
			"total_messages": totalMessages,
			"organisations":  orgUsage,
		})
	}
}

// ==================== Platform Logs & NATS Stats ====================

func getServiceLogs() fiber.Handler {
	return func(c *fiber.Ctx) error {
		service := c.Params("service")
		lines := c.QueryInt("lines", 100)

		// Allowed services
		allowed := map[string]string{
			"api":        "/var/log/arteria/api.log",
			"ingestion":  "/var/log/arteria/ingestion.log",
			"processing": "/var/log/arteria/processing.log",
			"egress":     "/var/log/arteria/egress.log",
			"broker":     "/var/log/arteria/broker.log",
		}

		logFile, ok := allowed[service]
		if !ok {
			return c.Status(400).JSON(fiber.Map{"error": "unknown service", "allowed": []string{"api", "ingestion", "processing", "egress", "broker"}})
		}

		// Read last N lines from log file
		data, err := os.ReadFile(logFile)
		if err != nil {
			return c.JSON(fiber.Map{"service": service, "logs": []string{}, "error": "log file not available"})
		}

		allLines := strings.Split(string(data), "\n")
		start := len(allLines) - lines
		if start < 0 {
			start = 0
		}
		logLines := allLines[start:]

		// Filter empty lines
		var result []string
		for _, l := range logLines {
			if l != "" {
				result = append(result, l)
			}
		}

		return c.JSON(fiber.Map{
			"service":    service,
			"logs":       result,
			"total_lines": len(allLines),
			"showing":    len(result),
		})
	}
}

func getNATSStats(nc *nats.Conn) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Query NATS JetStream account info
		js, err := nc.JetStream()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "jetstream not available"})
		}

		info, err := js.AccountInfo()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		// List streams
		var streams []fiber.Map
		for stream := range js.StreamNames() {
			si, err := js.StreamInfo(stream)
			if err != nil {
				continue
			}
			streams = append(streams, fiber.Map{
				"name":       si.Config.Name,
				"subjects":   si.Config.Subjects,
				"messages":   si.State.Msgs,
				"bytes":      si.State.Bytes,
				"consumers":  si.State.Consumers,
				"first_seq":  si.State.FirstSeq,
				"last_seq":   si.State.LastSeq,
				"storage":    si.Config.Storage.String(),
			})
		}

		return c.JSON(fiber.Map{
			"account": fiber.Map{
				"memory":    info.Memory,
				"storage":   info.Store,
				"streams":   info.Streams,
				"consumers": info.Consumers,
			},
			"streams": streams,
		})
	}
}

func getConnectionHistory(session *gocql.Session) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var nodes []fiber.Map
		iter := session.Query(`SELECT node_id, name, site_name, status, agent_version, last_seen FROM arteria.tunnel_nodes`).Iter()
		var id gocql.UUID
		var name, site, status, agentVer string
		var lastSeen time.Time
		for iter.Scan(&id, &name, &site, &status, &agentVer, &lastSeen) {
			uptime := ""
			if status == "CONNECTED" && !lastSeen.IsZero() {
				uptime = time.Since(lastSeen).Round(time.Second).String()
			}
			nodes = append(nodes, fiber.Map{
				"node_id":       id.String(),
				"name":          name,
				"site_name":     site,
				"status":        status,
				"agent_version": agentVer,
				"last_seen":     lastSeen,
				"uptime":        uptime,
			})
		}
		iter.Close()
		if nodes == nil {
			nodes = []fiber.Map{}
		}
		return c.JSON(fiber.Map{"nodes": nodes, "count": len(nodes)})
	}
}
