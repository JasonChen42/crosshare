# CrossHare Wayland 文本注入计划 - 2026-04-29

## 目标

在 GNOME Wayland 会话下，把 CrossHare 后端实时语音服务生成的 final transcript 注入到当前焦点输入框。

当前环境事实：

- 桌面会话是 `ubuntu-wayland` / GNOME Wayland。
- `xdg-desktop-portal` 和 `xdg-desktop-portal-gnome` 正在运行。
- RemoteDesktop portal 可用，支持 keyboard/pointer/touch。
- 系统没有安装 `wtype`、`ydotool`、`dotool`、`xdotool`。
- CrossHare 已能从手机音频流拿到 `transcript.final`。

## 方案 A：Portal MVP

MVP 使用 `org.freedesktop.portal.RemoteDesktop` 请求键盘控制授权，然后通过 portal 发送 Unicode 输入序列：

```text
Ctrl+Shift+u -> Unicode code point hex -> Enter
```

例如“测”会被注入为：

```text
Ctrl+Shift+u 6d4b Enter
```

### 优点

- 不需要切换到 Xorg。
- 不依赖 `xdotool`、`wtype`、`ydotool`。
- 中文文本不需要模拟拼音输入。
- 不污染剪贴板。
- 只需要一次 GNOME Remote Desktop 授权。

### 限制

- 依赖目标应用支持桌面 Unicode 输入序列；GNOME/GTK 文本框通常支持，部分应用可能不支持。
- 比剪贴板粘贴慢，长文本会逐字注入。
- 焦点必须仍在目标输入框。
- 首次注入会弹出 portal 授权窗口。

### CrossHare 接入点

- `audio.Receiver.updateTranscript()` 收到 final transcript 后异步调用 text injector。
- 注入失败只更新状态，不影响录音保存、转写结果和 HTTP 响应。
- 通过环境变量启用：

```bash
CROSSHARE_TEXT_INJECT=1
CROSSHARE_TEXT_INJECT_MODE=portal-unicode
```

## 方案 B：Portal 剪贴板粘贴

长期可补一个 `portal-paste` provider：

1. 保存当前剪贴板。
2. 通过 portal clipboard 设置识别文本。
3. 通过 RemoteDesktop portal 发送 `Ctrl+V`。
4. 可选恢复原剪贴板。

优点是速度快、应用兼容面大；缺点是短暂接管剪贴板，portal clipboard 协议也比 keyboard portal 更复杂。

## 方案 C：真正输入法 Bridge

长期最佳体验是实现一个输入法组件：

1. CrossHare 后端收到 final transcript。
2. 通过 D-Bus、Unix socket 或 localhost 把文本发给输入法 engine/addon。
3. 输入法组件对当前 input context 执行 commit text。

### IBus 路线

- 新增 `crosshare-voice` IBus engine。
- 安装 component 文件到 `~/.local/share/ibus/component/`。
- 用户切换到 CrossHare 输入法后，engine 可以 commit 识别文本。

### Fcitx5 路线

- 新增 Fcitx5 addon/input method。
- 当前机器已运行 Fcitx5 且当前输入法是 `rime`，因此本机状态更接近 Fcitx5。
- Fcitx5 原生 addon 通常需要 C++，工程量高于 portal MVP。

### 输入法 Bridge 优缺点

优点：

- 真正的当前光标 commit text。
- 不污染剪贴板。
- 不模拟快捷键。
- 适合后续做持续听写、候选词、分段提交。

缺点：

- 实现和安装复杂度高。
- 用户需要配置/切换输入法。
- IBus/Fcitx5 需要先选生态。
- 调试和自动化测试成本高。

## 当前推荐

先跑通 `portal-unicode` MVP，验证完整链路：

```text
手机语音 -> speech runtime -> transcript.final -> portal keyboard -> 当前输入框
```

随后按实际体验决定是否补 `portal-paste` 或进入输入法 bridge 阶段。

## MVP 验证结果

本机已经验证 `portal-unicode` 可用：

- `transcript.final` 能触发异步文本注入。
- GNOME Remote Desktop 授权后，文本能进入当前焦点输入框。
- `audio.text_injection_status` 返回 `ok`。

实际修正点：

- `session_handle` 在本机 portal 响应中是 string，需要兼容转换为 D-Bus object path。
- RemoteDesktop portal 的 device type 是 bitmask，`1 = KEYBOARD`，不是 `2`。

当前本机持久配置：

```ini
# ~/.config/systemd/user/crosshare.service.d/10-text-injection.conf
[Service]
Environment=CROSSHARE_TEXT_INJECT=1
Environment=CROSSHARE_TEXT_INJECT_MODE=portal-unicode
```

验证命令：

```bash
curl -fsS http://127.0.0.1:8384/api/ui/status \
  | jq '.audio | {text_injection_enabled,text_injection_status,text_injection_error,last_injected_text,last_injected_at}'
```

稳定化待办：

- UI 展示文本注入状态和最近错误。
- final transcript 去重，避免服务端重复 final 时重复输入。
- 注入失败时丢弃当前 portal session，下次重新走授权/session 建立。
- 补 `portal-paste` provider，作为长文本和不支持 Unicode 输入序列应用的 fallback。

## 一键启动脚本

新增：

```text
desktop/cmd/voiced/
desktop/scripts/start-voice-input.sh
```

脚本职责：

- 构建 CrossHare 桌面端。
- 显式执行 `localman --speech start --json` 启动语音识别服务。
- 安装/更新用户级 `crosshare.service`。
- 写入 `~/.config/systemd/user/crosshare.service.d/10-voice-input.conf`，启用：

```ini
[Service]
Environment=CROSSHARE_SPEECH_AUTOSTART=1
Environment=CROSSHARE_TEXT_INJECT=1
Environment=CROSSHARE_TEXT_INJECT_MODE=portal-unicode
Environment=CROSSHARE_TEXT_INJECT_PREAUTHORIZE=1
```

- 安装 `voiced` 到 `~/.local/bin`。
- 导入当前图形会话环境到 systemd user manager，便于 Wayland portal 授权窗口正常出现。
- 重启 CrossHare，使后端接收、speech runtime、portal-unicode 光标注入一起就绪。
- CrossHare 启动后异步预申请 RemoteDesktop portal 授权；用户点击允许后，后续识别文本可直接注入。

运行入口：

```bash
cd <repo>/desktop
go run ./cmd/voiced start
```

安装后也可以直接运行：

```bash
voiced start
voiced status
voiced logs
```

## 录音文件管理

新增 SQLite 录音记录表：

```text
audio_recordings
```

后端在 `/api/audio/stream` 完成 WAV 写入和实时识别 `Commit()` 后，写入：

- device id/name
- session id
- WAV 文件名和本地路径
- 文件大小
- sample rate/channels/bits per sample
- final transcript
- started/completed timestamp

录音清理 timer 已改为每天执行一次：

```text
crosshare-audio-prune.timer
```

当前本机已安装并启用：

```text
Loaded: ~/.config/systemd/user/crosshare-audio-prune.timer
Active: active (waiting)
Next trigger: 2026-04-30 00:00:00 BST
```

清理脚本仍默认保留最近约 1GB 录音；删除 WAV 时会同步删除 `audio_recordings.local_path` 对应记录，并清理已经不存在的录音文件记录。

验证：

```bash
cd <repo>/desktop
go test ./...
```

结果通过。当前历史录音文件没有回填 transcript，`audio_recordings` 从本次改动之后的新录音开始记录完整识别文本。
