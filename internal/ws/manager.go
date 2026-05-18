package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	AuthModeCloud  = 0
	AuthModeBypass = 1
	AuthModeLocal  = 2

	defaultPingInterval   = 30 * time.Second
	defaultPongWaitTime   = 60 * time.Second
	defaultWriteDeadline  = 10 * time.Second
	defaultReadDeadline   = 60 * time.Second
	defaultRequestTimeout = 30 * time.Second
)

var (
	ErrDeviceNotFound      = errors.New("device not found")
	ErrDeviceDisconnected  = errors.New("device disconnected")
	ErrInvalidToken        = errors.New("invalid token")
	ErrMessageTimeout      = errors.New("message timeout")
	ErrInvalidMessage      = errors.New("invalid message")
	ErrUnauthorized        = errors.New("unauthorized")
)

// Manager manages WebSocket connections from clawwrt devices
type Manager struct {
	mu           sync.RWMutex
	sessions     map[string]*DeviceSession
	pending      map[string]map[interface{}]*PendingRequest
	upgrader     websocket.Upgrader
	token        string
	requestTimeout time.Duration
	logger       Logger
	broadcaster  *EventBroadcaster
	aliases      *AliasStore

	// Event handlers for device connections/disconnections
	onConnect    func(*DeviceSession)
	onDisconnect func(string)
}

// Logger interface for logging
type Logger interface {
	Info(string)
	Warn(string)
	Error(string)
}

// SimpleLogger implements Logger
type SimpleLogger struct{}

func (l *SimpleLogger) Info(msg string)  { log.Println("[INFO]", msg) }
func (l *SimpleLogger) Warn(msg string)  { log.Println("[WARN]", msg) }
func (l *SimpleLogger) Error(msg string) { log.Println("[ERROR]", msg) }

// NewManager creates a new WebSocket device manager
func NewManager(token string, logger Logger) *Manager {
	if logger == nil {
		logger = &SimpleLogger{}
	}
	return &Manager{
		sessions:       make(map[string]*DeviceSession),
		pending:        make(map[string]map[interface{}]*PendingRequest),
		upgrader:       websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		token:          token,
		requestTimeout: defaultRequestTimeout,
		logger:         logger,
		broadcaster:    NewEventBroadcaster(),
	}
}

// NewManagerWithAliases creates a new WebSocket device manager with alias persistence
func NewManagerWithAliases(token string, logger Logger, aliasFilePath string) (*Manager, error) {
	m := NewManager(token, logger)

	aliases, err := NewAliasStore(aliasFilePath)
	if err != nil {
		return nil, err
	}

	m.aliases = aliases
	return m, nil
}

// SetRequestTimeout sets the request timeout for device calls
func (m *Manager) SetRequestTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestTimeout = d
}

// OnConnect registers a callback for device connections
func (m *Manager) OnConnect(fn func(*DeviceSession)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onConnect = fn
}

