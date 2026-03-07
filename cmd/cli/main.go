package main

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

const (
	dbPath      = "/etc/tunnelpanel/panel.db"
	serviceName = "tunnelpanel"
	version     = "1.0.0"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tunnelpanel-cli",
		Short: "TunnelPanel CLI — Manage your server panel from the terminal",
		Long: `
╔══════════════════════════════════════════╗
║       🚀 TunnelPanel CLI v` + version + `          ║
║    Server Control Panel Management       ║
╚══════════════════════════════════════════╝`,
		Run: func(cmd *cobra.Command, args []string) {
			showInteractiveMenu()
		},
	}

	// Status command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show panel service status",
		Run:   cmdStatus,
	})

	// Start command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start the panel service",
		Run: func(cmd *cobra.Command, args []string) {
			serviceControl("start")
		},
	})

	// Stop command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop the panel service",
		Run: func(cmd *cobra.Command, args []string) {
			serviceControl("stop")
		},
	})

	// Restart command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "Restart the panel service",
		Run: func(cmd *cobra.Command, args []string) {
			serviceControl("restart")
		},
	})

	// Info command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Show panel access information",
		Run:   cmdInfo,
	})

	// Password commands
	passwordCmd := &cobra.Command{
		Use:   "password",
		Short: "Password management",
	}

	passwordCmd.AddCommand(&cobra.Command{
		Use:   "reset",
		Short: "Reset admin password",
		Run:   cmdPasswordReset,
	})

	passwordCmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show admin username",
		Run:   cmdPasswordShow,
	})

	rootCmd.AddCommand(passwordCmd)

	// Tunnel commands
	tunnelCmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Tunnel management",
	}

	tunnelCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show tunnel status",
		Run:   cmdTunnelStatus,
	})

	tunnelCmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "Restart all tunnels",
		Run:   cmdTunnelRestart,
	})

	rootCmd.AddCommand(tunnelCmd)

	// Logs command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "logs",
		Short: "View panel logs",
		Run:   cmdLogs,
	})

	// Update command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Update TunnelPanel to latest version",
		Run:   cmdUpdate,
	})

	// Version
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("TunnelPanel CLI v%s\n", version)
		},
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func showInteractiveMenu() {
	fmt.Println(`
╔══════════════════════════════════════════╗
║       🚀 TunnelPanel CLI v` + version + `          ║
╚══════════════════════════════════════════╝

  1) Start panel
  2) Stop panel
  3) Restart panel
  4) Show panel status
  5) Show panel info
  6) Reset admin password
  7) Show tunnel status
  8) Restart tunnels
  9) View logs
  0) Exit
`)
	fmt.Print("Enter option: ")
	var choice string
	fmt.Scanln(&choice)

	switch choice {
	case "1":
		serviceControl("start")
	case "2":
		serviceControl("stop")
	case "3":
		serviceControl("restart")
	case "4":
		cmdStatus(nil, nil)
	case "5":
		cmdInfo(nil, nil)
	case "6":
		cmdPasswordReset(nil, nil)
	case "7":
		cmdTunnelStatus(nil, nil)
	case "8":
		cmdTunnelRestart(nil, nil)
	case "9":
		cmdLogs(nil, nil)
	case "0":
		fmt.Println("Goodbye!")
	default:
		fmt.Println("Invalid option")
	}
}

func cmdStatus(cmd *cobra.Command, args []string) {
	fmt.Println("\n📊 Service Status:")
	fmt.Println("─────────────────────────────────")
	showServiceStatus(serviceName, "TunnelPanel Server")
	showServiceStatus("tunnelpanel-panel-tunnel", "Panel Tunnel (#1)")
	showServiceStatus("tunnelpanel-apps-tunnel", "Apps Tunnel (#2)")
	showServiceStatus("nginx", "Nginx")
	showServiceStatus("docker", "Docker")
	showServiceStatus("mysql", "MySQL")
	fmt.Println()
}

func showServiceStatus(name, label string) {
	out, err := exec.Command("systemctl", "is-active", name).Output()
	status := strings.TrimSpace(string(out))
	if err != nil {
		status = "inactive"
	}

	icon := "🔴"
	if status == "active" {
		icon = "🟢"
	}
	fmt.Printf("  %s %-25s %s\n", icon, label, status)
}

func serviceControl(action string) {
	fmt.Printf("\n%s %s...\n", strings.Title(action)+"ing", serviceName)

	cmd := exec.Command("systemctl", action, serviceName)
	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ Failed to %s: %v\n", action, err)
		return
	}
	fmt.Printf("✅ %s %s successful\n", serviceName, action)
}

