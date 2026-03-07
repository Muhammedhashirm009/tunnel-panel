package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/httputil"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/portmanager"
)

// PortHandler handles port management endpoints
type PortHandler struct{}

// NewPortHandler creates a new handler
func NewPortHandler() *PortHandler {
	return &PortHandler{}
}

// List handles GET /api/ports — returns all allocated ports
func (h *PortHandler) List(c *gin.Context) {
	pm := portmanager.Get()
	ports, err := pm.GetAll()
	if err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}

	total, used, available := pm.GetStats()
	min, max := pm.GetRange()

	httputil.Success(c, gin.H{
		"ports": ports,
		"stats": gin.H{
			"total":     total,
			"used":      used,
			"available": available,
			"min_port":  min,
			"max_port":  max,
		},
	})
}

// Release handles DELETE /api/ports/:port — frees a port
func (h *PortHandler) Release(c *gin.Context) {
	var port int
	if _, err := fmt.Sscanf(c.Param("port"), "%d", &port); err != nil {
		httputil.Error(c, 400, "invalid port number")
		return
	}

	if err := portmanager.Get().Release(port); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}

	httputil.Success(c, gin.H{"message": "port released", "port": port})
}
