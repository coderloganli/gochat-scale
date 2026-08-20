# Multi-stage Dockerfile for GoChat
# Stage 1: Builder - Build the Go binary
FROM golang:1.23 AS builder

LABEL maintainer="Lock <lockexit@gmail.com>"

WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# The PostgreSQL driver is pure Go, so the binary can be built without cgo.
RUN CGO_ENABLED=0 GOOS=linux \
    go build -tags=etcd -ldflags="-w -s" -o gochat main.go

# Stage 2: Runtime - Minimal runtime image
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    netcat-openbsd \
    procps \
    && rm -rf /var/lib/apt/lists/*

# Copy binary from builder stage
COPY --from=builder /build/gochat /app/

# Copy application files
COPY --from=builder /build/config /app/config
COPY --from=builder /build/site /app/site

# Create symlink so config.go can find config files at build path
RUN mkdir -p /build && ln -s /app/config /build/config

# Default environment
ENV RUN_MODE=dev

# Default command (can be overridden by docker-compose)
CMD ["/app/gochat", "-module", "api"]
