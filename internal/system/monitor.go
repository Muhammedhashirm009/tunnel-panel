package system

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/load"
)

// SystemStats holds complete system information
type SystemStats struct {
	Hostname    string       `json:"hostname"`
	OS          string       `json:"os"`
	Platform    string       `json:"platform"`
	Kernel      string       `json:"kernel"`
	Uptime      string       `json:"uptime"`
	UptimeSec   uint64       `json:"uptime_sec"`
	CPU         CPUStats     `json:"cpu"`
	Memory      MemoryStats  `json:"memory"`
	Disk        []DiskStats  `json:"disk"`
	Network     NetworkStats `json:"network"`
	LoadAverage LoadStats    `json:"load_average"`
}

// CPUStats holds CPU information
type CPUStats struct {
	Model       string    `json:"model"`
	Cores       int       `json:"cores"`
	Threads     int       `json:"threads"`
	UsagePerCPU []float64 `json:"usage_per_cpu"`
	UsageTotal  float64   `json:"usage_total"`
}

// MemoryStats holds RAM information
type MemoryStats struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	Cached      uint64  `json:"cached"`
	UsedPercent float64 `json:"used_percent"`
	SwapTotal   uint64  `json:"swap_total"`
	SwapUsed    uint64  `json:"swap_used"`
}

// DiskStats holds disk partition information
type DiskStats struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	Fstype      string  `json:"fstype"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
}

// NetworkStats holds network I/O information
type NetworkStats struct {
	BytesSent   uint64 `json:"bytes_sent"`
	BytesRecv   uint64 `json:"bytes_recv"`
	PacketsSent uint64 `json:"packets_sent"`
	PacketsRecv uint64 `json:"packets_recv"`
}

// LoadStats holds system load averages
type LoadStats struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// GetSystemStats collects all system statistics
func GetSystemStats() (*SystemStats, error) {
	stats := &SystemStats{}

	// Host info
	hostInfo, err := host.Info()
	if err == nil {
		stats.Hostname = hostInfo.Hostname
		stats.OS = hostInfo.OS
		stats.Platform = fmt.Sprintf("%s %s", hostInfo.Platform, hostInfo.PlatformVersion)
		stats.Kernel = hostInfo.KernelVersion
		stats.UptimeSec = hostInfo.Uptime
		stats.Uptime = formatUptime(hostInfo.Uptime)
	}

	// CPU info
	cpuInfo, err := cpu.Info()
	if err == nil && len(cpuInfo) > 0 {
		stats.CPU.Model = cpuInfo[0].ModelName
		stats.CPU.Cores = int(cpuInfo[0].Cores)
	}
	stats.CPU.Threads = runtime.NumCPU()

	cpuPercent, err := cpu.Percent(time.Second, true)
	if err == nil {
		stats.CPU.UsagePerCPU = cpuPercent
		total := 0.0
		for _, p := range cpuPercent {
			total += p
		}
		if len(cpuPercent) > 0 {
			stats.CPU.UsageTotal = total / float64(len(cpuPercent))
		}
	}

	// Memory info
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		stats.Memory.Total = memInfo.Total
		stats.Memory.Used = memInfo.Used
		stats.Memory.Free = memInfo.Free
		stats.Memory.Cached = memInfo.Cached
		stats.Memory.UsedPercent = memInfo.UsedPercent
	}

	swapInfo, err := mem.SwapMemory()
	if err == nil {
		stats.Memory.SwapTotal = swapInfo.Total
		stats.Memory.SwapUsed = swapInfo.Used
	}

	// Disk info
	partitions, err := disk.Partitions(false)
	if err == nil {
		for _, p := range partitions {
			usage, err := disk.Usage(p.Mountpoint)
			if err != nil {
				continue
			}
			stats.Disk = append(stats.Disk, DiskStats{
				Device:      p.Device,
				Mountpoint:  p.Mountpoint,
				Fstype:      p.Fstype,
				Total:       usage.Total,
				Used:        usage.Used,
				Free:        usage.Free,
				UsedPercent: usage.UsedPercent,
			})
		}
	}

	// Network info
	netIO, err := net.IOCounters(false)
	if err == nil && len(netIO) > 0 {
		stats.Network.BytesSent = netIO[0].BytesSent
		stats.Network.BytesRecv = netIO[0].BytesRecv
		stats.Network.PacketsSent = netIO[0].PacketsSent
		stats.Network.PacketsRecv = netIO[0].PacketsRecv
	}

	// Load average
	loadAvg, err := load.Avg()
	if err == nil {
		stats.LoadAverage.Load1 = loadAvg.Load1
		stats.LoadAverage.Load5 = loadAvg.Load5
		stats.LoadAverage.Load15 = loadAvg.Load15
	}

	return stats, nil
}

func formatUptime(sec uint64) string {
	days := sec / 86400
	hours := (sec % 86400) / 3600
	mins := (sec % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// ServiceInfo represents a system service
type ServiceInfo struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Running bool   `json:"running"`
	Enabled bool   `json:"enabled"`
}

// GetServiceStatus checks the status of a systemd service
func GetServiceStatus(name string) (*ServiceInfo, error) {
	info := &ServiceInfo{Name: name}

	// Check if active
	out, err := exec.Command("systemctl", "is-active", name).Output()
	status := strings.TrimSpace(string(out))
	info.Status = status
	info.Running = (err == nil && status == "active")

	// Check if enabled
	out, _ = exec.Command("systemctl", "is-enabled", name).Output()
	info.Enabled = strings.TrimSpace(string(out)) == "enabled"

	return info, nil
}

// ControlService starts, stops, or restarts a system service
func ControlService(name, action string) error {
	validActions := map[string]bool{"start": true, "stop": true, "restart": true, "reload": true}
	if !validActions[action] {
		return fmt.Errorf("invalid action: %s", action)
	}

	cmd := exec.Command("systemctl", action, name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to %s %s: %s", action, name, string(out))
	}

	return nil
}

// GetAllServicesStatus gets status of all managed services
func GetAllServicesStatus() []ServiceInfo {
	services := []string{
		"nginx",
		"mysql", "mariadb",
		"docker",
		"portix",
		"tunnelpanel-tunnel",
		"php8.2-fpm", "php8.1-fpm", "php8.0-fpm", "php7.4-fpm",
		"redis-server",
	}

	var result []ServiceInfo
	for _, svc := range services {
		info, err := GetServiceStatus(svc)
		if err == nil {
			result = append(result, *info)
		}
	}
	return result
}
