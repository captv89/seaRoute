# syntax=docker/dockerfile:1

# --- Build Stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies for CGO (SQLite)
RUN apk add --no-cache gcc musl-dev

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" go build -ldflags='-s -w' -o /searoute

# --- Runtime Stage ---
FROM alpine:3.19

# Install runtime dependencies for SQLite
RUN apk add --no-cache ca-certificates libc6-compat wget

# Create non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /searoute /app/searoute

# Copy required data and web assets
COPY --from=builder /app/dataset /app/dataset
COPY --from=builder /app/web /app/web

# Create temp directory with proper permissions
RUN mkdir -p /app/temp && chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 -O - http://localhost:${PORT:-8080}/ > /dev/null || exit 1

CMD ["/app/searoute"]