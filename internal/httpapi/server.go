package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"chawrtd/internal/ops"
)

type Server struct {
	defaultTimeout time.Duration
	mux            *http.ServeMux
}

func New(defaultTimeout time.Duration) *Server {
	s := &Server{
		defaultTimeout: defaultTimeout,
		mux:            http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/healthz", s.handleHealth)

	s.mux.HandleFunc("/v1/frps/deploy", s.wrapJSON(s.handleFRPSDeploy))
	s.mux.HandleFunc("/v1/frps/status", s.wrapJSON(s.handleFRPSStatus))
	s.mux.HandleFunc("/v1/frps/reset", s.wrapJSON(s.handleFRPSReset))

	s.mux.HandleFunc("/v1/vps/public-ip", s.wrapJSON(s.handleVPSPublicIP))

	s.mux.HandleFunc("/v1/wg/deploy", s.wrapJSON(s.handleWGDeploy))
	s.mux.HandleFunc("/v1/wg/status", s.wrapJSON(s.handleWGStatus))
	s.mux.HandleFunc("/v1/wg/reset", s.wrapJSON(s.handleWGReset))
	s.mux.HandleFunc("/v1/wg/verify", s.wrapJSON(s.handleWGVerify))
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
