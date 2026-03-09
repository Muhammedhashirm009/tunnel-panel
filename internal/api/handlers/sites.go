package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/Muhammedhashirm009/portix/internal/httputil"
	"github.com/Muhammedhashirm009/portix/internal/sites"
)

// SitesHandler handles site management endpoints
type SitesHandler struct {
	manager *sites.Manager
}

// NewSitesHandler creates a new handler
func NewSitesHandler(mgr *sites.Manager) *SitesHandler {
	return &SitesHandler{manager: mgr}
}

// List handles GET /api/sites
func (h *SitesHandler) List(c *gin.Context) {
	siteList, err := h.manager.ListSites()
	if err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, siteList)
}

// Get handles GET /api/sites/:id
func (h *SitesHandler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		httputil.Error(c, 400, "invalid site ID")
		return
	}

	site, err := h.manager.GetSite(id)
	if err != nil {
		httputil.Error(c, 404, err.Error())
		return
	}
	httputil.Success(c, site)
}

// CreateSiteRequest represents the create site form
type CreateSiteRequest struct {
	Name       string `json:"name" binding:"required"`
	Domain     string `json:"domain" binding:"required"`
	PHPVersion string `json:"php_version"`
}

// Create handles POST /api/sites
func (h *SitesHandler) Create(c *gin.Context) {
	var req CreateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "name and domain are required")
		return
	}

	site, err := h.manager.CreateSite(req.Name, req.Domain, req.PHPVersion)
	if err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}

	httputil.Created(c, site)
}

// Delete handles DELETE /api/sites/:id
func (h *SitesHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		httputil.Error(c, 400, "invalid site ID")
		return
	}

	if err := h.manager.DeleteSite(id); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, gin.H{"message": "site deleted"})
}

// UpdatePHP handles PUT /api/sites/:id/php
func (h *SitesHandler) UpdatePHP(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		httputil.Error(c, 400, "invalid site ID")
		return
	}

	var req struct {
		Version string `json:"version" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, 400, "version is required")
		return
	}

	if err := h.manager.UpdatePHPVersion(id, req.Version); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, gin.H{"message": "PHP version updated"})
}

// GetPHPVersions handles GET /api/sites/php-versions
func (h *SitesHandler) GetPHPVersions(c *gin.Context) {
	versions := sites.GetInstalledPHP()
	httputil.Success(c, versions)
}
