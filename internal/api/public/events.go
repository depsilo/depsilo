package public

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
)

type EventsHandler struct {
	eventBus *cache.EventBus
}

func NewEventsHandler(eventBus *cache.EventBus) *EventsHandler {
	return &EventsHandler{eventBus: eventBus}
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
	c.Header("Access-Control-Allow-Origin", "*")

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-ch:
			if !ok {
				return false
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
