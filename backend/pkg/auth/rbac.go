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

	// Platform-level (super_admin / devops only)
	PermOrgView   Permission = "org:view"
	PermOrgManage Permission = "org:manage"
	PermPlatformManage Permission = "platform:manage"
)

// RolePermissions maps each role to its allowed permissions.
var RolePermissions = map[string][]Permission{
	// --- Platform Roles (not scoped to any org) ---
	"super_admin": {
		// Can see and manage orgs, users, config, metrics — but NOT messages/PHI
		PermOrgView, PermOrgManage,
		PermPlatformManage,
		PermCPView,       // Can see CPs exist (not their data)
		PermRouteView,    // Can see routes exist
		PermTunnelView, PermTunnelManage,
		PermErrorView,
		PermConfigView, PermConfigManage,
		PermUserView, PermUserManage,
		PermMetricsView,
		// NO PermMessageView — cannot read PHI
		// NO PermPlayground — cannot execute code in tenant context
	},
	"devops": {
		// Platform infrastructure only — cannot see into orgs
		PermOrgView,
		PermPlatformManage,
		PermMetricsView,
		PermConfigView,
		// NO access to CPs, routes, messages, users, tunnels
	},

	// --- Org-Level Roles (scoped to the user's organisation) ---
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

// IsPlatformRole returns true if the role is a platform-level role (not org-scoped).
func IsPlatformRole(role string) bool {
	return role == "super_admin" || role == "devops"
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
	return []string{"super_admin", "devops", "admin", "developer", "operator", "security", "viewer"}
}

// OrgRoles returns only org-level roles (for user creation within an org).
func OrgRoles() []string {
	return []string{"admin", "developer", "operator", "security", "viewer"}
}

// PlatformRoles returns only platform-level roles.
func PlatformRoles() []string {
	return []string{"super_admin", "devops"}
}
