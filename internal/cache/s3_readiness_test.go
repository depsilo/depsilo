package cache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type readinessS3Backend struct {
	mu sync.Mutex

	marker        bool
	denyMarkerGet bool
	listRequests  int
	markerPuts    int
}

func (backend *readinessS3Backend) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	_, _ = io.Copy(io.Discard, request.Body)
	_ = request.Body.Close()

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if request.URL.Query().Get("list-type") != "" {
		backend.listRequests++
		writeS3TestError(response, http.StatusForbidden, "AccessDenied")
		return
	}

	switch {
	case request.Method == http.MethodPut && request.URL.Path == "/cache":
		response.WriteHeader(http.StatusOK)
	case request.Method == http.MethodPut && request.URL.Path == "/cache/"+s3ReadinessMarkerKey:
		backend.marker = true
		backend.markerPuts++
		response.Header().Set("ETag", `"readiness"`)
		response.WriteHeader(http.StatusOK)
	case request.Method == http.MethodGet && request.URL.Path == "/cache/"+s3ReadinessMarkerKey:
		if backend.denyMarkerGet {
			writeS3TestError(response, http.StatusForbidden, "AccessDenied")
			return
		}
		if !backend.marker {
			writeS3TestError(response, http.StatusNotFound, "NoSuchKey")
			return
		}
		response.Header().Set("Content-Length", "0")
		response.WriteHeader(http.StatusOK)
	default:
		writeS3TestError(response, http.StatusNotFound, "NoSuchKey")
	}
}

func (backend *readinessS3Backend) snapshot() (listRequests, markerPuts int) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.listRequests, backend.markerPuts
}

func writeS3TestError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/xml")
	response.WriteHeader(status)
	_, _ = fmt.Fprintf(response, "<Error><Code>%s</Code><Message>test</Message></Error>", code)
}

func TestS3ReadinessUsesStableMarkerWithoutListBucket(t *testing.T) {
	backend := &readinessS3Backend{}
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)

	storage, err := NewS3Storage(server.URL, "cache", "us-east-1", "access", "secret")
	if err != nil {
		t.Fatalf("open S3 storage: %v", err)
	}
	if err := storage.CheckReady(context.Background()); err != nil {
		t.Fatalf("first readiness check: %v", err)
	}
	if err := storage.CheckReady(context.Background()); err != nil {
		t.Fatalf("repeat readiness check: %v", err)
	}

	listRequests, markerPuts := backend.snapshot()
	if listRequests != 0 {
		t.Fatalf("readiness issued %d ListObjects requests, want 0", listRequests)
	}
	if markerPuts != 1 {
		t.Fatalf("readiness marker PUT count = %d, want one startup write", markerPuts)
	}
}

func TestS3ReadinessReportsGetObjectPermissionFailure(t *testing.T) {
	backend := &readinessS3Backend{}
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)

	storage, err := NewS3Storage(server.URL, "cache", "us-east-1", "access", "secret")
	if err != nil {
		t.Fatalf("open S3 storage: %v", err)
	}
	backend.mu.Lock()
	backend.denyMarkerGet = true
	backend.mu.Unlock()

	if err := storage.CheckReady(context.Background()); err == nil {
		t.Fatal("readiness accepted an S3 backend that denied GetObject")
	}
	listRequests, _ := backend.snapshot()
	if listRequests != 0 {
		t.Fatalf("failed readiness issued %d ListObjects requests, want 0", listRequests)
	}
}
