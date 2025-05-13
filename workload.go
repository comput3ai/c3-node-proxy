package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Workload struct {
	Created  int64    `json:"created"`
	Expires  int64    `json:"expires"`
	Node     string   `json:"node"`
	Running  bool     `json:"running"`
	Status   string   `json:"status"`
	Type     string   `json:"type"`
	Workload string   `json:"workload"`
	Tags     []string `json:"tags,omitempty"`
}

type WorkloadCache struct {
	Workloads   []Workload
	LastFetch   time.Time
	LastAccess  time.Time
	StopRefresh chan struct{}
}

func (p *ProxyServer) updateLastAccess(apiKey string) {
	p.cacheLock.Lock()
	defer p.cacheLock.Unlock()

	if cache, exists := p.workloadCache[apiKey]; exists {
		cache.LastAccess = time.Now()
	}
}

func (p *ProxyServer) fetchWorkloads(apiKey string) ([]Workload, error) {
	body := map[string]bool{
		"running": true,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/workloads", p.apiURL),
		bytes.NewBuffer(jsonBody),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-C3-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("workloads API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var workloads []Workload
	if err := json.NewDecoder(resp.Body).Decode(&workloads); err != nil {
		return nil, err
	}

	return workloads, nil
}

// forceRefreshWorkloads forces an immediate refresh of workloads for an API key
// regardless of cache state
func (p *ProxyServer) forceRefreshWorkloads(apiKey string) ([]Workload, error) {
	p.logger.Debug("🔄 Forcing workload refresh for API key %s...", apiKey[:8])

	workloads, err := p.fetchWorkloads(apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workloads: %v", err)
	}

	p.updateCache(apiKey, workloads)

	// Ensure refresh cycle is running
	p.cacheLock.RLock()
	cache, exists := p.workloadCache[apiKey]
	p.cacheLock.RUnlock()

	if !exists || cache == nil {
		p.startCacheRefresh(apiKey)
	}

	return workloads, nil
}

func (p *ProxyServer) updateCache(apiKey string, workloads []Workload) {
	p.cacheLock.Lock()
	defer p.cacheLock.Unlock()

	cache := p.workloadCache[apiKey]
	if cache == nil {
		return
	}

	oldNodes := make(map[string]bool)
	if cache.Workloads != nil {
		for _, w := range cache.Workloads {
			if w.Running && w.Status == "running" {
				oldNodes[w.Node] = true
			}
		}
	}

	newNodes := make(map[string]bool)
	runningCount := 0

	tagMap := make(map[string][]string)
	for _, w := range workloads {
		if w.Running && w.Status == "running" {
			newNodes[w.Node] = true
			runningCount++

			tags := w.Tags
			if tags == nil {
				p.logger.Debug("⚠️  No tags found for node %s", w.Node)
				tags = []string{}
			}

			if len(tags) > 0 {
				p.logger.Debug("🏷️  Node %s has tags: %v", w.Node, tags)
			}

			for _, tag := range tags {
				tagMap[tag] = append(tagMap[tag], w.Node)
			}
		}
	}

	// Log node changes
	for node := range newNodes {
		if !oldNodes[node] {
			p.logger.Info("🆕 New node added: %s for API key %s...", node, apiKey[:8])
		}
	}

	for node := range oldNodes {
		if !newNodes[node] {
			p.logger.Info("🔌 Node removed: %s for API key %s...", node, apiKey[:8])
		}
	}

	// Log workload status changes
	if cache.Workloads == nil {
		if runningCount > 0 {
			p.logger.Info("✨ Initial workloads for API key %s...: %d running", apiKey[:8], runningCount)
		} else {
			p.logger.Info("💤 No running workloads for API key %s...", apiKey[:8])
		}
	} else if runningCount == 0 {
		p.logger.Info("🛑 All workloads stopped for API key %s...", apiKey[:8])
	}

	cache.Workloads = workloads
	cache.LastFetch = time.Now()
	p.tagMappings[apiKey] = tagMap
}

func (p *ProxyServer) startCacheRefresh(apiKey string) {
	p.cacheLock.Lock()
	existingCache, exists := p.workloadCache[apiKey]

	// If refresh is already running, just update the last access time
	if exists && existingCache != nil && existingCache.StopRefresh != nil {
		existingCache.LastAccess = time.Now()
		p.cacheLock.Unlock()
		p.logger.Debug("💫 Cache refresh already running for API key: %s...", apiKey[:8])
		return
	}

	// Create new cache if it doesn't exist or reinitialize if it was partially created
	var newCache *WorkloadCache
	if exists && existingCache != nil {
		// Reuse existing cache but ensure StopRefresh channel is created
		if existingCache.StopRefresh == nil {
			existingCache.StopRefresh = make(chan struct{})
		}
		existingCache.LastAccess = time.Now()
		newCache = existingCache
	} else {
		// Create completely new cache
		newCache = &WorkloadCache{
			StopRefresh: make(chan struct{}),
			LastAccess:  time.Now(),
		}
		p.workloadCache[apiKey] = newCache
	}
	p.cacheLock.Unlock()

	p.logger.Info("🔄 Starting cache refresh cycle for API key: %s...", apiKey[:8])

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			// Check for inactivity before fetching workloads
			p.cacheLock.RLock()
			cache, cacheExists := p.workloadCache[apiKey]
			if !cacheExists || cache == nil {
				// Cache was deleted while we were running
				p.cacheLock.RUnlock()
				p.logger.Warn("⚠️  Cache for API key %s... was deleted, stopping refresh cycle", apiKey[:8])
				return
			}

			lastAccess := cache.LastAccess
			p.cacheLock.RUnlock()

			// Check if there are any in-flight requests for this API key
			p.requestLock.RLock()
			apiKeyRequests, exists := p.inFlightRequests[apiKey]
			hasActiveRequests := false
			if exists {
				// Check if any node has active requests
				for _, count := range apiKeyRequests {
					if count > 0 {
						hasActiveRequests = true
						break
					}
				}
			}
			p.requestLock.RUnlock()

			inactivityDuration := time.Since(lastAccess)
			if inactivityDuration > 180*time.Second && !hasActiveRequests {
				// 3 minutes of inactivity with no requests - stop refreshing
				p.logger.Info("⏳ API key %s... inactive for %v with no active requests, stopping refresh cycle",
					apiKey[:8], inactivityDuration.Round(time.Second))

				p.cacheLock.Lock()
				// Only delete if this is our refresh cycle (avoid race conditions)
				if currentCache, exists := p.workloadCache[apiKey]; exists && currentCache == cache {
					delete(p.workloadCache, apiKey)
					delete(p.tagMappings, apiKey)
				}
				p.cacheLock.Unlock()

				return
			} else if inactivityDuration > 60*time.Second && !hasActiveRequests {
				// Log that we're still waiting but not deleting yet
				p.logger.Debug("⏳ API key %s... inactive for %v but continuing refresh cycle",
					apiKey[:8], inactivityDuration.Round(time.Second))
			}

			// Even if we're going to stop refreshing soon, fetch the latest workloads
			workloads, err := p.fetchWorkloads(apiKey)
			if err != nil {
				p.logger.Error("Failed fetching workloads for %s: %v", apiKey[:8], err)
			} else {
				p.updateCache(apiKey, workloads)

				// Don't stop refresh cycle when no workloads are found
				// This fixes the premature stopping issue
				if len(workloads) == 0 {
					p.logger.Warn("💤 No workloads found for %s, but continuing refresh cycle", apiKey[:8])
				} else {
					p.logger.Debug("✨ Refreshed %d workloads for API key %s...", len(workloads), apiKey[:8])
				}
			}

			select {
			case <-ticker.C:
				p.logger.Debug("⏰ Cache refresh tick for %s...", apiKey[:8])
			case <-newCache.StopRefresh:
				p.logger.Info("🛑 Stopping cache refresh for %s...", apiKey[:8])
				return
			}
		}
	}()
}

