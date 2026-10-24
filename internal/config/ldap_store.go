package config

import "context"

func (s *Store) sourcePatchEntries(source AuthenticationSourceConfig, id string, patch OIDCSettingsPatch) ([]oidcEntry, error) {
	entries := patch.entries()
	if source.Protocol != "ldap" {
		if patch.LDAP != nil {
			return nil, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError("ldap", "INVALID_VALUE", "LDAP settings require an LDAP source")}
		}
		return entries, nil
	}
	for _, entry := range entries {
		switch entry.field {
		case "enabled", "display_name", "admin_groups", "readonly_groups", "session_ttl":
		default:
			return nil, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError(entry.field, "INVALID_VALUE", "OIDC setting cannot be applied to an LDAP source")}
		}
	}
	if patch.LDAP != nil {
		candidate := cloneAuthenticationSource(source)
		if candidate.LDAP == nil {
			candidate.LDAP = defaultLDAPConfig()
		}
		for _, entry := range patch.LDAP.entries() {
			if s.sourceOverrides[id][entry.field] != "" {
				return nil, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError(entry.field, "ENV_OVERRIDE", "LDAP setting is controlled by environment")}
			}
			applyLDAPEntry(candidate.LDAP, entry)
		}
		entries = append(entries, oidcEntry{"ldap", ldapValues(candidate.LDAP)})
	}
	return entries, nil
}

func applyAuthenticationSourceEntry(source *AuthenticationSourceConfig, entry oidcEntry) error {
	if entry.field == "ldap" {
		source.LDAP = &LDAPConfig{}
		for field, value := range entry.value.(map[string]string) {
			applyLDAPEntry(source.LDAP, oidcEntry{field, value})
		}
		return nil
	}
	return applyOIDCEntry(&source.OIDCConfig, entry)
}

func validateAuthenticationSourceConfig(id string, source AuthenticationSourceConfig) error {
	if source.Protocol == "ldap" {
		return ValidateSourceLDAPConfig(id, source)
	}
	return ValidateSourceOIDCConfig(id, source.OIDCConfig)
}

func validateAuthenticationSourceUpdate(id string, source AuthenticationSourceConfig, entries []oidcEntry) error {
	if source.Protocol == "ldap" {
		return ValidateSourceLDAPConfig(id, source)
	}
	return validateSourceSettingsUpdate(id, source.OIDCConfig, entries)
}

// PreviewLDAPAuthenticationSourceDraft merges a private candidate without
// persisting it. Environment-owned values are applied from the startup snapshot.
func (s *Store) PreviewLDAPAuthenticationSourceDraft(ctx context.Context, draft AuthenticationSourceDraft) (AuthenticationSourceConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AuthenticationSourceConfig{}, err
	}
	if err := ValidateAuthenticationSourceID(draft.ID); err != nil {
		return AuthenticationSourceConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if draft.Protocol != "ldap" {
		return AuthenticationSourceConfig{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError("protocol", "UNSUPPORTED_PROTOCOL", "LDAP preview requires LDAP protocol")}
	}
	cfg, _, _, err := s.readConfigDocument()
	if err != nil {
		return AuthenticationSourceConfig{}, oidcStoreError(StoreConfigReadFailed, "cannot read authentication configuration")
	}
	configured := s.configuredSources(cfg.Auth)
	source, exists := configured[draft.ID]
	if draft.Create {
		if err := validateNewAuthenticationSource(draft.ID, draft.Protocol); err != nil {
			return AuthenticationSourceConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
		}
		if exists {
			return AuthenticationSourceConfig{}, &StoreError{Code: StoreAuthenticationSourceConflict, Err: authenticationFieldError("id", "SOURCE_EXISTS", "authentication source already exists")}
		}
		if _, running := s.effectiveSources[draft.ID]; running {
			return AuthenticationSourceConfig{}, oidcStoreError(StoreAuthenticationSourceConflict, "authentication source is pending removal")
		}
		source = newAuthenticationSource("ldap")
	} else if !exists {
		return AuthenticationSourceConfig{}, oidcStoreError(StoreAuthenticationSourceNotFound, "authentication source is not configured")
	}
	if source.Protocol != "ldap" {
		return AuthenticationSourceConfig{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError("protocol", "INVALID_VALUE", "authentication source protocol cannot change")}
	}
	entries, err := s.sourcePatchEntries(source, draft.ID, draft.Settings)
	if err != nil {
		return AuthenticationSourceConfig{}, err
	}
	for _, entry := range entries {
		if s.sourceOverrides[draft.ID][entry.field] != "" {
			return AuthenticationSourceConfig{}, &StoreError{Code: StoreInvalidSetting, Err: authenticationFieldError(entry.field, "ENV_OVERRIDE", "authentication setting is controlled by environment")}
		}
		if err := applyAuthenticationSourceEntry(&source, entry); err != nil {
			return AuthenticationSourceConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
		}
	}
	configured[draft.ID] = source
	source = s.sourcesAfterRestart(configured)[draft.ID]
	source.Enabled = true
	if err := ValidateSourceLDAPConfig(draft.ID, source); err != nil {
		return AuthenticationSourceConfig{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	return cloneAuthenticationSource(source), nil
}
