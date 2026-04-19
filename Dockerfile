# ============================================================
# Build Stage
# ============================================================
FROM golang:alpine AS builder

WORKDIR /app

# Copy dependency files first (cached unless go.mod/go.sum changes)
COPY go.mod go.sum ./
RUN go mod download



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

# Create data directory and place default config there
RUN mkdir -p /app/data/snapshots /app/data/timelapse
COPY --from=builder /app/go2rtc.yaml /app/data/go2rtc.yaml

# Expose ports
# 8080: Web Dashboard
# 8555: WebRTC UDP/TCP
EXPOSE 8080 8555 8555/udp

# Default environment variables
ENV DATABASE_URL=file:data/web-tr.db
ENV TZ=Asia/Jakarta

# Run the application
CMD ["./web-tr"]
