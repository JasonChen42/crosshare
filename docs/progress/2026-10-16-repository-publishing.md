# CrossHare 代码发布与远端分支统一 — 2026-10-16

把积压在本地工作区的改动落成提交，发布到两个远端，并把局域网 Gitea 的默认分支
从 `master` 换成 `main`，与 GitHub 保持一致。

## 提交

- `fe1feee` 桌面端：上传保存后合并桌面通知；照片库本机 UI 输出原图路径并支持
  「在文件夹打开」（细节见 `2026-10-15-library-entry-and-notices.md`）。
- `a7cc480` Android：启动自动恢复同步、服务未运行时排队上传、上传完成显示电脑地址。
- 连同此前 5 个本地提交（`3f3fcc3`、`8469296`、`d13cb0b`、`35db212`、`779b1f7`）一并发布。

## 远端

- `github`（`git@github.com:JasonChen42/crosshare.git`）：`main` 由 `5bd7003` 推进到 `a7cc480`。
- `origin`（局域网 Gitea v1.25.4，网页 `http://192.168.1.142:3000`）：URL 从
  `ssh://u0_a202@192.168.1.251:2222/ubuntu/crosshare.git` 改为 `192.168.1.142`，用户名、端口、
  路径不变。默认分支原本是 `master`，Gitea 拒绝删除默认分支（`branch master is the default
  branch and cannot be deleted`），因此先用 API `PATCH /api/v1/repos/ubuntu/crosshare`
  (`{"default_branch":"main"}`) 改默认分支，再删除 `master`。
- 核对结果：`git ls-remote --symref origin` 只剩 `refs/heads/main`（`a7cc480`）且
  `HEAD -> refs/heads/main`；API 的 `default_branch` 为 `main`。

## 验证

- 桌面端 `go build ./...`、`go vet ./...`、`go test ./...` 全部通过。
- Android `./gradlew testDebugUnitTest --rerun` 强制重跑：4 个测试类 8 个用例，0 失败。
- 本轮只做发布与远端整理，没有重新构建或部署二进制与 APK；用户机器上的服务仍是
  2026-10-15 部署的版本。

## 遗留与注意事项

- 本地 `main` 的上游仍是 `github/main`；`origin/main` 已在位但未设为上游，
  需要时可以 `git push -u origin main`（或两个远端各保留一条推送命令）。
- Gitea 的 SSH 只放行 git 协议命令（`u0_a202@…:2222` 无 shell，普通命令返回
  `Invalid repo name`），改默认分支、删默认分支这类仓库设置必须走网页或 API。
- 本次操作使用的 Gitea 令牌是临时授权，建议在 Gitea「设置 → 应用」里按需撤销。
