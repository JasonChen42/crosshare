# CrossHare

局域网照片/视频自动同步工具 — 手机连接到同一 WiFi 后，电脑自动发现手机并拉取照片/视频，按日期归档到指定文件夹。

## 架构

```
手机 (Android)                    电脑 (Linux/Windows)
┌──────────────────┐             ┌──────────────────────┐
│  HTTP Server     │◄────────────│  mDNS Discovery      │
│  mDNS Broadcast  │   发现 →    │  (每5分钟扫描)        │
│  _crosshare._tcp │   拉取 →    │                      │
│                  │             │  HTTP Client         │
│  /api/media/list │────────────►│  断点续传下载          │
│  /api/media/dl/* │             │                      │
│                  │             │  File Organizer      │
│  ContentObserver │             │  YYYY/YYYY-MM-DD/    │
│  SHA-256 Hash    │             │                      │
└──────────────────┘             │  System Tray         │
                                 │  SQLite DB           │
                                 └──────────────────────┘
```

## 功能特性

- 📡 **自动发现** — 电脑每5分钟通过 mDNS 扫描手机是否在线
- 📷 **照片+视频同步** — 支持所有相册中的照片和视频
- ⏸️ **断点续传** — 大文件传输中断后自动从断点恢复
- 📁 **日期归档** — 按 `YYYY/YYYY-MM-DD/` 目录结构自动整理
- 🔑 **安全配对** — 6位配对码 + API Token 认证
- 👨‍👩‍👧‍👦 **多设备支持** — 支持多台手机同步到一台电脑
- 🖥️ **系统托盘** — 最小化后台运行，不占桌面空间
- 🔄 **内容去重** — 基于 SHA-256 哈希，不会重复同步

## 桌面端 (Go)

### 构建

```bash
cd desktop

# Linux
make build

# Windows (交叉编译)
make build-windows

# 运行测试
make test
```

### 运行

```bash
./crosshare
```

### Linux 开机自启动

桌面端提供 systemd user service，后台运行时不会启动系统托盘，而是在本机浏览器提供一个简单 UI：

```bash
cd desktop
make install-autostart
```

安装后：

- 服务名称：`crosshare.service`
- 本机 UI：`http://127.0.0.1:8384/`
- 查看状态：`systemctl --user status crosshare.service`
- 查看日志：`journalctl --user -u crosshare.service -f`
- 语音服务：默认通过 `localman --speech start --json` 自动拉起，并在 UI 中显示状态

安装脚本会尝试启用 systemd linger，让服务在 Linux 开机后、用户未登录时也能自动启动。如果系统拒绝自动启用，可手动运行：

```bash
loginctl enable-linger "$USER"
```

如需手动后台运行，也可以使用：

```bash
./crosshare --no-tray
```

### 一键启动语音输入

项目自带独立语音输入命令，用于同时启动 CrossHare 后端、语音识别服务和当前光标文本注入：

```bash
cd desktop
go run ./cmd/voiced start
```

安装过自启动后，也可以直接运行已安装命令：

```bash
voiced start
voiced status
voiced logs
```

启动流程会：

1. 构建并安装 `~/.local/bin/crosshare`
2. 执行 `localman --speech start --json`
3. 为 `crosshare.service` 写入 `CROSSHARE_TEXT_INJECT=1` 和 `CROSSHARE_TEXT_INJECT_MODE=portal-unicode`
4. 重启用户级 systemd 服务

第一次注入到当前光标输入框时，GNOME 可能会弹出 Remote Desktop 授权窗口；允许后，把焦点放在目标输入框并从手机端开始说话即可。

实时语音识别默认连接：

```text
ws://127.0.0.1:8000/v1/audio/realtime
```

如果不想让 CrossHare 启动时自动拉起本机语音服务，可以设置：

```bash
CROSSHARE_SPEECH_AUTOSTART=0 ./crosshare --no-tray
```

首次运行会：
1. 在 `~/.crosshare/` 创建配置文件
2. 普通运行时在系统托盘显示图标，`--no-tray`/systemd 运行时提供本机 Web UI
3. 生成6位配对码（在托盘菜单或本机 Web UI 中查看）
4. 开始每5分钟扫描局域网中的手机

### 配置

配置文件位于：
- Linux: `~/.crosshare/config.json`
- Windows: `%APPDATA%\CrossHare\config.json`

```json
{
  "target_folder": "/home/user/Pictures/CrossHare",
  "http_port": 8384,
  "device_name": "My-PC",
  "discovery_interval_seconds": 300,
  "pair_code": "123456",
  "paired_devices": []
}
```

### 系统托盘菜单

- **状态** — 显示当前同步状态
- **配对码** — 在手机端输入此码完成配对
- **立即同步** — 手动触发一次同步
- **打开同步文件夹** — 在文件管理器中打开
- **退出** — 退出 CrossHare

## Android 端 (Kotlin)

> 🚧 开发中...

### 工作流程

1. 安装 App → 连接到和电脑相同的 WiFi
2. 输入电脑上显示的6位配对码
3. App 后台广播 mDNS 服务
4. 电脑发现手机后自动拉取新照片/视频
5. 也可以手动打开 App 触发同步

## 同步流程

```
电脑发现手机 (mDNS)
  ↓
电脑请求手机的媒体列表 (GET /api/media/list)
  ↓
电脑对比本地DB，找出未同步的文件
  ↓
电脑逐个下载新文件 (GET /api/media/download/{hash})
  ↓ 支持 Range 断点续传
按 EXIF 拍摄日期归档到 YYYY/YYYY-MM-DD/
  ↓
记录到 SQLite 数据库（content_hash 去重）
```

## 技术栈

| 组件 | 技术 |
|:---|:---|
| 桌面端 | Go, fyne.io/systray, grandcat/zeroconf, modernc.org/sqlite |
| Android | Kotlin, Jetpack Compose, NsdManager, Room, WorkManager |
| 发现协议 | mDNS/DNS-SD (`_crosshare._tcp`) |
| 断点续传 | HTTP Range headers |
| 去重 | SHA-256 content hash |

## License

MIT
