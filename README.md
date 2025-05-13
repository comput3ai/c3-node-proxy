# c3-node-proxy

A lightweight proxy server that routes API requests to Comput3 nodes based on API keys. It supports tag-based routing, load balancing, and automatic workload discovery.

## Features
- Routes requests based on Comput3 API keys
- Tag-based routing with load balancing
- Auto-discovers node assignments via Comput3 workloads API
- Smart caching with 60-second refresh and inactive cleanup
- Load balancing across nodes with the same tag
- Tracks in-flight requests for better load distribution
- Handles streaming responses
- Works with any HTTP/HTTPS API on the nodes
- Small Docker image based on Alpine Linux
- Detailed logging with configurable levels

## Quick Start
```bash
# Build and run with Docker
docker build -t c3-node-proxy .
docker run -p 8080:8080 \
  -e API_URL=https://api.comput3.com \
  -e LOG_LEVEL=INFO \
  c3-node-proxy

# Or build and run locally
go build -o c3-node-proxy
export API_URL=https://api.comput3.com
export LOG_LEVEL=INFO  # DEBUG, INFO, WARN, or ERROR
./c3-node-proxy
```

## API Usage
All requests require a Comput3 API key in the `X-C3-API-KEY` header or Bearer token.

### Get Workloads
```bash
curl http://localhost:8080/workloads \
  -H "X-C3-API-KEY: your_key"
```

### Route by Tag
```bash
# Route to least busy node with tag1
curl http://localhost:8080/tags/tag1/api/completion \
  -H "X-C3-API-KEY: your_key" \
  -d '{"your": "data"}'

# Route to any available node (load balanced)
curl http://localhost:8080/tags/all/api/completion \
  -H "X-C3-API-KEY: your_key" \
  -d '{"your": "data"}'
```

### Route by Index
```bash
# Route to first available node
curl http://localhost:8080/0/api/completion \
  -H "X-C3-API-KEY: your_key" \
  -d '{"your": "data"}'
```

## How It Works
1. Client makes request with their API key
2. For tag-based routing (/tags/tag1):
   - Proxy finds all nodes with matching tag
   - Selects node with fewest in-flight requests
   - Routes request to selected node
3. For index-based routing (/0, /1, etc.):
   - Proxy finds nth running workload
   - Routes request to that specific node
4. Proxy streams response back to client
5. Background processes:
   - Cache refreshes every 60 seconds
   - Tracks in-flight requests for load balancing
   - Stops refreshing for inactive API keys

## Error Codes
- 401: Missing API key
- 404: No active workload found or invalid index
- 500: Internal server error
- 502: Upstream server error

## Development
Required: Go 1.21 or later
```bash
# Get the code
git clone https://github.com/yourusername/c3-node-proxy
cd c3-node-proxy

# Run locally
export API_URL=https://api.comput3.com
export LOG_LEVEL=DEBUG  # For detailed logging
go run main.go

# Run tests
go test ./...
```

## Logging
The proxy supports multiple log levels:
- DEBUG: Detailed request/response info, load balancing decisions
- INFO: Node changes, workload status changes
- WARN: Important but non-critical issues
- ERROR: Critical errors

Set the log level via the LOG_LEVEL environment variable.

## Docker Image
The Docker image:
- Uses multi-stage builds for small size
- Runs as non-root user
- Includes CA certificates for HTTPS
- Based on Alpine Linux

## License
MIT