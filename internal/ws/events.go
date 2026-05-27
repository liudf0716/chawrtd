package ws

import (
	"fmt"
	"sync"
	"sync/atomic"
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

type eventListenerEntry struct {
	id       uint64
	listener EventListener
}

// EventBroadcaster manages event listeners for device events
type EventBroadcaster struct {
	mu           sync.RWMutex
	listeners    map[string][]eventListenerEntry // deviceID -> listeners
	allListeners []eventListenerEntry            // listeners for all events
	nextID       uint64
}

// NewEventBroadcaster creates a new event broadcaster
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		listeners:    make(map[string][]eventListenerEntry),
		allListeners: make([]eventListenerEntry, 0),
	}
}

// Subscribe registers a listener for events from a specific device.
// Returns an unsubscribe function.
func (b *EventBroadcaster) Subscribe(deviceID string, listener EventListener) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := atomic.AddUint64(&b.nextID, 1)
	b.listeners[deviceID] = append(b.listeners[deviceID], eventListenerEntry{id: id, listener: listener})
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if list, ok := b.listeners[deviceID]; ok {
			for i := range list {
				if list[i].id != id {
					continue
				}
				b.listeners[deviceID] = append(list[:i], list[i+1:]...)
				break
			}
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
	id := atomic.AddUint64(&b.nextID, 1)
	b.allListeners = append(b.allListeners, eventListenerEntry{id: id, listener: listener})
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i := range b.allListeners {
			if b.allListeners[i].id != id {
				continue
			}
			b.allListeners = append(b.allListeners[:i], b.allListeners[i+1:]...)
			break
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
		deviceListeners = make([]EventListener, 0, len(list))
		for _, entry := range list {
			deviceListeners = append(deviceListeners, entry.listener)
		}
	}
	if len(b.allListeners) > 0 {
		allListeners = make([]EventListener, 0, len(b.allListeners))
		for _, entry := range b.allListeners {
			allListeners = append(allListeners, entry.listener)
		}
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
