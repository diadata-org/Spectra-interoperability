# Build stage
FROM golang:1.23-alpine AS builder

# Install dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /

# Copy proto and pkg directories first
COPY proto ./proto
COPY pkg ./pkg
COPY go.mod go.sum ./

# Now set working directory to bridge
WORKDIR /bridge

# Copy bridge go mod files
COPY bridge/go.mod bridge/go.sum ./

# Install protoc and Go plugins
RUN apk add --no-cache protobuf protobuf-dev && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Regenerate proto files
RUN cd /proto && protoc --go_out=. --go-grpc_out=. bridge.proto

# Download dependencies
RUN go mod download

# Copy bridge source code
COPY bridge ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bridge ./cmd/bridge

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /bridge/bridge .

# Expose API port and gRPC port
EXPOSE 8080 8082

# Run the bridge
CMD ["./bridge"]