package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/compilecache"
)

// CCacheHandler implements the HTTP remote-storage contract used by stock
// ccache. It is a data-plane endpoint, not a JSON API.
type CCacheHandler struct {
	enabled    bool
	service    *compilecache.Service
	authorizer *compilecache.Authorizer
}

// NewCCacheHandler constructs the stock ccache HTTP remote-storage Adapter.
func NewCCacheHandler(enabled bool, service *compilecache.Service, authorizer *compilecache.Authorizer) *CCacheHandler {
	return &CCacheHandler{enabled: enabled, service: service, authorizer: authorizer}
}

// Handle serves one ccache data-plane request.
func (h *CCacheHandler) Handle(c *gin.Context) {
	// Disabled, unauthorized and cold-miss responses must not be retained by a
	// reverse proxy; otherwise enabling the service or populating a key can
	// remain hidden behind a cached 404/401.
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	operation := "other"
	switch c.Request.Method {
	case http.MethodHead, http.MethodGet, http.MethodPut, http.MethodDelete:
		operation = strings.ToLower(c.Request.Method)
	}
	started := time.Now()
	defer func() {
		M.CompileCacheDuration.WithLabelValues(string(compilecache.ProtocolCCache), operation).Observe(time.Since(started).Seconds())
	}()
	if c.Request.Method == http.MethodPut && h.enabled && h.service != nil {
		finishDeadline := h.installUploadDeadline(c)
		defer finishDeadline()
	}
	if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) && h.enabled && h.service != nil {
		finishDeadline := h.installDownloadDeadline(c)
		defer finishDeadline()
	}

	// Keep this route registered while disabled so the SPA fallback can never
	// return index.html with a misleading HTTP 200 to ccache.
	if !h.enabled || h.service == nil || h.authorizer == nil {
		h.record(operation, "disabled")
		preventRequestBodyDrain(c)
		c.Status(http.StatusNotFound)
		return
	}
	namespace, err := compilecache.NormalizeNamespace(c.Param("namespace"))
	if err != nil {
		h.protocolError(c, operation, http.StatusBadRequest, "invalid namespace")
		return
	}
	write := c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete
	if _, err := h.authorizer.Authenticate(c.Request.Context(), c.GetHeader("Authorization"), namespace, write); err != nil {
		switch {
		case errors.Is(c.Request.Context().Err(), context.DeadlineExceeded):
			h.protocolError(c, operation, http.StatusRequestTimeout, "compiler-cache request timed out")
		case errors.Is(err, compilecache.ErrAuthUnavailable):
			h.protocolError(c, operation, http.StatusServiceUnavailable, "credential service is unavailable")
		case errors.Is(err, compilecache.ErrForbidden):
			h.protocolError(c, operation, http.StatusForbidden, "credential does not grant this access")
		default:
			c.Header("WWW-Authenticate", `Bearer realm="depsilo-compile-cache"`)
			h.protocolError(c, operation, http.StatusUnauthorized, "invalid or expired credential")
		}
		return
	}
	id, err := compilecache.ParseCCacheArtifact(namespace, c.Param("key"))
	if err != nil {
		h.protocolError(c, operation, http.StatusBadRequest, "invalid ccache key")
		return
	}
	// GET, HEAD and DELETE have no request-body semantics in ccache's HTTP
	// backend. Reject a declared or chunked body before net/http attempts its
	// automatic post-handler drain; a slow client could otherwise pin a
	// connection after an otherwise successful response.
	if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodDelete) &&
		c.Request.ContentLength != 0 {
		h.protocolError(c, operation, http.StatusBadRequest, "request body is not allowed")
		return
	}

	switch c.Request.Method {
	case http.MethodHead:
		h.head(c, id)
	case http.MethodGet:
		h.get(c, id)
	case http.MethodPut:
		h.put(c, id)
	case http.MethodDelete:
		h.delete(c, id)
	default:
		c.Header("Allow", "HEAD, GET, PUT, DELETE")
		h.protocolError(c, operation, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *CCacheHandler) head(c *gin.Context, id compilecache.ArtifactID) {
	size, err := h.service.Stat(c.Request.Context(), id)
	if err != nil {
		h.serviceError(c, "head", err)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	h.record("head", "hit")
	c.Status(http.StatusOK)
}

func (h *CCacheHandler) get(c *gin.Context, id compilecache.ArtifactID) {
	entry, err := h.service.Open(c.Request.Context(), id)
	if err != nil {
		h.serviceError(c, "get", err)
		return
	}
	closed := make(chan struct{})
	stopClose := context.AfterFunc(c.Request.Context(), func() {
		_ = entry.Body.Close()
		close(closed)
	})
	defer func() {
		if !stopClose() {
			<-closed
		}
		if err := entry.Body.Close(); err != nil {
			zap.L().Debug("close compiler-cache response", zap.Error(err))
		}
	}()
	h.record("get", "hit")
	M.CompileCacheBytes.WithLabelValues(string(compilecache.ProtocolCCache), "served").Add(float64(entry.Size))
	c.DataFromReader(http.StatusOK, entry.Size, "application/octet-stream", entry.Body, nil)
}

func (h *CCacheHandler) put(c *gin.Context, id compilecache.ArtifactID) {
	size := c.Request.ContentLength
	if size < 0 {
		h.protocolError(c, "put", http.StatusLengthRequired, "Content-Length is required")
		return
	}
	if size > h.service.MaxEntryBytes() {
		h.protocolError(c, "put", http.StatusRequestEntityTooLarge, "entry exceeds max_entry_size_mb")
		return
	}
	// Service reads at most declared-size+1 so it can distinguish an overlong
	// body from a valid entry. Permit that single sentinel byte here as well.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.service.MaxEntryBytes()+1)
	result, err := h.service.Put(c.Request.Context(), id, c.Request.Body, size)
	if err != nil {
		h.serviceError(c, "put", err)
		return
	}
	M.CompileCacheBytes.WithLabelValues(string(compilecache.ProtocolCCache), "stored").Add(float64(result.Size))
	if result.Created {
		h.record("put", "created")
		c.Status(http.StatusCreated)
		return
	}
	h.record("put", "overwritten")
	c.Status(http.StatusNoContent)
}

// installUploadDeadline covers authentication, queueing and body transfer.
// On HTTP/1, Request.Body.Close cannot reliably interrupt a blocked Read, so
// the response controller moves the underlying connection's read deadline to
// now when the context expires. Body.Close remains a fallback for test and
// custom writers that do not expose connection deadlines.
func (h *CCacheHandler) installUploadDeadline(c *gin.Context) func() {
	uploadCtx, cancel := context.WithTimeout(c.Request.Context(), h.service.UploadTimeout())
	c.Request = c.Request.WithContext(uploadCtx)
	controller := http.NewResponseController(c.Writer)
	body := c.Request.Body
	finished := make(chan struct{})
	stop := context.AfterFunc(uploadCtx, func() {
		_ = controller.SetReadDeadline(time.Now())
		_ = body.Close()
		close(finished)
	})
	return func() {
		if !stop() {
			<-finished
		}
		cancel()
	}
}

func (h *CCacheHandler) installDownloadDeadline(c *gin.Context) func() {
	downloadCtx, cancel := context.WithTimeout(c.Request.Context(), h.service.DownloadTimeout())
	c.Request = c.Request.WithContext(downloadCtx)
	controller := http.NewResponseController(c.Writer)
	finished := make(chan struct{})
	stop := context.AfterFunc(downloadCtx, func() {
		_ = controller.SetWriteDeadline(time.Now())
		close(finished)
	})
	return func() {
		if !stop() {
			<-finished
		}
		cancel()
	}
}

func (h *CCacheHandler) delete(c *gin.Context, id compilecache.ArtifactID) {
	deleted, err := h.service.Delete(c.Request.Context(), id)
	if err != nil {
		h.serviceError(c, "delete", err)
		return
	}
	result := "missing"
	if deleted {
		result = "deleted"
	}
	h.record("delete", result)
	c.Status(http.StatusNoContent)
}

func (h *CCacheHandler) serviceError(c *gin.Context, operation string, err error) {
	switch {
	case errors.Is(err, compilecache.ErrNotFound):
		h.protocolError(c, operation, http.StatusNotFound, "cache miss")
	case errors.Is(err, compilecache.ErrInvalidKey), errors.Is(err, compilecache.ErrInvalidNamespace),
		errors.Is(err, compilecache.ErrInvalidSize), errors.Is(err, compilecache.ErrSizeMismatch):
		h.protocolError(c, operation, http.StatusBadRequest, "invalid cache entry")
	case errors.Is(err, compilecache.ErrTooLarge):
		h.protocolError(c, operation, http.StatusRequestEntityTooLarge, "entry is too large")
	case errors.Is(err, compilecache.ErrInsufficientStorage):
		h.protocolError(c, operation, http.StatusInsufficientStorage, "compiler cache is full")
	case errors.Is(err, compilecache.ErrUploadBusy):
		c.Header("Retry-After", "1")
		h.protocolError(c, operation, http.StatusServiceUnavailable, "compiler-cache upload queue is full")
	case errors.Is(err, compilecache.ErrDownloadBusy):
		c.Header("Retry-After", "1")
		h.protocolError(c, operation, http.StatusServiceUnavailable, "compiler-cache download concurrency is full")
	case errors.Is(err, context.DeadlineExceeded):
		message := "compiler-cache request timed out"
		if operation == "put" {
			message = "compiler-cache upload timed out"
		}
		h.protocolError(c, operation, http.StatusRequestTimeout, message)
	case errors.Is(err, context.Canceled):
		h.protocolError(c, operation, http.StatusServiceUnavailable, "compiler-cache request was canceled")
	default:
		zap.L().Warn("compiler-cache operation failed", zap.String("operation", operation), zap.Error(err))
		h.protocolError(c, operation, http.StatusServiceUnavailable, "compiler-cache storage is unavailable")
	}
}

func (h *CCacheHandler) protocolError(c *gin.Context, operation string, status int, message string) {
	preventRequestBodyDrain(c)
	h.record(operation, resultForStatus(status))
	c.Data(status, "text/plain; charset=utf-8", []byte(message+"\n"))
}

func preventRequestBodyDrain(c *gin.Context) {
	if c.Request == nil || c.Request.Body == nil || c.Request.ContentLength == 0 {
		return
	}
	c.Request.Close = true
	if c.Request.ProtoMajor == 1 {
		c.Header("Connection", "close")
	}
}

func (h *CCacheHandler) record(operation, result string) {
	M.CompileCacheRequests.WithLabelValues(string(compilecache.ProtocolCCache), operation, result).Inc()
}

func resultForStatus(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusLengthRequired, http.StatusRequestEntityTooLarge:
		return "rejected"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "miss"
	case http.StatusInsufficientStorage:
		return "full"
	case http.StatusRequestTimeout:
		return "timeout"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	default:
		if status >= 500 {
			return "error"
		}
		return fmt.Sprintf("status_%d", status)
	}
}
