package ws

import (
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
	mu          sync.RWMutex
	listeners   map[string][]EventListener // deviceID -> listeners
	allListeners []EventListener            // listeners for all events
}

// NewEventBroadcaster creates a new event broadcaster
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		listeners:    make(map[string][]EventListener),
		allListeners: make([]EventListener, 0),
	}
}

// Subscribe registers a listener for events from a specific device.
// Returns an unsubscribe function.
func (b *EventBroadcaster) Subscribe(deviceID string, listener EventListener) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[deviceID] = append(b.listeners[deviceID], listener)
	idx := len(b.listeners[deviceID]) - 1
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if list, ok := b.listeners[deviceID]; ok && idx < len(list) {
			b.listeners[deviceID] = append(list[:idx], list[idx+1:]...)
			if len(b.listeners[deviceID]) == 0 {
				delete(b.listeners, deviceID)
			}
		}
	}
}

// SubscribeAll registers a listener for all events.
// Returns an unsubscribe function.
func (b *EventBroadcaster) SubscribeAll(listener EventListener) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.allListeners = append(b.allListeners, listener)
	idx := len(b.allListeners) - 1
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if idx < len(b.allListeners) {
			b.allListeners = append(b.allListeners[:idx], b.allListeners[idx+1:]...)
		}
	}
}

// Emit broadcasts an event to all listeners synchronously.
// Listeners are called in the same goroutine to avoid goroutine leaks.
// Panics in listeners are recovered and logged.
func (b *EventBroadcaster) Emit(event *DeviceEvent) {
	b.mu.RLock()
	// Copy listener slices under lock to minimize hold time.
	var deviceListeners, allListeners []EventListener
	if list, ok := b.listeners[event.DeviceID]; ok {
		deviceListeners = make([]EventListener, len(list))
		copy(deviceListeners, list)
	}
	if len(b.allListeners) > 0 {
		allListeners = make([]EventListener, len(b.allListeners))
		copy(allListeners, b.allListeners)
	}
	b.mu.RUnlock()

	// Notify device-specific listeners
	for _, listener := range deviceListeners {
		safeCall(listener, event)
	}

	// Notify global listeners
	for _, listener := range allListeners {
		safeCall(listener, event)
	}
}

// safeCall invokes a listener with panic recovery.
func safeCall(l EventListener, event *DeviceEvent) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("event listener panic: %v\n", r)
		}
	}()
	l(event)
}
