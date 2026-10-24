package config

import (
	"context"
	"io/fs"
	"reflect"

	"go.uber.org/zap"
)

type AuthenticationSourceState struct {
	ID                string                `json:"id"`
	Protocol          string                `json:"protocol"`
	Legacy            bool                  `json:"legacy"`
	Configured        *OIDCSettingsSnapshot `json:"configured"`
	Effective         *OIDCSettingsSnapshot `json:"effective"`
	Overrides         map[string]string     `json:"overrides"`
	PendingRestart    bool                  `json:"pending_restart"`
	SessionsWillReset bool                  `json:"sessions_will_reset"`
	ConfigWritable    bool                  `json:"config_writable"`
	CanRestore        bool                  `json:"can_restore"`
}

type AuthenticationSourcesState struct {
	Sources        []AuthenticationSourceState `json:"sources"`
	ConfigWritable bool                        `json:"config_writable"`
}

type AuthenticationSourceCreate struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	OIDCSettingsPatch
}

func (s *Store) configuredSources(auth AuthConfig) map[string]AuthenticationSourceConfig {
	// Legacy environment-only deployments remain editable without pretending the
	// effective environment credentials were persisted in the file.
	if len(s.oidcOverrides) > 0 {
		auth.OIDCConfigured = true
	}
	return AuthenticationSources(auth)
}

func (s *Store) sourcesAfterRestart(configured map[string]AuthenticationSourceConfig) map[string]AuthenticationSourceConfig {
	next := make(map[string]AuthenticationSourceConfig, len(configured))
	for id, source := range configured {
		source = cloneAuthenticationSource(source)
		values := oidcValues(s.effectiveSources[id].OIDCConfig)
		for field := range s.sourceOverrides[id] {
			if field == "protocol" {
				source.Protocol = s.effectiveSources[id].Protocol
				continue
			}
			if len(field) > 5 && field[:5] == "ldap." {
				if source.LDAP == nil {
					source.LDAP = &LDAPConfig{}
				}
				applyLDAPEntry(source.LDAP, oidcEntry{field, ldapValues(s.effectiveSources[id].LDAP)[field[5:]]})
				continue
			}
			_ = applyOIDCEntry(&source.OIDCConfig, oidcEntry{field, values[field]})
		}
		next[id] = source
	}
	return next
}

func authenticationDocumentWritable(document []byte, sources map[string]AuthenticationSourceConfig) bool {
	// A probe also detects inline [auth] / [auth.sources] parents when no actual
	// sources exist. Every source must use an explicit table for this editor.
	if _, err := patchAuthenticationDocument(document, "new-source", nil, false); err != nil {
		return false
	}
	for id := range sources {
		if _, err := patchAuthenticationDocument(document, id, nil, false); err != nil {
			return false
		}
	}
	return true
}

func (s *Store) sourceState(id string, configured map[string]AuthenticationSourceConfig, document []byte) AuthenticationSourceState {
	state := AuthenticationSourceState{ID: id, Protocol: "oidc", Legacy: id == "oidc", Overrides: map[string]string{}, ConfigWritable: configWritable(s.path) && authenticationDocumentWritable(document, configured)}
	source, exists := configured[id]
	if exists {
		snapshot := authenticationSourceSnapshot(source)
		state.Configured = &snapshot
		state.Protocol = source.Protocol
	}
	effective, active := s.effectiveSources[id]
	if active {
		snapshot := authenticationSourceSnapshot(effective)
		state.Effective = &snapshot
	}
	for field, name := range s.sourceOverrides[id] {
		state.Overrides[field] = name
	}
	next := s.sourcesAfterRestart(configured)
	state.PendingRestart = exists != active || exists && (source.Protocol != effective.Protocol || !reflect.DeepEqual(authenticationSourceValues(next[id]), authenticationSourceValues(effective)))
	state.SessionsWillReset = active && effective.Enabled && (!exists || !authenticationSourceSessionConfigEqual(next[id], effective))
	_, baseline := s.startupSources[id]
	state.CanRestore = !exists && active && baseline && state.ConfigWritable
	return state
}

func (s *Store) AuthenticationSourceSettings(ctx context.Context) (AuthenticationSourcesState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AuthenticationSourcesState{}, err
	}
	cfg, document, _, err := s.readConfigDocument()
	if err != nil {
		return AuthenticationSourcesState{}, oidcStoreError(StoreConfigReadFailed, "cannot read authentication configuration")
	}
	configured := s.configuredSources(cfg.Auth)
	merged := make(map[string]AuthenticationSourceConfig, len(configured)+len(s.effectiveSources))
	for id, source := range configured {
		merged[id] = source
	}
	for id, source := range s.effectiveSources {
		merged[id] = source
	}
	state := AuthenticationSourcesState{Sources: []AuthenticationSourceState{}, ConfigWritable: configWritable(s.path) && authenticationDocumentWritable(document, configured)}
	for _, id := range sortedAuthenticationSourceIDs(merged) {
		state.Sources = append(state.Sources, s.sourceState(id, configured, document))
	}
	return state, nil
}

