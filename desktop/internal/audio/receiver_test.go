package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReceiveWritesWaveFile(t *testing.T) {
	root := t.TempDir()
	receiver := NewReceiver(root)

	stream := []byte{0x00, 0x00, 0x10, 0x00, 0xf0, 0xff, 0x20, 0x00}
	result, err := receiver.Receive(StreamMeta{
		DeviceID:      "device-1",
		DeviceName:    "OnePlus 12",
		SessionID:     "session-1",
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
	}, bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}

	if result.BytesWritten != int64(len(stream)) {
		t.Fatalf("BytesWritten = %d, want %d", result.BytesWritten, len(stream))
	}

	data, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if got, want := string(data[:4]), "RIFF"; got != want {
		t.Fatalf("wav header = %q, want %q", got, want)
	}
	if got, want := string(data[8:12]), "WAVE"; got != want {
		t.Fatalf("wave type = %q, want %q", got, want)
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != uint32(len(stream)) {
		t.Fatalf("data chunk size = %d, want %d", got, len(stream))
	}

	if string(data[44:]) != string(stream) {
		t.Fatalf("audio payload mismatch")
	}

	status := receiver.Status()
	if status.Active {
		t.Fatalf("status.Active = true, want false")
	}
	if filepath.Base(result.FilePath) != status.LastCompletedFile {
		t.Fatalf("LastCompletedFile = %q, want %q", status.LastCompletedFile, filepath.Base(result.FilePath))
	}
}

func TestReceiveCopiesPCMToRealtimeTranscriber(t *testing.T) {
	root := t.TempDir()
	transcriber := &fakeTranscriber{}
	receiver := NewReceiver(root)
	receiver.SetTranscriber(transcriber)

	stream := []byte{0x01, 0x00, 0x02, 0x00}
	result, err := receiver.Receive(StreamMeta{
		DeviceID:      "device-1",
		DeviceName:    "OnePlus 12",
		SessionID:     "session-2",
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	}, bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}

	if got := string(transcriber.stream.data); got != string(stream) {
		t.Fatalf("transcriber received %v, want %v", transcriber.stream.data, stream)
	}
	if !transcriber.stream.committed {
		t.Fatalf("transcriber stream was not committed")
	}
	if !transcriber.stream.closed {
		t.Fatalf("transcriber stream was not closed")
	}
	if result.Transcript != "hello world" {
		t.Fatalf("Transcript = %q, want hello world", result.Transcript)
	}

	status := receiver.Status()
	if status.LastTranscript != "hello world" {
		t.Fatalf("LastTranscript = %q, want hello world", status.LastTranscript)
	}
}

func TestReceiveInjectsFinalTranscript(t *testing.T) {
	root := t.TempDir()
	transcriber := &fakeTranscriber{}
	injector := &fakeTextInjector{texts: make(chan string, 1)}
	receiver := NewReceiver(root)
	receiver.SetTranscriber(transcriber)
	receiver.SetTextInjector(injector)

	_, err := receiver.Receive(StreamMeta{
		DeviceID:      "device-1",
		DeviceName:    "OnePlus 12",
		SessionID:     "session-3",
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	}, bytes.NewReader([]byte{0x01, 0x00}))
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}

	select {
	case got := <-injector.texts:
		if got != "hello world" {
			t.Fatalf("injected text = %q, want hello world", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for injected text")
	}

	status := receiver.Status()
	if status.TextInjectionStatus != "ok" {
		t.Fatalf("TextInjectionStatus = %q, want ok; error=%q", status.TextInjectionStatus, status.TextInjectionError)
	}
}

type fakeTranscriber struct {
	stream *fakeTranscriptStream
}

func (f *fakeTranscriber) Start(_ StreamMeta, onUpdate func(TranscriptUpdate)) (RealtimeTranscriptStream, error) {
	f.stream = &fakeTranscriptStream{onUpdate: onUpdate}
	onUpdate(TranscriptUpdate{Partial: "hello"})
	return f.stream, nil
}

type fakeTranscriptStream struct {
	onUpdate  func(TranscriptUpdate)
	data      []byte
	committed bool
	closed    bool
}

func (f *fakeTranscriptStream) WritePCM(p []byte) error {
	f.data = append(f.data, p...)
	return nil
}

func (f *fakeTranscriptStream) Commit() error {
	f.committed = true
	f.onUpdate(TranscriptUpdate{Final: "hello world"})
	return nil
}

func (f *fakeTranscriptStream) Close() error {
	if f.closed {
		return io.ErrClosedPipe
	}
	f.closed = true
	return nil
}

type fakeTextInjector struct {
	texts chan string
}

func (f *fakeTextInjector) InjectText(_ context.Context, text string) error {
	f.texts <- text
	return nil
}
