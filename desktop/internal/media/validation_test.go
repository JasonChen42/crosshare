package media

import (
	"strings"
	"testing"
)

func TestNormalizeSHA256(t *testing.T) {
	upper := strings.Repeat("AB", 32)
	got, err := NormalizeSHA256(upper)
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.ToLower(upper) {
		t.Fatalf("unexpected normalized hash %q", got)
	}

	for _, invalid := range []string{"", "abc123", strings.Repeat("g", 64), strings.Repeat("a", 63)} {
		if _, err := NormalizeSHA256(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestSafeFileNameRemovesPathComponents(t *testing.T) {
	for input, want := range map[string]string{
		"../../photo.jpg": "photo.jpg",
		`..\..\photo.jpg`: "photo.jpg",
		"":                "fallback",
	} {
		if got := SafeFileName(input, "fallback"); got != want {
			t.Fatalf("SafeFileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMaxFileBytesFromEnv(t *testing.T) {
	t.Setenv("CROSSHARE_MAX_MEDIA_BYTES", "1234")
	if got := MaxFileBytesFromEnv(); got != 1234 {
		t.Fatalf("got %d, want 1234", got)
	}
	t.Setenv("CROSSHARE_MAX_MEDIA_BYTES", "invalid")
	if got := MaxFileBytesFromEnv(); got != DefaultMaxFileBytes {
		t.Fatalf("invalid value should use default, got %d", got)
	}
}
