// Package distribution owns retained dependency-set drafts and their content.
// Content verification is not provenance verification or an offline install claim.
package distribution

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	ManifestFormat               = "depsilo/dependency-set-draft/v1"
	MaxManifestBytes             = 1 << 20
	MaxObjects                   = 4096
	DefaultMaxObjectBytes  int64 = 1 << 30
	DefaultMaxStorageBytes int64 = 20 << 30
)

var (
	ErrInvalid    = errors.New("invalid dependency set")
	ErrNotFound   = errors.New("dependency set or declared object not found")
	ErrConflict   = errors.New("dependency set revision or object size conflicts")
	ErrActive     = errors.New("active dependency set cannot be deleted")
	ErrQuota      = errors.New("distribution storage limit exceeded")
	ErrIntegrity  = errors.New("content length or SHA-256 mismatch")
	ErrIncomplete = errors.New("dependency set content is incomplete")
)

type Limits struct{ MaxObjectBytes, MaxStorageBytes int64 }

// Target is a declared preparation environment, not a qualification result.
type Target struct {
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	PythonVersion string `json:"python_version"`
	PythonABI     string `json:"python_abi"`
	PipVersion    string `json:"pip_version"`
	ClientImage   string `json:"client_image"`
}

type Object struct {
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Kind      string `json:"kind"`
	Filename  string `json:"filename"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	SourceURL string `json:"source_url,omitempty"`
}

type Manifest struct {
	Format    string   `json:"format"`
	Name      string   `json:"name"`
	Revision  string   `json:"revision"`
	Ecosystem string   `json:"ecosystem"`
	Target    Target   `json:"target"`
	Objects   []Object `json:"objects"`
}

type Set struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Revision          string      `json:"revision"`
	CreatedAt         time.Time   `json:"created_at"`
	VerifiedAt        *time.Time  `json:"verified_at"`
	CheckedAt         *time.Time  `json:"checked_at"`
	Manifest          Manifest    `json:"manifest"`
	ContentStatus     string      `json:"content_status"`
	ProvenanceStatus  string      `json:"provenance_status"`
	InstallValidation string      `json:"install_validation"`
	Serving           bool        `json:"serving"`
	Provenance        *Provenance `json:"provenance,omitempty"`
}

// Provenance identifies the trusted preparer whose signed transfer manifest
// delivered a set. Trust was decided at import time with the then-configured
// keys; removing a key later does not rewrite this historical record.
type Provenance struct {
	Origin      string    `json:"origin"`
	SignerKeyID string    `json:"signer_key_id"`
	ImportedBy  string    `json:"imported_by"`
	ImportedAt  time.Time `json:"imported_at"`
}

type UploadResult struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Reused bool   `json:"reused"`
}

type Issue struct {
	SHA256 string `json:"sha256"`
	Code   string `json:"code"`
}
type Verification struct {
	SetID             string    `json:"set_id"`
	ContentStatus     string    `json:"content_status"`
	VerifiedObjects   int       `json:"verified_objects"`
	Issues            []Issue   `json:"issues"`
	CheckedAt         time.Time `json:"checked_at"`
	ProvenanceStatus  string    `json:"provenance_status"`
	InstallValidation string    `json:"install_validation"`
	Serving           bool      `json:"serving"`
}
type Usage struct {
	Bytes      int64 `json:"bytes"`
	QuotaBytes int64 `json:"quota_bytes"`
	Objects    int64 `json:"objects"`
	Sets       int64 `json:"sets"`
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
var filename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]{0,254}$`)
var version = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+!-]{0,127}$`)

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// ParseManifest rejects ambiguous JSON, unknown fields and path-like names.
// IDs are hashes of the exact bytes, so reformatting an existing name/revision
// is a different declaration and conflicts instead of silently overwriting it.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if len(raw) == 0 || len(raw) > MaxManifestBytes {
		return m, fmt.Errorf("%w: manifest size", ErrInvalid)
	}
	if err := decodeStrict(raw, &m); err != nil {
		return m, fmt.Errorf("%w: malformed, duplicate, trailing, or unknown JSON fields", ErrInvalid)
	}
	if m.Format != ManifestFormat || !identifier.MatchString(m.Name) || !identifier.MatchString(m.Revision) || m.Ecosystem != "pypi" {
		return m, fmt.Errorf("%w: format, name, revision or ecosystem", ErrInvalid)
	}
	t := m.Target
	imageParts := strings.Split(t.ClientImage, "@sha256:")
	if t.OS != "linux" || t.Architecture != "amd64" || !version.MatchString(t.PythonVersion) || !version.MatchString(t.PythonABI) || !version.MatchString(t.PipVersion) || len(imageParts) != 2 || len(imageParts[0]) == 0 || len(t.ClientImage) > 512 || strings.ContainsAny(t.ClientImage, "\t\r\n ?#") || !validDigest(imageParts[1]) {
		return m, fmt.Errorf("%w: explicit Linux/amd64 Python target and pinned client image required", ErrInvalid)
	}
	if len(m.Objects) == 0 || len(m.Objects) > MaxObjects {
		return m, fmt.Errorf("%w: object count", ErrInvalid)
	}
	seen, names := map[string]bool{}, map[string]bool{}
	for _, o := range m.Objects {
		if !validDigest(o.SHA256) || o.Size <= 0 || !filename.MatchString(o.Filename) || !identifier.MatchString(o.Package) || !version.MatchString(o.Version) || seen[o.SHA256] || names[o.Filename] {
			return m, fmt.Errorf("%w: object identity, size or duplicate object", ErrInvalid)
		}
		if (o.Kind != "wheel" || !strings.HasSuffix(o.Filename, ".whl")) && (o.Kind != "metadata" || !strings.HasSuffix(o.Filename, ".whl.metadata")) {
			return m, fmt.Errorf("%w: wheel or wheel metadata required", ErrInvalid)
		}
		if o.SourceURL != "" {
			u, err := url.Parse(o.SourceURL)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(o.SourceURL) > 2048 || strings.ContainsAny(o.SourceURL, "\r\n\t ") {
				return m, fmt.Errorf("%w: source URL must be HTTPS without credentials or query", ErrInvalid)
			}
		}
		seen[o.SHA256], names[o.Filename] = true, true
	}
	return m, nil
}

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			// encoding/json matches struct keys without regard to case, including
			// Unicode aliases such as the long s. Schema keys are lowercase ASCII;
			// reject aliases before they can overwrite another key's value.
			if !ok || strings.IndexFunc(name, func(r rune) bool {
				return r != '_' && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
			}) >= 0 || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if err := uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	return err
}
