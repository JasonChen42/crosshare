package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type speechStatus struct {
	Status                    string                         `json:"status,omitempty"`
	Capability                string                         `json:"capability,omitempty"`
	RealtimeURL               string                         `json:"realtime_url,omitempty"`
	TranscriptionURL          string                         `json:"transcription_url,omitempty"`
	ModelsURL                 string                         `json:"models_url,omitempty"`
	GatewayHealthURL          string                         `json:"gateway_health_url,omitempty"`
	DirectRealtimeURL         string                         `json:"direct_realtime_url,omitempty"`
	DirectRealtimeHealthURL   string                         `json:"direct_realtime_health_url,omitempty"`
	OfflineHealthURL          string                         `json:"offline_health_url,omitempty"`
	RealtimeModel             string                         `json:"realtime_model,omitempty"`
	Services                  map[string]speechServiceStatus `json:"services,omitempty"`
	Health                    map[string]interface{}         `json:"health,omitempty"`
	Ready                     bool                           `json:"ready"`
	Error                     string                         `json:"error,omitempty"`
	LastCheckedAt             string                         `json:"last_checked_at,omitempty"`
	AutoStartAttempted        bool                           `json:"auto_start_attempted,omitempty"`
	AutoStartError            string                         `json:"auto_start_error,omitempty"`
	AutoStartCompletedAt      string                         `json:"auto_start_completed_at,omitempty"`
	AutoStartCommandAvailable bool                           `json:"auto_start_command_available"`
}

type speechServiceStatus struct {
	Status  string `json:"status,omitempty"`
	Running bool   `json:"running"`
	URL     string `json:"url,omitempty"`
}

type speechRuntimeState struct {
	autoStartAttempted   bool
	autoStartError       string
	autoStartCompletedAt string
	commandAvailable     bool
}

func (s *Server) maybeStartSpeechRuntime() {
	if !speechAutoStartEnabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		status, err := runLocalmanSpeech(ctx, "start")

		s.speechMu.Lock()
		defer s.speechMu.Unlock()
		s.speechState.autoStartAttempted = true
		s.speechState.autoStartCompletedAt = time.Now().Format(time.RFC3339)
		s.speechState.commandAvailable = err == nil || !errors.Is(err, errLocalmanUnavailable)
		if err != nil {
			s.speechState.autoStartError = err.Error()
			log.Printf("[server] speech runtime auto-start failed: %v", err)
			return
		}
		if status.Status != "" {
			log.Printf("[server] speech runtime auto-start status: %s", status.Status)
		}
	}()
}

func (s *Server) speechStatus() speechStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	status, err := runLocalmanSpeech(ctx, "status")
	if err != nil {
		status = speechStatus{Error: err.Error()}
	}

	status.LastCheckedAt = time.Now().Format(time.RFC3339)
	status.Ready = status.isReady()

	s.speechMu.Lock()
	state := s.speechState
	s.speechMu.Unlock()
	status.AutoStartAttempted = state.autoStartAttempted
	status.AutoStartError = state.autoStartError
	status.AutoStartCompletedAt = state.autoStartCompletedAt
	status.AutoStartCommandAvailable = state.commandAvailable
	return status
}

func (s speechStatus) serviceRunning(name string) bool {
	if s.Services == nil {
		return false
	}
	service, ok := s.Services[name]
	return ok && service.Running
}

func (s speechStatus) isReady() bool {
	return strings.EqualFold(s.Status, "ready") && s.serviceRunning("stt-realtime")
}

var errLocalmanUnavailable = errors.New("localman command is unavailable")

func runLocalmanSpeech(ctx context.Context, action string) (speechStatus, error) {
	localman, err := findLocalman()
	if err != nil {
		return speechStatus{}, err
	}

	cmd := exec.CommandContext(ctx, localman, "--speech", action, "--json")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return speechStatus{}, fmt.Errorf("localman --speech %s timed out", action)
	}
	if err != nil {
		return speechStatus{}, fmt.Errorf("localman --speech %s failed: %w: %s", action, err, strings.TrimSpace(string(output)))
	}

	status, err := parseSpeechStatus(output)
	if err != nil {
		return speechStatus{}, fmt.Errorf("parse localman --speech %s output: %w", action, err)
	}
	return status, nil
}

func parseSpeechStatus(output []byte) (speechStatus, error) {
	text := strings.TrimSpace(string(output))
	start := strings.Index(text, "{")
	if start < 0 {
		return speechStatus{}, fmt.Errorf("missing JSON object in output: %s", text)
	}

	var status speechStatus
	if err := json.Unmarshal([]byte(text[start:]), &status); err != nil {
		return speechStatus{}, err
	}
	return status, nil
}

func findLocalman() (string, error) {
	if path, err := exec.LookPath("localman"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".local", "bin", "localman")
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", errLocalmanUnavailable
}

func speechAutoStartEnabled() bool {
	value := strings.TrimSpace(os.Getenv("CROSSHARE_SPEECH_AUTOSTART"))
	if value == "" {
		return true
	}
	switch strings.ToLower(value) {
	case "0", "false", "off", "disabled", "none", "no":
		return false
	default:
		return true
	}
}
