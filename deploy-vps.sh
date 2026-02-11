#!/bin/bash
# VPS Deployment Script - Forces full rebuild without cache

echo "🚀 Starting VPS Deployment..."

# 1. Pull latest code
echo "📥 Pulling latest code..."
git pull

# 2. Stop old container
echo "🛑 Stopping old container..."
docker stop web-tr 2>/dev/null || echo "Container not running"
docker rm web-tr 2>/dev/null || echo "Container not found"

# 3. Remove old images to force fresh build
echo "🗑️  Removing old images..."
docker rmi web-tr 2>/dev/null || echo "Image not found"

# 4. Build WITHOUT cache (CRITICAL!)
echo "🔨 Building image WITHOUT cache..."
docker build --no-cache --pull -t web-tr .

# 5. Run new container
echo "▶️  Starting new container..."
docker run -d \
  --name web-tr \
  --restart unless-stopped \
  -p 8080:8080 \
  web-tr

# 6. Show logs
echo "📋 Container logs:"
sleep 2
docker logs web-tr --tail 50

echo "✅ Deployment complete!"
echo "🌐 Visit: https://stream.campod.my.id"
