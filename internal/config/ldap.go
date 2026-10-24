package config

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-ldap/ldap/v3"
)

// LDAP is an explicit directory protocol. Plain ldap URLs always use StartTLS;
// there is deliberately no insecure-bind or skip-certificate-verification flag.
type LDAPConfig struct {
	URL                  string `mapstructure:"url" json:"url"`
	BindDN               string `mapstructure:"bind_dn" json:"bind_dn"`
	BindPassword         string `mapstructure:"bind_password" json:"-"`
	UserBaseDN           string `mapstructure:"user_base_dn" json:"user_base_dn"`
	UserFilter           string `mapstructure:"user_filter" json:"user_filter"`
	SubjectAttribute     string `mapstructure:"subject_attribute" json:"subject_attribute"`
	DisplayNameAttribute string `mapstructure:"display_name_attribute" json:"display_name_attribute"`
	GroupsAttribute      string `mapstructure:"groups_attribute" json:"groups_attribute"`
	CACertPEM            string `mapstructure:"ca_cert_pem" json:"ca_cert_pem"`
}

type LDAPSettingsSnapshot struct {
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

type LDAPSettingsPatch struct {
	URL                  *string `json:"url"`
	BindDN               *string `json:"bind_dn"`
	BindPassword         *string `json:"bind_password"`
	UserBaseDN           *string `json:"user_base_dn"`
	UserFilter           *string `json:"user_filter"`
	SubjectAttribute     *string `json:"subject_attribute"`
	DisplayNameAttribute *string `json:"display_name_attribute"`
	GroupsAttribute      *string `json:"groups_attribute"`
	CACertPEM            *string `json:"ca_cert_pem"`
}

var ldapFields = []string{"url", "bind_dn", "bind_password", "user_base_dn", "user_filter", "subject_attribute", "display_name_attribute", "groups_attribute", "ca_cert_pem"}

func defaultLDAPConfig() *LDAPConfig {
	return &LDAPConfig{UserFilter: "(uid={username})", SubjectAttribute: "entryUUID", DisplayNameAttribute: "displayName", GroupsAttribute: "memberOf"}
}

func (p LDAPSettingsPatch) entries() []oidcEntry {
	values := map[string]*string{"url": p.URL, "bind_dn": p.BindDN, "bind_password": p.BindPassword, "user_base_dn": p.UserBaseDN, "user_filter": p.UserFilter, "subject_attribute": p.SubjectAttribute, "display_name_attribute": p.DisplayNameAttribute, "groups_attribute": p.GroupsAttribute, "ca_cert_pem": p.CACertPEM}
	var result []oidcEntry
	for _, field := range ldapFields {
		if value := values[field]; value != nil {
			result = append(result, oidcEntry{"ldap." + field, *value})
		}
	}
	return result
}

func ldapValues(c *LDAPConfig) map[string]string {
	if c == nil {
		c = &LDAPConfig{}
	}
	return map[string]string{"url": c.URL, "bind_dn": c.BindDN, "bind_password": c.BindPassword, "user_base_dn": c.UserBaseDN, "user_filter": c.UserFilter, "subject_attribute": c.SubjectAttribute, "display_name_attribute": c.DisplayNameAttribute, "groups_attribute": c.GroupsAttribute, "ca_cert_pem": c.CACertPEM}
}

func applyLDAPEntry(c *LDAPConfig, e oidcEntry) {
	value := e.value.(string)
	switch strings.TrimPrefix(e.field, "ldap.") {
	case "url":
		c.URL = value
	case "bind_dn":
		c.BindDN = value
	case "bind_password":
		c.BindPassword = value
	case "user_base_dn":
		c.UserBaseDN = value
	case "user_filter":
		c.UserFilter = value
	case "subject_attribute":
		c.SubjectAttribute = value
	case "display_name_attribute":
		c.DisplayNameAttribute = value
	case "groups_attribute":
		c.GroupsAttribute = value
	case "ca_cert_pem":
		c.CACertPEM = value
	}
}

func cloneAuthenticationSource(c AuthenticationSourceConfig) AuthenticationSourceConfig {
	c.OIDCConfig = cloneOIDCConfig(c.OIDCConfig)
	if c.LDAP != nil {
		copy := *c.LDAP
		c.LDAP = &copy
	}
	return c
}

func authenticationSourceValues(c AuthenticationSourceConfig) map[string]any {
	values := oidcValues(c.OIDCConfig)
	if c.LDAP != nil {
		values["ldap"] = ldapValues(c.LDAP)
	}
	return values
}

func authenticationSourceSnapshot(c AuthenticationSourceConfig) OIDCSettingsSnapshot {
	snapshot := oidcSnapshot(c.OIDCConfig)
	if c.LDAP != nil {
		l := c.LDAP
		snapshot.LDAP = &LDAPSettingsSnapshot{URL: l.URL, BindDN: l.BindDN, BindPasswordSet: l.BindPassword != "", UserBaseDN: l.UserBaseDN, UserFilter: l.UserFilter, SubjectAttribute: l.SubjectAttribute, DisplayNameAttribute: l.DisplayNameAttribute, GroupsAttribute: l.GroupsAttribute, CACertPEM: l.CACertPEM}
	}
	return snapshot
}

func authenticationSourceSessionConfigEqual(a, b AuthenticationSourceConfig) bool {
	return a.Protocol == b.Protocol && OIDCSessionConfigEqual(a.OIDCConfig, b.OIDCConfig) && reflect.DeepEqual(a.LDAP, b.LDAP)
}

func sourceEnvironmentFields(source AuthenticationSourceConfig) []string {
	fields := append(append([]string{}, oidcFields...), "protocol")
	if source.Protocol == "ldap" {
		for _, field := range ldapFields {
			fields = append(fields, "ldap."+field)
		}
	}
	return fields
}

func validateLDAPConfig(id string, source AuthenticationSourceConfig, complete bool) error {
	if source.Protocol != "ldap" || source.LDAP == nil {
		return authenticationFieldError("ldap", "REQUIRED", "LDAP settings are required")
	}
	common := source.OIDCConfig
	if common.Issuer != "" || common.ClientID != "" || common.ClientSecret != "" || common.RedirectURL != "" || common.GroupsClaim != "" || common.AllowInsecureLoopback {
		return authenticationFieldError("protocol", "INVALID_VALUE", "LDAP sources cannot contain OIDC settings")
	}
	// Reuse common name, role mapping and lifetime validation without requiring
	// OIDC credentials from a directory source.
	common.Enabled = true
	common.Issuer, common.ClientID, common.ClientSecret, common.GroupsClaim = "https://directory.invalid", "ldap", "ldap", "groups"
	common.RedirectURL = "https://depsilo.invalid/api/v1/auth/oidc/" + id + "/callback"
	if err := validateSourceOIDCConfig(id, common, complete && source.Enabled); err != nil {
		return sourceValidationError(id, err)
	}
	var groups []*ldap.DN
	for _, field := range []struct {
		name   string
		values []string
	}{{"admin_groups", source.AdminGroups}, {"readonly_groups", source.ReadonlyGroups}} {
		for _, value := range field.values {
			dn, err := ldap.ParseDN(value)
			if err != nil || len(dn.RDNs) == 0 {
				return authenticationFieldError(field.name, "INVALID_VALUE", "LDAP groups must use complete distinguished names")
			}
			for _, previous := range groups {
				if dn.EqualFold(previous) {
					return &AuthenticationValidationError{Fields: map[string]string{"admin_groups": "GROUPS_OVERLAP", "readonly_groups": "GROUPS_OVERLAP"}, Message: "LDAP group mappings must be disjoint and unique"}
				}
			}
			groups = append(groups, dn)
		}
	}
	l := source.LDAP
	required := complete && source.Enabled
	for field, value := range ldapValues(l) {
		if field == "ca_cert_pem" {
			continue
		}
		if value == "" && !required {
			continue
		}
		if value == "" {
			return authenticationFieldError("ldap."+field, "REQUIRED", "LDAP setting is required")
		}
		if len(value) > 4096 || strings.ContainsRune(value, 0) || field != "bind_password" && (value != strings.TrimSpace(value) || strings.ContainsFunc(value, unicode.IsControl)) {
			return authenticationFieldError("ldap."+field, "INVALID_VALUE", "LDAP setting is invalid")
		}
	}
	if l.URL != "" {
		u, err := url.Parse(l.URL)
		if err != nil || u.Scheme != "ldap" && u.Scheme != "ldaps" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(l.URL, "#") || u.RawPath != "" || u.String() != l.URL {
			return authenticationFieldError("ldap.url", "INVALID_URL", "LDAP URL must be ldap:// or ldaps:// with a host and optional port only")
		}
		if port := u.Port(); port != "" {
			if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
				return authenticationFieldError("ldap.url", "INVALID_URL", "LDAP port is invalid")
			}
		}
	}
	for field, dn := range map[string]string{"bind_dn": l.BindDN, "user_base_dn": l.UserBaseDN} {
		if dn != "" {
			if parsed, err := ldap.ParseDN(dn); err != nil || len(parsed.RDNs) == 0 {
				return authenticationFieldError("ldap."+field, "INVALID_VALUE", "LDAP distinguished name is invalid")
			}
		}
	}
	if l.UserFilter != "" {
		if strings.Count(l.UserFilter, "{username}") != 1 {
			return authenticationFieldError("ldap.user_filter", "INVALID_VALUE", "LDAP user filter must contain exactly one {username} placeholder")
		}
		if _, err := ldap.CompileFilter(strings.ReplaceAll(l.UserFilter, "{username}", "test-user")); err != nil {
			return authenticationFieldError("ldap.user_filter", "INVALID_VALUE", "LDAP search filter is invalid")
		}
	}
	if l.SubjectAttribute != "" && !strings.EqualFold(l.SubjectAttribute, "entryUUID") && !strings.EqualFold(l.SubjectAttribute, "objectGUID") {
		return authenticationFieldError("ldap.subject_attribute", "INVALID_VALUE", "LDAP identity must use entryUUID or objectGUID")
	}
	for field, attribute := range map[string]string{"display_name_attribute": l.DisplayNameAttribute, "groups_attribute": l.GroupsAttribute} {
		if attribute != "" && !ldapAttributeName(attribute) {
			return authenticationFieldError("ldap."+field, "INVALID_VALUE", "LDAP attribute name is invalid")
		}
	}
	if len(l.CACertPEM) > 65536 || l.CACertPEM != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(l.CACertPEM)) {
		return authenticationFieldError("ldap.ca_cert_pem", "INVALID_VALUE", "LDAP CA must contain valid PEM certificates")
	}
	return nil
}

