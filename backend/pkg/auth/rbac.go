package auth

// Permission represents an API action.
type Permission string

const (
	// Communication Points
	PermCPView   Permission = "cp:view"
	PermCPManage Permission = "cp:manage"

	// Routes & Filters
	PermRouteView   Permission = "route:view"
	PermRouteManage Permission = "route:manage"

	// Messages (PHI)
	PermMessageView        Permission = "message:view"
	PermMessageViewSandbox Permission = "message:view_sandbox"

	// Tunnel Mesh
	PermTunnelView   Permission = "tunnel:view"
	PermTunnelManage Permission = "tunnel:manage"

	// JS Playground
	PermPlayground Permission = "playground:execute"

	// Errors / DLQ
	PermErrorView   Permission = "error:view"
	PermErrorManage Permission = "error:manage"

	// Config & Backup
	PermConfigView   Permission = "config:view"
	PermConfigManage Permission = "config:manage"

	// Users & RBAC
	PermUserView   Permission = "user:view"
	PermUserManage Permission = "user:manage"

	// Metrics & Monitoring
	PermMetricsView Permission = "metrics:view"
)

// RolePermissions maps each role to its allowed permissions.
var RolePermissions = map[string][]Permission{
	"admin": {
		PermCPView, PermCPManage,
		PermRouteView, PermRouteManage,
		PermMessageView, PermMessageViewSandbox,
		PermTunnelView, PermTunnelManage,
		PermPlayground,
		PermErrorView, PermErrorManage,
		PermConfigView, PermConfigManage,
		PermUserView, PermUserManage,
		PermMetricsView,
	},
	"developer": {
		PermCPView, PermCPManage,
		PermRouteView, PermRouteManage,
		PermMessageViewSandbox, // Can only see sandbox/test messages
		PermTunnelView, PermTunnelManage,
		PermPlayground,
		PermErrorView,
		PermConfigView,
		PermMetricsView,
	},
	"operator": {
		PermCPView,
		PermRouteView,
		// No message access (PHI)
		PermTunnelView,
		// No playground
		PermErrorView, PermErrorManage,
		PermConfigView,
		PermMetricsView,
	},
	"security": {
		// No route/CP/message/tunnel access
		PermConfigView, PermConfigManage,
		PermUserView, PermUserManage,
		PermMetricsView,
	},
	"viewer": {
		PermCPView,
		PermRouteView,
		PermMessageView, // Read-only view of messages
		PermTunnelView,
		PermErrorView,
		PermMetricsView,
	},
}

// HasPermission checks if a role has a specific permission.
func HasPermission(role string, perm Permission) bool {
	perms, ok := RolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// GetPermissions returns all permissions for a role.
func GetPermissions(role string) []Permission {
	return RolePermissions[role]
}

// AllRoles returns the list of available roles.
func AllRoles() []string {
	return []string{"admin", "developer", "operator", "security", "viewer"}
}
