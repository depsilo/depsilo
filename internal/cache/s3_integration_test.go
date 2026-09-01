//go:build s3integration

package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"depsilo/internal/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3StorageContract(t *testing.T) {
	for _, name := range []string{
		"DEPSILO_STORAGE_TYPE",
		"DEPSILO_STORAGE_ENDPOINT",
		"DEPSILO_STORAGE_BUCKET",
		"DEPSILO_STORAGE_REGION",
		"DEPSILO_STORAGE_ACCESS_KEY",
		"DEPSILO_STORAGE_SECRET_KEY",
	} {
		if os.Getenv(name) == "" {
			t.Fatalf("S3 contract test requires %s", name)
		}
	}
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DEPSILO_CONFIG", "")
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "s3-contract-bootstrap-token-0123456789")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load env-only S3 config: %v", err)
	}
	if cfg.Storage.Type != "s3" ||
		cfg.Storage.Endpoint != os.Getenv("DEPSILO_STORAGE_ENDPOINT") ||
		cfg.Storage.Bucket != os.Getenv("DEPSILO_STORAGE_BUCKET") ||
		cfg.Storage.Region != os.Getenv("DEPSILO_STORAGE_REGION") ||
		cfg.Storage.AccessKey != os.Getenv("DEPSILO_STORAGE_ACCESS_KEY") ||
		cfg.Storage.SecretKey != os.Getenv("DEPSILO_STORAGE_SECRET_KEY") {
		t.Fatal("config.Load did not compose every env-only S3 field")
	}

	storage, err := NewS3Storage(
		cfg.Storage.Endpoint,
		cfg.Storage.Bucket,
		cfg.Storage.Region,
		cfg.Storage.AccessKey,
		cfg.Storage.SecretKey,
	)
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}
	ctx := context.Background()
	if err := storage.CheckReady(ctx); err != nil {
		t.Fatalf("CheckReady: %v", err)
	}

	knownKey := "contract/known.txt"
	knownPayload := "known-size payload"
	knownReader := io.LimitReader(strings.NewReader(knownPayload), int64(len(knownPayload)))
	if err := storage.Put(ctx, knownKey, knownReader, int64(len(knownPayload)), "text/plain"); err != nil {
		t.Fatalf("Put known-size object: %v", err)
	}
	assertS3Object(t, storage, knownKey, knownPayload, "text/plain")

	knownMultipartKey := "contract/known-multipart.bin"
	knownMultipartPayload := bytes.Repeat([]byte("known-non-seekable-"), s3MultipartPartSize/len("known-non-seekable-")+1024)
	knownMultipartReader := io.LimitReader(bytes.NewReader(knownMultipartPayload), int64(len(knownMultipartPayload)))
	if _, seekable := knownMultipartReader.(io.Seeker); seekable {
		t.Fatal("known-length multipart fixture unexpectedly implements io.Seeker")
	}
	if err := storage.Put(ctx, knownMultipartKey, knownMultipartReader, int64(len(knownMultipartPayload)), "application/x-depsilo-contract"); err != nil {
		t.Fatalf("Put known-size non-seekable multipart object: %v", err)
	}
	assertS3ObjectBytes(t, storage, knownMultipartKey, knownMultipartPayload, "application/x-depsilo-contract")

	truncatedKey := "contract/declared-size-truncated.bin"
	truncatedReader := io.LimitReader(bytes.NewReader(knownMultipartPayload), int64(len(knownMultipartPayload)-1))
	if err := storage.Put(ctx, truncatedKey, truncatedReader, int64(len(knownMultipartPayload)), "application/octet-stream"); err == nil {
		t.Fatal("Put accepted a stream shorter than its declared size")
	}
	assertS3ObjectAbsent(t, storage, truncatedKey)
	assertNoPendingS3MultipartUploads(t, storage, truncatedKey)

	overlongKey := "contract/declared-size-overlong.bin"
	overlongReader := io.MultiReader(bytes.NewReader(knownMultipartPayload), strings.NewReader("x"))
	if err := storage.Put(ctx, overlongKey, overlongReader, int64(len(knownMultipartPayload)), "application/octet-stream"); err == nil {
		t.Fatal("Put accepted a stream longer than its declared size")
	}
	assertS3ObjectAbsent(t, storage, overlongKey)
	assertNoPendingS3MultipartUploads(t, storage, overlongKey)

	unknownKey := "contract/unknown.bin"
	unknownPayload := bytes.Repeat([]byte("streamed-without-length-"), s3MultipartPartSize/len("streamed-without-length-")+1024)
	unknownReader := io.LimitReader(bytes.NewReader(unknownPayload), int64(len(unknownPayload)))
	if err := storage.Put(ctx, unknownKey, unknownReader, -1, "application/octet-stream"); err != nil {
		t.Fatalf("Put unknown-size stream: %v", err)
	}
	assertS3Object(t, storage, unknownKey, string(unknownPayload), "application/octet-stream")

	objects, err := storage.List(ctx, "contract")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	keys := make([]string, 0, len(objects))
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	sort.Strings(keys)
	if got, want := strings.Join(keys, ","), knownMultipartKey+","+knownKey+","+unknownKey; got != want {
		t.Fatalf("List keys = %q, want %q", got, want)
	}

	total, err := storage.TotalSize(ctx)
	if err != nil {
		t.Fatalf("TotalSize: %v", err)
	}
	if want := int64(len(knownPayload) + len(knownMultipartPayload) + len(unknownPayload)); total != want {
		t.Fatalf("TotalSize = %d, want %d", total, want)
	}

	failedKey := "contract/incomplete.bin"
	err = storage.Put(ctx, failedKey, &failAfterReader{payload: make([]byte, s3MultipartPartSize)}, -1, "application/octet-stream")
	if err == nil {
		t.Fatal("Put accepted a stream that failed before completion")
	}
	if exists, existsErr := storage.Exists(ctx, failedKey); existsErr != nil || exists {
		t.Fatalf("incomplete object Exists = %v, error = %v; want false, nil", exists, existsErr)
	}
	assertNoPendingS3MultipartUploads(t, storage, failedKey)

	completeFailureKey := "contract/complete-failure.bin"
	minioURL, err := url.Parse(cfg.Storage.Endpoint)
	if err != nil {
		t.Fatalf("parse MinIO endpoint: %v", err)
	}
	reverseProxy := httputil.NewSingleHostReverseProxy(minioURL)
	var rejectedCompleteRequests atomic.Int64
	var observedAbortRequests atomic.Int64
	completeFailureProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodDelete && request.URL.Query().Get("uploadId") != "" &&
			strings.HasSuffix(request.URL.Path, "/"+completeFailureKey) {
			observedAbortRequests.Add(1)
		}
		if request.Method == http.MethodPost && request.URL.Query().Get("uploadId") != "" &&
			strings.HasSuffix(request.URL.Path, "/"+completeFailureKey) {
			rejectedCompleteRequests.Add(1)
			_, _ = io.Copy(io.Discard, request.Body)
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `<Error><Code>InvalidRequest</Code><Message>injected complete failure</Message></Error>`)
			return
		}
		reverseProxy.ServeHTTP(w, request)
	}))
	t.Cleanup(completeFailureProxy.Close)
	completeFailureStorage, err := NewS3Storage(
		completeFailureProxy.URL,
		cfg.Storage.Bucket,
		cfg.Storage.Region,
		cfg.Storage.AccessKey,
		cfg.Storage.SecretKey,
	)
	if err != nil {
		t.Fatalf("NewS3Storage through complete-failure proxy: %v", err)
	}
	completeFailureReader := io.LimitReader(bytes.NewReader(knownMultipartPayload), int64(len(knownMultipartPayload)))
	if err := completeFailureStorage.Put(ctx, completeFailureKey, completeFailureReader, int64(len(knownMultipartPayload)), "application/octet-stream"); err == nil {
		t.Fatal("Put succeeded after CompleteMultipartUpload was rejected")
	}
	if rejectedCompleteRequests.Load() == 0 {
		t.Fatal("complete-failure proxy did not observe CompleteMultipartUpload")
	}
	if observedAbortRequests.Load() == 0 {
		t.Fatal("complete-failure proxy did not observe AbortMultipartUpload")
	}
	assertS3ObjectAbsent(t, completeFailureStorage, completeFailureKey)
	assertNoPendingS3MultipartUploads(t, completeFailureStorage, completeFailureKey)

	for _, key := range []string{knownKey, knownMultipartKey, unknownKey} {
		if err := storage.Delete(ctx, key); err != nil {
			t.Fatalf("Delete %q: %v", key, err)
		}
		if exists, err := storage.Exists(ctx, key); err != nil || exists {
			t.Fatalf("deleted object %q Exists = %v, error = %v; want false, nil", key, exists, err)
		}
	}
}

