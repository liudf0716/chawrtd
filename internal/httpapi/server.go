package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"chawrtd/internal/ops"
	"chawrtd/internal/version"
	"chawrtd/internal/ws"
)

// maxRequestBodySize limits the maximum size of incoming JSON request bodies (1 MB).
const maxRequestBodySize = 1 << 20

type Server struct {
	defaultTimeout time.Duration
	mux            *http.ServeMux
	wsManager      *ws.Manager
	eventStreams   map[chan *ws.DeviceEvent]struct{}
	mu             sync.RWMutex
}

func New(defaultTimeout time.Duration, token string) *Server {
	s := &Server{
		defaultTimeout: defaultTimeout,
		mux:            http.NewServeMux(),
		wsManager:      ws.NewManager(token, &ws.SimpleLogger{}),
		eventStreams:   make(map[chan *ws.DeviceEvent]struct{}),
	}
	s.wsManager.SetRequestTimeout(defaultTimeout)

	// Subscribe to all device events for SSE stream forwarding
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
		// Log all HTTP requests (at debug level for non-websocket)
		if r.URL.Path != "/ws/clawwrt" {
			log.Printf("chawrtd http request method=%s path=%s remote=%s query=%q", r.Method, r.URL.Path, r.RemoteAddr, r.URL.RawQuery)
		}

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
					"error":  "websocket upgrade required",
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

	// Event stream (SSE)
	s.mux.HandleFunc("/v1/events/stream", s.handleEventsStream)

	s.mux.HandleFunc("/v1/frps/deploy", s.wrapJSON(s.handleFRPSDeploy))
	s.mux.HandleFunc("/v1/frps/status", s.wrapJSON(s.handleFRPSStatus))
	s.mux.HandleFunc("/v1/frps/verify", s.wrapJSON(s.handleFRPSVerify))
	s.mux.HandleFunc("/v1/frps/reset", s.wrapJSON(s.handleFRPSReset))

	s.mux.HandleFunc("/v1/vps/public-ip", s.wrapJSON(s.handleVPSPublicIP))

	s.mux.HandleFunc("/v1/wg/deploy", s.wrapJSON(s.handleWGDeploy))
	s.mux.HandleFunc("/v1/wg/status", s.wrapJSON(s.handleWGStatus))
	s.mux.HandleFunc("/v1/wg/reset", s.wrapJSON(s.handleWGReset))
	s.mux.HandleFunc("/v1/wg/verify", s.wrapJSON(s.handleWGVerify))
}

