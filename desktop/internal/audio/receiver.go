package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrBusy = errors.New("another audio stream is already active")

type StreamMeta struct {
	DeviceID      string
	DeviceName    string
	SessionID     string
	SampleRate    int
	Channels      int
	BitsPerSample int
}

type Status struct {
	Active               bool   `json:"active"`
	DeviceID             string `json:"device_id,omitempty"`
	DeviceName           string `json:"device_name,omitempty"`
	SessionID            string `json:"session_id,omitempty"`
	SampleRate           int    `json:"sample_rate,omitempty"`
	Channels             int    `json:"channels,omitempty"`
	BitsPerSample        int    `json:"bits_per_sample,omitempty"`
	StartedAt            string `json:"started_at,omitempty"`
	BytesReceived        int64  `json:"bytes_received,omitempty"`
	CurrentFile          string `json:"current_file,omitempty"`
	CurrentPath          string `json:"current_path,omitempty"`
	LastCompletedFile    string `json:"last_completed_file,omitempty"`
	LastCompletedPath    string `json:"last_completed_path,omitempty"`
	LastCompletedAt      string `json:"last_completed_at,omitempty"`
	TranscriptPartial    string `json:"transcript_partial,omitempty"`
	TranscriptFinal      string `json:"transcript_final,omitempty"`
	LastTranscript       string `json:"last_transcript,omitempty"`
	TranscriptError      string `json:"transcript_error,omitempty"`
	TextInjectionEnabled bool   `json:"text_injection_enabled,omitempty"`
	TextInjectionStatus  string `json:"text_injection_status,omitempty"`
	TextInjectionError   string `json:"text_injection_error,omitempty"`
	LastInjectedText     string `json:"last_injected_text,omitempty"`
	LastInjectedAt       string `json:"last_injected_at,omitempty"`
	LastError            string `json:"last_error,omitempty"`
}

type ReceiveResult struct {
	FilePath     string
	FileName     string
	BytesWritten int64
	Transcript   string
	StartedAt    time.Time
	CompletedAt  time.Time
}

type TranscriptUpdate struct {
	Partial string
	Final   string
	Error   string
}

type RealtimeTranscriber interface {
	Start(meta StreamMeta, onUpdate func(TranscriptUpdate)) (RealtimeTranscriptStream, error)
}

type RealtimeTranscriptStream interface {
	WritePCM(p []byte) error
	Commit() error
	Close() error
}

type TextInjector interface {
	InjectText(ctx context.Context, text string) error
}

type Receiver struct {
	targetRoot  string
	transcriber RealtimeTranscriber
	injector    TextInjector

	mu     sync.RWMutex
	status Status
}

func NewReceiver(targetRoot string) *Receiver {
	return &Receiver{targetRoot: targetRoot}
}

func (r *Receiver) SetTranscriber(transcriber RealtimeTranscriber) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transcriber = transcriber
}

func (r *Receiver) SetTextInjector(injector TextInjector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.injector = injector
	r.status.TextInjectionEnabled = injector != nil
	if injector == nil {
		r.status.TextInjectionStatus = ""
	}
}

