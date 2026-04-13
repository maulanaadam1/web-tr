#!/bin/bash
# ============================================================
# DEPLOY / UPDATE APLIKASI WEB-TR KE VPS
# ============================================================
# Jalankan setiap kali ingin deploy atau update
# Usage: bash deploy-vps.sh
# ============================================================

set -e

echo "============================================"
echo "  🚀 Deploy web-tr ke VPS"
echo "============================================"
echo ""

# 1. Pull latest code (jika pakai git)
if [ -d .git ]; then
    echo "📥 [1/6] Pulling latest code..."
    git pull
else
    echo "📥 [1/6] Skipping git pull (not a git repo)"
fi

# 2. Stop old container
echo "🛑 [2/6] Stopping old container..."
docker stop web-tr 2>/dev/null || echo "   Container not running"
docker rm web-tr 2>/dev/null || echo "   Container not found"

# 3. Remove old image to force fresh build
echo "🗑️  [3/6] Removing old image..."
docker rmi web-tr 2>/dev/null || echo "   Image not found"

# 4. Build new image WITHOUT cache
echo "🔨 [4/6] Building Docker image (no cache)..."
docker build --no-cache --build-arg CACHEBUST=$(date +%s) -t web-tr .

# 5. Create persistent data directories
echo "📂 [5/6] Creating data directories..."
mkdir -p ./docker-data/snapshots
mkdir -p ./docker-data/timelapse
# Ensure streams.db file exists for volume mount
touch ./docker-data/streams.db

# 6. Run new container
echo "▶️  [6/6] Starting container..."
docker run -d \
  --name web-tr \
  --restart unless-stopped \
  -p 8080:8080 \
  -p 1984:1984 \
  -p 8554:8554 \
  -e DATABASE_URL="file:streams.db" \
  -e TZ=Asia/Jakarta \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS=admin123 \
  -v $(pwd)/docker-data/streams.db:/app/streams.db \
  -v $(pwd)/docker-data/snapshots:/app/data/snapshots \
  -v $(pwd)/docker-data/timelapse:/app/data/timelapse \
  web-tr

# Show status
echo ""
echo "📋 Container logs (tunggu 3 detik)..."
sleep 3
docker logs web-tr --tail 30

echo ""
echo "============================================"
echo "  ✅ Deploy berhasil!"
echo "============================================"
echo ""
echo "  🌐 Dashboard  : http://$(hostname -I | awk '{print $1}'):8080"
echo "  📡 Go2RTC API : http://$(hostname -I | awk '{print $1}'):1984"
echo "  📹 RTSP       : rtsp://$(hostname -I | awk '{print $1}'):8554/{nama_stream}"
echo ""
echo "  📦 Data persisten di: ./docker-data/"
echo "     - streams.db     (database kamera)"
echo "     - snapshots/     (foto snapshot)"
echo "     - timelapse/     (rekaman timelapse)"
echo ""
echo "  🔧 Perintah berguna:"
echo "     docker logs web-tr -f        # Lihat log realtime"
echo "     docker restart web-tr        # Restart container"
echo "     docker stop web-tr           # Stop container"
echo "     bash deploy-vps.sh           # Update & rebuild"
echo ""
