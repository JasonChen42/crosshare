# CrossHare

以照片管理为主的 Android 与电脑双向传输工具。手机和电脑连接同一可信 WiFi 后，可以选图上传到电脑，也可以通过电脑 Web 导入照片并发送到手机系统相册。

> 照片重构第一版：Android 提供照片 / 相册 / 传输 / 语音四个 Tab，电脑首页为 Web 图库，原语音页面位于 `/voice`。当前为手动选择传输；自动同步规则与回收站尚未实现。详细实现与验证边界见 [2026-09-08 进度](docs/progress/2026-09-08-photo-library.md)。

### 新版使用

1. 启动新版电脑程序，访问 `http://127.0.0.1:8384/`。
2. 安装新版 Android APK，通过设置中的配对入口连接电脑；Web「设备」页显示配对码。
3. 手机点击「读取本机照片并开启接收」，允许需要的媒体访问权限。
4. 在手机照片页长按多选，点击「上传电脑」；电脑图库刷新后可查看。
5. 电脑 Web 点击「导入图片」，选中照片和目标手机，点击「发送到手机」。
6. 手机传输开启时接收任务，校验后保存到系统相册；电脑收到回执后显示已送达。

相册是逻辑引用，不重复复制原图。传输记录不等于实时副本存在状态，删除不会自动传播。

独立开发预览可使用 `CROSSHARE_DATA_DIR=/path/to/profile ./crosshare --no-tray --port 8385`，避免更改默认配置；照片目录由该 profile 的配置决定。

## 架构

```
手机 (Android)                    电脑 (Linux/Windows)
┌──────────────────┐             ┌──────────────────────────┐
│  MediaStore Scan │             │  HTTP API                │
│  SHA-256 Hash    │             │  /api/pair               │
│  Manual Sync     │────────────►│  /api/synced             │
│                  │  上传媒体 → │  /api/media/upload/*     │
│  AudioRecord     │────────────►│  /api/audio/stream       │
│                  │  上传语音 → │                          │
│  Jetpack Compose │             │  File Organizer          │
└──────────────────┘             │  YYYY/YYYY-MM-DD/        │
                                 │  Local Web UI            │
                                 │  SQLite DB               │
                                 │                          │
                                 │  STT Engine (Docker)     │
                                 │  Qwen3-ASR / sherpa-onnx │
                                 │  ws://127.0.0.1:8013     │
                                 └──────────────────────────┘
```

## 功能特性

- 📷 **照片+视频同步** — Android 端扫描 MediaStore，手动推送未同步文件
- 🎙️ **手机语音输入** — Android 端检测语音片段并上传到桌面端保存为 WAV
- 🤖 **实时语音识别** — Qwen3-ASR-1.7B GPU 推理，支持 30+ 语言和 22 种中文方言
- 📁 **日期归档** — 按 `YYYY/YYYY-MM-DD/` 目录结构自动整理
- 🔑 **配对认证** — 6位配对码 + API Token，配对失败会限流
- 👨‍👩‍👧‍👦 **多设备支持** — 支持多台手机同步到一台电脑
- 🖥️ **系统托盘 / 本机 Web UI** — 桌面端可托盘运行，也可作为 systemd user service 常驻
- 🔄 **内容去重** — 基于 SHA-256 哈希，不会重复同步
- 🔀 **引擎热切换** — 改一个环境变量即可在 Qwen3-ASR 和 sherpa-onnx 之间切换

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
- 语音服务：默认通过 `localman --speech start --json` 自动拉起 Qwen3-ASR-1.7B（GPU 推理），支持引擎热切换。详见 [`docs/speech/`](docs/speech/)

安装脚本会尝试启用 systemd linger，让服务在 Linux 开机后、用户未登录时也能自动启动。如果系统拒绝自动启用，可手动运行：

```bash
loginctl enable-linger "$USER"
```

如需手动后台运行，也可以使用：

```bash
./crosshare --no-tray
```

### 一键启动语音输入

项目自带独立语音输入命令，用于同时启动 CrossHare 后端、语音识别服务（Qwen3-ASR 或 sherpa-onnx）和当前光标文本注入：

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
2. 执行 `localman --speech start --json` 拉起 STT 容器（stt-realtime + stt-offline）
3. 为 `crosshare.service` 写入 `CROSSHARE_TEXT_INJECT=1` 和 `CROSSHARE_TEXT_INJECT_MODE=portal-unicode`
4. 重启用户级 systemd 服务

第一次注入到当前光标输入框时，GNOME 可能会弹出 Remote Desktop 授权窗口；允许后，把焦点放在目标输入框并从手机端开始说话即可。

实时语音识别默认连接：

```text
ws://127.0.0.1:8013/v1/audio/realtime
```

