package cli

import "testing"

func TestTrustedHostForPlainHTTP(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "plain HTTP", baseURL: "http://depsilo.local:23333", want: "depsilo.local:23333"},
		{name: "uppercase scheme", baseURL: "HTTP://depsilo.local:23333", want: "depsilo.local:23333"},
		{name: "HTTPS", baseURL: "https://depsilo.example", want: ""},
		{name: "missing scheme", baseURL: "depsilo.local:23333", want: ""},
		{name: "missing host", baseURL: "http://", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trustedHostForPlainHTTP(tt.baseURL); got != tt.want {
				t.Errorf("trustedHostForPlainHTTP(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
