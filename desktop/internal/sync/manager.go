package sync

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/crosshare/desktop/internal/config"
	"github.com/crosshare/desktop/internal/discovery"
	"github.com/crosshare/desktop/internal/organizer"
	"github.com/crosshare/desktop/internal/storage"
)

// SyncStatus represents the current sync state.
type SyncStatus int

const (
	StatusIdle SyncStatus = iota
	StatusDiscovering
	StatusSyncing
	StatusError
)

func (s SyncStatus) String() string {
	switch s {
	case StatusIdle:
		return "空闲"
	case StatusDiscovering:
		return "搜索设备中..."
	case StatusSyncing:
		return "同步中..."
	case StatusError:
		return "错误"
	default:
		return "未知"
	}
}

// SyncProgress reports current sync progress.
type SyncProgress struct {
	Status       SyncStatus
	CurrentFile  string
	FilesTotal   int
	FilesDone    int
	BytesTotal   int64
	BytesDone    int64
	ErrorMessage string
}

// Manager coordinates the sync process between phone discovery and file download.
type Manager struct {
	cfg      *config.Config
	db       *storage.DB
	disc     *discovery.Discovery
	org      *organizer.Organizer
	progress atomic.Value // *SyncProgress
	onUpdate func(SyncProgress)
	syncing  atomic.Bool
}

// NewManager creates a new sync manager.
func NewManager(cfg *config.Config, db *storage.DB, disc *discovery.Discovery) *Manager {
	m := &Manager{
		cfg:  cfg,
		db:   db,
		disc: disc,
		org:  organizer.New(cfg.GetTargetFolder()),
	}

	m.progress.Store(&SyncProgress{Status: StatusIdle})

	// Set up discovery callback
	disc.OnPhoneFound(func(phone *discovery.PhoneInfo) {
		if !m.isPhonePaired(phone) {
			log.Printf("[sync] found unpaired phone %s (%s), waiting for pairing", phone.DeviceName, phone.DeviceID)
			return
		}
		go m.syncWithPhone(phone)
	})

	return m
}

// OnProgressUpdate sets the callback for sync progress updates.
func (m *Manager) OnProgressUpdate(fn func(SyncProgress)) {
	m.onUpdate = fn
}

// GetProgress returns the current sync progress.
func (m *Manager) GetProgress() SyncProgress {
	return *m.progress.Load().(*SyncProgress)
}

// GetKnownPhones returns phones seen by mDNS discovery.
func (m *Manager) GetKnownPhones() []*discovery.PhoneInfo {
	return m.disc.GetPhones()
}

// updateProgress updates the stored progress and notifies the callback.
func (m *Manager) updateProgress(p SyncProgress) {
	m.progress.Store(&p)
	if m.onUpdate != nil {
		m.onUpdate(p)
	}
}

// syncWithPhone handles the full sync flow with a discovered phone.
func (m *Manager) syncWithPhone(phone *discovery.PhoneInfo) {
	// Ignore phones that have not completed the one-time pairing flow.
	token := m.findTokenForPhone(phone)
	if token == "" {
		log.Printf("[sync] skipping unpaired phone %s (%s)", phone.DeviceName, phone.DeviceID)
		return
	}

	// Prevent concurrent syncs
	if !m.syncing.CompareAndSwap(false, true) {
		log.Printf("[sync] already syncing, skipping phone %s", phone.DeviceName)
		return
	}
	defer m.syncing.Store(false)

	log.Printf("[sync] starting sync with %s (%s:%d)", phone.DeviceName, phone.IP, phone.Port)
	m.updateProgress(SyncProgress{Status: StatusSyncing})

	// 1. Fetch media list from phone
	mediaList, err := discovery.FetchMediaList(phone, token)
	if err != nil {
		log.Printf("[sync] failed to fetch media list from %s: %v", phone.DeviceName, err)
		m.updateProgress(SyncProgress{
			Status:       StatusError,
			ErrorMessage: fmt.Sprintf("无法获取 %s 的媒体列表: %v", phone.DeviceName, err),
		})
		return
	}

	log.Printf("[sync] phone %s has %d media items", phone.DeviceName, len(mediaList.Items))

	// 2. Get already-synced hashes
	syncedHashes, err := m.db.GetSyncedHashes()
	if err != nil {
		log.Printf("[sync] failed to get synced hashes: %v", err)
		m.updateProgress(SyncProgress{
			Status:       StatusError,
			ErrorMessage: "无法查询已同步文件",
		})
		return
	}

	// 3. Filter to only new items
	var newItems []discovery.PhoneMediaItem
	for _, item := range mediaList.Items {
		if !syncedHashes[item.ContentHash] {
			newItems = append(newItems, item)
		}
	}

	if len(newItems) == 0 {
		log.Printf("[sync] no new files to sync from %s", phone.DeviceName)
		m.updateProgress(SyncProgress{Status: StatusIdle})
		return
	}

	log.Printf("[sync] %d new files to sync from %s", len(newItems), phone.DeviceName)

	// 4. Download each new file
	// Refresh organizer in case target folder changed
	m.org = organizer.New(m.cfg.GetTargetFolder())

	var successfulHashes []string
	for i, item := range newItems {
		m.updateProgress(SyncProgress{
			Status:      StatusSyncing,
			CurrentFile: item.FileName,
			FilesTotal:  len(newItems),
			FilesDone:   i,
		})

		if err := m.downloadAndOrganize(phone, &item, token); err != nil {
			log.Printf("[sync] failed to download %s: %v", item.FileName, err)
			continue
		}

		successfulHashes = append(successfulHashes, item.ContentHash)
		log.Printf("[sync] synced %s (%d/%d)", item.FileName, i+1, len(newItems))
	}

	if len(successfulHashes) > 0 {
		if err := discovery.NotifyMediaSynced(phone, token, successfulHashes); err != nil {
			log.Printf("[sync] failed to notify phone of synced hashes: %v", err)
		} else {
			log.Printf("[sync] notified phone of %d synced hashes", len(successfulHashes))
		}
	}

	m.updateProgress(SyncProgress{
		Status:     StatusIdle,
		FilesTotal: len(newItems),
		FilesDone:  len(newItems),
	})

	log.Printf("[sync] completed sync with %s", phone.DeviceName)
}

