package security

import (
	"github.com/gofiber/fiber/v2"
)

// Headers adds security headers required by healthcare compliance.
func Headers() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Prevent clickjacking
		c.Set("X-Frame-Options", "DENY")
		// Prevent MIME sniffing
		c.Set("X-Content-Type-Options", "nosniff")
		// XSS protection
		c.Set("X-XSS-Protection", "1; mode=block")
		// Referrer policy
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Permissions policy (disable unnecessary browser features)
		c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		// Cache control for sensitive data
		c.Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
		c.Set("Pragma", "no-cache")
		// HSTS (only effective with TLS, but set anyway for when TLS terminates upstream)
		c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		return c.Next()
	}
}

// IPAllowlist restricts access to a list of allowed IPs/CIDRs.
// If the allowlist is empty, all IPs are allowed.
func IPAllowlist(allowed []string) fiber.Handler {
	if len(allowed) == 0 {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	allowMap := make(map[string]bool, len(allowed))
	for _, ip := range allowed {
		allowMap[ip] = true
	}

	return func(c *fiber.Ctx) error {
		clientIP := c.IP()
		if !allowMap[clientIP] {
			return c.Status(403).JSON(fiber.Map{
				"error":  "access denied",
				"detail": "IP not in allowlist",
			})
		}
		return c.Next()
	}
}
