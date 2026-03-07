package portmanager

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Muhammedhashirm009/tunnel-panel/internal/database"
)

// Default port range for hosted apps
const (
	DefaultMinPort = 8080
	DefaultMaxPort = 9000
)

// PortManager handles port allocation for all app types (sites, docker, custom)
type PortManager struct {
	mu      sync.Mutex
	minPort int
	maxPort int
}

// PortInfo holds information about an allocated port
type PortInfo struct {
	Port        int       `json:"port"`
	AppType     string    `json:"app_type"`
	AppID       int       `json:"app_id"`
	AppName     string    `json:"app_name"`
	AllocatedAt time.Time `json:"allocated_at"`
}

var (
	instance *PortManager
	once     sync.Once
)

// Init creates the global port manager with the given range
func Init(minPort, maxPort int) *PortManager {
	once.Do(func() {
		if minPort <= 0 {
			minPort = DefaultMinPort
		}
		if maxPort <= 0 {
			maxPort = DefaultMaxPort
		}
		instance = &PortManager{
			minPort: minPort,
			maxPort: maxPort,
		}
	})
	return instance
}

// Get returns the global port manager (must call Init first)
func Get() *PortManager {
	if instance == nil {
		return Init(DefaultMinPort, DefaultMaxPort)
	}
	return instance
}

// Allocate finds and reserves the next available port in the range
func (pm *PortManager) Allocate(appType string, appID int, appName string) (int, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Get all used ports from the database
	rows, err := database.DB().Query("SELECT port FROM ports ORDER BY port ASC")
	if err != nil {
		return 0, fmt.Errorf("failed to query ports: %w", err)
	}
	defer rows.Close()

	usedPorts := make(map[int]bool)
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			continue
		}
		usedPorts[port] = true
	}

	// Find next available port that is not in use by another process
	for port := pm.minPort; port <= pm.maxPort; port++ {
		if usedPorts[port] {
			continue
		}

		// Also check if the port is actually free on the system
		if !isPortFree(port) {
			continue
		}

		// Reserve this port in the database
		_, err := database.DB().Exec(
			"INSERT INTO ports (port, app_type, app_id) VALUES (?, ?, ?)",
			port, appType, appID,
		)
		if err != nil {
			// Might be a race condition, try next port
			continue
		}
		return port, nil
	}

	return 0, fmt.Errorf("no available ports in range %d-%d (%d ports in use)",
		pm.minPort, pm.maxPort, len(usedPorts))
}

// Release frees a previously allocated port
func (pm *PortManager) Release(port int) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	_, err := database.DB().Exec("DELETE FROM ports WHERE port = ?", port)
	return err
}

// UpdateAppID updates the app_id for a port (used after DB insert gives us the real ID)
func (pm *PortManager) UpdateAppID(port int, appID int) {
	database.DB().Exec("UPDATE ports SET app_id = ? WHERE port = ?", appID, port)
}

// GetAll returns all currently allocated ports with app info
func (pm *PortManager) GetAll() ([]PortInfo, error) {
	rows, err := database.DB().Query(`
		SELECT p.port, p.app_type, p.app_id, p.allocated_at,
			COALESCE(
				CASE p.app_type
					WHEN 'site' THEN (SELECT name FROM sites WHERE id = p.app_id)
					WHEN 'docker' THEN (SELECT name FROM docker_apps WHERE id = p.app_id)
					ELSE 'custom'
				END, 'unknown'
			) as app_name
		FROM ports p ORDER BY p.port ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ports []PortInfo
	for rows.Next() {
		var p PortInfo
		var allocatedAt string
		if err := rows.Scan(&p.Port, &p.AppType, &p.AppID, &allocatedAt, &p.AppName); err != nil {
			continue
		}
		p.AllocatedAt, _ = time.Parse("2006-01-02 15:04:05", allocatedAt)
		ports = append(ports, p)
	}
	return ports, nil
}

// GetStats returns port allocation statistics
func (pm *PortManager) GetStats() (total, used, available int) {
	total = pm.maxPort - pm.minPort + 1
	var count int
	database.DB().QueryRow("SELECT COUNT(*) FROM ports").Scan(&count)
	used = count
	available = total - used
	return
}

// GetRange returns the configured port range
func (pm *PortManager) GetRange() (min, max int) {
	return pm.minPort, pm.maxPort
}

// isPortFree checks if a port is available on the system
func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
