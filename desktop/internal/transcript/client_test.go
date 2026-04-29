package transcript

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/crosshare/desktop/internal/audio"
	"github.com/gorilla/websocket"
)

func TestClientStreamsPCMAndReceivesTranscript(t *testing.T) {
	var received []byte
	var binaryFrames int
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error = %v", err)
			return
		}
		defer conn.Close()

		if err := conn.WriteJSON(map[string]string{"type": "session.started"}); err != nil {
			t.Errorf("WriteJSON(session.started) error = %v", err)
			return
		}

		messageType, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage(session.update) error = %v", err)
			return
		}
		if messageType != websocket.TextMessage {
			t.Errorf("session.update message type = %d, want text", messageType)
			return
		}
		var update struct {
			Type    string `json:"type"`
			Session struct {
				SampleRate        int `json:"sample_rate_hz"`
				Channels          int `json:"channels"`
				PartialIntervalMS int `json:"partial_interval_ms"`
			} `json:"session"`
		}
		if err := json.Unmarshal(data, &update); err != nil {
			t.Errorf("Unmarshal(session.update) error = %v", err)
			return
		}
		if update.Type != "session.update" || update.Session.SampleRate != 16000 || update.Session.Channels != 1 || update.Session.PartialIntervalMS != DefaultPartialIntervalMS {
			t.Errorf("unexpected session.update: %+v", update)
			return
		}
		if err := conn.WriteJSON(map[string]string{"type": "session.updated"}); err != nil {
			t.Errorf("WriteJSON(session.updated) error = %v", err)
			return
		}

		for {
			messageType, data, err = conn.ReadMessage()
			if err != nil {
				t.Errorf("ReadMessage(stream) error = %v", err)
				return
			}
			switch messageType {
			case websocket.BinaryMessage:
				binaryFrames += 1
				received = append(received, data...)
				if err := conn.WriteJSON(map[string]string{"type": "transcript.partial", "text": "hello"}); err != nil {
					t.Errorf("WriteJSON(partial) error = %v", err)
					return
				}
			case websocket.TextMessage:
				var event struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(data, &event); err != nil {
					t.Errorf("Unmarshal(commit) error = %v", err)
					return
				}
				if event.Type != "input_audio_buffer.commit" {
					t.Errorf("text event = %q, want commit", event.Type)
					return
				}
				if err := conn.WriteJSON(map[string]string{"type": "transcript.final", "text": "hello world"}); err != nil {
					t.Errorf("WriteJSON(final) error = %v", err)
				}
				return
			}
		}
	}))
	defer server.Close()

	client := NewClient("ws" + strings.TrimPrefix(server.URL, "http"))
	client.EventTimeout = time.Second

	var updates []audio.TranscriptUpdate
	stream, err := client.Start(audio.StreamMeta{
		DeviceID:      "device-1",
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	}, func(update audio.TranscriptUpdate) {
		updates = append(updates, update)
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer stream.Close()

	pcm := make([]byte, 5000)
	pcm[0] = 0x01
	pcm[3200] = 0x02
	if err := stream.WritePCM(pcm); err != nil {
		t.Fatalf("WritePCM() error = %v", err)
	}
	if err := stream.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if string(received) != string(pcm) {
		t.Fatalf("received pcm = %v, want %v", received, pcm)
	}
	if binaryFrames != 2 {
		t.Fatalf("binaryFrames = %d, want 2", binaryFrames)
	}
	if len(updates) == 0 || updates[len(updates)-1].Final != "hello world" {
		t.Fatalf("updates = %+v, want final transcript", updates)
	}
}
