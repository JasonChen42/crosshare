package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *recordingRunner) run(name string, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
}

func (r *recordingRunner) all() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

func (r *recordingRunner) waitFor(t *testing.T, count int) [][]string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls := r.all(); len(calls) >= count {
			return r.all()
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d notifications, got %+v", count, r.all())
	return nil
}

func TestUploadNotifierBatchesBurstIntoOneNotice(t *testing.T) {
	rec := &recordingRunner{}
	notifier := &uploadNotifier{port: 8384, run: rec.run, debounce: 5 * time.Millisecond}
	for i := 0; i < 3; i++ {
		notifier.saved()
	}
	calls := rec.waitFor(t, 1)
	if len(calls) != 1 {
		t.Fatalf("expected one batched notification, got %+v", calls)
	}
	joined := strings.Join(calls[0], " ")
	if calls[0][0] != "notify-send" {
		t.Fatalf("wrong command: %+v", calls[0])
	}
	if !strings.Contains(joined, "已保存 3 项新照片") || !strings.Contains(joined, "http://127.0.0.1:8384/") {
		t.Fatalf("missing count or library address: %s", joined)
	}
	// A quiet period must not resend the same batch.
	time.Sleep(30 * time.Millisecond)
	if calls := rec.all(); len(calls) != 1 {
		t.Fatalf("notification resent: %+v", calls)
	}
}

func TestUploadNotifierReportsSeparateBursts(t *testing.T) {
	rec := &recordingRunner{}
	notifier := &uploadNotifier{port: 9000, run: rec.run, debounce: 5 * time.Millisecond}
	notifier.saved()
	rec.waitFor(t, 1)
	notifier.saved()
	notifier.saved()
	calls := rec.waitFor(t, 2)
	if len(calls) != 2 {
		t.Fatalf("expected two notifications, got %+v", calls)
	}
	if !strings.Contains(strings.Join(calls[0], " "), "已保存 1 项") {
		t.Fatalf("first burst wrong: %+v", calls[0])
	}
	if !strings.Contains(strings.Join(calls[1], " "), "已保存 2 项") || !strings.Contains(strings.Join(calls[1], " "), "http://127.0.0.1:9000/") {
		t.Fatalf("second burst wrong: %+v", calls[1])
	}
}

func TestNotificationCommandPerPlatform(t *testing.T) {
	name, args := notificationCommand("linux", "CrossHare", "已保存 1 项新照片")
	if name != "notify-send" || len(args) != 2 || args[0] != "CrossHare" || args[1] != "已保存 1 项新照片" {
		t.Fatalf("linux command: %s %v", name, args)
	}
	name, args = notificationCommand("darwin", "CrossHare", "hi")
	if name != "osascript" || len(args) != 2 || args[0] != "-e" || !strings.Contains(args[1], "display notification") || !strings.Contains(args[1], "CrossHare") {
		t.Fatalf("darwin command: %s %v", name, args)
	}
	name, args = notificationCommand("windows", "CrossHare", "hi")
	if name != "notify-send" {
		t.Fatalf("fallback command: %s %v", name, args)
	}
}

func TestSaveNotificationOnlyForPhoneUploads(t *testing.T) {
	srv, device, _ := testUploadServer(t)
	rec := &recordingRunner{}
	srv.uploads.run = rec.run
	srv.uploads.debounce = 5 * time.Millisecond

	phoneBody := pngBytes(t, 4, 4)
	hash := fmt.Sprintf("%x", sha256.Sum256(phoneBody))
	req := httptest.NewRequest("POST", "/api/media/upload/"+hash, bytes.NewReader(phoneBody))
	req.SetPathValue("hash", hash)
	req.Header.Set("X-CrossHare-File-Size", fmt.Sprint(len(phoneBody)))
	req.Header.Set("X-CrossHare-File-Name-Base64", base64.StdEncoding.EncodeToString([]byte("photo.png")))
	req = req.WithContext(context.WithValue(req.Context(), "device", device))
	recorder := httptest.NewRecorder()
	srv.handleMediaUpload(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("phone upload: %d %s", recorder.Code, recorder.Body.String())
	}

	// A browser import lands in the same library but must stay silent.
	importReq := httptest.NewRequest("POST", "http://127.0.0.1/api/ui/library/import", bytes.NewReader(pngBytes(t, 5, 3)))
	importReq.RemoteAddr = "127.0.0.1:9"
	importReq.Header.Set("X-CrossHare-UI", "1")
	importRecorder := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(importRecorder, importReq)
	if importRecorder.Code != 200 {
		t.Fatalf("browser import: %d %s", importRecorder.Code, importRecorder.Body.String())
	}

	calls := rec.waitFor(t, 1)
	if len(calls) != 1 {
		t.Fatalf("expected exactly one phone notification, got %+v", calls)
	}
	if !strings.Contains(strings.Join(calls[0], " "), "已保存 1 项") {
		t.Fatalf("notification body: %+v", calls[0])
	}
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
