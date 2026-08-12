package sync

import (
	"path/filepath"
	"testing"
)

func TestLegacyPullSyncIsOptIn(t *testing.T) {
	t.Setenv("CROSSHARE_LEGACY_PULL_SYNC", "")
	if LegacyPullEnabledFromEnv() {
		t.Fatal("legacy pull sync must be disabled by default")
	}
	t.Setenv("CROSSHARE_LEGACY_PULL_SYNC", "true")
	if !LegacyPullEnabledFromEnv() {
		t.Fatal("explicit opt-in should enable legacy pull sync")
	}
}

func TestPathWithin(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "partial.bin")
	outside := filepath.Join(filepath.Dir(root), "outside.bin")
	if !pathWithin(root, inside) {
		t.Fatalf("expected %s to be inside %s", inside, root)
	}
	if pathWithin(root, outside) {
		t.Fatalf("expected %s to be outside %s", outside, root)
	}
}
