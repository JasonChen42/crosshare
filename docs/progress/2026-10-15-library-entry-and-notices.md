# CrossHare 本机入口与保存通知 — 2026-10-15

用户反馈：手机上传到本机的照片，希望能方便地在本地浏览器打开找到，并且要有一个可复制的地址。
确认的交互方向：上传后弹桌面通知（不自动开浏览器）；地址出现在网页、通知、手机 App 三处。

## 桌面端

- 新增 `desktop/internal/server/notify.go`：`uploadNotifier` 把突发上传合并成一条桌面通知
  （4 秒防抖，一批上传只响一次），Linux 用 `notify-send`，macOS 用 `osascript`，
  其他平台只写日志。通知文案为「已保存 N 项新照片 · 浏览器打开 http://127.0.0.1:8384/」。
- `handleMediaUpload` 保存成功后调用 `notifySavedUpload`；本机浏览器导入
  （`/api/ui/library/import`，设备 ID 固定为 `desktop`）不触发通知，避免用户已在页面上还被打扰。
- `Server.Stop` 停止未发出的通知；`main.go` 两种运行模式启动日志都输出照片库地址。
- 照片库页（`web/index.html` + `library.js` + `library.css`）在标题栏下新增常驻地址栏：
  显示当前地址（`location.origin`，带跳转链接）、「复制地址」按钮（clipboard API，
  失败回退 `execCommand`），右侧显示原图保存目录。目录来自 `/api/ui/library/devices`
  新增的 `target_folder` 字段（该接口本来就仅限 loopback + 同源）。
- 语音页（`ui.go` 模板）同样新增「照片库地址」面板，带复制按钮，与照片库页行为一致。

## Android

- 新增 `service/SyncMessages.kt`：纯函数 `savedNotice(savedCount, port)` 生成
  「已在电脑保存 N 项 · 电脑浏览器打开 http://127.0.0.1:8384/」，端口取配对时保存的电脑端口
  （loopback 地址，用户回到电脑浏览器即可打开；照片库 UI 按设计不向远端浏览器开放）。
- `SyncService.ServiceState` 新增 `savedNotice`：同步开始时清空，上传完成且确有上传时写入；
  全部照片电脑已有时不显示。
- `PhotoHomeScreen` 在提示区渲染 `savedNotice`，并用 `SelectionContainer` 包裹，地址可长按选择复制。

## 验证

- Go `go build ./...`、`go vet ./...`、`go test ./...` 与 `go test -race ./...` 全部通过。
  新增 `notify_test.go`：突发合并为一条、多批各自一条、各平台命令参数、
  手机上传通知而浏览器导入不通知（隔离数据目录 + 临时目标文件夹）。
- Android `testDebugUnitTest`（新增 `SyncMessagesTest`）、`assembleDebug` 通过。
- 隔离实例（端口 18384 + 独立数据目录）端到端：配对 → 上传 15 字节文件，
  4 秒防抖后日志输出「已保存 1 项新照片 · 浏览器打开 http://127.0.0.1:18384/」
  （沙箱内 notify-send 实际执行成功）。
- 无头 Chrome（`--dump-dom`）验证渲染结果：照片库页地址栏显示
  `http://127.0.0.1:18384/` 且 href 正确、原图位置文本填充；语音页 `libraryURL` 同样填充。
  `node --check` 校验 library.js 语法。

## 遗留

- 通知只覆盖 Linux（notify-send）与 macOS（osascript）；Windows 仅日志。地址栏复制在
  非安全上下文（非 localhost 的 http）下 clipboard API 不可用，已回退 execCommand。
- 未在用户正式服务与真机上部署；systemd 服务当前仍是旧二进制，手机 App 仍是旧 APK。

## 部署（2026-10-15 23:07，用户授权桌面 + 手机都部署）
- 替换前备份二进制、配置与 SQLite 到 `~/.crosshare/backups/20261015-230640-notices/`。
- 新桌面二进制（23,746,163 字节）安装到 `~/.local/bin/crosshare`，重启 `crosshare.service`，
  服务 active，`http://127.0.0.1:8384/` 返回 200，页面包含新地址栏；
  启动日志输出「Photo library ready: http://127.0.0.1:8384/」，语音页包含「照片库地址」面板。
- `adb install -r` 重装 PGKM10（保留数据），MainActivity 启动正常，进程持续运行，
  无 AndroidRuntime 错误；SyncService 保持运行，手机重连后 `last_seen` 立即更新、
  `recent` 为 true，正式库计数 4,175。
- 手机上传后的通知与地址提示需要真机上传照片做最终目视确认；本机验证已在隔离实例完成
  （配对 → 上传 → 4 秒防抖后发出带地址的通知）。

## 修正：原图路径与「在文件夹打开」（同日追加）

