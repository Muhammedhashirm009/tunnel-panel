#!/usr/bin/env bash
# =============================================================================
#  Portix — One-Line Installer
#  Usage: curl -sSL https://raw.githubusercontent.com/Muhammedhashirm009/portix/main/scripts/install.sh | sudo bash
#  Or:    bash <(curl -sSL https://raw.githubusercontent.com/Muhammedhashirm009/portix/main/scripts/install.sh)
# =============================================================================
set -euo pipefail

# ── Colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

ok()   { echo -e "${GREEN}  ✓${NC}  $*"; }
info() { echo -e "${CYAN}  ➜${NC}  $*"; }
warn() { echo -e "${YELLOW}  ⚠${NC}  $*"; }
fail() { echo -e "${RED}  ✗${NC}  $*" >&2; exit 1; }
hdr()  { echo -e "\n${BOLD}${BLUE}$*${NC}"; echo -e "${BLUE}$(printf '─%.0s' {1..60})${NC}"; }

# ── Banner ────────────────────────────────────────────────────────────────────
echo -e "${BOLD}${CYAN}"
cat << 'BANNER'
  ██████╗  ██████╗ ██████╗ ████████╗██╗██╗  ██╗
  ██╔══██╗██╔═══██╗██╔══██╗╚══██╔══╝██║╚██╗██╔╝
  ██████╔╝██║   ██║██████╔╝   ██║   ██║ ╚███╔╝
  ██╔═══╝ ██║   ██║██╔══██╗   ██║   ██║ ██╔██╗
  ██║     ╚██████╔╝██║  ██║   ██║   ██║██╔╝ ██╗
  ╚═╝      ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚═╝╚═╝  ╚═╝
  Server Control Panel + Cloudflare Tunnels
BANNER
echo -e "${NC}"

# ── Checks ────────────────────────────────────────────────────────────────────
hdr "Pre-flight Checks"

[[ $EUID -eq 0 ]] || fail "Run as root: sudo bash install.sh"

# OS check
if [[ -f /etc/os-release ]]; then
  . /etc/os-release
  if [[ "$ID" != "ubuntu" && "$ID" != "debian" && "$ID_LIKE" != *"debian"* ]]; then
    warn "Detected OS: $PRETTY_NAME — Portix is tested on Ubuntu 22.04/24.04 and Debian 12."
    read -rp "  Continue anyway? [y/N] " CONT
    [[ "$CONT" =~ ^[Yy]$ ]] || fail "Aborted."
  else
    ok "OS: $PRETTY_NAME"
  fi
fi

# Architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  GOARCH="amd64" ;;
  aarch64) GOARCH="arm64" ;;
  armv7l)  GOARCH="arm"   ;;
  *)       fail "Unsupported architecture: $ARCH" ;;
esac
ok "Architecture: $ARCH"

# ── Config ────────────────────────────────────────────────────────────────────
PORTIX_VERSION="${PORTIX_VERSION:-latest}"
GITHUB_REPO="Muhammedhashirm009/portix"
INSTALL_DIR="/usr/local/bin"
DATA_DIR="/etc/portix"
LOG_DIR="/var/log/portix"
BINARY="portix"
SERVICE="portix"
PANEL_PORT="${PORTIX_PORT:-8443}"

# ── Step 1: System update ─────────────────────────────────────────────────────
hdr "Step 1/7 — Updating Package Lists"
apt-get update -qq
ok "Package lists updated"

# ── Step 2: Core dependencies ─────────────────────────────────────────────────
hdr "Step 2/7 — Installing System Dependencies"

install_pkg() {
  local pkg="$1"
  if dpkg -l "$pkg" &>/dev/null | grep -q "^ii"; then
    ok "$pkg (already installed)"
  else
    info "Installing $pkg ..."
    apt-get install -y -qq "$pkg" > /dev/null 2>&1 && ok "$pkg installed" || warn "$pkg installation failed (non-fatal)"
  fi
}

# Base tools
for pkg in curl wget git sqlite3 unzip build-essential; do
  install_pkg "$pkg"
done

# Nginx
install_pkg nginx
systemctl enable nginx --now > /dev/null 2>&1 || true
ok "nginx enabled"

# MariaDB
if ! dpkg -l mariadb-server &>/dev/null | grep -q "^ii"; then
  info "Installing MariaDB..."
  apt-get install -y -qq mariadb-server mariadb-client > /dev/null 2>&1 && ok "MariaDB installed"
else
  ok "MariaDB (already installed)"
fi
systemctl enable mariadb --now > /dev/null 2>&1 || true

# PHP
PHP_VER="8.2"
for pkg in php${PHP_VER}-fpm php${PHP_VER}-mysql php${PHP_VER}-mbstring php${PHP_VER}-xml php${PHP_VER}-zip php${PHP_VER}-curl php${PHP_VER}-gd; do
  install_pkg "$pkg"
done
systemctl enable "php${PHP_VER}-fpm" --now > /dev/null 2>&1 || true
ok "PHP ${PHP_VER}-FPM enabled"

