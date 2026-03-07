package sites

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Muhammedhashirm009/tunnel-panel/internal/database"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/portmanager"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/tunnel"
)

// Site represents a hosted PHP website
type Site struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Domain          string    `json:"domain"`
	DocumentRoot    string    `json:"document_root"`
	PHPVersion      string    `json:"php_version"`
	Port            int       `json:"port"`
	NginxConfigPath string    `json:"nginx_config_path"`
	DNSRecordID     string    `json:"dns_record_id"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

// Manager handles site lifecycle operations
type Manager struct {
	tunnelMgr *tunnel.Manager
	sitesRoot string // e.g., /var/www
	nginxConf string // e.g., /etc/nginx/sites-enabled
}

// NewManager creates a new site manager
func NewManager(tunnelMgr *tunnel.Manager) *Manager {
	return &Manager{
		tunnelMgr: tunnelMgr,
		sitesRoot: "/var/www",
		nginxConf: "/etc/nginx/sites-enabled",
	}
}

// CreateSite provisions a new PHP site: dir, nginx vhost, port, tunnel ingress
func (m *Manager) CreateSite(name, domain, phpVersion string) (*Site, error) {
	if name == "" || domain == "" {
		return nil, fmt.Errorf("name and domain are required")
	}
	if phpVersion == "" {
		phpVersion = "8.2"
	}

	log.Printf("[sites] ═══ Creating site: %s (%s) ═══", name, domain)

	// Check if domain already exists
	var count int
	database.DB().QueryRow("SELECT COUNT(*) FROM sites WHERE domain = ?", domain).Scan(&count)
	if count > 0 {
		return nil, fmt.Errorf("domain %s already exists", domain)
	}

	// Step 1: Allocate port
	log.Printf("[sites] Step 1: Allocating port...")
	pm := portmanager.Get()
	port, err := pm.Allocate("site", 0, name)
	if err != nil {
		return nil, fmt.Errorf("port allocation failed: %w", err)
	}
	log.Printf("[sites]   ✓ Port allocated: %d", port)

	// Step 2: Create document root
	log.Printf("[sites] Step 2: Creating document root...")
	docRoot := filepath.Join(m.sitesRoot, name)
	if err := os.MkdirAll(docRoot, 0755); err != nil {
		pm.Release(port)
		return nil, fmt.Errorf("cannot create doc root: %w", err)
	}
	log.Printf("[sites]   ✓ Document root: %s", docRoot)

	// Create default index.php
	indexContent := fmt.Sprintf("<!DOCTYPE html>\n<html>\n<head><title>%s</title></head>\n<body>\n<h1>Welcome to %s</h1>\n<p>Domain: %s</p>\n<p>PHP Version: <?php echo phpversion(); ?></p>\n<p>Managed by TunnelPanel</p>\n</body>\n</html>", name, name, domain)
	os.WriteFile(filepath.Join(docRoot, "index.php"), []byte(indexContent), 0644)

	// Step 3: Generate Nginx vhost
	log.Printf("[sites] Step 3: Creating Nginx config (listening on port %d)...", port)
	confPath := filepath.Join(m.nginxConf, name+".conf")
	vhostContent := GenerateNginxVhost(domain, docRoot, phpVersion, port)
	if err := os.WriteFile(confPath, []byte(vhostContent), 0644); err != nil {
		pm.Release(port)
		return nil, fmt.Errorf("cannot write nginx config: %w", err)
	}
	log.Printf("[sites]   ✓ Nginx config: %s", confPath)

	// Set ownership
	exec.Command("chown", "-R", "www-data:www-data", docRoot).Run()

	// Step 4: Insert into database
	log.Printf("[sites] Step 4: Saving to database...")
	result, err := database.DB().Exec(
		`INSERT INTO sites (name, domain, document_root, php_version, port, nginx_config_path, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'active', ?)`,
		name, domain, docRoot, phpVersion, port, confPath, time.Now(),
	)
	if err != nil {
		pm.Release(port)
		os.Remove(confPath)
		return nil, fmt.Errorf("database error: %w", err)
	}
	siteID, _ := result.LastInsertId()
	pm.UpdateAppID(port, int(siteID))
	log.Printf("[sites]   ✓ Saved (site ID: %d)", siteID)

	// Step 5: Reload Nginx
	log.Printf("[sites] Step 5: Testing and reloading Nginx...")
	if err := ReloadNginx(); err != nil {
		log.Printf("[sites]   ⚠ Nginx reload failed: %v", err)
	} else {
		log.Printf("[sites]   ✓ Nginx reloaded")
	}

	// Step 6: Add tunnel ingress rule (domain → localhost:port)
	log.Printf("[sites] Step 6: Adding tunnel ingress (%s → localhost:%d)...", domain, port)
	tunnelErr := m.addTunnelIngress(domain, port, int(siteID))
	if tunnelErr != nil {
		log.Printf("[sites]   ⚠ Tunnel ingress failed: %v", tunnelErr)
		log.Printf("[sites]   Site is accessible locally at http://localhost:%d but NOT via tunnel", port)
	} else {
		log.Printf("[sites]   ✓ Tunnel ingress configured")
		log.Printf("[sites]   Site accessible at https://%s", domain)
	}

	log.Printf("[sites] ═══ Site created: %s (port %d) ═══", name, port)

	return &Site{
		ID:              int(siteID),
		Name:            name,
		Domain:          domain,
		DocumentRoot:    docRoot,
		PHPVersion:      phpVersion,
		Port:            port,
		NginxConfigPath: confPath,
		Status:          "active",
		CreatedAt:       time.Now(),
	}, nil
}

// addTunnelIngress adds a domain → localhost:port mapping via the tunnel manager
func (m *Manager) addTunnelIngress(domain string, port int, siteID int) error {
	if m.tunnelMgr == nil {
		// Try to create tunnel manager from DB config
		mgr, err := m.loadTunnelManager()
		if err != nil {
			return fmt.Errorf("no tunnel manager available: %w", err)
		}
		m.tunnelMgr = mgr
	}

	return m.tunnelMgr.AddIngressRule(domain, port, "site", siteID)
}

// loadTunnelManager tries to create a tunnel manager from DB-stored Cloudflare config
func (m *Manager) loadTunnelManager() (*tunnel.Manager, error) {
	var apiToken, accountID, zoneID, zoneName, appsTunnelID string
	err := database.DB().QueryRow(
		"SELECT api_token, account_id, zone_id, zone_name, COALESCE(tunnel_apps_id,'') FROM cloudflare_config WHERE id = 1",
	).Scan(&apiToken, &accountID, &zoneID, &zoneName, &appsTunnelID)
	if err != nil || apiToken == "" {
		return nil, fmt.Errorf("cloudflare not configured")
	}

	cf := tunnel.NewCloudflareClient(apiToken, accountID, zoneID, zoneName)
	mgr := tunnel.NewManager(cf, nil, "/etc/tunnelpanel", appsTunnelID, "")
	return mgr, nil
}

// ListSites returns all sites from the database
func (m *Manager) ListSites() ([]Site, error) {
	rows, err := database.DB().Query(
		"SELECT id, name, domain, document_root, php_version, port, nginx_config_path, COALESCE(dns_record_id,''), status, created_at FROM sites ORDER BY id DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.Name, &s.Domain, &s.DocumentRoot, &s.PHPVersion, &s.Port, &s.NginxConfigPath, &s.DNSRecordID, &s.Status, &s.CreatedAt); err != nil {
			continue
		}
		sites = append(sites, s)
	}
	return sites, nil
}

// GetSite returns a single site by ID
func (m *Manager) GetSite(id int) (*Site, error) {
	var s Site
	err := database.DB().QueryRow(
		"SELECT id, name, domain, document_root, php_version, port, nginx_config_path, COALESCE(dns_record_id,''), status, created_at FROM sites WHERE id = ?", id,
	).Scan(&s.ID, &s.Name, &s.Domain, &s.DocumentRoot, &s.PHPVersion, &s.Port, &s.NginxConfigPath, &s.DNSRecordID, &s.Status, &s.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("site not found")
	}
	return &s, nil
}

// DeleteSite removes a site completely
func (m *Manager) DeleteSite(id int) error {
	site, err := m.GetSite(id)
	if err != nil {
		return err
	}

	// Remove tunnel ingress
	if m.tunnelMgr != nil {
		m.tunnelMgr.RemoveIngressRule(site.Domain)
	} else {
		// Try to load tunnel manager for cleanup
		mgr, err := m.loadTunnelManager()
		if err == nil {
			mgr.RemoveIngressRule(site.Domain)
		}
	}

	// Remove Nginx config
	os.Remove(site.NginxConfigPath)

	// Free port
	portmanager.Get().Release(site.Port)

	// Delete from DB
	database.DB().Exec("DELETE FROM sites WHERE id = ?", id)

	// Reload Nginx
	ReloadNginx()

	return nil
}

// UpdatePHPVersion changes the PHP version for a site
func (m *Manager) UpdatePHPVersion(id int, newVersion string) error {
	site, err := m.GetSite(id)
	if err != nil {
		return err
	}

	vhostContent := GenerateNginxVhost(site.Domain, site.DocumentRoot, newVersion, site.Port)
	if err := os.WriteFile(site.NginxConfigPath, []byte(vhostContent), 0644); err != nil {
		return err
	}

	database.DB().Exec("UPDATE sites SET php_version = ? WHERE id = ?", newVersion, id)

	ReloadNginx()
	return nil
}

// GetAvailablePHPVersions returns installed PHP versions
func GetAvailablePHPVersions() []string {
	versions := []string{}
	candidates := []string{"7.4", "8.0", "8.1", "8.2", "8.3"}
	for _, v := range candidates {
		socket := fmt.Sprintf("/var/run/php/php%s-fpm.sock", v)
		if _, err := os.Stat(socket); err == nil {
			versions = append(versions, v)
		}
		bin := fmt.Sprintf("/usr/bin/php%s", v)
		if _, err := os.Stat(bin); err == nil {
			found := false
			for _, existing := range versions {
				if existing == v {
					found = true
					break
				}
			}
			if !found {
				versions = append(versions, v)
			}
		}
	}
	if len(versions) == 0 {
		versions = append(versions, "8.2")
	}
	return versions
}

// ReloadNginx tests and reloads nginx config
func ReloadNginx() error {
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx config test failed: %s", strings.TrimSpace(string(out)))
	}
	return exec.Command("systemctl", "reload", "nginx").Run()
}
