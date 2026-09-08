package middleware

import (
	"net/http/httptest"
	"testing"

	"depsilo/internal/requestid"
	"github.com/gin-gonic/gin"
)

func TestRequestIDAlwaysUsesServerOwnedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	r.GET("/", func(c *gin.Context) {
		if got := requestid.FromContext(c.Request.Context()); got == "" {
			t.Fatal("request context has no request id")
		}
		c.Status(204)
	})

	for _, input := range []string{"safe-client-id", "bad\nvalue"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set(requestIDHeader, input)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		got := rec.Header().Get(requestIDHeader)
		if got == "" || got == input {
			t.Fatalf("input %q produced request id %q", input, got)
		}
	}
}
