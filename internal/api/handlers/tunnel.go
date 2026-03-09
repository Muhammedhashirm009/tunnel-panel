package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/Muhammedhashirm009/portix/internal/config"
	"github.com/Muhammedhashirm009/portix/internal/database"
	"github.com/Muhammedhashirm009/portix/internal/httputil"
	"github.com/Muhammedhashirm009/portix/internal/tunnel"
)

// TunnelHandler handles tunnel management endpoints
type TunnelHandler struct {
	cfg     *config.Config
	manager *tunnel.Manager
}

// NewTunnelHandler creates a new tunnel handler
func NewTunnelHandler(cfg *config.Config, mgr *tunnel.Manager) *TunnelHandler {
	return &TunnelHandler{cfg: cfg, manager: mgr}
}

// GetStatus handles GET /api/tunnels/status
func (h *TunnelHandler) GetStatus(c *gin.Context) {
	status, err := h.manager.GetTunnelStatus()
	if err != nil {
		httputil.Error(c, 500, "failed to get tunnel status: "+err.Error())
		return
	}
	httputil.Success(c, status)
}

// GetIngressRules handles GET /api/tunnels/ingress
func (h *TunnelHandler) GetIngressRules(c *gin.Context) {
	rules, err := h.manager.GetIngressRules()
	if err != nil {
		httputil.Error(c, 500, "failed to get ingress rules: "+err.Error())
		return
	}
	httputil.Success(c, rules)
}

// AddIngressRuleRequest represents a new ingress rule
type AddIngressRuleRequest struct {
	Domain  string `json:"domain" binding:"required"`
	Port    int    `json:"port" binding:"required"`
	AppType string `json:"app_type" binding:"required"`
	AppID   int    `json:"app_id"`
}

// AddIngressRule handles POST /api/tunnels/ingress
func (h *TunnelHandler) AddIngressRule(c *gin.Context) {
	var req AddIngressRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "domain, port, and app_type are required")
		return
	}

	if err := h.manager.AddIngressRule(req.Domain, req.Port, req.AppType, req.AppID); err != nil {
		httputil.Error(c, 500, "failed to add ingress rule: "+err.Error())
		return
	}

	httputil.Created(c, gin.H{"message": "ingress rule added", "domain": req.Domain})
}

// RemoveIngressRule handles DELETE /api/tunnels/ingress/:domain
func (h *TunnelHandler) RemoveIngressRule(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		httputil.Error(c, http.StatusBadRequest, "domain is required")
		return
	}

	if err := h.manager.RemoveIngressRule(domain); err != nil {
		httputil.Error(c, 500, "failed to remove ingress rule: "+err.Error())
		return
	}

	httputil.Success(c, gin.H{"message": "ingress rule removed", "domain": domain})
}

// CloudflareConfigRequest holds Cloudflare settings
type CloudflareConfigRequest struct {
	APIToken    string `json:"api_token" binding:"required"`
	AccountID   string `json:"account_id" binding:"required"`
	ZoneID      string `json:"zone_id"`
	ZoneName    string `json:"zone_name"`
	PanelDomain string `json:"panel_domain"`
}

// UpdateCloudflareConfig handles PUT /api/tunnels/cloudflare
func (h *TunnelHandler) UpdateCloudflareConfig(c *gin.Context) {
	var req CloudflareConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "api_token and account_id are required")
		return
	}

	// Verify token with Cloudflare
	client := tunnel.NewCloudflareClient(req.APIToken, req.AccountID, req.ZoneID, req.ZoneName)
	if err := client.VerifyToken(); err != nil {
		httputil.Error(c, http.StatusBadRequest, "invalid Cloudflare API token: "+err.Error())
		return
	}

	// Save to DB
	_, err := database.DB().Exec(
		`UPDATE cloudflare_config SET api_token = ?, account_id = ?, zone_id = ?, zone_name = ?, panel_domain = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`,
		req.APIToken, req.AccountID, req.ZoneID, req.ZoneName, req.PanelDomain,
	)
	if err != nil {
		httputil.Error(c, 500, "failed to save config: "+err.Error())
		return
	}

	// Update app config
	h.cfg.Update(func(cfg *config.Config) {
		cfg.CloudflareAPIToken = req.APIToken
		cfg.CloudflareAccountID = req.AccountID
		cfg.CloudflareZoneID = req.ZoneID
		cfg.CloudflareZoneName = req.ZoneName
		cfg.PanelDomain = req.PanelDomain
	})

	httputil.Success(c, gin.H{"message": "cloudflare config updated"})
}

