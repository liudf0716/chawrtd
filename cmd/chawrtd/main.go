package main

import (
	"log"
	"net/http"

	"chawrtd/internal/config"
	"chawrtd/internal/httpapi"
)

func main() {
	cfg := config.Load()
	server := httpapi.New(cfg.DefaultTimeout)
	wsScheme := "ws"
	if cfg.TLSConfigured() {
		wsScheme = "wss"
	}

	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		log.Fatalf("chawrtd TLS requires both CHAWRTD_TLS_CERT_FILE and CHAWRTD_TLS_KEY_FILE")
	}

	log.Printf("chawrtd listening on %s", cfg.Addr)
	log.Printf("chawrtd websocket endpoint %s://<host>%s/ws/clawwrt", wsScheme, cfg.Addr)
	if cfg.TLSConfigured() {
		log.Printf("chawrtd TLS enabled with cert=%s key=%s", cfg.TLSCertFile, cfg.TLSKeyFile)
		if err := http.ListenAndServeTLS(cfg.Addr, cfg.TLSCertFile, cfg.TLSKeyFile, server.Handler()); err != nil {
			log.Fatalf("chawrtd tls server error: %v", err)
		}
		return
	}

	if err := http.ListenAndServe(cfg.Addr, server.Handler()); err != nil {
		log.Fatalf("chawrtd server error: %v", err)
	}
}