// OnDisconnect registers a callback for device disconnections
func (m *Manager) OnDisconnect(fn func(string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDisconnect = fn
}

// SubscribeEvents registers a listener for events from a specific device
func (m *Manager) SubscribeEvents(deviceID string, listener EventListener) {
	m.broadcaster.Subscribe(deviceID, listener)
}

// SubscribeAllEvents registers a listener for all device events
func (m *Manager) SubscribeAllEvents(listener EventListener) {
	m.broadcaster.SubscribeAll(listener)
}

// GetAlias returns the alias for a device
func (m *Manager) GetAlias(deviceID string) string {
	if m.aliases == nil {
		return ""
	}
	return m.aliases.Get(deviceID)
}

// SetAlias sets the alias for a device
func (m *Manager) SetAlias(deviceID, alias string) error {
	if m.aliases == nil {
		return fmt.Errorf("alias store not initialized")
	}
	if err := m.aliases.Set(deviceID, alias); err != nil {
		return err
	}

	// Update the session if it's connected
	m.mu.RLock()
	session, exists := m.sessions[deviceID]
	m.mu.RUnlock()

	if exists && session != nil {
		session.Alias = alias
	}

	return nil
}

// DeleteAlias removes the alias for a device
func (m *Manager) DeleteAlias(deviceID string) error {
	if m.aliases == nil {
		return fmt.Errorf("alias store not initialized")
	}
	if err := m.aliases.Delete(deviceID); err != nil {
		return err
	}

	// Update the session if it's connected
	m.mu.RLock()
	session, exists := m.sessions[deviceID]
	m.mu.RUnlock()

	if exists && session != nil {
		session.Alias = ""
	}

	return nil
}

// ListAliases returns all device aliases
func (m *Manager) ListAliases() map[string]string {
	if m.aliases == nil {
		return make(map[string]string)
	}
	return m.aliases.List()
}

// SetAliasStore sets the alias store for the manager
func (m *Manager) SetAliasStore(store *AliasStore) error {
	if store == nil {
		return fmt.Errorf("alias store cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.aliases = store
	return nil
}

// HandleUpgrade handles the WebSocket upgrade request
func (m *Manager) HandleUpgrade(w http.ResponseWriter, r *http.Request) error {
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("websocket upgrade failed: %w", err)
	}

	go m.handleConnection(conn, r.RemoteAddr)
	return nil
}

// handleConnection handles a new WebSocket connection
func (m *Manager) handleConnection(conn *websocket.Conn, remoteAddr string) {
	defer conn.Close()

	// Read initial connect message
	msg, err := m.readMessage(conn)
	if err != nil {
		m.logger.Warn(fmt.Sprintf("failed to read connect message: %v", err))
		return
	}

	// Validate token
	if msg.Token != m.token {
		m.logger.Warn(fmt.Sprintf("invalid token from %s", remoteAddr))
		m.sendError(conn, msg.ReqID, ErrInvalidToken)
		return
	}

	deviceID := msg.DeviceID
	if deviceID == "" {
		m.logger.Warn(fmt.Sprintf("missing device_id in connect message from %s", remoteAddr))
		m.sendError(conn, msg.ReqID, ErrInvalidMessage)
		return
	}

	// Create device session
	session := &DeviceSession{
		DeviceID:    deviceID,
		ConnectedAt: time.Now(),
		LastSeenAt:  time.Now(),
		RemoteAddr:  remoteAddr,
		AuthMode:    parseAuthMode(msg.Mode),
		ws:          &standardWebsocketConn{conn: conn},
		Alias:       m.GetAlias(deviceID),
	}

	if msg.Gateway != nil {
		session.Gateway = toMap(msg.Gateway)
	}
	if msg.Data != nil {
		if devInfo, ok := msg.Data["device_info"]; ok {
			session.DeviceInfo = toMap(devInfo)
		}
	}

	// Register session
	m.mu.Lock()
	oldSession, existed := m.sessions[deviceID]
	m.sessions[deviceID] = session
	if m.pending[deviceID] == nil {
		m.pending[deviceID] = make(map[interface{}]*PendingRequest)
	}
	m.mu.Unlock()

	// Close old session if it existed
	if existed && oldSession.ws != nil {
		oldSession.ws.Close()
	}

	// Send connect response
	m.sendResponse(conn, msg.ReqID, map[string]any{"ok": true})

	// Notify connected callback
	if m.onConnect != nil {
		m.onConnect(session)
	}

	m.logger.Info(fmt.Sprintf("device %s connected from %s", deviceID, remoteAddr))

	// Message loop
	conn.SetReadDeadline(time.Now().Add(defaultReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(defaultReadDeadline))
		return nil
	})

	ticker := time.NewTicker(defaultPingInterval)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			m.mu.RLock()
			currentSession := m.sessions[deviceID]
			m.mu.RUnlock()

			if currentSession != session {
				return
			}

			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(defaultWriteDeadline)); err != nil {
				return
			}
		}
	}()

	for {
		msg, err := m.readMessage(conn)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				m.logger.Warn(fmt.Sprintf("websocket error from device %s: %v", deviceID, err))
			}
			break
		}

		m.handleMessage(session, msg)
	}

	// Cleanup
	m.mu.Lock()
	if m.sessions[deviceID] == session {
		delete(m.sessions, deviceID)
		delete(m.pending, deviceID)
	}
	m.mu.Unlock()

	m.logger.Info(fmt.Sprintf("device %s disconnected", deviceID))
	if m.onDisconnect != nil {
		m.onDisconnect(deviceID)
	}
}

