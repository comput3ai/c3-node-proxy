package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	log.Printf("🏁 Starting c3-node-proxy application...")

	server, err := NewProxyServer()
	if err != nil {
		log.Fatalf("❌ Failed to initialize server: %v", err)
	}

	log.Printf("✅ Server initialized successfully")

	// Handle graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		server.logger.Info("👋 Shutting down...")
		server.Cleanup()
		os.Exit(0)
	}()

	log.Printf("🚀 About to call server.Start()...")
	server.Start()
}
