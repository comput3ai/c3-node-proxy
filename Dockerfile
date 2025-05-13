# Build stage
FROM golang:1.21-alpine AS builder

# Add git for private repos if needed
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files first for better cache
COPY go.mod ./
RUN go mod download

# Copy source
COPY . .

# Build the binary with security flags
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o proxy -ldflags="-w -s" .

# Final stage
FROM alpine:latest

# Add CA certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN adduser -D -u 10001 appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/proxy .

# Set default API URL
ENV API_URL="https://api.comput3.ai/api/v0"
ENV DEBUG_LEVEL="INFO"

# Use non-root user
USER appuser

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./proxy"]
