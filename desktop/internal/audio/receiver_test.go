package audio

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
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
