package requestid

import (
	"context"

	"github.com/google/uuid"
)

type contextKey struct{}

// New returns a server-owned opaque correlation ID. Request headers are never
// used as identity: callers may reuse IDs or include sensitive values there.
func New() string { return uuid.NewString() }

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
