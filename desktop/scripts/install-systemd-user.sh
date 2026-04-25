#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BINARY_NAME=crosshare
BIN_DIR="${HOME}/.local/bin"
UNIT_DIR="${HOME}/.config/systemd/user"
UNIT_NAME=crosshare.service

mkdir -p "$BIN_DIR" "$UNIT_DIR"

cd "$ROOT_DIR"
make build
install -m 0755 "$ROOT_DIR/$BINARY_NAME" "$BIN_DIR/$BINARY_NAME"
install -m 0644 "$ROOT_DIR/deploy/systemd/$UNIT_NAME" "$UNIT_DIR/$UNIT_NAME"

systemctl --user daemon-reload
systemctl --user enable --now "$UNIT_NAME"

LINGER_STATUS=unknown
if command -v loginctl >/dev/null 2>&1; then
	if [ "$(loginctl show-user "$USER" -p Linger --value 2>/dev/null || true)" = "yes" ]; then
		LINGER_STATUS=enabled
	elif loginctl enable-linger "$USER" 2>/dev/null; then
		LINGER_STATUS=enabled
	else
		LINGER_STATUS=failed
	fi
else
	LINGER_STATUS=unavailable
fi

cat <<EOF
CrossHare 已安装并启动。

本机 UI:
  http://127.0.0.1:8384/

查看状态:
  systemctl --user status $UNIT_NAME

查看日志:
  journalctl --user -u $UNIT_NAME -f

EOF

case "$LINGER_STATUS" in
	enabled)
		cat <<EOF
开机自启动:
  已启用 linger，机器开机后即使用户未登录也会启动。
EOF
		;;
	failed)
		cat <<EOF
开机自启动:
  未能自动启用 linger，当前会在用户登录后启动。
  请手动运行下面的命令，系统可能会弹出图形授权窗口:
  loginctl enable-linger "$USER"
EOF
		;;
	unavailable)
		cat <<EOF
开机自启动:
  当前系统没有 loginctl，服务已启用为用户登录后启动。
EOF
		;;
esac
