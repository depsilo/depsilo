package cascade

import (
	"errors"
	"strings"
	"testing"
)

func TestPairingCodeRoundTrips(t *testing.T) {
	want := Pairing{URL: "http://192.168.1.10:23333", Token: "0123456789abcdef0123", InstanceID: "parent-node"}
	code := EncodePairingCode(want)
	if !strings.HasPrefix(code, "depsilo-pair-1.") || strings.ContainsAny(code, " \n+/=") {
		t.Fatalf("code = %q", code)
	}
	got, err := DecodePairingCode("  " + code + "\n")
	if err != nil || got != want {
		t.Fatalf("decode = %+v, %v; want %+v", got, err, want)
	}
}

func TestDecodePairingCodeRejectsAnythingElse(t *testing.T) {
	for name, code := range map[string]string{
		"empty":         "",
		"no prefix":     "abc",
		"bad base64":    "depsilo-pair-1.!!!",
		"not json":      "depsilo-pair-1." + "bm90IGpzb24",
		"missing url":   EncodePairingCode(Pairing{Token: "t", InstanceID: "node"}),
		"ftp url":       EncodePairingCode(Pairing{URL: "ftp://host", Token: "t", InstanceID: "node"}),
		"missing token": EncodePairingCode(Pairing{URL: "http://host", InstanceID: "node"}),
		"bad instance":  EncodePairingCode(Pairing{URL: "http://host", Token: "t", InstanceID: "Bad ID"}),
	} {
		if _, err := DecodePairingCode(code); !errors.Is(err, ErrInvalidPairingCode) {
			t.Errorf("%s: err = %v, want ErrInvalidPairingCode", name, err)
		}
	}
}
