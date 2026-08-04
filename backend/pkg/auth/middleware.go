package auth

import (
	"github.com/gofiber/fiber/v2"
)

// RequirePermission returns a Fiber middleware that checks the user's role has the required permission.
func RequirePermission(perm Permission) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*Claims)
		if !ok || claims == nil {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}

		if !HasPermission(claims.Role, perm) {
			return c.Status(403).JSON(fiber.Map{
				"error":      "forbidden",
				"detail":     "insufficient permissions",
				"required":   string(perm),
				"your_role":  claims.Role,
			})
		}

		return c.Next()
	}
}

// RequireAnyPermission checks that the user has at least one of the given permissions.
func RequireAnyPermission(perms ...Permission) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*Claims)
		if !ok || claims == nil {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}

		for _, perm := range perms {
			if HasPermission(claims.Role, perm) {
				return c.Next()
			}
		}

		return c.Status(403).JSON(fiber.Map{
			"error":     "forbidden",
			"detail":    "insufficient permissions",
			"your_role": claims.Role,
		})
	}
}
