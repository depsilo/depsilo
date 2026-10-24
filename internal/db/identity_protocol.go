package db

import "strings"

// LDAPIdentityIssuerPrefix separates a directory authority from an OIDC URL.
// The suffix binds the source to its configured directory and stable-ID field.
const LDAPIdentityIssuerPrefix = "urn:depsilo:ldap:"

// LoginProtocol is derived from the durable identity authority, not from a
// caller's requested source or the account's permission authority.
func (identity ExternalIdentity) LoginProtocol() string {
	if strings.HasPrefix(identity.Issuer, LDAPIdentityIssuerPrefix) {
		return "ldap"
	}
	return "oidc"
}
