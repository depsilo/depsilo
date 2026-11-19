package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// publishOrderS3Backend records the object requests a staged S3 write sends,
// interleaved with the publish hook, so tests can see what became visible
// before publication.
type publishOrderS3Backend struct {
	mu     sync.Mutex
	events []string
}

func (backend *publishOrderS3Backend) record(event string) {
	backend.mu.Lock()
	backend.events = append(backend.events, event)
	backend.mu.Unlock()
}

func (backend *publishOrderS3Backend) recorded() string {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return strings.Join(backend.events, ",")
}

func (backend *publishOrderS3Backend) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	_, _ = io.Copy(io.Discard, request.Body)
	_ = request.Body.Close()
	if request.URL.Path == "/cache/"+s3ReadinessMarkerKey {
		response.WriteHeader(http.StatusOK)
		return
	}
	query := request.URL.Query()
	switch {
	case request.Method == http.MethodPost && query.Has("uploads"):
		backend.record("create")
		_, _ = fmt.Fprint(response, `<InitiateMultipartUploadResult><Bucket>cache</Bucket><Key>k</Key><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`)
	case request.Method == http.MethodPut && query.Has("partNumber"):
		backend.record("part")
		response.Header().Set("ETag", `"part"`)
		response.WriteHeader(http.StatusOK)
	case request.Method == http.MethodPost && query.Has("uploadId"):
		backend.record("complete")
		_, _ = fmt.Fprint(response, `<CompleteMultipartUploadResult><Bucket>cache</Bucket><Key>k</Key><ETag>"done"</ETag></CompleteMultipartUploadResult>`)
	case request.Method == http.MethodDelete && query.Has("uploadId"):
		backend.record("abort")
		response.WriteHeader(http.StatusNoContent)
	case request.Method == http.MethodPut:
		backend.record("put")
		response.Header().Set("ETag", `"object"`)
		response.WriteHeader(http.StatusOK)
	default:
		writeS3TestError(response, http.StatusNotFound, "NoSuchKey")
	}
}

func TestS3PutStagedMakesObjectsVisibleOnlyAfterPublish(t *testing.T) {
	large := bytes.Repeat([]byte("x"), s3MultipartPartSize+1)
	refused := errors.New("publication refused")
	tests := []struct {
		name       string
		body       io.Reader
		size       int64
		publishErr error
		want       string
	}{
		{name: "known size seekable", body: bytes.NewReader([]byte("small")), size: 5, want: "publish,put"},
		{name: "unknown size small", body: strings.NewReader("small"), size: -1, want: "publish,put"},
		{name: "known size multipart", body: io.LimitReader(bytes.NewReader(large), int64(len(large))), size: int64(len(large)), want: "create,part,part,publish,complete"},
		{name: "unknown size multipart", body: io.LimitReader(bytes.NewReader(large), int64(len(large))), size: -1, want: "create,part,part,publish,complete"},
		{name: "refused small", body: strings.NewReader("small"), size: 5, publishErr: refused, want: "publish"},
		{name: "refused multipart", body: io.LimitReader(bytes.NewReader(large), int64(len(large))), size: int64(len(large)), publishErr: refused, want: "create,part,part,publish,abort"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &publishOrderS3Backend{}
			server := httptest.NewServer(backend)
			t.Cleanup(server.Close)
			storage, err := NewS3Storage(server.URL, "cache", "us-east-1", "access", "secret")
			if err != nil {
				t.Fatal(err)
			}

			err = storage.PutStaged(context.Background(), "pypi/files/a.whl", test.body, test.size, "application/octet-stream",
				func() error {
					backend.record("publish")
					return test.publishErr
				})
			if test.publishErr != nil {
				if !errors.Is(err, test.publishErr) {
					t.Fatalf("PutStaged error = %v, want the publish error", err)
				}
			} else if err != nil {
				t.Fatalf("PutStaged: %v", err)
			}
			if got := backend.recorded(); got != test.want {
				t.Fatalf("requests = %q, want %q", got, test.want)
			}
		})
	}
}
