package config

import (
	"context"
	"os"
	"reflect"
	"time"
)

// AuthenticationSourceDraft is an in-memory candidate. Omitted fields preserve
// saved values; secrets never become part of the public snapshot.
type AuthenticationSourceDraft struct {
	ID       string            `json:"id"`
	Protocol string            `json:"protocol"`
	Create   bool              `json:"create"`
	Settings OIDCSettingsPatch `json:"settings"`
}

func validateNewAuthenticationSource(id, protocol string) error {
	if err := ValidateAuthenticationSourceID(id); err != nil {
		return err
	}
	if id == "oidc" {
		return authenticationFieldError("id", "RESERVED_ID", "source ID oidc is reserved for legacy configuration")
	}
	if protocol != "oidc" && protocol != "ldap" {
		return authenticationFieldError("protocol", "UNSUPPORTED_PROTOCOL", "authentication source protocol must be oidc or ldap")
	}
	return nil
}

// PreviewAuthenticationSourceDraft validates the configuration a network check
// or isolated trial would use. It never writes the document or live registry.
// Tests exercise the enabled configuration even when the saved source is off.
func (s *Store) PreviewAuthenticationSourceDraft(ctx context.Context, draft AuthenticationSourceDraft) (OIDCConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return OIDCConfig{}, err
	}
	if err := ValidateAuthenticationSourceID(draft.ID); err != nil {
		return OIDCConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if draft.Protocol != "oidc" {
		return OIDCConfig{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError("protocol", "UNSUPPORTED_PROTOCOL", "authentication source protocol must be oidc")}
	}
	cfg, _, _, err := s.readConfigDocument()
	if err != nil {
		return OIDCConfig{}, oidcStoreError(StoreConfigReadFailed, "cannot read authentication configuration")
	}
	configured := s.configuredSources(cfg.Auth)
	source, exists := configured[draft.ID]
	if exists && source.Protocol != "oidc" || draft.Settings.LDAP != nil {
		return OIDCConfig{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError("protocol", "INVALID_VALUE", "OIDC preview requires an OIDC source")}
	}
	if draft.Create {
		if err := validateNewAuthenticationSource(draft.ID, draft.Protocol); err != nil {
			return OIDCConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
		}
		if exists {
			return OIDCConfig{}, &StoreError{Code: StoreAuthenticationSourceConflict, Err: authenticationFieldError("id", "SOURCE_EXISTS", "authentication source already exists")}
		}
		if _, running := s.effectiveSources[draft.ID]; running {
			return OIDCConfig{}, oidcStoreError(StoreAuthenticationSourceConflict, "authentication source is pending removal")
		}
		source = AuthenticationSourceConfig{Protocol: "oidc", OIDCConfig: OIDCConfig{GroupsClaim: "groups", SessionTTL: 30 * time.Minute}}
	} else if !exists {
		return OIDCConfig{}, oidcStoreError(StoreAuthenticationSourceNotFound, "authentication source is not configured")
	}
	for _, entry := range draft.Settings.entries() {
		if s.sourceOverrides[draft.ID][entry.field] != "" {
			return OIDCConfig{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError(entry.field, "ENV_OVERRIDE", "authentication field is controlled by an environment override")}
		}
		if err := applyOIDCEntry(&source.OIDCConfig, entry); err != nil {
			return OIDCConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
		}
	}
	configured[draft.ID] = source
	after := s.sourcesAfterRestart(configured)
	source = after[draft.ID]
	source.Enabled = true
	after[draft.ID] = source
	if err := ValidateSourceOIDCConfig(draft.ID, source.OIDCConfig); err != nil {
		return OIDCConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if err := validateSourceRelationships(after); err != nil {
		return OIDCConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	return cloneOIDCConfig(source.OIDCConfig), nil
}

// Capture only a file snapshot whose effective values match the running
// configuration. This prevents restore from materializing environment values
// or treating an independently edited file as the original running document.
func (s *Store) captureAuthenticationSourceBaseline() {
	s.startupSources = make(map[string]AuthenticationSourceConfig)
	s.startupSourceFields = make(map[string]map[string]bool)
	document, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	cfg, err := decodeConfigDocument(document)
	if err != nil {
		return
	}
	configured := AuthenticationSources(cfg.Auth)
	after := s.sourcesAfterRestart(configured)
	for id, source := range configured {
		effective, exists := s.effectiveSources[id]
		if exists && effective.Protocol == after[id].Protocol && reflect.DeepEqual(authenticationSourceValues(effective), authenticationSourceValues(after[id])) {
			fields, err := authenticationSourceExplicitFields(document, id)
			if err != nil {
				continue
			}
			s.startupSources[id] = source
			s.startupSourceFields[id] = fields
		}
	}
}

func (s *Store) RestoreAuthenticationSource(ctx context.Context, id string) (AuthenticationSourceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AuthenticationSourceState{}, err
	}
	if err := ValidateAuthenticationSourceID(id); err != nil {
		return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	cfg, document, mode, err := s.readConfigDocument()
	if err != nil {
		return AuthenticationSourceState{}, oidcStoreError(StoreConfigReadFailed, "cannot read authentication configuration")
	}
	configured := s.configuredSources(cfg.Auth)
	if _, exists := configured[id]; exists {
		return AuthenticationSourceState{}, oidcStoreError(StoreAuthenticationSourceConflict, "authentication source is already configured")
	}
	source, baseline := s.startupSources[id]
	if _, running := s.effectiveSources[id]; !running || !baseline {
		return AuthenticationSourceState{}, oidcStoreError(StoreAuthenticationSourceConflict, "no saved configuration is available to restore this source")
	}
	if !authenticationDocumentWritable(document, configured) || !configWritable(s.path) {
		return AuthenticationSourceState{}, oidcStoreError(StoreConfigReadOnly, "configuration file is read-only")
	}
	configured[id] = source
	after := s.sourcesAfterRestart(configured)
	if err := validateAuthenticationSourceConfig(id, after[id]); err != nil {
		return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if err := validateSourceRelationships(after); err != nil {
		return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	entries := make([]oidcEntry, 0, len(oidcFields)+1)
	if id != "oidc" {
		entries = append(entries, oidcEntry{"protocol", source.Protocol})
	}
	values := authenticationSourceValues(source)
	for _, field := range oidcFields {
		if s.startupSourceFields[id][field] {
			entries = append(entries, oidcEntry{field, values[field]})
		}
	}
	if s.startupSourceFields[id]["ldap"] && source.LDAP != nil {
		entries = append(entries, oidcEntry{"ldap", ldapValues(source.LDAP)})
	}
	if len(entries) == 0 {
		entries = append(entries, oidcEntry{"enabled", false})
	}
	updated, err := patchAuthenticationDocument(document, id, entries, false)
	if err != nil {
		return AuthenticationSourceState{}, oidcStoreError(StoreConfigReadOnly, "cannot restore this authentication document shape")
	}
	if _, err := decodeConfigDocument(updated); err != nil {
		return AuthenticationSourceState{}, oidcStoreError(StoreInvalidSetting, "restored authentication configuration is invalid")
	}
	if source.ClientSecret != "" || source.LDAP != nil && source.LDAP.BindPassword != "" {
		mode &= 0o600
	}
	if err := s.writeAuthenticationDocument(updated, mode); err != nil {
		return AuthenticationSourceState{}, err
	}
	return s.sourceState(id, configured, updated), nil
}
