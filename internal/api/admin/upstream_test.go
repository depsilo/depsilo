package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/upstream"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func upstreamRouterFixtureWithPrincipal(t *testing.T, count int, canWrite bool, firstURL, firstProxy string) (*upstream.Registry, *gin.Engine) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "upstream-handler.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		urlValue := fmt.Sprintf("https://source-%d.example", i+1)
		proxyValue := ""
		if i == 0 && firstURL != "" {
			urlValue = firstURL
		}
		if i == 0 {
			proxyValue = firstProxy
		}
		record := db.UpstreamRecord{AdapterType: "pypi", Name: fmt.Sprintf("source-%d", i+1), URL: urlValue, Proxy: proxyValue, Priority: i + 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true, SuccessRate: 1}
		if err := database.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	registry, err := upstream.NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewUpstreamHandler(registry)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyPrincipal, middleware.Principal{ID: 1, Username: "operator", Role: "admin", Enabled: true, AuthMethod: middleware.AuthMethodJWT, CanWrite: canWrite})
		c.Next()
	})
	router.GET("/upstreams", handler.List)
	router.POST("/upstreams", handler.Create)
	router.PUT("/upstreams/:id", handler.Update)
	router.DELETE("/upstreams/:id", handler.Delete)
	router.POST("/upstreams/:id/check", handler.Check)
	return registry, router
}

func upstreamRouterFixture(t *testing.T, count int) (*upstream.Registry, *gin.Engine) {
	return upstreamRouterFixtureWithPrincipal(t, count, true, "", "")
}

func upstreamRouterFixtureWithURL(t *testing.T, firstURL string) (*upstream.Registry, *gin.Engine) {
	return upstreamRouterFixtureWithPrincipal(t, 1, true, firstURL, "")
}

