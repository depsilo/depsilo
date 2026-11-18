package admin

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/middleware"
)

// AdminRoleRequired permits viewing system configuration with the effective
// administrator role, including read-only tokens owned by an administrator.
// Mutation additionally requires WriteRequired and local credential authority.
func AdminRoleRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := middleware.PrincipalFromContext(c)
		if !ok || principal.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "administrator role required"})
			return
		}
		c.Next()
	}
}

func validAuthenticationObject(body []byte) bool {
	return validAuthenticationSourceObject(body, false)
}

// ValidAuthenticationSettingsObject lets draft diagnostics share the exact
// settings-key, duplicate-field, and explicit-null contract of saved updates.
func ValidAuthenticationSettingsObject(body []byte) bool {
	return validAuthenticationObject(body)
}

func validAuthenticationSourceObject(body []byte, create bool) bool {
	return validAuthenticationProtocolObject(body, create, "oidc")
}

// ValidAuthenticationProtocolSettingsObject keeps saved and diagnostic
// candidates on the same strict, protocol-specific field contract.
func ValidAuthenticationProtocolSettingsObject(body []byte, protocol string) bool {
	return validAuthenticationProtocolObject(body, false, protocol)
}

func validAuthenticationProtocolObject(body []byte, create bool, protocol string) bool {
	if protocol != "oidc" && protocol != "ldap" {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return false
		}
		// encoding/json also accepts case-insensitive struct field names. Use
		// exact protocol keys so aliases cannot bypass duplicate detection.
		switch key {
		case "id", "protocol":
			if !create {
				return false
			}
		case "enabled", "display_name", "admin_groups", "readonly_groups", "session_ttl":
		case "issuer", "client_id", "client_secret", "redirect_url", "groups_claim", "allow_insecure_loopback":
			if protocol != "oidc" {
				return false
			}
		case "ldap":
			if protocol != "ldap" {
				return false
			}
		default:
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
		if key == "ldap" && !validLDAPSettingsObject(value) {
			return false
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return false
	}
	return requireJSONEOF(decoder) == nil
}

func validLDAPSettingsObject(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return false
		}
		switch key {
		case "url", "bind_dn", "bind_password", "user_base_dn", "user_filter", "subject_attribute", "display_name_attribute", "groups_attribute", "ca_cert_pem":
		default:
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return false
		}
	}
	token, err = decoder.Token()
	return err == nil && token == json.Delim('}') && requireJSONEOF(decoder) == nil
}
