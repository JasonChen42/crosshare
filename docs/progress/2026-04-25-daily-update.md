# CrossHare 日度更新 - 2026-04-25

## 当前目标

把 CrossHare 从单纯的手机媒体/音频接收工具，推进为一套可常驻运行的本机语音识别接收台：

- 桌面端 UI 常驻运行，显示配对、语音服务、麦克风传输和识别结果。
- 手机完成配对后，通过 CrossHare 上传麦克风 PCM 流。
- CrossHare 将音频保存为 WAV，并转发到本机实时语音服务。
- UI 单独展示识别文本，避免和传输状态混在一行。

## 项目结构

```text
crosshare/
  android/                         Android 手机端
    app/src/main/java/com/crosshare/
      service/AudioStreamService.kt    手机麦克风采集和上传
      ui/screens/                      手机端 Compose 页面

  desktop/                         Go 桌面端
    cmd/crosshare/main.go              进程入口、systemd/no-tray 运行
    internal/server/                   HTTP API、本机 UI、speech 状态聚合
    internal/audio/                    音频接收、WAV 保存、转写状态
    internal/transcript/               WebSocket 实时转写客户端
    internal/config/                   配置、配对设备、令牌
    internal/storage/                  SQLite 同步记录
    scripts/install-systemd-user.sh    安装用户级 systemd 服务
    deploy/systemd/crosshare.service   systemd user service 模板

  docs/progress/                   日度进度记录
```

## 运行形态

桌面端作为用户级 systemd 服务常驻：

```bash
systemctl --user status crosshare
```

本机 UI：

```text
http://127.0.0.1:8384/
```

手机端连接电脑的局域网地址当前为：

```text
192.168.1.200:8384
```

实时语音统一入口：

```text
ws://127.0.0.1:8000/v1/audio/realtime
```

## 今日完成

### 1. UI 常驻和状态展示

确认 `crosshare.service` 已经启用并运行，服务入口是：

```text
/home/jason/.local/bin/crosshare --no-tray
```

UI 由 CrossHare 进程直接提供，端口来自配置，当前为 `8384`。页面仅允许本机访问状态接口 `/api/ui/status`。

### 2. 手机配对状态

UI 现在展示：

- 未配对：显示配对码。
- 已配对：显示设备数量。
- 最近连接：按 `LastSeen` 判断 10 分钟内是否最近连接。
- 设备列表：显示设备名、IP、最近连接时间和设备 ID。

当前已知配对设备包括：

- `PGKM10`
- `IN2020`

### 3. 语音识别服务管理

探索并接入了 `localman --speech` 命令：

```bash
localman --speech start --json
localman --speech status --json
localman --speech urls --json
localman --speech smoke --json
localman --speech stop --json
```

结论：

- `start --json` 是幂等的，服务已运行时仍会返回最终 JSON 状态。
- `status --json` 返回完整状态和 health，适合 UI 状态刷新。
- `urls --json` 返回 URL 和运行态，不带 health。
- `smoke --json` 做健康探测，当前返回 `ready`。
- `stop --json` 未在验证中执行，避免停止当前可用语音服务。

CrossHare 启动后会异步执行：

```bash
localman --speech start --json
```

UI 刷新时会执行：

```bash
localman --speech status --json
```

并将结果聚合到 `/api/ui/status` 的 `speech` 字段。

### 4. UI 新增识别准备提示

UI 新增两个面板：

- `语音服务`：显示 realtime URL、模型、gateway/stt-realtime 运行态。
- `识别准备`：按当前状态显示需要配对、等待语音服务、等待手机连接、准备识别、正在识别。

准备条件：

- 至少有一台手机已配对。
- speech 状态为 `ready`。
- gateway 和 stt-realtime 均 running。

### 5. 识别内容独立展示

原先识别内容会拼到麦克风状态行里。现在已经拆成独立 `语音识别` 面板：

- partial：显示正在识别。
- final/last transcript：显示最近识别结果。
- error：显示识别异常。

### 6. 实时转写稳定性

调整 `desktop/internal/transcript/client.go`：

- 默认实时入口保持 `ws://127.0.0.1:8000/v1/audio/realtime`。
- final 等待时间从 2 秒调到 10 秒，避免服务端解码稍慢时 CrossHare 误判超时。
- partial 间隔调到 `1600ms`，贴近 speech runtime 默认行为。

