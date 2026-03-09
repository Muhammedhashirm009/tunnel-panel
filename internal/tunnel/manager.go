package tunnel

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Muhammedhashirm009/portix/internal/database"
	"gopkg.in/yaml.v3"
)

// Manager orchestrates the App Tunnel (Tunnel #2) lifecycle
type Manager struct {
	mu            sync.RWMutex
	cf            *CloudflareClient
	portAllocator *PortAllocator
	configDir     string
	tunnelID      string
	tunnelSecret  string
}

// IngressRule defines a single ingress routing rule
type IngressRule struct {
	Hostname string            `yaml:"hostname,omitempty"`
	Service  string            `yaml:"service"`
	OriginRequest *OriginRequest `yaml:"originRequest,omitempty"`
}

// OriginRequest holds per-rule origin settings
type OriginRequest struct {
	NoTLSVerify bool `yaml:"noTLSVerify,omitempty"`
}

// TunnelConfig is the cloudflared config YAML structure
type TunnelConfig struct {
	Tunnel  string        `yaml:"tunnel"`
	CredentialsFile string `yaml:"credentials-file"`
	Ingress []IngressRule `yaml:"ingress"`
}

// NewManager creates a new tunnel manager
func NewManager(cf *CloudflareClient, pa *PortAllocator, configDir, tunnelID, tunnelSecret string) *Manager {
	return &Manager{
		cf:            cf,
		portAllocator: pa,
		configDir:     configDir,
		tunnelID:      tunnelID,
		tunnelSecret:  tunnelSecret,
	}
}

// AllocatePort allocates a port for a hosted app
func (m *Manager) AllocatePort(appType string, appID int) (int, error) {
	return m.portAllocator.Allocate(appType, appID)
}

// ensureConfigured loads CF credentials and tunnel IDs from DB if not already set
func (m *Manager) ensureConfigured() {
	if m.cf != nil && m.tunnelID != "" {
		return // already configured
	}

	var apiToken, accountID, zoneID, zoneName, appsTunnelID string
	err := database.DB().QueryRow(
		"SELECT COALESCE(api_token,''), COALESCE(account_id,''), COALESCE(zone_id,''), COALESCE(zone_name,''), COALESCE(tunnel_apps_id,'') FROM cloudflare_config WHERE id = 1",
	).Scan(&apiToken, &accountID, &zoneID, &zoneName, &appsTunnelID)

	if err != nil || apiToken == "" {
		log.Printf("[tunnel] ensureConfigured: no CF config in DB")
		return
	}

	if m.cf == nil {
		m.cf = NewCloudflareClient(apiToken, accountID, zoneID, zoneName)
		log.Printf("[tunnel] ensureConfigured: loaded CF client from DB")
	}

	if m.tunnelID == "" && appsTunnelID != "" {
		m.tunnelID = appsTunnelID
		log.Printf("[tunnel] ensureConfigured: loaded apps tunnel ID from DB: %s", appsTunnelID)
	}

	if m.configDir == "" {
		m.configDir = "/etc/portix"
	}
}

