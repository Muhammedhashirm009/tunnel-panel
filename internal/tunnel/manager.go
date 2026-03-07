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

	"github.com/Muhammedhashirm009/tunnel-panel/internal/database"
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
		m.configDir = "/etc/tunnelpanel"
	}
}

// AddIngressRule adds a domain → localhost:port mapping to Tunnel #2
func (m *Manager) AddIngressRule(domain string, port int, appType string, appID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load CF config from DB if not already set
	m.ensureConfigured()

	log.Printf("[tunnel] ── Adding ingress rule ──")
	log.Printf("[tunnel]   Domain:  %s", domain)
	log.Printf("[tunnel]   Target:  localhost:%d", port)
	log.Printf("[tunnel]   Type:    %s (ID: %d)", appType, appID)

	// 1. Create DNS CNAME record pointing to tunnel
	var dnsRecordID string
	if m.cf != nil && m.tunnelID != "" {
		log.Printf("[tunnel]   Step 1: Creating DNS CNAME → %s.cfargotunnel.com", m.tunnelID)
		record, err := m.cf.CreateDNSRecord(domain, m.tunnelID)
		if err != nil {
			log.Printf("[tunnel]   ⚠ DNS creation failed: %v (continuing without DNS)", err)
		} else {
			dnsRecordID = record.ID
			log.Printf("[tunnel]   ✓ DNS CNAME created (record ID: %s)", dnsRecordID)
			// Save DNS record ID for the site
			if appType == "site" && appID > 0 {
				database.DB().Exec("UPDATE sites SET dns_record_id = ? WHERE id = ?", dnsRecordID, appID)
			}
		}
	} else {
		log.Printf("[tunnel]   ⚠ Cloudflare not configured (cf=%v, tunnelID=%s) — skipping DNS", m.cf != nil, m.tunnelID)
	}

	// 2. Store the ingress rule in DB
	log.Printf("[tunnel]   Step 2: Storing ingress rule in database")
	_, err := database.DB().Exec(
		"INSERT INTO tunnel_ingress_rules (domain, target, app_type, app_id) VALUES (?, ?, ?, ?)",
		domain, fmt.Sprintf("http://localhost:%d", port), appType, appID,
	)
	if err != nil {
		if dnsRecordID != "" && m.cf != nil {
			m.cf.DeleteDNSRecord(dnsRecordID)
		}
		return fmt.Errorf("failed to store ingress rule: %w", err)
	}
	log.Printf("[tunnel]   ✓ Ingress rule stored")

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
		// Don't fail — the config is written, tunnel will pick it up when started
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
		return err
	}

	config := TunnelConfig{
		Tunnel:          m.tunnelID,
		CredentialsFile: filepath.Join(m.configDir, "tunnel-apps-creds.json"),
		Ingress:         []IngressRule{},
	}

	// Add all app rules
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		config.Ingress = append(config.Ingress, IngressRule{
			Hostname: r.Domain,
			Service:  r.Target,
		})
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

	configPath := filepath.Join(m.configDir, "tunnel-apps.yml")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write tunnel config: %w", err)
	}

	log.Printf("[tunnel] Regenerated config with %d ingress rules", len(rules))
	return nil
}

