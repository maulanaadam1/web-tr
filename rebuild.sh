#!/bin/bash

# Force rebuild without cache
echo "🔨 Rebuilding Docker image WITHOUT cache..."
docker build --no-cache -t web-tr .

echo "✅ Build complete. Now restart the container on your VPS with:"
echo "   docker stop web-tr"
echo "   docker rm web-tr"
echo "   docker pull your-registry/web-tr"
echo "   docker run -d --name web-tr -p 8080:8080 -p 1984:1984 web-tr"
