package contract

import "time"

// IdentityRole is the role a Depsilo account, session, or mapped identity
// carries.
type IdentityRole string

const (
	IdentityRoleAdmin    IdentityRole = "admin"
	IdentityRoleReadonly IdentityRole = "readonly"
)

// IdentityPermissionSource is the authority that owns an account's role.
type IdentityPermissionSource string

const (
	IdentityPermissionSourceLocal IdentityPermissionSource = "local"
	IdentityPermissionSourceOIDC  IdentityPermissionSource = "oidc"
	IdentityPermissionSourceLDAP  IdentityPermissionSource = "ldap"
)

// IdentityProtocol is the login protocol of an external authentication
// source.
type IdentityProtocol string

const (
	IdentityProtocolOIDC IdentityProtocol = "oidc"
	IdentityProtocolLDAP IdentityProtocol = "ldap"
)

// PrincipalAuthMethod is how the current request authenticated.
type PrincipalAuthMethod string

const (
	PrincipalAuthMethodJWT      PrincipalAuthMethod = "jwt"
	PrincipalAuthMethodAPIToken PrincipalAuthMethod = "api_token"
)

// PrincipalLoginMethod is how the session behind the current request began.
type PrincipalLoginMethod string

const (
	PrincipalLoginMethodPassword PrincipalLoginMethod = "password"
	PrincipalLoginMethodOIDC     PrincipalLoginMethod = "oidc"
	PrincipalLoginMethodLDAP     PrincipalLoginMethod = "ldap"
	PrincipalLoginMethodAPIToken PrincipalLoginMethod = "api_token"
)

// PrincipalTokenPermissions is what an API token permits.
type PrincipalTokenPermissions string

const (
	PrincipalTokenPermissionsReadOnly  PrincipalTokenPermissions = "readonly"
	PrincipalTokenPermissionsReadWrite PrincipalTokenPermissions = "readwrite"
)

// Principal answers GET /auth/me: the authenticated caller and what it may do.
// TokenPermissions is null for a JWT session. The current identity fields are
// present only for a session that began with an external identity.
type Principal struct {
	ID                   uint                       `json:"id"`
	Username             string                     `json:"username"`
	Role                 IdentityRole               `json:"role"`
	Enabled              bool                       `json:"enabled"`
	AuthMethod           PrincipalAuthMethod        `json:"auth_method"`
	TokenPermissions     *PrincipalTokenPermissions `json:"token_permissions" tstype:"PrincipalTokenPermissions | null,required"`
	CanWrite             bool                       `json:"can_write"`
	PermissionSource     IdentityPermissionSource   `json:"permission_source"`
	LoginMethod          PrincipalLoginMethod       `json:"login_method"`
	CanManageCredentials bool                       `json:"can_manage_credentials"`
	CurrentIdentityID    uint                       `json:"current_identity_id,omitempty"`
	CurrentProviderID    string                     `json:"current_provider_id,omitempty"`
}

// LoginUser is the account a password login signed in.
type LoginUser struct {
	ID       uint         `json:"id"`
	Username string       `json:"username"`
	Role     IdentityRole `json:"role"`
}

// LoginResponse answers POST /auth/login. ExpiresAt is Unix seconds.
type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt int64     `json:"expires_at"`
	User      LoginUser `json:"user"`
}

// RefreshResponse answers POST /auth/refresh. ExpiresAt is Unix seconds.
type RefreshResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// LogoutResponse answers POST /auth/logout.
type LogoutResponse struct {
	Message string `json:"message"`
}

// IdentityProvider names one external authentication source.
type IdentityProvider struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Protocol IdentityProtocol `json:"protocol"`
}

// IdentityProviderListResponse answers GET /auth/providers with the enabled
// sources a browser may sign in with.
type IdentityProviderListResponse struct {
	Providers []IdentityProvider `json:"providers"`
}

// IdentityAuthorizationStart answers a request that begins an OIDC browser
// flow: OIDC start, identity link, and an administrator's source trial.
type IdentityAuthorizationStart struct {
	AuthorizationURL string `json:"authorization_url"`
}

// IdentityCompletionMode tells which identity flow a completion finished.
type IdentityCompletionMode string

const (
	IdentityCompletionModeLogin  IdentityCompletionMode = "login"
	IdentityCompletionModeLink   IdentityCompletionMode = "link"
	IdentityCompletionModeChoice IdentityCompletionMode = "choice"
	IdentityCompletionModeTest   IdentityCompletionMode = "test"
)

// IdentityLoginResult is a completed external login. ExpiresAt is Unix
// seconds.
type IdentityLoginResult struct {
	Mode      IdentityCompletionMode `json:"mode"`
	Token     string                 `json:"token"`
	ExpiresAt int64                  `json:"expires_at"`
	ReturnTo  string                 `json:"return_to"`
}

