package cache

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"depsilo/internal/traffic"
)

// attributedBody mirrors upstream.originBody: it counts the bytes actually read
// through it into the request accumulator it was created with.
type attributedBody struct {
	reader *strings.Reader
	attr   *traffic.Attribution
}

func (b *attributedBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if n > 0 {
		b.attr.AddOriginBytes(int64(n))
	}
	return n, err
}

func (b *attributedBody) Close() error { return nil }

// A client-caused cache fill runs on the manager lifecycle context, which
// drops request values. The origin accumulator must still reach the fetch so
// the access-log row can carry the real Depsilo→upstream bytes instead of
// reporting zero for every range.
func TestManagerCarriesTheCallersOriginAttributionIntoTheFetch(t *testing.T) {
	manager := NewManager(newMemStorage(), openStreamTestDB(t), NewEventBus(), time.Hour)
	attr := traffic.NewAttribution()
	ctx := traffic.WithAttribution(context.Background(), attr)
	const payload = "left-pad index"

	result, err := manager.Get(ctx, "npm/simple/left-pad", "npm", time.Hour,
		func(fetchCtx context.Context) (io.ReadCloser, string, int64, string, error) {
			fetched := traffic.AttributionFromContext(fetchCtx)
			if fetched != attr {
				return nil, "", 0, "", errors.New("fetch context lost the caller's origin accumulator")
			}
			fetched.AddOriginExchange()
			return &attributedBody{reader: strings.NewReader(payload), attr: fetched},
				"text/plain", int64(len(payload)), "npmmirror", nil
		})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := io.Copy(io.Discard, result.Reader); err != nil {
		t.Fatalf("drain cache reader: %v", err)
	}

	requests, bytes := attr.OriginTotals()
	if requests != 1 || bytes != int64(len(payload)) {
		t.Fatalf("origin totals = (%d exchanges, %d bytes), want (1, %d)",
			requests, bytes, len(payload))
	}
}