可通过环境变量切换引擎或连接地址：

```bash
# 使用 Qwen3-ASR（默认）
CROSSHARE_TRANSCRIPT_WS=ws://127.0.0.1:8013/v1/audio/realtime

# 切回 sherpa-onnx
CROSSHARE_TRANSCRIPT_WS=ws://127.0.0.1:8013/v1/audio/realtime  # 引擎由 STT 端 SPEECH_REALTIME_ENGINE 控制
```

详细配置和引擎切换请参考 [`docs/speech/`](docs/speech/)。

如果不想让 CrossHare 启动时自动拉起本机语音服务，可以设置：

```bash
CROSSHARE_SPEECH_AUTOSTART=0 ./crosshare --no-tray
```

后端默认限制单个媒体文件为 20 GiB、单段音频为 64 MiB。可以按部署需要调整：

```bash
CROSSHARE_MAX_MEDIA_BYTES=21474836480
CROSSHARE_MAX_AUDIO_BYTES=67108864
```

旧版桌面端通过 mDNS 主动从手机拉取媒体的路径默认关闭；只有维护旧客户端时才应在可信网络显式设置 `CROSSHARE_LEGACY_PULL_SYNC=1`。

首次运行会：
1. 在 `~/.crosshare/` 创建配置文件
2. 普通运行时在系统托盘显示图标，`--no-tray`/systemd 运行时提供本机 Web UI
3. 生成6位配对码（在托盘菜单或本机 Web UI 中查看）
4. 等待 Android 端用配对码连接并上传媒体或音频

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
- **旧版拉取同步** — 仅在显式启用 `CROSSHARE_LEGACY_PULL_SYNC=1` 时显示
- **打开同步文件夹** — 在文件管理器中打开
- **退出** — 退出 CrossHare

## Android 端 (Kotlin)

> 🚧 开发中...

### 工作流程

1. 安装 App → 连接到和电脑相同的 WiFi
2. 输入电脑上显示的6位配对码、电脑局域网 IP 和端口
3. App 保存电脑返回的 API Token
4. 手动扫描并上传未同步照片/视频
5. 可启动麦克风监听，检测到语音片段后上传到电脑

## 同步流程

```
Android 扫描 MediaStore 并计算 SHA-256
  ↓
Android 请求桌面端已同步 hash 列表 (GET /api/synced)
  ↓
Android 对比本地 DB，找出未同步文件
  ↓
Android 逐个上传新文件 (POST /api/media/upload/{hash})
  ↓
按 EXIF 拍摄日期归档到 YYYY/YYYY-MM-DD/
  ↓
记录到 SQLite 数据库（content_hash 去重）
```

## 安全模型

CrossHare 目前面向可信局域网使用，不适合直接暴露到公网。

- 桌面端 HTTP API 默认监听局域网地址，Android 端通过明文 HTTP 连接。请只在可信 WiFi 下使用。
- 配对需要桌面端显示的 6 位配对码；配对成功后使用随机 API Token 认证，配对失败会按来源 IP 限流。
- 媒体上传必须声明大小，桌面端会重新计算并验证 SHA-256；同名并发上传使用原子落盘，不会互相覆盖。
- 旧版 mDNS 拉取会把 Token 发给局域网发现的设备，因此默认关闭；当前客户端通过认证接口主动上传或领取下载任务。
- 桌面端本机 UI 的敏感状态接口仅允许 loopback 访问。
- 桌面端配置目录和 `config.json` 使用私有权限保存；Android 端禁用系统备份，避免配对 token 进入备份。
- 可选语音识别和 Wayland 文本注入依赖本机 `localman --speech` 与 GNOME Remote Desktop portal 授权。

## 技术栈

| 组件 | 技术 |
|:---|:---|
| 桌面端 | Go, fyne.io/systray, grandcat/zeroconf, modernc.org/sqlite |
| Android | Kotlin, Jetpack Compose, NsdManager, Room, WorkManager |
| 当前同步协议 | Android 主动推送到桌面端 HTTP API |
| 语音识别 | Qwen3-ASR-1.7B / sherpa-onnx, Docker, Python FastAPI, PyTorch |
| 模型管理 | localman (llm-runtime-manager), Docker Compose |
| 实验/历史代码 | mDNS/DNS-SD (`_crosshare._tcp`), Android embedded HTTP server |
| 去重 | SHA-256 content hash |

## 文档

- [`docs/speech/architecture.md`](docs/speech/architecture.md) — 语音识别流水线架构
- [`docs/speech/qwen3-asr.md`](docs/speech/qwen3-asr.md) — Qwen3-ASR-1.7B 集成指南
- [`docs/speech/model-management.md`](docs/speech/model-management.md) — localman 模型管理

## License

MIT
