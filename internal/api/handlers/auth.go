package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tunnelpanel/tunnelpanel/internal/api"
	"github.com/tunnelpanel/tunnelpanel/internal/auth"
	"github.com/tunnelpanel/tunnelpanel/internal/config"
	"github.com/tunnelpanel/tunnelpanel/internal/database"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	cfg *config.Config
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{cfg: cfg}
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
		api.Error(c, http.StatusBadRequest, "username and password are required")
		return
	}

	token, user, err := auth.Authenticate(req.Username, req.Password, h.cfg.JWTSecret, h.cfg.SessionExpiry)
	if err != nil {
		// Log failed attempt
		database.DB().Exec(
			"INSERT INTO audit_log (action, details, ip_address) VALUES (?, ?, ?)",
			"login_failed", "username: "+req.Username, c.ClientIP(),
		)
		api.Error(c, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// Set httpOnly cookie
	c.SetCookie(
		"tunnelpanel_token",
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

	api.Success(c, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
		"token": token,
	})
}

// Logout handles POST /api/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("tunnelpanel_token", "", -1, "/", "", true, true)
	api.Success(c, gin.H{"message": "logged out"})
}

// Me handles GET /api/auth/me — returns current user info
func (h *AuthHandler) Me(c *gin.Context) {
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")

	api.Success(c, gin.H{
		"id":       userID,
		"username": username,
	})
}

// SetupRequest represents the first-run setup form
type SetupRequest struct {
	Username           string `json:"username" binding:"required"`
	Password           string `json:"password" binding:"required"`
	CloudflareAPIToken string `json:"cloudflare_api_token"`
	CloudflareAccountID string `json:"cloudflare_account_id"`
	PanelDomain        string `json:"panel_domain"`
}

// Setup handles POST /api/setup — first-time panel setup
func (h *AuthHandler) Setup(c *gin.Context) {
	// Check if setup already done
	count, _ := auth.UserCount()
	if count > 0 {
		api.Error(c, http.StatusConflict, "setup already completed")
		return
	}

	var req SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.Error(c, http.StatusBadRequest, "username and password are required")
		return
	}

	// Create admin user
	_, err := auth.CreateUser(req.Username, req.Password)
	if err != nil {
		api.Error(c, http.StatusInternalServerError, "failed to create user: "+err.Error())
		return
	}

	// Generate JWT secret
	secret, err := auth.GenerateJWTSecret()
	if err != nil {
		api.Error(c, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	// Update config
	h.cfg.Update(func(cfg *config.Config) {
		cfg.JWTSecret = secret
		if req.CloudflareAPIToken != "" {
			cfg.CloudflareAPIToken = req.CloudflareAPIToken
			cfg.CloudflareAccountID = req.CloudflareAccountID
		}
		if req.PanelDomain != "" {
			cfg.PanelDomain = req.PanelDomain
		}
	})

	// Store Cloudflare config in DB
	if req.CloudflareAPIToken != "" {
		database.DB().Exec(
			"UPDATE cloudflare_config SET api_token = ?, account_id = ?, panel_domain = ?, updated_at = ? WHERE id = 1",
			req.CloudflareAPIToken, req.CloudflareAccountID, req.PanelDomain, time.Now(),
		)
	}

	// Mark setup complete
	database.DB().Exec("UPDATE settings SET value = 'true' WHERE key = 'setup_complete'")

	// Auto-login
	token, _, _ := auth.Authenticate(req.Username, req.Password, secret, h.cfg.SessionExpiry)
	c.SetCookie("tunnelpanel_token", token, h.cfg.SessionExpiry*3600, "/", "", true, true)

	api.Success(c, gin.H{
		"message": "setup completed successfully",
		"token":   token,
	})
}
