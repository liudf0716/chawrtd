package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string
	DefaultTimeout time.Duration
	TLSCertFile    string
	TLSKeyFile     string
}

func Load() Config {
	addr := os.Getenv("CHAWRTD_ADDR")
	if addr == "" {
		addr = ":8001"
	}

	timeout := 120 * time.Second
	tlsCertFile := strings.TrimSpace(os.Getenv("CHAWRTD_TLS_CERT_FILE"))
	tlsKeyFile := strings.TrimSpace(os.Getenv("CHAWRTD_TLS_KEY_FILE"))
	if raw := os.Getenv("CHAWRTD_DEFAULT_TIMEOUT_SECONDS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			timeout = time.Duration(parsed) * time.Second
		}
	}

	return Config{
		Addr:           addr,
		DefaultTimeout: timeout,
		TLSCertFile:    tlsCertFile,
		TLSKeyFile:     tlsKeyFile,
	}
}

func (c Config) TLSConfigured() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}
