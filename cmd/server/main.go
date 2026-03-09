package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Muhammedhashirm009/portix/internal/api"
	"github.com/Muhammedhashirm009/portix/internal/config"
	"github.com/Muhammedhashirm009/portix/internal/database"
	"github.com/Muhammedhashirm009/portix/internal/portmanager"
	"github.com/Muhammedhashirm009/portix/internal/tunnel"
)

func main() {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║         🚀 Portix v1.0          ║")
	fmt.Println("║    Server Control Panel + Tunnels     ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Config loaded from %s", cfg.DataDir)

	// Initialize database
	if err := database.Init(cfg.DBPath); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()
	log.Printf("Database initialized at %s", cfg.DBPath)

	// Initialize port manager
	pm := portmanager.Init(cfg.PortRangeMin, cfg.PortRangeMax)
	minP, maxP := pm.GetRange()
	_, used, avail := pm.GetStats()
	log.Printf("Port manager ready: range %d-%d (%d used, %d available)", minP, maxP, used, avail)

	// Initialize tunnel manager — try config first, then DB
	var tunnelMgr *tunnel.Manager

	// Try to load CF config from DB if not in config.json
	cfToken := cfg.CloudflareAPIToken
	cfAccount := cfg.CloudflareAccountID
	cfZoneID := cfg.CloudflareZoneID
	cfZoneName := cfg.CloudflareZoneName
	appsTunnelID := cfg.AppsTunnelID

	if cfToken == "" {
		// Try DB
		database.DB().QueryRow(
			"SELECT COALESCE(api_token,''), COALESCE(account_id,''), COALESCE(zone_id,''), COALESCE(zone_name,''), COALESCE(tunnel_apps_id,'') FROM cloudflare_config WHERE id = 1",
		).Scan(&cfToken, &cfAccount, &cfZoneID, &cfZoneName, &appsTunnelID)
	}

	if cfToken != "" {
		cf := tunnel.NewCloudflareClient(cfToken, cfAccount, cfZoneID, cfZoneName)
		pa := tunnel.NewPortAllocator(cfg.PortRangeMin, cfg.PortRangeMax)
		tunnelMgr = tunnel.NewManager(cf, pa, cfg.DataDir, appsTunnelID, "")
		log.Printf("Tunnel manager initialized (apps tunnel: %s)", appsTunnelID)
	} else {
		// No CF config anywhere — create stub
		pa := tunnel.NewPortAllocator(cfg.PortRangeMin, cfg.PortRangeMax)
		tunnelMgr = tunnel.NewManager(nil, pa, cfg.DataDir, "", "")
		log.Println("Tunnel manager initialized (Cloudflare not configured)")
	}

	// Setup router (web assets are embedded via web.FS inside the router)
	router := api.SetupRouter(cfg, tunnelMgr)

	// Ensure log directory exists
	os.MkdirAll(cfg.LogDir, 0755)

	// Start server
	listenAddr := cfg.GetListenAddr()
	log.Printf("Portix server starting on %s", listenAddr)

	if cfg.AllowDirectAccess {
		log.Println("⚠️  Direct IP access is ENABLED (emergency fallback mode)")
	} else {
		log.Println("🔒 Listening on localhost only — access via Cloudflare Tunnel")
	}

	// Graceful shutdown
	go func() {
		if err := router.Run(listenAddr); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down Portix...")
	database.Close()
}