// reloadTunnel gracefully restarts the cloudflared service for app tunnel
func (m *Manager) reloadTunnel() error {
	cmd := exec.Command("systemctl", "restart", "tunnelpanel-apps-tunnel")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restart apps tunnel: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// GetTunnelStatus returns the status of both tunnels
func (m *Manager) GetTunnelStatus() (*TunnelStatus, error) {
	panelStatus := "unknown"
	appsStatus := "unknown"

	// Check panel tunnel systemd service
	out, _ := exec.Command("systemctl", "is-active", "tunnelpanel-panel-tunnel").Output()
	panelStatus = strings.TrimSpace(string(out))

	// Check apps tunnel systemd service
	out, _ = exec.Command("systemctl", "is-active", "tunnelpanel-apps-tunnel").Output()
	appsStatus = strings.TrimSpace(string(out))

	// Get more info from Cloudflare API if possible
	var panelInfo, appsInfo *Tunnel
	if m.tunnelID != "" {
		// Panel tunnel info
		var panelTunnelID string
		database.DB().QueryRow("SELECT tunnel_panel_id FROM cloudflare_config WHERE id = 1").Scan(&panelTunnelID)
		if panelTunnelID != "" {
			panelInfo, _ = m.cf.GetTunnel(panelTunnelID)
		}
		// Apps tunnel info
		appsInfo, _ = m.cf.GetTunnel(m.tunnelID)
	}

	return &TunnelStatus{
		PanelTunnel: TunnelInfo{
			ServiceStatus: panelStatus,
			Running:       panelStatus == "active",
			CloudflareInfo: panelInfo,
		},
		AppsTunnel: TunnelInfo{
			ServiceStatus: appsStatus,
			Running:       appsStatus == "active",
			CloudflareInfo: appsInfo,
		},
	}, nil
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

	// 2. Create Panel Tunnel (#1)
	panelTunnel, panelSecret, err := m.cf.CreateTunnel("tunnelpanel-panel")
	if err != nil {
		return nil, fmt.Errorf("failed to create panel tunnel: %w", err)
	}
	log.Printf("[tunnel] Created panel tunnel: %s (ID: %s)", panelTunnel.Name, panelTunnel.ID)

	// 3. Create Apps Tunnel (#2)
	appsTunnel, appsSecret, err := m.cf.CreateTunnel("tunnelpanel-apps")
	if err != nil {
		// Cleanup panel tunnel on failure
		m.cf.DeleteTunnel(panelTunnel.ID)
		return nil, fmt.Errorf("failed to create apps tunnel: %w", err)
	}
	log.Printf("[tunnel] Created apps tunnel: %s (ID: %s)", appsTunnel.Name, appsTunnel.ID)

	// 4. Write credential files
	if err := m.writeCredentials(panelTunnel.ID, panelSecret, "tunnel-panel-creds.json"); err != nil {
		return nil, fmt.Errorf("failed to write panel tunnel credentials: %w", err)
	}
	if err := m.writeCredentials(appsTunnel.ID, appsSecret, "tunnel-apps-creds.json"); err != nil {
		return nil, fmt.Errorf("failed to write apps tunnel credentials: %w", err)
	}
	log.Println("[tunnel] Credentials written ✓")

	// 5. Write cloudflared config for panel tunnel
	panelConfig := TunnelConfig{
		Tunnel:          panelTunnel.ID,
		CredentialsFile: filepath.Join(m.configDir, "tunnel-panel-creds.json"),
		Ingress: []IngressRule{
			{Hostname: panelDomain, Service: "http://localhost:8443"},
			{Service: "http_status:404"},
		},
	}
	panelConfigData, _ := yaml.Marshal(panelConfig)
	panelConfigPath := filepath.Join(m.configDir, "tunnel-panel.yml")
	if err := os.WriteFile(panelConfigPath, panelConfigData, 0600); err != nil {
		return nil, fmt.Errorf("failed to write panel tunnel config: %w", err)
	}

	// 6. Write cloudflared config for apps tunnel
	m.tunnelID = appsTunnel.ID
	m.tunnelSecret = appsSecret
	appsConfig := TunnelConfig{
		Tunnel:          appsTunnel.ID,
		CredentialsFile: filepath.Join(m.configDir, "tunnel-apps-creds.json"),
		Ingress: []IngressRule{
			{Service: "http_status:404"}, // catch-all, rules added later
		},
	}
	appsConfigData, _ := yaml.Marshal(appsConfig)
	appsConfigPath := filepath.Join(m.configDir, "tunnel-apps.yml")
	if err := os.WriteFile(appsConfigPath, appsConfigData, 0600); err != nil {
		return nil, fmt.Errorf("failed to write apps tunnel config: %w", err)
	}
	log.Println("[tunnel] Configs written ✓")

	// 7. Create DNS CNAME for panel domain
	_, err = m.cf.CreateDNSRecord(panelDomain, panelTunnel.ID)
	if err != nil {
		log.Printf("[tunnel] Warning: DNS record creation failed (may already exist): %v", err)
	} else {
		log.Printf("[tunnel] DNS CNAME created: %s → %s.cfargotunnel.com", panelDomain, panelTunnel.ID)
	}

	// 8. Update systemd service files to use correct config paths
	m.updateSystemdService("tunnelpanel-panel-tunnel", panelConfigPath)
	m.updateSystemdService("tunnelpanel-apps-tunnel", appsConfigPath)

	// 9. Store tunnel IDs in database
	database.DB().Exec(
		"UPDATE cloudflare_config SET tunnel_panel_id = ?, tunnel_apps_id = ? WHERE id = 1",
		panelTunnel.ID, appsTunnel.ID,
	)

	// 10. Reload systemd and start tunnels
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "tunnelpanel-panel-tunnel").Run()
	exec.Command("systemctl", "enable", "tunnelpanel-apps-tunnel").Run()
	exec.Command("systemctl", "start", "tunnelpanel-panel-tunnel").Run()
	exec.Command("systemctl", "start", "tunnelpanel-apps-tunnel").Run()
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
