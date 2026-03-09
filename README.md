# 🚀 TunnelPanel branch V2

A powerful server control panel for Ubuntu Server with built-in **Cloudflare Tunnel** integration. Manage websites, Docker containers, databases, and files — all auto-tunneled to your custom domains without opening any ports.

## Features

- **Dashboard** — Real-time CPU, RAM, disk, network monitoring
- **Tunnel Manager** — Two-tunnel architecture with Cloudflare integration
- **Website Management** — PHP sites with Nginx (Phase 2)
- **Docker Management** — Container CRUD with auto-tunnel (Phase 3)
- **File Manager** — Browse, edit, upload/download files (Phase 2)
- **Database Management** — MySQL/MariaDB (Phase 4)
- **Web Terminal** — Browser-based SSH terminal (Phase 4)
- **CLI Tool** — `tunnelpanel-cli` for SSH-based management

## Quick Install

```bash
# Clone the repo
git clone https://github.com/tunnel-panel/tunnelpanel.git
cd tunnelpanel

# Run installer (as root)
sudo bash scripts/install.sh
```

## Build from Source

```bash
# Prerequisites: Go 1.22+, GCC (for SQLite)
make build

# Install binaries
sudo make install
```

## CLI Usage

```bash
tunnelpanel-cli              # Interactive menu
tunnelpanel-cli status       # Service status
tunnelpanel-cli info         # Panel access info
tunnelpanel-cli restart      # Restart panel
tunnelpanel-cli password reset  # Reset admin password
tunnelpanel-cli tunnel status   # Tunnel status
```

## Architecture

```
Internet → Cloudflare → Tunnel #1 → Panel UI (:8443)
Internet → Cloudflare → Tunnel #2 → App1 (:8080), App2 (:8081), ...
```

- **Tunnel #1**: Tunnels the panel UI (runs as separate systemd service)
- **Tunnel #2**: Routes all hosted apps to their domains (managed by panel)

## Tech Stack

- **Backend**: Go 1.22 + Gin
- **Frontend**: HTML + CSS + JavaScript (embedded in binary)
- **Database**: SQLite
- **Auth**: bcrypt + JWT
- **Tunnels**: cloudflared + Cloudflare API

## License

MIT
