package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr           string
	DefaultTimeout time.Duration
}

func Load() Config {
	addr := os.Getenv("CHAWRTD_ADDR")
	if addr == "" {
		addr = ":8090"
	}

	timeout := 120 * time.Second
	if raw := os.Getenv("CHAWRTD_DEFAULT_TIMEOUT_SECONDS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			timeout = time.Duration(parsed) * time.Second
		}
	}

	return Config{
		Addr:           addr,
		DefaultTimeout: timeout,
	}
}
