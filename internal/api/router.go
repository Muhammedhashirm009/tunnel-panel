package api

import (
	"html/template"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/api/handlers"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/config"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/sites"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/tunnel"
	"github.com/Muhammedhashirm009/tunnel-panel/web"
)

// SetupRouter configures all routes and returns the Gin engine
func SetupRouter(cfg *config.Config, tunnelMgr *tunnel.Manager) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// --- Load HTML templates from embedded FS ---
	tmpl := template.Must(template.New("").ParseFS(web.FS, "templates/pages/*.html", "templates/partials/*.html"))
	r.SetHTMLTemplate(tmpl)

	// --- Serve static files from embedded FS ---
	staticFS, _ := fs.Sub(web.FS, "static")
	r.StaticFS("/static", http.FS(staticFS))

	// --- Setup check middleware ---
	r.Use(SetupCheckMiddleware())
	r.Use(RateLimitMiddleware())

	// --- Handlers ---
	authHandler := handlers.NewAuthHandler(cfg, tunnelMgr)
	dashHandler := handlers.NewDashboardHandler()
	tunnelHandler := handlers.NewTunnelHandler(cfg, tunnelMgr)
	fileHandler := handlers.NewFileManagerHandler()
	siteMgr := sites.NewManager(tunnelMgr)
	sitesHandler := handlers.NewSitesHandler(siteMgr)

	// --- Public routes ---
	r.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", gin.H{"Title": "Login — TunnelPanel"})
	})

	r.GET("/setup", func(c *gin.Context) {
		c.HTML(http.StatusOK, "setup.html", gin.H{"Title": "Setup — TunnelPanel"})
	})

	// --- Public API ---
	publicAPI := r.Group("/api")
	{
		publicAPI.POST("/auth/login", authHandler.Login)
		publicAPI.POST("/setup", authHandler.Setup)
	}

	// --- Protected routes ---
	protected := r.Group("/")
	protected.Use(AuthMiddleware(cfg))
	{
		// Pages
		protected.GET("/", func(c *gin.Context) {
			c.Redirect(http.StatusFound, "/dashboard")
		})

		protected.GET("/dashboard", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "dashboard.html", gin.H{
				"Title": "Dashboard — TunnelPanel", "Active": "dashboard", "Username": username,
			})
		})

		protected.GET("/tunnels", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "tunnels.html", gin.H{
				"Title": "Tunnel Manager — TunnelPanel", "Active": "tunnels", "Username": username,
			})
		})

		protected.GET("/sites", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "sites.html", gin.H{
				"Title": "Sites — TunnelPanel", "Active": "sites", "Username": username,
			})
		})

		protected.GET("/docker", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "docker.html", gin.H{
				"Title": "Docker — TunnelPanel", "Active": "docker", "Username": username,
			})
		})

		protected.GET("/files", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "filemanager.html", gin.H{
				"Title": "File Manager — TunnelPanel", "Active": "files", "Username": username,
			})
		})

		protected.GET("/databases", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "databases.html", gin.H{
				"Title": "Databases — TunnelPanel", "Active": "databases", "Username": username,
			})
		})

		protected.GET("/terminal", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "terminal.html", gin.H{
				"Title": "Terminal — TunnelPanel", "Active": "terminal", "Username": username,
			})
		})

		protected.GET("/settings", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "settings.html", gin.H{
				"Title": "Settings — TunnelPanel", "Active": "settings", "Username": username,
			})
		})
	}

	// --- Protected API ---
	protectedAPI := r.Group("/api")
	protectedAPI.Use(AuthMiddleware(cfg))
	{
		// Auth
		protectedAPI.POST("/auth/logout", authHandler.Logout)
		protectedAPI.GET("/auth/me", authHandler.Me)

		// Dashboard
		protectedAPI.GET("/dashboard/stats", dashHandler.GetStats)
		protectedAPI.GET("/dashboard/services", dashHandler.GetServices)
		protectedAPI.POST("/services/:name/:action", dashHandler.ControlService)

		// Tunnels
		protectedAPI.GET("/tunnels/status", tunnelHandler.GetStatus)
		protectedAPI.GET("/tunnels/ingress", tunnelHandler.GetIngressRules)
		protectedAPI.POST("/tunnels/ingress", tunnelHandler.AddIngressRule)
		protectedAPI.DELETE("/tunnels/ingress/:domain", tunnelHandler.RemoveIngressRule)
		protectedAPI.GET("/tunnels/cloudflare", tunnelHandler.GetCloudflareConfig)
		protectedAPI.PUT("/tunnels/cloudflare", tunnelHandler.UpdateCloudflareConfig)
		protectedAPI.GET("/tunnels/zones", tunnelHandler.ListZones)
		protectedAPI.POST("/tunnels/setup", tunnelHandler.SetupTunnels)

		// File Manager
		protectedAPI.GET("/files/browse", fileHandler.Browse)
		protectedAPI.GET("/files/read", fileHandler.ReadFile)
		protectedAPI.POST("/files/write", fileHandler.WriteFile)
		protectedAPI.POST("/files/create", fileHandler.CreateFile)
		protectedAPI.POST("/files/rename", fileHandler.Rename)
		protectedAPI.POST("/files/move", fileHandler.Move)
		protectedAPI.POST("/files/copy", fileHandler.CopyFiles)
		protectedAPI.POST("/files/delete", fileHandler.Delete)
		protectedAPI.POST("/files/chmod", fileHandler.Chmod)
		protectedAPI.GET("/files/search", fileHandler.Search)
		protectedAPI.POST("/files/upload", fileHandler.Upload)
		protectedAPI.GET("/files/download", fileHandler.Download)

		// Sites
		protectedAPI.GET("/sites", sitesHandler.List)
		protectedAPI.GET("/sites/:id", sitesHandler.Get)
		protectedAPI.POST("/sites", sitesHandler.Create)
		protectedAPI.DELETE("/sites/:id", sitesHandler.Delete)
		protectedAPI.PUT("/sites/:id/php", sitesHandler.UpdatePHP)
		protectedAPI.GET("/sites/php-versions", sitesHandler.GetPHPVersions)

		// Port Management
		portHandler := handlers.NewPortHandler()
		protectedAPI.GET("/ports", portHandler.List)
		protectedAPI.DELETE("/ports/:port", portHandler.Release)

		// Docker Management
		dockerHandler := handlers.NewDockerHandler(tunnelMgr)
		protectedAPI.GET("/docker/containers", dockerHandler.ListContainers)
		protectedAPI.POST("/docker/containers", dockerHandler.CreateContainer)
		protectedAPI.POST("/docker/containers/:id/:action", dockerHandler.ContainerAction)
		protectedAPI.GET("/docker/containers/:id/logs", dockerHandler.GetContainerLogs)
		protectedAPI.GET("/docker/images", dockerHandler.ListImages)
		protectedAPI.POST("/docker/images/pull", dockerHandler.PullImage)
		protectedAPI.POST("/docker/deploy", dockerHandler.DeployFromRepo)
		protectedAPI.GET("/docker/deploy/:id/status", dockerHandler.GetDeployStatus)

		// Web Terminal
		terminalHandler := handlers.NewTerminalHandler()
		protectedAPI.GET("/terminal/ws", terminalHandler.WebSocket)
        
		// Database Management
		dbHandler := handlers.NewDatabaseHandler(tunnelMgr)
		protectedAPI.GET("/databases", dbHandler.ListDatabases)
		protectedAPI.POST("/databases", dbHandler.CreateDatabase)
		protectedAPI.DELETE("/databases/:id", dbHandler.DeleteDatabase)
	}

	return r
}

