package server

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crosshare/desktop/internal/audio"
	"github.com/crosshare/desktop/internal/config"
	"github.com/crosshare/desktop/internal/injector"
	"github.com/crosshare/desktop/internal/organizer"
	"github.com/crosshare/desktop/internal/storage"
	csync "github.com/crosshare/desktop/internal/sync"
	"github.com/crosshare/desktop/internal/transcript"
)

// Server is the HTTP server that handles API requests from phones.
type Server struct {
	cfg           *config.Config
	db            *storage.DB
	syncMgr       *csync.Manager
	audioReceiver *audio.Receiver
	server        *http.Server
	pairLimiter   *pairLimiter
	speechMu      sync.Mutex
	speechState   speechRuntimeState
}

// New creates a new HTTP server.
func New(cfg *config.Config, db *storage.DB, syncMgr *csync.Manager) *Server {
	audioReceiver := audio.NewReceiver(cfg.GetTargetFolder())
	if transcriptURL := transcriptURLFromEnv(); transcriptURL != "" {
		log.Printf("[server] realtime audio transcript websocket: %s", transcriptURL)
		client := transcript.NewClient(transcriptURL)
		client.Language = transcriptLanguageFromEnv()
		log.Printf("[server] realtime audio transcript language: %s", client.Language)
		audioReceiver.SetTranscriber(client)
	}
	if injector.EnabledFromEnv() {
		textInjector, err := injector.NewFromEnv()
		if err != nil {
			log.Printf("[server] text injection disabled: %v", err)
		} else {
			log.Printf("[server] text injection enabled: %s", injector.ModeFromEnv())
			audioReceiver.SetTextInjector(textInjector)
			if injector.PreauthorizeFromEnv() {
				go preauthorizeTextInjection(textInjector)
			}
		}
	}

	s := &Server{
		cfg:           cfg,
		db:            db,
		syncMgr:       syncMgr,
		audioReceiver: audioReceiver,
		pairLimiter:   newPairLimiter(),
	}

	mux := http.NewServeMux()

	// Local browser UI.
	mux.HandleFunc("GET /", s.handleUI)
	mux.HandleFunc("GET /api/ui/status", s.localOnly(s.handleUIStatus))

	// Public endpoints (no auth required)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/pair", s.handlePair)

	// Authenticated endpoints
	mux.HandleFunc("GET /api/synced", s.authMiddleware(s.handleSyncedList))
	mux.HandleFunc("GET /api/stats", s.authMiddleware(s.handleStats))
	mux.HandleFunc("POST /api/media/upload/{hash}", s.authMiddleware(s.handleMediaUpload))
	mux.HandleFunc("POST /api/audio/stream", s.authMiddleware(s.handleAudioStream))
	mux.HandleFunc("GET /api/audio/status", s.authMiddleware(s.handleAudioStatus))

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      0, // No write timeout for large file uploads or audio streams
		IdleTimeout:       120 * time.Second,
	}

	s.maybeStartSpeechRuntime()

	return s
}

func preauthorizeTextInjection(textInjector *injector.PortalUnicode) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	log.Printf("[server] requesting text injection portal authorization")
	if err := textInjector.Preauthorize(ctx); err != nil {
		log.Printf("[server] text injection portal authorization failed: %v", err)
		return
	}
	log.Printf("[server] text injection portal authorization ready")
}

func transcriptURLFromEnv() string {
	value := strings.TrimSpace(os.Getenv("CROSSHARE_TRANSCRIPT_WS"))
	if value == "" {
		return transcript.DefaultURL
	}
	switch strings.ToLower(value) {
	case "0", "false", "off", "disabled", "none":
		return ""
	default:
		return value
	}
}

func transcriptLanguageFromEnv() string {
	value := strings.TrimSpace(os.Getenv("CROSSHARE_TRANSCRIPT_LANGUAGE"))
	if value == "" {
		return "auto"
	}
	return value
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	log.Printf("[server] starting HTTP server on %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop gracefully stops the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// authMiddleware wraps a handler with token authentication.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}

		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "missing authorization token",
			})
			return
		}

		device := s.cfg.FindDeviceByToken(token)
		if device == nil {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "invalid token",
			})
			return
		}

		if host, _, err := netSplitHostPort(r.RemoteAddr); err == nil {
			if err := s.cfg.UpdateDeviceSeenByToken(token, host); err != nil {
				log.Printf("[server] failed to update device last seen: %v", err)
			}
		}

		// Store device info in context
		ctx := context.WithValue(r.Context(), "device", device)
		next(w, r.WithContext(ctx))
	}
}

