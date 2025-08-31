FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /

# Copy shared dependencies first  
COPY proto ./proto
COPY pkg ./pkg
COPY go.mod go.sum ./

# Now set working directory to hyperlane-monitor
WORKDIR /hyperlane-monitor

COPY hyperlane-monitor/go.mod hyperlane-monitor/go.sum ./

RUN go mod download

COPY hyperlane-monitor .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o hyperlane-monitor ./cmd/monitor

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

RUN addgroup -g 1000 -S monitor && \
    adduser -u 1000 -S monitor -G monitor

WORKDIR /app

COPY --from=builder /hyperlane-monitor/hyperlane-monitor .
COPY --from=builder /hyperlane-monitor/config/config.json ./config/

RUN chown -R monitor:monitor /app

USER monitor

EXPOSE 9091

ENTRYPOINT ["./hyperlane-monitor"]
CMD ["-config", "/app/config/config.json"]