package pypi

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

// TestSignedArtifactRouteWithUV exercises the client behavior that a plain
// handler GET cannot cover: uv must recognize the final URL segment as a wheel,
// install it, and then receive the same bytes from Depsilo's cache. The test is
// skipped in short runs and on builders that do not provide uv + Python.
func TestSignedArtifactRouteWithUV(t *testing.T) {
	if testing.Short() {
		t.Skip("real package-client test")
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("uv is not installed")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}

	gin.SetMode(gin.TestMode)
	wheel := buildMinimalUniversalWheel(t)
	var artifactRequests atomic.Int64
	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/artifacts/demo_pkg-1.0-py3-none-any.whl" {
			http.NotFound(w, request)
			return
		}
		artifactRequests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(wheel)
	}))
	defer artifactServer.Close()

	indexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/demo-pkg/" {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body><a href="`+artifactServer.URL+`/artifacts/demo_pkg-1.0-py3-none-any.whl">demo_pkg-1.0-py3-none-any.whl</a></body></html>`)
	}))
	defer indexServer.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "uv-client.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})

	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock-index", URL: indexServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewWithOptions(
		manager,
		upstream.NewPrioritySelector(pool),
		config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour},
		database,
		Options{
			PathPrefix:         "/pypi-test-wheels",
			AdapterID:          "extra:test-wheels",
			UpstreamSimplePath: "/",
			ArtifactSigningKey: testArtifactSigningKey,
			ArtifactSelector:   upstream.NewEgressSelector(pool),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/pypi-test-wheels"))
	depsiloServer := httptest.NewServer(router)
	defer depsiloServer.Close()

	for attempt := 1; attempt <= 2; attempt++ {
		target := filepath.Join(t.TempDir(), "site-packages")
		command := exec.Command(
			uv, "pip", "install",
			"--python", python,
			"--target", target,
			"--index-url", depsiloServer.URL+"/pypi-test-wheels/simple/",
			"--no-deps", "--no-cache", "--no-progress", "--no-config",
			"demo-pkg==1.0",
		)
		command.Env = append(os.Environ(),
			"UV_PYTHON_DOWNLOADS=never",
			"UV_EXTRA_INDEX_URL=",
			"HTTP_PROXY=", "HTTPS_PROXY=", "ALL_PROXY=",
			"NO_PROXY=127.0.0.1,localhost",
		)
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			t.Fatalf("uv install attempt %d: %v\n%s", attempt, runErr, output)
		}
		if _, err := os.Stat(filepath.Join(target, "demo_pkg", "__init__.py")); err != nil {
			t.Fatalf("uv did not install the signed wheel on attempt %d: %v", attempt, err)
		}
	}

	if got := artifactRequests.Load(); got != 1 {
		t.Fatalf("artifact origin requests = %d, want 1 after two uv installs", got)
	}
}

func buildMinimalUniversalWheel(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := []struct {
		name string
		body string
	}{
		{name: "demo_pkg/__init__.py", body: "__version__ = '1.0'\n"},
		{name: "demo_pkg-1.0.dist-info/METADATA", body: "Metadata-Version: 2.1\nName: demo-pkg\nVersion: 1.0\n"},
		{name: "demo_pkg-1.0.dist-info/WHEEL", body: "Wheel-Version: 1.0\nGenerator: depsilo-test\nRoot-Is-Purelib: true\nTag: py3-none-any\n"},
		{name: "demo_pkg-1.0.dist-info/RECORD", body: "demo_pkg/__init__.py,,\ndemo_pkg-1.0.dist-info/METADATA,,\ndemo_pkg-1.0.dist-info/WHEEL,,\ndemo_pkg-1.0.dist-info/RECORD,,\n"},
	}
	for _, file := range files {
		entry, err := writer.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, file.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
