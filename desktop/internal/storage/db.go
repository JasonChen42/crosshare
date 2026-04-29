package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/crosshare/desktop/internal/config"
	_ "modernc.org/sqlite"
)

// SyncedFile represents a file that has been synced from a phone.
type SyncedFile struct {
	ID           int64
	ContentHash  string
	OriginalName string
	DateTaken    time.Time
	LocalPath    string
	FileSize     int64
	MediaType    string // "photo" or "video"
	DeviceID     string
	SyncedAt     time.Time
}

// AudioRecording represents a WAV recording received from a paired phone.
type AudioRecording struct {
	ID            int64
	DeviceID      string
	DeviceName    string
	SessionID     string
	FileName      string
	LocalPath     string
	FileSize      int64
	SampleRate    int
	Channels      int
	BitsPerSample int
	Transcript    string
	StartedAt     time.Time
	CompletedAt   time.Time
	CreatedAt     time.Time
}

// DownloadProgress tracks partial downloads for resume support.
type DownloadProgress struct {
	ContentHash   string
	DeviceID      string
	OriginalName  string
	TempPath      string
	BytesReceived int64
	TotalSize     int64
	DateTaken     time.Time
	MediaType     string
	LastUpdated   time.Time
}

// DB wraps the SQLite database.
type DB struct {
	db *sql.DB
}

// openSQLite opens a raw SQLite connection at the given path.
func openSQLite(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
}

// Open opens or creates the SQLite database at the default config location.
func Open() (*DB, error) {
	dbDir, err := config.EnsureConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to create db dir: %w", err)
	}

	dbPath := filepath.Join(dbDir, "crosshare.db")
	sqlDB, err := openSQLite(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return d, nil
}