// downloadAndOrganize downloads a single file from the phone with resume support.
func (m *Manager) downloadAndOrganize(phone *discovery.PhoneInfo, item *discovery.PhoneMediaItem, token string) error {
	configDir := config.ConfigDir()
	tmpDir := filepath.Join(configDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create tmp dir: %w", err)
	}

	tmpPath := filepath.Join(tmpDir, item.ContentHash+filepath.Ext(item.FileName))
	deviceID := phone.DeviceID

	// Check for existing partial download
	var offset int64
	progress, err := m.db.GetDownloadProgress(item.ContentHash, deviceID)
	if err == nil && progress != nil {
		// Verify temp file still exists and has expected size
		info, statErr := os.Stat(progress.TempPath)
		if statErr == nil && info.Size() == progress.BytesReceived {
			offset = progress.BytesReceived
			tmpPath = progress.TempPath
			log.Printf("[sync] resuming %s from byte %d", item.FileName, offset)
		}
	}

	// Download with resume support
	body, totalSize, err := discovery.DownloadMedia(phone, item.ContentHash, token, offset)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer body.Close()

	// Open temp file for writing (append if resuming)
	var f *os.File
	if offset > 0 {
		f, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0644)
	} else {
		f, err = os.Create(tmpPath)
	}
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}

	// Save download progress periodically
	bytesWritten := offset
	buf := make([]byte, 256*1024) // 256KB buffer
	lastProgressSave := time.Now()

	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				f.Close()
				return fmt.Errorf("failed to write to temp file: %w", writeErr)
			}
			bytesWritten += int64(n)

			// Save progress every 5 seconds
			if time.Since(lastProgressSave) > 5*time.Second {
				parsedDate, _ := time.Parse(time.RFC3339, item.DateTaken)
				m.db.SaveDownloadProgress(&storage.DownloadProgress{
					ContentHash:   item.ContentHash,
					DeviceID:      deviceID,
					OriginalName:  item.FileName,
					TempPath:      tmpPath,
					BytesReceived: bytesWritten,
					TotalSize:     totalSize,
					DateTaken:     parsedDate,
					MediaType:     item.MediaType,
				})
				lastProgressSave = time.Now()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			f.Close()
			// Save progress before returning error (for resume on retry)
			parsedDate, _ := time.Parse(time.RFC3339, item.DateTaken)
			m.db.SaveDownloadProgress(&storage.DownloadProgress{
				ContentHash:   item.ContentHash,
				DeviceID:      deviceID,
				OriginalName:  item.FileName,
				TempPath:      tmpPath,
				BytesReceived: bytesWritten,
				TotalSize:     totalSize,
				DateTaken:     parsedDate,
				MediaType:     item.MediaType,
			})
			return fmt.Errorf("download interrupted: %w", readErr)
		}
	}
	f.Close()

	// Parse date for organization
	dateTaken, _ := time.Parse(time.RFC3339, item.DateTaken)

	// Organize the file into date-based directory
	finalPath, err := m.org.OrganizeFile(tmpPath, item.FileName, dateTaken)
	if err != nil {
		return fmt.Errorf("failed to organize file: %w", err)
	}

	// Record in database
	if err := m.db.AddSyncedFile(&storage.SyncedFile{
		ContentHash:  item.ContentHash,
		OriginalName: item.FileName,
		DateTaken:    dateTaken,
		LocalPath:    finalPath,
		FileSize:     item.FileSize,
		MediaType:    item.MediaType,
		DeviceID:     deviceID,
	}); err != nil {
		return fmt.Errorf("failed to record synced file: %w", err)
	}

	// Clean up download progress
	m.db.DeleteDownloadProgress(item.ContentHash, deviceID)

	return nil
}

// findTokenForPhone finds the API token for a given phone.
func (m *Manager) findTokenForPhone(phone *discovery.PhoneInfo) string {
	for _, d := range m.cfg.GetPairedDevices() {
		if d.DeviceID == phone.DeviceID {
			return d.Token
		}
	}
	return ""
}

func (m *Manager) isPhonePaired(phone *discovery.PhoneInfo) bool {
	return m.findTokenForPhone(phone) != ""
}

// TriggerSync manually triggers a sync with all known phones.
func (m *Manager) TriggerSync(_ context.Context) {
	go func() {
		m.updateProgress(SyncProgress{Status: StatusDiscovering})

		scanCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		m.disc.ScanNow(scanCtx)

		phones := m.disc.GetPhones()
		started := false
		for _, phone := range phones {
			if !m.isPhonePaired(phone) {
				continue
			}
			started = true
			go m.syncWithPhone(phone)
		}
		if !started {
			m.updateProgress(SyncProgress{Status: StatusIdle})
		}
	}()
}
