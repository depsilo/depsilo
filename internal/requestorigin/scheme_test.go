package requestorigin

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScheme(t *testing.T) {
	for _, test := range []struct {
		name      string
		directTLS bool
		forwarded []string
		want      string
	}{
		{name: "plain connection without the header", want: "http"},
		{name: "direct TLS without the header", directTLS: true, want: "https"},
		{name: "direct TLS wins over a forwarded http", directTLS: true, forwarded: []string{"http"}, want: "https"},
		{name: "direct TLS ignores a bogus value", directTLS: true, forwarded: []string{"gopher"}, want: "https"},
		{name: "forwarded https", forwarded: []string{"https"}, want: "https"},
		{name: "forwarded http", forwarded: []string{"http"}, want: "http"},
		{name: "upper case", forwarded: []string{"HTTPS"}, want: "https"},
		{name: "mixed case http", forwarded: []string{"Http"}, want: "http"},
		{name: "surrounding space", forwarded: []string{"  https\t"}, want: "https"},
		{name: "two proxies, client used https", forwarded: []string{"https, http"}, want: "https"},
		{name: "two proxies, client used http", forwarded: []string{"http, https"}, want: "http"},
		{name: "list without spaces", forwarded: []string{"https,http"}, want: "https"},
		{name: "list with an upper-case first element", forwarded: []string{" HTTPS , http"}, want: "https"},
		{name: "repeated header lines use the first", forwarded: []string{"https", "http"}, want: "https"},
		{name: "another scheme", forwarded: []string{"gopher"}, want: "http"},
		{name: "another scheme first in a list", forwarded: []string{"gopher, https"}, want: "http"},
		{name: "websocket scheme", forwarded: []string{"wss"}, want: "http"},
		{name: "a URL instead of a scheme", forwarded: []string{"https://evil.example/"}, want: "http"},
		{name: "empty first element", forwarded: []string{", https"}, want: "http"},
		{name: "empty value", forwarded: []string{""}, want: "http"},
		{name: "only separators", forwarded: []string{" , "}, want: "http"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://depsilo.example.test/", nil)
			if test.directTLS {
				request.TLS = &tls.ConnectionState{}
			}
			for _, value := range test.forwarded {
				request.Header.Add("X-Forwarded-Proto", value)
			}
			if got := Scheme(request); got != test.want {
				t.Fatalf("Scheme with TLS=%v and X-Forwarded-Proto %q = %q, want %q",
					test.directTLS, test.forwarded, got, test.want)
			}
		})
	}
}
