package middleware

import (
	"github.com/gin-gonic/gin"

	"depsilo/internal/requestid"
)

const requestIDHeader = "X-Request-ID"

// RequestID establishes one opaque correlation ID for each HTTP request.
// Existing valid IDs are preserved so callers can join their own traces;
// malformed or oversized values are replaced before they reach logs.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := requestid.MiddlewareID(c.GetHeader(requestIDHeader))
		c.Request = c.Request.WithContext(requestid.With(c.Request.Context(), id))
		c.Header(requestIDHeader, id)
		c.Next()
	}
}
