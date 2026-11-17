package public

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
)

// Behind a reverse proxy the stream must not be buffered and must not stay
// silent: with no event at all, a comment line still reaches the client.
func TestEventStreamDisablesProxyBufferingAndSendsHeartbeats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bus := cache.NewEventBus()
	handler := NewEventsHandler(bus)
	if handler.heartbeat != 25*time.Second {
		t.Fatalf("default heartbeat = %s, want 25s", handler.heartbeat)
	}
	handler.heartbeat = 5 * time.Millisecond
	router := gin.New()
	router.GET("/api/v1/events/stream", handler.Stream)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := response.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", got)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	// No event has been published: the first two heartbeats arrive on their
	// own, each a comment line closed by a blank line.
	reader := bufio.NewReader(response.Body)
	for index := 0; index < 2; index++ {
		for _, want := range []string{": keep-alive\n", "\n"} {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read heartbeat %d: %v", index, err)
			}
			if line != want {
				t.Fatalf("heartbeat %d line = %q, want %q", index, line, want)
			}
		}
	}

	// Events still arrive between heartbeats, in the form they always had.
	bus.Publish(cache.CacheEvent{PackageName: "left-pad"})
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			if !strings.Contains(line, `"package_name":"left-pad"`) {
				t.Fatalf("event line = %q", line)
			}
			break
		}
		if line != ": keep-alive\n" && line != "\n" {
			t.Fatalf("unexpected stream line %q", line)
		}
	}
}

// The stream answers at once: its headers do not wait for the first event or
// the first heartbeat, which on a quiet cache is 25 seconds away.
func TestEventStreamSendsHeadersBeforeTheFirstHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewEventsHandler(cache.NewEventBus())
	handler.heartbeat = time.Hour
	router := gin.New()
	router.GET("/api/v1/events/stream", handler.Stream)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("no response headers with no event and no heartbeat due: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("status = %d, headers = %v", response.StatusCode, response.Header)
	}
}