func (s *Store) CreateAuthenticationSource(ctx context.Context, create AuthenticationSourceCreate) (AuthenticationSourceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AuthenticationSourceState{}, err
	}
	if err := ValidateAuthenticationSourceID(create.ID); err != nil {
		return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if err := validateNewAuthenticationSource(create.ID, create.Protocol); err != nil {
		return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	cfg, document, mode, err := s.readConfigDocument()
	if err != nil {
		return AuthenticationSourceState{}, oidcStoreError(StoreConfigReadFailed, "cannot read authentication configuration")
	}
	configured := s.configuredSources(cfg.Auth)
	if _, exists := configured[create.ID]; exists {
		return AuthenticationSourceState{}, &StoreError{Code: StoreAuthenticationSourceConflict, Err: authenticationFieldError("id", "SOURCE_EXISTS", "authentication source already exists")}
	}
	if _, exists := s.effectiveSources[create.ID]; exists {
		return AuthenticationSourceState{}, oidcStoreError(StoreAuthenticationSourceConflict, "authentication source is pending removal; restart before reusing its ID")
	}
	configured[create.ID] = newAuthenticationSource(create.Protocol)
	return s.writeAuthenticationSource(create.ID, configured, document, mode, create.OIDCSettingsPatch, true)
}

func (s *Store) UpdateAuthenticationSource(ctx context.Context, id string, patch OIDCSettingsPatch) (AuthenticationSourceState, error) {
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
	if _, exists := configured[id]; !exists {
		return AuthenticationSourceState{}, oidcStoreError(StoreAuthenticationSourceNotFound, "authentication source is not configured")
	}
	return s.writeAuthenticationSource(id, configured, document, mode, patch, false)
}

// Caller holds Store.mu. Reads, validation, and atomic replacement share the
// same lock as general settings so concurrent editors cannot lose updates.
func (s *Store) writeAuthenticationSource(id string, configured map[string]AuthenticationSourceConfig, document []byte, mode fs.FileMode, patch OIDCSettingsPatch, create bool) (AuthenticationSourceState, error) {
	entries, err := s.sourcePatchEntries(configured[id], id, patch)
	if err != nil {
		return AuthenticationSourceState{}, err
	}
	if !create && len(entries) == 0 {
		return AuthenticationSourceState{}, oidcStoreError(StoreInvalidSetting, "authentication patch is empty")
	}
	if !authenticationDocumentWritable(document, configured) || !configWritable(s.path) {
		return AuthenticationSourceState{}, oidcStoreError(StoreConfigReadOnly, "configuration is read-only; source editing requires explicit authentication source tables")
	}
	next := configured[id]
	for _, entry := range entries {
		if s.sourceOverrides[id][entry.field] != "" {
			return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError(entry.field, "ENV_OVERRIDE", "authentication field is controlled by an environment override")}
		}
		if err := applyAuthenticationSourceEntry(&next, entry); err != nil {
			return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
		}
	}
	configured[id] = next
	after := s.sourcesAfterRestart(configured)
	if err := validateAuthenticationSourceUpdate(id, after[id], entries); err != nil {
		return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if err := validateSourceRelationships(after); err != nil {
		return AuthenticationSourceState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if create {
		entries = append([]oidcEntry{{"protocol", next.Protocol}}, entries...)
	}
	updated, err := patchAuthenticationDocument(document, id, entries, false)
	if err != nil {
		return AuthenticationSourceState{}, oidcStoreError(StoreConfigReadOnly, "cannot edit this authentication document shape")
	}
	if _, err := decodeConfigDocument(updated); err != nil {
		return AuthenticationSourceState{}, oidcStoreError(StoreInvalidSetting, "authentication update would produce an invalid configuration")
	}
	if patch.ClientSecret != nil && *patch.ClientSecret != "" || next.LDAP != nil && next.LDAP.BindPassword != "" {
		mode &= 0o600
	}
	if err := s.writeAuthenticationDocument(updated, mode); err != nil {
		return AuthenticationSourceState{}, err
	}
	return s.sourceState(id, configured, updated), nil
}

func (s *Store) DeleteAuthenticationSource(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateAuthenticationSourceID(id); err != nil {
		return &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	cfg, document, mode, err := s.readConfigDocument()
	if err != nil {
		return oidcStoreError(StoreConfigReadFailed, "cannot read authentication configuration")
	}
	configured := s.configuredSources(cfg.Auth)
	if _, exists := configured[id]; !exists {
		return oidcStoreError(StoreAuthenticationSourceNotFound, "authentication source is not configured")
	}
	if len(s.sourceOverrides[id]) > 0 {
		return oidcStoreError(StoreConfigReadOnly, "remove source environment overrides before deleting this authentication source")
	}
	if !authenticationDocumentWritable(document, configured) || !configWritable(s.path) {
		return oidcStoreError(StoreConfigReadOnly, "configuration is read-only; source editing requires explicit authentication source tables")
	}
	updated, err := patchAuthenticationDocument(document, id, nil, true)
	if err != nil {
		return oidcStoreError(StoreConfigReadOnly, "cannot edit this authentication document shape")
	}
	if _, err := decodeConfigDocument(updated); err != nil {
		return oidcStoreError(StoreInvalidSetting, "source removal would produce an invalid configuration")
	}
	return s.writeAuthenticationDocument(updated, mode)
}

func (s *Store) writeAuthenticationDocument(document []byte, mode fs.FileMode) error {
	outcome, err := s.writer.Write(s.path, document, mode)
	if !outcome.committed {
		return oidcStoreError(StoreConfigWriteFailed, "cannot save authentication configuration")
	}
	if err != nil || outcome.durabilityErr != nil {
		zap.L().Warn("authentication config rename committed but durability could not be confirmed")
	}
	return nil
}