# phpMyAdmin (optional)
if dpkg -l phpmyadmin &>/dev/null | grep -q "^ii"; then
  ok "phpMyAdmin (already installed)"
else
  info "Installing phpMyAdmin (non-interactive)..."
  export DEBIAN_FRONTEND=noninteractive
  echo "phpmyadmin phpmyadmin/dbconfig-install boolean true" | debconf-set-selections
  echo "phpmyadmin phpmyadmin/app-password-confirm password portix" | debconf-set-selections
  echo "phpmyadmin phpmyadmin/mysql/admin-pass password" | debconf-set-selections
  echo "phpmyadmin phpmyadmin/mysql/app-pass password portix" | debconf-set-selections
  echo "phpmyadmin phpmyadmin/reconfigure-webserver multiselect none" | debconf-set-selections
  apt-get install -y -qq phpmyadmin > /dev/null 2>&1 && ok "phpMyAdmin installed" || warn "phpMyAdmin install failed — use Auto Tunnel later"
fi

# cloudflared
hdr "Step 3/7 — Installing cloudflared"
if command -v cloudflared &>/dev/null; then
  ok "cloudflared $(cloudflared --version 2>&1 | head -1 | awk '{print $3}') (already installed)"
else
  info "Downloading cloudflared..."
  CF_URL="https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${GOARCH}.deb"
  TMP_CF=$(mktemp /tmp/cloudflared-XXXX.deb)
  if curl -sSL "$CF_URL" -o "$TMP_CF"; then
    dpkg -i "$TMP_CF" > /dev/null 2>&1 && ok "cloudflared installed" || {
      warn "dpkg install failed, trying direct binary..."
      curl -sSL "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${GOARCH}" \
        -o /usr/local/bin/cloudflared
      chmod +x /usr/local/bin/cloudflared
      ok "cloudflared binary installed"
    }
    rm -f "$TMP_CF"
  else
    warn "cloudflared download failed — configure tunnels manually later"
  fi
fi

# ── Step 4: Download Portix binary ────────────────────────────────────────────
hdr "Step 4/7 — Installing Portix Binary"

# Try to download pre-built binary from GitHub releases
BINARY_URL=""
if [[ "$PORTIX_VERSION" == "latest" ]]; then
  info "Fetching latest release from GitHub..."
  RELEASE_JSON=$(curl -sSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" 2>/dev/null || echo "{}")
  BINARY_URL=$(echo "$RELEASE_JSON" | grep -o "\"browser_download_url\":[^,}]*portix-linux-${GOARCH}[^\"]*" | cut -d'"' -f4 | head -1)
fi

if [[ -n "$BINARY_URL" ]]; then
  info "Downloading Portix binary from release..."
  curl -sSL "$BINARY_URL" -o "${INSTALL_DIR}/${BINARY}"
  chmod +x "${INSTALL_DIR}/${BINARY}"
  ok "Portix binary installed from release"
else
  warn "No pre-built release found — building from source..."

  # Install Go if needed
  if ! command -v go &>/dev/null || [[ "$(go version | awk '{print $3}' | sed 's/go//' | cut -d. -f1)" -lt 22 ]]; then
    info "Installing Go 1.22..."
    GO_VER="1.22.5"
    GO_URL="https://go.dev/dl/go${GO_VER}.linux-${GOARCH}.tar.gz"
    curl -sSL "$GO_URL" -o /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    export PATH="/usr/local/go/bin:$PATH"
    echo 'export PATH="/usr/local/go/bin:$PATH"' >> /etc/profile.d/go.sh
    rm /tmp/go.tar.gz
    ok "Go $(go version | awk '{print $3}') installed"
  else
    ok "Go $(go version | awk '{print $3}') (already installed)"
  fi

  # Clone and build
  BUILD_DIR=$(mktemp -d /tmp/portix-build-XXXX)
  info "Cloning Portix source from GitHub..."
  git clone --depth 1 "https://github.com/${GITHUB_REPO}.git" "$BUILD_DIR" > /dev/null 2>&1
  info "Building Portix (this takes ~30s)..."
  cd "$BUILD_DIR"
  CGO_ENABLED=1 go build -ldflags "-s -w -X main.version=${PORTIX_VERSION}" -o "${INSTALL_DIR}/${BINARY}" ./cmd/server/ 2>&1
  chmod +x "${INSTALL_DIR}/${BINARY}"
  cd /
  rm -rf "$BUILD_DIR"
  ok "Portix built and installed from source"
fi

# ── Step 5: Config directory ───────────────────────────────────────────────────
hdr "Step 5/7 — Setting Up Configuration"

mkdir -p "$DATA_DIR" "$LOG_DIR"
chmod 700 "$DATA_DIR"

# Generate JWT secret
JWT_SECRET=$(cat /dev/urandom | tr -dc 'a-f0-9' | fold -w 64 | head -1)

# Write config.json if it doesn't exist
if [[ ! -f "${DATA_DIR}/config.json" ]]; then
  cat > "${DATA_DIR}/config.json" << EOF
{
  "host": "127.0.0.1",
  "port": ${PANEL_PORT},
  "data_dir": "${DATA_DIR}",
  "db_path": "${DATA_DIR}/panel.db",
  "log_dir": "${LOG_DIR}",
  "jwt_secret": "${JWT_SECRET}",
  "port_range_min": 8080,
  "port_range_max": 9000,
  "allow_direct_access": false
}
EOF
  ok "Config written to ${DATA_DIR}/config.json"
else
  ok "Config already exists — skipping (existing installation detected)"
fi

# ── Step 6: Systemd service ────────────────────────────────────────────────────
hdr "Step 6/7 — Creating Systemd Services"

# Main portix service
cat > /etc/systemd/system/${SERVICE}.service << EOF
[Unit]
Description=Portix — Server Control Panel
After=network.target mariadb.service nginx.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY}
WorkingDirectory=${DATA_DIR}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=portix
Environment=PORTIX_CONFIG=${DATA_DIR}/config.json

