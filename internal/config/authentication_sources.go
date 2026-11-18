package config

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

var authenticationSourceID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func ValidateAuthenticationSourceID(id string) error {
	if !authenticationSourceID.MatchString(id) {
		return authenticationFieldError("id", "INVALID_VALUE", "authentication source ID must be 1–32 lowercase letters, digits or hyphens and start with a letter")
	}
	return nil
}

// AuthenticationSources returns independent copies of the configured sources.
func AuthenticationSources(auth AuthConfig) map[string]AuthenticationSourceConfig {
	result := make(map[string]AuthenticationSourceConfig, len(auth.Sources))
	for id, source := range auth.Sources {
		result[id] = cloneAuthenticationSource(source)
	}
	return result
}

func ValidateSourceOIDCConfig(id string, cfg OIDCConfig) error {
	if err := ValidateAuthenticationSourceID(id); err != nil {
		return err
	}
	return validateSourceOIDCConfig(id, cfg, true)
}

func ValidateAuthenticationSources(auth AuthConfig) error {
	return validateAuthenticationSources(auth, true)
}

func validateAuthenticationSources(auth AuthConfig, complete bool) error {
	sources := AuthenticationSources(auth)
	if err := validateSourceRelationships(sources); err != nil {
		return err
	}
	for _, id := range sortedAuthenticationSourceIDs(sources) {
		source := sources[id]
		if err := ValidateAuthenticationSourceID(id); err != nil {
			return err
		}
		if source.Protocol == "ldap" {
			if err := validateLDAPConfig(id, source, complete); err != nil {
				return err
			}
			continue
		}
		if source.Protocol != "oidc" || source.LDAP != nil {
			return authenticationFieldError("protocol", "UNSUPPORTED_PROTOCOL", "authentication source protocol must be oidc or ldap with matching settings")
		}
		if err := validateSourceOIDCConfig(id, source.OIDCConfig, complete); err != nil {
			return err
		}
	}
	return nil
}

// Issuer ownership stays unique even for disabled sources: reusing an issuer
// under another ID would otherwise make existing global identity bindings ambiguous.
func validateSourceRelationships(sources map[string]AuthenticationSourceConfig) error {
	issuers := map[string]bool{}
	origin := ""
	for _, id := range sortedAuthenticationSourceIDs(sources) {
		if sources[id].Protocol == "ldap" {
			continue
		}
		cfg := sources[id].OIDCConfig
		if cfg.Issuer != "" {
			issuer := cfg.Issuer
			if issuers[issuer] {
				return authenticationFieldError("issuer", "DUPLICATE_ISSUER", "authentication sources must use distinct issuers, including disabled sources")
			}
			issuers[issuer] = true
		}
		if cfg.Enabled && cfg.RedirectURL != "" {
			callback, err := url.Parse(cfg.RedirectURL)
			if err != nil {
				return authenticationFieldError("redirect_url", "INVALID_URL", "authentication source callback URL is invalid")
			}
			next := strings.ToLower(callback.Scheme + "://" + callback.Host)
			if origin != "" && next != origin {
				return authenticationFieldError("redirect_url", "CALLBACK_ORIGIN", "enabled authentication sources must share the same callback origin")
			}
			origin = next
		}
	}
	return nil
}

func sortedAuthenticationSourceIDs(sources map[string]AuthenticationSourceConfig) []string {
	ids := make([]string, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func authenticationSourceEnvName(id, field string) string {
	return "DEPSILO_AUTH_SOURCES_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_" + strings.ToUpper(strings.ReplaceAll(field, ".", "_"))
}

// Sources are enumerated from TOML only. An arbitrary environment variable must
// not silently introduce a new source or rename an existing identity authority.
func prepareAuthenticationSources(v *viper.Viper, useEnvironment bool) error {
	for id := range v.GetStringMap("auth.sources") {
		if err := ValidateAuthenticationSourceID(id); err != nil {
			return err
		}
		prefix := "auth.sources." + id + "."
		source := newAuthenticationSource(v.GetString(prefix + "protocol"))
		defaults := oidcValues(source.OIDCConfig)
		if source.LDAP != nil {
			for field, value := range ldapValues(source.LDAP) {
				defaults["ldap."+field] = value
			}
		}
		for _, field := range sourceEnvironmentFields(source) {
			if field != "protocol" {
				v.SetDefault(prefix+field, defaults[field])
			}
			if useEnvironment {
				if err := v.BindEnv(prefix+field, authenticationSourceEnvName(id, field)); err != nil {
					return fmt.Errorf("bind authentication source environment: %w", err)
				}
			}
		}
	}
	return nil
}

// Viper normalizes map keys, while source IDs are durable binding identifiers.
// Validate the original TOML keys before decoding so casing cannot alias IDs.
func validateAuthenticationDocumentIDs(document []byte) error {
	var raw struct {
		Auth struct {
			Sources map[string]any `toml:"sources"`
		} `toml:"auth"`
	}
	if err := toml.Unmarshal(document, &raw); err != nil {
		return err
	}
	for id := range raw.Auth.Sources {
		if err := ValidateAuthenticationSourceID(id); err != nil {
			return err
		}
	}
	return nil
}
