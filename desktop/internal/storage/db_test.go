package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := OpenAt(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestHasFile(t *testing.T) {
	db := setupTestDB(t)

	has, err := db.HasFile("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("should not have file before adding")
	}

	err = db.AddSyncedFile(&SyncedFile{
		ContentHash:  "abc123",
		OriginalName: "photo.jpg",
		DateTaken:    time.Now(),
		LocalPath:    "/photos/2026/2026-04-03/photo.jpg",
		FileSize:     1024,
		MediaType:    "photo",
		DeviceID:     "phone-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	has, err = db.HasFile("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("should have file after adding")
	}
}

func TestGetSyncedHashes(t *testing.T) {
	db := setupTestDB(t)

	for _, hash := range []string{"aaa", "bbb", "ccc"} {
		db.AddSyncedFile(&SyncedFile{
			ContentHash:  hash,
			OriginalName: hash + ".jpg",
			DateTaken:    time.Now(),
			LocalPath:    "/photos/" + hash + ".jpg",
			FileSize:     1024,
			MediaType:    "photo",
			DeviceID:     "phone-1",
		})
	}

	hashes, err := db.GetSyncedHashes()
	if err != nil {
		t.Fatal(err)
	}

	if len(hashes) != 3 {
		t.Errorf("expected 3 hashes, got %d", len(hashes))
	}

	for _, h := range []string{"aaa", "bbb", "ccc"} {
		if !hashes[h] {
			t.Errorf("missing hash %s", h)
		}
	}
}

func TestDuplicateInsert(t *testing.T) {
	db := setupTestDB(t)

	file := &SyncedFile{
		ContentHash:  "dup123",
		OriginalName: "photo.jpg",
		DateTaken:    time.Now(),
		LocalPath:    "/photos/photo.jpg",
		FileSize:     1024,
		MediaType:    "photo",
		DeviceID:     "phone-1",
	}

	if err := db.AddSyncedFile(file); err != nil {
		t.Fatal(err)
	}

	if err := db.AddSyncedFile(file); err != nil {
		t.Fatalf("duplicate insert should not error: %v", err)
	}

	hashes, _ := db.GetSyncedHashes()
	if len(hashes) != 1 {
		t.Errorf("expected 1 hash after duplicate insert, got %d", len(hashes))
	}
}

func TestSyncStats(t *testing.T) {
	db := setupTestDB(t)

	count, size, err := db.GetSyncStats()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || size != 0 {
		t.Errorf("expected 0/0, got count=%d size=%d", count, size)
	}

	db.AddSyncedFile(&SyncedFile{
		ContentHash: "a", OriginalName: "a.jpg", LocalPath: "/a.jpg",
		FileSize: 1000, MediaType: "photo", DeviceID: "p1",
	})
	db.AddSyncedFile(&SyncedFile{
		ContentHash: "b", OriginalName: "b.mp4", LocalPath: "/b.mp4",
		FileSize: 5000000, MediaType: "video", DeviceID: "p1",
	})

	count, size, err = db.GetSyncStats()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}
	if size != 5001000 {
		t.Errorf("expected size=5001000, got %d", size)
	}
}

func TestDownloadProgress(t *testing.T) {
	db := setupTestDB(t)

	p, err := db.GetDownloadProgress("hash1", "device1")
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Error("expected nil progress")
	}

	err = db.SaveDownloadProgress(&DownloadProgress{
		ContentHash:   "hash1",
		DeviceID:      "device1",
		OriginalName:  "video.mp4",
		TempPath:      "/tmp/hash1.mp4",
		BytesReceived: 1024000,
		TotalSize:     5000000,
		MediaType:     "video",
	})
	if err != nil {
		t.Fatal(err)
	}

	p, err = db.GetDownloadProgress("hash1", "device1")
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil progress")
	}
	if p.BytesReceived != 1024000 {
		t.Errorf("expected 1024000 bytes, got %d", p.BytesReceived)
	}

	err = db.SaveDownloadProgress(&DownloadProgress{
		ContentHash:   "hash1",
		DeviceID:      "device1",
		OriginalName:  "video.mp4",
		TempPath:      "/tmp/hash1.mp4",
		BytesReceived: 3000000,
		TotalSize:     5000000,
		MediaType:     "video",
	})
	if err != nil {
		t.Fatal(err)
	}

	p, _ = db.GetDownloadProgress("hash1", "device1")
	if p.BytesReceived != 3000000 {
		t.Errorf("expected 3000000 bytes updated, got %d", p.BytesReceived)
	}

	err = db.DeleteDownloadProgress("hash1", "device1")
	if err != nil {
		t.Fatal(err)
	}

	p, _ = db.GetDownloadProgress("hash1", "device1")
	if p != nil {
		t.Error("expected nil after delete")
	}
}
