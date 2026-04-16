# ============================================================
# Build Stage
# ============================================================
FROM golang:alpine AS builder

WORKDIR /app

# Copy dependency files first (cached unless go.mod/go.sum changes)
COPY go.mod go.sum ./
RUN go mod download

# ---- AUTO CACHE BUSTER ----
# Docker's ADD instruction with an HTTP URL is ALWAYS checked on every build.
# When a new commit is pushed to GitHub, this file changes → all subsequent
# layers (COPY, go build) are automatically invalidated.
# No manual changes needed — this is fully automatic.
ADD https://api.github.com/repos/maulanaadam1/web-tr/commits/main?force=1 /tmp/gitversion
# ---- END AUTO CACHE BUSTER ----

# Copy source code and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o web-tr ./cmd/server/main.go

# ============================================================
# Runtime Stage
# ============================================================
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ffmpeg ca-certificates curl tzdata

# Download Go2RTC binary (v1.9.8 for amd64)
ADD https://github.com/AlexxIT/go2rtc/releases/download/v1.9.8/go2rtc_linux_amd64 /usr/local/bin/go2rtc
RUN chmod +x /usr/local/bin/go2rtc

# Copy the compiled binary and assets
COPY --from=builder /app/web-tr .
COPY --from=builder /app/web/templates ./web/templates
COPY --from=builder /app/web/static ./web/static
COPY --from=builder /app/go2rtc.yaml .

# Expose ports
# 8080: Web Dashboard
# 8555: WebRTC UDP/TCP
EXPOSE 8080 8555

# Create data directories for snapshots and timelapse
RUN mkdir -p /app/data/snapshots /app/data/timelapse

# Default environment variables
ENV DATABASE_URL=file:data/streams.db
ENV TZ=Asia/Jakarta

# Run the application
CMD ["./web-tr"]
