#!/bin/bash
# Quick rebuild script for VPS
# Usage: bash rebuild.sh

set -e

echo "🔨 Rebuilding Docker image WITHOUT cache..."
docker build --no-cache --build-arg CACHEBUST=$(date +%s) -t web-tr .

echo ""
echo "✅ Build complete!"
echo ""
echo "To restart the container, run:"
echo ""
echo "  docker stop web-tr && docker rm web-tr"
echo "  docker run -d \\"
echo "    --name web-tr \\"
echo "    --restart unless-stopped \\"
echo "    -p 8080:8080 -p 1984:1984 -p 8554:8554 \\"
echo "    -e DATABASE_URL='file:streams.db' \\"
echo "    -e TZ=Asia/Jakarta \\"
echo "    -v \$(pwd)/docker-data/streams.db:/app/streams.db \\"
echo "    -v \$(pwd)/docker-data/snapshots:/app/data/snapshots \\"
echo "    -v \$(pwd)/docker-data/timelapse:/app/data/timelapse \\"
echo "    web-tr"
echo ""
echo "Or simply run: bash deploy-vps.sh"
