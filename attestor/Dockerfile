# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /

# Copy shared dependencies first
COPY pkg ./pkg
COPY go.mod go.sum ./

# Now set working directory to attestor
WORKDIR /app

# Copy attestor go mod files
COPY attestor/go.mod attestor/go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY attestor .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o attestor .

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS and wget for health checks
RUN apk --no-cache add ca-certificates wget

# Create non-root user
RUN addgroup -g 1000 -S attestor && \
    adduser -u 1000 -S attestor -G attestor

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/attestor .

# Copy config file (optional, can be mounted)
COPY --from=builder /app/config.yaml.example ./config.yaml

# Change ownership
RUN chown -R attestor:attestor /app

# Switch to non-root user
USER attestor

# Expose ports
EXPOSE 8080 8081

# Set entrypoint
ENTRYPOINT ["./attestor"]

# Default command (can be overridden)
CMD ["-config", "/app/config.yaml"]