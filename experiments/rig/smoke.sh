#!/bin/sh
# The real-clock smoke: the same binaries the rig uses, on a machine where time actually
# passes, for MINUTES minutes (default 10) with a WINDOW of 60s.
#
# It is a sanity check, not a substitute for the virtual day
# (`internal/loop`: TestATwentyFourHourRunKeepsTheWindow): a virtual day can assert 48 writes and a
# 30m bound exactly, and this can only prove the pieces fit together on a real clock — the plan
# reaches a real process, the write lands, the window restarts from the seal, and the stop is
# answered.
set -eu

RIG=$(cd "$(dirname "$0")" && pwd)
. "$RIG/lib.sh"

MINUTES=${MINUTES:-10}
WINDOW=${WINDOW:-60s}
WRITE_MS=${WRITE_MS:-2000}
STEP_MS=${STEP_MS:-100}

build
d="$TMP/smoke"
mkdir -p "$d"

"$BIN/ckptctl" run --dir "$d" --no-us=600s --window="$WINDOW" --estimate="${WRITE_MS}ms" \
	--every=200ms --teardown=5s >"$TMP/ctl.log" 2>&1 &
ctl=$!
"$BIN/ckpttrainer" --dir "$d" --steps 1000000 --step-ms "$STEP_MS" --write-ms "$WRITE_MS" \
	--grace 20s >"$TMP/job.log" 2>&1 &
job=$!

start=$(date +%s)
sleep "${MINUTES}m"
elapsed=$(( $(date +%s) - start ))

# The user's stop, and the handshake that answers it: the job spends its grace period on one final
# write, we write `may-exit`, and the job leaves. A smoke that kills -9 would skip exactly the thing
# the smoke exists to see.
kill -TERM "$job" 2>/dev/null || true
i=0
while [ ! -f "$d/.ckpt-governor/may-exit" ] && [ "$i" -lt 200 ]; do
	sleep 0.1
	i=$((i + 1))
done
kill "$ctl" 2>/dev/null || true
wait 2>/dev/null || true

python3 - "$d" "$elapsed" "$WINDOW" "$WRITE_MS" <<'PY'
import json, os, sys, time

d, elapsed, window, write_ms = sys.argv[1], int(sys.argv[2]), sys.argv[3], int(sys.argv[4])
base = os.path.join(d, ".ckpt-governor")


def span(text):
    """60s / 10m / 1h30m -> seconds. The script's own numbers, in the units a person says them."""
    total, num = 0, ""
    for ch in text:
        if ch.isdigit() or ch == ".":
            num += ch
            continue
        unit = {"s": 1, "m": 60, "h": 3600}[ch]
        total += float(num) * unit
        num = ""
    return total


limit = span(window) + 2 * (write_ms / 1000.0) + 5  # the request goes out one W early; +one write
led = json.load(open(os.path.join(base, "internal.json")))
st = led.get("status", led)
writes = st.get("writes") or []
ends = sorted(w.get("endedAt", 0) for w in writes if w.get("endedAt"))
gaps = [b - a for a, b in zip(ends, ends[1:])]
longest = max(gaps) if gaps else 0
failures = st.get("failures", 0)
released = os.path.exists(os.path.join(base, "may-exit"))
per_minute = len(ends) / (elapsed / 60.0) if elapsed else 0
print("smoke %dm · %d writes (%.1f/min) · longest gap %.1fs (window %s + W %.0fs) · %d failures · stop answered: %s"
      % (elapsed // 60 or 1, len(ends), per_minute, longest, window, write_ms / 1000.0, failures,
         "yes" if released else "NO"))
bad = []
if elapsed < 60:
    bad.append("the smoke ran for less than a minute")
if failures:
    bad.append("%d write failures" % failures)
if not released:
    bad.append("the job's stop was never answered (no may-exit)")
if longest > limit:
    bad.append("the window was broken: %.0fs between seals, the bound is %.0fs" % (longest, limit))
if not ends:
    bad.append("no checkpoint was ever written")
if len(ends) > (elapsed / span(window)) + 4:
    bad.append("%d writes in %ds is more than the window allows" % (len(ends), elapsed))
for b in bad:
    print("FAIL: " + b)
if bad:
    # A failure without the facts behind it is a bug report nobody can act on.
    print("  rounds %s · decisions %s · failure %r · window due %s · refreshed %s"
          % ((st.get("overhead") or {}).get("rounds"), (st.get("overhead") or {}).get("decisions"),
             st.get("failure"), st.get("windowDueAt"), st.get("windowRefreshedAt")))
    print("  gaps: " + " ".join("%.0f" % g for g in sorted(gaps, reverse=True)[:6]))
sys.exit(1 if bad else 0)
PY

record l1 real-clock-smoke ours "{\"ok\":1,\"minutes\":$((elapsed / 60)),\"window\":\"$WINDOW\"}"
