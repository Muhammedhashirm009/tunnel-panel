package tunnel

import (
	"fmt"
	"sync"

	"github.com/Muhammedhashirm009/portix/internal/database"
)

// PortAllocator manages port assignment for hosted apps
type PortAllocator struct {
	mu       sync.Mutex
	minPort  int
	maxPort  int
}

// NewPortAllocator creates a new port allocator with the given range
func NewPortAllocator(min, max int) *PortAllocator {
	return &PortAllocator{
		minPort: min,
		maxPort: max,
	}
}

// Allocate finds and reserves the next available port
func (pa *PortAllocator) Allocate(appType string, appID int) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

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

	// Find next available port
	for port := pa.minPort; port <= pa.maxPort; port++ {
		if !usedPorts[port] {
			// Reserve this port
			_, err := database.DB().Exec(
				"INSERT INTO ports (port, app_type, app_id) VALUES (?, ?, ?)",
				port, appType, appID,
			)
			if err != nil {
				return 0, fmt.Errorf("failed to allocate port %d: %w", port, err)
			}
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available ports in range %d-%d", pa.minPort, pa.maxPort)
}

// Release frees a previously allocated port
func (pa *PortAllocator) Release(port int) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	_, err := database.DB().Exec("DELETE FROM ports WHERE port = ?", port)
	return err
}

// GetUsedPorts returns all currently allocated ports
func (pa *PortAllocator) GetUsedPorts() ([]PortAllocation, error) {
	rows, err := database.DB().Query(
		"SELECT port, app_type, app_id, allocated_at FROM ports ORDER BY port ASC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ports []PortAllocation
	for rows.Next() {
		var p PortAllocation
		if err := rows.Scan(&p.Port, &p.AppType, &p.AppID, &p.AllocatedAt); err != nil {
			continue
		}
		ports = append(ports, p)
	}
	return ports, nil
}

// PortAllocation represents an allocated port
type PortAllocation struct {
	Port        int    `json:"port"`
	AppType     string `json:"app_type"`
	AppID       int    `json:"app_id"`
	AllocatedAt string `json:"allocated_at"`
}

// GetStats returns port allocation statistics
func (pa *PortAllocator) GetStats() (total, used, available int) {
	total = pa.maxPort - pa.minPort + 1
	var count int
	database.DB().QueryRow("SELECT COUNT(*) FROM ports").Scan(&count)
	used = count
	available = total - used
	return
}