第一轮把「可复制的地址」做成了网页 URL，用户实际要的是**预览页里每张原图在这台电脑上的
文件路径，可点击在文件夹打开**（「下载原图」按钮是原有行为，非本轮新增）。

- `storage.LibraryItem.LocalPath` 改为输出 `local_path`；`libraryList` 只对通过
  `libraryLocal`（loopback + 同源）的本机 UI 请求输出路径，手机 `/api/library` 响应里
  该字段被抹掉，桌面文件路径不外泄。
- 新增 `POST /api/ui/library/reveal/{hash}`（同样只限本机 UI + 自定义请求头）：
  浏览器无法自己打开文件夹，由本机服务执行平台命令 —— Linux `xdg-open <目录>`、
  macOS `open -R <文件>`、Windows `explorer /select,<文件>`；文件不存在返回 404。
  `Server.reveal` 可注入，测试用假 runner 记录调用。
- 预览对话框在「下载原图」下方新增「原图位置」行：等宽字体显示完整绝对路径，
  点击路径即在文件管理器中打开所在文件夹，旁边「复制路径」按钮复制该路径
  （clipboard API，失败回退 `execCommand`）；原图不在本机时置灰。

## 验证补充

- 新增测试：`TestLibraryRevealsOriginalPathOnlyForLocalUI`（UI 有路径 / 手机无路径）、
  `TestRevealOriginalOpensFolderForLocalUIOnly`（跨来源、缺请求头、远端 peer 一律 403；
  未知哈希 404；本机请求 200 且 `xdg-open` 目标为文件所在目录）、
  `TestRevealCommandPerPlatform`。
- Node + `vm` 桩 DOM 驱动真实 `library.js`：打开预览后路径文本完整、按钮可点、
  点击发出带 `X-CrossHare-UI` 的 reveal POST、复制按钮反馈正常，6 项全过。
- 正式服务（`http://127.0.0.1:8384/`）第一页 48 项全部带 `local_path`；
  对 `mmexport1791622247608.jpg` 真实调用 reveal，返回
  `/home/jason/Pictures/CrossHare/2026/2026-10-10/mmexport1791622247608.jpg`，
  日志记录 revealed，用户桌面弹出对应文件夹。

## 部署补充（2026-10-15 23:38）

- 备份到 `~/.crosshare/backups/20261015-233707-reveal/`，新二进制安装并重启
  `crosshare.service`，服务 active、图库 200；手机 App 无改动，无需重装。

## 撤回：摘掉网页地址栏（2026-10-15 23:55）

用户确认网页 URL 地址栏属于会错意的多余设计，予以摘掉。保留原图路径、「在文件夹打开」、
上传通知；语音页的「照片库地址」面板同属一类展示，一并移除。

- 照片库页移除 `local-bar`、`renderLocalAddress`、复制地址按钮及其样式；
  `loadChoices` 不再读取 `target_folder`，`/api/ui/library/devices` 相应字段撤回
  （语音页 `/api/ui/status` 的同步目录是原有功能，未动）。
- 语音页模板移除「照片库地址」面板、复制按钮与相关 JS。
- 验证：`gofmt`/`go vet`/`go test ./...` 全过，无残留引用；正式服务重启后图库 200、
  无头 Chrome 渲染 48 张照片正常；Node 桩 DOM 复跑 viewer 链路 4 项全过
  （路径显示、可点击、reveal POST、复制反馈）。
- 备份：`~/.crosshare/backups/20261015-235445-drop-addressbar/`。

## 追加：重装后上传按钮全灰的修正（同日 23:17）

重装 APK（`adb install -r`）后进程重启，`SyncService` 未运行，而上传相关按钮此前都要求
`isRunning`，于是整页上传入口被禁用。

- 新增 `service/SyncLaunchPolicy.kt`：纯函数 `shouldAutoStartSync(paired, userStopped)` ——
  已配对且用户没有在传输页主动暂停时，`MainActivity` 启动后自动拉服务；暂停状态持久化在
  `crosshare_server` 的 `sync_stopped`（`startSyncService` 清除、`stopSyncService` 写入）。
- `SyncService.enqueue`：没有绑定实例时，把所选哈希写入 `crosshare_transfers` 的 `uploads`，
  服务启动后由接收轮询取走，队列不丢；按钮文案改为「开启传输并上传」，点击先启服务再入队。
- 重复 `onStartCommand` 不再重扫媒体库（`isRunning` 守卫），避免服务重启时白扫一遍。
- `ServiceState` 同时新增 `savedNotice`（见上「上传通知」一节），本次一并提交。
- 验证：`SyncLaunchPolicyTest`（配对/暂停/未配对三种组合）、`SyncMessagesTest`；2026-10-16
  强制重跑 `testDebugUnitTest --rerun`，4 个测试类 8 个用例全部通过。
