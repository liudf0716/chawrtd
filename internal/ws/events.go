package ws

import (
	"context"
	"fmt"
	"sync"
)

// EventListener is a function that receives device events
type EventListener func(*DeviceEvent)

// DeviceEvent represents an event from a device
type DeviceEvent struct {
	Op       string                 `json:"op"`
	DeviceID string                 `json:"device_id"`
	Alias    string                 `json:"alias,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"`
	Time     int64                  `json:"time"`
}

// EventBroadcaster manages event listeners for device events
type EventBroadcaster struct {
	mu        sync.RWMutex
	listeners map[string][]EventListener // deviceID -> listeners
	allListeners []EventListener           // listeners for all events
}

// NewEventBroadcaster creates a new event broadcaster
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		listeners:    make(map[string][]EventListener),
		allListeners: make([]EventListener, 0),
	}
}

// Subscribe registers a listener for events from a specific device
func (b *EventBroadcaster) Subscribe(deviceID string, listener EventListener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[deviceID] = append(b.listeners[deviceID], listener)
}

// SubscribeAll registers a listener for all device events
func (b *EventBroadcaster) SubscribeAll(listener EventListener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.allListeners = append(b.allListeners, listener)
}

// Emit broadcasts an event to all listeners
func (b *EventBroadcaster) Emit(ctx context.Context, event *DeviceEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Notify device-specific listeners
	for _, listener := range b.listeners[event.DeviceID] {
		go func(l EventListener) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("event listener panic: %v\n", r)
				}
			}()
			select {
			case <-ctx.Done():
				return
			default:
				l(event)
			}
		}(listener)
	}

	// Notify global listeners
	for _, listener := range b.allListeners {
		go func(l EventListener) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("event listener panic: %v\n", r)
				}
			}()
			select {
			case <-ctx.Done():
				return
			default:
				l(event)
			}
		}(listener)
	}
}