// OpenAt opens or creates the SQLite database at a specific path (for testing).
func OpenAt(dbPath string) (*DB, error) {
	sqlDB, err := openSQLite(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS synced_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			content_hash TEXT NOT NULL,
			original_name TEXT NOT NULL,
			date_taken DATETIME,
			local_path TEXT NOT NULL,
			file_size INTEGER NOT NULL DEFAULT 0,
			media_type TEXT NOT NULL DEFAULT 'photo',
			device_id TEXT NOT NULL DEFAULT '',
			synced_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_synced_files_hash_device 
		 ON synced_files(content_hash, device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_synced_files_device 
		 ON synced_files(device_id)`,
		`CREATE TABLE IF NOT EXISTS download_progress (
			content_hash TEXT NOT NULL,
			device_id TEXT NOT NULL,
			original_name TEXT NOT NULL,
			temp_path TEXT NOT NULL,
			bytes_received INTEGER NOT NULL DEFAULT 0,
			total_size INTEGER NOT NULL DEFAULT 0,
			date_taken DATETIME,
			media_type TEXT NOT NULL DEFAULT 'photo',
			last_updated DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (content_hash, device_id)
		)`,
		`CREATE TABLE IF NOT EXISTS audio_recordings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL DEFAULT '',
			device_name TEXT NOT NULL DEFAULT '',
			session_id TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL,
			local_path TEXT NOT NULL,
			file_size INTEGER NOT NULL DEFAULT 0,
			sample_rate INTEGER NOT NULL DEFAULT 0,
			channels INTEGER NOT NULL DEFAULT 0,
			bits_per_sample INTEGER NOT NULL DEFAULT 0,
			transcript TEXT NOT NULL DEFAULT '',
			started_at DATETIME,
			completed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_audio_recordings_local_path
		 ON audio_recordings(local_path)`,
		`CREATE INDEX IF NOT EXISTS idx_audio_recordings_completed_at
		 ON audio_recordings(completed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audio_recordings_device
		 ON audio_recordings(device_id)`,
	}

	for _, m := range migrations {
		if _, err := d.db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}

	return nil
}

// HasFile checks if a file with the given content hash has already been synced.
func (d *DB) HasFile(contentHash string) (bool, error) {
	var count int
	err := d.db.QueryRow(
		"SELECT COUNT(*) FROM synced_files WHERE content_hash = ?",
		contentHash,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetSyncedHashes returns all content hashes that have been synced.
func (d *DB) GetSyncedHashes() (map[string]bool, error) {
	rows, err := d.db.Query("SELECT content_hash FROM synced_files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hashes := make(map[string]bool)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes[hash] = true
	}
	return hashes, rows.Err()
}

// AddSyncedFile records a successfully synced file.
func (d *DB) AddSyncedFile(f *SyncedFile) error {
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO synced_files 
		 (content_hash, original_name, date_taken, local_path, file_size, media_type, device_id, synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ContentHash, f.OriginalName, f.DateTaken, f.LocalPath,
		f.FileSize, f.MediaType, f.DeviceID, time.Now(),
	)
	return err
}

// GetSyncStats returns the total count and size of synced files.
func (d *DB) GetSyncStats() (count int, totalSize int64, err error) {
	err = d.db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(file_size), 0) FROM synced_files",
	).Scan(&count, &totalSize)
	return
}

// AddAudioRecording records a completed phone audio recording.
func (d *DB) AddAudioRecording(r *AudioRecording) error {
	completedAt := r.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now()
	}
	_, err := d.db.Exec(
		`INSERT INTO audio_recordings
		 (device_id, device_name, session_id, file_name, local_path, file_size,
		  sample_rate, channels, bits_per_sample, transcript, started_at, completed_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(local_path) DO UPDATE SET
		   device_id = excluded.device_id,
		   device_name = excluded.device_name,
		   session_id = excluded.session_id,
		   file_name = excluded.file_name,
		   file_size = excluded.file_size,
		   sample_rate = excluded.sample_rate,
		   channels = excluded.channels,
		   bits_per_sample = excluded.bits_per_sample,
		   transcript = excluded.transcript,
		   started_at = excluded.started_at,
		   completed_at = excluded.completed_at`,
		r.DeviceID, r.DeviceName, r.SessionID, r.FileName, r.LocalPath, r.FileSize,
		r.SampleRate, r.Channels, r.BitsPerSample, r.Transcript, r.StartedAt, completedAt, time.Now(),
	)
	return err
}

// GetAudioRecording returns a completed recording by local path, if present.
func (d *DB) GetAudioRecording(localPath string) (*AudioRecording, error) {
	r := &AudioRecording{}
	err := d.db.QueryRow(
		`SELECT id, device_id, device_name, session_id, file_name, local_path, file_size,
		        sample_rate, channels, bits_per_sample, transcript, started_at, completed_at, created_at
		   FROM audio_recordings WHERE local_path = ?`,
		localPath,
	).Scan(
		&r.ID, &r.DeviceID, &r.DeviceName, &r.SessionID, &r.FileName, &r.LocalPath,
		&r.FileSize, &r.SampleRate, &r.Channels, &r.BitsPerSample, &r.Transcript,
		&r.StartedAt, &r.CompletedAt, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// GetAudioRecordingStats returns the total count and size of recorded audio files.
func (d *DB) GetAudioRecordingStats() (count int, totalSize int64, err error) {
	err = d.db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(file_size), 0) FROM audio_recordings",
	).Scan(&count, &totalSize)
	return
}

// SaveDownloadProgress saves or updates partial download state for resume.
func (d *DB) SaveDownloadProgress(p *DownloadProgress) error {
	_, err := d.db.Exec(
		`INSERT INTO download_progress 
		 (content_hash, device_id, original_name, temp_path, bytes_received, total_size, date_taken, media_type, last_updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(content_hash, device_id) DO UPDATE SET
		   bytes_received = excluded.bytes_received,
		   temp_path = excluded.temp_path,
		   last_updated = excluded.last_updated`,
		p.ContentHash, p.DeviceID, p.OriginalName, p.TempPath,
		p.BytesReceived, p.TotalSize, p.DateTaken, p.MediaType, time.Now(),
	)
	return err
}

// GetDownloadProgress returns the partial download state for a file, if it exists.
func (d *DB) GetDownloadProgress(contentHash, deviceID string) (*DownloadProgress, error) {
	p := &DownloadProgress{}
	err := d.db.QueryRow(
		`SELECT content_hash, device_id, original_name, temp_path, bytes_received, total_size, date_taken, media_type, last_updated
		 FROM download_progress WHERE content_hash = ? AND device_id = ?`,
		contentHash, deviceID,
	).Scan(&p.ContentHash, &p.DeviceID, &p.OriginalName, &p.TempPath,
		&p.BytesReceived, &p.TotalSize, &p.DateTaken, &p.MediaType, &p.LastUpdated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// DeleteDownloadProgress removes the partial download state after completion.
func (d *DB) DeleteDownloadProgress(contentHash, deviceID string) error {
	_, err := d.db.Exec(
		"DELETE FROM download_progress WHERE content_hash = ? AND device_id = ?",
		contentHash, deviceID,
	)
	return err
}

// CleanStaledProgress removes download progress entries older than the given duration.
func (d *DB) CleanStaledProgress(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	_, err := d.db.Exec(
		"DELETE FROM download_progress WHERE last_updated < ?",
		cutoff,
	)
	return err
}