func cmdInfo(cmd *cobra.Command, args []string) {
	fmt.Println("\n📋 TunnelPanel Info:")
	fmt.Println("─────────────────────────────────")
	fmt.Printf("  Version:    %s\n", version)
	fmt.Printf("  Config:     /etc/tunnelpanel/config.json\n")
	fmt.Printf("  Database:   %s\n", dbPath)
	fmt.Printf("  Logs:       /var/log/tunnelpanel/\n")

	// Read panel domain from DB
	db, err := openDB()
	if err != nil {
		fmt.Printf("  Panel URL:  Unable to read config\n")
		return
	}
	defer db.Close()

	var panelDomain string
	db.QueryRow("SELECT panel_domain FROM cloudflare_config WHERE id = 1").Scan(&panelDomain)
	if panelDomain != "" {
		fmt.Printf("  Panel URL:  https://%s\n", panelDomain)
	} else {
		fmt.Printf("  Panel URL:  https://localhost:8443 (tunnel not configured)\n")
	}

	var username string
	db.QueryRow("SELECT username FROM users LIMIT 1").Scan(&username)
	if username != "" {
		fmt.Printf("  Admin:      %s\n", username)
	}
	fmt.Println()
}

func cmdPasswordReset(cmd *cobra.Command, args []string) {
	db, err := openDB()
	if err != nil {
		fmt.Printf("❌ Database error: %v\n", err)
		return
	}
	defer db.Close()

	// Generate random password
	newPass := generatePassword(16)
	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	// Update first admin user
	result, err := db.Exec("UPDATE users SET password_hash = ? WHERE id = (SELECT id FROM users LIMIT 1)", string(hash))
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		fmt.Println("❌ No admin user found. Run setup first.")
		return
	}

	var username string
	db.QueryRow("SELECT username FROM users LIMIT 1").Scan(&username)

	fmt.Println("\n✅ Password reset successful!")
	fmt.Println("─────────────────────────────────")
	fmt.Printf("  Username:  %s\n", username)
	fmt.Printf("  Password:  %s\n", newPass)
	fmt.Println("─────────────────────────────────")
	fmt.Println("  ⚠️  Save this password, it won't be shown again!")
	fmt.Println()
}

func cmdPasswordShow(cmd *cobra.Command, args []string) {
	db, err := openDB()
	if err != nil {
		fmt.Printf("❌ Database error: %v\n", err)
		return
	}
	defer db.Close()

	var username string
	db.QueryRow("SELECT username FROM users LIMIT 1").Scan(&username)
	if username == "" {
		fmt.Println("❌ No admin user found")
		return
	}
	fmt.Printf("\n  Admin username: %s\n", username)
	fmt.Println("  (use 'tunnelpanel-cli password reset' to reset password)")
	fmt.Println()
}

func cmdTunnelStatus(cmd *cobra.Command, args []string) {
	fmt.Println("\n🔗 Tunnel Status:")
	fmt.Println("─────────────────────────────────")
	showServiceStatus("tunnelpanel-panel-tunnel", "Panel Tunnel (#1)")
	showServiceStatus("tunnelpanel-apps-tunnel", "Apps Tunnel (#2)")
	fmt.Println()
}

func cmdTunnelRestart(cmd *cobra.Command, args []string) {
	fmt.Println("\n↻ Restarting tunnels...")
	exec.Command("systemctl", "restart", "tunnelpanel-panel-tunnel").Run()
	exec.Command("systemctl", "restart", "tunnelpanel-apps-tunnel").Run()
	time.Sleep(2 * time.Second)
	cmdTunnelStatus(nil, nil)
}

func cmdLogs(cmd *cobra.Command, args []string) {
	fmt.Println("\n📋 Last 50 log lines:")
	fmt.Println("─────────────────────────────────")
	out, _ := exec.Command("journalctl", "-u", serviceName, "--no-pager", "-n", "50").CombinedOutput()
	fmt.Println(string(out))
}

func cmdUpdate(cmd *cobra.Command, args []string) {
	fmt.Println("\n🔄 Update functionality coming in a future release.")
	fmt.Println("   For now, manually download the latest binary and replace /usr/local/bin/tunnelpanel")
	fmt.Println()
}

func openDB() (*sql.DB, error) {
	return sql.Open("sqlite3", dbPath)
}

func generatePassword(length int) string {
	const charset = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	pw := make([]byte, length)
	for i := range pw {
		pw[i] = charset[r.Intn(len(charset))]
	}
	return string(pw)
}
