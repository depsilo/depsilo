package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type readinessS3Backend struct {
	mu sync.Mutex

	marker             bool
	bucketMissing      bool
	denyCreate         bool
	denyMarkerGet      bool
	listRequests       int
	markerPuts         int
	markerPutRequests  int
	createRequests     int
	headBucketRequests int
	createBody         string
	payloadHash        string
	requests           []string
}

func (backend *readinessS3Backend) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	_ = request.Body.Close()

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if request.URL.Query().Get("list-type") != "" {
		backend.listRequests++
		writeS3TestError(response, http.StatusForbidden, "AccessDenied")
		return
	}

	switch {
	case request.Method == http.MethodHead && request.URL.Path == "/cache":
		backend.headBucketRequests++
		backend.requests = append(backend.requests, "head-bucket")
		writeS3TestError(response, http.StatusNotFound, "NotFound")
	case request.Method == http.MethodPut && request.URL.Path == "/cache":
		backend.createRequests++
		backend.requests = append(backend.requests, "create-bucket")
		backend.createBody = string(body)
		if backend.denyCreate {
			writeS3TestError(response, http.StatusForbidden, "AccessDenied")
			return
		}
		backend.bucketMissing = false
		response.WriteHeader(http.StatusOK)
	case request.Method == http.MethodPut && request.URL.Path == "/cache/"+s3ReadinessMarkerKey:
		backend.markerPutRequests++
		backend.requests = append(backend.requests, "put-marker")
		if backend.bucketMissing {
			writeS3TestError(response, http.StatusNotFound, "NoSuchBucket")
			return
		}
		backend.marker = true
		backend.markerPuts++
		response.Header().Set("ETag", `"readiness"`)
		response.WriteHeader(http.StatusOK)
	case request.Method == http.MethodPut && request.URL.Path == "/cache/signed-stream":
		backend.payloadHash = request.Header.Get("X-Amz-Content-Sha256")
		response.Header().Set("ETag", `"signed-stream"`)
		response.WriteHeader(http.StatusOK)
	case request.Method == http.MethodGet && request.URL.Path == "/cache/"+s3ReadinessMarkerKey:
		backend.requests = append(backend.requests, "get-marker")
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

func TestS3CreateBucketUsesConfiguredAWSRegion(t *testing.T) {
	backend := &readinessS3Backend{bucketMissing: true}
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)

	if _, err := NewS3Storage(server.URL, "cache", "eu-west-1", "access", "secret"); err != nil {
		t.Fatalf("create regional S3 bucket: %v", err)
	}
	backend.mu.Lock()
	createBody := backend.createBody
	backend.mu.Unlock()
	if !strings.Contains(createBody, "<LocationConstraint>eu-west-1</LocationConstraint>") {
		t.Fatalf("CreateBucket body = %q, want eu-west-1 LocationConstraint", createBody)
	}
}

func TestS3StorageCreatesBucketOnlyAfterMarkerPutReportsNoSuchBucket(t *testing.T) {
	backend := &readinessS3Backend{bucketMissing: true}
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)

	storage, err := NewS3Storage(server.URL, "cache", "us-east-1", "access", "secret")
	if err != nil {
		t.Fatalf("create missing S3 bucket: %v", err)
	}
	if err := storage.CheckReady(context.Background()); err != nil {
		t.Fatalf("read marker from created S3 bucket: %v", err)
	}

	backend.mu.Lock()
	requests := append([]string(nil), backend.requests...)
	createRequests := backend.createRequests
	markerPutRequests := backend.markerPutRequests
	headBucketRequests := backend.headBucketRequests
	createBody := backend.createBody
	backend.mu.Unlock()
	if got, want := strings.Join(requests, ","), "put-marker,create-bucket,put-marker,get-marker"; got != want {
		t.Fatalf("S3 initialization requests = %q, want %q", got, want)
	}
	if createRequests != 1 || markerPutRequests != 2 {
		t.Fatalf("CreateBucket/marker Put requests = %d/%d, want 1/2", createRequests, markerPutRequests)
	}
	if headBucketRequests != 0 {
		t.Fatalf("S3 initialization issued %d HeadBucket requests, want 0", headBucketRequests)
	}
	if createBody != "" {
		t.Fatalf("us-east-1 CreateBucket body = %q, want empty body", createBody)
	}
}

func TestS3KnownLengthNonSeekableStreamUsesSignedPayload(t *testing.T) {
	backend := &readinessS3Backend{}
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)

	storage, err := NewS3Storage(server.URL, "cache", "us-east-1", "access", "secret")
	if err != nil {
		t.Fatalf("open S3 storage: %v", err)
	}
	payload := "signed over plain HTTP for a hardened S3 policy"
	nonSeekable := io.LimitReader(strings.NewReader(payload), int64(len(payload)))
	if err := storage.Put(context.Background(), "signed-stream", nonSeekable, int64(len(payload)), "text/plain"); err != nil {
		t.Fatalf("Put non-seekable stream: %v", err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	backend.mu.Lock()
	payloadHash := backend.payloadHash
	backend.mu.Unlock()
	if payloadHash != wantHash {
		t.Fatalf("X-Amz-Content-Sha256 = %q, want signed hash %q", payloadHash, wantHash)
	}
}

func TestS3StorageUsesExistingBucketWithoutCreatePermission(t *testing.T) {
	backend := &readinessS3Backend{denyCreate: true}
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)

	storage, err := NewS3Storage(server.URL, "cache", "us-east-1", "access", "secret")
	if err != nil {
		t.Fatalf("open existing S3 bucket with object-only credentials: %v", err)
	}
	if err := storage.CheckReady(context.Background()); err != nil {
		t.Fatalf("read existing S3 bucket: %v", err)
	}
	backend.mu.Lock()
	createRequests := backend.createRequests
	backend.mu.Unlock()
	if createRequests != 0 {
		t.Fatalf("existing bucket received %d CreateBucket requests, want 0", createRequests)
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
