package sync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/crosshare/desktop/internal/config"
	"github.com/crosshare/desktop/internal/discovery"
	"github.com/crosshare/desktop/internal/media"
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
		if !LegacyPullEnabledFromEnv() {
			return
		}
		if !m.isPhonePaired(phone) {
			log.Printf("[sync] found unpaired phone %s (%s), waiting for pairing", phone.DeviceName, phone.DeviceID)
			return
		}
		go m.syncWithPhone(phone)
	})

	return m
}

// LegacyPullEnabledFromEnv keeps the old mDNS desktop-pull protocol opt-in.
// The current supported protocol is authenticated Android-to-desktop push.
func LegacyPullEnabledFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CROSSHARE_LEGACY_PULL_SYNC"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
		normalizedHash, err := media.NormalizeSHA256(item.ContentHash)
		if err != nil {
			log.Printf("[sync] ignoring media item with invalid hash from %s: %v", phone.DeviceName, err)
			continue
		}
		if item.FileSize < 0 || item.FileSize > media.MaxFileBytesFromEnv() {
			log.Printf("[sync] ignoring media item with invalid size from %s: %d", phone.DeviceName, item.FileSize)
			continue
		}
		item.ContentHash = normalizedHash
		item.FileName = media.SafeFileName(item.FileName, normalizedHash)
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
	failedCount := 0
	for i, item := range newItems {
		m.updateProgress(SyncProgress{
			Status:      StatusSyncing,
			CurrentFile: item.FileName,
			FilesTotal:  len(newItems),
			FilesDone:   i,
		})

		if err := m.downloadAndOrganize(phone, &item, token); err != nil {
			log.Printf("[sync] failed to download %s: %v", item.FileName, err)
			failedCount++
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

	finalProgress := SyncProgress{
		Status:     StatusIdle,
		FilesTotal: len(newItems),
		FilesDone:  len(successfulHashes),
	}
	if failedCount > 0 {
		finalProgress.Status = StatusError
		finalProgress.ErrorMessage = fmt.Sprintf("%d 个文件同步失败", failedCount)
	}
	m.updateProgress(finalProgress)

	log.Printf("[sync] completed sync with %s", phone.DeviceName)
}

// downloadAndOrganize downloads a single file from the phone with resume support.
func (m *Manager) downloadAndOrganize(phone *discovery.PhoneInfo, item *discovery.PhoneMediaItem, token string) error {
	configDir := config.ConfigDir()
	tmpDir := filepath.Join(configDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return fmt.Errorf("failed to create tmp dir: %w", err)
	}

	tmpPath := filepath.Join(tmpDir, item.ContentHash+filepath.Ext(item.FileName))
	deviceID := phone.DeviceID

	// Check for existing partial download
	var offset int64
	progress, err := m.db.GetDownloadProgress(item.ContentHash, deviceID)
	if err != nil {
		return fmt.Errorf("failed to read download progress: %w", err)
	}
	if progress != nil && pathWithin(tmpDir, progress.TempPath) {
		// Verify temp file still exists and has expected size
		info, statErr := os.Stat(progress.TempPath)
		if statErr == nil && info.Size() == progress.BytesReceived && info.Size() <= media.MaxFileBytesFromEnv() {
			offset = progress.BytesReceived
			tmpPath = progress.TempPath
			log.Printf("[sync] resuming %s from byte %d", item.FileName, offset)
		}
	}

	// Download with resume support
	body, totalSize, partial, err := discovery.DownloadMedia(phone, item.ContentHash, token, offset)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer body.Close()
	if offset > 0 && !partial {
		log.Printf("[sync] phone ignored range request for %s; restarting download", item.FileName)
		offset = 0
	}
	if totalSize > media.MaxFileBytesFromEnv() {
		return fmt.Errorf("download exceeds maximum size of %d bytes", media.MaxFileBytesFromEnv())
	}
	if item.FileSize > 0 && totalSize > 0 && totalSize != item.FileSize {
		return fmt.Errorf("download size changed: phone listed %d bytes, response has %d", item.FileSize, totalSize)
	}

	// Open temp file for writing (append if resuming)
	var f *os.File
	hasher := sha256.New()
	if offset > 0 {
		existing, openErr := os.Open(tmpPath)
		if openErr != nil {
			return fmt.Errorf("failed to open partial file for verification: %w", openErr)
		}
		_, hashErr := io.Copy(hasher, existing)
		closeErr := existing.Close()
		if hashErr != nil || closeErr != nil {
			return fmt.Errorf("failed to hash partial file: %v", firstError(hashErr, closeErr))
		}
		f, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0644)
	} else {
		f, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	}
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}

	// Save download progress periodically
	bytesWritten := offset
	buf := make([]byte, 256*1024) // 256KB buffer
	lastProgressSave := time.Now()

	limit := media.MaxFileBytesFromEnv() - offset + 1
	limitedBody := &io.LimitedReader{R: body, N: limit}
	for {
		n, readErr := limitedBody.Read(buf)
		if n > 0 {
			if _, writeErr := io.MultiWriter(f, hasher).Write(buf[:n]); writeErr != nil {
				f.Close()
				return fmt.Errorf("failed to write to temp file: %w", writeErr)
			}
			bytesWritten += int64(n)

			// Save progress every 5 seconds
			if time.Since(lastProgressSave) > 5*time.Second {
				parsedDate, _ := time.Parse(time.RFC3339, item.DateTaken)
				if err := m.db.SaveDownloadProgress(&storage.DownloadProgress{
					ContentHash:   item.ContentHash,
					DeviceID:      deviceID,
					OriginalName:  item.FileName,
					TempPath:      tmpPath,
					BytesReceived: bytesWritten,
					TotalSize:     totalSize,
					DateTaken:     parsedDate,
					MediaType:     item.MediaType,
				}); err != nil {
					f.Close()
					return fmt.Errorf("failed to save download progress: %w", err)
				}
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
			_ = m.db.SaveDownloadProgress(&storage.DownloadProgress{
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
	if limitedBody.N == 0 {
		f.Close()
		return fmt.Errorf("download exceeds maximum size of %d bytes", media.MaxFileBytesFromEnv())
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if item.FileSize > 0 && bytesWritten != item.FileSize {
		return fmt.Errorf("download size mismatch: expected %d bytes, received %d", item.FileSize, bytesWritten)
	}
	if actualHash := fmt.Sprintf("%x", hasher.Sum(nil)); actualHash != item.ContentHash {
		_ = os.Remove(tmpPath)
		_ = m.db.DeleteDownloadProgress(item.ContentHash, deviceID)
		return fmt.Errorf("download hash mismatch: expected %s, got %s", item.ContentHash, actualHash)
	}

	// Parse date for organization
	dateTaken, _ := time.Parse(time.RFC3339, item.DateTaken)

	// Organize the file into date-based directory
	finalPath, err := m.org.OrganizeFile(tmpPath, item.FileName, dateTaken)
	if err != nil {
		return fmt.Errorf("failed to organize file: %w", err)
	}

	// Record in database
	inserted, err := m.db.AddSyncedFile(&storage.SyncedFile{
		ContentHash:  item.ContentHash,
		OriginalName: item.FileName,
		DateTaken:    dateTaken,
		LocalPath:    finalPath,
		FileSize:     item.FileSize,
		MediaType:    item.MediaType,
		DeviceID:     deviceID,
	})
	if err != nil {
		_ = os.Remove(finalPath)
		return fmt.Errorf("failed to record synced file: %w", err)
	}
	if !inserted {
		_ = os.Remove(finalPath)
	}

	// Clean up download progress
	if err := m.db.DeleteDownloadProgress(item.ContentHash, deviceID); err != nil {
		return fmt.Errorf("failed to delete download progress: %w", err)
	}

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
	if !LegacyPullEnabledFromEnv() {
		log.Printf("[sync] legacy desktop-pull sync is disabled; phones should push media to the desktop API")
		return
	}
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

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
