package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"chawrtd/internal/ops"
	"chawrtd/internal/ws"
)

type Server struct {
	defaultTimeout time.Duration
	mux            *http.ServeMux
	wsManager      *ws.Manager
	eventCallbacks map[string][]string // deviceID -> list of callback URLs
	eventStreams   map[chan *ws.DeviceEvent]struct{}
	mu             sync.RWMutex
}

func New(defaultTimeout time.Duration, token string) *Server {
	s := &Server{
		defaultTimeout: defaultTimeout,
		mux:            http.NewServeMux(),
		wsManager:      ws.NewManager(token, &ws.SimpleLogger{}),
		eventCallbacks: make(map[string][]string),
		eventStreams:   make(map[chan *ws.DeviceEvent]struct{}),
	}
	s.wsManager.SetRequestTimeout(defaultTimeout)
	
	// Subscribe to all device events for callback forwarding
	s.wsManager.SubscribeAllEvents(s.forwardEventToCallbacks)
	s.wsManager.SubscribeAllEvents(s.forwardEventToStreams)
	
	s.registerRoutes()
	return s
}

// GetWSManager returns the WebSocket manager for external use
func (s *Server) GetWSManager() *ws.Manager {
	return s.wsManager
}

// InitializeAliasStore initializes the alias store for device names
func (s *Server) InitializeAliasStore(filePath string) error {
	aliases, err := ws.NewAliasStore(filePath)
	if err != nil {
		return err
	}
	// Note: This sets the internal aliases field, which is a private field
	// We'll need to add a public method to Manager for this
	return s.wsManager.SetAliasStore(aliases)
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket upgrade for device connections
		if r.URL.Path == "/ws/clawwrt" {
			connectionHeader := r.Header.Get("Connection")
			upgradeHeader := r.Header.Get("Upgrade")
			xForwardedProto := r.Header.Get("X-Forwarded-Proto")
			isUpgrade, reason := websocketHandshakeCheck(r)

			log.Printf(
				"chawrtd websocket request path=%s method=%s remote=%s ua=%q conn=%q upgrade=%q x_forwarded_proto=%q tls=%t is_upgrade=%t reason=%q",
				r.URL.Path,
				r.Method,
				r.RemoteAddr,
				r.UserAgent(),
				connectionHeader,
				upgradeHeader,
				xForwardedProto,
				r.TLS != nil,
				isUpgrade,
				reason,
			)

			if !isUpgrade {
				writeJSON(w, http.StatusUpgradeRequired, map[string]any{
					"error": "websocket upgrade required",
					"reason": reason,
				})
				return
			}

			if err := s.wsManager.HandleUpgrade(w, r); err != nil {
				log.Printf("chawrtd websocket upgrade failed remote=%s err=%v", r.RemoteAddr, err)
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			}
			return
		}

		// Device command routing: /v1/device/{deviceId}/{operation}
		if strings.HasPrefix(r.URL.Path, "/v1/device/") {
			s.handleDeviceCommand(w, r)
			return
		}

		// Delegate to existing routes
		s.mux.ServeHTTP(w, r)
	})
}

func websocketHandshakeCheck(r *http.Request) (bool, string) {
	if r.Method != http.MethodGet {
		return false, "method must be GET"
	}
	if !headerHasToken(r.Header.Get("Connection"), "upgrade") {
		return false, "connection header missing upgrade token"
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false, "upgrade header must be websocket"
	}
	return true, "ok"
}

func headerHasToken(value string, token string) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/healthz", s.handleHealth)

	// Device list endpoints
	s.mux.HandleFunc("/v1/devices", s.wrapJSON(s.handleDevicesList))

	// Device alias management
	s.mux.HandleFunc("/v1/devices/aliases", s.wrapJSON(s.handleListAliases))
	s.mux.HandleFunc("/v1/devices/alias/set", s.wrapJSON(s.handleSetAlias))
	s.mux.HandleFunc("/v1/devices/alias/delete", s.wrapJSON(s.handleDeleteAlias))

	// Event callback management
	s.mux.HandleFunc("/v1/events/subscribe", s.wrapJSON(s.handleSubscribeEvents))
	s.mux.HandleFunc("/v1/events/unsubscribe", s.wrapJSON(s.handleUnsubscribeEvents))
	s.mux.HandleFunc("/v1/events/stream", s.handleEventsStream)

	s.mux.HandleFunc("/v1/frps/deploy", s.wrapJSON(s.handleFRPSDeploy))
	s.mux.HandleFunc("/v1/frps/status", s.wrapJSON(s.handleFRPSStatus))
	s.mux.HandleFunc("/v1/frps/reset", s.wrapJSON(s.handleFRPSReset))

	s.mux.HandleFunc("/v1/vps/public-ip", s.wrapJSON(s.handleVPSPublicIP))

	s.mux.HandleFunc("/v1/wg/deploy", s.wrapJSON(s.handleWGDeploy))
	s.mux.HandleFunc("/v1/wg/status", s.wrapJSON(s.handleWGStatus))
	s.mux.HandleFunc("/v1/wg/reset", s.wrapJSON(s.handleWGReset))
	s.mux.HandleFunc("/v1/wg/verify", s.wrapJSON(s.handleWGVerify))
}

