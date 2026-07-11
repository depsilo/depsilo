package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
)

const settingsHandlerConfig = `# handler fixture
[server]
host = "127.0.0.1"
port = 23333
log_level = "info"
[database]
driver = "sqlite"
dsn = "./data/depsilo.db"
[storage]
type = "local"
path = "./data/cache"
[cache]
max_size_gb = 20
ttl_index = "5m"
ttl_blob = "72h"
lru_threshold = 90
[auth]
enabled = true
jwt_secret = "test-secret"
token_ttl = "168h"
[custom]
keep = "untouched"
`

var settingsHandlerEnv = []string{
	"DEPSILO_SERVER_HOST", "DEPSILO_SERVER_PORT", "DEPSILO_SERVER_LOG_LEVEL",
	"DEPSILO_DATABASE_DRIVER", "DEPSILO_STORAGE_TYPE", "DEPSILO_STORAGE_PATH",
	"DEPSILO_CACHE_MAX_SIZE_GB", "DEPSILO_CACHE_TTL_INDEX", "DEPSILO_CACHE_TTL_BLOB",
	"DEPSILO_CACHE_LRU_THRESHOLD", "DEPSILO_AUTH_TOKEN_TTL",
}

type trackingSettingsStore struct {
	settingsStore
	updateCalls int
}

func (s *trackingSettingsStore) Update(ctx context.Context, patch config.SettingsPatch) (config.SettingsUpdateResult, error) {
	s.updateCalls++
	return s.settingsStore.Update(ctx, patch)
}

func newTrackedSettingsHandlerFixture(t *testing.T) (*gin.Engine, string, zap.AtomicLevel, *trackingSettingsStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	for _, name := range settingsHandlerEnv {
		t.Setenv(name, "")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(settingsHandlerConfig), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	level, err := zap.ParseAtomicLevel(cfg.Server.LogLevel)
	if err != nil {
		t.Fatal(err)
	}
	store := &trackingSettingsStore{settingsStore: config.NewStore(path, cfg, level)}
	handler := NewSettingsHandler(store)
	router := gin.New()
	router.GET("/settings", handler.Get)
	router.PUT("/settings", handler.Update)
	return router, path, level, store
}

func newSettingsHandlerFixture(t *testing.T) (*gin.Engine, string, zap.AtomicLevel) {
	router, path, level, _ := newTrackedSettingsHandlerFixture(t)
	return router, path, level
}

func performSettingsRequest(router http.Handler, method, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/settings", strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertSettingPaths(t *testing.T, got, want []config.SettingPath) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
}

func settingsResponseCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, recorder.Body.String())
	}
	return response.Code
}

func settingsErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var response struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, recorder.Body.String())
	}
	return response.Code, response.Message
}

