package contract

import "time"

// EntitlementSource is which grant currently provides Pro access.
type EntitlementSource string

const (
	EntitlementSourceNone  EntitlementSource = "none"
	EntitlementSourceTrial EntitlementSource = "trial"
	EntitlementSourcePaid  EntitlementSource = "paid"
)

// EntitlementStatus answers GET /admin/license/status and every license or
// trial mutation that succeeds. ExpiresAt is absent when no license or trial
// carries an expiry.
type EntitlementStatus struct {
	IsPro            bool              `json:"is_pro"`
	Source           EntitlementSource `json:"source"`
	ExpiresAt        *time.Time        `json:"expires_at,omitempty"`
	DaysLeft         int               `json:"days_left"`
	TrialUsed        bool              `json:"trial_used"`
	TrialAvailable   bool              `json:"trial_available"`
	LicenseKeyMasked string            `json:"license_key_masked,omitempty"`
	LicenseError     string            `json:"license_error,omitempty"`
	LastChecked      time.Time         `json:"last_checked"`
}

// LicenseRevalidateResponse answers POST /admin/license/revalidate, which only
// starts the background check.
type LicenseRevalidateResponse struct {
	Message string `json:"message"`
}

// WebhookPlatform is the message format a webhook is sent in.
type WebhookPlatform string

const (
	WebhookPlatformSlack    WebhookPlatform = "slack"
	WebhookPlatformDingTalk WebhookPlatform = "dingtalk"
	WebhookPlatformWeCom    WebhookPlatform = "wecom"
	WebhookPlatformFeishu   WebhookPlatform = "feishu"
	WebhookPlatformGeneric  WebhookPlatform = "generic"
)

// WebhookConfig is one notification webhook. The list masks URL for a
// principal that may not view credentials; creation echoes the URL it stored.
type WebhookConfig struct {
	ID              uint            `json:"id"`
	Name            string          `json:"name"`
	Platform        WebhookPlatform `json:"platform"`
	URL             string          `json:"url"`
	Enabled         bool            `json:"enabled"`
	Events          string          `json:"events"`
	CooldownMinutes int             `json:"cooldown_minutes"`
	LastSentAt      *time.Time      `json:"last_sent_at" tstype:"string | null,required"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// WebhookAckStatus is the outcome a webhook mutation reports.
type WebhookAckStatus string

const (
	WebhookAckStatusOK         WebhookAckStatus = "ok"
	WebhookAckStatusDeleted    WebhookAckStatus = "deleted"
	WebhookAckStatusTestQueued WebhookAckStatus = "test queued"
)

// WebhookAck answers webhook update ("ok"), delete ("deleted"), and test
// ("test queued", sent with 202 because delivery is asynchronous).
type WebhookAck struct {
	Status WebhookAckStatus `json:"status"`
}

// AdminUserRole is a local account's Admin role.
type AdminUserRole string

const (
	AdminUserRoleAdmin    AdminUserRole = "admin"
	AdminUserRoleReadonly AdminUserRole = "readonly"
)

// AdminUserPermissionSource is who owns an account's role and credentials:
// Depsilo itself or an external identity provider.
type AdminUserPermissionSource string

const (
	AdminUserPermissionSourceLocal AdminUserPermissionSource = "local"
	AdminUserPermissionSourceOIDC  AdminUserPermissionSource = "oidc"
	AdminUserPermissionSourceLDAP  AdminUserPermissionSource = "ldap"
)

// AdminUser is an account without its password hash or credential version.
// GET /admin/users returns a list of them; create and update return one.
type AdminUser struct {
	ID               uint                      `json:"id"`
	Username         string                    `json:"username"`
	Role             AdminUserRole             `json:"role"`
	Enabled          bool                      `json:"enabled"`
	LastLoginAt      *time.Time                `json:"last_login_at" tstype:"string | null,required"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	PermissionSource AdminUserPermissionSource `json:"permission_source"`
}

// AdminUserDeleteResponse answers DELETE /admin/users/:id.
type AdminUserDeleteResponse struct {
	Message string `json:"message"`
}

// APITokenPermissions is what an Admin API token may do.
type APITokenPermissions string

const (
	APITokenPermissionsReadonly  APITokenPermissions = "readonly"
	APITokenPermissionsReadwrite APITokenPermissions = "readwrite"
)

// APITokenSummary is an Admin API token without its hash. GET /admin/tokens
// returns a list of them.
type APITokenSummary struct {
	ID          uint                `json:"id"`
	UserID      uint                `json:"user_id"`
	Name        string              `json:"name"`
	Permissions APITokenPermissions `json:"permissions"`
	ExpiresAt   *time.Time          `json:"expires_at" tstype:"string | null,required"`
	LastUsedAt  *time.Time          `json:"last_used_at" tstype:"string | null,required"`
	CreatedAt   time.Time           `json:"created_at"`
}

// CreateAPITokenResponse answers token creation. It is the only response that
// carries the plain token.
type CreateAPITokenResponse struct {
	ID          uint                `json:"id"`
	Name        string              `json:"name"`
	Token       string              `json:"token"`
	Permissions APITokenPermissions `json:"permissions"`
	ExpiresAt   *time.Time          `json:"expires_at" tstype:"string | null,required"`
	Warning     string              `json:"warning"`
}

// APITokenRevokeResponse answers DELETE /admin/tokens/:id.
type APITokenRevokeResponse struct {
	Message string `json:"message"`
}
