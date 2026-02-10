# Build Stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
# We build specifically for Linux/AMD64 inside the container
RUN CGO_ENABLED=0 GOOS=linux go build -o web-tr ./cmd/server/main.go

# Runtime Stage
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
# ffmpeg: For transcoding and snapshots
# ca-certificates: For HTTPS requests
# curl: For healthchecks or downloading files
# tzdata: For correct timezone handling
RUN apk add --no-cache ffmpeg ca-certificates curl tzdata

# Download Go2RTC binary (v1.9.8)
# Ensure this matches the target architecture (amd64 is standard for most servers)
ADD https://github.com/AlexxIT/go2rtc/releases/download/v1.9.8/go2rtc_linux_amd64 /usr/local/bin/go2rtc
RUN chmod +x /usr/local/bin/go2rtc

# Copy the built binary
COPY --from=builder /app/web-tr .

# Copy necessary assets
# The structure inside the container will mimic the project structure
COPY --from=builder /app/web/templates ./web/templates
COPY --from=builder /app/web/static ./web/static

# Copy default config
# Note: In production, you might mount this as a volume or use env vars
COPY --from=builder /app/go2rtc.yaml .

# Expose necessary ports
# 8080: Web Dashboard
# 1984: Go2RTC API & Streaming
# 8554: RTSP Server (if acting as server)
# 8888: HLS/MSE (if used directly)
EXPOSE 8080 1984 8554

# Run the application
CMD ["./web-tr"]
