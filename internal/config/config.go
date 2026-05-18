package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Addr           string
	DefaultTimeout time.Duration
	TLSCertFile    string
	TLSKeyFile     string
	Token          string
	AliasFile      string
}

// tomlConfig represents the structure of chawrtd.toml
type tomlConfig struct {
	Addr      string `toml:"addr"`
	Timeout   int    `toml:"timeout_seconds"`
	AliasFile string `toml:"alias_file"`
	TLS       struct {
		CertFile string `toml:"cert_file"`
		KeyFile  string `toml:"key_file"`
	} `toml:"tls"`
	Token string `toml:"token"`
}

func Load() Config {
	// Start with defaults
	cfg := Config{
		Addr:           ":8001",
		DefaultTimeout: 120 * time.Second,
		Token:          "clawwrt",
		AliasFile:      "device-aliases.json",
	}

	// Try to load from TOML config file
	if fileCfg, err := loadFromFile(); err == nil {
		cfg = fileCfg
	}

	// Override with environment variables (highest priority)
	if addr := os.Getenv("CHAWRTD_ADDR"); addr != "" {
		cfg.Addr = addr
	}
	if timeout := os.Getenv("CHAWRTD_DEFAULT_TIMEOUT_SECONDS"); timeout != "" {
		if parsed, err := strconv.Atoi(timeout); err == nil && parsed > 0 {
			cfg.DefaultTimeout = time.Duration(parsed) * time.Second
		}
	}
	if cert := strings.TrimSpace(os.Getenv("CHAWRTD_TLS_CERT_FILE")); cert != "" {
		cfg.TLSCertFile = cert
	}
	if key := strings.TrimSpace(os.Getenv("CHAWRTD_TLS_KEY_FILE")); key != "" {
		cfg.TLSKeyFile = key
	}
	if token := strings.TrimSpace(os.Getenv("CHAWRTD_TOKEN")); token != "" {
		cfg.Token = token
	}
	if aliasFile := strings.TrimSpace(os.Getenv("CHAWRTD_ALIAS_FILE")); aliasFile != "" {
		cfg.AliasFile = aliasFile
	}

	return cfg
}

func loadFromFile() (Config, error) {
	// Determine config file path
	configPath := os.Getenv("CHAWRTD_CONFIG_FILE")
	if configPath == "" {
		// Try standard locations
		for _, path := range []string{
			"./chawrtd.toml",
			"/etc/chawrtd/chawrtd.toml",
		} {
			if _, err := os.Stat(path); err == nil {
				configPath = path
				break
			}
		}
	}

	// If no config file found, return zero value (will use defaults)
	if configPath == "" {
		return Config{}, os.ErrNotExist
	}

	// Read and parse TOML
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, err
	}

	var tomlCfg tomlConfig
	if err := toml.Unmarshal(data, &tomlCfg); err != nil {
		return Config{}, err
	}

	// Build Config from parsed TOML
	cfg := Config{
		Addr:        ":8001", // default
		Token:       "clawwrt",
		AliasFile:   "device-aliases.json",
		TLSCertFile: tomlCfg.TLS.CertFile,
		TLSKeyFile:  tomlCfg.TLS.KeyFile,
	}

	if tomlCfg.Addr != "" {
		cfg.Addr = tomlCfg.Addr
	}
	if tomlCfg.Timeout > 0 {
		cfg.DefaultTimeout = time.Duration(tomlCfg.Timeout) * time.Second
	} else {
		cfg.DefaultTimeout = 120 * time.Second
	}
	if tomlCfg.Token != "" {
		cfg.Token = tomlCfg.Token
	}
	if tomlCfg.AliasFile != "" {
		cfg.AliasFile = tomlCfg.AliasFile
	}

	return cfg, nil
}

func (c Config) TLSConfigured() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}
