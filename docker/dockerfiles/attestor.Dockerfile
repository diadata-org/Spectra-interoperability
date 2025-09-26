# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /workspace

# Copy root go.mod and shared packages
COPY ../../go.mod ../../go.sum ./
COPY ../../pkg ./pkg

# Set working directory to service
WORKDIR /workspace/services/attestor

# Copy service files
COPY . .

# Download dependencies
RUN go mod download

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
COPY --from=builder /workspace/services/attestor/attestor .

# Copy config file (optional, can be mounted)
COPY --from=builder /workspace/services/attestor/config.yaml.example ./config.yaml

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