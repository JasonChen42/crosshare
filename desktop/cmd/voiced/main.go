package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	crosshareBinary = "crosshare"
	commandBinary   = "voiced"

	unitName       = "crosshare.service"
	pruneUnitName  = "crosshare-audio-prune.service"
	pruneTimerName = "crosshare-audio-prune.timer"

	defaultUIURL       = "http://127.0.0.1:8384/"
	defaultUIStatusURL = "http://127.0.0.1:8384/api/ui/status"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "start"
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "start":
		return start()
	case "stop":
		return systemctl("stop", unitName)
	case "status":
		return status()
	case "logs":
		return journalctl()
	case "install":
		return installCommand()
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Print(`Usage: voiced [start|stop|status|logs|install]

Commands:
  start    Build and install CrossHare, start speech, enable text injection, restart the service.
  stop     Stop the CrossHare user service.
  status   Show CrossHare user service status and local UI status JSON.
  logs     Follow CrossHare user service logs.
  install  Install this command to ~/.local/bin/voiced.
`)
}

func start() error {
	desktopRoot, err := findDesktopRoot()
	if err != nil {
		return err
	}

	if err := build(desktopRoot, crosshareBinary, "./cmd/crosshare/"); err != nil {
		return err
	}
	if err := installCrosshare(desktopRoot); err != nil {
		return err
	}
	if err := startSpeechRuntime(); err != nil {
		return err
	}

	if systemdUserAvailable() {
		importGraphicalEnvironment()
		if err := systemctl("daemon-reload"); err != nil {
			return err
		}
		if err := systemctl("enable", "--now", pruneTimerName); err != nil {
			return err
		}
		if err := systemctl("enable", "--now", unitName); err != nil {
			return err
		}
		if err := systemctl("restart", unitName); err != nil {
			return err
		}
		waitForUI()
		printStartSummary()
		return nil
	}

	fmt.Println("systemd user manager is unavailable; running CrossHare in the foreground.")
	return runForeground(desktopRoot)
}

func status() error {
	if systemdUserAvailable() {
		_ = systemctl("status", unitName, "--no-pager")
	} else {
		fmt.Println("systemd user manager is unavailable")
	}

	fmt.Println()
	fmt.Println("CrossHare UI status:")
	body, err := httpGet(uiStatusURL())
	if err != nil {
		fmt.Printf("unavailable: %v\n", err)
	} else {
		fmt.Println(string(body))
	}
	fmt.Printf("CrossHare UI: %s\n", uiURL())
	return nil
}

func journalctl() error {
	if !systemdUserAvailable() {
		return errors.New("systemd user manager is unavailable")
	}
	return commandRun("", nil, "journalctl", "--user", "-u", unitName, "-f")
}

