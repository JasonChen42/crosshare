#!/usr/bin/env bash
set -euo pipefail

DEFAULT_LIMIT_BYTES=$((1024 * 1024 * 1024))
DEFAULT_INTERVAL_SECONDS=300
DEFAULT_ROOT_DIR="${HOME}/Pictures/CrossHare/Audio"
CONFIG_PATH="${HOME}/.crosshare/config.json"

ROOT_DIR=""
LIMIT_BYTES="$DEFAULT_LIMIT_BYTES"
INTERVAL_SECONDS="$DEFAULT_INTERVAL_SECONDS"
MODE="watch"
DRY_RUN=0

usage() {
	cat <<EOF
Usage: $(basename "$0") [options]

Monitor the CrossHare audio directory and delete the oldest files when the
total size exceeds the configured limit.

Options:
  --root PATH         Audio root directory to monitor
  --limit-bytes N     Maximum allowed bytes before pruning starts
  --interval SECONDS  Watch interval in seconds (default: $DEFAULT_INTERVAL_SECONDS)
  --once              Run one cleanup pass and exit
  --dry-run           Show what would be deleted without removing files
  --help              Show this message
EOF
}

load_default_root() {
	if [[ -n "${ROOT_DIR}" ]]; then
		return
	fi

	if [[ -f "$CONFIG_PATH" ]] && command -v python3 >/dev/null 2>&1; then
		local target_folder
		target_folder="$(python3 - "$CONFIG_PATH" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], "r", encoding="utf-8") as f:
        data = json.load(f)
    value = data.get("target_folder", "")
    if isinstance(value, str):
        print(value)
except Exception:
    pass
PY
)"

		if [[ -n "$target_folder" ]]; then
			ROOT_DIR="${target_folder}/Audio"
			return
		fi
	fi

	ROOT_DIR="$DEFAULT_ROOT_DIR"
}

log() {
	printf '[audio-prune] %s\n' "$*"
}

current_size_bytes() {
	local root="$1"
	find "$root" -type f -printf '%s\n' 2>/dev/null | awk '{sum += $1} END {print sum + 0}'
}

prune_once() {
	local root="$1"
	local total_size

	if [[ ! -d "$root" ]]; then
		log "skip: directory does not exist: $root"
		return 0
	fi

	total_size="$(current_size_bytes "$root")"
	log "scan: root=$root total_bytes=$total_size limit_bytes=$LIMIT_BYTES"

	if (( total_size <= LIMIT_BYTES )); then
		log "within limit: nothing to delete"
		return 0
	fi

	while (( total_size > LIMIT_BYTES )); do
		local oldest_entry
		oldest_entry="$(find "$root" -type f -printf '%T@\t%s\t%p\n' 2>/dev/null | sort -n | head -n 1 || true)"

		if [[ -z "$oldest_entry" ]]; then
			log "stop: no more files found but size is still above limit"
			break
		fi

		local file_size
		local file_path
		file_size="$(printf '%s' "$oldest_entry" | cut -f2)"
		file_path="$(printf '%s' "$oldest_entry" | cut -f3-)"

		if [[ -z "$file_path" || ! -f "$file_path" ]]; then
			log "skip: stale entry: $oldest_entry"
			total_size="$(current_size_bytes "$root")"
			continue
		fi

		if (( DRY_RUN == 1 )); then
			log "dry-run delete: $file_path ($file_size bytes)"
		else
			log "delete: $file_path ($file_size bytes)"
			rm -f -- "$file_path"
		fi

		total_size=$(( total_size - file_size ))
	done

	if (( DRY_RUN == 0 )); then
		find "$root" -depth -type d -empty -delete 2>/dev/null || true
	fi

	log "done: total_bytes=$total_size"
}

while (( $# > 0 )); do
	case "$1" in
	--root)
		ROOT_DIR="${2:?missing value for --root}"
		shift 2
		;;
	--limit-bytes)
		LIMIT_BYTES="${2:?missing value for --limit-bytes}"
		shift 2
		;;
	--interval)
		INTERVAL_SECONDS="${2:?missing value for --interval}"
		shift 2
		;;
	--once)
		MODE="once"
		shift
		;;
	--dry-run)
		DRY_RUN=1
		shift
		;;
	--help)
		usage
		exit 0
		;;
	*)
		printf 'Unknown option: %s\n\n' "$1" >&2
		usage >&2
		exit 1
		;;
	esac
done

load_default_root

case "$MODE" in
once)
	prune_once "$ROOT_DIR"
	;;
watch)
	log "watching: root=$ROOT_DIR interval=${INTERVAL_SECONDS}s limit_bytes=$LIMIT_BYTES"
		while true; do
			prune_once "$ROOT_DIR"
			sleep "$INTERVAL_SECONDS"
		done
	;;
*)
		printf 'Unknown mode: %s\n' "$MODE" >&2
		exit 1
		;;
esac
