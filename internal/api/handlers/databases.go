package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/Muhammedhashirm009/tunnel-panel/internal/database"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/tunnel"
	"github.com/gin-gonic/gin"
)

// DatabaseHandler handles database management API
type DatabaseHandler struct {
	manager   *database.Manager
	tunnelMgr *tunnel.Manager
}

// NewDatabaseHandler creates a new database HTTP handler
func NewDatabaseHandler(tunnelMgr *tunnel.Manager) *DatabaseHandler {
	return &DatabaseHandler{
		manager:   database.NewManager(),
		tunnelMgr: tunnelMgr,
	}
}

// ListDatabases returns all managed databases
func (h *DatabaseHandler) ListDatabases(c *gin.Context) {
	dbs, err := h.manager.ListDatabases()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": dbs})
}

// CreateDatabase provisions a new DB and phpMyAdmin
func (h *DatabaseHandler) CreateDatabase(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		Type         string `json:"type" binding:"required"`
		RootPassword string `json:"root_password" binding:"required"`
		User         string `json:"user" binding:"required"`
		UserPassword string `json:"user_password" binding:"required"`
		PMADomain    string `json:"pma_domain" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request: missing required fields"})
		return
	}

	dbRec, pmaPort, err := h.manager.ProvisionDatabase(
		req.Name, req.Type, req.RootPassword, req.User, req.UserPassword, req.PMADomain,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Provision failed: " + err.Error()})
		return
	}

	// Setup tunnel ingress for phpmyadmin
	if h.tunnelMgr != nil {
		err := h.tunnelMgr.AddIngressRule(req.PMADomain, pmaPort, "db", dbRec.ID)
		if err != nil {
			log.Printf("[database] tunnel error for %s: %v", req.PMADomain, err)
			c.JSON(http.StatusOK, gin.H{
				"success": true, 
				"data": dbRec, 
				"warning": "Database created, but tunnel failed: " + err.Error(),
			})
			return
		}
	} else {
		log.Printf("[database] local tunnelMgr not found, skipping tunnel ingress.")
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": dbRec})
}

// DeleteDatabase removes a database and its tunnel
func (h *DatabaseHandler) DeleteDatabase(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid ID"})
		return
	}

	dbRec, err := h.manager.GetDatabase(id)
	if err == nil && dbRec.PmaDomain != "" && h.tunnelMgr != nil {
		_ = h.tunnelMgr.RemoveIngressRule(dbRec.PmaDomain)
	}

	if err := h.manager.DeleteDatabase(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
