package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crosshare/desktop/internal/config"
	"github.com/crosshare/desktop/internal/storage"
)

func testUploadServer(t *testing.T) (*Server, *config.PairedDevice, *storage.DB) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CROSSHARE_SPEECH_AUTOSTART", "0")
	t.Setenv("CROSSHARE_TRANSCRIPT_WS", "0")

	target := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.TargetFolder = target
	device := &config.PairedDevice{DeviceID: "phone-1", DeviceName: "Pixel", Token: "token"}
	db, err := storage.OpenAt(filepath.Join(t.TempDir(), "crosshare.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(cfg, db, nil), device, db
}

func TestHandleMediaUploadVerifiesHash(t *testing.T) {
	srv, device, db := testUploadServer(t)
	body := []byte("actual media")
	wrongHash := strings.Repeat("0", 64)
	req := httptest.NewRequest(http.MethodPost, "/api/media/upload/"+wrongHash, bytes.NewReader(body))
	req.SetPathValue("hash", wrongHash)
	req.Header.Set("X-CrossHare-File-Size", fmt.Sprint(len(body)))
	req = req.WithContext(context.WithValue(req.Context(), "device", device))
	recorder := httptest.NewRecorder()

	srv.handleMediaUpload(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	count, _, err := db.GetSyncStats()
	if err != nil || count != 0 {
		t.Fatalf("unexpected database state count=%d err=%v", count, err)
	}
}

func TestHandleMediaUploadSanitizesNameAndStoresContent(t *testing.T) {
	srv, device, db := testUploadServer(t)
	body := []byte("actual media")
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	req := httptest.NewRequest(http.MethodPost, "/api/media/upload/"+hash, bytes.NewReader(body))
	req.SetPathValue("hash", hash)
	req.Header.Set("X-CrossHare-File-Size", fmt.Sprint(len(body)))
	req.Header.Set("X-CrossHare-File-Name-Base64", base64.StdEncoding.EncodeToString([]byte("../../photo.jpg")))
	req = req.WithContext(context.WithValue(req.Context(), "device", device))
	recorder := httptest.NewRecorder()

	srv.handleMediaUpload(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	count, size, err := db.GetSyncStats()
	if err != nil || count != 1 || size != int64(len(body)) {
		t.Fatalf("unexpected database state count=%d size=%d err=%v", count, size, err)
	}
	storedPath := ""
	err = filepath.Walk(srv.cfg.GetTargetFolder(), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			storedPath = path
		}
		return nil
	})
	if err != nil || storedPath == "" {
		t.Fatalf("failed to find stored file: path=%q err=%v", storedPath, err)
	}
	if filepath.Base(storedPath) != "photo.jpg" {
		t.Fatalf("unsafe stored path %s", storedPath)
	}
	data, err := os.ReadFile(storedPath)
	if err != nil || !bytes.Equal(data, body) {
		t.Fatalf("stored content mismatch: %q err=%v", data, err)
	}
}

func TestParseAudioStreamMetaRejectsUnsupportedFormat(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/audio/stream", nil)
	req.Header.Set("X-CrossHare-Audio-Sample-Rate", "48000")
	req.Header.Set("X-CrossHare-Audio-Channels", "1")
	req.Header.Set("X-CrossHare-Audio-Bits-Per-Sample", "16")
	if _, err := parseAudioStreamMeta(req, &config.PairedDevice{DeviceID: "phone"}); err == nil {
		t.Fatal("expected unsupported audio format to be rejected")
	}
}

func TestListenReportsPortConflict(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	srv := &Server{server: &http.Server{Addr: occupied.Addr().String()}}
	if listener, err := srv.Listen(); err == nil {
		listener.Close()
		t.Fatal("expected a synchronous bind error")
	}
}

func TestHandleMediaUploadRequiresDeclaredSize(t *testing.T) {
	srv, device, _ := testUploadServer(t)
	body := []byte("actual media")
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	req := httptest.NewRequest(http.MethodPost, "/api/media/upload/"+hash, bytes.NewReader(body))
	req.SetPathValue("hash", hash)
	req = req.WithContext(context.WithValue(req.Context(), "device", device))
	recorder := httptest.NewRecorder()

	srv.handleMediaUpload(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}