// handleStatus returns the computer's status info (no auth required).
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	count, totalSize, _ := s.db.GetSyncStats()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"device_name":    s.cfg.DeviceName,
		"api_version":    "1",
		"paired_devices": s.cfg.GetPairedDeviceCount(),
		"synced_count":   count,
		"synced_size":    totalSize,
	})
}

// handlePair processes a pairing request from a phone.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	clientKey := pairClientKey(r)
	now := time.Now()
	if blockedUntil, blocked := s.pairLimiter.blocked(clientKey, now); blocked {
		writeRetryAfter(w, blockedUntil, now)
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "too many invalid pair attempts",
		})
		return
	}

	var req struct {
		PairCode   string `json:"pair_code"`
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
		return
	}

	if subtle.ConstantTimeCompare([]byte(req.PairCode), []byte(s.cfg.GetPairCode())) != 1 {
		if blockedUntil, blocked := s.pairLimiter.recordFailure(clientKey, now); blocked {
			writeRetryAfter(w, blockedUntil, now)
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "too many invalid pair attempts",
			})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "invalid pair code",
		})
		return
	}

	if req.DeviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "device_id is required",
		})
		return
	}

	// Check if device is already paired
	for _, d := range s.cfg.GetPairedDevices() {
		if d.DeviceID == req.DeviceID {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"token":       d.Token,
				"device_name": s.cfg.DeviceName,
				"message":     "already paired",
			})
			return
		}
	}

	// Create new pairing
	token := config.GenerateToken()
	device := config.PairedDevice{
		DeviceID:   req.DeviceID,
		DeviceName: req.DeviceName,
		Token:      token,
		PairedAt:   time.Now().Format(time.RFC3339),
	}

	if err := s.cfg.AddPairedDevice(device); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to save pairing",
		})
		return
	}

	s.pairLimiter.recordSuccess(clientKey)

	log.Printf("[server] paired with device: %s (%s)", req.DeviceName, req.DeviceID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":       token,
		"device_name": s.cfg.DeviceName,
		"message":     "paired successfully",
	})
}

// handleSyncedList returns the list of already-synced content hashes.
func (s *Server) handleSyncedList(w http.ResponseWriter, r *http.Request) {
	hashes, err := s.db.GetSyncedHashes()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to query synced files",
		})
		return
	}

	hashList := make([]string, 0, len(hashes))
	for h := range hashes {
		hashList = append(hashList, h)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hashes": hashList,
		"count":  len(hashList),
	})
}

// handleStats returns sync statistics.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	count, totalSize, err := s.db.GetSyncStats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to query stats",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"synced_count": count,
		"synced_size":  totalSize,
		"target":       s.cfg.GetTargetFolder(),
	})
}

func (s *Server) handleAudioStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.audioReceiver.Status())
}

