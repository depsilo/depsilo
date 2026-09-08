package requestid

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

type contextKey struct{}

// MiddlewareID returns a bounded, opaque request identifier suitable for
// correlating diagnostics. Caller supplied IDs are accepted only when they
// contain printable non-whitespace bytes; otherwise a fresh UUID is used.
func MiddlewareID(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate != "" && len(candidate) <= 128 {
		for _, r := range candidate {
			if r < 0x21 || r > 0x7e {
				candidate = ""
				break
			}
		}
	}
	if candidate == "" {
		return uuid.NewString()
	}
	return candidate
}

func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
