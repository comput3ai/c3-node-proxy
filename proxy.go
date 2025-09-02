package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HandleProxyRequest handles proxying a single request to a node
func (p *ProxyServer) HandleProxyRequest(w http.ResponseWriter, r *http.Request, node string, apiKey string) {
	targetURL := fmt.Sprintf("https://%s%s", node, r.URL.Path)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		p.logger.Debug("❌ Error creating proxy request: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	copyHeader(proxyReq.Header, r.Header)
	proxyReq.Header.Set("Host", node)

	p.logger.Debug("📈 Incrementing in-flight count for node %s", node)
	p.TrackRequest(apiKey, node, 1)
	if logLevel == DEBUG {
		p.DumpInFlightRequests() // Debug in-flight requests
	}

	// Use defer to ensure we decrement even if there's a panic
	defer func() {
		p.logger.Debug("📉 Decrementing in-flight count for node %s", node)
		p.TrackRequest(apiKey, node, -1)
		if logLevel == DEBUG {
			p.DumpInFlightRequests() // Debug in-flight requests
		}
	}()

	p.logger.Debug("📡 Proxying request to %s: %s %s", node, r.Method, targetURL)

	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		p.logger.Debug("❌ Proxy request failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		p.logger.Debug("⚠️  Proxy returned non-200 status: %d for %s %s", resp.StatusCode, r.Method, targetURL)
	}

	if f, ok := w.(http.Flusher); ok {
		done := make(chan bool)
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					if _, writeErr := w.Write(buf[:n]); writeErr != nil {
						p.logger.Debug("❌ Error writing response: %v", writeErr)
						break
					}
					f.Flush()
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					p.logger.Debug("❌ Error reading from upstream: %v", err)
					break
				}
			}
			close(done)
		}()
		<-done
	} else {
		if _, err := io.Copy(w, resp.Body); err != nil {
			p.logger.Debug("❌ Error copying response: %v", err)
		}
	}
}

// ProxyHandler handles all incoming HTTP requests
func (p *ProxyServer) ProxyHandler(w http.ResponseWriter, r *http.Request) {
	p.logger.Debug("🌐 Incoming request: %s %s", r.Method, r.URL.Path)

	if r.URL.Path == "/" && r.Method == "GET" {
		p.logger.Debug("💚 Health check request - returning healthy status")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "healthy",
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	apiKey := r.Header.Get("X-C3-API-KEY")
	if apiKey == "" {
		auth := r.Header.Get("Authorization")
		if auth != "" && len(auth) > 7 && auth[:7] == "Bearer " {
			apiKey = auth[7:]
		}
	}

	if apiKey == "" {
		http.Error(w, "Missing API key (use X-C3-API-KEY header or Authorization Bearer)", http.StatusUnauthorized)
		return
	}

	// Update last access time for this API key
	p.updateLastAccess(apiKey)

	p.logger.Debug("📝 Request from API key %s...: %s %s", apiKey[:8], r.Method, r.URL.Path)

	if r.URL.Path == "/workloads" {
		workloads, err := p.getWorkloads(apiKey)
		if err != nil {
			p.logger.Debug("❌ Error fetching workloads: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(workloads)
		return
	}

	if _, err := p.getWorkloads(apiKey); err != nil {
		p.logger.Debug("❌ Error refreshing workloads: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	if len(pathParts) < 1 {
		http.Error(w, "Invalid path. Use /tags/{tag} or /{index} to access workloads", http.StatusBadRequest)
		return
	}

	var node string
	var err error

	if pathParts[0] == "tags" {
		if len(pathParts) < 2 {
			http.Error(w, "Missing tag. Use /tags/{tag}", http.StatusBadRequest)
			return
		}
		tag := pathParts[1]
		node, err = p.GetLeastBusyNode(apiKey, tag)
		if err != nil {
			p.logger.Debug("❌ No nodes found for tag %s: %v", tag, err)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if len(pathParts) > 2 {
			r.URL.Path = "/" + pathParts[2]
		} else {
			r.URL.Path = "/"
		}
	} else {
		index, err := strconv.Atoi(pathParts[0])
		if err != nil {
			http.Error(w, "Invalid workload index. Must be a number", http.StatusBadRequest)
			return
		}

		// Force refresh workloads to ensure we have the latest data
		workloads, err := p.forceRefreshWorkloads(apiKey)
		if err != nil {
			p.logger.Error("❌ Error refreshing workloads: %v", err)
			http.Error(w, fmt.Sprintf("Failed to refresh workloads: %v", err), http.StatusInternalServerError)
			return
		}

		// Create a filtered list of only running workloads
		runningWorkloads := make([]Workload, 0)
		for _, w := range workloads {
			if w.Running && w.Status == "running" {
				runningWorkloads = append(runningWorkloads, w)
			}
		}

		// Check if we have any running workloads
		if len(runningWorkloads) == 0 {
			p.logger.Error("⚠️ No running workloads found for API key %s...", apiKey[:8])
			http.Error(w, "No running workloads found", http.StatusNotFound)
			return
		}

		// Check if the index is valid
		if index < 0 || index >= len(runningWorkloads) {
			p.logger.Error("⚠️ Invalid workload index %d (valid range: 0-%d)",
				index, len(runningWorkloads)-1)
			http.Error(w, fmt.Sprintf("Workload index %d out of range (0-%d)",
				index, len(runningWorkloads)-1), http.StatusNotFound)
			return
		}

		node = runningWorkloads[index].Node
		p.logger.Debug("🔢 Selected node %s by index %d", node, index)

		if len(pathParts) > 1 {
			r.URL.Path = "/" + strings.Join(pathParts[1:], "/")
		} else {
			r.URL.Path = "/"
		}
	}

	done := make(chan bool)
	go func() {
		p.HandleProxyRequest(w, r, node, apiKey)
		close(done)
	}()
	<-done
}