// handleMessage processes a message from a device
func (m *Manager) handleMessage(session *DeviceSession, msg Message) {
	op := msg.Op
	if op == "" {
		op = msg.Type
	}

	session.LastSeenAt = time.Now()
	log.Printf("chawrtd websocket message from device=%q op=%q reqID=%v", session.DeviceID, op, msg.ReqID)

	// Handle response messages
	if isResponse(op) {
		log.Printf("chawrtd websocket response from device=%q reqID=%v", session.DeviceID, msg.ReqID)
		m.handleResponse(session.DeviceID, msg)
		return
	}

	// Handle event messages (device push events)
	if isEventMessage(op) {
		log.Printf("chawrtd device event from=%q op=%q", session.DeviceID, op)
		m.broadcaster.Emit(context.Background(), &DeviceEvent{
			Op:       op,
			DeviceID: session.DeviceID,
			Data:     msg.Data,
			Time:     time.Now().UnixMilli(),
		})
		return
	}

	// Handle other message types as needed
}

// handleResponse handles a response from a device
func (m *Manager) handleResponse(deviceID string, msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reqID := msg.ReqID
	if reqID == nil {
		return
	}

	pending, ok := m.pending[deviceID][reqID]
	if !ok {
		log.Printf("chawrtd response from device=%q reqID=%v: no pending request", deviceID, reqID)
		return
	}

	log.Printf("chawrtd response from device=%q reqID=%v: found pending request", deviceID, reqID)
	delete(m.pending[deviceID], reqID)

	select {
	case pending.ReqChan <- msg:
	case <-time.After(100 * time.Millisecond):
		log.Printf("chawrtd response from device=%q reqID=%v: response channel blocked", deviceID, reqID)
	}
}

// readMessage reads and parses a JSON message from the connection
func (m *Manager) readMessage(conn *websocket.Conn) (Message, error) {
	var msg Message
	err := conn.ReadJSON(&msg)
	return msg, err
}

// sendError sends an error response
func (m *Manager) sendError(conn *websocket.Conn, reqID interface{}, err error) {
	resp := Message{
		ReqID:  reqID,
		Error:  err.Error(),
		Op:     "request_error",
	}
	conn.WriteJSON(resp)
}

// sendResponse sends a successful response
func (m *Manager) sendResponse(conn *websocket.Conn, reqID interface{}, data map[string]any) {
	resp := Message{
		ReqID: reqID,
		Data:  data,
	}
	conn.WriteJSON(resp)
}

