package traffic

import (
	"context"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Transfer is an ephemeral cache-fill snapshot. No cache keys, target URLs,
// credentials or raw upstream errors cross this Admin boundary.
type Transfer struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Ecosystem       string     `json:"ecosystem"`
	Upstream        string     `json:"upstream"`
	Status          string     `json:"status"`
	DownloadedBytes int64      `json:"downloaded_bytes"`
	TotalBytes      int64      `json:"total_bytes"`
	ResumeAttempts  int        `json:"resume_attempts"`
	ResumeOffset    int64      `json:"resume_offset"`
	BytesPerSecond  float64    `json:"bytes_per_second"`
	StartedAt       time.Time  `json:"started_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

const TransferLimit = 64

type Transfers struct {
	mu    sync.Mutex
	next  uint64
	items map[string]*Transfer
}
type TransferHandle struct {
	registry *Transfers
	id       string
}
type transferNameKey struct{}
type transferHandleKey struct{}

func WithTransferName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, transferNameKey{}, name)
}
func TransferName(ctx context.Context) string {
	name, _ := ctx.Value(transferNameKey{}).(string)
	return name
}
func WithTransfer(ctx context.Context, h *TransferHandle) context.Context {
	return context.WithValue(ctx, transferHandleKey{}, h)
}
func TransferFromContext(ctx context.Context) *TransferHandle {
	h, _ := ctx.Value(transferHandleKey{}).(*TransferHandle)
	return h
}

func NewTransfers() *Transfers { return &Transfers{items: make(map[string]*Transfer)} }
func (r *Transfers) Start(ecosystem, name string) *TransferHandle {
	if r == nil || name == "" {
		return nil
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.SplitN(strings.SplitN(name, "?", 2)[0], "#", 2)[0]
	name = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return -1
		}
		return c
	}, name)
	runes := []rune(name)
	if len(runes) > 240 {
		name = string(runes[:240])
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) >= TransferLimit {
		var oldest *Transfer
		for _, item := range r.items {
			if item.FinishedAt != nil && (oldest == nil || item.UpdatedAt.Before(oldest.UpdatedAt)) {
				oldest = item
			}
		}
		if oldest == nil {
			return nil
		}
		delete(r.items, oldest.ID)
	}
	r.next++
	id := strconv.FormatUint(r.next, 10)
	now := time.Now().UTC()
	r.items[id] = &Transfer{ID: id, Name: name, Ecosystem: ecosystem, Status: "connecting", TotalBytes: -1, StartedAt: now, UpdatedAt: now}
	return &TransferHandle{registry: r, id: id}
}
func (h *TransferHandle) update(fn func(*Transfer)) {
	if h == nil {
		return
	}
	r := h.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if item := r.items[h.id]; item != nil && item.FinishedAt == nil {
		fn(item)
		item.UpdatedAt = time.Now().UTC()
	}
}
func (h *TransferHandle) Downloading(total int64, upstream string) {
	h.update(func(t *Transfer) { t.Status = "downloading"; t.TotalBytes = total; t.Upstream = upstream })
}
func (h *TransferHandle) Resume(attempt int, offset int64) {
	h.update(func(t *Transfer) { t.Status = "resuming"; t.ResumeAttempts = attempt; t.ResumeOffset = offset })
}
func (h *TransferHandle) Resumed() { h.update(func(t *Transfer) { t.Status = "downloading" }) }
func (h *TransferHandle) Finish(cached bool) {
	h.update(func(t *Transfer) {
		t.Status = "failed"
		if cached {
			t.Status = "cached"
		}
		now := time.Now().UTC()
		t.FinishedAt = &now
	})
}
func (r *Transfers) Snapshot() []Transfer {
	result := make([]Transfer, 0)
	if r == nil {
		return result
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		copy := *item
		end := time.Now()
		if copy.FinishedAt != nil {
			end = *copy.FinishedAt
			finished := *copy.FinishedAt
			copy.FinishedAt = &finished
		}
		if seconds := end.Sub(copy.StartedAt).Seconds(); seconds > 0 {
			copy.BytesPerSecond = float64(copy.DownloadedBytes) / seconds
		}
		result = append(result, copy)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if (a.FinishedAt == nil) != (b.FinishedAt == nil) {
			return a.FinishedAt == nil
		}
		return a.StartedAt.After(b.StartedAt)
	})
	return result
}

type transferBody struct {
	io.ReadCloser
	handle *TransferHandle
}

func (b *transferBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.handle.update(func(t *Transfer) {
		t.DownloadedBytes += int64(n)
		if err == io.EOF {
			t.Status = "saving"
		}
	})
	return n, err
}
func ObserveTransferBody(body io.ReadCloser, h *TransferHandle) io.ReadCloser {
	if h == nil {
		return body
	}
	return &transferBody{ReadCloser: body, handle: h}
}
