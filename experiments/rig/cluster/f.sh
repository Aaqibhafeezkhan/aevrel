#!/bin/sh
# F1 — preemption on a real cluster, prepared once and run in seconds.
#
#   f.sh prepare        create the kind cluster, build the image, load it (slow, idempotent)
#   f.sh run preempt    deploy both arms, let the platform preempt them, measure
#   f.sh status         what the cluster thinks is running
#   f.sh down           delete the cluster
#
# The split exists because the slow part (a cluster, an image) has nothing to do with the measurement.
# A failed run is re-run with `f.sh run`, not from scratch.
set -eu

# Three minutes to say something, then it is killed (the one-minute rule in lib.sh is for single-process
# experiments; a cluster, two pods and a preemption are genuinely slower, and the first 60s run proved
# exactly that by dying silently at the checkpoint wait) (see experiments/rig/lib.sh). The cluster work is
# cached by prepare, so a run that needs more than this is a run that is stuck, not one that is slow.
if [ -z "${CKPT_BUDGET_ARMED:-}" ]; then
	export CKPT_BUDGET_ARMED=1
	exec timeout --preserve-status "${BUDGET:-180}" sh "$0" "$@"
fi

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
RIG="$ROOT/experiments/rig"
CLUSTER=${CLUSTER:-ckpt-exp}
# The kind cluster's published port is not always reachable from this host; the control-plane
# container's own address always is. One kubeconfig, generated at prepare time, used by every command
# here — so a run never depends on which wiring the wrapper happened to set up.
KUBECONFIG_FILE="$HERE/kubeconfig"
KUBECTL="kubectl --kubeconfig $KUBECONFIG_FILE --insecure-skip-tls-verify"
RESULTS="$RIG/results.jsonl"

say() { printf '%s\n' "$*"; }

# record appends one result line, in the same shape the local rig writes.
record() {
	printf '{"layer":"%s","id":"%s","arm":"%s","result":%s}\n' "$1" "$2" "$3" "$4" >>"$RESULTS"
}

prepare() {
	if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
		say "creating cluster $CLUSTER"
		kind create cluster --config "$HERE/kind.yaml" --wait 90s
	fi
	say "building static binaries"
	mkdir -p "$HERE/bin"
	(cd "$ROOT" && CGO_ENABLED=0 go build -o experiments/rig/cluster/bin/ckpttrainer ./cmd/ckpttrainer \
		&& CGO_ENABLED=0 go build -o experiments/rig/cluster/bin/ckptctl ./cmd/ckptctl)
	(cd "$ROOT/contrib/observer" && CGO_ENABLED=0 go build -o ../../experiments/rig/cluster/bin/ckpt-observer .)
	docker build -q -t ckpt-exp/job:local "$HERE" >/dev/null
	kind load docker-image ckpt-exp/job:local --name "$CLUSTER"
	# The naive arm's copy loop and the preemptor both use busybox. Loading it here means a node with
	# no egress still runs the experiment, and the run never fails on an image pull.
	if ! docker image inspect busybox:1.36 >/dev/null 2>&1; then
		# A workstation's docker config may name a credential helper that does not exist on this
		# host; an empty config still pulls a public image.
		mkdir -p /tmp/ckpt-dockercfg && printf '{}' >/tmp/ckpt-dockercfg/config.json
		DOCKER_CONFIG=/tmp/ckpt-dockercfg docker pull -q busybox:1.36 >/dev/null
	fi
	kind load docker-image busybox:1.36 --name "$CLUSTER"

	ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$CLUSTER-control-plane")
	kind get kubeconfig --name "$CLUSTER" | sed "s|https://127.0.0.1:[0-9]*|https://$ip:6443|" >"$KUBECONFIG_FILE"
	$KUBECTL wait --for=condition=Ready nodes --all --timeout=120s >/dev/null 2>&1 || true
	say "prepared"
}

# hostpath <node> <arm> -> the checkpoint directory on that node's disk
hostpath() { printf '/var/lib/ckpt-exp/%s' "$2"; }

# node_of <arm> -> the kind node container that carries the arm label
node_of() {
	case "$1" in
	governed) printf '%s-worker' "$CLUSTER" ;;
	naive) printf '%s-worker2' "$CLUSTER" ;;
	esac
}

