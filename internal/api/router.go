package api

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tunnelpanel/tunnelpanel/internal/api/handlers"
	"github.com/tunnelpanel/tunnelpanel/internal/config"
	"github.com/tunnelpanel/tunnelpanel/internal/tunnel"
)

// SetupRouter configures all routes and returns the Gin engine
func SetupRouter(cfg *config.Config, tunnelMgr *tunnel.Manager, webFS embed.FS) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// --- Load HTML templates from embedded FS ---
	tmpl := template.Must(template.New("").ParseFS(webFS, "web/templates/**/*.html"))
	r.SetHTMLTemplate(tmpl)

	// --- Serve static files from embedded FS ---
	staticFS, _ := fs.Sub(webFS, "web/static")
	r.StaticFS("/static", http.FS(staticFS))

	// --- Setup check middleware (redirect to /setup if not configured) ---
	r.Use(SetupCheckMiddleware())
	r.Use(RateLimitMiddleware())

	// --- Handlers ---
	authHandler := handlers.NewAuthHandler(cfg)
	dashHandler := handlers.NewDashboardHandler()
	tunnelHandler := handlers.NewTunnelHandler(cfg, tunnelMgr)

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
				"Title":    "Dashboard — TunnelPanel",
				"Active":   "dashboard",
				"Username": username,
			})
		})

		protected.GET("/tunnels", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "tunnels.html", gin.H{
				"Title":    "Tunnel Manager — TunnelPanel",
				"Active":   "tunnels",
				"Username": username,
			})
		})

		protected.GET("/sites", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "sites.html", gin.H{
				"Title":    "Sites — TunnelPanel",
				"Active":   "sites",
				"Username": username,
			})
		})

		protected.GET("/docker", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "docker.html", gin.H{
				"Title":    "Docker — TunnelPanel",
				"Active":   "docker",
				"Username": username,
			})
		})

		protected.GET("/files", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "filemanager.html", gin.H{
				"Title":    "File Manager — TunnelPanel",
				"Active":   "files",
				"Username": username,
			})
		})

		protected.GET("/databases", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "databases.html", gin.H{
				"Title":    "Databases — TunnelPanel",
				"Active":   "databases",
				"Username": username,
			})
		})

		protected.GET("/terminal", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "terminal.html", gin.H{
				"Title":    "Terminal — TunnelPanel",
				"Active":   "terminal",
				"Username": username,
			})
		})

		protected.GET("/settings", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.HTML(http.StatusOK, "settings.html", gin.H{
				"Title":    "Settings — TunnelPanel",
				"Active":   "settings",
				"Username": username,
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
	}

	return r
}
