package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Note: itoa() helper is in helpers.go

const (
	DefaultPort         = 8443
	DefaultHost         = "127.0.0.1"
	DefaultDataDir      = "/etc/tunnelpanel"
	DefaultDBPath       = "/etc/tunnelpanel/panel.db"
	DefaultLogDir       = "/var/log/tunnelpanel"
	DefaultPortRangeMin = 8080
	DefaultPortRangeMax = 9000
	AppName             = "TunnelPanel"
	Version             = "1.0.0"
)

// Config holds the application configuration
type Config struct {
	mu sync.RWMutex

	// Server settings
	Host string `json:"host"`
	Port int    `json:"port"`

	// Allow direct IP access (emergency fallback)
	AllowDirectAccess bool `json:"allow_direct_access"`

	// Paths
	DataDir string `json:"data_dir"`
	DBPath  string `json:"db_path"`
	LogDir  string `json:"log_dir"`

	// Port allocation range for hosted apps
	PortRangeMin int `json:"port_range_min"`
	PortRangeMax int `json:"port_range_max"`

	// JWT secret (auto-generated on first run)
	JWTSecret string `json:"jwt_secret"`

	// Session expiry in hours
	SessionExpiry int `json:"session_expiry"`

	// Cloudflare
	CloudflareAPIToken string `json:"cloudflare_api_token"`
	CloudflareAccountID string `json:"cloudflare_account_id"`
	CloudflareZoneID   string `json:"cloudflare_zone_id"`
	CloudflareZoneName string `json:"cloudflare_zone_name"`

	// Tunnel IDs
	PanelTunnelID string `json:"panel_tunnel_id"`
	AppsTunnelID  string `json:"apps_tunnel_id"`
	PanelDomain   string `json:"panel_domain"`
}

var (
	globalConfig *Config
	once         sync.Once
)

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Host:            DefaultHost,
		Port:            DefaultPort,
		AllowDirectAccess: false,
		DataDir:         DefaultDataDir,
		DBPath:          DefaultDBPath,
		LogDir:          DefaultLogDir,
		PortRangeMin:    DefaultPortRangeMin,
		PortRangeMax:    DefaultPortRangeMax,
		SessionExpiry:   24,
	}
}

// Load reads configuration from the config file
func Load() (*Config, error) {
	cfg := DefaultConfig()
	configPath := filepath.Join(DefaultDataDir, "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// First run — save defaults
			if err := cfg.Save(); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	globalConfig = cfg
	return cfg, nil
}

// Get returns the global config (call Load first)
func Get() *Config {
	if globalConfig == nil {
		cfg, _ := Load()
		return cfg
	}
	return globalConfig
}

// Save writes the configuration to disk
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	configPath := filepath.Join(c.DataDir, "config.json")

	// Ensure directory exists
	if err := os.MkdirAll(c.DataDir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600)
}

// Update applies a modifier function to the config and saves
func (c *Config) Update(fn func(cfg *Config)) error {
	c.mu.Lock()
	fn(c)
	c.mu.Unlock()
	return c.Save()
}

// GetListenAddr returns "host:port"
func (c *Config) GetListenAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.AllowDirectAccess {
		return "0.0.0.0:" + itoa(c.Port)
	}
	return c.Host + ":" + itoa(c.Port)
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