func (s *Server) handleAudioStream(w http.ResponseWriter, r *http.Request) {
	device, _ := r.Context().Value("device").(*config.PairedDevice)
	if device == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing paired device"})
		return
	}

	meta, err := parseAudioStreamMeta(r, device)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	defer r.Body.Close()
	result, err := s.audioReceiver.Receive(meta, r.Body)
	if err != nil {
		switch {
		case errors.Is(err, audio.ErrBusy):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	log.Printf("[server] received audio stream from %s -> %s (%d bytes)", device.DeviceName, result.FileName, result.BytesWritten)
	if err := s.db.AddAudioRecording(&storage.AudioRecording{
		DeviceID:      meta.DeviceID,
		DeviceName:    meta.DeviceName,
		SessionID:     meta.SessionID,
		FileName:      result.FileName,
		LocalPath:     result.FilePath,
		FileSize:      result.BytesWritten,
		SampleRate:    meta.SampleRate,
		Channels:      meta.Channels,
		BitsPerSample: meta.BitsPerSample,
		Transcript:    result.Transcript,
		StartedAt:     result.StartedAt,
		CompletedAt:   result.CompletedAt,
	}); err != nil {
		log.Printf("[server] failed to record audio metadata: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record audio metadata"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"uploaded":   true,
		"bytes":      result.BytesWritten,
		"file":       result.FileName,
		"path":       result.FilePath,
		"transcript": result.Transcript,
	})
}

// handleMediaUpload accepts a media file pushed by a paired phone.
func (s *Server) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	device, _ := r.Context().Value("device").(*config.PairedDevice)
	if device == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing paired device"})
		return
	}

	hash := r.PathValue("hash")
	if hash == "" || strings.Contains(hash, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid content hash"})
		return
	}

	if has, err := s.db.HasFile(hash); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check synced file"})
		return
	} else if has {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"hash":    hash,
			"skipped": true,
			"message": "already synced",
		})
		return
	}

	originalName := decodeHeaderBase64(r.Header.Get("X-CrossHare-File-Name-Base64"))
	if originalName == "" {
		originalName = hash
	}
	originalName = filepath.Base(originalName)
	if originalName == "." || originalName == string(filepath.Separator) {
		originalName = hash
	}

	declaredSize, _ := strconv.ParseInt(r.Header.Get("X-CrossHare-File-Size"), 10, 64)
	if declaredSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, declaredSize+1)
	}
	defer r.Body.Close()

	tmpDir := filepath.Join(config.ConfigDir(), "uploads")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create upload directory"})
		return
	}

	tmpFile, err := os.CreateTemp(tmpDir, hash+"-*"+filepath.Ext(originalName))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
		return
	}
	tmpPath := tmpFile.Name()
	copied, copyErr := io.Copy(tmpFile, r.Body)
	closeErr := tmpFile.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmpPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to receive upload"})
		return
	}
	if declaredSize > 0 && copied != declaredSize {
		os.Remove(tmpPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload size mismatch"})
		return
	}

	dateTaken := timeFromMillis(r.Header.Get("X-CrossHare-Date-Taken-Ms"))
	org := organizer.New(s.cfg.GetTargetFolder())
	finalPath, err := org.OrganizeFile(tmpPath, originalName, dateTaken)
	if err != nil {
		os.Remove(tmpPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store uploaded file"})
		return
	}

	mediaType := r.Header.Get("X-CrossHare-Media-Type")
	if mediaType == "" {
		mediaType = "photo"
	}

	if err := s.db.AddSyncedFile(&storage.SyncedFile{
		ContentHash:  hash,
		OriginalName: originalName,
		DateTaken:    dateTaken,
		LocalPath:    finalPath,
		FileSize:     copied,
		MediaType:    mediaType,
		DeviceID:     device.DeviceID,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record uploaded file"})
		return
	}

	log.Printf("[server] received %s from %s (%d bytes)", originalName, device.DeviceName, copied)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hash":     hash,
		"uploaded": true,
		"bytes":    copied,
	})
}

func decodeHeaderBase64(value string) string {
	if value == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func timeFromMillis(value string) time.Time {
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ms*int64(time.Millisecond))
}

func parseAudioStreamMeta(r *http.Request, device *config.PairedDevice) (audio.StreamMeta, error) {
	sampleRate, err := strconv.Atoi(r.Header.Get("X-CrossHare-Audio-Sample-Rate"))
	if err != nil || sampleRate <= 0 {
		return audio.StreamMeta{}, errors.New("invalid audio sample rate")
	}

	channels, err := strconv.Atoi(r.Header.Get("X-CrossHare-Audio-Channels"))
	if err != nil || channels <= 0 {
		return audio.StreamMeta{}, errors.New("invalid audio channels")
	}

	bitsPerSample, err := strconv.Atoi(r.Header.Get("X-CrossHare-Audio-Bits-Per-Sample"))
	if err != nil || bitsPerSample <= 0 {
		return audio.StreamMeta{}, errors.New("invalid bits per sample")
	}

	return audio.StreamMeta{
		DeviceID:      device.DeviceID,
		DeviceName:    device.DeviceName,
		SessionID:     r.Header.Get("X-CrossHare-Audio-Session-ID"),
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: bitsPerSample,
	}, nil
}

func netSplitHostPort(remoteAddr string) (string, string, error) {
	host, port, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr, "", err
	}
	return host, port, nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
