package cascade

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// pairingPrefix starts every pairing code, so a pasted code is recognizable
// and a future format can use another version.
const pairingPrefix = "depsilo-pair-1."

// Pairing is what a child needs to use a parent: the parent's address, the
// token it accepts, and its instance ID, which lets the child refuse to pair
// with itself.
type Pairing struct {
	URL        string `json:"u"`
	Token      string `json:"t"`
	InstanceID string `json:"i"`
}

// ErrInvalidPairingCode reports a code that is not a Depsilo pairing code.
var ErrInvalidPairingCode = errors.New("not a Depsilo cascade pairing code")

// EncodePairingCode returns the pairing code for pairing. The code carries the
// token: anyone who holds it can fetch through the parent.
func EncodePairingCode(pairing Pairing) string {
	encoded, _ := json.Marshal(pairing)
	return pairingPrefix + base64.RawURLEncoding.EncodeToString(encoded)
}

// DecodePairingCode parses a code from EncodePairingCode, ignoring
// surrounding whitespace.
func DecodePairingCode(code string) (Pairing, error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, pairingPrefix) || len(code) > 4096 {
		return Pairing{}, ErrInvalidPairingCode
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, pairingPrefix))
	if err != nil {
		return Pairing{}, ErrInvalidPairingCode
	}
	var pairing Pairing
	if err := json.Unmarshal(raw, &pairing); err != nil {
		return Pairing{}, ErrInvalidPairingCode
	}
	parsed, err := url.Parse(pairing.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		pairing.Token == "" || !ValidInstanceID(pairing.InstanceID) {
		return Pairing{}, ErrInvalidPairingCode
	}
	return pairing, nil
}
