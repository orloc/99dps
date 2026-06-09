#!/usr/bin/env bash
#
# EverQuest log maintenance for the 99dps meter's source logs:
#   1. rotate any eqlog_*.txt over THRESHOLD by gzip-archiving a copy and
#      truncating the original in place (EQ keeps appending to its open handle).
#      The original's mtime is preserved so the meter's "most-recently-active"
#      character detection isn't tripped by the truncate.
#   2. keep client logging enabled (Log=TRUE) for every character — but only
#      when EQ is closed, since EQ rewrites eqclient.ini on exit and would
#      clobber a live edit.
#
# Intended to run daily via the systemd user timer in scripts/systemd/.
# Rotation itself is size-gated (THRESHOLD), so it only fires when a log is
# actually oversized — in practice every few days of normal play.
set -euo pipefail

EQ_DIR="${EQ_DIR:-/mnt/storage/p99/drive_c/EQ2Lite}"
LOGS="$EQ_DIR/Logs"
ARCHIVE="$LOGS/archive"
INI="$EQ_DIR/eqclient.ini"
THRESHOLD="${THRESHOLD:-$((5 * 1024 * 1024))}" # 5 MB

mkdir -p "$ARCHIVE"

# --- 1. rotate oversized logs (copy-truncate) ------------------------------
shopt -s nullglob
for f in "$LOGS"/eqlog_*.txt; do
	size=$(stat -c '%s' "$f")
	[ "$size" -ge "$THRESHOLD" ] || continue

	base=$(basename "$f" .txt)
	dest="$ARCHIVE/${base}_$(date +%Y%m%d-%H%M%S).txt.gz"

	# remember the original mtime so the truncate doesn't look like fresh activity
	mref=$(mktemp)
	touch -r "$f" "$mref"

	gzip -c "$f" >"$dest" # archive first — if this fails, set -e aborts before truncate
	: >"$f"               # truncate in place; EQ's append handle continues at offset 0
	touch -r "$mref" "$f" # restore the original mtime
	rm -f "$mref"

	echo "rotated $base (${size} bytes) -> $(basename "$dest")"
done

# --- 2. keep logging enabled (only while EQ is not running) -----------------
if ! pgrep -fi 'eqgame\.exe' >/dev/null 2>&1; then
	if grep -q '^Log=FALSE' "$INI"; then
		sed -i 's/^Log=FALSE/Log=TRUE/' "$INI" # CRLF-safe: no end-anchor
		echo "re-enabled logging (Log=TRUE) in eqclient.ini"
	fi
fi