// SendCommand sends a command to a device and waits for response
func (m *Manager) SendCommand(deviceID string, op string, payload map[string]any, timeout time.Duration) (map[string]any, error) {
	log.Printf("chawrtd SendCommand: deviceId=%q op=%q timeout=%v", deviceID, op, timeout)

	m.mu.RLock()
	session, ok := m.sessions[deviceID]
	m.mu.RUnlock()

	if !ok {
		log.Printf("chawrtd SendCommand: deviceId=%q op=%q - device not found", deviceID, op)
		return nil, ErrDeviceNotFound
	}

	if timeout == 0 {
		m.mu.RLock()
		timeout = m.requestTimeout
		m.mu.RUnlock()
	}

	reqID := generateReqID()
	log.Printf("chawrtd SendCommand: deviceId=%q op=%q reqID=%v - generated request", deviceID, op, reqID)

	msg := Message{
		Op:      op,
		ReqID:   reqID,
		Payload: payload,
	}

	// Register pending request
	m.mu.Lock()
	if m.pending[deviceID] == nil {
		m.pending[deviceID] = make(map[interface{}]*PendingRequest)
	}

	reqChan := make(chan interface{}, 1)
	timer := time.NewTimer(timeout)

	pending := &PendingRequest{
		DeviceID:  deviceID,
		ReqID:     reqID,
		ReqChan:   reqChan,
		Timer:     timer,
		CreatedAt: time.Now(),
	}

	m.pending[deviceID][reqID] = pending
	m.mu.Unlock()

	defer func() {
		timer.Stop()
		m.mu.Lock()
		delete(m.pending[deviceID], reqID)
		m.mu.Unlock()
	}()

	// Send command
	if err := session.ws.SendJSON(msg); err != nil {
		log.Printf("chawrtd SendCommand: deviceId=%q op=%q reqID=%v - send failed: %v", deviceID, op, reqID, err)
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	log.Printf("chawrtd SendCommand: deviceId=%q op=%q reqID=%v - sent, waiting for response (timeout=%v)", deviceID, op, reqID, timeout)

	// Wait for response or timeout
	select {
	case resp := <-reqChan:
		if respMsg, ok := resp.(Message); ok {
			if respMsg.Error != "" {
				log.Printf("chawrtd SendCommand: deviceId=%q op=%q reqID=%v - response error: %s", deviceID, op, reqID, respMsg.Error)
				return nil, errors.New(respMsg.Error)
			}
			log.Printf("chawrtd SendCommand: deviceId=%q op=%q reqID=%v - response success", deviceID, op, reqID)
			return respMsg.Data, nil
		}
		log.Printf("chawrtd SendCommand: deviceId=%q op=%q reqID=%v - invalid response message", deviceID, op, reqID)
		return nil, ErrInvalidMessage
	case <-timer.C:
		log.Printf("chawrtd SendCommand: deviceId=%q op=%q reqID=%v - timeout after %v", deviceID, op, reqID, timeout)
		return nil, ErrMessageTimeout
	}
}

// ListDevices returns a list of all connected devices
func (m *Manager) ListDevices() []DeviceSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]DeviceSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		result = append(result, *session)
	}
	return result
}

// GetDevice returns a device session by ID
func (m *Manager) GetDevice(deviceID string) *DeviceSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, _ := m.sessions[deviceID]
	return session
}

// Helper functions

func parseAuthMode(mode interface{}) *int {
	switch v := mode.(type) {
	case float64:
		if v == AuthModeCloud || v == AuthModeBypass || v == AuthModeLocal {
			val := int(v)
			return &val
		}
	case string:
		switch v {
		case "cloud":
			val := AuthModeCloud
			return &val
		case "bypass":
			val := AuthModeBypass
			return &val
		case "local":
			val := AuthModeLocal
			return &val
		}
	}
	return nil
}

func toMap(v interface{}) map[string]any {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	data, _ := json.Marshal(v)
	var m map[string]any
	json.Unmarshal(data, &m)
	return m
}

func isResponse(op string) bool {
	return op == "request_error" || (len(op) > 9 && op[len(op)-9:] == "_response") || (len(op) > 6 && op[len(op)-6:] == "_error")
}

func isEventMessage(op string) bool {
	// Event messages are messages that are not responses
	// Examples: client_connected, client_disconnected, net_link_up, net_link_down, usb_storage_attached, usb_storage_detached
	if op == "" {
		return false
	}
	return !isResponse(op) && op != "connect" && op != "heartbeat" && op != "ping" && op != "pong"
}

func generateReqID() string {
	return fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), net.ParseIP("127.0.0.1").To4()[3])
}

// standardWebsocketConn wraps a gorilla/websocket connection
type standardWebsocketConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *standardWebsocketConn) SendJSON(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(msg)
}

func (c *standardWebsocketConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

func (c *standardWebsocketConn) IsClosed() bool {
	return false // Gorilla doesn't expose closed state directly
}
