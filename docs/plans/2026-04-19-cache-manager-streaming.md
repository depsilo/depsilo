# Cache Manager Streaming Fix

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate full-body buffering in cache manager so large files (torch ~2GB) don't cause OOM.

**Architecture:** Replace `bytes.Buffer` intermediary with direct streaming from upstream body to `storage.Put`. Add a lightweight `countingReader` wrapper to track bytes written (needed when upstream doesn't report Content-Length). Two functions affected: `fetchAndStore` and `backgroundRefresh`, both in `internal/cache/manager.go`.

**Tech Stack:** Go standard library (`io`), no new dependencies.

---

## File Structure

| Action | File | Responsibility |
| ------ | ---- | -------------- |
| Modify | `internal/cache/manager.go` | Remove `bytes.Buffer`, add `countingReader`, stream body directly to `storage.Put` |
| Create | `tests/unit/counting_reader_test.go` | Unit test for `countingReader` |
| Create | `tests/unit/cache_streaming_test.go` | Integration test proving large bodies aren't buffered |

---

### Task 1: Add `countingReader` and test it

**Files:**
- Modify: `internal/cache/manager.go` (add `countingReader` struct after line 16)
- Create: `tests/unit/counting_reader_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/unit/counting_reader_test.go`:

```go
package unit

import (
	"bytes"
	"io"
	"testing"

	"depsilo/internal/cache"
)

func TestCountingReader_CountsBytes(t *testing.T) {
	data := []byte("hello world") // 11 bytes
	cr := cache.NewCountingReader(bytes.NewReader(data))

	buf, err := io.ReadAll(cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(buf) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(buf))
	}
	if cr.BytesRead() != 11 {
		t.Errorf("expected 11 bytes read, got %d", cr.BytesRead())
	}
}

func TestCountingReader_EmptyReader(t *testing.T) {
	cr := cache.NewCountingReader(bytes.NewReader(nil))

	buf, err := io.ReadAll(cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buf) != 0 {
		t.Errorf("expected empty, got %d bytes", len(buf))
	}
	if cr.BytesRead() != 0 {
		t.Errorf("expected 0 bytes read, got %d", cr.BytesRead())
	}
}

func TestCountingReader_LargeData(t *testing.T) {
	data := make([]byte, 10*1024*1024) // 10 MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	cr := cache.NewCountingReader(bytes.NewReader(data))

	written, err := io.Copy(io.Discard, cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != int64(len(data)) {
		t.Errorf("io.Copy reported %d, expected %d", written, len(data))
	}
	if cr.BytesRead() != int64(len(data)) {
		t.Errorf("expected %d bytes read, got %d", len(data), cr.BytesRead())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/unit/ -run TestCountingReader -v`

Expected: FAIL with compilation error — `cache.NewCountingReader` undefined.

- [ ] **Step 3: Implement `countingReader`**

Add to `internal/cache/manager.go`, after the import block (after line 16):

```go
// countingReader wraps an io.Reader and counts bytes read through it.
// Used to determine actual body size when upstream doesn't report Content-Length.
type countingReader struct {
	r    io.Reader
	n    int64
}

func NewCountingReader(r io.Reader) *countingReader {
	return &countingReader{r: r}
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}

func (cr *countingReader) BytesRead() int64 {
	return cr.n
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/unit/ -run TestCountingReader -v`

Expected: PASS (all 3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cache/manager.go tests/unit/counting_reader_test.go
git commit -m "feat(cache): add countingReader for streaming byte counting"
```

---

### Task 2: Convert `fetchAndStore` to streaming

**Files:**
- Modify: `internal/cache/manager.go:487-550` (the `fetchAndStore` singleflight callback)

- [ ] **Step 1: Replace buffer with streaming in `fetchAndStore`**

In `internal/cache/manager.go`, replace lines 497-504:

```go
		var buf bytes.Buffer
		written, err := io.Copy(&buf, body)
		if err != nil {
			return nil, fmt.Errorf("read upstream body: %w", err)
		}
		if size <= 0 {
			size = written
		}
```

With:

```go
		cr := NewCountingReader(body)
```

Then replace line 507:

```go
		if putErr := m.storage.Put(fetchCtx, key, bytes.NewReader(buf.Bytes()), size, contentType); putErr != nil {
```

With:

```go
		if putErr := m.storage.Put(fetchCtx, key, cr, size, contentType); putErr != nil {
```

And add size resolution right after the `Put` block (after the closing `}` of the `} else {` block that does the DB upsert, before `m.publishEvent`):

```go
		if size <= 0 {
			size = cr.BytesRead()
		}
```

The full singleflight callback should now look like:

```go
	val, err, _ := m.group.Do(key, func() (interface{}, error) {
		fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer fetchCancel()

		body, contentType, size, err := fetchFn(fetchCtx)
		if err != nil {
			return nil, err
		}
		defer body.Close()

		cr := NewCountingReader(body)

		// Write to storage (streams directly from upstream)
		if putErr := m.storage.Put(fetchCtx, key, cr, size, contentType); putErr != nil {
			zap.L().Warn("cache put failed", zap.String("key", key), zap.Error(putErr))
		} else {
			if size <= 0 {
				size = cr.BytesRead()
			}
			// Upsert DB entry (never delete, just update expiry)
			now := time.Now()
			entry := db.CacheEntry{
				Key:          key,
				AdapterType:  adapterType,
				StoragePath:  key,
				Size:         size,
				HitCount:     0,
				ContentType:  contentType,
				PackageName:  ExtractPackageName(adapterType, key),
				ExpiresAt:    now.Add(ttl),
				LastAccessed: now,
			}
			if createErr := m.db.Create(&entry).Error; createErr != nil {
				// Already exists — update instead
				m.db.Where("key = ?", key).Updates(map[string]interface{}{
					"size":          size,
					"content_type":  contentType,
					"package_name":  ExtractPackageName(adapterType, key),
					"expires_at":    now.Add(ttl),
					"last_accessed": now,
				})
			}
		}

		m.publishEvent(key, adapterType, false, size)

		// Trigger async security scan for new packages
		if securityScanner != nil {
			pkgName := ExtractPackageName(adapterType, key)
			if pkgName != "" {
				go func() {
					if err := securityScanner.ScanPackage(context.Background(), adapterType, pkgName); err != nil {
						zap.L().Debug("security scan for new package failed", zap.Error(err))
					}
				}()
			}
		}

		return &sfResult{contentType: contentType, size: size}, nil
	})
```

- [ ] **Step 2: Run existing tests**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./... && go test ./tests/... -v`

Expected: All tests pass, build succeeds.

- [ ] **Step 3: Commit**

```bash
git add internal/cache/manager.go
git commit -m "fix(cache): stream upstream body directly to storage in fetchAndStore

Eliminates bytes.Buffer that could OOM on large files (torch ~2GB).
Body now flows directly from upstream HTTP response to storage.Put via
countingReader, which tracks written bytes for size resolution."
```

---

### Task 3: Convert `backgroundRefresh` to streaming

**Files:**
- Modify: `internal/cache/manager.go:574-616` (the `backgroundRefresh` function)

- [ ] **Step 1: Replace buffer with streaming in `backgroundRefresh`**

In `internal/cache/manager.go`, the `backgroundRefresh` singleflight callback currently has:

```go
		var buf bytes.Buffer
		written, err := io.Copy(&buf, body)
		if err != nil {
			return nil, fmt.Errorf("bg refresh read body: %w", err)
		}
		if size <= 0 {
			size = written
		}

		if putErr := m.storage.Put(ctx, key, bytes.NewReader(buf.Bytes()), size, contentType); putErr != nil {
			return nil, fmt.Errorf("bg refresh put: %w", putErr)
		}
```

Replace with:

```go
		cr := NewCountingReader(body)

		if putErr := m.storage.Put(ctx, key, cr, size, contentType); putErr != nil {
			return nil, fmt.Errorf("bg refresh put: %w", putErr)
		}
		if size <= 0 {
			size = cr.BytesRead()
		}
```

- [ ] **Step 2: Remove `"bytes"` from imports**

In `internal/cache/manager.go`, remove `"bytes"` from the import block (line 4). The import block should become:

```go
import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"depsilo/internal/db"
)
```

- [ ] **Step 3: Run all tests and build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./... && go test ./tests/... -v`

Expected: All tests pass, build succeeds with no `bytes` import error.

- [ ] **Step 4: Commit**

```bash
git add internal/cache/manager.go
git commit -m "fix(cache): stream backgroundRefresh and remove bytes import

Completes the streaming migration. Both fetchAndStore and backgroundRefresh
now stream upstream bodies directly to storage without memory buffering."
```

---

### Task 4: Add streaming integration test

**Files:**
- Create: `tests/unit/cache_streaming_test.go`

- [ ] **Step 1: Write the integration test**

Create `tests/unit/cache_streaming_test.go`:

```go
package unit

import (
	"bytes"
	"context"
	"io"
	"testing"

	"depsilo/internal/cache"
)

// fakeStorage is a minimal Storage implementation for testing.
type fakeStorage struct {
	data map[string][]byte
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{data: make(map[string][]byte)}
}

func (s *fakeStorage) Exists(_ context.Context, key string) (bool, error) {
	_, ok := s.data[key]
	return ok, nil
}

func (s *fakeStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	d, ok := s.data[key]
	if !ok {
		return nil, 0, io.EOF
	}
	return io.NopCloser(bytes.NewReader(d)), int64(len(d)), nil
}

func (s *fakeStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	d, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.data[key] = d
	return nil
}

func (s *fakeStorage) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func (s *fakeStorage) Stat(_ context.Context, key string) (*cache.ObjectMeta, error) {
	return nil, nil
}

func (s *fakeStorage) List(_ context.Context, _ string) ([]cache.ObjectMeta, error) {
	return nil, nil
}

func (s *fakeStorage) TotalSize(_ context.Context) (int64, error) {
	return 0, nil
}

// TestStreamingPut verifies that storage.Put receives data directly from
// the reader without full buffering. We check this by confirming the stored
// data matches the source and that countingReader accurately tracks size.
func TestStreamingPut(t *testing.T) {
	store := newFakeStorage()

	// Simulate a 5 MB upstream body
	size := 5 * 1024 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251) // prime modulus for variety
	}

	cr := cache.NewCountingReader(bytes.NewReader(data))

	err := store.Put(context.Background(), "test/big-file.whl", cr, int64(size), "application/octet-stream")
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify countingReader tracked all bytes
	if cr.BytesRead() != int64(size) {
		t.Errorf("countingReader reported %d bytes, expected %d", cr.BytesRead(), size)
	}

	// Verify stored data matches
	stored := store.data["test/big-file.whl"]
	if len(stored) != size {
		t.Errorf("stored %d bytes, expected %d", len(stored), size)
	}
	if !bytes.Equal(stored, data) {
		t.Error("stored data does not match source data")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/unit/ -run TestStreamingPut -v`

Expected: PASS.

- [ ] **Step 3: Run full test suite**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/... -v`

Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add tests/unit/cache_streaming_test.go
git commit -m "test(cache): add streaming integration test for storage.Put"
```

---

### Task 5: Final verification

- [ ] **Step 1: Full build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build -o /dev/null ./cmd/server/`

Expected: Build succeeds.

- [ ] **Step 2: Run all tests**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./... 2>&1 | tail -20`

Expected: All packages pass.

- [ ] **Step 3: Verify no bytes.Buffer usage remains in manager.go**

Run: `grep -n 'bytes\.Buffer\|bytes\.NewReader' internal/cache/manager.go`

Expected: No output (no matches).
