package dbmanager

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Muhammedhashirm009/portix/internal/portmanager"
)

// PhpMyAdminSetupResult is returned from EnsurePhpMyAdminServed
type PhpMyAdminSetupResult struct {
	Port    int
	PMAPath string
	Created bool // true if we created a new nginx config
}

// EnsurePhpMyAdminServed ensures phpMyAdmin is accessible on a local port via nginx.
// Uses the exact same flow as Sites:
//   1. portmanager.Allocate()
//   2. Write nginx config directly to sites-enabled/
//   3. nginx -t + systemctl reload nginx
//   4. Return port for the caller to wire up the tunnel ingress rule
func EnsurePhpMyAdminServed() (*PhpMyAdminSetupResult, error) {
	pmaPath := FindPhpMyAdminPath()
	if pmaPath == "" {
		return nil, fmt.Errorf("phpMyAdmin is not installed. Run: sudo apt install phpmyadmin")
	}

	// Check if our managed config already exists and nginx is serving it
	existingPort := FindPhpMyAdminPort()
	if existingPort > 0 {
		return &PhpMyAdminSetupResult{Port: existingPort, PMAPath: pmaPath, Created: false}, nil
	}

	// Allocate a port via the shared port manager (same as Sites)
	pm := portmanager.Get()
	port, err := pm.Allocate("custom", 0, "phpmyadmin")
	if err != nil {
		return nil, fmt.Errorf("port manager could not allocate a port for phpMyAdmin: %w", err)
	}

	// Find PHP-FPM socket (same detection as GenerateNginxVhost in sites package)
	phpSocket := findPhpFpmSocket()
	if phpSocket == "" {
		pm.Release(port)
		return nil, fmt.Errorf("PHP-FPM not found. Install it: sudo apt install php-fpm")
	}

	// Write nginx config DIRECTLY to sites-enabled/ (same as Sites flow, no symlink needed)
	confPath := fmt.Sprintf("/etc/nginx/sites-enabled/tunnelpanel-phpmyadmin.conf")
	nginxConfig := generatePmaVhost(port, pmaPath, phpSocket)
	if err := os.WriteFile(confPath, []byte(nginxConfig), 0644); err != nil {
		pm.Release(port)
		return nil, fmt.Errorf("failed to write nginx config (need root): %w", err)
	}

	// Test + reload nginx (same as sites.ReloadNginx())
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		os.Remove(confPath)
		pm.Release(port)
		return nil, fmt.Errorf("nginx config error: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		pm.Release(port)
		return nil, fmt.Errorf("nginx reload failed: %s", strings.TrimSpace(string(out)))
	}

	// Wait briefly for nginx to start listening
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	return &PhpMyAdminSetupResult{Port: port, PMAPath: pmaPath, Created: true}, nil
}

// generatePmaVhost generates an nginx server block for phpMyAdmin (mirrors GenerateNginxVhost from sites).
func generatePmaVhost(port int, pmaPath, phpSocket string) string {
	return fmt.Sprintf(`# phpMyAdmin — managed by Portix
# Do not edit manually, changes will be overwritten

server {
    listen %d;
    server_name _;

    root %s;
    index index.php;

    access_log /var/log/nginx/phpmyadmin-access.log;
    error_log  /var/log/nginx/phpmyadmin-error.log;

    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;

    client_max_body_size 100M;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        fastcgi_pass unix:%s;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_read_timeout 300;
    }

    location ~* /config\.inc\.php { deny all; }
    location ~ /\. { deny all; }
}
`, port, pmaPath, phpSocket)
}

// findPhpFpmSocket looks for a PHP-FPM unix socket (same detection as used in sites.GenerateNginxVhost)
func findPhpFpmSocket() string {
	candidates := []string{
		"/var/run/php/php8.3-fpm.sock",
		"/var/run/php/php8.2-fpm.sock",
		"/var/run/php/php8.1-fpm.sock",
		"/var/run/php/php8.0-fpm.sock",
		"/var/run/php/php7.4-fpm.sock",
		"/run/php/php8.2-fpm.sock",
		"/run/php/php8.1-fpm.sock",
	}
	for _, s := range candidates {
		if info, err := os.Stat(s); err == nil && info.Mode()&os.ModeSocket != 0 {
			return s
		}
	}
	// Glob fallback
	out, _ := exec.Command("sh", "-c", "ls /var/run/php/php*-fpm.sock 2>/dev/null | head -1").Output()
	if sock := strings.TrimSpace(string(out)); sock != "" {
		return sock
	}
	return ""
}