func assertS3Object(t *testing.T, storage *S3Storage, key, wantBody, wantContentType string) {
	t.Helper()
	assertS3ObjectBytes(t, storage, key, []byte(wantBody), wantContentType)
}

func assertS3ObjectBytes(t *testing.T, storage *S3Storage, key string, wantBody []byte, wantContentType string) {
	t.Helper()
	ctx := context.Background()
	exists, err := storage.Exists(ctx, key)
	if err != nil || !exists {
		t.Fatalf("Exists(%q) = %v, %v; want true, nil", key, exists, err)
	}
	body, size, err := storage.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	got, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	if !bytes.Equal(got, wantBody) || size != int64(len(wantBody)) {
		t.Fatalf("Get(%q) returned %d bytes, size %d; want %d bytes, size %d", key, len(got), size, len(wantBody), len(wantBody))
	}
	meta, err := storage.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat(%q): %v", key, err)
	}
	if meta.Key != key || meta.Size != int64(len(wantBody)) || meta.ContentType != wantContentType {
		t.Fatalf("Stat(%q) = %+v, want size %d and content type %q", key, meta, len(wantBody), wantContentType)
	}
}

func assertS3ObjectAbsent(t *testing.T, storage *S3Storage, key string) {
	t.Helper()
	exists, err := storage.Exists(context.Background(), key)
	if err != nil || exists {
		t.Fatalf("Exists(%q) = %v, %v; want false, nil", key, exists, err)
	}
}

func assertNoPendingS3MultipartUploads(t *testing.T, storage *S3Storage, key string) {
	t.Helper()
	paginator := s3.NewListMultipartUploadsPaginator(storage.client, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(storage.bucket),
		Prefix: aws.String(key),
	})
	var uploadIDs []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			t.Fatalf("ListMultipartUploads(%q): %v", key, err)
		}
		for _, upload := range page.Uploads {
			if aws.ToString(upload.Key) == key {
				uploadIDs = append(uploadIDs, aws.ToString(upload.UploadId))
			}
		}
	}
	if len(uploadIDs) != 0 {
		t.Fatalf("ListMultipartUploads(%q) returned pending upload IDs %q; want none", key, uploadIDs)
	}
}

type failAfterReader struct {
	payload []byte
	sent    bool
}

func (r *failAfterReader) Read(target []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(target, r.payload), nil
	}
	return 0, errors.New("injected stream failure")
}
