package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

type ProxyServer struct {
	nodeCache        map[string]string
	workloadCache    map[string]*WorkloadCache
	inFlightRequests map[string]map[string]int
	tagMappings      map[string]map[string][]string
	cacheLock        sync.RWMutex
	requestLock      sync.RWMutex
	apiURL           string
	logger           *Logger
}

func NewProxyServer() (*ProxyServer, error) {
	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		return nil, fmt.Errorf("API_URL environment variable is required")
	}

	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		log.Printf("📊 No LOG_LEVEL set, defaulting to INFO")
	}
	setLogLevel(level)

	logger := NewLogger("proxy")
	logger.Info("🚀 Starting proxy server with API URL: %s", apiURL)

	return &ProxyServer{
		nodeCache:        make(map[string]string),
		workloadCache:    make(map[string]*WorkloadCache),
		inFlightRequests: make(map[string]map[string]int),
		tagMappings:      make(map[string]map[string][]string),
		apiURL:           apiURL,
		logger:           logger,
	}, nil
}

func (p *ProxyServer) Start() {
	http.HandleFunc("/", p.ProxyHandler)
	p.logger.Info("🚀 Starting proxy server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		p.logger.Error("Failed to start server: %v", err)
		os.Exit(1)
	}
}

func (p *ProxyServer) Cleanup() {
	p.logger.Info("🧹 Starting cleanup...")
	p.cacheLock.Lock()
	defer p.cacheLock.Unlock()

	for apiKey, cache := range p.workloadCache {
		p.logger.Debug("🚫 Stopping refresh for API key %s...", apiKey[:8])
		if cache.StopRefresh != nil {
			close(cache.StopRefresh)
		}
	}
}

// DumpInFlightRequests logs the current state of in-flight requests
func (p *ProxyServer) DumpInFlightRequests() {
	if logLevel != DEBUG {
		return // Only dump in debug mode
	}

	p.requestLock.RLock()
	defer p.requestLock.RUnlock()

	for apiKey, nodes := range p.inFlightRequests {
		// Skip empty maps
		hasRequests := false
		for _, count := range nodes {
			if count > 0 {
				hasRequests = true
				break
			}
		}

		if hasRequests {
			shortKey := apiKey
			if len(shortKey) > 8 {
				shortKey = shortKey[:8] + "..."
			}

			p.logger.Debug("📊 In-flight requests for API key %s:", shortKey)
			for node, count := range nodes {
				p.logger.Debug("  - Node %s: %d requests", node, count)
			}
		}
	}
}

func copyHeader(dst, src http.Header) {
	stripOrigin := os.Getenv("STRIP_ORIGIN") == "true"

	for k, vv := range src {
		// Skip Origin header if STRIP_ORIGIN is true
		if stripOrigin && k == "Origin" {
			continue
		}

		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
