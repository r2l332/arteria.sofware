package security

import (
	"encoding/json"
	"time"

	"github.com/gocql/gocql"
	"github.com/gofiber/fiber/v2"
)

// AuditEntry records a security-relevant action.
type AuditEntry struct {
	EventID   gocql.UUID `json:"event_id"`
	Timestamp time.Time  `json:"timestamp"`
	Username  string     `json:"username"`
	Action    string     `json:"action"`
	Resource  string     `json:"resource"`
	ResourceID string   `json:"resource_id"`
	ClientIP  string     `json:"client_ip"`
	UserAgent string     `json:"user_agent"`
	Details   string     `json:"details"`
}

// AuditLogger writes security audit events to ScyllaDB.
type AuditLogger struct {
	session *gocql.Session
}

// NewAuditLogger creates an audit logger.
func NewAuditLogger(session *gocql.Session) *AuditLogger {
	return &AuditLogger{session: session}
}

// Log records an audit event.
func (al *AuditLogger) Log(username, action, resource, resourceID, clientIP, userAgent string, details interface{}) {
	detailsJSON := ""
	if details != nil {
		b, _ := json.Marshal(details)
		detailsJSON = string(b)
	}

	al.session.Query(`INSERT INTO arteria.audit_log (event_id, timestamp, username, action, resource, resource_id, client_ip, user_agent, details) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gocql.TimeUUID(), time.Now(), username, action, resource, resourceID, clientIP, userAgent, detailsJSON,
	).Exec()
}

// AuditMiddleware logs all mutating API actions.
func AuditMiddleware(al *AuditLogger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Only audit mutating requests
		method := c.Method()
		if method == "GET" || method == "OPTIONS" || method == "HEAD" {
			return c.Next()
		}

		// Execute the request first
		err := c.Next()

		// Log after execution (so we know if it succeeded)
		username := "anonymous"
		if claims, ok := c.Locals("claims").(interface{ GetUsername() string }); ok {
			username = claims.(interface{ GetUsername() string }).GetUsername()
		}
		// Try to extract username from claims struct
		if claimsRaw := c.Locals("claims"); claimsRaw != nil {
			type hasUsername interface {
				GetUsername() string
			}
			// Use reflection-free approach
			if b, ok := claimsRaw.(interface{ GetField(string) string }); ok {
				username = b.GetField("username")
			}
		}

		al.Log(
			username,
			method,
			c.Path(),
			"",
			c.IP(),
			c.Get("User-Agent"),
			map[string]interface{}{
				"status": c.Response().StatusCode(),
			},
		)

		return err
	}
}