// handleDeviceCommand routes commands to devices
func (s *Server) handleDeviceCommand(w http.ResponseWriter, r *http.Request) {
	// Parse path: /v1/device/{deviceId}/{operation}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/device/"), "/")
	if len(parts) < 2 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid device path"})
		return
	}

	deviceID := parts[0]
	operation := parts[1]

	if r.Method == http.MethodGet {
		// Get device info
		device := s.wsManager.GetDevice(deviceID)
		if device == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "device not found"})
			return
		}
		writeJSON(w, http.StatusOK, device)
		return
	}

	if r.Method == http.MethodPost {
		// Send command to device
		var payload map[string]any
		if err := decodeJSON(r, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		result, err := s.wsManager.SendCommand(deviceID, operation, payload, s.defaultTimeout)
		if err != nil {
			if errors.Is(err, ws.ErrDeviceNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, result)
		return
	}

	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
}

// handleDevicesList returns list of connected devices
func (s *Server) handleDevicesList(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("method not allowed")
	}

	devices := s.wsManager.ListDevices()
	writeJSON(w, http.StatusOK, map[string]any{
		"devices": devices,
		"count":   len(devices),
	})
	return nil
}

// handleSubscribeEvents registers an event callback URL
func (s *Server) handleSubscribeEvents(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}

	var req struct {
		CallbackURL string `json:"callback_url"`
		DeviceID    string `json:"device_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	if req.CallbackURL == "" || req.DeviceID == "" {
		return errors.New("callback_url and device_id are required")
	}

	s.mu.Lock()
	s.eventCallbacks[req.DeviceID] = append(s.eventCallbacks[req.DeviceID], req.CallbackURL)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleUnsubscribeEvents unregisters an event callback URL
func (s *Server) handleUnsubscribeEvents(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}

	var req struct {
		CallbackURL string `json:"callback_url"`
		DeviceID    string `json:"device_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	s.mu.Lock()
	if cbs, ok := s.eventCallbacks[req.DeviceID]; ok {
		filtered := make([]string, 0, len(cbs))
		for _, cb := range cbs {
			if cb != req.CallbackURL {
				filtered = append(filtered, cb)
			}
		}
		if len(filtered) == 0 {
			delete(s.eventCallbacks, req.DeviceID)
		} else {
			s.eventCallbacks[req.DeviceID] = filtered
		}
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleEventsStream exposes a server-sent event stream for device events.
func (s *Server) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	stream := make(chan *ws.DeviceEvent, 32)
	s.mu.Lock()
	s.eventStreams[stream] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.eventStreams, stream)
		s.mu.Unlock()
	}()

	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-stream:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("event: device\n"))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(payload)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}

// forwardEventToCallbacks forwards device events to registered callbacks
func (s *Server) forwardEventToCallbacks(event *ws.DeviceEvent) {
	s.mu.RLock()
	callbacks := make([]string, 0)
	if cbs, ok := s.eventCallbacks[event.DeviceID]; ok {
		callbacks = append(callbacks, cbs...)
	}
	s.mu.RUnlock()

	if len(callbacks) == 0 {
		return
	}

	// Forward to each callback URL asynchronously
	go func() {
		data, _ := json.Marshal(event)
		for _, url := range callbacks {
			go func(cbURL string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cbURL, bytes.NewReader(data))
				req.Header.Set("Content-Type", "application/json")

				client := &http.Client{Timeout: 5 * time.Second}
				_, _ = client.Do(req)
			}(url)
		}
	}()
}

// forwardEventToStreams forwards device events to SSE subscribers.
func (s *Server) forwardEventToStreams(event *ws.DeviceEvent) {
	s.mu.RLock()
	streams := make([]chan *ws.DeviceEvent, 0, len(s.eventStreams))
	for stream := range s.eventStreams {
		streams = append(streams, stream)
	}
	s.mu.RUnlock()

	for _, stream := range streams {
		select {
		case stream <- event:
		default:
			// Drop events for slow subscribers instead of blocking device handling.
		}
	}
}

// handleListAliases returns all device aliases
func (s *Server) handleListAliases(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("method not allowed")
	}

	aliases := s.wsManager.ListAliases()
	writeJSON(w, http.StatusOK, map[string]any{
		"aliases": aliases,
	})
	return nil
}

// handleSetAlias sets or updates a device alias
func (s *Server) handleSetAlias(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}

	var req struct {
		DeviceID string `json:"device_id"`
		Alias    string `json:"alias"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	if req.DeviceID == "" || req.Alias == "" {
		return errors.New("device_id and alias are required")
	}

	if err := s.wsManager.SetAlias(req.DeviceID, req.Alias); err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleDeleteAlias removes a device alias
func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}

	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	if req.DeviceID == "" {
		return errors.New("device_id is required")
	}

	if err := s.wsManager.DeleteAlias(req.DeviceID); err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "chawrtd"})
}

type jsonHandler func(http.ResponseWriter, *http.Request) error

func (s *Server) wrapJSON(next jsonHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := next(w, r); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
	}
}

func (s *Server) handleFRPSDeploy(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}
	var req ops.DeployFRPSRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	res, err := ops.DeployFRPS(req, s.defaultTimeout)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleFRPSStatus(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("method not allowed")
	}
	res, err := ops.GetFRPSStatus(s.defaultTimeout)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleFRPSReset(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}
	res, err := ops.ResetFRPS(s.defaultTimeout)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleVPSPublicIP(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("method not allowed")
	}
	res, err := ops.GetVpsPublicIP(s.defaultTimeout)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleWGDeploy(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}
	var req ops.DeployWireGuardRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	res, err := ops.DeployWireGuard(req, s.defaultTimeout)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleWGStatus(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("method not allowed")
	}
	res, err := ops.GetWireGuardStatus(s.defaultTimeout)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleWGReset(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}
	var req ops.ResetWireGuardRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	res, err := ops.ResetWireGuard(req, s.defaultTimeout)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleWGVerify(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}
	var req ops.VerifyWireGuardRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	res, err := ops.VerifyWireGuardServer(req, s.defaultTimeout)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
