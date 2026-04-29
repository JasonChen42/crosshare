#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BINARY_NAME=crosshare
UNIT_NAME=crosshare.service
BIN_DIR="${HOME}/.local/bin"
UNIT_DIR="${HOME}/.config/systemd/user"
DROPIN_DIR="${UNIT_DIR}/${UNIT_NAME}.d"
DROPIN_FILE="${DROPIN_DIR}/10-voice-input.conf"
UI_URL="${CROSSHARE_UI_URL:-http://127.0.0.1:8384/}"
UI_STATUS_URL="${CROSSHARE_UI_STATUS_URL:-http://127.0.0.1:8384/api/ui/status}"

PATH="${HOME}/.local/bin:${PATH}"
export PATH

log() {
	printf '%s\n' "$*"
}

have() {
	command -v "$1" >/dev/null 2>&1
}

find_localman() {
	if have localman; then
		command -v localman
		return 0
	fi
	if [ -x "${HOME}/.local/bin/localman" ]; then
		printf '%s\n' "${HOME}/.local/bin/localman"
		return 0
	fi
	return 1
}

systemd_user_available() {
	have systemctl && systemctl --user show-environment >/dev/null 2>&1
}

import_graphical_environment() {
	if ! systemd_user_available; then
		return 0
	fi

	systemctl --user import-environment \
		DISPLAY \
		WAYLAND_DISPLAY \
		XAUTHORITY \
		DBUS_SESSION_BUS_ADDRESS \
		XDG_CURRENT_DESKTOP \
		XDG_SESSION_DESKTOP \
		XDG_SESSION_TYPE \
		XDG_RUNTIME_DIR >/dev/null 2>&1 || true
}

build_crosshare() {
	log "构建 CrossHare 桌面端..."
	(
		cd "$ROOT_DIR"
		"${GO:-go}" build -o "$BINARY_NAME" ./cmd/crosshare/
	)
}

install_crosshare() {
	mkdir -p "$BIN_DIR" "$UNIT_DIR" "$DROPIN_DIR"
	install -m 0755 "$ROOT_DIR/$BINARY_NAME" "$BIN_DIR/$BINARY_NAME"
	install -m 0644 "$ROOT_DIR/deploy/systemd/$UNIT_NAME" "$UNIT_DIR/$UNIT_NAME"
	cat >"$DROPIN_FILE" <<'EOF'
[Service]
Environment=CROSSHARE_SPEECH_AUTOSTART=1
Environment=CROSSHARE_TEXT_INJECT=1
Environment=CROSSHARE_TEXT_INJECT_MODE=portal-unicode
EOF
}

start_speech_runtime() {
	localman_bin=$(find_localman) || {
		log "找不到 localman，无法启动本机语音识别服务。"
		log "请先安装/修复 localman，或确认 ${HOME}/.local/bin 在 PATH 中。"
		exit 1
	}

	log "启动语音识别服务..."
	"$localman_bin" --speech start --json
}

start_systemd_service() {
	import_graphical_environment
	install_crosshare

	log "启动 CrossHare 后端和光标文本注入..."
	systemctl --user daemon-reload
	systemctl --user enable --now "$UNIT_NAME"
	systemctl --user restart "$UNIT_NAME"
}

wait_for_ui() {
	if ! have curl; then
		return 0
	fi

	i=0
	while [ "$i" -lt 20 ]; do
		if curl -fsS "$UI_STATUS_URL" >/dev/null 2>&1; then
			return 0
		fi
		i=$((i + 1))
		sleep 0.5
	done

	log "CrossHare UI 暂未返回状态，请查看日志确认启动进度。"
}

print_next_steps() {
	cat <<EOF

已启动语音输入链路。

本机 UI:
  $UI_URL

查看 CrossHare 状态:
  systemctl --user status $UNIT_NAME

查看 CrossHare 日志:
  journalctl --user -u $UNIT_NAME -f

提示:
  第一次把识别文本输入到当前光标时，GNOME 可能会弹出 Remote Desktop 授权窗口。
  允许后，把焦点放在目标输入框中，再从手机端开始说话即可。
EOF
}

run_foreground() {
	log "当前没有可用的 systemd user 会话，改为前台启动 CrossHare。"
	log "保持这个终端窗口打开，按 Ctrl+C 停止。"
	CROSSHARE_SPEECH_AUTOSTART=1 \
	CROSSHARE_TEXT_INJECT=1 \
	CROSSHARE_TEXT_INJECT_MODE=portal-unicode \
		"$ROOT_DIR/$BINARY_NAME" --no-tray
}

main() {
	build_crosshare
	start_speech_runtime

	if systemd_user_available; then
		start_systemd_service
		wait_for_ui
		print_next_steps
	else
		run_foreground
	fi
}

main "$@"
