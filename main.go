package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	server, err := NewProxyServer()
	if err != nil {
		log.Fatalf("❌ Failed to initialize server: %v", err)
	}

	// Handle graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		server.logger.Info("👋 Shutting down...")
		server.Cleanup()
		os.Exit(0)
	}()

	server.Start()
}