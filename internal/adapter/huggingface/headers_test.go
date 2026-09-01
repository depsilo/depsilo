package huggingface

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestUpstreamAuthorizationNeverReturnsProjectCredentials(t *testing.T) {
	tests := []struct {
		name string
		auth string
		want string
	}{
		{name: "Hugging Face token", auth: "Bearer hf_private", want: "Bearer hf_private"},
		{name: "Depsilo project token", auth: "Bearer depsilo_proj_secret", want: ""},
		{name: "case-insensitive scheme", auth: "bearer depsilo_proj_secret", want: ""},
		{name: "unrelated authorization", auth: "Basic abc123", want: "Basic abc123"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, "http://example.test/file", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", test.auth)
			if got := upstreamAuthorization(request); got != test.want {
				t.Fatalf("upstreamAuthorization() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestOriginRequestHeadersForceIdentityForCacheableRequest(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Authorization", "Bearer depsilo_proj_secret")

	headers := originRequestHeaders(request, true)
	if got := headers.Get("Accept-Encoding"); got != "identity" {
		t.Fatalf("Accept-Encoding = %q, want identity", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Fatalf("Authorization leaked into origin headers: %q", got)
	}
}

func TestCacheEligibleOnlyAllowsCompletePublicGET(t *testing.T) {
	tests := []struct {
		name   string
		method string
		header http.Header
		want   bool
	}{
		{name: "anonymous GET", method: http.MethodGet, want: true},
		{name: "project-token GET", method: http.MethodGet, header: http.Header{"Authorization": {"Bearer depsilo_proj_x"}}, want: true},
		{name: "HF-auth GET", method: http.MethodGet, header: http.Header{"Authorization": {"Bearer hf_x"}}, want: false},
		{name: "HEAD", method: http.MethodHead, want: false},
		{name: "Range", method: http.MethodGet, header: http.Header{"Range": {"bytes=0-1"}}, want: false},
		{name: "conditional", method: http.MethodGet, header: http.Header{"If-None-Match": {`"etag"`}}, want: false},
		{name: "varying Accept", method: http.MethodGet, header: http.Header{"Accept": {"application/json"}}, want: false},
		{name: "identity forbidden", method: http.MethodGet, header: http.Header{"Accept-Encoding": {"gzip, identity;q=0"}}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, "http://example.test/file", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header = test.header.Clone()
			if got := cacheEligible(request); got != test.want {
				t.Fatalf("cacheEligible() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCacheEligibleRejectsCredentialQueries(t *testing.T) {
	for _, rawQuery := range []string{
		"token=secret",
		"access_token=secret",
		"X-Amz-Signature=secret",
		"X-Goog-Credential=secret",
	} {
		t.Run(rawQuery, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, "http://example.test/file?"+rawQuery, nil)
			if err != nil {
				t.Fatal(err)
			}
			if cacheEligible(request) {
				t.Fatalf("credential-bearing query %q was cache eligible", rawQuery)
			}
		})
	}
}

func TestCacheSafeQueryRejectsUnboundedArtifactVariants(t *testing.T) {
	artifact := ParseRequestPath("/acme/model/resolve/0123456789abcdef0123456789abcdef01234567/model.bin")
	for name, test := range map[string]struct {
		query url.Values
		want  bool
	}{
		"none":              {want: true},
		"download true":     {query: url.Values{"download": {"true"}}, want: true},
		"unknown nonce":     {query: url.Values{"nonce": {"1"}}},
		"download repeated": {query: url.Values{"download": {"true", "true"}}},
		"download plus junk": {
			query: url.Values{"download": {"true"}, "nonce": {"1"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := cacheSafeQuery(artifact, test.query); got != test.want {
				t.Fatalf("cacheSafeQuery() = %v, want %v", got, test.want)
			}
		})
	}
	metadata := ParseRequestPath("/api/models/acme/model/tree/main")
	if !cacheSafeQuery(metadata, url.Values{"cursor": {"opaque"}, "limit": {"100"}}) {
		t.Fatal("structured metadata pagination query was rejected")
	}
	for name, query := range map[string]url.Values{
		"unknown metadata nonce": {"nonce": {"1"}},
		"repeated cursor":        {"cursor": {"one", "two"}},
		"invalid limit":          {"limit": {"010"}},
		"invalid tree option":    {"recursive": {"sometimes"}},
	} {
		t.Run(name, func(t *testing.T) {
			if cacheSafeQuery(metadata, query) {
				t.Fatalf("unsafe metadata query was cacheable: %v", query)
			}
		})
	}

	modelInfo := ParseRequestPath("/api/models/acme/model")
	if !cacheSafeQuery(modelInfo, url.Values{
		"blobs":          {"True"},
		"securityStatus": {"true"},
	}) {
		t.Fatal("official model info flags were rejected")
	}
	if cacheSafeQuery(modelInfo, url.Values{"expand": {"likes", "downloads"}}) {
		t.Fatal("combinatorial model info expansion was cacheable")
	}

	datasetInfo := ParseRequestPath("/api/datasets/acme/data")
	if !cacheSafeQuery(datasetInfo, url.Values{"blobs": {"True"}}) {
		t.Fatal("official dataset info query was rejected")
	}
	if cacheSafeQuery(datasetInfo, url.Values{"securityStatus": {"True"}}) {
		t.Fatal("model-only security query was cacheable for a dataset")
	}
}

func TestCacheResponseReusableHonorsSharedCacheConstraints(t *testing.T) {
	const ttl = 5 * time.Minute
	for name, test := range map[string]struct {
		header http.Header
		want   bool
	}{
		"no directives": {want: true},
		"long max age": {
			header: http.Header{"Cache-Control": {"public, max-age=600"}},
			want:   true,
		},
		"short max age": {
			header: http.Header{"Cache-Control": {"public, max-age=30"}},
		},
		"age leaves enough freshness": {
			header: http.Header{
				"Cache-Control": {"public, max-age=600"},
				"Age":           {"299"},
			},
			want: true,
		},
		"age exhausts local ttl": {
			header: http.Header{
				"Cache-Control": {"public, max-age=300"},
				"Age":           {"299"},
			},
		},
		"date exhausts local ttl": {
			header: http.Header{
				"Cache-Control": {"public, max-age=600"},
				"Date": {
					time.Now().Add(-301 * time.Second).UTC().Format(http.TimeFormat),
				},
			},
		},
		"malformed age": {
			header: http.Header{
				"Cache-Control": {"public, max-age=600"},
				"Age":           {"invalid"},
			},
		},
		"no store": {
			header: http.Header{"Cache-Control": {"no-store"}},
		},
		"private fields": {
			header: http.Header{"Cache-Control": {`private="Set-Cookie"`}},
		},
		"no cache": {
			header: http.Header{"Cache-Control": {"no-cache"}},
		},
		"vary fixed dimension": {
			header: http.Header{"Vary": {"User-Agent"}},
			want:   true,
		},
		"vary star": {
			header: http.Header{"Vary": {"*"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := cacheResponseReusable(test.header, ttl); got != test.want {
				t.Fatalf("cacheResponseReusable() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFreshnessHeadersPreservedForDirectButNotCachedResponses(t *testing.T) {
	filtered := filterResponseHeaders(http.Header{
		"Age":  {"12"},
		"Date": {"Mon, 27 Jul 2026 12:00:00 GMT"},
	})
	if filtered.Get("Age") != "12" || filtered.Get("Date") == "" {
		t.Fatalf("freshness headers were not retained internally: %v", filtered)
	}
	direct := clientResponseHeaders(filtered, "")
	if direct.Get("Age") != "12" || direct.Get("Date") == "" {
		t.Fatalf("direct response lost upstream freshness headers: %v", direct)
	}
	cached := cachedClientResponseHeaders(filtered, "")
	if cached.Get("Age") != "" || cached.Get("Date") != "" {
		t.Fatalf("cached response replayed upstream freshness headers: %v", cached)
	}
	errorHeaders := filterErrorResponseHeaders(filtered)
	if errorHeaders.Get("Age") != "12" || errorHeaders.Get("Date") == "" {
		t.Fatalf("direct error response lost upstream freshness headers: %v", errorHeaders)
	}
}

func TestXetConnectionHeadersAreScopedToReadTokenResponses(t *testing.T) {
	source := http.Header{
		"X-Xet-Cas-Url":          {"https://cas.example.test"},
		"X-Xet-Access-Token":     {"short-lived-token"},
		"X-Xet-Token-Expiration": {"1848535668"},
		"X-Origin-Secret":        {"must-not-pass"},
	}

	token := filterResponseHeadersForTarget(
		"/api/models/acme/model/xet-read-token/main",
		source,
	)
	for name, want := range map[string]string{
		"X-Xet-Cas-Url":          "https://cas.example.test",
		"X-Xet-Access-Token":     "short-lived-token",
		"X-Xet-Token-Expiration": "1848535668",
	} {
		if got := token.Get(name); got != want {
			t.Fatalf("token response %s = %q, want %q", name, got, want)
		}
	}
	if got := token.Get("X-Origin-Secret"); got != "" {
		t.Fatalf("token response leaked unlisted origin header %q", got)
	}

	for _, target := range []string{
		"/api/models/acme/model",
		"/acme/model/resolve/main/model.bin",
		"https://huggingface.co/api/models/acme/model/xet-read-token/main",
	} {
		t.Run(target, func(t *testing.T) {
			filtered := filterResponseHeadersForTarget(target, source)
			for _, name := range hfXetConnectionHeaders {
				if got := filtered.Get(name); got != "" {
					t.Fatalf("target %q leaked %s = %q", target, name, got)
				}
			}
		})
	}
}

func TestCDNRequestHeadersPreserveRangeValidatorsWithoutCredentials(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=4-")
	request.Header.Set("If-Range", `"blob-etag"`)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Authorization", "Bearer hf_private")
	request.Header.Set("Cookie", "secret=value")

	headers := cdnRequestHeaders(request, false)
	if got := headers.Get("Range"); got != "bytes=4-" {
		t.Fatalf("Range = %q", got)
	}
	if got := headers.Get("If-Range"); got != `"blob-etag"` {
		t.Fatalf("If-Range = %q", got)
	}
	if got := headers.Get("Accept-Encoding"); got != "identity" {
		t.Fatalf("Accept-Encoding = %q, want identity", got)
	}
	if headers.Get("Authorization") != "" || headers.Get("Cookie") != "" {
		t.Fatalf("credentials leaked to CDN: %v", headers)
	}
}

func TestRangeUpstreamHeadersForceIdentityEncoding(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=0-1,4-5")
	request.Header.Set("Accept-Encoding", "gzip, br")

	for name, headers := range map[string]http.Header{
		"origin": originRequestHeaders(request, false),
		"CDN":    cdnRequestHeaders(request, false),
	} {
		t.Run(name, func(t *testing.T) {
			if got := headers.Get("Accept-Encoding"); got != "identity" {
				t.Fatalf("Accept-Encoding = %q, want identity", got)
			}
		})
	}
}

func TestNonGETUpstreamHeadersIgnoreRange(t *testing.T) {
	request, err := http.NewRequest(http.MethodHead, "http://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=4-")
	request.Header.Set("If-Range", `"blob-etag"`)

	for name, headers := range map[string]http.Header{
		"origin": originRequestHeaders(request, false),
		"CDN":    cdnRequestHeaders(request, false),
	} {
		t.Run(name, func(t *testing.T) {
			if headers.Get("Range") != "" || headers.Get("If-Range") != "" {
				t.Fatalf("HEAD forwarded Range headers: %v", headers)
			}
		})
	}
}
