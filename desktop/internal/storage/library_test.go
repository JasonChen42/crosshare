package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestLibraryDeliverySurvivesReopenAndAlbumsDoNotCopyAssets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := "aabb"
	_, err = db.AddSyncedFile(&SyncedFile{ContentHash: hash, OriginalName: "summer.jpg", DateTaken: time.Now(), LocalPath: "/test/summer.jpg", FileSize: 12, MediaType: "photo", DeviceID: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateAlbum("旅行")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AddToAlbum(id, hash); err != nil {
		t.Fatal(err)
	}
	if err = db.AddToAlbum(id, hash); err != nil {
		t.Fatal(err)
	}
	if err = db.SetFavorite(hash, true); err != nil {
		t.Fatal(err)
	}
	if err = db.QueueDelivery(hash, "phone"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tasks, err := db.Deliveries("phone")
	if err != nil || len(tasks) != 1 || tasks[0].Status != "pending" {
		t.Fatalf("tasks lost: %+v %v", tasks, err)
	}
	items, err := db.Library(100, 0, "summer", id, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("library: %+v %v", items, err)
	}
	albums, err := db.Albums()
	if err != nil || len(albums) != 1 || albums[0].Count != 1 {
		t.Fatalf("albums: %+v %v", albums, err)
	}
	count, _, _ := db.GetSyncStats()
	if count != 1 {
		t.Fatal("album created duplicate original")
	}
}

func TestQueueWholeAlbumIsNotLimitedByPageAndKeepsFoldersDistinct(t *testing.T) {
	db, err := OpenAt(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := db.CreateAlbum("旅行")
	b, _ := db.CreateAlbum("精选")
	for i := 0; i < 125; i++ {
		hash := fmt.Sprintf("%064x", i)
		_, err = db.AddSyncedFile(&SyncedFile{ContentHash: hash, OriginalName: fmt.Sprintf("%d.jpg", i), DateTaken: time.Now(), LocalPath: "/test", MediaType: "photo"})
		if err != nil {
			t.Fatal(err)
		}
		if err = db.AddToAlbum(a, hash); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			db.AddToAlbum(b, hash)
		}
	}
	db.AddSyncedFile(&SyncedFile{ContentHash: "video", OriginalName: "clip.mp4", DateTaken: time.Now(), LocalPath: "/video", MediaType: "video"})
	db.AddToAlbum(a, "video")
	n, err := db.QueueAlbum(a, "phone")
	if err != nil || n != 125 {
		t.Fatalf("queued=%d err=%v", n, err)
	}
	db.QueueAlbum(a, "phone") // Idempotent batch; no duplicate tasks.
	db.QueueAlbum(b, "phone") // Same original in another folder must have its own task.
	tasks, err := db.DeliveriesPage("phone", 200, 0, false)
	if err != nil || len(tasks) != 126 {
		t.Fatalf("tasks=%d err=%v", len(tasks), err)
	}
	counts := map[int64]int{}
	for _, task := range tasks {
		counts[task.AlbumID]++
		if task.AlbumName == "" {
			t.Fatal("missing folder name")
		}
	}
	if counts[a] != 125 || counts[b] != 1 {
		t.Fatalf("folder collision: %+v", counts)
	}
}

func TestOldDeliverySchemaMigrationPreservesPendingTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := openSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE delivery_tasks(id INTEGER PRIMARY KEY AUTOINCREMENT,hash TEXT NOT NULL,device_id TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'pending',error TEXT NOT NULL DEFAULT '',updated TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,UNIQUE(hash,device_id)); INSERT INTO delivery_tasks(id,hash,device_id) VALUES(7,'abc','phone');`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db, err := OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tasks, err := db.Deliveries("phone")
	if err != nil || len(tasks) != 1 || tasks[0].ID != 7 || tasks[0].AlbumID != 0 {
		t.Fatalf("lost legacy tasks: %+v %v", tasks, err)
	}
	if err = db.QueueDelivery("abc", "phone"); err != nil {
		t.Fatal(err)
	}
}
