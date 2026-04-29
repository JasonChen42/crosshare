package server

import "testing"

func TestParseSpeechStatusSkipsStartLogs(t *testing.T) {
	status, err := parseSpeechStatus([]byte(`started app: speech-runtime
service already running: gateway
{
  "status": "ready",
  "realtime_url": "ws://127.0.0.1:8000/v1/audio/realtime",
  "services": {
    "gateway": {"status": "running", "running": true},
    "stt-realtime": {"status": "running", "running": true}
  }
}`))
	if err != nil {
		t.Fatalf("parseSpeechStatus() error = %v", err)
	}
	if status.Status != "ready" {
		t.Fatalf("Status = %q, want ready", status.Status)
	}
	if status.RealtimeURL != "ws://127.0.0.1:8000/v1/audio/realtime" {
		t.Fatalf("RealtimeURL = %q", status.RealtimeURL)
	}
	if !status.serviceRunning("gateway") || !status.serviceRunning("stt-realtime") {
		t.Fatalf("services = %+v, want gateway and stt-realtime running", status.Services)
	}
}