// handleDeviceCommand routes commands to devices
func (s *Server) handleDeviceCommand(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/device/")
	parts := strings.Split(trimmed, "/")
	deviceID := ""
	operation := ""
	if len(parts) >= 1 {
		deviceID = parts[0]
	}
	if len(parts) >= 2 {
		operation = parts[1]
	}
	if len(parts) >= 2 && parts[1] == "diagnose" {
		operation = ""
		if len(parts) >= 3 {
			switch parts[2] {
			case "dhcp":
				operation = "dhcp_diagnose"
			case "dns":
				operation = "dns_diagnose"
			case "http":
				operation = "http_service_diagnose"
			case "https":
				operation = "https_service_diagnose"
			default:
				operation = ""
			}
		}
	}
	log.Printf("chawrtd device command: method=%s deviceId=%q operation=%q", r.Method, deviceID, operation)

	if deviceID == "" {
		log.Printf("chawrtd device command: invalid path=%q", r.URL.Path)
		writeErr(w, http.StatusBadRequest, "invalid device path")
		return
	}

	if r.Method == http.MethodGet {
		// Get device info
		device := s.wsManager.GetDevice(deviceID)
		if device == nil {
			log.Printf("chawrtd GET /v1/device/%s: device not found", deviceID)
			writeErr(w, http.StatusNotFound, "device not found")
			return
		}
		log.Printf("chawrtd GET /v1/device/%s: found", deviceID)
		writeOK(w, device)
		return
	}

	if r.Method == http.MethodPost {
		if operation == "" {
			log.Printf("chawrtd POST /v1/device/%s: missing operation", deviceID)
			writeErr(w, http.StatusBadRequest, "invalid device path")
			return
		}

		// Send command to device
		var requestData map[string]any
		if err := decodeJSON(r, &requestData); err != nil {
			log.Printf("chawrtd POST /v1/device/%s/%s: decode error: %v", deviceID, operation, err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf(
			"chawrtd POST /v1/device/%s/%s: decoded requestData=%v",
			deviceID,
			operation,
			ws.SanitizeDataForLog(requestData),
		)

		expectResponse, parseErr := parseExpectResponse(r, requestData)
		if parseErr != nil {
			log.Printf("chawrtd POST /v1/device/%s/%s: invalid expect-response: %v", deviceID, operation, parseErr)
			writeErr(w, http.StatusBadRequest, parseErr.Error())
			return
		}

		var (
			result map[string]any
			err    error
		)
		if expectResponse {
			log.Printf("chawrtd POST /v1/device/%s/%s: sending command with timeout=%v", deviceID, operation, s.defaultTimeout)
			result, err = s.wsManager.SendCommand(deviceID, operation, requestData, s.defaultTimeout)
		} else {
			log.Printf("chawrtd POST /v1/device/%s/%s: sending command without waiting for response", deviceID, operation)
			result, err = s.wsManager.SendCommandNoWait(deviceID, operation, requestData)
		}
		if err != nil {
			if errors.Is(err, ws.ErrDeviceNotFound) {
				log.Printf("chawrtd POST /v1/device/%s/%s: device not found", deviceID, operation)
				writeErr(w, http.StatusNotFound, err.Error())
				return
			}
			log.Printf("chawrtd POST /v1/device/%s/%s: command error: %v", deviceID, operation, err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		log.Printf("chawrtd POST /v1/device/%s/%s: command success", deviceID, operation)
		writeOK(w, result)
		return
	}

	writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
}

// handleDevicesList returns list of connected devices
func (s *Server) handleDevicesList(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("method not allowed")
	}

	devices := s.wsManager.ListDevices()

	deviceIDs := make([]string, len(devices))
	for i, dev := range devices {
		deviceIDs[i] = dev.DeviceID
	}
	log.Printf("chawrtd GET /v1/devices: returning %d connected devices: %v", len(devices), deviceIDs)

	// Convert DeviceSession to map for JSON response
	var devicesList []map[string]any
	for _, session := range devices {
		deviceMap := map[string]any{
			"device_id":    session.DeviceID,
			"connected_at": session.ConnectedAt.UnixMilli(),
			"last_seen_at": session.LastSeenAt.UnixMilli(),
			"remote_addr":  session.RemoteAddr,
		}
		if session.Alias != "" {
			deviceMap["alias"] = session.Alias
		}
		if session.DeviceInfo != nil {
			deviceMap["device_info"] = session.DeviceInfo
		}
		devicesList = append(devicesList, deviceMap)
	}

	writeOK(w, map[string]any{
		"devices": devicesList,
		"count":   len(devices),
	})
	return nil
}

// handleEventsStream exposes a server-sent event stream for device events.
func (s *Server) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
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

	// Keep the stream alive during idle periods so clients do not get dropped by
	// intermediary idle timeouts (reverse proxies, NAT, etc.).
	heartbeatTicker := time.NewTicker(25 * time.Second)
	defer heartbeatTicker.Stop()

	if _, err := w.Write([]byte(": connected\n\n")); err != nil {
		return
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeatTicker.C:
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-stream:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("event: device\n")); err != nil {
				return
			}
			if _, err := w.Write([]byte("data: ")); err != nil {
				return
			}
			if _, err := w.Write(payload); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
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
	writeOK(w, map[string]any{"aliases": aliases})
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

	writeOK(w, map[string]any{"ok": true})
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

	writeOK(w, map[string]any{"ok": true})
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeOK(w, map[string]any{"service": "chawrtd", "version": version.Version})
}

type jsonHandler func(http.ResponseWriter, *http.Request) error

func (s *Server) wrapJSON(next jsonHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := next(w, r); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
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
	writeOK(w, res)
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
	writeOK(w, res)
	return nil
}

func (s *Server) handleFRPSVerify(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}
	var req ops.VerifyFRPSRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	res, err := ops.VerifyFRPS(req, s.defaultTimeout)
	if err != nil {
		return err
	}
	writeOK(w, res)
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
	writeOK(w, res)
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
	writeOK(w, res)
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
	writeOK(w, res)
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
	writeOK(w, res)
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
	writeOK(w, res)
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
	writeOK(w, res)
	return nil
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodySize))
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	return nil
}

func parseExpectResponse(r *http.Request, requestData map[string]any) (bool, error) {
	if rawHeader := strings.TrimSpace(r.Header.Get("X-Expect-Response")); rawHeader != "" {
		value, ok := parseBooleanLike(rawHeader)
		if !ok {
			return false, errors.New("invalid X-Expect-Response header")
		}
		return value, nil
	}

	rawExpectResponse, ok := requestData["__expect_response"]
	if !ok {
		return true, nil
	}
	delete(requestData, "__expect_response")

	switch v := rawExpectResponse.(type) {
	case bool:
		return v, nil
	case string:
		parsed, ok := parseBooleanLike(v)
		if !ok {
			return false, errors.New("__expect_response must be a boolean-like value")
		}
		return parsed, nil
	default:
		return false, errors.New("__expect_response must be bool or string")
	}
}

func parseBooleanLike(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeOK wraps a successful result in the standard API envelope.
func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"data": data,
	})
}

// writeErr writes a standardized error response.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": msg,
	})
}
