package main

import (
	"log"
	"os"

	"mangahub/internal/tcp"
)

func main() {
	addr := getEnv("TCP_ADDR", ":9090")
	server := tcp.NewProgressSyncServer(addr, log.Default())
	log.Printf("Starting TCP progress sync server on %s", addr)
	if err := server.Start(); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