// AddIngressRule adds a domain → localhost:port mapping to Tunnel #2
func (m *Manager) AddIngressRule(domain string, port int, appType string, appID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reject well-known non-HTTP ports — cloudflared tunnels only support HTTP/HTTPS.
	// Tunneling binary protocols (MySQL, Redis, SMTP, etc.) causes malformed HTTP errors.
	nonHTTPPorts := map[int]string{
		3306:  "MySQL/MariaDB (binary protocol — use phpMyAdmin via Auto Tunnel instead)",
		5432:  "PostgreSQL (binary protocol — use a web-based DB tool instead)",
		6379:  "Redis (binary protocol)",
		27017: "MongoDB (binary protocol)",
		25:    "SMTP (email protocol)",
		465:   "SMTPS (email protocol)",
		587:   "SMTP submission (email protocol)",
		22:    "SSH (binary protocol)",
		21:    "FTP (binary protocol)",
		3389:  "RDP (binary protocol)",
	}
	if reason, bad := nonHTTPPorts[port]; bad {
		return fmt.Errorf("port %d is not HTTP-compatible (%s). Cloudflare tunnels only forward HTTP/HTTPS traffic", port, reason)
	}

	// Load CF config from DB if not already set
	m.ensureConfigured()

	log.Printf("[tunnel] ── Adding ingress rule ──")
	log.Printf("[tunnel]   Domain:  %s", domain)
	log.Printf("[tunnel]   Target:  localhost:%d", port)
	log.Printf("[tunnel]   Type:    %s (ID: %d)", appType, appID)

	// Check if this domain already has an ingress rule
	var existingID int
	var existingDNSRecordID string
	database.DB().QueryRow("SELECT id FROM tunnel_ingress_rules WHERE domain = ?", domain).Scan(&existingID)
	if existingID > 0 {
		log.Printf("[tunnel]   ⚠ Ingress rule already exists for %s (id=%d) — updating target to localhost:%d", domain, existingID, port)
	}

	// 1. Create or update DNS CNAME record
	var dnsRecordID string
	if m.cf != nil && m.tunnelID != "" {
		log.Printf("[tunnel]   Step 1: Upserting DNS CNAME → %s.cfargotunnel.com", m.tunnelID)
		record, err := m.cf.CreateDNSRecord(domain, m.tunnelID)
		if err != nil {
			log.Printf("[tunnel]   ⚠ DNS upsert failed: %v (continuing without DNS)", err)
		} else {
			dnsRecordID = record.ID
			existingDNSRecordID = dnsRecordID
			log.Printf("[tunnel]   ✓ DNS CNAME upserted (record ID: %s)", dnsRecordID)
			if appType == "site" && appID > 0 {
				database.DB().Exec("UPDATE sites SET dns_record_id = ? WHERE id = ?", dnsRecordID, appID)
			}
		}
	} else {
		log.Printf("[tunnel]   ⚠ Cloudflare not configured — skipping DNS")
	}
	_ = existingDNSRecordID

	// 2. Upsert the ingress rule in DB (handles duplicates gracefully)
	log.Printf("[tunnel]   Step 2: Upserting ingress rule in database")
	_, err := database.DB().Exec(
		"INSERT INTO tunnel_ingress_rules (domain, target, app_type, app_id) VALUES (?, ?, ?, ?) "+
			"ON CONFLICT(domain) DO UPDATE SET target=excluded.target, app_type=excluded.app_type, app_id=excluded.app_id",
		domain, fmt.Sprintf("http://localhost:%d", port), appType, appID,
	)
	if err != nil {
		if dnsRecordID != "" && m.cf != nil {
			m.cf.DeleteDNSRecord(dnsRecordID)
		}
		return fmt.Errorf("failed to store ingress rule: %w", err)
	}
	log.Printf("[tunnel]   ✓ Ingress rule upserted")

	// 3. Regenerate tunnel config and reload
	log.Printf("[tunnel]   Step 3: Regenerating apps tunnel config")
	if err := m.regenerateConfig(); err != nil {
		log.Printf("[tunnel]   ⚠ Config regeneration failed: %v", err)
		return fmt.Errorf("failed to regenerate tunnel config: %w", err)
	}
	log.Printf("[tunnel]   ✓ Tunnel config regenerated")

	log.Printf("[tunnel]   Step 4: Reloading apps tunnel service")
	if err := m.reloadTunnel(); err != nil {
		log.Printf("[tunnel]   ⚠ Tunnel reload failed: %v (service may not be running yet)", err)
	} else {
		log.Printf("[tunnel]   ✓ Tunnel service reloaded")
	}

	log.Printf("[tunnel] ── Ingress complete: %s → localhost:%d ──", domain, port)
	return nil
}

