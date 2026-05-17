package ws

import (
	"time"
)

// DeviceSession represents a connected device
type DeviceSession struct {
	DeviceID    string            `json:"device_id"`
	ConnectedAt time.Time         `json:"connected_at"`
	LastSeenAt  time.Time         `json:"last_seen_at"`
	RemoteAddr  string            `json:"remote_addr"`
	Gateway     map[string]any    `json:"gateway,omitempty"`
	DeviceInfo  map[string]any    `json:"device_info,omitempty"`
	AuthMode    *int              `json:"auth_mode,omitempty"`
	Alias       string            `json:"alias,omitempty"`
	ws          WSConn            `json:"-"`
}

// Message represents a WebSocket message
type Message struct {
	Op        string                 `json:"op,omitempty"`
	Type      string                 `json:"type,omitempty"`
	DeviceID  string                 `json:"device_id,omitempty"`
	ReqID     interface{}            `json:"req_id,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	Token     string                 `json:"token,omitempty"`
	Mode      interface{}            `json:"mode,omitempty"`
	Gateway   interface{}            `json:"gateway,omitempty"`
	Response  interface{}            `json:"response,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// WSConn is an interface for WebSocket connection
type WSConn interface {
	SendJSON(msg interface{}) error
	Close() error
	IsClosed() bool
}

// PendingRequest tracks an in-flight request
type PendingRequest struct {
	DeviceID  string
	ReqID     interface{}
	ReqChan   chan interface{}
	Timer     *time.Timer
	CreatedAt time.Time
}

