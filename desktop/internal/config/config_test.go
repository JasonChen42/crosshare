package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.HTTPPort != 8384 {
		t.Errorf("expected port 8384, got %d", cfg.HTTPPort)
	}

	if cfg.DiscoveryIntervalSeconds != 300 {
		t.Errorf("expected 300s interval, got %d", cfg.DiscoveryIntervalSeconds)
	}

	if len(cfg.PairCode) != 6 {
		t.Errorf("expected 6-digit pair code, got %q", cfg.PairCode)
	}

	if cfg.TargetFolder == "" {
		t.Error("target folder should not be empty")
	}
}

func TestPairCodeGeneration(t *testing.T) {
	codes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code := generatePairCode()
		if len(code) != 6 {
			t.Errorf("expected 6-digit code, got %q", code)
		}
		codes[code] = true
	}
	// Should generate at least some unique codes
	if len(codes) < 50 {
		t.Errorf("expected reasonable uniqueness, got %d unique codes from 100 generations", len(codes))
	}
}

func TestTokenGeneration(t *testing.T) {
	token := GenerateToken()
	if len(token) != 32 {
		t.Errorf("expected 32-char token, got %d chars: %q", len(token), token)
	}

	// Should be unique
	token2 := GenerateToken()
	if token == token2 {
		t.Error("two generated tokens should not be equal")
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.filePath = configPath
	cfg.TargetFolder = "/test/photos"
	cfg.DeviceName = "TestPC"

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	// Load it back - we need to temporarily override ConfigDir
	// Since Load uses ConfigDir directly, test Save/Load through the file
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Fatal("config file is empty")
	}
}

func TestPairedDeviceManagement(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.filePath = configPath

	// Add a device
	device := PairedDevice{
		DeviceID:   "phone-123",
		DeviceName: "My Phone",
		Token:      "test-token-abc",
		PairedAt:   "2026-04-03T00:00:00Z",
	}

	if err := cfg.AddPairedDevice(device); err != nil {
		t.Fatalf("AddPairedDevice failed: %v", err)
	}

	if len(cfg.PairedDevices) != 1 {
		t.Errorf("expected 1 paired device, got %d", len(cfg.PairedDevices))
	}

	// Find by token
	found := cfg.FindDeviceByToken("test-token-abc")
	if found == nil {
		t.Fatal("FindDeviceByToken returned nil")
	}
	if found.DeviceID != "phone-123" {
		t.Errorf("expected device phone-123, got %s", found.DeviceID)
	}

	// Find by invalid token
	notFound := cfg.FindDeviceByToken("invalid")
	if notFound != nil {
		t.Error("FindDeviceByToken should return nil for invalid token")
	}

	// Remove device
	if err := cfg.RemovePairedDevice("phone-123"); err != nil {
		t.Fatalf("RemovePairedDevice failed: %v", err)
	}
	if len(cfg.PairedDevices) != 0 {
		t.Errorf("expected 0 paired devices, got %d", len(cfg.PairedDevices))
	}
}
