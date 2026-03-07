package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/tunnelpanel/tunnelpanel/internal/api"
	"github.com/tunnelpanel/tunnelpanel/internal/system"
)

// DashboardHandler handles dashboard endpoints
type DashboardHandler struct{}

// NewDashboardHandler creates a new dashboard handler
func NewDashboardHandler() *DashboardHandler {
	return &DashboardHandler{}
}

// GetStats handles GET /api/dashboard/stats
func (h *DashboardHandler) GetStats(c *gin.Context) {
	stats, err := system.GetSystemStats()
	if err != nil {
		api.Error(c, 500, "failed to get system stats: "+err.Error())
		return
	}

	api.Success(c, stats)
}

// GetServices handles GET /api/dashboard/services
func (h *DashboardHandler) GetServices(c *gin.Context) {
	services := system.GetAllServicesStatus()
	api.Success(c, services)
}

// ControlService handles POST /api/services/:name/:action
func (h *DashboardHandler) ControlService(c *gin.Context) {
	name := c.Param("name")
	action := c.Param("action")

	// Whitelist allowed services
	allowed := map[string]bool{
		"nginx": true, "mysql": true, "mariadb": true,
		"docker": true, "redis-server": true,
		"tunnelpanel": true, "tunnelpanel-panel-tunnel": true,
		"tunnelpanel-apps-tunnel": true,
		"php7.4-fpm": true, "php8.0-fpm": true,
		"php8.1-fpm": true, "php8.2-fpm": true, "php8.3-fpm": true,
	}

	if !allowed[name] {
		api.Error(c, 400, "service not allowed: "+name)
		return
	}

	if err := system.ControlService(name, action); err != nil {
		api.Error(c, 500, err.Error())
		return
	}

	api.Success(c, gin.H{"message": name + " " + action + " successful"})
}