func performJSON(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestUpstreamHandlerListEnvelopeAndCreateContract(t *testing.T) {
	registry, router := upstreamRouterFixture(t, 1)
	body := `{"adapter_type":"pypi","name":"secondary","url":"https://secondary.example","proxy":"","priority":2,"probe_mode":"passive","probe_interval":"30m"}`
	w := performJSON(router, http.MethodPost, "/upstreams", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var created adminUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.AdapterType != "pypi" || created.WorkerRunning || created.LastCheckedAt != nil {
		t.Fatalf("created=%#v", created)
	}
	if _, ok := registry.Pools()["pypi"].Find(created.ID); !ok {
		t.Fatal("HTTP success did not publish runtime source")
	}

	w = performJSON(router, http.MethodGet, "/upstreams", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var list upstreamListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("list=%#v", list)
	}
}

func TestUpstreamHandlerExactErrors(t *testing.T) {
	_, router := upstreamRouterFixture(t, 1)
	cases := []struct {
		method, path, body string
		status             int
		code               string
	}{
		{http.MethodPost, "/upstreams", `{`, 400, "BAD_REQUEST"},
		{http.MethodDelete, "/upstreams/not-a-number", ``, 400, "BAD_REQUEST"},
		{http.MethodPost, "/upstreams", `{"adapter_type":"npm","name":"x","url":"https://x.example","priority":1,"probe_mode":"passive","probe_interval":"30m"}`, 409, "ECOSYSTEM_NOT_ACTIVE"},
		{http.MethodPost, "/upstreams", `{"adapter_type":"pypi","name":"bad","url":"file:///tmp/source","proxy":"","priority":1,"probe_mode":"passive","probe_interval":"30m"}`, 422, "INVALID_UPSTREAM"},
		{http.MethodPost, "/upstreams", `{"adapter_type":"pypi","name":"source-1","url":"https://duplicate.example","proxy":"","priority":2,"probe_mode":"passive","probe_interval":"30m"}`, 409, "CONFLICT"},
		{http.MethodPut, "/upstreams/1", `{"adapter_type":"npm","name":"x","url":"https://x.example","proxy":"","priority":1,"probe_mode":"passive","probe_interval":"30m"}`, 422, "IMMUTABLE_ECOSYSTEM"},
		{http.MethodDelete, "/upstreams/1", ``, 409, "LAST_UPSTREAM"},
		{http.MethodPut, "/upstreams/999", `{"adapter_type":"pypi","name":"x","url":"https://x.example","proxy":"","priority":1,"probe_mode":"passive","probe_interval":"30m"}`, 404, "NOT_FOUND"},
	}
	for _, tc := range cases {
		w := performJSON(router, tc.method, tc.path, tc.body)
		var response struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.status || response.Code != tc.code {
			t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestWriteUpstreamErrorMatrix(t *testing.T) {
	cases := []struct {
		err     error
		status  int
		code    string
		message string
	}{
		{upstream.ErrNotFound, 404, "NOT_FOUND", "upstream not found"},
		{upstream.ErrConflict, 409, "CONFLICT", "upstream conflict"},
		{upstream.ErrLastUpstream, 409, "LAST_UPSTREAM", "last upstream"},
		{upstream.ErrEcosystemNotActive, 409, "ECOSYSTEM_NOT_ACTIVE", "ecosystem not active"},
		{upstream.ErrImmutableEcosystem, 422, "IMMUTABLE_ECOSYSTEM", "immutable ecosystem"},
		{upstream.ErrInvalidUpstream, 422, "INVALID_UPSTREAM", "invalid upstream"},
		{fmt.Errorf("%w: reload /srv/private.db", upstream.ErrReconcileFailed), 500, "REGISTRY_RECONCILE_FAILED", "registry reconciliation failed"},
		{errors.New("database unavailable at /srv/private.db"), 500, "INTERNAL_ERROR", "internal server error"},
	}
	logger, logs := observer.New(zap.ErrorLevel)
	restore := zap.ReplaceGlobals(zap.New(logger))
	defer restore()
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		writeUpstreamError(context, tc.err)
		var response struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != tc.status || response.Code != tc.code || response.Message != tc.message {
			t.Fatalf("err=%v status=%d body=%s", tc.err, recorder.Code, recorder.Body.String())
		}
	}
	if logs.Len() != 2 {
		t.Fatalf("server error log count=%d, want 2", logs.Len())
	}
}

func TestUpstreamMutationRejectsMalformedBodiesBeforeRegistryCall(t *testing.T) {
	valid := `{"adapter_type":"pypi","name":"secondary","url":"https://secondary.example","proxy":"","priority":2,"probe_mode":"passive","probe_interval":"30m"}`
	cases := map[string]string{
		"duplicate key":      strings.Replace(valid, `"name":"secondary"`, `"name":"secondary","name":"other"`, 1),
		"null":               strings.Replace(valid, `"proxy":""`, `"proxy":null`, 1),
		"unknown key":        strings.Replace(valid, `"proxy":""`, `"proxy":"","extra":true`, 1),
		"case alias":         strings.Replace(valid, `"name":"secondary"`, `"Name":"secondary"`, 1),
		"trailing value":     valid + ` {}`,
		"missing field":      strings.Replace(valid, `,"probe_interval":"30m"`, ``, 1),
		"wrong string type":  strings.Replace(valid, `"name":"secondary"`, `"name":7`, 1),
		"wrong integer type": strings.Replace(valid, `"priority":2`, `"priority":"2"`, 1),
		"not object":         `[]`,
		"oversize":           `{"adapter_type":"pypi","name":"` + strings.Repeat("x", (1<<20)+1) + `","url":"https://secondary.example","priority":2,"probe_mode":"passive","probe_interval":"30m"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			handler := &UpstreamHandler{}
			router := gin.New()
			router.POST("/upstreams", handler.Create)
			recorder := performJSON(router, http.MethodPost, "/upstreams", body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}

	handler := &UpstreamHandler{}
	router := gin.New()
	router.PUT("/upstreams/:id", handler.Update)
	recorder := performJSON(router, http.MethodPut, "/upstreams/1", `{"adapter_type":"pypi","name":"x","url":"https://x.example","priority":1,"probe_mode":"passive","probe_interval":"30m","URL":"https://alias.example"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("update status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestUpstreamIDOverflowRejectedBeforeRegistryCall(t *testing.T) {
	handler := &UpstreamHandler{}
	router := gin.New()
	router.DELETE("/upstreams/:id", handler.Delete)
	recorder := performJSON(router, http.MethodDelete, "/upstreams/18446744073709551616", "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestUpstreamHandlerDeleteResponseIsExact(t *testing.T) {
	registry, router := upstreamRouterFixture(t, 2)
	id := registry.Pools()["pypi"].Snapshot()[1].ID
	w := performJSON(router, http.MethodDelete, fmt.Sprintf("/upstreams/%d", id), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 2 || uint(response["deleted_id"].(float64)) != id || response["adapter_type"] != "pypi" {
		t.Fatalf("response=%#v", response)
	}
}

func TestUpstreamHandlerCheckReturns200ForUnhealthyNetwork(t *testing.T) {
	_, router := upstreamRouterFixtureWithURL(t, "http://127.0.0.1:1")
	w := performJSON(router, http.MethodPost, "/upstreams/1/check", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response checkUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Check.Healthy {
		t.Fatal("check unexpectedly healthy")
	}
	if response.Check.Error == nil || *response.Check.Error == "" {
		t.Fatal("missing network error")
	}
}

func TestUpstreamHandlerMasksCredentialsForReadonlyResponses(t *testing.T) {
	_, readonly := upstreamRouterFixtureWithPrincipal(t, 1, false, "http://source-user:source-secret@source.example", "http://proxy-user:proxy-secret@proxy.example:8080")
	w := performJSON(readonly, http.MethodGet, "/upstreams", "")
	var list upstreamListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Items[0].URL != "http://source.example/***" || list.Items[0].Proxy != "http://proxy.example:8080/***" {
		t.Fatalf("readonly=%#v", list.Items[0])
	}

	createBody := `{"adapter_type":"pypi","name":"masked","url":"https://alice:secret@masked.example","proxy":"http://bob:secret@proxy.example","priority":2,"probe_mode":"passive","probe_interval":"30m"}`
	w = performJSON(readonly, http.MethodPost, "/upstreams", createBody)
	var created adminUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.URL != "https://masked.example/***" || created.Proxy != "http://proxy.example/***" {
		t.Fatalf("created=%#v", created)
	}

	updateBody := `{"adapter_type":"pypi","name":"masked","url":"http://carol:secret@127.0.0.1:1","proxy":"http://dave:secret@127.0.0.1:1","priority":2,"probe_mode":"passive","probe_interval":"30m"}`
	w = performJSON(readonly, http.MethodPut, fmt.Sprintf("/upstreams/%d", created.ID), updateBody)
	var updated adminUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.URL != "http://127.0.0.1:1/***" || updated.Proxy != "http://127.0.0.1:1/***" {
		t.Fatalf("updated=%#v", updated)
	}

	w = performJSON(readonly, http.MethodPost, fmt.Sprintf("/upstreams/%d/check", created.ID), "")
	var checked checkUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &checked); err != nil {
		t.Fatal(err)
	}
	if checked.Upstream.URL != "http://127.0.0.1:1/***" || checked.Upstream.Proxy != "http://127.0.0.1:1/***" {
		t.Fatalf("checked=%#v", checked.Upstream)
	}
	if checked.Check.Error == nil || *checked.Check.Error != "upstream check failed" || strings.Contains(w.Body.String(), "carol") || strings.Contains(w.Body.String(), "dave") || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("readonly check leaked operational error: %s", w.Body.String())
	}

	_, writable := upstreamRouterFixtureWithPrincipal(t, 1, true, "http://source-user:source-secret@source.example", "http://proxy-user:proxy-secret@proxy.example:8080")
	w = performJSON(writable, http.MethodGet, "/upstreams", "")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Items[0].URL != "http://source-user:source-secret@source.example" {
		t.Fatalf("writable url=%q", list.Items[0].URL)
	}
}

func TestWritableUpstreamCheckRetainsOperationalError(t *testing.T) {
	_, router := upstreamRouterFixtureWithPrincipal(t, 1, true, "http://127.0.0.1:1", "")
	recorder := performJSON(router, http.MethodPost, "/upstreams/1/check", "")
	var response checkUpstreamResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Check.Error == nil || *response.Check.Error == "" || *response.Check.Error == "upstream check failed" {
		t.Fatalf("response=%s", recorder.Body.String())
	}
}
