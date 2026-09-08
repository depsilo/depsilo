package middleware

import (
	"github.com/gin-gonic/gin"

	"depsilo/internal/requestid"
)

const requestIDHeader = "X-Request-ID"

// RequestID establishes one server-owned opaque correlation ID for each HTTP
// request. Caller supplied IDs cannot join unrelated historical records.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := requestid.New()
		c.Request = c.Request.WithContext(requestid.With(c.Request.Context(), id))
		c.Header(requestIDHeader, id)
		c.Next()
	}
}
