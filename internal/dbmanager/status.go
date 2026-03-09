package dbmanager

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ServiceStatus holds the overall DB service status
type ServiceStatus struct {
	MySQLRunning      bool   `json:"mysql_running"`
	MySQLVersion      string `json:"mysql_version"`
	MySQLPort         int    `json:"mysql_port"`
	MySQLPortOpen     bool   `json:"mysql_port_open"`
	PhpMyAdminInstalled bool `json:"phpmyadmin_installed"`
	PhpMyAdminPort    int    `json:"phpmyadmin_port"`
	PhpMyAdminTunneled bool  `json:"phpmyadmin_tunneled"`
	PhpMyAdminDomain  string `json:"phpmyadmin_domain"`
}

// GetServiceStatus returns comprehensive status of MySQL/MariaDB and phpMyAdmin
func (m *Manager) GetServiceStatus() *ServiceStatus {
	s := &ServiceStatus{MySQLPort: 3306}

	// Check if MariaDB/MySQL service is running
	for _, svc := range []string{"mariadb", "mysql", "mysqld"} {
		out, _ := exec.Command("systemctl", "is-active", svc).Output()
		if strings.TrimSpace(string(out)) == "active" {
			s.MySQLRunning = true
			break
		}
	}

	// Get MySQL version
	if s.MySQLRunning {
		out, err := exec.Command("mysql", "--version").Output()
		if err == nil {
			parts := strings.Fields(strings.TrimSpace(string(out)))
			for i, p := range parts {
				if p == "Ver" || p == "Distrib" || strings.HasPrefix(p, "10.") || strings.HasPrefix(p, "8.") {
					if i < len(parts) {
						s.MySQLVersion = parts[i]
						break
					}
				}
			}
			if s.MySQLVersion == "" && len(parts) > 0 {
				s.MySQLVersion = string(out)
				if len(s.MySQLVersion) > 40 {
					s.MySQLVersion = s.MySQLVersion[:40]
				}
				s.MySQLVersion = strings.TrimSpace(s.MySQLVersion)
			}
		}
	}

	// Check if port 3306 is actually listening
	conn, err := net.DialTimeout("tcp", "127.0.0.1:3306", 2*time.Second)
	if err == nil {
		conn.Close()
		s.MySQLPortOpen = true
	}

	// Check phpMyAdmin installation
	pmaPath := FindPhpMyAdminPath()
	s.PhpMyAdminInstalled = pmaPath != ""

	// Check if phpMyAdmin is already served on a port
	if s.PhpMyAdminInstalled {
		s.PhpMyAdminPort = FindPhpMyAdminPort()
	}

	return s
}

// FindPhpMyAdminPath finds the phpMyAdmin installation directory
func FindPhpMyAdminPath() string {
	candidates := []string{
		"/usr/share/phpmyadmin",
		"/var/www/html/phpmyadmin",
		"/var/www/phpmyadmin",
		"/opt/phpmyadmin",
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	// Check if phpmyadmin package is installed
	out, _ := exec.Command("dpkg", "-l", "phpmyadmin").Output()
	if strings.Contains(string(out), "ii") {
		return "/usr/share/phpmyadmin"
	}
	return ""
}

// FindPhpMyAdminPort finds the port where Portix's managed phpMyAdmin nginx config is serving.
// Only checks our own managed config to avoid false-positives from other nginx sites.
func FindPhpMyAdminPort() int {
	// Check our managed config in sites-enabled/ (same location as Sites)
	configPath := "/etc/nginx/sites-enabled/portix-phpmyadmin.conf"
	content, err := os.ReadFile(configPath)
	if err != nil {
		// Also try the old sites-available path for backward compat
		configPath = "/etc/nginx/sites-available/portix-phpmyadmin"
		content, err = os.ReadFile(configPath)
		if err != nil {
			return 0 // not created yet
		}
	}

	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "listen ") {
			parts := strings.Fields(trimmed)
			for _, p := range parts {
				p = strings.TrimRight(p, ";")
				var port int
				fmt.Sscanf(p, "%d", &port)
				if port > 1024 && port < 65535 {
					return port
				}
			}
		}
	}
	return 0
}
