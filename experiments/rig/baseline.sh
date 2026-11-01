#!/bin/sh
# The baseline arms, written the way teams actually write them (docs/experiments-matrix.md §0.5).
#
#   b1  the timer            save every N minutes, nothing else — by far the most common
#   b2  the timer + rotate   same, keeping the last two copies, because somebody noticed the disk
#   b3  the timer + SIGTERM  b2 plus a signal handler that takes one last save — the strongest
#                            baseline, and the one a careful team actually ships
#
# All three copy the job's step directory with `cp -a`: that is what a `torch.save` into a new folder
# looks like from the outside, non-atomic and unverified, which is the point. A baseline that used an
# atomic rename would be our write protocol wearing a costume.
set -eu

DIR=""
PERIOD=2
MODE=b3
KEEP=2

while [ $# -gt 0 ]; do
	case "$1" in
	--dir) DIR="$2"; shift 2 ;;
	--period) PERIOD="$2"; shift 2 ;;
	--mode) MODE="$2"; shift 2 ;;
	--keep) KEEP="$2"; shift 2 ;;
	*) echo "baseline: unknown argument $1" >&2; exit 2 ;;
	esac
done
[ -n "$DIR" ] || { echo "baseline: --dir is required" >&2; exit 2; }

DEST="$DIR/.backup"
mkdir -p "$DEST"

# snapshot copies the newest step directory the job has finished. Non-atomic on purpose: a kill in the
# middle of this copy is exactly the failure mode under test.
snapshot() {
	newest=""
	for d in "$DIR"/step-*; do
		[ -d "$d" ] || continue
		newest="$d"
	done
	[ -n "$newest" ] || return 0
	name=$(basename "$newest")
	[ -d "$DEST/$name" ] && return 0
	cp -a "$newest" "$DEST/$name" 2>/dev/null || true
	rotate
}

rotate() {
	[ "$MODE" = "b1" ] && return 0
	# keep the newest $KEEP copies, drop the rest (oldest first)
	ls -1d "$DEST"/step-* 2>/dev/null | sort -t- -k2 -n | head -n -"$KEEP" | while read -r old; do
		rm -rf "$old"
	done
}

stopped=0
on_term() {
	stopped=1
	# b3's one extra capability: take a last save before leaving.
	snapshot
	exit 0
}
trap on_term TERM INT

while [ "$stopped" = "0" ]; do
	snapshot
	sleep "$PERIOD"
done
