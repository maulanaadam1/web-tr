# Force rebuild and deploy script
# This ensures Docker doesn't use cached old JavaScript files

echo "🧹 Cleaning up old containers and images..."
docker stop web-tr 2>/dev/null || true
docker rm web-tr 2>/dev/null || true

echo "🔨 Building with CACHEBUST to force fresh assets..."
docker build --build-arg CACHEBUST=$(date +%s) -t web-tr .

echo "✅ Build complete!"
echo ""
echo "📤 Now push to deploy:"
echo "   git add -A"
echo "   git commit -m 'fix: Force cache invalidation'"
echo "   git push"
echo ""
echo "🚀 On your VPS, run:"
echo "   cd /path/to/repo && git pull"
echo "   docker build --build-arg CACHEBUST=\$(date +%s) -t web-tr ."
echo "   docker stop web-tr && docker rm web-tr"
echo "   docker run -d --name web-tr --restart unless-stopped -p 8080:8080 web-tr"
