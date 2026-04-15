# ============================================================
# Build Stage
# ============================================================
FROM golang:alpine AS builder

WORKDIR /app

# Copy dependency files first (cached unless go.mod/go.sum changes)
COPY go.mod go.sum ./
RUN go mod download

# ---- CACHE BUSTER ----
# This ARG must be passed from Easypanel Build Arguments every time you redeploy.
# Set it to any new value (e.g. a timestamp) in Easypanel → Service → Build → Build Arguments:
#   Key: BUILD_DATE
#   Value: (e.g. 2026-04-15_09-00)
# Without this, Docker reuses the cached image and changes won't appear.
ARG BUILD_DATE=unknown
RUN echo "Build triggered at: $BUILD_DATE"
# ---- END CACHE BUSTER ----

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

# Copy the compiled binary
COPY --from=builder /app/web-tr .

# Copy frontend assets
COPY --from=builder /app/web/templates ./web/templates
COPY --from=builder /app/web/static ./web/static

# Copy go2rtc config
COPY --from=builder /app/go2rtc.yaml .

# Expose ports
# 8080: Web Dashboard
# 8555: WebRTC UDP/TCP
EXPOSE 8080 8555

# Create data directories
RUN mkdir -p /app/data/snapshots /app/data/timelapse

# Default environment variables
ENV DATABASE_URL=file:streams.db
ENV TZ=Asia/Jakarta

# Include the build date as a label for traceability
ARG BUILD_DATE=unknown
LABEL build-date=$BUILD_DATE

# Run the application
CMD ["./web-tr"]
