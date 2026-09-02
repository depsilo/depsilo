package gomoduleidentity

import "testing"

func TestDecodeProxyVersion(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		escaped string
		want    string
		valid   bool
	}{
		{escaped: "v1.2.3", want: "v1.2.3", valid: true},
		{escaped: "v1.0.0-!r!c1", want: "v1.0.0-RC1", valid: true},
		{escaped: "v1.0.0-!!rc1"},
		{escaped: "v1.0.0-RC1"},
		{escaped: "v1.0.0-!R!c1"},
		{escaped: "v1.0.0-!1rc1"},
		{escaped: "v1.0.0-rc1!"},
		{escaped: ""},
	} {
		t.Run(test.escaped, func(t *testing.T) {
			got, err := DecodeProxyVersion(test.escaped)
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("DecodeProxyVersion(%q) = (%q, %v), want (%q, nil)", test.escaped, got, err, test.want)
				}
				return
			}
			if err == nil || got != "" {
				t.Fatalf("DecodeProxyVersion(%q) = (%q, %v), want error", test.escaped, got, err)
			}
		})
	}
}
