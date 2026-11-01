#!/bin/sh
# scenarios.sh runs the three situations a user actually meets, and asserts what each one decides.
#
# It is the executable version of the README: if a change makes the product do something else, this
# script says so in one line instead of a paragraph. Run it before merging anything that touches the
# conditions, the plan or the executor.
set -eu

cd "$(dirname "$0")/../.."
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
failures=0

CKPTCTL="$TMP/ckptctl"
TRAINER="$TMP/ckpttrainer"
go build -o "$CKPTCTL" ./cmd/ckptctl
go build -o "$TRAINER" ./cmd/ckpttrainer

# expect <dir> <what the decision must name>
#
# Either line counts: `why` is the gate's answer ("cooldown"), `reason` is the condition that fired.
# A scenario that asserts the condition must not fail because a gate did its job.
expect() {
	if "$CKPTCTL" explain --dir "$1" 2>/dev/null | grep -Eq "^(why|reason) .*$2"; then
		printf 'ok    %-24s %s\n' "$(basename "$1")" "$2"
	else
		printf 'FAIL  %-24s wanted %s\n' "$(basename "$1")" "$2"
		"$CKPTCTL" explain --dir "$1" 2>/dev/null | sed 's/^/      /'
		failures=$((failures + 1))
	fi
}

# 1. The user stops the job. It writes `stopping` and spends its grace period on one last write.
d="$TMP/user-stop"
mkdir -p "$d/.ckpt-governor"
printf '1' >"$d/.ckpt-governor/stopping"
printf '1' >"$d/.ckpt-governor/may-exit"
"$CKPTCTL" run --once --dir "$d" --no-us=none >/dev/null
expect "$d" "risk.shutdown"

# 2. The platform is filling up. Nobody announced a preemption; the observer noticed the node is at
#    92% with two pods queued, which is an inference and says so.
d="$TMP/eviction"
mkdir -p "$d"
"$CKPTCTL" world --dir "$d" --fake --nodes 3 >/dev/null
"$CKPTCTL" run --once --dir "$d" --no-us=none >/dev/null
expect "$d" "risk.eviction-likely"

# 3. The disk is nearly full. Once a checkpoint exists the storage rule has a scale to measure
#    against: the trainer obeys the plan from (1), writes one copy, and the second `--once` — in the
#    same directory, because a one-shot must re-decide — sees the room running out.
d="$TMP/full-disk"
mkdir -p "$d/.ckpt-governor"
printf '1' >"$d/.ckpt-governor/ending"
"$CKPTCTL" run --once --dir "$d" --no-us=none >/dev/null
"$TRAINER" --dir "$d" --steps 2 --step-ms 10 --write-ms 40 >/dev/null 2>&1
rm -f "$d/.ckpt-governor/ending"
"$CKPTCTL" run --once --dir "$d" --no-us=none --free-factor=1000000 >/dev/null
expect "$d" "store.low-space"

if [ "$failures" -gt 0 ]; then
	printf '\n%d scenario(s) did not decide what they are for\n' "$failures"
	exit 1
fi
printf '\n3 scenarios, 3 expected decisions\n'
