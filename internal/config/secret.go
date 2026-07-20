package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

const secureTokenBytes = 32

// NewSecureToken returns a URL-safe, 256-bit random token suitable for
// bootstrap authentication and symmetric application secrets.
func NewSecureToken() (string, error) {
	raw := make([]byte, secureTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
