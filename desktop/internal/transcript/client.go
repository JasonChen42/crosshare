package transcript

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/crosshare/desktop/internal/audio"
	"github.com/gorilla/websocket"
)

const DefaultURL = "ws://127.0.0.1:8000/v1/audio/realtime"
const DefaultPartialIntervalMS = 1600

type Client struct {
	URL          string
	Language     string
	PartialMS    int
	DialTimeout  time.Duration
	EventTimeout time.Duration
	WriteTimeout time.Duration
}

func NewClient(rawURL string) *Client {
	if rawURL == "" {
		rawURL = DefaultURL
	}
	return &Client{
		URL:          rawURL,
		Language:     "auto",
		PartialMS:    DefaultPartialIntervalMS,
		DialTimeout:  3 * time.Second,
		EventTimeout: 10 * time.Second,
		WriteTimeout: 2 * time.Second,
	}
}

func (c *Client) Start(meta audio.StreamMeta, onUpdate func(audio.TranscriptUpdate)) (audio.RealtimeTranscriptStream, error) {
	if err := validateMeta(meta); err != nil {
		return nil, err
	}
	if _, err := url.ParseRequestURI(c.URL); err != nil {
		return nil, fmt.Errorf("invalid transcript websocket url: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: c.DialTimeout}
	conn, _, err := dialer.Dial(c.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("connect transcript websocket: %w", err)
	}

	stream := &Stream{
		conn:         conn,
		onUpdate:     onUpdate,
		eventTimeout: c.EventTimeout,
		writeTimeout: c.WriteTimeout,
		frameBytes:   meta.SampleRate * meta.Channels * (meta.BitsPerSample / 8) / 10,
		done:         make(chan struct{}),
		final:        make(chan struct{}),
	}

	language := c.Language
	if language == "" {
		language = "auto"
	}
	partialMS := c.PartialMS
	if partialMS <= 0 {
		partialMS = DefaultPartialIntervalMS
	}
	if err := stream.expectStartupEvent("session.started"); err != nil {
		_ = stream.Close()
		return nil, err
	}
	if err := stream.sendJSON(map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"language":            language,
			"audio_format":        "pcm16le",
			"sample_rate_hz":      meta.SampleRate,
			"channels":            meta.Channels,
			"partial_interval_ms": partialMS,
		},
	}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("send transcript session.update: %w", err)
	}
	if err := stream.expectStartupEvent("session.updated"); err != nil {
		_ = stream.Close()
		return nil, err
	}

	go stream.readEvents()
	return stream, nil
}

func validateMeta(meta audio.StreamMeta) error {
	if meta.SampleRate != 16000 || meta.Channels != 1 || meta.BitsPerSample != 16 {
		return fmt.Errorf("transcript service requires pcm16le 16kHz mono, got %dHz %dch %dbit", meta.SampleRate, meta.Channels, meta.BitsPerSample)
	}
	return nil
}

type Stream struct {
	conn         *websocket.Conn
	onUpdate     func(audio.TranscriptUpdate)
	eventTimeout time.Duration
	writeTimeout time.Duration
	frameBytes   int

	writeMu sync.Mutex
	closeMu sync.Mutex
	closed  bool
	pending []byte

	done      chan struct{}
	final     chan struct{}
	doneOnce  sync.Once
	finalOnce sync.Once
}

func (s *Stream) WritePCM(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.pending = append(s.pending, p...)
	for len(s.pending) >= s.frameBytes {
		if err := s.writeLocked(websocket.BinaryMessage, s.pending[:s.frameBytes]); err != nil {
			return err
		}
		s.pending = s.pending[s.frameBytes:]
	}
	return nil
}

func (s *Stream) Commit() error {
	if err := s.flushPending(); err != nil {
		return err
	}
	if err := s.sendJSON(map[string]string{"type": "input_audio_buffer.commit"}); err != nil {
		return fmt.Errorf("send transcript commit: %w", err)
	}

	timer := time.NewTimer(s.eventTimeout)
	defer timer.Stop()
	select {
	case <-s.final:
		return nil
	case <-s.done:
		return nil
	case <-timer.C:
		return errors.New("timed out waiting for final transcript")
	}
}

func (s *Stream) Close() error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closed = true
	s.closeMu.Unlock()

	_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	err := s.conn.Close()
	s.doneOnce.Do(func() { close(s.done) })
	return err
}

func (s *Stream) expectStartupEvent(want string) error {
	if err := s.conn.SetReadDeadline(time.Now().Add(s.eventTimeout)); err != nil {
		return err
	}
	_, data, err := s.conn.ReadMessage()
	_ = s.conn.SetReadDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("read transcript %s: %w", want, err)
	}

	var event transcriptEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("decode transcript startup event: %w", err)
	}
	if event.Type == "transcript.error" {
		return fmt.Errorf("transcript service error: %s", event.errorText())
	}
	if event.Type != want {
		return fmt.Errorf("unexpected transcript event %q, want %q", event.Type, want)
	}
	return nil
}

func (s *Stream) sendJSON(value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.write(websocket.TextMessage, data)
}

func (s *Stream) write(messageType int, data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeLocked(messageType, data)
}

func (s *Stream) writeLocked(messageType int, data []byte) error {
	s.closeMu.Lock()
	closed := s.closed
	s.closeMu.Unlock()
	if closed {
		return errors.New("transcript websocket is closed")
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return err
	}
	return s.conn.WriteMessage(messageType, data)
}

func (s *Stream) flushPending() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	err := s.writeLocked(websocket.BinaryMessage, s.pending)
	s.pending = nil
	return err
}

func (s *Stream) readEvents() {
	defer s.doneOnce.Do(func() { close(s.done) })

	finalSeen := false
	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			s.closeMu.Lock()
			closed := s.closed
			s.closeMu.Unlock()
			if !closed && !finalSeen {
				s.emit(audio.TranscriptUpdate{Error: fmt.Sprintf("read transcript event: %v", err)})
			}
			return
		}

		var event transcriptEvent
		if err := json.Unmarshal(data, &event); err != nil {
			s.emit(audio.TranscriptUpdate{Error: fmt.Sprintf("decode transcript event: %v", err)})
			continue
		}

		switch event.Type {
		case "transcript.partial":
			s.emit(audio.TranscriptUpdate{Partial: event.Text})
		case "transcript.final":
			finalSeen = true
			s.emit(audio.TranscriptUpdate{Final: event.Text})
			s.finalOnce.Do(func() { close(s.final) })
		case "transcript.error":
			s.emit(audio.TranscriptUpdate{Error: event.errorText()})
		}
	}
}

func (s *Stream) emit(update audio.TranscriptUpdate) {
	if s.onUpdate != nil {
		s.onUpdate(update)
	}
}

type transcriptEvent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Error json.RawMessage `json:"error"`
}

func (e transcriptEvent) errorText() string {
	if len(e.Error) == 0 {
		return "unknown transcript error"
	}
	var text string
	if err := json.Unmarshal(e.Error, &text); err == nil {
		return text
	}
	return string(e.Error)
}