[Install]
WantedBy=multi-user.target
EOF
ok "portix.service created"

# Panel tunnel service (created by Portix itself at setup, pre-stub here)
cat > /etc/systemd/system/portix-panel-tunnel.service << EOF
[Unit]
Description=Portix — Panel Cloudflare Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/cloudflared tunnel --config ${DATA_DIR}/tunnel-panel.yml run
Restart=always
RestartSec=10
StartLimitIntervalSec=0
SyslogIdentifier=portix-panel-tunnel

[Install]
WantedBy=multi-user.target
EOF
ok "portix-panel-tunnel.service created"

# Apps tunnel service
cat > /etc/systemd/system/portix-apps-tunnel.service << EOF
[Unit]
Description=Portix — Apps Cloudflare Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/cloudflared tunnel --config ${DATA_DIR}/tunnel-apps.yml run
Restart=always
RestartSec=10
StartLimitIntervalSec=0
SyslogIdentifier=portix-apps-tunnel

[Install]
WantedBy=multi-user.target
EOF
ok "portix-apps-tunnel.service created"

systemctl daemon-reload
systemctl enable portix
systemctl restart portix
sleep 2

if systemctl is-active --quiet portix; then
  ok "Portix service is running"
else
  warn "Portix service failed to start. Check: journalctl -u portix -n 30"
fi

# ── Step 7: Firewall ───────────────────────────────────────────────────────────
hdr "Step 7/7 — Firewall & Log Rotation"

# Allow port 80/443 for Cloudflare tunnel (outbound only — no inbound needed)
if command -v ufw &>/dev/null && ufw status 2>/dev/null | grep -q "active"; then
  ufw allow 80/tcp > /dev/null 2>&1 || true
  ufw allow 443/tcp > /dev/null 2>&1 || true
  ok "ufw: ports 80 and 443 allowed"
fi

# Log rotation
cat > /etc/logrotate.d/portix << 'EOF'
/var/log/portix/*.log
/var/log/nginx/portix-*.log {
    daily
    missingok
    rotate 14
    compress
    delaycompress
    notifempty
    sharedscripts
    postrotate
        systemctl reload nginx > /dev/null 2>&1 || true
    endscript
}
EOF
ok "Log rotation configured"

# ── Done ───────────────────────────────────────────────────────────────────────
LOCAL_IP=$(hostname -I | awk '{print $1}')

echo ""
echo -e "${BOLD}${GREEN}╔══════════════════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}${GREEN}║           ✅  Portix Installed Successfully!             ║${NC}"
echo -e "${BOLD}${GREEN}╚══════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${BOLD}Next steps:${NC}"
echo ""
echo -e "  1. ${CYAN}Complete setup via the web UI:${NC}"
echo -e "     The panel runs on ${BOLD}localhost:${PANEL_PORT}${NC} only."
echo -e "     To access setup, temporarily enable direct access:"
echo ""
echo -e "     ${YELLOW}sudo portix --allow-direct-access${NC}"
echo -e "     Then open: ${BOLD}http://${LOCAL_IP}:${PANEL_PORT}/setup${NC}"
echo ""
echo -e "  2. ${CYAN}Or configure Cloudflare Tunnel first:${NC}"
echo -e "     During setup you'll enter your Cloudflare API token,"
echo -e "     account ID, and zone — Portix will auto-create tunnels."
echo ""
echo -e "  3. ${CYAN}Useful commands:${NC}"
echo -e "     ${YELLOW}systemctl status portix${NC}              — service status"
echo -e "     ${YELLOW}journalctl -u portix -f${NC}              — live logs"
echo -e "     ${YELLOW}systemctl restart portix${NC}             — restart panel"
echo -e "     ${YELLOW}cat ${DATA_DIR}/config.json${NC}    — view config"
echo ""
echo -e "  ${BOLD}Dependencies installed:${NC} nginx, MariaDB, PHP ${PHP_VER}-FPM, phpMyAdmin, cloudflared"
echo -e "  ${BOLD}Config directory:${NC} ${DATA_DIR}"
echo -e "  ${BOLD}Binary:${NC} ${INSTALL_DIR}/${BINARY}"
echo ""
