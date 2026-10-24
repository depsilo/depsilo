package config

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// OIDCSessionConfigEqual compares authentication authority, excluding only the
// source's presentation name. Keep this boundary aligned with identity's
// session fingerprint; credentials and all policy fields remain significant.
func OIDCSessionConfigEqual(a, b OIDCConfig) bool {
	a.DisplayName, b.DisplayName = "", ""
	return reflect.DeepEqual(a, b)
}

// ValidateOIDCConfig validates the operator's exact issuer and callback URL.
// It performs no discovery or network requests: an unavailable identity
// provider must not prevent local recovery administrators from starting Depsilo.
func ValidateOIDCConfig(cfg OIDCConfig) error {
	return validateOIDCConfig(cfg, true)
}

func validateOIDCConfig(cfg OIDCConfig, requireComplete bool) error {
	return validateSourceOIDCConfig("oidc", cfg, requireComplete)
}

func validateSourceOIDCConfig(id string, cfg OIDCConfig, requireComplete bool) error {
	if !cfg.Enabled {
		return nil
	}
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"display_name", cfg.DisplayName, 80},
		{"client_id", cfg.ClientID, 255},
		{"client_secret", cfg.ClientSecret, 4096},
		{"groups_claim", cfg.GroupsClaim, 128},
	} {
		if !requireComplete && field.value == "" {
			continue
		}
		if field.value == "" || field.value != strings.TrimSpace(field.value) || len(field.value) > field.limit || strings.ContainsFunc(field.value, unicode.IsControl) {
			// Errors deliberately contain the field name, never the supplied value.
			code := "INVALID_VALUE"
			if field.value == "" {
				code = "REQUIRED"
			}
			return authenticationFieldError(field.name, code, fmt.Sprintf("auth.oidc.%s must be nonempty, at most %d bytes, and contain no control characters or surrounding whitespace", field.name, field.limit))
		}
	}
	if requireComplete || cfg.Issuer != "" {
		if err := validateOIDCURL(cfg.Issuer, false, cfg.AllowInsecureLoopback); err != nil {
			return authenticationFieldError("issuer", "INVALID_URL", fmt.Sprintf("auth.oidc.issuer %s", err))
		}
	}
	if requireComplete || cfg.RedirectURL != "" {
		if err := validateOIDCCallback(cfg.RedirectURL, id, cfg.AllowInsecureLoopback); err != nil {
			code := "INVALID_URL"
			if strings.HasPrefix(err.Error(), "must use the exact path") {
				code = "CALLBACK_PATH"
			}
			return authenticationFieldError("redirect_url", code, fmt.Sprintf("auth.oidc.redirect_url %s", err))
		}
	}
	if cfg.SessionTTL < 5*time.Minute || cfg.SessionTTL > time.Hour {
		return authenticationFieldError("session_ttl", "INVALID_VALUE", "auth.oidc.session_ttl must be between 5m and 1h")
	}
	if requireComplete && len(cfg.AdminGroups)+len(cfg.ReadonlyGroups) == 0 {
		return &AuthenticationValidationError{Fields: map[string]string{"admin_groups": "GROUPS_REQUIRED", "readonly_groups": "GROUPS_REQUIRED"}, Message: "auth.oidc requires at least one admin_groups or readonly_groups mapping"}
	}
	seen := make(map[string]bool)
	for _, group := range append(append([]string{}, cfg.AdminGroups...), cfg.ReadonlyGroups...) {
		if group == "" || group != strings.TrimSpace(group) || len(group) > 255 || strings.ContainsFunc(group, unicode.IsControl) {
			return &AuthenticationValidationError{Fields: map[string]string{"admin_groups": "INVALID_VALUE", "readonly_groups": "INVALID_VALUE"}, Message: "auth.oidc group mappings must be nonempty, at most 255 bytes, and contain no control characters or surrounding whitespace"}
		}
		if seen[group] {
			return &AuthenticationValidationError{Fields: map[string]string{"admin_groups": "GROUPS_OVERLAP", "readonly_groups": "GROUPS_OVERLAP"}, Message: "auth.oidc admin_groups and readonly_groups must be disjoint and contain no duplicates"}
		}
		seen[group] = true
	}
	return nil
}

func validateOIDCURL(raw string, callback, allowInsecureLoopback bool) error {
	u, err := url.Parse(raw)
	if err != nil || raw == "" || len(raw) > 1024 || raw != strings.TrimSpace(raw) || strings.ContainsFunc(raw, unicode.IsSpace) ||
		u.Scheme != "https" && u.Scheme != "http" || u.Hostname() == "" || u.Opaque != "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || u.RawPath != "" || u.String() != raw {
		return errors.New("must be an absolute canonical HTTP(S) URL without credentials, query, fragment, or escaped path")
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return errors.New("must use a valid port")
		}
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return errors.New("must not contain dot path segments")
		}
	}
	if u.Scheme != "https" && (!allowInsecureLoopback || !isLoopbackHost(u.Hostname())) {
		return errors.New("must use HTTPS; HTTP requires allow_insecure_loopback=true and a loopback host")
	}
	if callback && u.Path != "/api/v1/auth/oidc/callback" {
		return errors.New("must use the exact path /api/v1/auth/oidc/callback")
	}
	return nil
}

func validateOIDCCallback(raw, id string, allowInsecureLoopback bool) error {
	if err := validateOIDCURL(raw, false, allowInsecureLoopback); err != nil {
		return err
	}
	parsed, _ := url.Parse(raw)
	expected := "/api/v1/auth/oidc/callback"
	if id != "oidc" {
		expected = "/api/v1/auth/oidc/" + id + "/callback"
	}
	if parsed.Path != expected {
		return fmt.Errorf("must use the exact path %s", expected)
	}
	return nil
}
