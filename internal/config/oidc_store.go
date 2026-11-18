package config

import (
	"errors"
	"slices"
	"time"
)

// OIDCSettingsSnapshot is deliberately separate from OIDCConfig: a secret is
// write-only, including when configuration is supplied by the environment.
type OIDCSettingsSnapshot struct {
	LDAP                  *LDAPSettingsSnapshot `json:"ldap,omitempty"`
	Enabled               bool                  `json:"enabled"`
	DisplayName           string                `json:"display_name"`
	Issuer                string                `json:"issuer"`
	ClientID              string                `json:"client_id"`
	ClientSecretSet       bool                  `json:"client_secret_set"`
	RedirectURL           string                `json:"redirect_url"`
	AdminGroups           []string              `json:"admin_groups"`
	ReadonlyGroups        []string              `json:"readonly_groups"`
	GroupsClaim           string                `json:"groups_claim"`
	SessionTTL            string                `json:"session_ttl"`
	AllowInsecureLoopback bool                  `json:"allow_insecure_loopback"`
}

// OIDCSettingsPatch preserves absent fields. An explicit empty ClientSecret
// clears the file value; environment-owned fields cannot be changed here.
type OIDCSettingsPatch struct {
	LDAP                  *LDAPSettingsPatch `json:"ldap,omitempty"`
	Enabled               *bool              `json:"enabled"`
	DisplayName           *string            `json:"display_name"`
	Issuer                *string            `json:"issuer"`
	ClientID              *string            `json:"client_id"`
	ClientSecret          *string            `json:"client_secret"`
	RedirectURL           *string            `json:"redirect_url"`
	AdminGroups           *[]string          `json:"admin_groups"`
	ReadonlyGroups        *[]string          `json:"readonly_groups"`
	GroupsClaim           *string            `json:"groups_claim"`
	SessionTTL            *string            `json:"session_ttl"`
	AllowInsecureLoopback *bool              `json:"allow_insecure_loopback"`
}

type oidcEntry struct {
	field string
	value any
}

var oidcFields = []string{"enabled", "display_name", "issuer", "client_id", "client_secret", "redirect_url", "admin_groups", "readonly_groups", "groups_claim", "session_ttl", "allow_insecure_loopback"}

func (p OIDCSettingsPatch) entries() []oidcEntry {
	result := make([]oidcEntry, 0, len(oidcFields))
	if p.Enabled != nil {
		result = append(result, oidcEntry{"enabled", *p.Enabled})
	}
	if p.DisplayName != nil {
		result = append(result, oidcEntry{"display_name", *p.DisplayName})
	}
	if p.Issuer != nil {
		result = append(result, oidcEntry{"issuer", *p.Issuer})
	}
	if p.ClientID != nil {
		result = append(result, oidcEntry{"client_id", *p.ClientID})
	}
	if p.ClientSecret != nil {
		result = append(result, oidcEntry{"client_secret", *p.ClientSecret})
	}
	if p.RedirectURL != nil {
		result = append(result, oidcEntry{"redirect_url", *p.RedirectURL})
	}
	if p.AdminGroups != nil {
		result = append(result, oidcEntry{"admin_groups", append([]string{}, (*p.AdminGroups)...)})
	}
	if p.ReadonlyGroups != nil {
		result = append(result, oidcEntry{"readonly_groups", append([]string{}, (*p.ReadonlyGroups)...)})
	}
	if p.GroupsClaim != nil {
		result = append(result, oidcEntry{"groups_claim", *p.GroupsClaim})
	}
	if p.SessionTTL != nil {
		result = append(result, oidcEntry{"session_ttl", *p.SessionTTL})
	}
	if p.AllowInsecureLoopback != nil {
		result = append(result, oidcEntry{"allow_insecure_loopback", *p.AllowInsecureLoopback})
	}
	return result
}

func oidcValues(c OIDCConfig) map[string]any {
	return map[string]any{"enabled": c.Enabled, "display_name": c.DisplayName, "issuer": c.Issuer, "client_id": c.ClientID, "client_secret": c.ClientSecret, "redirect_url": c.RedirectURL, "admin_groups": append([]string{}, c.AdminGroups...), "readonly_groups": append([]string{}, c.ReadonlyGroups...), "groups_claim": c.GroupsClaim, "session_ttl": compactDuration(c.SessionTTL), "allow_insecure_loopback": c.AllowInsecureLoopback}
}

func applyOIDCEntry(c *OIDCConfig, e oidcEntry) error {
	switch e.field {
	case "enabled":
		c.Enabled = e.value.(bool)
	case "display_name":
		c.DisplayName = e.value.(string)
	case "issuer":
		c.Issuer = e.value.(string)
	case "client_id":
		c.ClientID = e.value.(string)
	case "client_secret":
		c.ClientSecret = e.value.(string)
	case "redirect_url":
		c.RedirectURL = e.value.(string)
	case "admin_groups":
		c.AdminGroups = append([]string{}, e.value.([]string)...)
	case "readonly_groups":
		c.ReadonlyGroups = append([]string{}, e.value.([]string)...)
	case "groups_claim":
		c.GroupsClaim = e.value.(string)
	case "session_ttl":
		parsed, err := time.ParseDuration(e.value.(string))
		if err != nil {
			return authenticationFieldError("session_ttl", "INVALID_VALUE", "session_ttl must be a duration between 5m and 1h")
		}
		c.SessionTTL = parsed
	case "allow_insecure_loopback":
		c.AllowInsecureLoopback = e.value.(bool)
	}
	return nil
}

func cloneOIDCConfig(c OIDCConfig) OIDCConfig {
	c.AdminGroups = slices.Clone(c.AdminGroups)
	c.ReadonlyGroups = slices.Clone(c.ReadonlyGroups)
	return c
}

func oidcSnapshot(c OIDCConfig) OIDCSettingsSnapshot {
	return OIDCSettingsSnapshot{Enabled: c.Enabled, DisplayName: c.DisplayName, Issuer: c.Issuer, ClientID: c.ClientID, ClientSecretSet: c.ClientSecret != "", RedirectURL: c.RedirectURL, AdminGroups: append([]string{}, c.AdminGroups...), ReadonlyGroups: append([]string{}, c.ReadonlyGroups...), GroupsClaim: c.GroupsClaim, SessionTTL: compactDuration(c.SessionTTL), AllowInsecureLoopback: c.AllowInsecureLoopback}
}

func oidcStoreError(code StoreErrorCode, message string) error {
	return &StoreError{Code: code, Err: errors.New(message)}
}

// Activation validates the complete post-restart configuration, including
// environment-owned credentials. Disabling remains possible for existing
// configurations: only fields explicitly changed in this request are checked.
// This prevents an unrelated old value from blocking a local recovery action.
func validateSourceSettingsUpdate(id string, next OIDCConfig, entries []oidcEntry) error {
	if next.Enabled {
		return ValidateSourceOIDCConfig(id, next)
	}
	candidate := OIDCConfig{Enabled: true, SessionTTL: 30 * time.Minute, AllowInsecureLoopback: next.AllowInsecureLoopback}
	for _, entry := range entries {
		if entry.field == "enabled" {
			continue
		}
		if entry.field == "admin_groups" || entry.field == "readonly_groups" {
			candidate.AdminGroups = append([]string{}, next.AdminGroups...)
			candidate.ReadonlyGroups = append([]string{}, next.ReadonlyGroups...)
			continue
		}
		if err := applyOIDCEntry(&candidate, entry); err != nil {
			return err
		}
	}
	return validateSourceOIDCConfig(id, candidate, false)
}
