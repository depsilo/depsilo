package public

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
)

// eventStreamHeartbeat is how often an open stream writes a comment line. A
// reverse proxy closes a response that stays silent for its read timeout
// (60 seconds in nginx by default), and a quiet cache produces no event for
// much longer than that.
const eventStreamHeartbeat = 25 * time.Second

type EventsHandler struct {
	eventBus  *cache.EventBus
	heartbeat time.Duration
}

func NewEventsHandler(eventBus *cache.EventBus) *EventsHandler {
	return &EventsHandler{eventBus: eventBus, heartbeat: eventStreamHeartbeat}
}

func (h *EventsHandler) Stream(c *gin.Context) {
	ch, ok := h.eventBus.Subscribe()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    "TOO_MANY_SUBSCRIBERS",
			"message": "Too many active event streams",
		})
		return
	}
	defer h.eventBus.Unsubscribe(ch)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	// nginx buffers a proxied response by default and would hold every event
	// back; this header turns that off for this response alone.
	c.Header("X-Accel-Buffering", "no")
	// Send the status line and headers now. Left to the first event or
	// heartbeat they would keep a client connecting, and a proxy waiting for
	// its first byte, for a whole heartbeat interval on a quiet cache.
	c.Writer.Flush()

	heartbeat := time.NewTicker(h.heartbeat)
	defer heartbeat.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-ch:
			if !ok {
				return false
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return true
		case <-heartbeat.C:
			// A comment line: clients ignore it, and it is flushed like an event.
			io.WriteString(w, ": keep-alive\n\n")
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
