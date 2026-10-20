package traffic

import (
	"io"
	"strings"
	"testing"
)

func TestTransferProgressSeparatesDownloadedFromCached(t *testing.T) {
	registry := NewTransfers()
	handle := registry.Start("pypi", "/packages/demo.whl?credential=hidden#fragment")
	handle.Downloading(3, "official")
	body := ObserveTransferBody(io.NopCloser(strings.NewReader("abc")), handle)
	first := make([]byte, 1)
	if _, err := body.Read(first); err != nil {
		t.Fatal(err)
	}
	handle.Resume(1, 1)
	item := registry.Snapshot()[0]
	if item.Name != "demo.whl" || item.Status != "resuming" || item.DownloadedBytes != 1 || item.ResumeOffset != 1 {
		t.Fatalf("unexpected progress: %+v", item)
	}
	handle.Resumed()
	if _, err := io.Copy(io.Discard, body); err != nil {
		t.Fatal(err)
	}
	item = registry.Snapshot()[0]
	if item.Status != "saving" || item.FinishedAt != nil || item.DownloadedBytes != 3 {
		t.Fatalf("download completion mistaken for cache commit: %+v", item)
	}
	handle.Finish(true)
	handle.Resume(2, 3)
	item = registry.Snapshot()[0]
	if item.Status != "cached" || item.ResumeAttempts != 1 || item.FinishedAt == nil {
		t.Fatalf("terminal state changed: %+v", item)
	}
}

func TestTransferRegistryBoundDoesNotEvictActiveDownloads(t *testing.T) {
	registry := NewTransfers()
	first := registry.Start("pypi", "first.whl")
	for i := 1; i < TransferLimit; i++ {
		if registry.Start("pypi", "other.whl") == nil {
			t.Fatal("registry filled too early")
		}
	}
	if registry.Start("pypi", "overflow.whl") != nil {
		t.Fatal("active registry exceeded bound")
	}
	first.Finish(false)
	if registry.Start("pypi", "new.whl") == nil {
		t.Fatal("completed record was not reclaimed")
	}
	records := registry.Snapshot()
	if len(records) != TransferLimit {
		t.Fatal("incorrect registry bound")
	}
	for _, record := range records {
		if record.Name == "first.whl" {
			t.Fatal("retained terminal entry instead of active entry")
		}
	}
}
