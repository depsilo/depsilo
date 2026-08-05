package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"github.com/gin-gonic/gin"
)

type unavailableReadinessStorage struct{ cache.Storage }

func (unavailableReadinessStorage) CheckReady(context.Context) error {
	return errors.New("storage unavailable")
}

func TestReadinessHandlerChecksDatabaseAndStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/ready", nil)
	readinessHandler(database, storage)(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); body == "" || !containsAll(body, `"status":"ready"`, `"database":"ready"`, `"storage":"ready"`) {
		t.Fatalf("unexpected readiness body: %s", body)
	}
}

func TestReadinessHandlerReturns503WithoutLeakingDependencyErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/ready", nil)
	readinessHandler(database, unavailableReadinessStorage{Storage: storage})(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !containsAll(body, `"status":"not_ready"`, `"storage":"unavailable"`) {
		t.Fatalf("unexpected readiness body: %s", body)
	}
	if containsAll(body, "storage unavailable") {
		t.Fatalf("readiness response leaked dependency error: %s", body)
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