// RemoveIngressRule removes a domain routing rule
func (m *Manager) RemoveIngressRule(domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get DNS record ID from the ingress rule in our DB
	// We need to look it up by domain in the appropriate table (sites or docker_apps)
	var dnsRecordID string

	// Try sites table
	err := database.DB().QueryRow("SELECT dns_record_id FROM sites WHERE domain = ?", domain).Scan(&dnsRecordID)
	if err != nil {
		// Try docker_apps table
		err = database.DB().QueryRow("SELECT dns_record_id FROM docker_apps WHERE domain = ?", domain).Scan(&dnsRecordID)
	}

	// Delete DNS record from Cloudflare
	if dnsRecordID != "" && m.cf != nil {
		if err := m.cf.DeleteDNSRecord(dnsRecordID); err != nil {
			log.Printf("[tunnel] Warning: failed to delete DNS record for %s: %v", domain, err)
		}
	} else if m.cf != nil {
		// Try to find and delete by name
		rec, err := m.cf.GetDNSRecordByName(domain)
		if err == nil {
			m.cf.DeleteDNSRecord(rec.ID)
		}
	}

	// Remove from DB
	database.DB().Exec("DELETE FROM tunnel_ingress_rules WHERE domain = ?", domain)

	// Regenerate and reload
	if err := m.regenerateConfig(); err != nil {
		return err
	}
	return m.reloadTunnel()
}

// GetIngressRules returns all current ingress rules (public, acquires read lock)
func (m *Manager) GetIngressRules() ([]IngressRuleInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getIngressRulesInternal()
}