func (r *Receiver) Status() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *Receiver) Receive(meta StreamMeta, body io.Reader) (*ReceiveResult, error) {
	if meta.DeviceID == "" {
		return nil, errors.New("device id is required")
	}
	if meta.SampleRate <= 0 {
		return nil, errors.New("sample rate must be positive")
	}
	if meta.Channels <= 0 {
		return nil, errors.New("channels must be positive")
	}
	if meta.BitsPerSample <= 0 || meta.BitsPerSample%8 != 0 {
		return nil, errors.New("bits per sample must be a positive multiple of 8")
	}

	now := time.Now()
	sessionID := sanitizeSegment(meta.SessionID)
	if sessionID == "" {
		sessionID = now.Format("20060102-150405")
	}

	deviceName := meta.DeviceName
	if strings.TrimSpace(deviceName) == "" {
		deviceName = meta.DeviceID
	}
	deviceSlug := sanitizeSegment(deviceName)
	if deviceSlug == "" {
		deviceSlug = "device"
	}

	recordingsDir := filepath.Join(
		r.targetRoot,
		"Audio",
		now.Format("2006"),
		now.Format("2006-01-02"),
	)
	if err := os.MkdirAll(recordingsDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create recordings directory: %w", err)
	}

	fileName := fmt.Sprintf(
		"%s-%s-%s.wav",
		now.Format("15-04-05"),
		deviceSlug,
		sessionID,
	)
	finalPath := filepath.Join(recordingsDir, fileName)
	tempPath := finalPath + ".partial"

	if err := r.begin(Status{
		Active:        true,
		DeviceID:      meta.DeviceID,
		DeviceName:    deviceName,
		SessionID:     sessionID,
		SampleRate:    meta.SampleRate,
		Channels:      meta.Channels,
		BitsPerSample: meta.BitsPerSample,
		StartedAt:     now.Format(time.RFC3339),
		CurrentFile:   fileName,
		CurrentPath:   finalPath,
	}); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		r.finishWithError(err)
		return nil, fmt.Errorf("failed to create temp recording: %w", err)
	}

	writer := &wavWriter{
		file:          file,
		sampleRate:    meta.SampleRate,
		channels:      meta.Channels,
		bitsPerSample: meta.BitsPerSample,
	}

	bytesWritten := int64(0)
	success := false
	defer func() {
		if !success {
			file.Close()
			_ = os.Remove(tempPath)
		}
	}()

	if err := writer.WriteHeader(); err != nil {
		r.finishWithError(err)
		return nil, fmt.Errorf("failed to write wav header: %w", err)
	}

	transcriptStream := r.startTranscriptStream(meta)
	if transcriptStream != nil {
		defer func() {
			if transcriptStream != nil {
				_ = transcriptStream.Close()
			}
		}()
	}

	buf := make([]byte, 32*1024)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			written, writeErr := writer.Write(buf[:n])
			bytesWritten += int64(written)
			r.updateBytes(bytesWritten)
			if writeErr != nil {
				r.finishWithError(writeErr)
				return nil, fmt.Errorf("failed to write audio data: %w", writeErr)
			}
			if transcriptStream != nil {
				if err := transcriptStream.WritePCM(buf[:n]); err != nil {
					r.updateTranscript(TranscriptUpdate{Error: err.Error()})
					_ = transcriptStream.Close()
					transcriptStream = nil
				}
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			r.finishWithError(readErr)
			return nil, fmt.Errorf("failed to read audio stream: %w", readErr)
		}
	}

	if transcriptStream != nil {
		if err := transcriptStream.Commit(); err != nil {
			r.updateTranscript(TranscriptUpdate{Error: err.Error()})
		}
	}

	if err := writer.Finalize(); err != nil {
		r.finishWithError(err)
		return nil, fmt.Errorf("failed to finalize wav: %w", err)
	}

	if err := file.Close(); err != nil {
		r.finishWithError(err)
		return nil, fmt.Errorf("failed to close recording: %w", err)
	}

	if err := os.Rename(tempPath, finalPath); err != nil {
		r.finishWithError(err)
		return nil, fmt.Errorf("failed to finalize recording path: %w", err)
	}

	success = true
	completedAt := time.Now()
	r.finishSuccess(fileName, finalPath)
	status := r.Status()

	return &ReceiveResult{
		FilePath:     finalPath,
		FileName:     fileName,
		BytesWritten: bytesWritten,
		Transcript:   status.LastTranscript,
		StartedAt:    now,
		CompletedAt:  completedAt,
	}, nil
}

func (r *Receiver) startTranscriptStream(meta StreamMeta) RealtimeTranscriptStream {
	r.mu.RLock()
	transcriber := r.transcriber
	r.mu.RUnlock()
	if transcriber == nil {
		return nil
	}

	stream, err := transcriber.Start(meta, r.updateTranscript)
	if err != nil {
		r.updateTranscript(TranscriptUpdate{Error: err.Error()})
		return nil
	}
	return stream
}

