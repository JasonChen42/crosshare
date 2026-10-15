# CrossHare 真机部署与端到端跑通 — 2026-10-15

在保留 2026-09-08 未提交改动的工作区上，完成测试 → 桌面端部署 → PGKM10 真机安装 → 双向原图端到端验证。

## 测试

- 桌面端 `go test ./...` 与 `go test -race ./...`、`go vet ./...` 全部通过。
- Android `testDebugUnitTest` 4 个用例通过，`assembleDebug` 成功；APK 时间戳晚于全部源码，确认包含当前工作区改动。

## 部署

- 桌面端二进制重新构建（23.7 MB），替换前备份二进制、配置与 SQLite 到
  `~/.crosshare/backups/20261015-221059-runthrough/`。
- 停止服务后 `install -m 755` 覆盖 `~/.local/bin/crosshare`，重启 `crosshare.service`，
  服务保持 active，`http://127.0.0.1:8384/` 返回 200。
- 手机经无线调试（`adb pair` + `adb connect`）连接 PGKM10 / Android 15 / SDK 35，
  `adb install -r` 覆盖安装并保留数据；应用启动无 AndroidRuntime 错误。
- READ_MEDIA_IMAGES / READ_MEDIA_VIDEO / POST_NOTIFICATIONS 已授权。

## 重新配对

- 手机保存的电脑地址仍是旧 IP（桌面端更换网段后不再有效），
  App 显示「电脑暂时不可用，已显示本机照片：Failed to connect to /<保存的旧地址>:8384」。
- 通过设置 → 设备配对 → 取消配对 → 重新输入 IP / 端口 / 配对码完成配对，
  配对后桌面端 `last_seen` 立即更新，App 标题变为「jasonbourne · 已连接」。
- 取消配对会清除 `paired_computer_host`，属于预期行为；换网段后必须重新配对。

## 端到端验证

- 电脑 → 手机：导入一张手机上没有的新图（SHA-256 `4c0fc35c…741aa`，130,006 字节）后发送到 PGKM10，
  投递任务 14 秒内变为 `done`；手机 `Pictures/CrossHare/4c0fc35c….jpg` 落盘，
  拉回电脑后 SHA-256 与原文件完全一致。
- 先前发送的 `c9ef3a04…` 命中「本机已有同哈希文件」优化路径，直接确认不重复落盘，
  与该设计文档描述一致。
- 手机 → 电脑：照片页长按选中 `wx_camera_1792066267480.jpg` 上传，
  归档到 `~/Pictures/CrossHare/2026/2026-10-15/`（975,295 字节），图库计数 4,171 → 4,173。
- Web 图库本机验证：共 4,173 项、87 页、48 个缩略图请求，无 console 错误；
  缩略图接口返回合法 JPEG（240x320）。
- 手机进程持续运行，`SyncService` 前台服务保持，日志无崩溃。

## 遗留

- 自动化浏览器未取到自然宽高（标签页不可见时不解码），缩略图已用 curl 单独验证为合法 JPEG，
  未做人工目视确认。
- 测试导入的 `4c0fc35c…741aa` 已进入正式图库与手机相册；图库暂无删除接口，需手动清理。
- 真机上的后台中断续传、权限弹窗交互仍未自动化验证。
