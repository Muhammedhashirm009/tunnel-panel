package tunnel

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tunnelpanel/tunnelpanel/internal/database"
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

// AddIngressRule adds a domain → localhost:port mapping to Tunnel #2
func (m *Manager) AddIngressRule(domain string, port int, appType string, appID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Create DNS CNAME record pointing to tunnel
	record, err := m.cf.CreateDNSRecord(domain, m.tunnelID)
	if err != nil {
		return fmt.Errorf("failed to create DNS record for %s: %w", domain, err)
	}

	// 2. Store the ingress rule in DB
	_, err = database.DB().Exec(
		"INSERT INTO tunnel_ingress_rules (domain, target, app_type, app_id) VALUES (?, ?, ?, ?)",
		domain, fmt.Sprintf("http://localhost:%d", port), appType, appID,
	)
	if err != nil {
		// Rollback DNS record
		m.cf.DeleteDNSRecord(record.ID)
		return fmt.Errorf("failed to store ingress rule: %w", err)
	}

	// 3. Regenerate tunnel config and reload
	if err := m.regenerateConfig(); err != nil {
		return fmt.Errorf("failed to regenerate tunnel config: %w", err)
	}

	if err := m.reloadTunnel(); err != nil {
		return fmt.Errorf("failed to reload tunnel: %w", err)
	}

	log.Printf("[tunnel] Added ingress: %s → localhost:%d (%s #%d)", domain, port, appType, appID)
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
	if dnsRecordID != "" {
		if err := m.cf.DeleteDNSRecord(dnsRecordID); err != nil {
			log.Printf("[tunnel] Warning: failed to delete DNS record for %s: %v", domain, err)
		}
	} else {
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

// GetIngressRules returns all current ingress rules
func (m *Manager) GetIngressRules() ([]IngressRuleInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

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
func (m *Manager) regenerateConfig() error {
	rules, err := m.GetIngressRules()
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
