package cache

import (
	"sync"
	"time"
)

const MaxSubscribers = 20

type CacheEvent struct {
	Time        time.Time `json:"time"`
	PackageName string    `json:"package_name"`
	FileName    string    `json:"file_name"`
	AdapterType string    `json:"adapter_type"`
	Hit         bool      `json:"hit"`
	Upstream    string    `json:"upstream,omitempty"`
	Size        int64     `json:"size,omitempty"`
}

type EventBus struct {
	subscribers map[chan CacheEvent]struct{}
	mu          sync.RWMutex
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan CacheEvent]struct{}),
	}
}

func (eb *EventBus) Subscribe() (chan CacheEvent, bool) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if len(eb.subscribers) >= MaxSubscribers {
		return nil, false
	}
	ch := make(chan CacheEvent, 64)
	eb.subscribers[ch] = struct{}{}
	return ch, true
}

func (eb *EventBus) Unsubscribe(ch chan CacheEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	delete(eb.subscribers, ch)
	close(ch)
}

func (eb *EventBus) Publish(event CacheEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
			// drop if subscriber is slow
		}
	}
}
