package distribution

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

const publicKeyPrefix = "ed25519:"

// TransferKeys hold the dedicated transfer-signing identity. They are separate
// from Admin login, JWT, URL-signing and SBOM compliance keys.
type TransferKeys struct {
	Signer  ed25519.PrivateKey
	Trusted []ed25519.PublicKey
}

// KeyID is the lowercase SHA-256 of the raw public key. It only selects a
// configured key; it never confers trust by itself.
func KeyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}

// FormatPublicKey renders the configuration form accepted by ParsePublicKey.
func FormatPublicKey(key ed25519.PublicKey) string {
	return publicKeyPrefix + base64.StdEncoding.EncodeToString(key)
}

func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	encoded, ok := strings.CutPrefix(value, publicKeyPrefix)
	if !ok {
		return nil, errors.New("trusted transfer key must use the form ed25519:<base64>")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("trusted transfer key is not a 32-byte base64 Ed25519 public key")
	}
	return ed25519.PublicKey(raw), nil
}

// ParseTrustedKeys rejects malformed and duplicate keys so a typo cannot
// silently shrink the trusted set an operator believes is configured.
func ParseTrustedKeys(values []string) ([]ed25519.PublicKey, error) {
	keys := make([]ed25519.PublicKey, 0, len(values))
	seen := map[string]bool{}
	for index, value := range values {
		key, err := ParsePublicKey(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("distribution.trusted_transfer_keys[%d]: %w", index, err)
		}
		id := KeyID(key)
		if seen[id] {
			return nil, fmt.Errorf("distribution.trusted_transfer_keys[%d]: duplicate key", index)
		}
		seen[id] = true
		keys = append(keys, key)
	}
	return keys, nil
}

// LoadSigningKey reads a PKCS#8 PEM Ed25519 private key.
func LoadSigningKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read transfer signing key: %w", err)
	}
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("transfer signing key must be one PKCS#8 PEM PRIVATE KEY block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("parse transfer signing key: invalid PKCS#8 data")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("transfer signing key must be an Ed25519 private key")
	}
	return key, nil
}

// GenerateSigningKey returns a new PKCS#8 PEM private key and its public key.
func GenerateSigningKey() ([]byte, ed25519.PublicKey, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), public, nil
}
