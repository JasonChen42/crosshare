package tray

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"

	"github.com/crosshare/desktop/internal/config"
	csync "github.com/crosshare/desktop/internal/sync"
	"fyne.io/systray"
)

// Tray manages the system tray icon and menu.
type Tray struct {
	cfg         *config.Config
	syncMgr     *csync.Manager
	mStatus     *systray.MenuItem
	mPairCode   *systray.MenuItem
	mSyncNow    *systray.MenuItem
	mOpenFolder *systray.MenuItem
	mStats      *systray.MenuItem
	mQuit       *systray.MenuItem
	onQuit      func()
}

// New creates a new system tray handler.
func New(cfg *config.Config, syncMgr *csync.Manager, onQuit func()) *Tray {
	return &Tray{
		cfg:     cfg,
		syncMgr: syncMgr,
		onQuit:  onQuit,
	}
}

// Run starts the system tray. This blocks until the tray is closed.
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	// systray.SetIcon(defaultIcon())
	systray.SetTitle("CrossHare")
	systray.SetTooltip("CrossHare - 照片同步")

	// Status info
	t.mStatus = systray.AddMenuItem("状态: 空闲", "当前同步状态")
	t.mStatus.Disable()

	systray.AddSeparator()

	// Pair code
	t.mPairCode = systray.AddMenuItem(
		fmt.Sprintf("配对码: %s", t.cfg.GetPairCode()),
		"在手机端输入此配对码",
	)
	t.mPairCode.Disable()

	// Paired devices count
	pairedCount := len(t.cfg.PairedDevices)
	mPaired := systray.AddMenuItem(
		fmt.Sprintf("已配对设备: %d", pairedCount),
		"已配对的手机数量",
	)
	mPaired.Disable()

	systray.AddSeparator()

	// Sync now button
	t.mSyncNow = systray.AddMenuItem("立即同步", "手动触发同步")

	// Open target folder
	t.mOpenFolder = systray.AddMenuItem("打开同步文件夹", "在文件管理器中打开")

	// Stats
	t.mStats = systray.AddMenuItem("同步统计", "查看已同步文件数")
	t.mStats.Disable()

	systray.AddSeparator()

	// Quit
	t.mQuit = systray.AddMenuItem("退出", "退出 CrossHare")

	// Listen for sync progress updates
	t.syncMgr.OnProgressUpdate(func(p csync.SyncProgress) {
		switch p.Status {
		case csync.StatusSyncing:
			statusText := fmt.Sprintf("同步中: %s (%d/%d)", p.CurrentFile, p.FilesDone, p.FilesTotal)
			t.mStatus.SetTitle(statusText)
			systray.SetTooltip(statusText)
		case csync.StatusDiscovering:
			t.mStatus.SetTitle("状态: 搜索设备中...")
		case csync.StatusError:
			t.mStatus.SetTitle(fmt.Sprintf("错误: %s", p.ErrorMessage))
		default:
			if p.FilesDone > 0 {
				t.mStatus.SetTitle(fmt.Sprintf("状态: 空闲 (上次同步 %d 个文件)", p.FilesDone))
			} else {
				t.mStatus.SetTitle("状态: 空闲")
			}
			systray.SetTooltip("CrossHare - 照片同步")
		}
	})

	// Handle menu clicks
	go func() {
		for {
			select {
			case <-t.mSyncNow.ClickedCh:
				log.Println("[tray] manual sync triggered")
				t.syncMgr.TriggerSync(nil)

			case <-t.mOpenFolder.ClickedCh:
				openFolder(t.cfg.GetTargetFolder())

			case <-t.mQuit.ClickedCh:
				if t.onQuit != nil {
					t.onQuit()
				}
				systray.Quit()
				return
			}
		}
	}()
}

func (t *Tray) onExit() {
	log.Println("[tray] system tray exiting")
}

// openFolder opens a folder in the default file manager.
func openFolder(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default: // linux
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[tray] failed to open folder: %v", err)
	}
}

// defaultIcon returns a minimal tray icon as PNG bytes.
// This is a 16x16 white square with a camera-like shape.
func defaultIcon() []byte {
	// Minimal 16x16 PNG icon (a simple colored square placeholder)
	// In production, this should be replaced with a proper icon file.
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x10,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x91, 0x68, 0x36, 0x00, 0x00, 0x00,
		0x1c, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x62, 0x60, 0x60, 0xa0, 0x06,
		0x60, 0x60, 0x60, 0xf8, 0xff, 0xff, 0x3f, 0x35, 0x4c, 0x18, 0x18, 0x18,
		0x00, 0x00, 0x04, 0x01, 0x00, 0x01, 0x69, 0x0c, 0x71, 0x9b, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}
