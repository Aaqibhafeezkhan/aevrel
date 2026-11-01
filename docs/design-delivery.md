# The delivery layer, in detail

> **Status: superseded in part (2026-11-01).** §5 (the exit protocol) and §6 (the gate) are rewritten
> by `docs/design-v1.md` §4 and §7 and by `docs/decisions/0017-two-numbers.md`: `may-exit` is now
> written by the **controller** (it is the side that can see whether a write is in flight), a final
> write is exempt from the cooldown and the backoff, and a final write that cannot be made is a named
> failed final write printed as the last line of the run. §7 (units, queue, supervisor) still holds. The
> rest of the page is history: it explains how the delivery layer was built, not how it behaves now.

Status: design, round 2. Date: 2026-11-01. Round 1 made the
planes real: attempts, the gate, the window, the sampler, the queue, the supervisor, the report.
This page says what is still coarse, and what "done properly" means for each piece. `0016` is the
shape; this is the substance.

## 0. Why a simple kernel can carry a complicated world

The kernel is the **vocabulary**, not the logic: 13 booleans, 6 actions, 5 verdicts, 6 phases, one
gate, one attempt. Everything else is a producer or a fact. Top controllers stay maintainable by
three habits, and we copy all three:

1. **One operator per fact, with a card.** kubelet gives each condition one manager and one
   `SetCondition` path; node-problem-detector lets an external observer collapse messy kernel or
   cloud signals into *one named condition*. The card ("what is 1, from which file, at which
   cadence, what does absent mean") is what makes "how is this 0/1" answerable by anyone.
2. **One place owns all waiting.** The kubelet eviction manager owns ordered actions and the grace
   period; the probes only say 0/1. Nothing else may invent a second retry policy.
3. **Level-triggered reconcile with a queue and a resync floor.** controller-manager never reasons
   about event order; a missing event costs one resync interval, not correctness.

The rule that follows: complexity is allowed **inside** a producer and **inside** the gate. It is
forbidden **between** them: no probe reads another probe, no probe knows the policy, the policy
knows nothing, and no counter exists outside the attempt history.

## 1. Inventory: real, declared absent, and derived

Closed at 14/14 (two tests read `docs/conditions.md` from disk: a catalogue row without a card, or a
card without a producer and a written reason, fails the build).

| Condition | State | Reads | Threshold |
|---|---|---|---|
| `watch.broken` | real (sampler) | the sampler's own budget | 3 consecutive unreadable |
| `store.low-space` | real | `statfs` of the checkpoint volume | free < factor × one checkpoint (2 by default) |
| `store.blocked` | real | create+remove a temp file in `.ckpt-governor` | error non-ENOENT |
| `host.memory-pressure` | real | cgroup v2 `memory.current` / `memory.max` | used/limit ≥ 0.95 |
| `host.cpu-throttled` | real | cgroup `cpu.stat` throttled/usage | ratio ≥ 0.2 |
| `job.ending` | real | presence of `.ckpt-governor/ending` | present |
| `risk.shutdown` | real | presence of `.ckpt-governor/stopping`, written by the job on SIGTERM | present |
| `store.slow` | real | `write-baseline`/`write-rate`, supplied by the job | ratio ≥ factor (`conditions.json`, 2 by default) |
| `job.step-slow` | real | `step-baseline`/`step-rate`, supplied by the job | ratio ≥ factor |
| `risk.eviction-likely` | **derived** | node allocatable vs requests, pending high-priority pods, cordon — composed into one 0/1 | any signal, `unknown` without access, off unless enabled |
| `risk.preempt` | declared absent | — (no scheduler in a single-node deployment); the announcement path is the glue's `seconds-left` | — |
| `host.gpu-fault` | declared absent | — | — |
| `host.gpu-hot` | declared absent | — (`.ckpt-governor/gpu`, if the job's own probe writes it) | — |
| `net.long-outage` | declared absent | — | — |

The finding that shaped round 2: a catalogue row with no producer is a promise that silently never
fires, and no test notices it. So the completeness test fails on a row that is neither real nor
*declared* absent with a reason, and `risk.eviction-likely` is the one row invented on this side —
outside the job — under four guardrails: its own condition name, a reason that names the signals it
saw, `unknown` when it has no permission to look, and a switch that turns it off (off by default
where looking is unsafe).

Still-placeholder facts in `Snapshot`: `WriteThroughputBytesPerSecond` (-1),
`WeightsOnlyBytes` (-1), `RemainingSteps`/`Remaining`, `SaveProgress` (only when the job writes
`progress`), and the deprecated interval fields (published empty on purpose).

## 2. The condition card

Every condition answers these ten questions, in one row of `docs/conditions.md` (written by the condition cards,
enforced by a test):

| Field | Example |
|---|---|
`name` | `store.low-space`
`what 1 means` | one sentence, no numbers ("the next write may be the last that can complete")
`source` | the exact file, syscall or API
`definition` | the threshold and the comparison, with its default
`cadence` | how often it may be read
`cost` | cheap (one syscall) or heavy (reads content / walks a tree)
`absent means` | why this environment does not have it
`unknown means` | what blocks the read, and why that is urgent or inert
`consumer` | the catalogue row (tier, missing policy) it feeds
`test` | the truth-table test that pins 0/1/absent/unknown

Rules: one producer per row; a producer never reads another condition; a threshold is deployment
config, never a runtime knob; a producer that cannot see is `unknown`, never `0`.

## 3. The write watch

The controller knows a write through **four facts only**: a directory appeared, its manifest
appeared and matches its shards, its size, and how long it has been quiet. Everything else
(progress, throughput, ETA) is a courtesy, not evidence.

- **Read-back is rationed and never competes.** Content verification runs only when nothing is being
  written, with a byte budget (2 GiB/h) and a cadence (10 min), and the deferral is *visible*: "we
  have not looked" must never look like "nothing is broken" (see the three verify facts).
- **Headroom is a space rule, not a constant.** The new copies and the copies we must keep coexist:
  `need = (copies + retained) × one checkpoint`, where `copies` is what the decision says to write
  (one today) and `retained` is what retention keeps (capped by `KeepCopies`). With no checkpoint yet
  `retained = 0`, so the first write needs **1×**, not 2×. When the size (or the free space) is
  unknown the gate says `headroom unknown` out loud instead of passing silently (the headroom-unknown case).
- **A write's duration is only the duration of a finished write.** `sealed` and `broken` carry a
  cost; `failed`, `abandoned` and `refused` carry none (the full-disk case depends on this).

## 4. Estimates, for the cold start

Before the first seal we have no size and no duration. The order of authority:

1. **The job declares** (`expectedBytes`, `lastSaveSeconds`) — it knows its own parameter count and
   its own history, and it costs us nothing.
2. **A planned manifest** — a writer may announce the shard sizes before writing them.
3. **One bounded throughput probe** — write 32 MiB into `.ckpt-governor`, fsync, remove, measure;
   once per start, counted in overhead.
4. **Measured** — after two seals the p90 is the number, and the estimate is retired.

Discipline: **estimates never enter `measured`**. They live in separate fields and are printed as
`(estimated)`; p90 is only fed by seals.

## 5. The exit protocol: three owners of the deadline

| Situation | Who owns the deadline | What the executor does |
|---|---|---|
| Platform announcement (preemption, drain, SIGTERM from the platform) | the platform, in seconds | **fit test**: T vs W. Fits → save now. Does not fit → refuse (`no-time`) and report the loss. |
| Inferred risk (`risk.eviction-likely`) | nobody | treat as a present risk: a normal write, explainable, switchable off. A false positive costs one write. |
| User stop | **us** | **two-phase, both written by the job**: `stopping` when it is told, `may-exit` when its final write landed. The controller only *reports* the phase and the work at risk; it never writes `may-exit`, because only the job knows that its own write is on disk, and a permission the job does not wait for is a file nobody reads. |

**A refusal is not a debt.** A near miss does not raise the probability of the next one, so writing
later "because we owed it" is tax without a reason. The refusal stays as *history for a human*, and
the protection against near misses is the user's own timer, not a hidden obligation.

## 6. The gate: one place for every wait

Doors, in order: `not-a-save`, `in-flight`, `already-saved`, `no-time`, `headroom`, `duplicate`,
`cooldown`, `backoff`; `volume-busy` is held by the executor outside the gate (a volume slot is not
a second decision). `already-saved` comes before `no-time` on purpose: work that is already on disk
needs no time to write. Rules that hold for all of them: a refusal is not a write (it resets no
clock, grows no backoff, and may be asked again); a hold is not a refusal; only `sealed` and `broken`
have a duration; a duplicate is only a duplicate for a request that was actually served.

## 7. Units, queue, supervisor

Built: coalescing, per-key backoff, bounded queue, resync floor, one loop per unit, generation
adoption, per-volume write gate, and `ckptctl supervise` (see the supervisor) with one roll-up line over N units.
Missing: fairness between units on one volume (backlog — it needs a second unit busy at the same
moment to be observed at all).

## 8. Retention and deletion safety

Three rules, all tested: **the last copy is never deleted**; keep `KeepCopies` and free
`(1 + retained) × size`; removal is two-phase (rename into `.trash`, unlink later) and bounded per
round. A copy is never retired while a write into the same step is in flight (the retirement rule).

## 9. Lease, epochs and crashes

Already exact: publish-then-crash and record-then-crash reconcile on start; a generation change
drops the dead generation's attempts; the attempt identity makes double counting impossible. Also
built (lease takeover): **lease takeover mid-write** — the holder that loses its lease goes read-only, gives
its volume slot back, and the new holder adopts the running attempt (the ledger, not the process, is
the truth).

## 10. What may be configured

Only what changes the promise the user made: `baseline` and `window`. Thresholds are deployment
config with correct defaults and one recorded precedence — flag over file over default — plus an
event on every override (the override event), so a number can never change without a record of who changed it.
There is no runtime knob that
changes a decision mid-run, because a decision that depends on a knob is a decision nobody can
reproduce.

## 11. What the controller costs, asserted

Overhead is printed and **tested**: a round on a directory with ten copies stays under a target
(1 ms), each probe is read at most once per cadence, publishing happens at most once per round
(the round-overhead case). An unmeasured cost is a swamp with no map.

## 12. Invariants both sessions must keep

1. Exactly once: one request, one attempt, one outcome; a restart changes nothing.
2. The last copy is never deleted.
3. `unknown` is never rendered as `0`; `absent` is never rendered as `unknown`.
4. A round never blocks on a write, and a probe never blocks a round.
5. One fact has one owner and one representation.
6. The algorithm reads no clock, no file, no device.
7. A refusal resets no clock and may be asked again.
8. A verdict's wait is not a write's duration.

## 13. Where each part lives

`docs/design-v1.md` §12 covers the executor's arithmetic and safety — headroom, `no-time`,
estimates, stop protocol, retention, lease, verification visibility, cost budget, invariants,
crash points. This page covers the delivery side: producers, glue, CLI, docs, demo — everything a
user can see, and nothing inside `exec`, `loop` or the sampler core.
