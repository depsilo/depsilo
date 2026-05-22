//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	return resp
}

func assertStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected status %d, got %d. Body: %s", expected, resp.StatusCode, string(body))
	}
}

func assertBodyContains(t *testing.T, resp *http.Response, substr string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), substr) {
		t.Errorf("body does not contain %q. Got: %s", substr, string(body[:min(len(body), 500)]))
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func getFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("service not ready at %s after %v", url, timeout)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// httpPost sends a POST request with an optional body.
func httpPost(t *testing.T, url string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		t.Fatalf("create POST request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// adminToken is the cached JWT for the default admin user.
// It is populated lazily by loginAdmin() on first call.
var (
	adminTokenOnce  sync.Once
	adminTokenValue string
)

// loginAdmin logs in as the default admin (admin/admin) and caches the token.
// Subsequent calls return the cached token.
func loginAdmin(t *testing.T) string {
	t.Helper()
	adminTokenOnce.Do(func() {
		body := strings.NewReader(`{"username":"admin","password":"admin"}`)
		req, err := http.NewRequest(http.MethodPost, depsiloURL+"/api/v1/auth/login", body)
		if err != nil {
			t.Errorf("create login request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("login request: %v", err)
			return
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("login failed: status %d, body: %s", resp.StatusCode, raw)
			return
		}
		// Extract token from {"token":"...","expires_at":...,"user":{...}}
		var result struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Errorf("unmarshal login response: %v; body: %s", err, raw)
			return
		}
		adminTokenValue = result.Token
	})
	return adminTokenValue
}

// adminGet sends an authenticated GET request using the default admin token.
func adminGet(t *testing.T, url string) *http.Response {
	t.Helper()
	token := loginAdmin(t)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// adminPost sends an authenticated POST request using the default admin token.
func adminPost(t *testing.T, url string, body io.Reader) *http.Response {
	t.Helper()
	token := loginAdmin(t)
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// adminPut sends an authenticated PUT request using the default admin token.
func adminPut(t *testing.T, url string, body io.Reader) *http.Response {
	t.Helper()
	token := loginAdmin(t)
	req, err := http.NewRequest(http.MethodPut, url, body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	return resp
}

// adminDelete sends an authenticated DELETE request using the default admin token.
func adminDelete(t *testing.T, url string) *http.Response {
	t.Helper()
	token := loginAdmin(t)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", url, err)
	}
	return resp
}
