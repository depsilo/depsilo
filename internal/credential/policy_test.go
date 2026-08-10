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
		wantErr  bool
	}{
		{name: "strong password", username: "operator", password: "Tr0ub4dor&Correct"},
		{name: "long passphrase", username: "operator", password: "correct horse battery staple"},
		{name: "short password", username: "operator", password: "Ab1!", wantErr: true},
		{name: "low diversity", username: "operator", password: "abcdefghijkl", wantErr: true},
		{name: "contains username", username: "operator", password: "Operator-Is-Safe-123!", wantErr: true},
		{name: "control character", username: "operator", password: "Strong&Pass1\n", wantErr: true},
		{name: "bcrypt byte limit", username: "operator", password: strings.Repeat("密", 25), wantErr: true},
		{name: "common password", username: "operator", password: "password123!", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CredentialPolicy.ValidatePassword(test.username, test.password)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidatePassword() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
