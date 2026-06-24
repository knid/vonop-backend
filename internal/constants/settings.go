package constants

const (
	// WEMOB_API_BASE_URL is the public base URL for the Wemob API (used for storefront API references)
	WEMOB_API_BASE_URL = "https://api.wemob.io"

	// WEMOB_APP_BASE_DOMAIN is the base domain for deployed V2 apps (e.g. {slug}.wemob.app)
	WEMOB_APP_BASE_DOMAIN = "wemob.app"

	// WEMOB_CNAME_TARGET is the CNAME value users point their custom domains to
	WEMOB_CNAME_TARGET = "cname.wemob.app"

	// MAX_DOMAINS_PER_PROJECT is the maximum number of custom domains per project
	MAX_DOMAINS_PER_PROJECT = 10

	// Agent settings keys written in AppProject.Settings.
	// These are used by the agent tool authorization layer and sandbox control plane.
	AgentSettingAllowSandboxExec = "agent_allow_sandbox_exec"
)

// -----------------------------------------------------------------------------
// Organization granular permissions (Phase B).
// These are capability strings stored in OrganizationMember.Permissions and
// OrganizationInvite.Permissions (jsonb arrays). Role is retained as a label/
// preset for UI and backward compat. When Permissions is empty on a row, the
// effective set is derived from the Role using DefaultPermissionsForRole.
// -----------------------------------------------------------------------------
const (
	// Core org & membership management (owner/admin typically)
	PermOrgSettings  = "org.settings"
	PermMemberInvite = "member.invite"
	PermMemberManage = "member.manage" // includes remove, role/perm changes
	PermOrgTransfer  = "org.transfer"  // ownership transfer (owner only)

	// AI features (generate full apps, chat in studio)
	PermAIGenerate = "ai.generate"
	PermAIChat     = "ai.chat"

	// Visibility & editing inside projects
	PermAnalyticsView   = "analytics.view"
	PermEnvManage       = "env.manage"
	PermChatViewHistory = "chat.view_history"
	PermCodeExport      = "code.export"

	// Deploy actions
	PermBuildTrigger = "build.trigger"

	// Secrets / connections
	PermCredentialsManage = "credentials.manage"

	// Push / email / notification automation
	PermNotificationSend = "notification.send"

	// Other admin actions
	PermProjectCreate = "project.create" // creating new projects inside the org
)

// AllOrgPermissions is the exhaustive list of known capabilities (useful for
// UI matrices and validation).
var AllOrgPermissions = []string{
	PermOrgSettings,
	PermMemberInvite,
	PermMemberManage,
	PermOrgTransfer,
	PermAIGenerate,
	PermAIChat,
	PermAnalyticsView,
	PermEnvManage,
	PermChatViewHistory,
	PermCodeExport,
	PermBuildTrigger,
	PermCredentialsManage,
	PermNotificationSend,
	PermProjectCreate,
}

// DefaultPermissionsForRole returns the baseline set of permissions implied by
// the legacy coarse role. These are used when a member's Permissions field is
// empty (migration / legacy rows) or when inviting without an explicit override.
func DefaultPermissionsForRole(role string) []string {
	switch role {
	case "owner":
		// Full power
		return append([]string(nil), AllOrgPermissions...)
	case "admin":
		// Most capabilities, but not ownership transfer or sole-owner actions.
		return []string{
			PermOrgSettings,
			PermMemberInvite,
			PermMemberManage,
			// PermOrgTransfer intentionally omitted for admin
			PermAIGenerate,
			PermAIChat,
			PermAnalyticsView,
			PermEnvManage,
			PermChatViewHistory,
			PermCodeExport,
			PermBuildTrigger,
			PermCredentialsManage,
			PermNotificationSend,
			PermProjectCreate,
		}
	case "member":
		// Safe collaboration set for agency team members / contractors.
		// Includes AI + build + viewing, but not member/credential/org admin.
		return []string{
			PermAIGenerate,
			PermAIChat,
			PermAnalyticsView,
			PermEnvManage,
			PermChatViewHistory,
			PermCodeExport,
			PermBuildTrigger,
			PermNotificationSend,
			PermProjectCreate,
			// Intentionally no: member.*, org.settings, credentials.*, org.transfer
		}
	default:
		// Unknown role -> very restricted (only basic read via membership)
		return []string{}
	}
}