func TestSettingsGetReturnsCompleteConfiguredEffectiveContract(t *testing.T) {
	router, path, _ := newSettingsHandlerFixture(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`ttl_blob = "72h"`), []byte(`ttl_blob = "96h"`), 1)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}

	recorder := performSettingsRequest(router, http.MethodGet, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response AdminSettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Configured.Server.Host != "127.0.0.1" || response.Configured.Server.Port != 23333 || response.Configured.Database.Driver != "sqlite" || response.Configured.Storage.Type != "local" || response.Configured.Storage.Path != "./data/cache" {
		t.Fatalf("configured identity = %+v", response.Configured)
	}
	if response.Configured.Cache.TTLBlob != "96h" || response.Effective.Cache.TTLBlob != "72h" || response.Configured.Auth.TokenTTL != "168h" {
		t.Fatalf("configured/effective = %+v / %+v", response.Configured, response.Effective)
	}
	assertSettingPaths(t, response.PendingRestart, []config.SettingPath{config.SettingCacheTTLBlob})
	if response.Sources == nil || response.Overrides == nil || response.Editable == nil || len(response.Sources) != 11 || len(response.Editable) != 6 || !response.ConfigWritable {
		t.Fatalf("metadata = %+v", response)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	var configured map[string]json.RawMessage
	if err := json.Unmarshal(top["configured"], &configured); err != nil {
		t.Fatal(err)
	}
	var auth map[string]json.RawMessage
	if err := json.Unmarshal(configured["auth"], &auth); err != nil {
		t.Fatal(err)
	}
	if _, exists := auth["enabled"]; exists {
		t.Fatalf("auth.enabled leaked in %s", configured["auth"])
	}
}

func TestSettingsPutReturnsCompleteAppliedAndRestartContract(t *testing.T) {
	router, path, level := newSettingsHandlerFixture(t)
	body := `{"server":{"log_level":"debug"},"cache":{"ttl_blob":"96h"}}`
	recorder := performSettingsRequest(router, http.MethodPut, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response UpdateAdminSettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	assertSettingPaths(t, response.Changed, []config.SettingPath{config.SettingServerLogLevel, config.SettingCacheTTLBlob})
	assertSettingPaths(t, response.AppliedNow, []config.SettingPath{config.SettingServerLogLevel})
	assertSettingPaths(t, response.RestartRequired, []config.SettingPath{config.SettingCacheTTLBlob})
	assertSettingPaths(t, response.BlockedByOverride, []config.SettingPath{})
	assertSettingPaths(t, response.PendingRestart, []config.SettingPath{config.SettingCacheTTLBlob})
	if response.Configured.Server.LogLevel != "debug" || response.Effective.Server.LogLevel != "debug" || response.Configured.Cache.TTLBlob != "96h" || response.Effective.Cache.TTLBlob != "72h" {
		t.Fatalf("response = %+v", response)
	}
	if response.Configured.Server.Host != "127.0.0.1" || response.Configured.Database.Driver != "sqlite" || response.Configured.Storage.Path != "./data/cache" || response.Configured.Auth.TokenTTL != "168h" || len(response.Sources) != 11 || len(response.Editable) != 6 {
		t.Fatalf("incomplete response = %+v", response)
	}
	if level.Level() != zap.DebugLevel {
		t.Fatalf("level = %s", level.Level())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`log_level = "debug"`)) || !bytes.Contains(data, []byte(`ttl_blob = "96h"`)) || !bytes.Contains(data, []byte(`keep = "untouched"`)) {
		t.Fatalf("disk =\n%s", data)
	}
}

func TestSettingsPutRejectsEmptyUnknownAndTrailingJSON(t *testing.T) {
	tests := []string{
		`{}`,
		`null`,
		`{"auth":{"enabled":false}}`,
		`{"server":{"host":"0.0.0.0"}}`,
		`{"Server":{"log_level":"debug"}}`,
		`{"server":{"LOG_LEVEL":"debug"}}`,
		`{"cache":{"TTL_BLOB":"96h"}}`,
		`{"auth":{"TOKEN_TTL":"24h"}}`,
		`{"server":{"log_level":"debug","LOG_LEVEL":"info"}}`,
		`{"server":null,"cache":{"ttl_blob":"96h"}}`,
		`{"server":{"log_level":null},"cache":{"ttl_blob":"96h"}}`,
		`{"cache":{"ttl_blob":"96h"},"cache":{"ttl_blob":"24h"}}`,
		`{"cache":{"ttl_blob":"96h","ttl_blob":"24h"}}`,
		`{"cache":{"ttl_blob":{"value":"96h","value":"24h"}}}`,
		`{"cache":{"ttl_blob":"96h"}} {"cache":{"ttl_blob":"24h"}}`,
		`{`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			router, path, _, store := newTrackedSettingsHandlerFixture(t)
			before, _ := os.ReadFile(path)
			recorder := performSettingsRequest(router, http.MethodPut, body)
			if recorder.Code != http.StatusBadRequest || settingsResponseCode(t, recorder) != "BAD_REQUEST" {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(after, before) {
				t.Fatal("bad request changed disk")
			}
			if store.updateCalls != 0 {
				t.Fatalf("Store.Update calls = %d, want 0", store.updateCalls)
			}
		})
	}
}

