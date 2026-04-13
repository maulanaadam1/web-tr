#!/bin/bash
# ============================================================
# SETUP SERVER LINUX BARU (Fresh VPS) - Jalankan SEKALI saja
# ============================================================
# Tested on: Ubuntu 20.04 / 22.04 / 24.04, Debian 11/12
# Usage: curl -sSL <url> | bash   ATAU   bash setup-server.sh
# ============================================================

set -e

echo "============================================"
echo "  🖥️  Setup Server Linux Baru untuk web-tr"
echo "============================================"
echo ""

# 1. Update system
echo "📦 [1/5] Updating system packages..."
sudo apt-get update -y
sudo apt-get upgrade -y

# 2. Install dependencies
echo "📦 [2/5] Installing Git, Curl, and other tools..."
sudo apt-get install -y git curl wget ufw

# 3. Install Docker
echo "🐳 [3/5] Installing Docker..."
if command -v docker &> /dev/null; then
    echo "   Docker already installed: $(docker --version)"
else
    curl -fsSL https://get.docker.com | sh
    sudo usermod -aG docker $USER
    echo "   Docker installed: $(docker --version)"
    echo "   ⚠️  You may need to logout and login again for docker group to take effect."
fi

# 4. Enable and start Docker
echo "🐳 [4/5] Starting Docker service..."
sudo systemctl enable docker
sudo systemctl start docker

# 5. Configure Firewall (UFW)
echo "🔒 [5/5] Configuring firewall..."
sudo ufw allow 22/tcp    # SSH
sudo ufw allow 8080/tcp  # Web Dashboard
sudo ufw allow 1984/tcp  # Go2RTC API
sudo ufw allow 8554/tcp  # RTSP Server
sudo ufw --force enable

echo ""
echo "============================================"
echo "  ✅ Server setup complete!"
echo "============================================"
echo ""
echo "Langkah selanjutnya:"
echo ""
echo "  1. Clone repository:"
echo "     git clone <YOUR_REPO_URL> web-tr"
echo "     cd web-tr"
echo ""
echo "  2. Deploy aplikasi:"
echo "     bash deploy-vps.sh"
echo ""
echo "  3. Buka di browser:"
echo "     http://<IP_SERVER>:8080"
echo ""
echo "⚠️  Jika docker permission denied, logout lalu login lagi:"
echo "     exit"
echo "     ssh user@server"
echo ""
