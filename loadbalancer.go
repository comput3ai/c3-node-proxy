package main

import "fmt"

// GetLeastBusyNode returns the node with the least number of in-flight requests
func (p *ProxyServer) GetLeastBusyNode(apiKey string, tag string) (string, error) {
	// Check if we need to refresh workloads first
	p.cacheLock.RLock()
	cache, exists := p.workloadCache[apiKey]
	nodesExist := false
	if exists && cache.Workloads != nil {
		for _, workload := range cache.Workloads {
			if workload.Running && workload.Status == "running" {
				nodesExist = true
				break
			}
		}
	}
	p.cacheLock.RUnlock()

	// If no nodes exist and we haven't refreshed recently, force a refresh
	if !nodesExist {
		p.logger.Debug("🔍 No nodes found for API key %s... - forcing workload refresh", apiKey[:8])
		if _, err := p.forceRefreshWorkloads(apiKey); err != nil {
			return "", fmt.Errorf("failed to refresh workloads: %v", err)
		}
	}

	p.requestLock.RLock()
	defer p.requestLock.RUnlock()

	p.cacheLock.RLock()
	defer p.cacheLock.RUnlock()

	var nodes []string

	if tag == "all" {
		// Make sure we have a valid workload cache before proceeding
		if _, exists := p.workloadCache[apiKey]; !exists || p.workloadCache[apiKey].Workloads == nil {
			return "", fmt.Errorf("no workloads found for API key")
		}

		for _, workload := range p.workloadCache[apiKey].Workloads {
			if workload.Running && workload.Status == "running" {
				nodes = append(nodes, workload.Node)
			}
		}
		p.logger.Debug("🎯 Using all nodes for tag 'all': %v", nodes)
	} else {
		tagMap, exists := p.tagMappings[apiKey]
		if !exists {
			return "", fmt.Errorf("no nodes found for API key")
		}

		nodes, exists = tagMap[tag]
		if !exists || len(nodes) == 0 {
			return "", fmt.Errorf("no nodes found for tag: %s", tag)
		}
	}

	if len(nodes) == 0 {
		return "", fmt.Errorf("no active nodes found")
	}

	if _, exists := p.inFlightRequests[apiKey]; !exists {
		p.requestLock.RUnlock()
		p.requestLock.Lock()
		p.inFlightRequests[apiKey] = make(map[string]int)
		p.requestLock.Unlock()
		p.requestLock.RLock()
	}

	var selectedNode string
	minRequests := -1
	for _, node := range nodes {
		requests := p.inFlightRequests[apiKey][node]
		if minRequests == -1 || requests < minRequests {
			minRequests = requests
			selectedNode = node
		}
	}

	p.logger.Debug("⚖️  Load balancing: selected node %s with %d in-flight requests", selectedNode, minRequests)
	return selectedNode, nil
}

// TrackRequest updates the count of in-flight requests for a node
func (p *ProxyServer) TrackRequest(apiKey, node string, delta int) {
	p.requestLock.Lock()
	defer p.requestLock.Unlock()

	if _, exists := p.inFlightRequests[apiKey]; !exists {
		p.inFlightRequests[apiKey] = make(map[string]int)
	}

	// For safety, check the current count before decrementing
	currentCount := p.inFlightRequests[apiKey][node]
	if delta < 0 && currentCount <= 0 {
		p.logger.Warn("⚠️  Attempted to decrement in-flight requests below zero for node %s (current: %d, delta: %d)",
			node, currentCount, delta)
		p.inFlightRequests[apiKey][node] = 0
		return
	}

	// If we're decrementing, ensure we never go below zero
	if delta < 0 && currentCount < -delta {
		p.logger.Warn("⚠️  Would decrement below zero for node %s (current: %d, delta: %d), setting to 0 instead",
			node, currentCount, delta)
		p.inFlightRequests[apiKey][node] = 0
		return
	}

	p.inFlightRequests[apiKey][node] += delta

	// Double-check after the operation to ensure we never have negative counts
	if p.inFlightRequests[apiKey][node] < 0 {
		p.logger.Warn("⚠️  In-flight requests went negative for node %s (became: %d), resetting to 0",
			node, p.inFlightRequests[apiKey][node])
		p.inFlightRequests[apiKey][node] = 0
	}
}
