package config

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// PairedDevice represents a phone that has been paired with this computer.
type PairedDevice struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Token      string `json:"token"`
	PairedAt   string `json:"paired_at"`
	LastSeen   string `json:"last_seen,omitempty"`
	LastIP     string `json:"last_ip,omitempty"`
}

// Config holds all application configuration.
type Config struct {
	// TargetFolder is where synced photos/videos are stored.
	TargetFolder string `json:"target_folder"`

	// HTTPPort is the port the HTTP server listens on (for manual phone push).
	HTTPPort int `json:"http_port"`

	// DeviceName is the display name for this computer.
	DeviceName string `json:"device_name"`

	// PairCode is the 6-digit code shown in the tray for pairing.
	PairCode string `json:"pair_code"`

	// PairedDevices is the list of phones paired with this computer.
	PairedDevices []PairedDevice `json:"paired_devices"`

	// DiscoveryIntervalSeconds is how often to scan for phones (default 300 = 5min).
	DiscoveryIntervalSeconds int `json:"discovery_interval_seconds"`

	mu       sync.RWMutex
	filePath string
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "CrossHare-PC"
	}

	homeDir, _ := os.UserHomeDir()
	defaultTarget := filepath.Join(homeDir, "Pictures", "CrossHare")

	return &Config{
		TargetFolder:             defaultTarget,
		HTTPPort:                 8384,
		DeviceName:               hostname,
		PairCode:                 generatePairCode(),
		PairedDevices:            []PairedDevice{},
		DiscoveryIntervalSeconds: 300, // 5 minutes
	}
}

// ConfigDir returns the platform-specific config directory.
func ConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "CrossHare")
	default: // linux, darwin
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".crosshare")
	}
}

// EnsureConfigDir creates the config directory with private permissions.
func EnsureConfigDir() (string, error) {
	configDir := ConfigDir()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(configDir, 0700); err != nil {
		return "", err
	}
	return configDir, nil
}

// Load reads the config from disk, or creates a default one if it doesn't exist.
func Load() (*Config, error) {
	configDir, err := EnsureConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to create config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			cfg.filePath = configPath
			if saveErr := cfg.Save(); saveErr != nil {
				return nil, fmt.Errorf("failed to save default config: %w", saveErr)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	cfg.filePath = configPath

	// Ensure pair code exists
	if cfg.PairCode == "" {
		cfg.PairCode = generatePairCode()
		_ = cfg.Save()
	}

	return cfg, nil
}

// Save writes the config to disk.
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(c.filePath, data, 0600)
}

// SetTargetFolder updates the target folder and saves.
func (c *Config) SetTargetFolder(path string) error {
	c.mu.Lock()
	c.TargetFolder = path
	c.mu.Unlock()
	return c.Save()
}

// AddPairedDevice adds a new paired device and saves.
func (c *Config) AddPairedDevice(device PairedDevice) error {
	c.mu.Lock()
	c.PairedDevices = append(c.PairedDevices, device)
	// Regenerate pair code after successful pairing
	c.PairCode = generatePairCode()
	c.mu.Unlock()
	return c.Save()
}

// FindDeviceByToken returns the paired device for a given token.
func (c *Config) FindDeviceByToken(token string) *PairedDevice {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.PairedDevices {
		if subtle.ConstantTimeCompare([]byte(c.PairedDevices[i].Token), []byte(token)) == 1 {
			return &c.PairedDevices[i]
		}
	}
	return nil
}

// UpdateDeviceSeenByToken records that a paired device contacted this computer.
func (c *Config) UpdateDeviceSeenByToken(token, ip string) error {
	c.mu.Lock()
	updated := false
	now := timeNowRFC3339()
	for i := range c.PairedDevices {
		if c.PairedDevices[i].Token == token {
			c.PairedDevices[i].LastSeen = now
			c.PairedDevices[i].LastIP = ip
			updated = true
			break
		}
	}
	c.mu.Unlock()

	if !updated {
		return nil
	}
	return c.Save()
}

// GetPairedDevices returns a copy of the paired device list.
func (c *Config) GetPairedDevices() []PairedDevice {
	c.mu.RLock()
	defer c.mu.RUnlock()
	devices := make([]PairedDevice, len(c.PairedDevices))
	copy(devices, c.PairedDevices)
	return devices
}

// GetPairedDeviceCount returns the current number of paired devices.
func (c *Config) GetPairedDeviceCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.PairedDevices)
}

// RemovePairedDevice removes a device by ID.
func (c *Config) RemovePairedDevice(deviceID string) error {
	c.mu.Lock()
	for i, d := range c.PairedDevices {
		if d.DeviceID == deviceID {
			c.PairedDevices = append(c.PairedDevices[:i], c.PairedDevices[i+1:]...)
			break
		}
	}
	c.mu.Unlock()
	return c.Save()
}

// GetTargetFolder returns the current target folder.
func (c *Config) GetTargetFolder() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TargetFolder
}

// GetPairCode returns the current pair code.
func (c *Config) GetPairCode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.PairCode
}

// generatePairCode generates a random 6-digit numeric code.
func generatePairCode() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	code := (int(b[0])<<16 | int(b[1])<<8 | int(b[2])) % 1000000
	return fmt.Sprintf("%06d", code)
}

// GenerateToken generates a random API token (32 hex chars).
func GenerateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func timeNowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}
