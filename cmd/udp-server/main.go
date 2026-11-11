package main

import (
	"log"
	"os"

	"mangahub/internal/udp"
)

func main() {
	addr := getEnv("UDP_ADDR", ":7071")
	server := udp.NewNotificationServer(addr, log.Default())
	log.Printf("Starting UDP notification server on %s", addr)
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