已验证手机端实时输入可返回识别结果：

- `速度可以再快一点`
- `没问题`
- `OKOK没问题`

## 关键代码改动

### 新增 speech runtime 管理

文件：

```text
desktop/internal/server/speech.go
desktop/internal/server/speech_test.go
```

职责：

- 查找 `localman`。
- 执行 `localman --speech start/status --json`。
- 解析可能带普通日志前缀的 JSON 输出。
- 将服务状态整理为 UI 可直接消费的结构。
- 支持 `CROSSHARE_SPEECH_AUTOSTART=0` 关闭自动启动。

### Server 接入 speech 状态

文件：

```text
desktop/internal/server/server.go
desktop/internal/server/ui.go
```

改动：

- `Server` 增加 speech runtime 状态。
- `New()` 中异步启动 speech runtime。
- `/api/ui/status` 返回 `speech` 字段。
- UI 读取 `speech.ready` 和 service running 状态渲染提示。

### Transcript 客户端调整

文件：

```text
desktop/internal/transcript/client.go
desktop/internal/transcript/client_test.go
```

改动：

- 增加 `DefaultPartialIntervalMS = 1600`。
- 增加 `PartialMS` 可配置字段。
- 默认 `EventTimeout = 10s`。
- 测试覆盖 session.update 中的 partial interval。

### 安装和说明更新

文件：

```text
README.md
desktop/scripts/install-systemd-user.sh
```

改动：

- 说明 CrossHare 启动时会拉起 speech runtime。
- 说明 UI 会显示配对、语音服务、识别准备状态。
- 记录实时语音入口和关闭自动启动的环境变量。

## 验证记录

测试：

```bash
cd desktop
go test ./...
```

结果：通过。

服务状态：

```bash
systemctl --user status crosshare --no-pager
```

结果：`crosshare.service` active running。

speech smoke：

```bash
localman --speech smoke --json
```

结果：

```json
{
  "status": "ready",
  "realtime_url": "ws://127.0.0.1:8000/v1/audio/realtime",
  "services": {
    "gateway": true,
    "realtime": true,
    "offline": true
  }
}
```

UI 状态摘要：

```json
{
  "paired": 2,
  "recent": 0,
  "speech_ready": true,
  "speech_status": "ready",
  "speech_url": "ws://127.0.0.1:8000/v1/audio/realtime",
  "speech_auto_start": true,
  "audio": {
    "active": false
  }
}
```

## 当前可用操作

安装/更新并启用自启动：

```bash
cd /home/jason/development/crosshare/desktop
make install-autostart
```

重启 CrossHare：

```bash
systemctl --user restart crosshare
```

查看 CrossHare 日志：

```bash
journalctl --user -u crosshare -f
```

查看 speech 状态：

```bash
localman --speech status --json
```

禁用 CrossHare 自动拉起 speech：

```bash
CROSSHARE_SPEECH_AUTOSTART=0 ./crosshare --no-tray
```

## 后续建议

- UI 增加显式“启动语音服务/停止语音服务”按钮，目前只做自动启动和状态展示。
- 将 `localman --speech` 调用做短期缓存，避免 UI 每 3 秒刷新时频繁启动子进程。
- 如果需要支持远程浏览 UI，可以单独设计授权机制；当前 `/api/ui/status` 只允许 loopback。
- Android 端如果后续改为更长连接或真正持续流式，需要同步检查服务端 `audio.Receive` 的并发和超时策略。

## 追加记录：句尾截断调整

反馈：手机端本地语音触发阈值偏高，部分短句最后几个字偶发缺失。

处理：

- Android 端 VAD 改为启动门槛和持续说话门槛分离。
- 进入语音段前仍使用较高门槛，避免环境噪声误触发。
- 进入语音段后使用更低门槛，避免句尾轻声字被当成静音。
- 延长尾部静音窗口：
  - 高敏：约 `700ms` 调整为 `1200ms`
  - 平衡：约 `800ms` 调整为 `1300ms`
  - 低敏：约 `900ms` 调整为 `1400ms`

涉及文件：

```text
android/app/src/main/java/com/crosshare/service/AudioStreamService.kt
android/app/src/main/java/com/crosshare/ui/screens/SettingsScreen.kt
```