func (r *Receiver) begin(next Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.Active {
		return ErrBusy
	}
	next.LastCompletedFile = r.status.LastCompletedFile
	next.LastCompletedPath = r.status.LastCompletedPath
	next.LastCompletedAt = r.status.LastCompletedAt
	next.LastTranscript = r.status.LastTranscript
	next.TextInjectionEnabled = r.status.TextInjectionEnabled
	next.TextInjectionStatus = r.status.TextInjectionStatus
	next.TextInjectionError = r.status.TextInjectionError
	next.LastInjectedText = r.status.LastInjectedText
	next.LastInjectedAt = r.status.LastInjectedAt
	next.TranscriptPartial = ""
	next.TranscriptFinal = ""
	next.TranscriptError = ""
	next.LastError = ""
	r.status = next
	return nil
}

func (r *Receiver) updateBytes(bytesWritten int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.status.Active {
		return
	}
	r.status.BytesReceived = bytesWritten
}

func (r *Receiver) updateTranscript(update TranscriptUpdate) {
	final := ""
	r.mu.Lock()
	if update.Partial != "" {
		r.status.TranscriptPartial = update.Partial
	}
	if update.Final != "" {
		r.status.TranscriptFinal = update.Final
		r.status.LastTranscript = update.Final
		r.status.TranscriptPartial = ""
		final = update.Final
	}
	if update.Error != "" {
		r.status.TranscriptError = update.Error
	}
	r.mu.Unlock()

	if final != "" {
		r.injectText(final)
	}
}

func (r *Receiver) injectText(text string) {
	r.mu.RLock()
	injector := r.injector
	r.mu.RUnlock()
	if injector == nil || strings.TrimSpace(text) == "" {
		return
	}

	r.mu.Lock()
	r.status.TextInjectionEnabled = true
	r.status.TextInjectionStatus = "injecting"
	r.status.TextInjectionError = ""
	r.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := injector.InjectText(ctx, text)

		r.mu.Lock()
		defer r.mu.Unlock()
		if err != nil {
			r.status.TextInjectionStatus = "error"
			r.status.TextInjectionError = err.Error()
			return
		}
		r.status.TextInjectionStatus = "ok"
		r.status.TextInjectionError = ""
		r.status.LastInjectedText = text
		r.status.LastInjectedAt = time.Now().Format(time.RFC3339)
	}()
}

func (r *Receiver) finishSuccess(fileName, filePath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Active = false
	r.status.BytesReceived = 0
	r.status.CurrentFile = ""
	r.status.CurrentPath = ""
	r.status.LastCompletedFile = fileName
	r.status.LastCompletedPath = filePath
	r.status.LastCompletedAt = time.Now().Format(time.RFC3339)
	if r.status.TranscriptFinal != "" {
		r.status.LastTranscript = r.status.TranscriptFinal
	}
	r.status.LastError = ""
}

func (r *Receiver) finishWithError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Active = false
	r.status.BytesReceived = 0
	r.status.CurrentFile = ""
	r.status.CurrentPath = ""
	r.status.LastError = err.Error()
}

type wavWriter struct {
	file          *os.File
	sampleRate    int
	channels      int
	bitsPerSample int
	dataSize      int64
}

func (w *wavWriter) WriteHeader() error {
	header := make([]byte, 44)
	copy(header[0:4], []byte("RIFF"))
	copy(header[8:12], []byte("WAVE"))
	copy(header[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(w.channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(w.sampleRate))
	blockAlign := uint16(w.channels * (w.bitsPerSample / 8))
	byteRate := uint32(w.sampleRate) * uint32(blockAlign)
	binary.LittleEndian.PutUint32(header[28:32], byteRate)
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], uint16(w.bitsPerSample))
	copy(header[36:40], []byte("data"))

	_, err := w.file.Write(header)
	return err
}

func (w *wavWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	w.dataSize += int64(n)
	return n, err
}

func (w *wavWriter) Finalize() error {
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}

	header := make([]byte, 44)
	copy(header[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+w.dataSize))
	copy(header[8:12], []byte("WAVE"))
	copy(header[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(w.channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(w.sampleRate))
	blockAlign := uint16(w.channels * (w.bitsPerSample / 8))
	byteRate := uint32(w.sampleRate) * uint32(blockAlign)
	binary.LittleEndian.PutUint32(header[28:32], byteRate)
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], uint16(w.bitsPerSample))
	copy(header[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(header[40:44], uint32(w.dataSize))

	_, err := w.file.Write(header)
	return err
}

func sanitizeSegment(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}

	return strings.Trim(b.String(), "-.")
}