func TestSettingsPutRejectsOversizedBodyBeforeStoreOrDiskMutation(t *testing.T) {
	router, path, _, store := newTrackedSettingsHandlerFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"server":{"log_level":"debug"}}` + strings.Repeat(" ", int(maxSettingsRequestBodyBytes))
	recorder := performSettingsRequest(router, http.MethodPut, body)
	code, message := settingsErrorResponse(t, recorder)
	if recorder.Code != http.StatusBadRequest || code != "BAD_REQUEST" || message != "request body too large" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("oversized request changed disk")
	}
	if store.updateCalls != 0 {
		t.Fatalf("Store.Update calls = %d, want 0", store.updateCalls)
	}
}

func TestValidateSettingsJSONRejectsDuplicateKeysAtEveryDepth(t *testing.T) {
	tests := []string{
		`{"server":{"log_level":{"inner":{"key":1,"key":2}}}}`,
		`{"server":{"log_level":[{"inner":{"key":1,"key":2}}]}}`,
	}
	for _, body := range tests {
		err := validateSettingsJSON([]byte(body))
		if err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
			t.Fatalf("validateSettingsJSON(%s) error = %v, want duplicate-key error", body, err)
		}
	}
}

func TestSettingsPutRejectsInvalidValuesWithoutDiskMutation(t *testing.T) {
	tests := []string{
		`{"cache":{"ttl_index":"bad"}}`,
		`{"cache":{"max_size_gb":0}}`,
		`{"cache":{"lru_threshold":101}}`,
		`{"auth":{"token_ttl":"never"}}`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			router, path, _ := newSettingsHandlerFixture(t)
			before, _ := os.ReadFile(path)
			recorder := performSettingsRequest(router, http.MethodPut, body)
			if recorder.Code != http.StatusUnprocessableEntity || settingsResponseCode(t, recorder) != "INVALID_SETTING" {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(after, before) {
				t.Fatal("invalid setting changed disk")
			}
		})
	}
}

type stubSettingsStore struct {
	snapshot    config.SettingsState
	update      config.SettingsUpdateResult
	snapshotErr error
	updateErr   error
}

func (s *stubSettingsStore) Snapshot(context.Context) (config.SettingsState, error) {
	return s.snapshot, s.snapshotErr
}

func (s *stubSettingsStore) Update(context.Context, config.SettingsPatch) (config.SettingsUpdateResult, error) {
	return s.update, s.updateErr
}

func TestSettingsHandlerMapsStoreErrors(t *testing.T) {
	tests := []struct {
		name, method string
		err          error
		status       int
		code         string
		message      string
	}{
		{"get read failure", http.MethodGet, &config.StoreError{Code: config.StoreConfigReadFailed, Err: errors.New("read failed")}, 500, "CONFIG_READ_FAILED", "read failed"},
		{"put invalid", http.MethodPut, &config.StoreError{Code: config.StoreInvalidSetting, Err: errors.New("invalid")}, 422, "INVALID_SETTING", "invalid"},
		{"put readonly", http.MethodPut, &config.StoreError{Code: config.StoreConfigReadOnly, Err: errors.New("readonly")}, 409, "CONFIG_READ_ONLY", "readonly"},
		{"put read failure", http.MethodPut, &config.StoreError{Code: config.StoreConfigReadFailed, Err: errors.New("read failed")}, 500, "CONFIG_READ_FAILED", "read failed"},
		{"put write failure", http.MethodPut, &config.StoreError{Code: config.StoreConfigWriteFailed, Err: errors.New("write failed")}, 500, "CONFIG_WRITE_FAILED", "write failed"},
		{"get unexpected", http.MethodGet, errors.New("/secret/config.toml exploded"), 500, "INTERNAL_ERROR", "internal server error"},
		{"put unknown store code", http.MethodPut, &config.StoreError{Code: config.StoreErrorCode("FUTURE_ERROR"), Err: errors.New("/secret/store path")}, 500, "INTERNAL_ERROR", "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubSettingsStore{}
			if tt.method == http.MethodGet {
				stub.snapshotErr = tt.err
			} else {
				stub.updateErr = tt.err
			}
			handler := NewSettingsHandler(stub)
			router := gin.New()
			router.GET("/settings", handler.Get)
			router.PUT("/settings", handler.Update)
			body := ""
			if tt.method == http.MethodPut {
				body = `{"server":{"log_level":"debug"}}`
			}
			recorder := performSettingsRequest(router, tt.method, body)
			code, message := settingsErrorResponse(t, recorder)
			if recorder.Code != tt.status || code != tt.code || message != tt.message {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
