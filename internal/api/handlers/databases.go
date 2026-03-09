package handlers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/Muhammedhashirm009/portix/internal/database"
	"github.com/Muhammedhashirm009/portix/internal/dbmanager"
	"github.com/Muhammedhashirm009/portix/internal/tunnel"
	"github.com/gin-gonic/gin"
)

// DatabaseHandler handles MySQL database management API
type DatabaseHandler struct {
	mgr       *dbmanager.Manager
	tunnelMgr *tunnel.Manager
}

// NewDatabaseHandler creates a new DatabaseHandler
func NewDatabaseHandler(tunnelMgr *tunnel.Manager) *DatabaseHandler {
	return &DatabaseHandler{mgr: dbmanager.NewManager(), tunnelMgr: tunnelMgr}
}

// ListDatabases returns all user-created databases
func (h *DatabaseHandler) ListDatabases(c *gin.Context) {
	if !h.mgr.IsAvailable() {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "MySQL/MariaDB is not available. Please ensure it is installed and running."})
		return
	}
	dbs, err := h.mgr.ListDatabases()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if dbs == nil {
		dbs = []dbmanager.Database{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": dbs})
}

// CreateDatabase creates a new database
func (h *DatabaseHandler) CreateDatabase(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Database name is required"})
		return
	}
	if err := h.mgr.CreateDatabase(req.Name); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Database created: " + req.Name})
}

// DropDatabase drops a database
func (h *DatabaseHandler) DropDatabase(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Database name is required"})
		return
	}
	if err := h.mgr.DropDatabase(name); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Database dropped: " + name})
}

// GetTables returns tables in a specific database
func (h *DatabaseHandler) GetTables(c *gin.Context) {
	name := c.Param("name")
	tables, err := h.mgr.GetTables(name)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if tables == nil {
		tables = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": tables})
}

// ListUsers returns all non-system MySQL users
func (h *DatabaseHandler) ListUsers(c *gin.Context) {
	if !h.mgr.IsAvailable() {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "MySQL/MariaDB is not available"})
		return
	}
	users, err := h.mgr.ListUsers()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if users == nil {
		users = []dbmanager.DBUser{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": users})
}

// CreateUser creates a MySQL user
func (h *DatabaseHandler) CreateUser(c *gin.Context) {
	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
		Host     string `json:"host"`
		Database string `json:"database"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.User == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Username and password are required"})
		return
	}
	if req.Host == "" {
		req.Host = "localhost"
	}
	if err := h.mgr.CreateUser(req.User, req.Password, req.Host, req.Database); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "User created: " + req.User + "@" + req.Host})
}

// DropUser drops a MySQL user
func (h *DatabaseHandler) DropUser(c *gin.Context) {
	user := c.Param("user")
	host := c.DefaultQuery("host", "localhost")
	if user == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Username is required"})
		return
	}
	if err := h.mgr.DropUser(user, host); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "User dropped: " + user})
}

// GetStatus returns the status of MySQL/MariaDB, port 3306, and phpMyAdmin
func (h *DatabaseHandler) GetStatus(c *gin.Context) {
	status := h.mgr.GetServiceStatus()

	// Check if phpMyAdmin already has a tunnel ingress rule
	if status.PhpMyAdminInstalled {
		var domain string
		database.DB().QueryRow(
			"SELECT domain FROM tunnel_ingress_rules WHERE app_type = 'custom' AND target LIKE '%:%' AND domain LIKE '%pma%' OR domain LIKE '%phpmyadmin%' LIMIT 1",
		).Scan(&domain)
		if domain == "" {
			// Broader search: check for our phpmyadmin port in targets
			if status.PhpMyAdminPort > 0 {
				target := fmt.Sprintf("http://localhost:%d", status.PhpMyAdminPort)
				database.DB().QueryRow(
					"SELECT domain FROM tunnel_ingress_rules WHERE target = ? LIMIT 1", target,
				).Scan(&domain)
			}
		}
		status.PhpMyAdminTunneled = domain != ""
		status.PhpMyAdminDomain = domain
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

// SetupPhpMyAdminTunnel provisions phpMyAdmin access using the exact same flow as Sites:
//   1. EnsurePhpMyAdminServed → portmanager port + nginx in sites-enabled/
//   2. AddIngressRule(domain, port) → Cloudflare DNS + cloudflared YAML + restart
func (h *DatabaseHandler) SetupPhpMyAdminTunnel(c *gin.Context) {
	var req struct {
		Domain string `json:"domain"` // e.g. pma.hxdev.in
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Domain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "domain is required (e.g. pma.yourdomain.com)"})
		return
	}

	log.Printf("[pma-tunnel] Step 1: EnsurePhpMyAdminServed (domain=%s)", req.Domain)
	result, err := dbmanager.EnsurePhpMyAdminServed()
	if err != nil {
		log.Printf("[pma-tunnel] Step 1 FAILED: %v", err)
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "nginx setup failed: " + err.Error()})
		return
	}
	log.Printf("[pma-tunnel] Step 1 OK: port=%d created=%v", result.Port, result.Created)

	log.Printf("[pma-tunnel] Step 2: AddIngressRule (tunnelMgr nil=%v)", h.tunnelMgr == nil)
	if h.tunnelMgr == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "tunnel manager not available — ensure Cloudflare is configured first"})
		return
	}
	if err := h.tunnelMgr.AddIngressRule(req.Domain, result.Port, "custom", 0); err != nil {
		log.Printf("[pma-tunnel] Step 2 FAILED: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   fmt.Sprintf("phpMyAdmin is serving on port %d but tunnel wiring failed: %s", result.Port, err.Error()),
		})
		return
	}
	log.Printf("[pma-tunnel] Step 2 OK: ingress rule added")


	action := "already running"
	if result.Created {
		action = "set up fresh"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("✅ phpMyAdmin %s on port %d — now accessible at https://%s", action, result.Port, req.Domain),
		"data": gin.H{
			"port":   result.Port,
			"domain": req.Domain,
			"url":    "https://" + req.Domain,
		},
	})
}


