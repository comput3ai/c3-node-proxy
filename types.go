package main

import "net/http"

// NodeManager defines the interface for managing nodes and workloads
type NodeManager interface {
	GetLeastBusyNode(apiKey string, tag string) (string, error)
	TrackRequest(apiKey, node string, delta int)
}

// ProxyManager defines the interface for proxy operations
type ProxyManager interface {
	NodeManager
	HandleProxyRequest(w http.ResponseWriter, r *http.Request, node string, apiKey string)
}

// Ensure ProxyServer implements all required interfaces
var (
	_ NodeManager  = (*ProxyServer)(nil)
	_ ProxyManager = (*ProxyServer)(nil)
)