package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

const opaqueURL = "***"

// safeURLOrigin keeps enough context to identify a failing host while
// dropping userinfo, paths, queries and fragments, all of which may carry
// repository credentials or signed download tokens.
func safeURLOrigin(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return opaqueURL
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
}

type redactedTransportError struct{ cause error }

func (e redactedTransportError) Error() string {
	switch {
	case errors.Is(e.cause, context.Canceled):
		return "request canceled"
	case errors.Is(e.cause, context.DeadlineExceeded):
		return "request deadline exceeded"
	default:
		cause := e.cause
		if wrapped, ok := cause.(*url.Error); ok && wrapped.Err != nil {
			cause = wrapped.Err
		}
		// Transport error strings may repeat the full request or proxy URL.
		// Preserve the concrete failure class and errors.Is chain without
		// rendering that potentially credential-bearing text.
		return fmt.Sprintf("transport failure (%T)", cause)
	}
}

func (e redactedTransportError) Unwrap() error { return e.cause }