// getIngressRulesInternal fetches ingress rules WITHOUT locking (caller must hold lock)
func (m *Manager) getIngressRulesInternal() ([]IngressRuleInfo, error) {
	rows, err := database.DB().Query(
		"SELECT id, domain, target, app_type, app_id, enabled, created_at FROM tunnel_ingress_rules ORDER BY domain ASC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []IngressRuleInfo
	for rows.Next() {
		var r IngressRuleInfo
		if err := rows.Scan(&r.ID, &r.Domain, &r.Target, &r.AppType, &r.AppID, &r.Enabled, &r.CreatedAt); err != nil {
			continue
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// IngressRuleInfo holds ingress rule information for the API
type IngressRuleInfo struct {
	ID        int    `json:"id"`
	Domain    string `json:"domain"`
	Target    string `json:"target"`
	AppType   string `json:"app_type"`
	AppID     int    `json:"app_id"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

// regenerateConfig rebuilds the cloudflared config YAML from DB
// NOTE: caller must hold the write lock (m.mu)
func (m *Manager) regenerateConfig() error {
	rules, err := m.getIngressRulesInternal()
	if err != nil {
		return fmt.Errorf("failed to get ingress rules: %w", err)
	}

	if m.tunnelID == "" {
		return fmt.Errorf("apps tunnel ID is empty — cannot generate config")
	}

	credsFile := filepath.Join(m.configDir, "tunnel-apps-creds.json")
	config := TunnelConfig{
		Tunnel:          m.tunnelID,
		CredentialsFile: credsFile,
		Ingress:         []IngressRule{},
	}

	// Add all enabled app rules
	enabledCount := 0
	for _, r := range rules {
		if !r.Enabled {
			log.Printf("[tunnel]     skip disabled rule: %s → %s", r.Domain, r.Target)
			continue
		}
		config.Ingress = append(config.Ingress, IngressRule{
			Hostname: r.Domain,
			Service:  r.Target,
		})
		log.Printf("[tunnel]     ingress: %s → %s", r.Domain, r.Target)
		enabledCount++
	}

	// Catch-all rule (required by cloudflared)
	config.Ingress = append(config.Ingress, IngressRule{
		Service: "http_status:404",
	})

	// Write YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal tunnel config: %w", err)
	}

	// Ensure Unix line endings
	yamlStr := strings.ReplaceAll(string(data), "\r\n", "\n")
	yamlStr = strings.ReplaceAll(yamlStr, "\r", "\n")

	configPath := filepath.Join(m.configDir, "tunnel-apps.yml")
	if err := os.WriteFile(configPath, []byte(yamlStr), 0600); err != nil {
		return fmt.Errorf("failed to write tunnel config: %w", err)
	}

	log.Printf("[tunnel] Regenerated %s: tunnel=%s, %d ingress rules, creds=%s", configPath, m.tunnelID, enabledCount, credsFile)
	return nil
}

// reloadTunnel gracefully restarts the cloudflared service for app tunnel
func (m *Manager) reloadTunnel() error {
	cmd := exec.Command("systemctl", "restart", "portix-apps-tunnel")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restart apps tunnel: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// CreateDNSForDomain creates or updates a Cloudflare DNS CNAME so the given domain
// routes to the apps tunnel. Used when adding new custom domains (e.g. phpMyAdmin).
func (m *Manager) CreateDNSForDomain(domain string) error {
	if m.cf == nil {
		return fmt.Errorf("cloudflare client not configured")
	}
	if m.tunnelID == "" {
		return fmt.Errorf("apps tunnel ID not set — run Setup Tunnels first")
	}
	_, err := m.cf.CreateDNSRecord(domain, m.tunnelID)
	return err
}

// GetTunnelStatus returns the status of both tunnels including health diagnostics
func (m *Manager) GetTunnelStatus() (*TunnelStatus, error) {
	m.ensureConfigured()

	panelStatus := strings.TrimSpace(func() string {
		out, _ := exec.Command("systemctl", "is-active", "portix-panel-tunnel").Output()
		return string(out)
	}())
	appsStatus := strings.TrimSpace(func() string {
		out, _ := exec.Command("systemctl", "is-active", "portix-apps-tunnel").Output()
		return string(out)
	}())

	// Check journal for connection errors (last 30 lines is enough)
	panelErr := checkTunnelServiceHealth("portix-panel-tunnel")
	appsErr := checkTunnelServiceHealth("portix-apps-tunnel")

	// Try to get CF tunnel info to verify IDs actually exist
	var panelInfo, appsInfo *Tunnel
	if m.cf != nil {
		var panelTunnelID string
		database.DB().QueryRow("SELECT COALESCE(tunnel_panel_id,'') FROM cloudflare_config WHERE id=1").Scan(&panelTunnelID)
		if panelTunnelID != "" {
			if t, err := m.cf.GetTunnel(panelTunnelID); err != nil {
				// CF returns an error — the tunnel ID doesn't exist
				if panelErr == "" {
					panelErr = "Tunnel ID not found in Cloudflare — click Repair to recreate"
				}
			} else {
				panelInfo = t
			}
		}
		if m.tunnelID != "" {
			if t, err := m.cf.GetTunnel(m.tunnelID); err != nil {
				if appsErr == "" {
					appsErr = "Tunnel ID not found in Cloudflare — click Repair to recreate"
				}
			} else {
				appsInfo = t
			}
		}
	}

	return &TunnelStatus{
		PanelTunnel: TunnelInfo{
			ServiceStatus:  panelStatus,
			Running:        panelStatus == "active",
			Healthy:        panelStatus == "active" && panelErr == "" && panelInfo != nil,
			LastError:       panelErr,
			CloudflareInfo: panelInfo,
		},
		AppsTunnel: TunnelInfo{
			ServiceStatus:  appsStatus,
			Running:        appsStatus == "active",
			Healthy:        appsStatus == "active" && appsErr == "" && appsInfo != nil,
			LastError:       appsErr,
			CloudflareInfo: appsInfo,
		},
	}, nil
}

// checkTunnelServiceHealth reads the journal for this service and returns the last error message,
// or empty string if everything looks healthy.
func checkTunnelServiceHealth(serviceName string) string {
	out, err := exec.Command("journalctl", "-u", serviceName, "-n", "20", "--no-pager", "--output=cat").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	// Scan from newest to oldest for known error patterns
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		switch {
		case strings.Contains(line, "Tunnel not found"):
			return "Tunnel not found — credentials are stale. Click Repair Tunnels."
		case strings.Contains(line, "Unauthorized"):
			return "Unauthorized — API token or tunnel secret is invalid. Click Repair Tunnels."
		case strings.Contains(line, "failed to sufficiently increase receive buffer size"):
			continue // Non-critical warning, ignore
		case strings.Contains(line, "ERR ") && strings.Contains(line, "error"):
			// Generic error — trim timestamp prefix and return
			if idx := strings.Index(line, "ERR "); idx >= 0 {
				return strings.TrimSpace(line[idx+4:])
			}
		}
	}
	return ""
}

// TunnelStatus holds status of both tunnels
type TunnelStatus struct {
	PanelTunnel TunnelInfo `json:"panel_tunnel"`
	AppsTunnel  TunnelInfo `json:"apps_tunnel"`
}

// TunnelInfo holds individual tunnel status
type TunnelInfo struct {
	ServiceStatus  string  `json:"service_status"`
	Running        bool    `json:"running"`
	Healthy        bool    `json:"healthy"` // true = running AND no connection errors
	LastError      string  `json:"last_error,omitempty"` // last cloudflared error if any
	CloudflareInfo *Tunnel `json:"cloudflare_info,omitempty"`
}

// SetupResult holds the result of the automated tunnel setup
type SetupResult struct {
	PanelTunnelID string `json:"panel_tunnel_id"`
	AppsTunnelID  string `json:"apps_tunnel_id"`
	PanelDomain   string `json:"panel_domain"`
}

// SetupTunnels creates both tunnels via Cloudflare API, writes credentials, and configures cloudflared
func (m *Manager) SetupTunnels(panelDomain string) (*SetupResult, error) {
	if m.cf == nil {
		return nil, fmt.Errorf("cloudflare client not configured")
	}

	// 1. Verify API token
	if err := m.cf.VerifyToken(); err != nil {
		return nil, fmt.Errorf("invalid API token: %w", err)
	}
	log.Println("[tunnel] API token verified ✓")

	// 2. Find or create Panel Tunnel (#1)
	panelTunnel, panelSecret, err := m.findOrCreateTunnel("tunnelpanel-panel")
	if err != nil {
		return nil, fmt.Errorf("failed to provision panel tunnel: %w", err)
	}
	log.Printf("[tunnel] Panel tunnel ready: %s (ID: %s)", panelTunnel.Name, panelTunnel.ID)

	// 3. Find or create Apps Tunnel (#2)
	appsTunnel, appsSecret, err := m.findOrCreateTunnel("portix-apps")
	if err != nil {
		return nil, fmt.Errorf("failed to provision apps tunnel: %w", err)
	}
	log.Printf("[tunnel] Apps tunnel ready: %s (ID: %s)", appsTunnel.Name, appsTunnel.ID)

	// 4. Write credential files (only for newly created tunnels — secret is empty for reused ones)
	if panelSecret != "" {
		if err := m.writeCredentials(panelTunnel.ID, panelSecret, "portix-creds.json"); err != nil {
			return nil, fmt.Errorf("failed to write panel tunnel credentials: %w", err)
		}
	} else {
		log.Println("[tunnel] Reusing existing panel tunnel — skipping credential overwrite")
	}
	if appsSecret != "" {
		if err := m.writeCredentials(appsTunnel.ID, appsSecret, "tunnel-apps-creds.json"); err != nil {
			return nil, fmt.Errorf("failed to write apps tunnel credentials: %w", err)
		}
	} else {
		log.Println("[tunnel] Reusing existing apps tunnel — skipping credential overwrite")
	}
	log.Println("[tunnel] Credentials handled ✓")

	// 5. Write cloudflared config for panel tunnel
	panelConfig := TunnelConfig{
		Tunnel:          panelTunnel.ID,
		CredentialsFile: filepath.Join(m.configDir, "portix-creds.json"),
		Ingress: []IngressRule{
			{Hostname: panelDomain, Service: "http://localhost:8443"},
			{Service: "http_status:404"},
		},
	}
	panelConfigData, _ := yaml.Marshal(panelConfig)
	panelConfigPath := filepath.Join(m.configDir, "portix.yml")
	if err := os.WriteFile(panelConfigPath, panelConfigData, 0600); err != nil {
		return nil, fmt.Errorf("failed to write panel tunnel config: %w", err)
	}

	// 6. Write cloudflared config for apps tunnel — rebuild from DB ingress rules
	m.tunnelID = appsTunnel.ID
	m.tunnelSecret = appsSecret
	appsConfigPath := filepath.Join(m.configDir, "tunnel-apps.yml")
	if err := m.regenerateConfig(); err != nil {
		// Fallback: write minimal config with catch-all if regenerate fails
		log.Printf("[tunnel] regenerateConfig failed, writing minimal: %v", err)
		appsConfig := TunnelConfig{
			Tunnel:          appsTunnel.ID,
			CredentialsFile: filepath.Join(m.configDir, "tunnel-apps-creds.json"),
			Ingress:         []IngressRule{{Service: "http_status:404"}},
		}
		appsConfigData, _ := yaml.Marshal(appsConfig)
		if werr := os.WriteFile(appsConfigPath, appsConfigData, 0600); werr != nil {
			return nil, fmt.Errorf("failed to write apps tunnel config: %w", werr)
		}
	}
	log.Println("[tunnel] Apps tunnel config written from DB ingress rules ✓")


	// 7. Create DNS CNAME for panel domain
	_, err = m.cf.CreateDNSRecord(panelDomain, panelTunnel.ID)
	if err != nil {
		log.Printf("[tunnel] Warning: panel DNS record failed (may already exist): %v", err)
	} else {
		log.Printf("[tunnel] DNS CNAME updated: %s → %s.cfargotunnel.com", panelDomain, panelTunnel.ID)
	}

	// 7b. Update DNS CNAMEs for ALL existing ingress rules to point to the new apps tunnel
	// This is critical after a repair — old domains still point to the deleted tunnel ID.
	existingRules, _ := m.getIngressRulesInternal()
	for _, rule := range existingRules {
		if !rule.Enabled || rule.Domain == "" {
			continue
		}
		if _, dnsErr := m.cf.CreateDNSRecord(rule.Domain, appsTunnel.ID); dnsErr != nil {
			log.Printf("[tunnel] ⚠ DNS update failed for %s: %v", rule.Domain, dnsErr)
		} else {
			log.Printf("[tunnel] ✓ DNS CNAME updated: %s → %s.cfargotunnel.com", rule.Domain, appsTunnel.ID)
		}
	}
	log.Printf("[tunnel] DNS records updated for %d ingress rule(s) ✓", len(existingRules))

	// 8. Update systemd service files to use correct config paths
	m.updateSystemdService("portix-panel-tunnel", panelConfigPath)
	m.updateSystemdService("portix-apps-tunnel", appsConfigPath)

	// 9. Store tunnel IDs in database
	database.DB().Exec(
		"UPDATE cloudflare_config SET tunnel_panel_id = ?, tunnel_apps_id = ? WHERE id = 1",
		panelTunnel.ID, appsTunnel.ID,
	)

	// 10. Reload systemd and start tunnels
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "portix-panel-tunnel").Run()
	exec.Command("systemctl", "enable", "portix-apps-tunnel").Run()
	exec.Command("systemctl", "start", "portix-panel-tunnel").Run()
	exec.Command("systemctl", "start", "portix-apps-tunnel").Run()
	log.Println("[tunnel] Tunnels started ✓")

	return &SetupResult{
		PanelTunnelID: panelTunnel.ID,
		AppsTunnelID:  appsTunnel.ID,
		PanelDomain:   panelDomain,
	}, nil
}

// writeCredentials writes a cloudflared tunnel credentials JSON file
func (m *Manager) writeCredentials(tunnelID, secret, filename string) error {
	creds := map[string]string{
		"AccountTag":   m.cf.accountID,
		"TunnelSecret": secret,
		"TunnelID":     tunnelID,
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(m.configDir, filename), data, 0600)
}

// updateSystemdService updates a systemd service file to use the correct cloudflared config
func (m *Manager) updateSystemdService(serviceName, configPath string) {
	serviceContent := fmt.Sprintf("[Unit]\nDescription=Cloudflare Tunnel - %s\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=/usr/bin/cloudflared tunnel --config %s run\nRestart=always\nRestartSec=5\nKillMode=process\n\n[Install]\nWantedBy=multi-user.target\n", serviceName, configPath)

	// Ensure Unix line endings (important when code is compiled from Windows)
	serviceContent = strings.ReplaceAll(serviceContent, "\r\n", "\n")
	serviceContent = strings.ReplaceAll(serviceContent, "\r", "\n")

	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	os.WriteFile(servicePath, []byte(serviceContent), 0644)
}

// findOrCreateTunnel deletes any existing Cloudflare tunnel with baseName and creates a fresh one.
// This guarantees that the credential file on disk always matches what Cloudflare expects —
// avoiding "Tunnel not found" errors caused by stale/rotated secrets.
func (m *Manager) findOrCreateTunnel(baseName string) (*Tunnel, string, error) {
	// Stop any running tunnel service first so cloudflared releases the tunnel connection
	svcName := map[string]string{
		"tunnelpanel-panel": "portix-panel-tunnel",
		"portix-apps":  "portix-apps-tunnel",
	}[baseName]
	if svcName != "" {
		exec.Command("systemctl", "stop", svcName).Run()
		log.Printf("[tunnel] findOrCreateTunnel: stopped %s", svcName)
	}

	// List all active tunnels and delete any with the same base name
	tunnels, err := m.cf.ListTunnels()
	if err != nil {
		log.Printf("[tunnel] findOrCreateTunnel: could not list tunnels (%v) — proceeding with creation", err)
	} else {
		for _, t := range tunnels {
			if t.Name == baseName {
				log.Printf("[tunnel] findOrCreateTunnel: deleting stale tunnel '%s' (id=%s)", baseName, t.ID)
				if delErr := m.cf.DeleteTunnel(t.ID); delErr != nil {
					// If delete fails (e.g. tunnel has active connections), log and proceed anyway
					log.Printf("[tunnel] findOrCreateTunnel: delete failed for %s (%v) — will try timestamp-name instead", t.ID, delErr)
					// Fall through to suffix creation
					suffixedName := fmt.Sprintf("%s-%d", baseName, time.Now().Unix())
					tunnel, secret, err := m.cf.CreateTunnel(suffixedName)
					if err != nil {
						return nil, "", fmt.Errorf("failed to create tunnel '%s': %w", suffixedName, err)
					}
					log.Printf("[tunnel] findOrCreateTunnel: created '%s' (id=%s)", suffixedName, tunnel.ID)
					return tunnel, secret, nil
				}
				log.Printf("[tunnel] findOrCreateTunnel: deleted stale '%s' ✓", baseName)
				break
			}
		}
	}

	// Create a fresh tunnel with the base name
	tunnel, secret, err := m.cf.CreateTunnel(baseName)
	if err != nil {
		// Name collision from a recently-deleted tunnel (Cloudflare may cache names briefly)
		suffixedName := fmt.Sprintf("%s-%d", baseName, time.Now().Unix())
		log.Printf("[tunnel] findOrCreateTunnel: '%s' failed (%v) — retrying as '%s'", baseName, err, suffixedName)
		tunnel, secret, err = m.cf.CreateTunnel(suffixedName)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create tunnel (tried '%s' and '%s'): %w", baseName, suffixedName, err)
		}
	}

	log.Printf("[tunnel] findOrCreateTunnel: created fresh tunnel '%s' (id=%s)", tunnel.Name, tunnel.ID)
	return tunnel, secret, nil
}

