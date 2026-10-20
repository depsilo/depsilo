package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"depsilo/internal/traffic"
	"go.uber.org/zap"
)

const artifactResumeAttempts = 2

// FetchArtifact streams a relative artifact and may recover interrupted reads
// from this exact source. Metadata fetches retain their existing behavior.
func (u *Upstream) FetchArtifact(ctx context.Context, path string) (*FetchResult, error) {
	target, err := originRequestURL(u.URL, path)
	if err != nil {
		return nil, err
	}
	return u.fetchArtifact(ctx, target, true)
}

// FetchArtifactURL keeps the external-target and redirect guards of FetchURL.
// Recovery never reselects a source or restores credentials to another origin.
func (u *Upstream) FetchArtifactURL(ctx context.Context, target string) (*FetchResult, error) {
	if !validHTTPURL(target) {
		return nil, errors.New("invalid artifact URL")
	}
	parsed, _ := url.Parse(target)
	origin, _ := url.Parse(u.URL)
	if err := validateExternalTarget(origin, parsed, u.directNetworkGuard); err != nil {
		return nil, errors.New("artifact target rejected")
	}
	return u.fetchArtifact(ctx, target, false)
}

func (u *Upstream) fetchArtifact(ctx context.Context, target string, report bool) (*FetchResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	open := func(headers map[string]string) (*FetchResult, error) {
		return u.doWithHeaders(ctx, target, report, headers)
	}
	result, err := open(map[string]string{"Accept-Encoding": "identity"})
	if err != nil {
		cancel()
		return nil, err
	}
	if result.StatusCode != http.StatusOK || !identityEncoding(result.ContentEncoding) {
		_ = result.Body.Close()
		cancel()
		return nil, errors.New("artifact response is not a complete identity representation")
	}
	body := &artifactBody{ctx: ctx, cancel: cancel, open: open, body: result.Body, size: result.Size, etag: result.ETag, url: result.URL, source: u.Name}
	body.resumable = result.StatusCode == http.StatusOK && result.Size > 0 && strongArtifactETag(result.ETag) && identityEncoding(result.ContentEncoding)
	result.Body = body
	return result, nil
}

func strongArtifactETag(tag string) bool {
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return false
	}
	for i := 1; i < len(tag)-1; i++ {
		if tag[i] < 0x21 || tag[i] == '"' || tag[i] == 0x7f {
			return false
		}
	}
	return true
}
func identityEncoding(encoding string) bool {
	return encoding == "" || strings.EqualFold(encoding, "identity")
}

// Read is owned by one streaming pump. Close may race with Read (including
// a blocked reopen); cancellation and the body swap share this lock.
type artifactBody struct {
	ctx          context.Context
	cancel       context.CancelFunc
	open         func(map[string]string) (*FetchResult, error)
	mu           sync.Mutex
	body         io.ReadCloser
	closed       bool
	size, offset int64
	etag, url    string
	source       string
	resumable    bool
	attempts     int
	pending      error
}

func (b *artifactBody) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	body := b.body
	b.mu.Unlock()
	b.cancel()
	return body.Close()
}

func (b *artifactBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		b.mu.Lock()
		closed := b.closed
		body := b.body
		b.mu.Unlock()
		if closed {
			return 0, io.ErrClosedPipe
		}
		if b.pending != nil {
			if err := b.resume(); err != nil {
				return 0, err
			}
			continue
		}
		n, err := body.Read(p)
		b.offset += int64(n)
		if err != nil && b.offset < b.size {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			b.pending = err
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (b *artifactBody) resume() error {
	failure := b.pending
	if !b.resumable || b.attempts >= artifactResumeAttempts || b.ctx.Err() != nil {
		return failure
	}
	b.mu.Lock()
	old := b.body
	b.mu.Unlock()
	_ = old.Close()
	for b.attempts < artifactResumeAttempts {
		b.attempts++
		timer := time.NewTimer(time.Duration(b.attempts) * 100 * time.Millisecond)
		select {
		case <-b.ctx.Done():
			timer.Stop()
			return b.ctx.Err()
		case <-timer.C:
		}
		traffic.TransferFromContext(b.ctx).Resume(b.attempts, b.offset)
		zap.L().Info("attempting artifact resume", zap.String("upstream", b.source), zap.Int("attempt", b.attempts), zap.Int64("offset", b.offset), zap.Int64("size", b.size))
		next, err := b.open(map[string]string{"Accept-Encoding": "identity", "Range": fmt.Sprintf("bytes=%d-", b.offset), "If-Range": b.etag})
		if err != nil {
			failure = err
			continue
		}
		expected := fmt.Sprintf("bytes %d-%d/%d", b.offset, b.size-1, b.size)
		if next.StatusCode != http.StatusPartialContent || next.ContentRange != expected || next.Size != b.size-b.offset || next.ETag != b.etag || next.URL != b.url || !identityEncoding(next.ContentEncoding) {
			_ = next.Body.Close()
			return errors.New("artifact resume rejected: object identity or byte range changed")
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			_ = next.Body.Close()
			return io.ErrClosedPipe
		}
		b.body = next.Body
		b.mu.Unlock()
		b.pending = nil
		traffic.TransferFromContext(b.ctx).Resumed()
		zap.L().Info("artifact transfer resumed", zap.String("upstream", b.source), zap.Int("attempt", b.attempts), zap.Int64("offset", b.offset), zap.Int64("size", b.size))
		return nil
	}
	return failure
}