func (p *ProxyServer) getWorkloads(apiKey string) ([]Workload, error) {
	p.cacheLock.RLock()
	cache, exists := p.workloadCache[apiKey]
	p.cacheLock.RUnlock()

	// Update last access time whenever getWorkloads is called
	p.updateLastAccess(apiKey)

	if !exists {
		// No cache exists, start refresh cycle and fetch workloads
		p.startCacheRefresh(apiKey)

		workloads, err := p.fetchWorkloads(apiKey)
		if err != nil {
			return nil, err
		}
		p.updateCache(apiKey, workloads)
		return workloads, nil
	}

	if cache.Workloads == nil || len(cache.Workloads) == 0 {
		// Cache exists but no workloads, fetch them
		workloads, err := p.fetchWorkloads(apiKey)
		if err != nil {
			return nil, err
		}

		// Always update the cache with latest workloads
		p.updateCache(apiKey, workloads)

		// Even if workloads are empty, ensure refresh cycle is running
		// as long as the API key is active
		p.startCacheRefresh(apiKey)

		return workloads, nil
	}

	// Check if we have any running nodes in our cache
	p.cacheLock.RLock()
	hasRunningNodes := false
	for _, w := range cache.Workloads {
		if w.Running && w.Status == "running" {
			hasRunningNodes = true
			break
		}
	}
	staleCache := time.Since(cache.LastFetch) > 60*time.Second
	p.cacheLock.RUnlock()

	// If no running nodes or cache is stale, force a refresh
	if !hasRunningNodes || staleCache {
		var cacheStatus string
		if !hasRunningNodes {
			cacheStatus = "no running nodes"
		} else {
			cacheStatus = "stale data"
		}
		p.logger.Debug("🔄 Cache has %s - forcing refresh for API key %s...", cacheStatus, apiKey[:8])

		workloads, err := p.fetchWorkloads(apiKey)
		if err != nil {
			// On error, return the cached workloads as fallback
			p.logger.Warn("⚠️  Failed to refresh workloads: %v, using cached data", err)
			p.cacheLock.RLock()
			defer p.cacheLock.RUnlock()
			return cache.Workloads, nil
		}

		p.updateCache(apiKey, workloads)
		return workloads, nil
	}

	p.cacheLock.RLock()
	defer p.cacheLock.RUnlock()
	return cache.Workloads, nil
}
