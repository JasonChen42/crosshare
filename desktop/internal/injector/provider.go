package injector

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type TextInjector interface {
	InjectText(ctx context.Context, text string) error
}

func EnabledFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CROSSHARE_TEXT_INJECT"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func ModeFromEnv() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("CROSSHARE_TEXT_INJECT_MODE")))
	if mode == "" {
		return "portal-unicode"
	}
	return mode
}

func NewFromEnv() (*PortalUnicode, error) {
	switch mode := ModeFromEnv(); mode {
	case "portal-unicode":
		return NewPortalUnicode()
	default:
		return nil, fmt.Errorf("unsupported text injection mode %q", mode)
	}
}