func ldapAttributeName(value string) bool {
	for i, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && (r >= '0' && r <= '9' || r == '-')) {
			return false
		}
	}
	return value != "" && len(value) <= 128
}

func ValidateSourceLDAPConfig(id string, source AuthenticationSourceConfig) error {
	if err := ValidateAuthenticationSourceID(id); err != nil {
		return err
	}
	return validateLDAPConfig(id, source, true)
}

// LDAPAuthority binds persistent identities to a specific directory and UUID
// scheme. Rotating bind credentials or renaming the source keeps that identity;
// repointing the directory cannot silently take over its existing accounts.
func LDAPAuthority(id string, c LDAPConfig) string {
	u, _ := url.Parse(c.URL)
	server := c.URL
	if u != nil {
		port := u.Port()
		if port == "" {
			if u.Scheme == "ldaps" {
				port = "636"
			} else {
				port = "389"
			}
		}
		u.Scheme, u.Host = strings.ToLower(u.Scheme), net.JoinHostPort(strings.ToLower(u.Hostname()), port)
		server = u.String()
	}
	base := c.UserBaseDN
	if dn, err := ldap.ParseDN(base); err == nil {
		base = dn.String()
	}
	raw, _ := json.Marshal([]string{id, server, base, strings.ToLower(c.SubjectAttribute)})
	hash := sha256.Sum256(raw)
	return "urn:depsilo:ldap:" + hex.EncodeToString(hash[:])
}

func renderLDAPValue(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var fields []string
	for _, key := range keys {
		fields = append(fields, fmt.Sprintf("%s = %s", key, strconv.Quote(values[key])))
	}
	return []byte("{ " + strings.Join(fields, ", ") + " }")
}

func newAuthenticationSource(protocol string) AuthenticationSourceConfig {
	source := AuthenticationSourceConfig{Protocol: protocol, OIDCConfig: OIDCConfig{SessionTTL: 30 * time.Minute}}
	if protocol == "ldap" {
		source.LDAP = defaultLDAPConfig()
	} else {
		source.GroupsClaim = "groups"
	}
	return source
}
