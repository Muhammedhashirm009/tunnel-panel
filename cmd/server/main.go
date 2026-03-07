package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/tunnelpanel/tunnelpanel/internal/api"
	"github.com/tunnelpanel/tunnelpanel/internal/config"
	"github.com/tunnelpanel/tunnelpanel/internal/database"
	"github.com/tunnelpanel/tunnelpanel/internal/tunnel"
)

//go:embed web/*
var webFS embed.FS

func main() {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║         🚀 TunnelPanel v1.0          ║")
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

	// Initialize tunnel manager
	var tunnelMgr *tunnel.Manager
	if cfg.CloudflareAPIToken != "" {
		cf := tunnel.NewCloudflareClient(
			cfg.CloudflareAPIToken,
			cfg.CloudflareAccountID,
			cfg.CloudflareZoneID,
			cfg.CloudflareZoneName,
		)
		pa := tunnel.NewPortAllocator(cfg.PortRangeMin, cfg.PortRangeMax)
		tunnelMgr = tunnel.NewManager(cf, pa, cfg.DataDir, cfg.AppsTunnelID, "")
		log.Println("Tunnel manager initialized")
	} else {
		// Create a stub manager (no Cloudflare configured yet)
		pa := tunnel.NewPortAllocator(cfg.PortRangeMin, cfg.PortRangeMax)
		tunnelMgr = tunnel.NewManager(nil, pa, cfg.DataDir, "", "")
		log.Println("Tunnel manager initialized (Cloudflare not configured)")
	}

	// Setup router
	router := api.SetupRouter(cfg, tunnelMgr, webFS)

	// Ensure log directory exists
	os.MkdirAll(cfg.LogDir, 0755)

	// Start server
	listenAddr := cfg.GetListenAddr()
	log.Printf("TunnelPanel server starting on %s", listenAddr)

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

	log.Println("Shutting down TunnelPanel...")
	database.Close()
}
