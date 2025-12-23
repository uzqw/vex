# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vex ./cmd/server

# Final stage
FROM alpine:3.19

# Add non-root user for security
RUN adduser -D -g '' vex

WORKDIR /app

# Copy binary from builder
COPY --from=builder /vex .

# Use non-root user
USER vex

# Default port
EXPOSE 6379

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD nc -z localhost 6379 || exit 1

ENTRYPOINT ["./vex"]
