package handlers

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/Muhammedhashirm009/portix/internal/auth"
	"github.com/Muhammedhashirm009/portix/internal/config"
	"github.com/Muhammedhashirm009/portix/internal/database"
	"github.com/Muhammedhashirm009/portix/internal/httputil"
	"github.com/Muhammedhashirm009/portix/internal/tunnel"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	cfg       *config.Config
	tunnelMgr *tunnel.Manager
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(cfg *config.Config, tunnelMgr *tunnel.Manager) *AuthHandler {
	return &AuthHandler{cfg: cfg, tunnelMgr: tunnelMgr}
}

// LoginRequest represents the login form data
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles POST /api/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "username and password are required")
		return
	}

	token, user, err := auth.Authenticate(req.Username, req.Password, h.cfg.JWTSecret, h.cfg.SessionExpiry)
	if err != nil {
		// Log failed attempt
		database.DB().Exec(
			"INSERT INTO audit_log (action, details, ip_address) VALUES (?, ?, ?)",
			"login_failed", "username: "+req.Username, c.ClientIP(),
		)
		httputil.Error(c, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// Set httpOnly cookie
	c.SetCookie(
		"portix_token",
		token,
		h.cfg.SessionExpiry*3600, // seconds
		"/",
		"",
		true,  // secure (tunnel uses HTTPS)
		true,  // httpOnly
	)

	// Log success
	database.DB().Exec(
		"INSERT INTO audit_log (user_id, action, ip_address) VALUES (?, ?, ?)",
		user.ID, "login_success", c.ClientIP(),
	)

	httputil.Success(c, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
		"token": token,
	})
}

// Logout handles POST /api/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("portix_token", "", -1, "/", "", true, true)
	httputil.Success(c, gin.H{"message": "logged out"})
}

// Me handles GET /api/auth/me — returns current user info
func (h *AuthHandler) Me(c *gin.Context) {
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")

	httputil.Success(c, gin.H{
		"id":       userID,
		"username": username,
	})
}

// SetupRequest represents the first-run setup form
type SetupRequest struct {
	Username            string `json:"username" binding:"required"`
	Password            string `json:"password" binding:"required"`
	CloudflareAPIToken  string `json:"cloudflare_api_token"`
	CloudflareAccountID string `json:"cloudflare_account_id"`
	CloudflareZoneID    string `json:"cloudflare_zone_id"`
	CloudflareZoneName  string `json:"cloudflare_zone_name"`
	PanelDomain         string `json:"panel_domain"`
}

// Setup handles POST /api/setup — first-time panel setup
func (h *AuthHandler) Setup(c *gin.Context) {
	// Check if setup already done
	count, _ := auth.UserCount()
	if count > 0 {
		httputil.Error(c, http.StatusConflict, "setup already completed")
		return
	}

	var req SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "username and password are required")
		return
	}

	// Create admin user
	_, err := auth.CreateUser(req.Username, req.Password)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, "failed to create user: "+err.Error())
		return
	}

	// Generate JWT secret
	secret, err := auth.GenerateJWTSecret()
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	// Update config
	h.cfg.Update(func(cfg *config.Config) {
		cfg.JWTSecret = secret
		if req.CloudflareAPIToken != "" {
			cfg.CloudflareAPIToken = req.CloudflareAPIToken
			cfg.CloudflareAccountID = req.CloudflareAccountID
			cfg.CloudflareZoneID = req.CloudflareZoneID
			cfg.CloudflareZoneName = req.CloudflareZoneName
		}
		if req.PanelDomain != "" {
			cfg.PanelDomain = req.PanelDomain
		}
	})

	// Store Cloudflare config in DB
	if req.CloudflareAPIToken != "" {
		database.DB().Exec(
			`UPDATE cloudflare_config SET api_token = ?, account_id = ?, zone_id = ?, zone_name = ?, panel_domain = ?, updated_at = ? WHERE id = 1`,
			req.CloudflareAPIToken, req.CloudflareAccountID, req.CloudflareZoneID, req.CloudflareZoneName, req.PanelDomain, time.Now(),
		)
	}

	// Mark setup complete
	database.DB().Exec("INSERT OR REPLACE INTO settings (key, value) VALUES ('setup_complete', 'true')")

	// Auto-create tunnels if Cloudflare is configured
	var tunnelResult *tunnel.SetupResult
	var tunnelError string
	if req.CloudflareAPIToken != "" && req.PanelDomain != "" {
		// Create a fresh Cloudflare client with the new credentials
		cf := tunnel.NewCloudflareClient(
			req.CloudflareAPIToken,
			req.CloudflareAccountID,
			req.CloudflareZoneID,
			req.CloudflareZoneName,
		)
		// Create a temporary manager with the new client for setup
		dataDir := h.cfg.DataDir
		if dataDir == "" {
			dataDir = "/etc/portix"
		}
		setupMgr := tunnel.NewManager(cf, nil, dataDir, "", "")
		result, setupErr := setupMgr.SetupTunnels(req.PanelDomain)
		if setupErr != nil {
			log.Printf("[setup] Tunnel auto-creation failed: %v", setupErr)
			tunnelError = setupErr.Error()
		} else {
			tunnelResult = result
			// Update config with tunnel IDs
			h.cfg.Update(func(cfg *config.Config) {
				cfg.PanelTunnelID = result.PanelTunnelID
				cfg.AppsTunnelID = result.AppsTunnelID
			})
		}
	}

	// Auto-login
	token, _, _ := auth.Authenticate(req.Username, req.Password, secret, h.cfg.SessionExpiry)
	c.SetCookie("portix_token", token, h.cfg.SessionExpiry*3600, "/", "", true, true)

	response := gin.H{
		"message": "setup completed successfully",
		"token":   token,
	}
	if tunnelResult != nil {
		response["tunnels"] = tunnelResult
	}
	if tunnelError != "" {
		response["tunnel_error"] = tunnelError
	}

	httputil.Success(c, response)
}