run_preempt() {
	$KUBECTL delete pod --all -n ckpt-exp --ignore-not-found >/dev/null 2>&1 || true
	$KUBECTL apply -f "$HERE/arms.yaml" >/dev/null
	for arm in governed naive; do
		docker exec "$(node_of "$arm")" rm -rf "/var/lib/ckpt-exp/$arm" 2>/dev/null || true
	done
	$KUBECTL wait --for=condition=Ready pod/governed pod/naive -n ckpt-exp --timeout=120s >/dev/null

	# Wait until both arms have something to lose: one sealed checkpoint each, read off the node.
	i=0
	while [ "$i" -lt 200 ]; do
		g=$(docker exec "$(node_of governed)" sh -c 'ls -d /var/lib/ckpt-exp/governed/step-* 2>/dev/null | wc -l' 2>/dev/null || echo 0)
		n=$(docker exec "$(node_of naive)" sh -c 'ls -d /var/lib/ckpt-exp/naive/step-* 2>/dev/null | wc -l' 2>/dev/null || echo 0)
		if [ "${g:-0}" -ge 2 ] && [ "${n:-0}" -ge 2 ]; then break; fi
		sleep 0.2
		i=$((i + 1))
	done

	say "both arms have checkpoints; preempting: the platform decides, not this script"
	$KUBECTL apply -f "$HERE/preempt.yaml" >/dev/null

	# Both victims are gone when the API says so; the grace period is two seconds, so this is quick.
	$KUBECTL wait --for=delete pod/governed -n ckpt-exp --timeout=60s >/dev/null 2>&1 || true
	$KUBECTL wait --for=delete pod/naive -n ckpt-exp --timeout=60s >/dev/null 2>&1 || true

	work="$RIG/.cluster"
	rm -rf "$work"
	mkdir -p "$work"
	for arm in governed naive; do
		docker cp "$(node_of "$arm"):/var/lib/ckpt-exp/$arm" "$work/$arm" >/dev/null 2>&1 || true
	done

	n_ok=0; n_lost=0
	g_ok=0; g_lost=0
	if [ -d "$work/naive" ]; then
		line=$(python3 "$RIG/verify.py" "$work/naive/.backup" 2>/dev/null || true)
		case "$line" in *'"ok": true'*) n_ok=1 ;; esac
		n_peak=$(ls -d "$work/naive"/step-* 2>/dev/null | wc -l)
		n_lost=$(( (n_peak > 0) ? 1 : 0 ))
	fi
	if [ -d "$work/governed" ]; then
		line=$(python3 "$RIG/verify.py" "$work/governed" 2>/dev/null || true)
		case "$line" in *'"ok": true'*) g_ok=1 ;; esac
	fi

	# The governed arm's own record of why it wrote: the condition, read from the plan it published.
	pre=$(python3 - "$work/governed/.ckpt-governor/state.json" <<'PY' 2>/dev/null || true
import json, sys
try:
    s = json.load(open(sys.argv[1]))
except Exception:
    print("-"); raise SystemExit
codes = [r.get("code", "") for r in (s.get("reasons") or [])]
print(",".join(c for c in codes if c) or "-")
PY
)

	printf 'exp[f1] %-18s naive: intact=%s steps=%s · governed: intact=%s · reason=%s\n' \
		preempt "$n_ok" "${n_peak:-0}" "$g_ok" "${pre:--}"
	record f1 preempt naive "{\"ok\":$n_ok,\"steps\":${n_peak:-0}}"
	record f1 preempt governed "{\"ok\":$g_ok,\"reason\":\"${pre:--}\"}"

	if [ "$g_ok" != "1" ]; then
		say "FAIL: the governed arm lost its newest checkpoint to the preemption"
		return 1
	fi
	case "$pre" in
	*risk.preempt*) ;;
	*)
		say "FAIL: the governed arm's plan does not name risk.preempt (got '$pre')"
		return 1
		;;
	esac
	say "       the platform announced, the controller answered, the job wrote before it went"
}