// IdentityLinkResult is an external identity linked to the signed-in account.
type IdentityLinkResult struct {
	Mode     IdentityCompletionMode `json:"mode"`
	Result   string                 `json:"result" tstype:"'linked'"`
	ReturnTo string                 `json:"return_to"`
}

// IdentityChoiceResult asks the browser whether a new external identity
// creates an account or links to an existing one. ExpiresAt is Unix seconds.
type IdentityChoiceResult struct {
	Mode        IdentityCompletionMode `json:"mode"`
	Provider    IdentityProvider       `json:"provider"`
	DisplayName string                 `json:"display_name"`
	Role        IdentityRole           `json:"role"`
	ReturnTo    string                 `json:"return_to"`
	ExpiresAt   int64                  `json:"expires_at"`
}

// IdentityTrialOutcome is the verdict of an administrator's source trial.
type IdentityTrialOutcome string

const (
	IdentityTrialOutcomeSuccess IdentityTrialOutcome = "success"
	IdentityTrialOutcomeDenied  IdentityTrialOutcome = "denied"
	IdentityTrialOutcomeFailed  IdentityTrialOutcome = "failed"
)

// IdentityTrialResult reports an administrator's trial sign-in against a draft
// source. It never creates an account or session. MappedRole is null when the
// identity maps to no role; DisplayName and Code are empty when unknown.
type IdentityTrialResult struct {
	Mode          IdentityCompletionMode `json:"mode"`
	Provider      IdentityProvider       `json:"provider"`
	Outcome       IdentityTrialOutcome   `json:"outcome"`
	Authenticated bool                   `json:"authenticated"`
	MappedRole    *IdentityRole          `json:"mapped_role" tstype:"IdentityRole | null,required"`
	DisplayName   string                 `json:"display_name"`
	Code          string                 `json:"code"`
	CheckedAt     time.Time              `json:"checked_at"`
	ReturnTo      string                 `json:"return_to"`
}

// LoginIdentity is one external identity linked to the signed-in account.
type LoginIdentity struct {
	ID              uint             `json:"id"`
	Provider        string           `json:"provider"`
	Protocol        IdentityProtocol `json:"protocol"`
	ProviderName    string           `json:"provider_name"`
	ProviderMatches bool             `json:"provider_matches"`
	LoginAvailable  bool             `json:"login_available"`
	DisplayName     string           `json:"display_name"`
	CreatedAt       time.Time        `json:"created_at"`
	LastLoginAt     *time.Time       `json:"last_login_at" tstype:"string | null,required"`
	CanUnlink       bool             `json:"can_unlink"`
}

// LoginIdentityProvider is a configured source as the account page sees it.
// SessionTTLSeconds is present only when the source limits its sessions.
type LoginIdentityProvider struct {
	ID                string           `json:"id"`
	Protocol          IdentityProtocol `json:"protocol"`
	Name              string           `json:"name"`
	Enabled           bool             `json:"enabled"`
	CanLink           bool             `json:"can_link"`
	SessionTTLSeconds *int64           `json:"session_ttl_seconds,omitempty"`
}

// LoginIdentitiesResponse answers GET /auth/identities. The current identity
// fields are present only when the session began with an external identity.
type LoginIdentitiesResponse struct {
	PermissionSource  IdentityPermissionSource `json:"permission_source"`
	PasswordEnabled   bool                     `json:"password_enabled"`
	CanLink           bool                     `json:"can_link"`
	Identities        []LoginIdentity          `json:"identities"`
	Providers         []LoginIdentityProvider  `json:"providers"`
	CurrentIdentityID uint                     `json:"current_identity_id,omitempty"`
	CurrentProviderID string                   `json:"current_provider_id,omitempty"`
}

// AuthenticationCheck answers POST /admin/authentication/check for an OIDC
// draft that passed every check.
type AuthenticationCheck struct {
	CheckedAt          time.Time `json:"checked_at"`
	ConfigurationValid bool      `json:"configuration_valid"`
	DiscoveryValid     bool      `json:"discovery_valid"`
	JWKSReadable       bool      `json:"jwks_readable"`
}

// LDAPConfigurationCheck answers POST /admin/authentication/ldap/check for an
// LDAP draft that passed every check.
type LDAPConfigurationCheck struct {
	CheckedAt          time.Time `json:"checked_at"`
	ConfigurationValid bool      `json:"configuration_valid"`
	TLSValid           bool      `json:"tls_valid"`
	BindValid          bool      `json:"bind_valid"`
	DirectoryReadable  bool      `json:"directory_readable"`
}
