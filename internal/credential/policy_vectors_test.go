package credential

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vectorReasons maps the reason names of testdata/password_vectors.json to
// the text ValidatePassword returns. web/unit/passwordPolicy.test.ts runs the
// browser copy of the policy against the same file.
var vectorReasons = map[string]string{
	"too_short":         "at least 12 characters",
	"too_long":          "at most 72 bytes",
	"control_character": "control characters",
	"weak":              "three character classes",
	"contains_username": "must not contain the username",
	"common":            "too common",
}

func TestCredentialPolicySharedVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "password_vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var document struct {
		Vectors []struct {
			Name     string `json:"name"`
			Username string `json:"username"`
			Password string `json:"password"`
			Accepted bool   `json:"accepted"`
			Reason   string `json:"reason"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	if len(document.Vectors) == 0 {
		t.Fatal("password_vectors.json has no vectors")
	}

	covered := map[string]bool{}
	for _, vector := range document.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			if vector.Accepted != (vector.Reason == "") {
				t.Fatalf("vector is inconsistent: accepted = %v, reason = %q", vector.Accepted, vector.Reason)
			}
			err := CredentialPolicy.ValidatePassword(vector.Username, vector.Password)
			if vector.Accepted {
				if err != nil {
					t.Fatalf("ValidatePassword() error = %v, want accepted", err)
				}
				return
			}
			want, known := vectorReasons[vector.Reason]
			if !known {
				t.Fatalf("unknown reason %q", vector.Reason)
			}
			covered[vector.Reason] = true
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("ValidatePassword() error = %v, want an error containing %q", err, want)
			}
		})
	}
	for reason := range vectorReasons {
		if !covered[reason] {
			t.Errorf("no vector covers the reason %q", reason)
		}
	}
}
