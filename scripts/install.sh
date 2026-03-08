#!/bin/bash
# ============================================
# TunnelPanel Installer for Ubuntu Server
# Supports: Ubuntu 20.04, 22.04, 24.04
# ============================================

set -e

# Resolve project root (works whether run from scripts/ or project root)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"
echo "📂 Project root: $PROJECT_ROOT"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

INSTALL_DIR="/etc/tunnelpanel"
LOG_DIR="/var/log/tunnelpanel"
BIN_DIR="/usr/local/bin"

echo -e "${CYAN}"
echo "╔══════════════════════════════════════════╗"
echo "║       🚀 TunnelPanel Installer v1.0      ║"
echo "║   Server Control Panel + Tunnels          ║"
echo "╚══════════════════════════════════════════╝"
echo -e "${NC}"

# Check root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}❌ Please run as root (sudo)${NC}"
    exit 1
fi

# Check Ubuntu
if ! grep -qi "ubuntu" /etc/os-release 2>/dev/null; then
    echo -e "${YELLOW}⚠️  This installer is designed for Ubuntu. Continue anyway? (y/n)${NC}"
    read -r confirm
    if [ "$confirm" != "y" ]; then
        exit 1
    fi
fi

echo -e "${GREEN}[1/7]${NC} Updating system packages..."
apt-get update -y > /dev/null 2>&1

echo -e "${GREEN}[2/7]${NC} Installing dependencies..."
apt-get install -y \
    curl wget git build-essential \
    nginx \
    mariadb-server \
    redis-server \
    software-properties-common \
    apt-transport-https \
    ca-certificates \
    gnupg lsb-release \
    sqlite3 \
    > /dev/null 2>&1

# Install PHP (multiple versions via PPA)
echo -e "${GREEN}[3/7]${NC} Installing PHP..."
add-apt-repository -y ppa:ondrej/php > /dev/null 2>&1 || true
apt-get update -y > /dev/null 2>&1
apt-get install -y \
    php8.2-fpm php8.2-cli php8.2-common php8.2-mysql php8.2-zip \
    php8.2-gd php8.2-mbstring php8.2-curl php8.2-xml php8.2-bcmath \
    php8.2-intl php8.2-readline php8.2-redis php8.2-sqlite3 \
    > /dev/null 2>&1

# Install Docker
echo -e "${GREEN}[4/7]${NC} Installing Docker..."
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com | sh > /dev/null 2>&1
    systemctl enable docker
    systemctl start docker
else
    echo "  Docker already installed, skipping."
fi

# Install cloudflared via official Cloudflare APT repository
echo -e "${GREEN}[5/7]${NC} Installing cloudflared..."
if ! command -v cloudflared &> /dev/null; then
    mkdir -p --mode=0755 /usr/share/keyrings
    curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | tee /usr/share/keyrings/cloudflare-main.gpg > /dev/null
    echo 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main' | tee /etc/apt/sources.list.d/cloudflared.list > /dev/null
    apt-get update -y > /dev/null 2>&1
    apt-get install -y cloudflared > /dev/null 2>&1
else
    echo "  cloudflared already installed, skipping."
fi

# Install Go (for building from source)
echo -e "${GREEN}[6/7]${NC} Installing Go..."
if ! command -v go &> /dev/null; then
    GO_VERSION="1.22.5"
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    export PATH=$PATH:/usr/local/go/bin
    rm -f /tmp/go.tar.gz
else
    echo "  Go already installed, skipping."
fi

# Build and install TunnelPanel
echo -e "${GREEN}[7/7]${NC} Building TunnelPanel..."
mkdir -p "$INSTALL_DIR"
mkdir -p "$LOG_DIR"

# If source directory is provided, build from there
if [ -d "./cmd/server" ]; then
    export PATH=$PATH:/usr/local/go/bin
    go mod tidy
    make build
    cp build/tunnelpanel "$BIN_DIR/tunnelpanel"
    cp build/tunnelpanel-cli "$BIN_DIR/tunnelpanel-cli"
    chmod +x "$BIN_DIR/tunnelpanel"
    chmod +x "$BIN_DIR/tunnelpanel-cli"
fi

# Install systemd services
echo -e "${CYAN}📦 Installing systemd services...${NC}"
cp configs/systemd/tunnelpanel.service /etc/systemd/system/
cp configs/systemd/tunnelpanel-panel-tunnel.service /etc/systemd/system/
cp configs/systemd/tunnelpanel-apps-tunnel.service /etc/systemd/system/
systemctl daemon-reload

# Enable services
systemctl enable tunnelpanel
systemctl enable nginx
systemctl enable mariadb
systemctl enable docker
systemctl enable redis-server

# Start services
systemctl start nginx
systemctl start mariadb
systemctl start redis-server

# Start TunnelPanel
systemctl start tunnelpanel

echo ""
echo -e "${GREEN}╔══════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║       ✅ Installation Complete!           ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${BOLD}Panel URL:${NC}    https://localhost:8443"
echo -e "  ${BOLD}Config:${NC}       /etc/tunnelpanel/"
echo -e "  ${BOLD}Logs:${NC}         /var/log/tunnelpanel/"
echo ""
echo -e "  ${YELLOW}⚡ Run setup wizard:${NC}"
echo -e "     Open the panel URL in your browser to create"
echo -e "     your admin account and configure Cloudflare."
echo ""
echo -e "  ${YELLOW}🔧 CLI commands:${NC}"
echo -e "     tunnelpanel-cli          # Interactive menu"
echo -e "     tunnelpanel-cli status   # Check status"
echo -e "     tunnelpanel-cli info     # Panel info"
echo ""
