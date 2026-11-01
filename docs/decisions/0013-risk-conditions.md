# 0013 — The risk conditions: a state machine of booleans

Status: accepted for the algorithm layer. Date: 2026-11-01. The names below are frozen: the
implementation layer builds adapters against them, so adding one is a row here plus a producer plus
a test, and changing the meaning of one is not allowed.

This supersedes the interval story (0007, 0008, 0010 §1's "period from L, τ and W") for the product
claim. The old machinery is not deleted yet; it is marked superseded and stops being the thing the
product sells.

## Context

A checkpoint exists because the work might disappear. So the algorithm's only question is:

> Is there an event after which the work would be lost, and is there still time to save it?

That question is a boolean, not a number. It does not depend on whether the job is alone or in a
fleet, on CPU or GPU, on-cloud or on-prem, on one cluster or many. Everything that differs between
those worlds is absorbed by the producer, which turns its own observations into `0`, `1`,
"not applicable" or "unknown". The algorithm never sees a machine, a scheduler, a network, or a
model.

The product is therefore trust, not performance: we act only on facts we can see, we never pretend
to predict, and we say when we are blind.

## The rule

1. **A trigger must still leave time to act.** A condition that means "it is already too late" is a
   diagnostic: it is shown to the user and never asks for a write. (`store.blocked` was a trigger in
   the first draft; it is a diagnostic now, because asking for a write we cannot perform teaches
   nobody anything.)
2. **Any urgent condition that is 1 → advise one write now.** `Reason` names the first such
   condition in catalogue order, so the answer is stable.
3. **Warn and note conditions never trigger a write.** They make training slower; they do not
   endanger the work.
4. **"Not applicable" is inert; "unknown" is not.** A condition the environment does not have is
   absent and changes nothing. A required condition we cannot read is itself urgent: blindness is a
   risk, not a zero.
5. **The algorithm has no clock.** Backoff, the window timer, retries, atomic sealing and
   read-back verification belong to the implementation. The algorithm is a stateless advisor; the
   executor holds the state.

## Erratum, 2026-11-01: one fact, one representation

Reviewing the catalogue for closure found three entries that duplicated something the algorithm
already reads as a typed field, and one entry that could never be reached:

- `store.corrupt` duplicated `Copies[].State == CopyBroken`. The typed field is canonical: a copy
  that fails verification is marked broken in `Copies`, not reported as a condition.
- `write.failed-twice` duplicated `ConsecutiveWriteFailures >= 2`. Same rule: the typed field wins.
- `watch.blind` duplicated the *unknown* state of each required condition, which the algorithm
  already treats as urgent. Blindness is a state of a condition, not a condition.
- `watch.broken` stays, but **moves to the front of the catalogue**: a dead probe is the most
  alarming fact there is, and placed after the conditions it can no longer report, it would be
  shadowed by whatever the snapshot happened to say.

The catalogue is therefore **14 conditions: 8 urgent, 6 show-only**, in this order:

```
watch.broken, risk.preempt, risk.shutdown, store.low-space, host.memory-pressure,
host.gpu-fault, job.ending, risk.eviction-likely, store.blocked, store.slow, host.gpu-hot,
host.cpu-throttled, job.step-slow, net.long-outage
```

The rows below are kept as the record of how the set was chosen; where they conflict with this
erratum, this erratum wins.


Every condition is a named boolean. "Missing" says what the absence of a reading means:
`inert` (not applicable, ignored), `urgent` (blindness is a trigger), `note` (recorded, no action).

| Condition | 1 means | Urgent? | Missing | Applies to |
|---|---|---|---|---|
| `risk.preempt` | higher-priority or more urgent work can take these resources | yes | urgent | any scheduler that can preempt |
| `risk.shutdown` | the job, container or node is being closed (graceful stop, drain, maintenance) | yes | urgent | anywhere |
| `store.low-space` | free space is below 2× one checkpoint, so a write may be the last that can complete | yes | urgent | anywhere |
| `store.corrupt` | the checkpoint we have does not verify | yes | urgent | anywhere |
| `host.memory-pressure` | memory is at the eviction line (≥95% of the limit; ≥90% is a warning) | yes | urgent | anywhere, CPU included |
| `host.gpu-fault` | a GPU is failing: Xid, ECC growth, fell off the bus | yes | urgent | GPU jobs only; absent on CPU |
| `watch.blind` | one required condition could not be read for N attempts | yes | — (it *is* the missing state) | anywhere |
| `watch.broken` | the probe itself is dead: cannot reach its source, clock jumped | yes | — | anywhere |
| `job.ending` | training is finishing or a stop was requested: the last write is the useful one | yes (user may disable) | inert | anywhere |
| `write.failed-twice` | two writes failed in a row: rewrite, then report | yes (repair) | inert | anywhere |

Diagnostics — never trigger a write, always visible:

| Condition | 1 means | Why not a trigger |
|---|---|---|
| `store.blocked` | the write target refuses right now: full, quota, IO error, permission, remote unreachable | a write now would fail; the user must fix the target |
| `store.slow` | writes are much slower than this deployment's normal | slower is not unsafe; the executor decides whether a write still fits |
| `host.gpu-hot` | the GPU is throttling | slower training, not a lost work |
| `host.cpu-throttled` | CPU quota is throttling the job | slower training, not a lost work |
| `job.step-slow` | step time is well above its normal | slower training, not a lost work |
| `net.long-outage` | the network has been down long enough to matter | it matters only if storage is remote, and then it is `store.blocked` |

Job-local facts (not environment, and they are what remains of the old contract): there is no
complete checkpoint yet (`checkpoint.none`, which makes the next write a full one), a write is in
flight (`write.inflight`, which means hold), and the copies on disk (`Copies`).

## Why the storage trigger is 200%

A write can only be kept if the previous one survives until the new one is sealed and verified: two
copies of one checkpoint must coexist. Below 2× the checkpoint size, the next write may be the last
that can complete, so the trigger fires while it still can — and the retention rule removes only
our own older copies, never anyone else's data.

`store.blocked` then means the window closed: we can no longer save, and the honest answer is to say
so rather than to loop.

## The producer's contract

For every condition in the catalogue, the producer reports one of three things:

- **absent** — this environment does not have it (CPU job and `host.gpu-fault`, on-prem and
  `risk.preempt`). Inert.
- **unknown** — it has it, but cannot read it right now. After N consecutive failed reads (N is the
  producer's own definition; 3 is the default), it becomes `watch.blind` for that condition.
- **0 or 1** — fresh, just read.

The producer owns every threshold and every raw signal (percentages, milliseconds, bytes, event
payloads). The algorithm owns none of them, which is what keeps it environment-blind.

## What dies

`Bounds.MaxTax`, `MaxLostWork`/`MaxLostSteps` as an optimisation target, `TightestBound`,
`Guarantee`, `OnConflict`, the pattern and its chain, `DeltaShare`, `FullWriteSeconds` as a planning
input, and eleven of the fifteen reason codes. What remains of a decision: which condition fired,
whether to write now, what it would contain, and how many copies to keep.

## Falsifiers

- A trigger that fires when it is already too late means the trigger is a diagnostic in disguise.
- Two environments needing different rules means the producer mapping is wrong, not the algorithm.
- A job with no signals behaving differently from the same job with all-zero signals means the
  absent/unknown distinction is broken.
- If the catalogue grows past a page, the abstraction is being bypassed: each new condition must
  have a one-line answer to "what work does its 1 lose, and is there still time to save it".

## Open, to be settled before the code

1. `job.ending`: on by default, user can disable — assumed yes.
2. Diagnostics are shown to the user — assumed yes.
3. `watch.blind` after 3 failed reads — assumed default, producer may override.
