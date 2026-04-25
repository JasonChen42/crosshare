package organizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOrganizeFile_CreatesDateDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	org := New(tmpDir)

	// Create a test file
	srcFile := filepath.Join(t.TempDir(), "test_photo.jpg")
	if err := os.WriteFile(srcFile, []byte("fake photo data"), 0644); err != nil {
		t.Fatal(err)
	}

	dateTaken := time.Date(2026, 4, 3, 10, 30, 0, 0, time.UTC)

	finalPath, err := org.OrganizeFile(srcFile, "test_photo.jpg", dateTaken)
	if err != nil {
		t.Fatalf("OrganizeFile failed: %v", err)
	}

	// Verify path contains YYYY/YYYY-MM-DD/
	expectedDir := filepath.Join(tmpDir, "2026", "2026-04-03")
	if !strings.HasPrefix(finalPath, expectedDir) {
		t.Errorf("expected path to start with %s, got %s", expectedDir, finalPath)
	}

	// Verify file exists at final path
	if _, err := os.Stat(finalPath); os.IsNotExist(err) {
		t.Errorf("file not found at %s", finalPath)
	}

	// Verify file content
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fake photo data" {
		t.Errorf("file content mismatch: got %s", string(data))
	}
}

func TestOrganizeFile_HandlesDuplicateNames(t *testing.T) {
	tmpDir := t.TempDir()
	org := New(tmpDir)

	dateTaken := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	// Create first file
	src1 := filepath.Join(t.TempDir(), "photo.jpg")
	os.WriteFile(src1, []byte("first"), 0644)
	path1, err := org.OrganizeFile(src1, "photo.jpg", dateTaken)
	if err != nil {
		t.Fatal(err)
	}

	// Create second file with same name
	src2 := filepath.Join(t.TempDir(), "photo.jpg")
	os.WriteFile(src2, []byte("second"), 0644)
	path2, err := org.OrganizeFile(src2, "photo.jpg", dateTaken)
	if err != nil {
		t.Fatal(err)
	}

	// Paths should be different
	if path1 == path2 {
		t.Errorf("duplicate paths: %s", path1)
	}

	// Second file should have _1 suffix
	if !strings.Contains(filepath.Base(path2), "_1") {
		t.Errorf("expected _1 suffix, got %s", filepath.Base(path2))
	}

	// Both should have correct content
	d1, _ := os.ReadFile(path1)
	d2, _ := os.ReadFile(path2)
	if string(d1) != "first" || string(d2) != "second" {
		t.Errorf("content mismatch: first=%s, second=%s", d1, d2)
	}
}

func TestOrganizeFile_FallbackToCurrentTime(t *testing.T) {
	tmpDir := t.TempDir()
	org := New(tmpDir)

	srcFile := filepath.Join(t.TempDir(), "photo.jpg")
	os.WriteFile(srcFile, []byte("data"), 0644)

	// Zero time - should fallback
	finalPath, err := org.OrganizeFile(srcFile, "photo.jpg", time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	// Should use today's date
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(finalPath, today) {
		t.Errorf("expected path to contain today's date %s, got %s", today, finalPath)
	}
}

func TestUniqueFilename(t *testing.T) {
	tmpDir := t.TempDir()
	org := New(tmpDir)

	// No conflict
	name := org.uniqueFilename(tmpDir, "photo.jpg")
	if name != "photo.jpg" {
		t.Errorf("expected photo.jpg, got %s", name)
	}

	// Create the file
	os.WriteFile(filepath.Join(tmpDir, "photo.jpg"), []byte{}, 0644)

	// Should get _1
	name = org.uniqueFilename(tmpDir, "photo.jpg")
	if name != "photo_1.jpg" {
		t.Errorf("expected photo_1.jpg, got %s", name)
	}

	// Create _1 too
	os.WriteFile(filepath.Join(tmpDir, "photo_1.jpg"), []byte{}, 0644)

	// Should get _2
	name = org.uniqueFilename(tmpDir, "photo.jpg")
	if name != "photo_2.jpg" {
		t.Errorf("expected photo_2.jpg, got %s", name)
	}
}
