package credential

import (
	"strings"
	"testing"
)

func TestCredentialPolicy(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		// wantErr is a part of the expected error; empty means accepted.
		wantErr string
	}{
		{name: "strong password", username: "operator", password: "Tr0ub4dor&Correct"},
		{name: "long passphrase", username: "operator", password: "correct horse battery staple"},
		{name: "invalid UTF-8", username: "operator", password: "Strong&Pass1\xff", wantErr: "valid UTF-8"},
		{name: "short password", username: "operator", password: "Ab1!", wantErr: "at least 12 characters"},
		{name: "low diversity", username: "operator", password: "abcdefghijkl", wantErr: "three character classes"},
		{name: "contains username", username: "operator", password: "Operator-Is-Safe-123!", wantErr: "must not contain the username"},
		{name: "control character", username: "operator", password: "Strong&Pass1\n", wantErr: "control characters"},
		{name: "bcrypt byte limit", username: "operator", password: strings.Repeat("密", 25), wantErr: "at most 72 bytes"},
		{name: "common password", username: "operator", password: "password123!", wantErr: "too common"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CredentialPolicy.ValidatePassword(test.username, test.password)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidatePassword() error = %v, want accepted", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ValidatePassword() error = %v, want an error containing %q", err, test.wantErr)
			}
		})
	}
}
