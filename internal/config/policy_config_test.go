package config

import (
	"strings"
	"testing"
)

func TestPolicyOnLoadErrorDefaultsExplicitly(t *testing.T) {
	cfg, err := decodeConfigDocument(nil)
	if err != nil {
		t.Fatalf("decode default config: %v", err)
	}
	if got, want := cfg.Policy.OnLoadError, defaultPolicyOnLoadError; got != want {
		t.Fatalf("policy.on_load_error = %q, want %q", got, want)
	}
}

func TestPolicyOnLoadErrorValuesNormalize(t *testing.T) {
	for _, value := range []string{"use_stale_then_allow", "use_stale_then_deny", "allow", "deny"} {
		t.Run(value, func(t *testing.T) {
			cfg, err := decodeConfigDocument([]byte("[policy]\non_load_error = \"" + strings.ToUpper(value) + "\"\n"))
			if err != nil {
				t.Fatalf("decode policy config: %v", err)
			}
			if cfg.Policy.OnLoadError != value {
				t.Fatalf("policy.on_load_error = %q, want canonical %q", cfg.Policy.OnLoadError, value)
			}
		})
	}
}

func TestPolicyOnLoadErrorRejectsUnknownValue(t *testing.T) {
	_, err := decodeConfigDocument([]byte("[policy]\non_load_error = \"sometimes\"\n"))
	if err == nil || !strings.Contains(err.Error(), "policy.on_load_error") {
		t.Fatalf("decode accepted/poorly reported invalid policy: %v", err)
	}
}
