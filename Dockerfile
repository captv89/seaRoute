# syntax=docker/dockerfile:1

# --- Build Stage ---
# Build on the host platform and cross-compile: the binary is pure Go (no CGO).
FROM --platform=$BUILDPLATFORM golang:1.26.9-alpine3.24 AS builder
ARG TARGETOS TARGETARCH

WORKDIR /app

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o /searoute

# --- Runtime Stage ---
FROM alpine:3.24

# Runtime dependencies: CA certificates, wget for the health check
RUN apk add --no-cache ca-certificates wget

# Create non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /searoute /app/searoute

# Copy required data and web assets
COPY dataset /app/dataset
COPY web /app/web

# Create temp and weather directories with proper permissions (a named volume
# mounted on /app/weather takes this owner)
RUN mkdir -p /app/temp /app/weather && chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 -O - http://localhost:${PORT:-8080}/ > /dev/null || exit 1

CMD ["/app/searoute"]