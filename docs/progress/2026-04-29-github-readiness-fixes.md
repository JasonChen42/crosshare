# CrossHare GitHub 发布准备修复 - 2026-04-29

## 目标

根据公开发布前 review 结果，优先修复安全阻塞、安装可移植性和仓库公开文档问题。

## 已完成

### Android 暴露面收紧

- `SyncService` 和 `AudioStreamService` 改为 `android:exported="false"`。
- 应用禁用 `android:allowBackup`，避免配对 token 进入系统备份。
- `AudioStreamService` 在创建 `AudioRecord` 前显式检查 `RECORD_AUDIO` 权限。

提交：

```text
b343e65 Harden Android service exposure
```

### 桌面端配对和本地密钥加固

- `~/.crosshare` 配置目录使用 `0700` 权限。
- `config.json` 使用 `0600` 权限保存配对 token。
- 配对接口限制请求体大小，并按来源 IP 对错误配对码进行限流。
- 未认证的 `/api/status` 不再返回本地同步目标路径。
- 临时上传目录使用 `0700` 权限。

提交：

```text
017f2ac Harden desktop pairing and local secrets
```

### systemd 安装路径修复

- 录音清理脚本安装为 `~/.local/bin/crosshare-audio-prune`。
- `crosshare-audio-prune.service` 不再引用开发机仓库路径。
- `voiced start` 同步安装清理脚本。

提交：

```text
7410f6b Install audio prune script portably
```

### 公开仓库文档

- 新增 MIT `LICENSE`。
- README 改为描述当前真实同步路径：Android 主动推送到桌面端。
- README 增加可信局域网安全模型说明。

提交：

```text
b83b0c8 Document GitHub release security posture
```

## 待验证

- `desktop`: `go test ./...`
- `android`: `./gradlew testDebugUnitTest`
- `android`: `./gradlew lintDebug`
