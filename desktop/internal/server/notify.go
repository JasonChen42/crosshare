package server

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// uploadNoticeDebounce groups a burst of phone uploads into one desktop
// notification, so saving a dozen photos produces a single notice.
const uploadNoticeDebounce = 4 * time.Second

// uploadNotifier batches newly stored phone uploads and emits one desktop
// notification per burst, pointing the user at the local photo library.
type uploadNotifier struct {
	port     int
	run      func(name string, args ...string) error
	debounce time.Duration

	mu      sync.Mutex
	pending int
	timer   *time.Timer
}

func newUploadNotifier(port int, run func(string, ...string) error) *uploadNotifier {
	if run == nil {
		run = func(name string, args ...string) error {
			return exec.Command(name, args...).Run()
		}
	}
	return &uploadNotifier{port: port, run: run, debounce: uploadNoticeDebounce}
}

// saved records one newly stored upload that came from a paired phone.
func (n *uploadNotifier) saved() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pending++
	if n.timer != nil {
		n.timer.Stop()
	}
	n.timer = time.AfterFunc(n.debounce, n.flush)
}

// stop cancels a notification that has not been sent yet.
func (n *uploadNotifier) stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.timer != nil {
		n.timer.Stop()
	}
	n.timer = nil
	n.pending = 0
}

// flush sends the queued count as a single notification.
func (n *uploadNotifier) flush() {
	n.mu.Lock()
	count := n.pending
	n.pending = 0
	n.timer = nil
	n.mu.Unlock()
	if count == 0 {
		return
	}

	title := "CrossHare"
	body := fmt.Sprintf("已保存 %d 项新照片 · 浏览器打开 %s", count, n.libraryURL())
	switch runtime.GOOS {
	case "linux", "darwin":
		name, args := notificationCommand(runtime.GOOS, title, body)
		if err := n.run(name, args...); err != nil {
			log.Printf("[server] save notification failed: %v", err)
			return
		}
	default:
		log.Printf("[server] %s (desktop notifications need Linux or macOS)", body)
		return
	}
	log.Printf("[server] %s", body)
}

// libraryURL is the loopback address of the local photo library.
func (n *uploadNotifier) libraryURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", n.port)
}

// notificationCommand builds the platform command that shows a notification.
func notificationCommand(goos, title, body string) (string, []string) {
	if goos == "darwin" {
		return "osascript", []string{"-e", fmt.Sprintf("display notification %q with title %q", body, title)}
	}
	return "notify-send", []string{title, body}
}

// execReveal opens a file manager at the requested path without blocking the
// request; the process detaches and may outlive this server.
func execReveal(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}