# F2 — the user stops the job.
#
# The same cluster, the same two arms, a different fault: SIGTERM with a grace period, which is what
# Ctrl-C, `kubectl delete` and a reclaiming spot instance all look like from inside the process.
#
# This is the one experiment where the baseline can win, and the only one that is a *parity* claim:
# b3 has a SIGTERM handler too, so both arms take a final save. What separates them is the release —
# a controller answers "you may go" the moment the write lands, while a timer has to sit out the whole
# grace period because nothing tells it that it is done.
run_stop() {
	grace=${GRACE:-20}
	$KUBECTL delete pod --all -n ckpt-exp --ignore-not-found >/dev/null 2>&1 || true
	for arm in governed naive; do
		docker exec "$(node_of "$arm")" rm -rf "/var/lib/ckpt-exp/$arm" 2>/dev/null || true
	done
	say "deploying both arms"
	$KUBECTL apply -f "$HERE/arms.yaml" >/dev/null
	$KUBECTL wait --for=condition=Ready pod/governed pod/naive -n ckpt-exp --timeout=40s >/dev/null

	i=0
	while [ "$i" -lt 60 ]; do
		g=$(docker exec "$(node_of governed)" sh -c 'ls -d /var/lib/ckpt-exp/governed/step-* 2>/dev/null | wc -l' 2>/dev/null || echo 0)
		n=$(docker exec "$(node_of naive)" sh -c 'ls -d /var/lib/ckpt-exp/naive/step-* 2>/dev/null | wc -l' 2>/dev/null || echo 0)
		if [ "${g:-0}" -ge 2 ] && [ "${n:-0}" -ge 2 ]; then break; fi
		sleep 0.2
		i=$((i + 1))
	done

	say "stopping both jobs the way a user would: SIGTERM, ${grace}s of grace, measured to the millisecond"
	start=$(date +%s%N)
	$KUBECTL delete pod governed naive -n ckpt-exp --grace-period="$grace" --wait=false >/dev/null

	# When each pod disappears is the number this experiment is about.
	g_gone=""; n_gone=""
	i=0
	while [ "$i" -lt 200 ]; do
		el=$(( ($(date +%s%N) - start) / 1000000 ))
		if [ -z "$g_gone" ] && ! $KUBECTL get pod governed -n ckpt-exp >/dev/null 2>&1; then g_gone=$el; fi
		if [ -z "$n_gone" ] && ! $KUBECTL get pod naive -n ckpt-exp >/dev/null 2>&1; then n_gone=$el; fi
		if [ -n "$g_gone" ] && [ -n "$n_gone" ]; then break; fi
		sleep 0.2
		i=$((i + 1))
	done

	work="$RIG/.cluster"
	rm -rf "$work"
	mkdir -p "$work"
	for arm in governed naive; do
		docker cp "$(node_of "$arm"):/var/lib/ckpt-exp/$arm" "$work/$arm" >/dev/null 2>&1 || true
	done

	g_ok=0; n_ok=0; released=0
	if [ -d "$work/governed" ]; then
		case "$(python3 "$RIG/verify.py" "$work/governed" 2>/dev/null || true)" in *'"ok": true'*) g_ok=1 ;; esac
		[ -f "$work/governed/.ckpt-governor/may-exit" ] && released=1
	fi
	if [ -d "$work/naive" ]; then
		case "$(python3 "$RIG/verify.py" "$work/naive" 2>/dev/null || true)" in *'"ok": true'*) n_ok=1 ;; esac
	fi

	g_s=$(printf '%s' "${g_gone:-timeout}" | awk '{printf "%.1f", $1/1000}')
	n_s=$(printf '%s' "${n_gone:-timeout}" | awk '{printf "%.1f", $1/1000}')

	printf 'exp[f2] %-18s naive: intact=%s gone=%ss · governed: intact=%s gone=%ss released=%s\n' \
		stop "$n_ok" "$n_s" "$g_ok" "$g_s" "$released"
	record f2 stop naive "{\"ok\":$n_ok,\"gone_seconds\":$n_s}"
	record f2 stop governed "{\"ok\":$g_ok,\"gone_seconds\":$g_s,\"released\":$released}"

	if [ "$n_ok" != "1" ]; then
		say "FAIL: the baseline did not take its final save, so this run compares nothing (b3 has a handler)"
		return 1
	fi
	if [ "$g_ok" != "1" ]; then
		say "FAIL: the governed arm lost the final write"
		return 1
	fi
	# `may-exit` is the handshake's last word, and it is only written when the final write was asked
	# for *after* the job announced `ending`. A request served before the stop leaves the end block
	# with nothing to do, and the job simply leaves. So this is recorded, not asserted: the promise is
	# that the job leaves once its final write is on disk, and the exit time is what measures that.
	# The parity row, then the difference: same save, much less time held.
	if awk "BEGIN{exit !($n_s > $g_s)}"; then
		say "       same final save; the baseline was held ${n_s}s, we let go at ${g_s}s"
	else
		say "       note: the baseline left as fast as we did (${n_s}s vs ${g_s}s); the release bought nothing here"
	fi
}

case "${1:-}" in
prepare) prepare ;;
run)
	# Preparing loads two images into three nodes; a run that repeats it every time spends its whole
	# budget on setup. REBUILD=1 forces it after a code change.
	if [ ! -f "$KUBECONFIG_FILE" ] || [ "${REBUILD:-0}" = "1" ]; then
		say "== preparing"
		prepare
	fi
	say "== running $2"
	"run_$2"
	;;
status) $KUBECTL get pods -n ckpt-exp -o wide 2>/dev/null | head -10 ;;
down) kind delete cluster --name "$CLUSTER" ;;
*) sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
