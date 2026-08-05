package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const sensitiveArtifactRequestPath = "/p/project/python/files/_external/v1.aHR0cHM6Ly9jZG4uZXhhbXBsZS93aGVlbC53aGw_c2lnPXNlY3JldA.mac/wheel-1.0-py3-none-any.whl"

func TestRedactedRequestPathHidesSignedArtifactReference(t *testing.T) {
	want := "/p/project/python/files/_external/<redacted>"
	if got := redactedRequestPath(sensitiveArtifactRequestPath); got != want {
		t.Fatalf("redacted path = %q, want %q", got, want)
	}
	ordinary := "/pypi/simple/requests/"
	if got := redactedRequestPath(ordinary); got != ordinary {
		t.Fatalf("ordinary path changed to %q", got)
	}
}

func TestLoggerRedactsSignedArtifactReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.InfoLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restore)

	router := gin.New()
	router.Use(Logger())
	router.NoRoute(func(c *gin.Context) { c.Status(http.StatusNotFound) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, sensitiveArtifactRequestPath, nil))

	entries := logs.FilterMessage("request").All()
	if len(entries) != 1 {
		t.Fatalf("request log entries = %d, want 1", len(entries))
	}
	assertSignedArtifactLogRedacted(t, entries[0].ContextMap()["path"])
}

func TestRecoveryRedactsSignedArtifactReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.ErrorLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restore)

	router := gin.New()
	router.Use(Recovery())
	router.NoRoute(func(*gin.Context) { panic("test panic") })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, sensitiveArtifactRequestPath, nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}

	entries := logs.FilterMessage("panic recovered").All()
	if len(entries) != 1 {
		t.Fatalf("recovery log entries = %d, want 1", len(entries))
	}
	assertSignedArtifactLogRedacted(t, entries[0].ContextMap()["path"])
}

func assertSignedArtifactLogRedacted(t *testing.T, value any) {
	t.Helper()
	path, ok := value.(string)
	if !ok {
		t.Fatalf("logged path = %#v, want string", value)
	}
	if strings.Contains(path, "aHR0cHM6") || strings.Contains(path, "secret") || path != "/p/project/python/files/_external/<redacted>" {
		t.Fatalf("signed artifact path was not safely redacted: %q", path)
	}
}
