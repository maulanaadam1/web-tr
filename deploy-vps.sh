#!/bin/bash
# ============================================================
# DEPLOY / UPDATE APLIKASI WEB-TR KE VPS MELALUI DOCKER COMPOSE
# ============================================================
# Jalankan setiap kali ingin deploy atau update
# ============================================================

set -e

echo "============================================"
echo "  🚀 Deploy web-tr + Nginx ke VPS (via Traefik)"
echo "============================================"
echo ""

# 1. Pull latest code (jika pakai git)
if [ -d .git ]; then
    echo "📥 [1/5] Pulling latest code..."
    git pull || true
else
    echo "📥 [1/5] Skipping git pull (not a git repo)"
fi

# 2. Hapus container 'web-tr' lama yg tidak pakai docker-compose
echo "🛑 [2/5] Cleaning old independent container..."
docker stop web-tr 2>/dev/null || true
docker rm web-tr 2>/dev/null || true

# 3. Create persistent data directories
echo "📂 [3/5] Creating data directories..."
mkdir -p ./docker-data/snapshots
mkdir -p ./docker-data/timelapse
touch ./docker-data/streams.db

# 4. Build and Restart via Docker Compose
echo "🔨 [4/5] Building & Starting Docker Compose..."
docker compose build --no-cache
docker compose up -d

# 5. Show status
echo ""
echo "📋 [5/5] Docker compose logs..."
sleep 3
docker compose ps

echo ""
echo "============================================"
echo "  ✅ Deploy Berhasil!"
echo "============================================"
echo "  🌐 Domain Publik: https://stream.campod.my.id"
echo "  📹 RTSP Akses   : rtsp://$(hostname -I | awk '{print $1}'):8554/{nama_stream}"
echo ""
