package media

import (
	"encoding/hex"
	"errors"
	"os"
	"path"
	"strconv"
	"strings"
)

const SHA256HexLength = 64
const DefaultMaxFileBytes int64 = 20 << 30

// NormalizeSHA256 validates and normalizes a hexadecimal SHA-256 digest.
func NormalizeSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != SHA256HexLength {
		return "", errors.New("content hash must be a 64-character SHA-256 digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("content hash must be hexadecimal")
	}
	return value, nil
}

func MaxFileBytesFromEnv() int64 {
	value := strings.TrimSpace(os.Getenv("CROSSHARE_MAX_MEDIA_BYTES"))
	if value == "" {
		return DefaultMaxFileBytes
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return DefaultMaxFileBytes
	}
	return parsed
}

// SafeFileName removes path components supplied by another device.
func SafeFileName(value, fallback string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	value = path.Base(value)
	if value == "" || value == "." || value == "/" || strings.ContainsRune(value, '\x00') {
		return fallback
	}
	return value
}