func installCommand() error {
	desktopRoot, err := findDesktopRoot()
	if err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "voiced-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	binaryPath := filepath.Join(tmpDir, commandBinary)
	if err := build(desktopRoot, binaryPath, "./cmd/voiced/"); err != nil {
		return err
	}
	binDir := filepath.Join(homeDir(), ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	if err := copyFile(binaryPath, filepath.Join(binDir, commandBinary), 0755); err != nil {
		return err
	}
	fmt.Printf("installed %s\n", filepath.Join(binDir, commandBinary))
	return nil
}

func findDesktopRoot() (string, error) {
	if value := strings.TrimSpace(os.Getenv("CROSSHARE_DESKTOP_ROOT")); value != "" {
		return value, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if isDesktopRoot(dir) {
			return dir, nil
		}
		if filepath.Base(dir) == "crosshare" && isDesktopRoot(filepath.Join(dir, "desktop")) {
			return filepath.Join(dir, "desktop"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	return "", errors.New("cannot find CrossHare desktop root; run from the repo or set CROSSHARE_DESKTOP_ROOT")
}

func isDesktopRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "crosshare", "main.go")); err != nil {
		return false
	}
	return true
}

func build(desktopRoot, output, pkg string) error {
	fmt.Printf("building %s...\n", output)
	goBin := os.Getenv("GO")
	if strings.TrimSpace(goBin) == "" {
		goBin = "go"
	}
	return commandRun(desktopRoot, nil, goBin, "build", "-o", output, pkg)
}

func installCrosshare(desktopRoot string) error {
	binDir := filepath.Join(homeDir(), ".local", "bin")
	unitDir := filepath.Join(homeDir(), ".config", "systemd", "user")
	dropinDir := filepath.Join(unitDir, unitName+".d")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(dropinDir, 0755); err != nil {
		return err
	}

	if err := copyFile(filepath.Join(desktopRoot, crosshareBinary), filepath.Join(binDir, crosshareBinary), 0755); err != nil {
		return err
	}
	for _, unit := range []string{unitName, pruneUnitName, pruneTimerName} {
		if err := copyFile(
			filepath.Join(desktopRoot, "deploy", "systemd", unit),
			filepath.Join(unitDir, unit),
			0644,
		); err != nil {
			return err
		}
	}

	dropin := `[Service]
Environment=CROSSHARE_SPEECH_AUTOSTART=1
Environment=CROSSHARE_TEXT_INJECT=1
Environment=CROSSHARE_TEXT_INJECT_MODE=portal-unicode
`
	return os.WriteFile(filepath.Join(dropinDir, "10-voice-input.conf"), []byte(dropin), 0644)
}

func startSpeechRuntime() error {
	localman, err := findExecutable("localman")
	if err != nil {
		return fmt.Errorf("localman is required for the speech runtime: %w", err)
	}
	fmt.Println("starting speech runtime...")
	return commandRun("", nil, localman, "--speech", "start", "--json")
}

func findExecutable(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	path := filepath.Join(homeDir(), ".local", "bin", name)
	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
		return path, nil
	}
	return "", fmt.Errorf("%s not found on PATH or in ~/.local/bin", name)
}

func systemdUserAvailable() bool {
	return exec.Command("systemctl", "--user", "show-environment").Run() == nil
}

func importGraphicalEnvironment() {
	args := []string{
		"--user", "import-environment",
		"DISPLAY",
		"WAYLAND_DISPLAY",
		"XAUTHORITY",
		"DBUS_SESSION_BUS_ADDRESS",
		"XDG_CURRENT_DESKTOP",
		"XDG_SESSION_DESKTOP",
		"XDG_SESSION_TYPE",
		"XDG_RUNTIME_DIR",
	}
	_ = commandRun("", nil, "systemctl", args...)
}

func systemctl(args ...string) error {
	return commandRun("", nil, "systemctl", append([]string{"--user"}, args...)...)
}

func runForeground(desktopRoot string) error {
	env := append(os.Environ(),
		"CROSSHARE_SPEECH_AUTOSTART=1",
		"CROSSHARE_TEXT_INJECT=1",
		"CROSSHARE_TEXT_INJECT_MODE=portal-unicode",
	)
	return commandRun(desktopRoot, env, filepath.Join(desktopRoot, crosshareBinary), "--no-tray")
}

func waitForUI() {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := httpGet(uiStatusURL()); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println("CrossHare UI did not respond yet; check logs if startup continues to fail.")
}

func printStartSummary() {
	fmt.Println()
	fmt.Println("CrossHare voice input is running.")
	fmt.Printf("UI: %s\n", uiURL())
	fmt.Printf("Status: %s\n", "voiced status")
	fmt.Printf("Logs: %s\n", "voiced logs")
	fmt.Println()
	fmt.Println("GNOME may ask for Remote Desktop permission the first time text is injected.")
}

func httpGet(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func uiURL() string {
	if value := strings.TrimSpace(os.Getenv("CROSSHARE_UI_URL")); value != "" {
		return value
	}
	return defaultUIURL
}

func uiStatusURL() string {
	if value := strings.TrimSpace(os.Getenv("CROSSHARE_UI_STATUS_URL")); value != "" {
		return value
	}
	return defaultUIStatusURL
}

func commandRun(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = env
	}
	return cmd.Run()
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return os.Getenv("HOME")
	}
	return home
}
