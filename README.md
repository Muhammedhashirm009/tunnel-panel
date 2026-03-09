# <img src="https://raw.githubusercontent.com/Muhammedhashirm009/portix/main/web/static/img/logo.png" alt="Portix" width="32" height="32" style="vertical-align:middle"> Portix

**Portix** is a self-hosted server control panel with native Cloudflare Tunnel integration. Deploy PHP websites, manage Docker containers, host databases, and expose everything to the internet — all from a single dashboard.

---

## ✨ Features

- 🌐 **PHP Sites** — Create and manage nginx-backed PHP sites with one click
- 🐳 **Docker** — Deploy containers from Git repos (Koyeb-style), manage images & logs
- 🗄️ **Databases** — MySQL/MariaDB management + phpMyAdmin auto-tunnel
- 🔒 **Cloudflare Tunnels** — Auto-creates and manages panel + apps tunnels
- 📁 **File Manager** — Browse and edit files on the server
- 🖥️ **Web Terminal** — Real PTY terminal in the browser
- 📊 **Dashboard** — CPU, RAM, disk, network live stats

---

## 🚀 Quick Install

One command installs Portix and all its dependencies on Ubuntu/Debian:

```bash
curl -sSL https://raw.githubusercontent.com/Muhammedhashirm009/portix/main/scripts/install.sh | sudo bash
```

**Installs automatically:**
- `nginx` — web server for hosted sites
- `MariaDB` — database server
- `PHP 8.2-FPM` — PHP runtime + extensions
- `phpMyAdmin` — database web UI
- `cloudflared` — Cloudflare Tunnel daemon
- `portix` binary + systemd service

**After install, complete setup via the web UI:**
```bash
# Temporarily allow direct IP access:
sudo sed -i 's/"allow_direct_access": false/"allow_direct_access": true/' /etc/portix/config.json
sudo systemctl restart portix

# Then open in browser:
http://<YOUR_SERVER_IP>:8443/setup
```

---

## 🔧 Manual Installation

### Prerequisites
- Ubuntu 22.04+ / Debian 12+ (amd64 or arm64)
- Root access

### Steps

```bash
# 1. Clone the repo
git clone https://github.com/Muhammedhashirm009/portix.git
cd portix

# 2. Install dependencies
sudo apt install -y nginx mariadb-server php8.2-fpm php8.2-mysql \
  php8.2-mbstring php8.2-xml php8.2-zip sqlite3 cloudflared

# 3. Build
go build -ldflags "-s -w" -o /usr/local/bin/portix ./cmd/server/

# 4. Create config directory
sudo mkdir -p /etc/portix
sudo cp -n config.example.json /etc/portix/config.json
# Edit /etc/portix/config.json with your settings

# 5. Install systemd service
sudo cp scripts/portix.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now portix
```

---

## ⚙️ Configuration

Config file: `/etc/portix/config.json`

| Field | Default | Description |
|---|---|---|
| `host` | `127.0.0.1` | Listen address (use `0.0.0.0` for direct access) |
| `port` | `8443` | Panel port |
| `data_dir` | `/etc/portix` | Config & credentials directory |
| `jwt_secret` | auto-generated | Session signing key |
| `port_range_min` | `8080` | Start of port pool for sites |
| `port_range_max` | `9000` | End of port pool |
| `allow_direct_access` | `false` | Allow access without tunnel (emergency use) |

---

## 🛠️ CLI Usage

```bash
portix                      # Interactive menu
portix status               # Service status
portix info                 # Panel access info + tunnel URLs
portix restart              # Restart panel
portix password reset       # Reset admin password
portix tunnel status        # Tunnel health check
portix tunnel restart       # Restart both tunnels
```

---

## 📁 Directory Layout

```
/etc/portix/
├── config.json              # Main config
├── panel.db                 # SQLite database
├── tunnel-panel.yml         # Panel tunnel cloudflared config
├── tunnel-apps.yml          # Apps tunnel cloudflared config
├── tunnel-panel-creds.json  # Panel tunnel credentials
└── tunnel-apps-creds.json   # Apps tunnel credentials

/usr/local/bin/portix        # Main binary
/var/log/portix/             # Log files
/var/lib/portix/apps/        # Docker app clones
```

---

## 🔑 First-Time Setup

1. **Navigate to** `https://<your-panel-domain>/setup`
2. **Enter Cloudflare credentials:**
   - API Token (with `Zone:DNS` + `Cloudflare Tunnel` permissions)
   - Account ID
   - Zone ID + Zone Name
3. **Choose your panel domain** (e.g. `panel.yourdomain.com`)
4. **Create admin account**
5. **Click Setup** — Portix auto-creates Cloudflare Tunnels and configures DNS

---

## 🏥 Troubleshooting

```bash
# Check panel logs
journalctl -u portix -f

# Check tunnel logs
journalctl -u portix-panel-tunnel -f
journalctl -u portix-apps-tunnel -f

# Repair tunnels (if Error 1033)
# Go to: Dashboard → Tunnels → 🔧 Repair Tunnels

# Reset admin password
portix password reset
```

---

## 📦 Tech Stack

| Layer | Technology |
|---|---|
| Backend | Go 1.22, gin-gonic, SQLite |
| Tunneling | Cloudflare Tunnel (cloudflared) |
| Web Server | nginx |
| Database | MariaDB + SQLite |
| PHP | PHP-FPM 8.2 |
| Terminal | xterm.js + PTY |
| Frontend | Vanilla HTML/CSS/JS |

---

## 📄 License

MIT — see [LICENSE](LICENSE)
