package upstream

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
)

// ProvenanceSourceResolver resolves the exact configured upstream that
// produced trusted metadata. Artifact adapters use this narrower seam when a
// later download must not run the normal failover selector again.
type ProvenanceSourceResolver interface {
	ResolveProvenanceSource(sourceID string) (*Upstream, error)
}

// ProvenanceSourceID returns an opaque, stable identity for the configured
// source endpoint and egress boundary. Changes to its database identity,
// credential-free origin/proxy endpoint, name, or adapter ownership invalidate
// references; credential rotation deliberately retains the endpoint identity.
func (u *Upstream) ProvenanceSourceID() string {
	if u == nil {
		return ""
	}
	hash := sha256.New()
	for _, value := range []string{
		"depsilo/upstream-provenance-source/v1",
		u.AdapterType,
		u.Name,
		provenanceURLIdentity(u.URL),
		provenanceURLIdentity(u.Proxy),
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	// Persisted registry IDs disambiguate delete/recreate operations. Config
	// pools use ID zero, where the non-secret endpoint configuration above is
	// still stable and unique within a validated pool.
	for shift := 0; shift < 8; shift++ {
		_, _ = hash.Write([]byte{byte(uint64(u.ID) >> (shift * 8))})
	}
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}

// provenanceURLIdentity keeps endpoint/transport identity while excluding
// secret-bearing userinfo and proxy query parameters. Credential rotation
// should not invalidate a source reference, and an opaque source ID must not
// become a deterministic offline password verifier.
func provenanceURLIdentity(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func resolveProvenanceSource(pool *Pool, sourceID string) (*Upstream, error) {
	if pool == nil || sourceID == "" {
		return nil, errors.New("artifact provenance source is unavailable")
	}
	var matched *Upstream
	for _, candidate := range pool.Snapshot() {
		if candidate == nil || candidate.ProvenanceSourceID() != sourceID {
			continue
		}
		if matched != nil {
			return nil, errors.New("artifact provenance source identity is ambiguous")
		}
		matched = candidate
	}
	if matched == nil {
		return nil, errors.New("artifact provenance source is unavailable")
	}
	return matched, nil
}
