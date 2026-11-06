//go:build s3integration

package distribution_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/distribution"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestDistributionS3StorageContract(t *testing.T) {
	for _, key := range []string{"ENDPOINT", "BUCKET", "REGION", "ACCESS_KEY", "SECRET_KEY"} {
		if os.Getenv("DEPSILO_STORAGE_"+key) == "" {
			t.Fatalf("S3 contract requires DEPSILO_STORAGE_%s", key)
		}
	}
	// The test runner owns and destroys the isolated RustFS instance. Dedicated
	// buckets avoid contaminating the ordinary cache contract running alongside it.
	base := fmt.Sprintf("depsilo-distribution-%d", time.Now().UnixNano())
	packageConfig := config.StorageConfig{
		Type: "s3", Endpoint: os.Getenv("DEPSILO_STORAGE_ENDPOINT"), Bucket: base + "-cache",
		Region: os.Getenv("DEPSILO_STORAGE_REGION"), AccessKey: os.Getenv("DEPSILO_STORAGE_ACCESS_KEY"), SecretKey: os.Getenv("DEPSILO_STORAGE_SECRET_KEY"),
	}
	cfg := config.DistributionConfig{Enabled: true, MaxSizeBytes: 4096, MaxObjectBytes: 1024, Storage: packageConfig}
	if _, err := distribution.NewStorage(cfg, packageConfig, config.StorageConfig{}); err == nil {
		t.Fatal("S3 construction accepted shared bucket")
	}
	cfg.Storage.Bucket = base + "-retained"
	packages, err := cache.NewS3Storage(packageConfig.Endpoint, packageConfig.Bucket, packageConfig.Region, packageConfig.AccessKey, packageConfig.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := distribution.NewStorage(cfg, packageConfig, config.StorageConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, item := range []struct {
		storage cache.Storage
		body    string
	}{{packages, "cache"}, {retained, "retained"}} {
		if err := item.storage.Put(ctx, "object", strings.NewReader(item.body), int64(len(item.body)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
	}
	if total, err := packages.TotalSize(ctx); err != nil || total != 5 {
		t.Fatalf("package cache accounting: total %d, error %v", total, err)
	}
	if total, err := retained.TotalSize(ctx); err != nil || total != 8 {
		t.Fatalf("retained accounting: total %d, error %v", total, err)
	}
	if err := packages.Delete(ctx, "object"); err != nil {
		t.Fatal(err)
	}
	reopened, err := distribution.NewStorage(cfg, packageConfig, config.StorageConfig{})
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := reopened.Get(ctx, "object")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "retained" {
		t.Fatalf("retained object after cache deletion/reopen: %q, %v", body, err)
	}
}

// The payloads here are integrity fixtures, not installable wheels. This
// contract crosses the catalog, validating reader, and real S3 multipart seam.
func TestDistributionS3StoreContract(t *testing.T) {
	for _, key := range []string{"ENDPOINT", "REGION", "ACCESS_KEY", "SECRET_KEY"} {
		if os.Getenv("DEPSILO_STORAGE_"+key) == "" {
			t.Fatalf("S3 contract requires DEPSILO_STORAGE_%s", key)
		}
	}
	cfg := config.DistributionConfig{Enabled: true, MaxSizeBytes: 64 << 20, MaxObjectBytes: 32 << 20, Storage: config.StorageConfig{
		Type: "s3", Endpoint: os.Getenv("DEPSILO_STORAGE_ENDPOINT"), Bucket: fmt.Sprintf("depsilo-store-%d", time.Now().UnixNano()),
		Region: os.Getenv("DEPSILO_STORAGE_REGION"), AccessKey: os.Getenv("DEPSILO_STORAGE_ACCESS_KEY"), SecretKey: os.Getenv("DEPSILO_STORAGE_SECRET_KEY"),
	}}
	retained, err := distribution.NewStorage(cfg, config.StorageConfig{}, config.StorageConfig{})
	if err != nil {
		t.Fatal(err)
	}
	client := s3.New(s3.Options{
		Region: cfg.Storage.Region, BaseEndpoint: aws.String(cfg.Storage.Endpoint), UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.Storage.AccessKey, cfg.Storage.SecretKey, ""),
	})
	f := &catalogFixture{dbPath: filepath.Join(t.TempDir(), "catalog.db"), limits: distribution.Limits{MaxObjectBytes: cfg.MaxObjectBytes, MaxStorageBytes: cfg.MaxSizeBytes}}
	f.openDatabase(t)
	f.store = f.newStore(t, retained)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stableBytes := []byte("published content must survive failed multipart uploads")
	stable := contentObject(stableBytes, "stable")
	old, err := f.store.Stage(ctx, manifestBytes(t, "python", "v1", stable), "s3-contract")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := f.store.Put(ctx, old.ID, stable.SHA256, io.LimitReader(bytes.NewReader(stableBytes), int64(len(stableBytes)))); err != nil || result.Reused {
		t.Fatalf("initial stream upload: %+v, %v", result, err)
	}
	if _, err := f.store.Activate(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	// More than the existing S3 transport's 8 MiB part size: every failing
	// stream below observes an actual in-progress upload before failing.
	payload := bytes.Repeat([]byte("m"), (8<<20)+257)
	large := contentObject(payload, "large")
	candidate, err := f.store.Stage(ctx, manifestBytes(t, "python", "v2", stable, large), "s3-contract")
	if err != nil {
		t.Fatal(err)
	}
	sourceError := errors.New("producer failed after the final byte")
	for _, name := range []string{"wrong-hash", "short", "long", "producer-error", "unexpected-eof", "canceled"} {
		t.Run(name, func(t *testing.T) {
			uploadCtx, cancelUpload := context.WithCancel(ctx)
			defer cancelUpload()
			stream := &observedMultipartStream{reader: bytes.NewReader(payload), observe: func() error {
				count := pendingDistributionMultipart(t, ctx, client, cfg.Storage.Bucket, storageKey(large.SHA256))
				if count != 1 {
					return fmt.Errorf("expected one multipart upload before producer failure, got %d", count)
				}
				return nil
			}}
			want := distribution.ErrIntegrity
			switch name {
			case "wrong-hash":
				wrong := append([]byte(nil), payload...)
				wrong[len(wrong)-1] = 'x'
				stream.reader = bytes.NewReader(wrong)
			case "short":
				stream.reader = bytes.NewReader(payload[:len(payload)-1])
			case "long":
				stream.reader = io.MultiReader(bytes.NewReader(payload), strings.NewReader("x"))
			case "producer-error":
				stream.endError, want = sourceError, sourceError
			case "unexpected-eof":
				stream.endError, want = io.ErrUnexpectedEOF, io.ErrUnexpectedEOF
			case "canceled":
				stream.cancel, want = cancelUpload, context.Canceled
			}
			if _, err := f.store.Put(uploadCtx, candidate.ID, large.SHA256, stream); !errors.Is(err, want) {
				t.Fatalf("rejected multipart upload error = %v, want %v", err, want)
			}
			if !stream.observed {
				t.Fatal("fixture failed before reaching the multipart boundary")
			}
			if pending := pendingDistributionMultipart(t, ctx, client, cfg.Storage.Bucket, storageKey(large.SHA256)); pending != 0 {
				t.Fatalf("failed multipart upload was not aborted: %d pending", pending)
			}
			if exists, err := retained.Exists(ctx, storageKey(large.SHA256)); err != nil || exists {
				t.Fatalf("failed upload published content: exists %v, error %v", exists, err)
			}
			if usage, err := f.store.Usage(ctx); err != nil || usage.Bytes != stable.Size || usage.Objects != 1 {
				t.Fatalf("failed upload changed accounted content: %+v, %v", usage, err)
			}
			if active, err := f.store.Active(ctx, "python"); err != nil || active.ID != old.ID {
				t.Fatalf("failed upload changed previously active content: %+v, %v", active, err)
			}
		})
	}
	if t.Failed() {
		return
	}
	for _, reused := range []bool{false, true} {
		result, err := f.store.Put(ctx, candidate.ID, large.SHA256, io.LimitReader(bytes.NewReader(payload), int64(len(payload))))
		if err != nil || result.Reused != reused {
			t.Fatalf("valid/repeated multipart upload: %+v, %v; want reused %v", result, err, reused)
		}
	}
	if report, err := f.store.Verify(ctx, candidate.ID); err != nil || report.ContentStatus != "content_verified" || report.VerifiedObjects != 2 || len(report.Issues) != 0 || report.Serving || report.InstallValidation != "not_run" {
		t.Fatalf("verify retained S3 content: %+v, %v", report, err)
	}
	if _, err := f.store.Activate(ctx, candidate.ID); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	f.openDatabase(t)
	reopened, err := distribution.NewStorage(cfg, config.StorageConfig{}, config.StorageConfig{})
	if err != nil {
		t.Fatal(err)
	}
	f.store = f.newStore(t, reopened)
	if active, err := f.store.Active(ctx, "python"); err != nil || active.ID != candidate.ID || active.Serving {
		t.Fatalf("reopened active content: %+v, %v", active, err)
	}
	if err := f.store.Delete(ctx, candidate.ID); !errors.Is(err, distribution.ErrActive) {
		t.Fatalf("active catalog deletion = %v, want ErrActive", err)
	}
	if err := f.store.Delete(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if removed, err := f.store.Prune(ctx); err != nil || removed != 0 {
		t.Fatalf("prune deleted content still referenced by active set: %d, %v", removed, err)
	}
	next, err := f.store.Stage(ctx, manifestBytes(t, "python", "v3", stable), "s3-contract")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Activate(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, candidate.ID); err != nil {
		t.Fatal(err)
	}
	if removed, err := f.store.Prune(ctx); err != nil || removed != large.Size {
		t.Fatalf("prune unreferenced S3 content: %d, %v; want %d", removed, err, large.Size)
	}
	if usage, err := f.store.Usage(ctx); err != nil || usage.Bytes != stable.Size || usage.Objects != 1 || usage.Sets != 1 {
		t.Fatalf("post-prune retained usage: %+v, %v", usage, err)
	}
	if active, err := f.store.Active(ctx, "python"); err != nil || active.ID != next.ID {
		t.Fatalf("prune damaged shared active content: %+v, %v", active, err)
	}
	if pending := pendingDistributionMultipart(t, ctx, client, cfg.Storage.Bucket, ""); pending != 0 {
		t.Fatalf("lifecycle left %d pending multipart uploads", pending)
	}
}

// It exposes only io.Reader, and injects failures after observing a real
// multipart upload. endError is emitted once, followed by genuine EOF.
type observedMultipartStream struct {
	reader   io.Reader
	read     int64
	observe  func() error
	observed bool
	endError error
	cancel   context.CancelFunc
}

func (r *observedMultipartStream) Read(p []byte) (int, error) {
	if r.read >= 8<<20 && !r.observed {
		if err := r.observe(); err != nil {
			return 0, err
		}
		r.observed = true
		if r.cancel != nil {
			r.cancel()
			return 0, context.Canceled
		}
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	if err == io.EOF && r.endError != nil {
		err, r.endError = r.endError, nil
	}
	return n, err
}

func pendingDistributionMultipart(t *testing.T, ctx context.Context, client *s3.Client, bucket, prefix string) int {
	t.Helper()
	paginator := s3.NewListMultipartUploadsPaginator(client, &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	count := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			t.Fatalf("inspect multipart uploads: %v", err)
		}
		count += len(page.Uploads)
	}
	return count
}
