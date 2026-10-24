package config

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"
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

type OIDCSettingsState struct {
	Configured     OIDCSettingsSnapshot `json:"configured"`
	Effective      OIDCSettingsSnapshot `json:"effective"`
	Overrides      map[string]string    `json:"overrides"`
	PendingRestart bool                 `json:"pending_restart"`
	ConfigWritable bool                 `json:"config_writable"`
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

func oidcEnvName(field string) string { return "DEPSILO_AUTH_OIDC_" + strings.ToUpper(field) }

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
			return authenticationFieldError("session_ttl", "INVALID_VALUE", "auth.oidc.session_ttl must be a duration between 5m and 1h")
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

// oidcAfterRestart overlays the values captured when this process started.
// Reading the process environment again would claim a runtime authority change
// that has not occurred, and could accidentally materialize a secret in TOML.
func (s *Store) oidcAfterRestart(configured OIDCConfig) OIDCConfig {
	next := cloneOIDCConfig(configured)
	values := oidcValues(s.effectiveOIDC)
	for field := range s.oidcOverrides {
		_ = applyOIDCEntry(&next, oidcEntry{field, values[field]})
	}
	return next
}

func (s *Store) oidcState(configured OIDCConfig, document []byte) OIDCSettingsState {
	_, shapeErr := patchOIDCDocument(document, nil)
	overrides := make(map[string]string, len(s.oidcOverrides))
	for key, value := range s.oidcOverrides {
		overrides[key] = value
	}
	return OIDCSettingsState{Configured: oidcSnapshot(configured), Effective: oidcSnapshot(s.effectiveOIDC), Overrides: overrides, PendingRestart: !reflect.DeepEqual(oidcValues(s.oidcAfterRestart(configured)), oidcValues(s.effectiveOIDC)), ConfigWritable: shapeErr == nil && configWritable(s.path)}
}

func (s *Store) OIDCSettings(ctx context.Context) (OIDCSettingsState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return OIDCSettingsState{}, err
	}
	cfg, document, _, err := s.readConfigDocument()
	if err != nil {
		return OIDCSettingsState{}, oidcStoreError(StoreConfigReadFailed, "cannot read authentication configuration")
	}
	return s.oidcState(cfg.Auth.OIDC, document), nil
}

// UpdateOIDCSettings shares the configuration lock and atomic writer with the
// ordinary settings/Cascade owners. It never mutates the live identity manager.
func (s *Store) UpdateOIDCSettings(ctx context.Context, patch OIDCSettingsPatch) (OIDCSettingsState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return OIDCSettingsState{}, err
	}
	if patch.LDAP != nil {
		return OIDCSettingsState{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError("ldap", "INVALID_VALUE", "LDAP settings require an LDAP source")}
	}
	entries := patch.entries()
	if len(entries) == 0 {
		return OIDCSettingsState{}, oidcStoreError(StoreInvalidSetting, "authentication patch is empty")
	}
	cfg, document, mode, err := s.readConfigDocument()
	if err != nil {
		return OIDCSettingsState{}, oidcStoreError(StoreConfigReadFailed, "cannot read authentication configuration")
	}
	if _, configured := s.configuredSources(cfg.Auth)["oidc"]; !configured {
		if _, effective := s.effectiveSources["oidc"]; effective {
			return OIDCSettingsState{}, oidcStoreError(StoreAuthenticationSourceConflict, "authentication source is pending removal; restart before reusing its ID")
		}
	}
	if _, err := patchOIDCDocument(document, nil); err != nil {
		return OIDCSettingsState{}, oidcStoreError(StoreConfigReadOnly, "convert authentication configuration to an explicit [auth.oidc] table before editing it here")
	}
	before := cloneOIDCConfig(cfg.Auth.OIDC)
	for _, entry := range entries {
		if env := s.oidcOverrides[entry.field]; env != "" {
			return OIDCSettingsState{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError(entry.field, "ENV_OVERRIDE", fmt.Sprintf("auth.oidc.%s is controlled by %s", entry.field, env))}
		}
		if err := applyOIDCEntry(&cfg.Auth.OIDC, entry); err != nil {
			return OIDCSettingsState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
		}
	}
	if err := validateOIDCSettingsUpdate(s.oidcAfterRestart(cfg.Auth.OIDC), entries); err != nil {
		return OIDCSettingsState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	cfg.Auth.OIDCConfigured = true
	if err := validateSourceRelationships(s.sourcesAfterRestart(s.configuredSources(cfg.Auth))); err != nil {
		return OIDCSettingsState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if reflect.DeepEqual(oidcValues(before), oidcValues(cfg.Auth.OIDC)) {
		return s.oidcState(cfg.Auth.OIDC, document), nil
	}
	updated, err := patchOIDCDocument(document, entries)
	if err != nil {
		return OIDCSettingsState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if _, err := decodeConfigDocument(updated); err != nil {
		return OIDCSettingsState{}, oidcStoreError(StoreInvalidSetting, "authentication update would produce an invalid configuration")
	}
	if !configWritable(s.path) {
		return OIDCSettingsState{}, oidcStoreError(StoreConfigReadOnly, "configuration file is read-only")
	}
	if patch.ClientSecret != nil && *patch.ClientSecret != "" {
		mode &= 0o600
	}
	outcome, err := s.writer.Write(s.path, updated, mode)
	if !outcome.committed {
		return OIDCSettingsState{}, oidcStoreError(StoreConfigWriteFailed, "cannot save authentication configuration")
	}
	// A successful rename remains committed even if the subsequent directory
	// sync reports an error, matching the existing settings write contract.
	if err != nil || outcome.durabilityErr != nil {
		zap.L().Warn("authentication config rename committed but durability could not be confirmed")
	}
	return s.oidcState(cfg.Auth.OIDC, updated), nil
}

func oidcStoreError(code StoreErrorCode, message string) error {
	return &StoreError{Code: code, Err: errors.New(message)}
}

// Activation validates the complete post-restart configuration, including
// environment-owned credentials. Disabling remains possible for legacy
// configurations: only fields explicitly changed in this request are checked.
// This prevents an unrelated old value from blocking a local recovery action.
func validateOIDCSettingsUpdate(next OIDCConfig, entries []oidcEntry) error {
	return validateSourceSettingsUpdate("oidc", next, entries)
}

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
	return sourceValidationError(id, validateSourceOIDCConfig(id, candidate, false))
}
