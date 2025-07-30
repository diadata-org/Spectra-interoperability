# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN go build -o attestor main.go

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 -S attestor && \
    adduser -u 1000 -S attestor -G attestor

# Set working directory
WORKDIR /app

# Copy binary from build stage
COPY --from=builder /app/attestor /app/attestor

# Change ownership
RUN chown -R attestor:attestor /app

# Switch to non-root user
USER attestor

# Expose metrics port
EXPOSE 8080

# Set entrypoint
ENTRYPOINT ["/app/attestor"]