package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cascadeServer answers the pairing and join endpoints and records the
// bodies it received.
func cascadeServer(t *testing.T) (*httptest.Server, map[string]map[string]any) {
	t.Helper()
	received := map[string]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer admin-token" {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(writer, `{"code":"UNAUTHORIZED","message":"sign in"}`)
			return
		}
		body := map[string]any{}
		_ = json.NewDecoder(request.Body).Decode(&body)
		received[request.URL.Path] = body
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/admin/cascade/pairing":
			_, _ = io.WriteString(writer, `{"code":"depsilo-pair-1.abc","url":"http://192.0.2.10:23333","enabled_now":true,"loopback":false,"restart_required":false}`)
		case "/api/v1/admin/cascade/join":
			if body["code"] == "bad" {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(writer, `{"code":"INVALID_PAIRING_CODE","message":"the pairing code is not valid"}`)
				return
			}
			_, _ = io.WriteString(writer, `{"peer":"parent","url":"http://192.0.2.10:23333","bound":12,"skipped":["pypi/intranet"],"insecure_http_allowed":true,"restart_required":false}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("DEPSILO_URL", server.URL)
	t.Setenv("DEPSILO_TOKEN", "admin-token")
	t.Setenv("HOME", t.TempDir())
	return server, received
}

func TestCascadePairPrintsTheCodeForTheGivenAddress(t *testing.T) {
	_, received := cascadeServer(t)
	output, code := captureCLIStdout(t, func() int { return Run("cascade", []string{"pair", "--url", "http://192.0.2.10:23333"}) })
	if code != 0 || !strings.Contains(output, "depsilo-pair-1.abc") || !strings.Contains(output, "Cascade is now on") {
		t.Fatalf("exit=%d output=%s", code, output)
	}
	if received["/api/v1/admin/cascade/pairing"]["url"] != "http://192.0.2.10:23333" {
		t.Fatalf("request = %v", received)
	}
}

func TestCascadeJoinSendsTheCodeAndReportsTheRouting(t *testing.T) {
	_, received := cascadeServer(t)
	output, code := captureCLIStdout(t, func() int {
		return Run("cascade", []string{"join", "depsilo-pair-1.abc", "--name", "home", "--json"})
	})
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil || code != 0 || result["ok"] != true || result["bound"] != float64(12) {
		t.Fatalf("exit=%d output=%s err=%v", code, output, err)
	}
	if body := received["/api/v1/admin/cascade/join"]; body["code"] != "depsilo-pair-1.abc" || body["name"] != "home" || body["route_all"] != true {
		t.Fatalf("request = %v", body)
	}

	_, code = captureCLIStdout(t, func() int { return Run("cascade", []string{"join", "depsilo-pair-1.abc", "--no-route-all"}) })
	if code != 0 || received["/api/v1/admin/cascade/join"]["route_all"] != false {
		t.Fatalf("--no-route-all: exit=%d request=%v", code, received["/api/v1/admin/cascade/join"])
	}
}

func TestCascadeJoinFailsOnARejectedCode(t *testing.T) {
	cascadeServer(t)
	output, code := captureCLIStdout(t, func() int { return Run("cascade", []string{"join", "bad", "--json"}) })
	if code != 1 || !strings.Contains(output, "not valid") {
		t.Fatalf("exit=%d output=%s", code, output)
	}
	if _, code := captureCLIStdout(t, func() int { return Run("cascade", []string{"join"}) }); code != 1 {
		t.Fatalf("missing code exit=%d, want 1", code)
	}
}
