FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /build

COPY go.mod go.sum ./

# Copy proto directory for local replace
COPY proto ./proto

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o hyperlane-monitor ./cmd/monitor

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

RUN addgroup -g 1000 -S monitor && \
    adduser -u 1000 -S monitor -G monitor

WORKDIR /app

COPY --from=builder /build/hyperlane-monitor .
COPY --from=builder /build/config/config.json ./config/

RUN chown -R monitor:monitor /app

USER monitor

EXPOSE 9091

ENTRYPOINT ["./hyperlane-monitor"]
CMD ["-config", "/app/config/config.json"]