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

	log.Printf("chawrtd listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, server.Handler()); err != nil {
		log.Fatalf("chawrtd server error: %v", err)
	}
}
