#!/bin/sh
# The bill — act one of the demo.
#
# The same job, trained for the same wall time, under three checkpointing settings. Nothing is at
# risk here and nothing goes wrong: this act is purely about what the writes cost, which is a number
# almost nobody has ever computed for their own job.
#
#   A  the team's own timer, every 1 s   (the world's "every 10 minutes", scaled down to fit a demo)
#   B  us, window 1 s                    (the same cadence — we must not cost more to cost the same)
#   C  us, window 3 s                    (the world's "every 30 minutes")
#
# Every number comes from a run: the checkpoints are counted on disk and the write cost is the
# trainer's own measured write, not our estimate.
set -eu

if [ -z "${CKPT_BUDGET_ARMED:-}" ]; then
	export CKPT_BUDGET_ARMED=1
	exec timeout --preserve-status "${BUDGET:-90}" sh "$0" "$@"
fi

RIG=$(cd "$(dirname "$0")/../rig" && pwd)
ROOT=$(cd "$RIG/../.." && pwd)
BIN="$RIG/.bin"
mkdir -p "$BIN"
if [ ! -x "$BIN/ckptctl" ] || [ -n "$(find "$ROOT/cmd" "$ROOT/internal" -name '*.go' -newer "$BIN/ckptctl" -print -quit)" ]; then
	(cd "$ROOT" && go build -o "$BIN/ckptctl" ./cmd/ckptctl && go build -o "$BIN/ckpttrainer" ./cmd/ckpttrainer)
fi

WALL=${WALL:-10}      # seconds each profile trains
STEP_MS=20
WRITE_MS=150

# count_sealed <dir> -> checkpoints whose manifest landed (a torn one does not count)
count_sealed() {
	n=0
	for d in "$1"/step-*; do
		[ -f "$d/manifest.json" ] && n=$((n + 1))
	done
	printf '%s' "$n"
}

# write_ms <dir> -> the trainer's own measure of its last write, in milliseconds
write_ms() { cat "$1/.ckpt-governor/write-rate" 2>/dev/null || echo 0; }

# profile <label> <arm: timer|window> <period> -> "label sealed write_ms wall tax"
profile() {
	label=$1 arm=$2 period=$3
	dir=$(mktemp -d)
	if [ "$arm" = "timer" ]; then
		"$BIN/ckpttrainer" --dir "$dir" --steps 100000 --step-ms $STEP_MS --write-ms $WRITE_MS \
			--save-every "$period" >/dev/null 2>&1 &
	else
		"$BIN/ckptctl" run --dir "$dir" --no-us=none --window "$period" --every 50ms >/dev/null 2>&1 &
	fi
	ctl=$!
	"$BIN/ckpttrainer" --dir "$dir" --steps 100000 --step-ms $STEP_MS --write-ms $WRITE_MS >/dev/null 2>&1 &
	job=$!
	sleep "$WALL"
	kill -9 "$job" "$ctl" 2>/dev/null || true
	sleep 0.05
	n=$(count_sealed "$dir")
	w=$(write_ms "$dir")
	# tax = the share of wall time spent writing. With zero writes it is zero, not a division error.
	tax=$(awk "BEGIN{ if ($WALL <= 0) print 0; else printf \"%.1f\", ($n * $w) / ($WALL * 1000) * 100 }")
	printf '%-14s sealed=%-5s write=%sms tax=%s%%\n' "$label" "$n" "$w" "$tax" >&2
	printf '%s %s %s %s' "$label" "$n" "$w" "$tax"
	rm -rf "$dir"
}

say() { printf '%s\n' "$*"; }

say "the bill: the same job, ${WALL}s of training, three checkpointing settings"
say ""

a=$(profile "timer-1s" timer "1s")
b=$(profile "us-window-1s" window "1s")
c=$(profile "us-window-3s" window "3s")

set -- $a; a_n=$2; a_w=$3; a_t=$4
set -- $b; b_n=$2; b_w=$3; b_t=$4
set -- $c; c_n=$2; c_w=$3; c_t=$4

say ""
printf '  %-16s %-10s %-12s %-8s\n' "setting" "checkpoints" "per write" "tax"
printf '  %-16s %-10s %-12s %-8s\n' "the timer, 1s" "$a_n" "${a_w}ms" "${a_t}%"
printf '  %-16s %-10s %-12s %-8s\n' "us, window 1s" "$b_n" "${b_w}ms" "${b_t}%"
printf '  %-16s %-10s %-12s %-8s\n' "us, window 3s" "$c_n" "${c_w}ms" "${c_t}%"

# The same arithmetic at the scale a real run uses: a four-hour training, a 1.2s write.
say ""
say "  scaled to a four-hour run with a 1.2s write:"
say "    every 10 minutes  ->  24 writes  ->  $(awk 'BEGIN{printf "%.0f", 24*1.2}')s of GPU time"
say "    every 30 minutes  ->   8 writes  ->  $(awk 'BEGIN{printf "%.0f", 8*1.2}')s of GPU time"
say "    the difference is two thirds of the write time, and nothing at risk is left uncovered:"
say "    the announced faults are still caught, because those are not a timer's job."
