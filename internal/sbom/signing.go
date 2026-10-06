package sbom

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// Signature is a detached Ed25519 signature over the exported SBOM document
// bytes. Verifiers hash the exact document payload and check it with the
// embedded public key.
type Signature struct {
	Algorithm     string `json:"algorithm"`
	PayloadSHA256 string `json:"payload_sha256"`
	PublicKey     string `json:"public_key"`
	Signature     string `json:"signature"`
}

// LoadSigningKey reads a PKCS#8 PEM Ed25519 private key from path.
func LoadSigningKey(path string) (ed25519.PrivateKey, error) {
	if path == "" {
		return nil, errors.New("no compliance signing key configured")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compliance signing key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("compliance signing key is not PEM encoded")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse compliance signing key: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("compliance signing key must be an Ed25519 PKCS#8 private key")
	}
	return key, nil
}

// SignDocument returns the detached signature block for one document.
func SignDocument(key ed25519.PrivateKey, document []byte) Signature {
	digest := sha256.Sum256(document)
	publicKey := key.Public().(ed25519.PublicKey)
	return Signature{
		Algorithm:     "ed25519",
		PayloadSHA256: hex.EncodeToString(digest[:]),
		PublicKey:     base64.StdEncoding.EncodeToString(publicKey),
		Signature:     base64.StdEncoding.EncodeToString(ed25519.Sign(key, document)),
	}
}
