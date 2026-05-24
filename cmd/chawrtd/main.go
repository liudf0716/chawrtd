package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"chawrtd/internal/config"
	"chawrtd/internal/httpapi"
	"chawrtd/internal/version"
)

func printUsage() {
	fmt.Fprintf(os.Stdout, "Usage: chawrtd [options]\n\n")
	fmt.Fprintf(os.Stdout, "Options:\n")
	fmt.Fprintf(os.Stdout, "  -h, --help       Show help\n")
	fmt.Fprintf(os.Stdout, "  -v, --version    Show version\n\n")
	fmt.Fprintf(os.Stdout, "Environment variables:\n")
	fmt.Fprintf(os.Stdout, "  CHAWRTD_ADDR (default :8001)\n")
	fmt.Fprintf(os.Stdout, "  CHAWRTD_DEFAULT_TIMEOUT_SECONDS (default 120)\n")
	fmt.Fprintf(os.Stdout, "  CHAWRTD_CONFIG_FILE (optional, defaults: ./chawrtd.toml or /etc/chawrtd/chawrtd.toml)\n")
	fmt.Fprintf(os.Stdout, "  CHAWRTD_TOKEN (default clawwrt)\n")
	fmt.Fprintf(os.Stdout, "  CHAWRTD_ALIAS_FILE (default device-aliases.json)\n")
	fmt.Fprintf(os.Stdout, "  CHAWRTD_TLS_CERT_FILE (optional, requires CHAWRTD_TLS_KEY_FILE)\n")
	fmt.Fprintf(os.Stdout, "  CHAWRTD_TLS_KEY_FILE (optional, requires CHAWRTD_TLS_CERT_FILE)\n")
}

func detectConfigPath() string {
	configPath := os.Getenv("CHAWRTD_CONFIG_FILE")
	if configPath != "" {
		return configPath
	}

	for _, path := range []string{"./chawrtd.toml", "/etc/chawrtd/chawrtd.toml"} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

func main() {
	showVersion := flag.Bool("version", false, "show version")
	showVersionShort := flag.Bool("v", false, "show version")
	showHelp := flag.Bool("help", false, "show help")
	showHelpShort := flag.Bool("h", false, "show help")

	flag.Usage = printUsage
	flag.Parse()

	if *showHelp || *showHelpShort {
		printUsage()
		return
	}

	if *showVersion || *showVersionShort {
		version.PrintVersion()
		return
	}

	configPath := detectConfigPath()
	cfg := config.Load()
	server := httpapi.New(cfg.DefaultTimeout, cfg.Token)
	if err := server.InitializeAliasStore(cfg.AliasFile); err != nil {
		log.Fatalf("failed to initialize alias store (%s): %v", cfg.AliasFile, err)
	}
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
	log.Printf("chawrtd version %s", version.Version)
	log.Printf("chawrtd listening on %s", cfg.Addr)
	log.Printf("chawrtd websocket endpoint %s://<host>%s/ws/clawwrt", wsScheme, cfg.Addr)
	log.Printf("chawrtd token=***")
	log.Printf("chawrtd alias file=%s", cfg.AliasFile)
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