// GetCloudflareConfig handles GET /api/tunnels/cloudflare
func (h *TunnelHandler) GetCloudflareConfig(c *gin.Context) {
	var token, accountID, zoneID, zoneName, panelDomain string
	database.DB().QueryRow(
		"SELECT api_token, account_id, zone_id, zone_name, panel_domain FROM cloudflare_config WHERE id = 1",
	).Scan(&token, &accountID, &zoneID, &zoneName, &panelDomain)

	// Mask API token
	maskedToken := ""
	if len(token) > 8 {
		maskedToken = token[:4] + "****" + token[len(token)-4:]
	}

	httputil.Success(c, gin.H{
		"api_token":    maskedToken,
		"account_id":   accountID,
		"zone_id":      zoneID,
		"zone_name":    zoneName,
		"panel_domain": panelDomain,
		"configured":   token != "",
	})
}

// ListZones handles GET /api/tunnels/zones
func (h *TunnelHandler) ListZones(c *gin.Context) {
	client := tunnel.NewCloudflareClient(
		h.cfg.CloudflareAPIToken,
		h.cfg.CloudflareAccountID,
		h.cfg.CloudflareZoneID,
		h.cfg.CloudflareZoneName,
	)

	zones, err := client.ListZones()
	if err != nil {
		httputil.Error(c, 500, "failed to list zones: "+err.Error())
		return
	}

	httputil.Success(c, zones)
}

// SetupTunnels handles POST /api/tunnels/setup — creates both tunnels via Cloudflare API
func (h *TunnelHandler) SetupTunnels(c *gin.Context) {
	// Read Cloudflare config from DB
	var apiToken, accountID, zoneID, zoneName, panelDomain string
	err := database.DB().QueryRow(
		"SELECT COALESCE(api_token,''), COALESCE(account_id,''), COALESCE(zone_id,''), COALESCE(zone_name,''), COALESCE(panel_domain,'') FROM cloudflare_config WHERE id = 1",
	).Scan(&apiToken, &accountID, &zoneID, &zoneName, &panelDomain)

	if err != nil || apiToken == "" {
		httputil.Error(c, http.StatusBadRequest, "Cloudflare credentials not configured. Please set them first.")
		return
	}
	if panelDomain == "" {
		httputil.Error(c, http.StatusBadRequest, "Panel domain not configured. Please set it in Cloudflare config.")
		return
	}

	// Create a fresh Cloudflare client
	cf := tunnel.NewCloudflareClient(apiToken, accountID, zoneID, zoneName)

	// Create a temporary manager for setup
	dataDir := h.cfg.DataDir
	if dataDir == "" {
		dataDir = "/etc/portix"
	}
	setupMgr := tunnel.NewManager(cf, nil, dataDir, "", "")

	log.Printf("[tunnel] Setting up tunnels for domain: %s", panelDomain)
	result, setupErr := setupMgr.SetupTunnels(panelDomain)
	if setupErr != nil {
		log.Printf("[tunnel] Tunnel setup failed: %v", setupErr)
		httputil.Error(c, http.StatusInternalServerError, "Tunnel setup failed: "+setupErr.Error())
		return
	}

	// Update config with tunnel IDs
	h.cfg.Update(func(cfg *config.Config) {
		cfg.PanelTunnelID = result.PanelTunnelID
		cfg.AppsTunnelID = result.AppsTunnelID
	})

	log.Printf("[tunnel] Tunnels created successfully: panel=%s, apps=%s", result.PanelTunnelID, result.AppsTunnelID)
	httputil.Success(c, result)
}
