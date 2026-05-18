package main

import (
	"log"
	"net/http"
	"os"

	"chawrtd/internal/config"
	"chawrtd/internal/httpapi"
)

func main() {
	// Detect which config file was loaded
	configPath := os.Getenv("CHAWRTD_CONFIG_FILE")
	if configPath == "" {
		for _, path := range []string{"./chawrtd.toml", "/etc/chawrtd/chawrtd.toml"} {
			if _, err := os.Stat(path); err == nil {
				configPath = path
				break
			}
		}
	}

	cfg := config.Load()
	server := httpapi.New(cfg.DefaultTimeout, cfg.Token)
	wsScheme := "ws"
	if cfg.TLSConfigured() {
		wsScheme = "wss"
	}

	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		log.Fatalf("chawrtd TLS requires both CHAWRTD_TLS_CERT_FILE and CHAWRTD_TLS_KEY_FILE")
	}

	if configPath != "" {
		log.Printf("chawrtd config file: %s", configPath)
	} else {
		log.Printf("chawrtd config: using environment variables and defaults")
	}
	log.Printf("chawrtd listening on %s", cfg.Addr)
	log.Printf("chawrtd websocket endpoint %s://<host>%s/ws/clawwrt", wsScheme, cfg.Addr)
	log.Printf("chawrtd token=%q", cfg.Token)
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
