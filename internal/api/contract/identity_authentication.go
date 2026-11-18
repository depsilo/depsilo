package contract

import "time"

// LDAPDirectorySettings is the directory half of an LDAP source. The bind
// password is never returned; BindPasswordSet says whether one is stored.
type LDAPDirectorySettings struct {
	URL                  string `json:"url"`
	BindDN               string `json:"bind_dn"`
	BindPasswordSet      bool   `json:"bind_password_set"`
	UserBaseDN           string `json:"user_base_dn"`
	UserFilter           string `json:"user_filter"`
	SubjectAttribute     string `json:"subject_attribute"`
	DisplayNameAttribute string `json:"display_name_attribute"`
	GroupsAttribute      string `json:"groups_attribute"`
	CACertPEM            string `json:"ca_cert_pem"`
}

// AuthenticationSourceSettings is one authentication source's settings. Every
// source carries the OIDC fields (empty for LDAP); LDAP is present exactly for
// an LDAP source. The client secret is never returned.
type AuthenticationSourceSettings struct {
	LDAP                  *LDAPDirectorySettings `json:"ldap,omitempty"`
	Enabled               bool                   `json:"enabled"`
	DisplayName           string                 `json:"display_name"`
	Issuer                string                 `json:"issuer"`
	ClientID              string                 `json:"client_id"`
	ClientSecretSet       bool                   `json:"client_secret_set"`
	RedirectURL           string                 `json:"redirect_url"`
	AdminGroups           []string               `json:"admin_groups"`
	ReadonlyGroups        []string               `json:"readonly_groups"`
	GroupsClaim           string                 `json:"groups_claim"`
	SessionTTL            string                 `json:"session_ttl"`
	AllowInsecureLoopback bool                   `json:"allow_insecure_loopback"`
}

// AuthenticationSource is one source's configured and running state, answered
// by source create, update, and restore. Configured is null when the source is
// not in the configuration file; Effective is null when it is not running.
// Overrides maps an environment-owned field to its variable name.
type AuthenticationSource struct {
	ID                string                        `json:"id"`
	Protocol          IdentityProtocol              `json:"protocol"`
	Configured        *AuthenticationSourceSettings `json:"configured" tstype:"AuthenticationSourceSettings | null,required"`
	Effective         *AuthenticationSourceSettings `json:"effective" tstype:"AuthenticationSourceSettings | null,required"`
	Overrides         map[string]string             `json:"overrides"`
	PendingRestart    bool                          `json:"pending_restart"`
	SessionsWillReset bool                          `json:"sessions_will_reset"`
	ConfigWritable    bool                          `json:"config_writable"`
	CanRestore        bool                          `json:"can_restore"`
}

// AuthenticationSourceList answers GET /admin/authentication/sources.
type AuthenticationSourceList struct {
	Sources        []AuthenticationSource `json:"sources"`
	ConfigWritable bool                   `json:"config_writable"`
}

// AuthenticationImpactUser is an account bound to a source under review.
type AuthenticationImpactUser struct {
	ID               uint                     `json:"id"`
	Username         string                   `json:"username"`
	PermissionSource IdentityPermissionSource `json:"permission_source"`
	Enabled          bool                     `json:"enabled"`
}

// AuthenticationImpact answers GET /admin/authentication/sources/:id/impact:
// what removing or changing a source would affect. Users lists at most the
// first accounts; UsersTruncated says whether more are bound.
type AuthenticationImpact struct {
	SourceID                  string                     `json:"source_id"`
	BoundUserCount            int64                      `json:"bound_user_count"`
	DependentManagedUserCount int64                      `json:"dependent_managed_user_count"`
	ActiveSessionCount        int64                      `json:"active_session_count"`
	ObservedAt                time.Time                  `json:"observed_at"`
	Users                     []AuthenticationImpactUser `json:"users"`
	UsersTruncated            bool                       `json:"users_truncated"`
}
