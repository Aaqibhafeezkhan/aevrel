# Design

> **Superseded.** This page describes the design before the window and the two numbers existed: it
> still names `Fallback`/`FallbackEvery`, the deprecated interval fields, and an expiry computed from
> `2 × fallback`. Those parts are no longer true. The current design is
> `docs/design-backstop.md` (why the product has this shape) and `docs/design-v1.md` (how it is
> built); the contract is `docs/decisions/0017-two-numbers.md`. This page is kept because decisions
> 0013–0016 reference its vocabulary, and because the delivery plane descriptions are still useful.

Status: historical. The code is in `internal/policy` (the algorithm) and
`internal/exec`, `internal/loop`, `internal/produce`, `internal/queue`, `internal/supervisor`
(delivery).

## Scope

The controller owns the **write side** of checkpoints:

- when a checkpoint is written;
- how often it is written;
- how it is written (later: full or partial, how many copies, where);
- how long copies are kept;
- whether the checkpoint on disk is complete and readable.

It does **not** own:

- restarting a job, choosing a resume point, or admitting a pod;
- reacting to preemption (later the risk of preemption becomes one input to the write decision,
  never a separate feature);
- monitoring, metrics, or dashboards.

## The promise

The promise is a **risk model**, not an interval (decisions 0013, 0014):

| Question | Answer |
|---|---|
| when does the controller ask for a write? | when a named condition says the work is about to be lost, or when the user's own timer expires |
| what does the job do without a controller? | nothing, unless the user set a fallback timer: "not decided" is a refusal to start, not a default |

The conditions are booleans with a frozen catalogue (`docs/decisions/0013-risk-conditions.md`):
preemption, shutdown, low space, memory pressure, a GPU fault, the end of training, a dead
watchdog, and six informational ones. A condition this environment does not have is inert; one it
has and cannot read is urgent, because blindness is the state in which the work is most likely to be
lost.

Why the interval is gone: the user cannot observe the quantity it optimised (a failure rate), so an
interval computed from it is a promise nobody can check, and the whole exercise recovered only a few
percent. What a user buys here is **feasibility and advance
warning**: a write that fits inside the warning window, and a warning that arrives earlier than the
event. The warning must therefore be at least one write long — a 90-second warning with a
three-minute write is a write that never happens.

Write cost is measured, never assumed: the 90th percentile of the writes this deployment produced,
and a value without samples is unknown, never zero.

## Inputs

The contract is frozen for the first version, and its field-by-field authority — why each field
exists and what fills it — is `docs/decisions/0011-frozen-v1-contract.md`. It is defined in
`internal/policy/policy.go`; this page lists it once so that both layers can be built against the
same names.

From the user (all in `Bounds`):

| Field | Meaning | Required |
|---|---|---|
| `Fallback` | what the job does without a controller: `unset` (refuse to start), `none`, or `every` | yes |
| `FallbackEvery` | the period for `every` | with `every` |
| `MinCopies` | smallest number of copies; below 1 means 1 | yes |
| `Retention` | how long a copy is kept; zero keeps all | no |
| `StorageLimitBytes` | space the copies may use; zero means the volume's free space | no |
| `MaxLostWork`, `MaxLostSteps`, `MaxTax`, `Guarantee`, `OnConflict` | **deprecated**: still in the type until the contract removes them, read by nothing | no |

From observation (`Snapshot`), never from guesswork. A negative number means unknown:

| Field | What | How the implementation gets it |
|---|---|---|
| `UnitID`, `Generation` | opaque unit identity and restart counter | the adapter |
| `Now` | the moment of the snapshot | the clock, passed in |
| `Step`, `StepTimeSamples` | progress and recent seconds per step | step counter and wall time |
| `RemainingSteps`, `Remaining` | work left, in steps and in time | job config, `ExpectedEnd`, or step time |
| `LastSeal` | when the newest complete checkpoint was sealed | directory and seal time |
| `SaveInFlight`, `SaveStartedAt`, `SaveProgress` | the running write and its progress | the writer or the directory |
| `WriteCostSamples` | recent seconds per completed write | first sighting to seal |
| `CheckpointBytes` | size of one checkpoint | manifest or directory size |
| `FreeBytes`, `VolumeBytes`, `WriteThroughputBytesPerSecond` | storage state | file system statistics, or -1 |
| `Copies` | every copy on disk: step, bytes, state, creation time | directory scan |
| `ConsecutiveWriteFailures` | failures since the last success | the write result |
| `WriteSecondsTotal`, `ElapsedSecondsTotal` | time spent writing, elapsed time | the adapter |
| `Previous` | the last decision | the state file |
| `Evidence` | optional named observations (`Observation`) | any adapter; a missing key is unknown |

Rules that keep measured numbers honest: the two sample lists are rolling; the write cost uses the
90th percentile, not the median; and a value without samples is unknown, never zero.

## Output

The algorithm returns a `Decision` (fields in `internal/policy/policy.go`):

| Field | Meaning |
|---|---|
| `UnitID`, `Generation` | copied from the snapshot; a decision is valid only for that unit and generation |
| `Action` | `save`, `repair`, `skip`, `hold`, `infeasible`, `unknown` |
| `Reason` | machine-readable cause: `risk`, `calm`, `repair`, `first-checkpoint`, `schedule` (the user's own timer) |
| `SaveNow` | true for `save` and `repair` |
| `CopiesToWrite` | how many copies the next write should produce |
| `Kind`, `WeightsOnly` | what the next write should contain: full or delta, and weights only when the user's resume requirement allows it |
| `KeepCopies`, `OldestStepToKeep` | retention result; delete sealed copies older than this step, but never drop below `KeepCopies` |
| `Missing` | which measurements were unknown and changed the decision |
| `Reasons`, `Sensitivity` | structured explanation: the cause chain and what threshold would change the answer |
| `Why`, `Version` | one sentence for a human; the policy that produced it |
| `Interval`, `TriggerAt`, `Tax`, `FallbackInterval`, `NextCheck`, `TightestBound` | **deprecated**: published empty and read by nobody; they belong to the interval algorithm 0015 deleted |

One small file per job, inside the checkpoint directory:

```
<checkpointDir>/.ckpt-governor/state.json
```

```json
{
  "schema": 1,
  "epoch": 7,
  "expiresAt": 1730000000,
  "unitId": "train-1",
  "generation": 7,
  "action": "save",
  "reason": "risk",
  "fallbackIntervalSeconds": 0,
  "copiesToWrite": 2,
  "kind": "",
  "weightsOnly": false,
  "shards": 0,
  "async": false,
  "buffers": 0,
  "keepCopies": 2,
  "oldestStepToKeep": 12000,
  "why": "store.low-space is 1: free space is below 2x one checkpoint",
  "version": "risk-v1",
  "missing": []
}
```

The file is written whole and then renamed into place, so a reader never sees half a file. The
training job reads it at its normal save point and acts on it. `expiresAt` is an expiry stamp: when
it passes, the job must fall back.

## Failure behaviour

| Event | Behaviour |
|---|---|
| controller stops | the job does what the user decided: nothing (`none`), or its own timer (`every`); `unset` means the controller refuses to start |
| controller restarts | `epoch` increases; a plan published but never recorded is adopted and its attempt rebuilt; a generation change drops the dead generation's attempts |
| job ignores the file | the attempt misses its deadline (ten minutes when no write cost has been measured) and is recorded as `failed` |
| a write dies half way | a partial directory past its deadline is recorded as `abandoned` |
| a write seals and the content does not verify | the copy is `broken`; the next decision is a repair |
| the gate says no | the refusal is recorded in the history, and the job is told "not now" instead of a write it would have to ignore |
| free space is below two checkpoints | the gate refuses with `headroom`; copies are retired first, and the last copy is never retired |
| a condition cannot be read | three consecutive failed reads make it blind, and blindness is urgent for every condition that is required |
| the probes are all blind, or a probe reports itself dead | `watch.broken` is 1, and the policy treats that as urgent |

## Boundaries with other tools

| Question | Who answers it |
|---|---|
| which job runs, which is preempted | the scheduler (for example Kueue or Volcano) |
| which checkpoint a job resumes from | the platform, using the state published by this controller |
| was the job healthy | monitoring, not this controller |
| when to write, how often, how, how many copies | **this controller** |

## The line between the algorithm and the implementation

The algorithm is one function behind one interface:

```go
type Policy interface {
    Name() string
    Decide(Bounds, Snapshot) Decision
}
```

Rules that keep the two layers apart:

1. `internal/policy` imports only the standard library. A test fails when that changes.
2. Every input arrives in `Snapshot` and every output leaves in `Decision`. The algorithm reads no
   clock, disk, network, or cluster. `Now` is a field, not a call.
3. The implementation layer owns collection and delivery: directories, storage, pods, job groups,
   the state file, the fallback. It may change freely as long as `Snapshot` and `Decision` stay
   complete.
4. The job shape never reaches the algorithm. The implementation decides what one write unit is
   (one job, a gang, a batch) and puts only an opaque `UnitID` in the snapshot. The algorithm does
   not know what a pod or a gang is.
5. A decision is machine-readable. `Why` is for a human, and no code parses it.
6. The algorithm can be replaced by a stub. `policy.Risk` is the only shipped one; the loop tests
   use their own stub, and policies are selected by name, so the implementation can be built and
   tested before a different algorithm exists.
7. Purity is what makes replay possible: the same snapshot in, the same decision out. A
   determinism test holds this in place.
8. A change to `Bounds`, `Snapshot`, or `Decision` needs a decision record, because both layers
   depend on it.

Why this prevents collisions: each layer touches different files. A different grouping (gang,
batch, single pod) is a change in the adapter; different arithmetic is a change in one package.
Neither forces the other to move.

## Implementation design

The algorithm is one package. Everything else is delivery, and it is split by responsibility:

| Package | Owns | Never does |
|---|---|---|
| `internal/unit` | which directory is one unit, its members, its generation, the per-volume write gate | measure, decide |
| `internal/observe` | reading the directory and the file system into a `Snapshot` | decide, write |
| `internal/advise` | publishing a `Decision` as the state file, and the private ledger | measure, decide |
| `internal/exec` | attempts, the four gates, the fallback timer, the attempt history | decide, measure the world |
| `internal/produce` | the probe contract, the sampler's failure budget, `watch.broken` | decide |
| `internal/loop` | one round: scan → sample → settle → decide → gate → publish → record | block on a write |
| `internal/queue` | the dirty set, per-key backoff, and the resync floor | reconcile a unit |
| `internal/supervisor` | one loop per unit, the queue and the floor | own a unit's state |
| `cmd/ckptctl` | the offline commands (a trace in, a table out; `status`, `doctor`) | talk to a cluster |

`observe` and `advise` import `policy` for its types only. `unit` imports both. Nothing imports
`unit` except the controller. The existing import test keeps `policy` free of all of them.

### Two files, one boundary

| File | Belongs to | Read by | May change |
|---|---|---|---|
| `<dir>/.ckpt-governor/state.json` | the contract | the training job, the platform | only with a decision record |
| `<dir>/.ckpt-governor/internal.json` | the implementation | nobody else | freely |

The private ledger exists because the implementation needs memory across restarts: the accumulated
write seconds, when each directory was first seen, the last published bytes, the failure counter,
the boot id. Putting that into the published file would break its frozen schema; keeping it only in
memory would make a restart produce different decisions from the same trace.

### Five planes

The delivery layer is five planes with one direction of data (decision 0016):

| Plane | Owns | Never does |
|---|---|---|
| producer | one adapter per condition: its thresholds, its "unreadable after N", its cadence and cost | decide anything |
| advisor | the pure function `(Bounds, Snapshot) → Decision` | read a clock, a file, or a device |
| executor | attempts, cooldowns, backoff, the fallback timer, retention, atomic seal, read-back | interpret the environment |
| observation | the status view and the event log | change state |
| persistence | the ledger, the state file, the lease | hold state the disk does not have |

**An attempt is the unit of accounting.** One write request produces one `Attempt` with an identity
and an immutable outcome; every counter, cooldown and retry is expressed in attempts, not in
directories. A write that outlives a round is normal: the round starts the attempt and returns, and
later rounds settle it. This is what lets a preemption that arrives mid-write still be seen — the
sampler never stops.

The sample cadence is a latency choice, not a cost choice: a round is a handful of syscalls. It has
to be shorter than the time between "a condition became true" and "the job reads the file".

Durations are measured on **one clock**: the controller's. The first sighting of a directory is
recorded in the ledger; a write's cost is `seal observed at − first seen at`. The directory's mtime
is never used, because it comes from the trainer's clock, which may differ.

### Unit identity

A unit is the set of processes that seal one checkpoint directory together. The **directory** is
the identity; a pod is only a member.

- one pod, one directory: a unit of one member;
- a gang: every rank writes the same directory, so the gang is the unit;
- a batch: every job has its own directory, so every job is a unit.

Per-pod units would produce one decision per rank: several triggers, simultaneous writes on one
volume, and retention rules fighting each other. That multiplies the write tax by the number of
ranks, which is the opposite of the promise.

**Generation** increases when the writing set changes: members replaced, world size changed, or the
trainer reports a new boot id. A decision from an older generation must not be applied.

**Volume gate.** Several units may share one volume. The algorithm cannot see the others, so the
implementation limits concurrent writes per volume (one by default). This is a resource limit in the
executor, not a second decision: the algorithm still says when. It does not stagger starts — jitter
belongs to the policy (decision 0003), and a gate that also scheduled would be a second scheduler.

**Misconfiguration is reported, not guessed.** A directory written by pods of two different
workloads, or one workload writing several directories, is stated in the log and in `Why`'s
source sentence; the controller does not pick a "main" directory.

### Measurement

Each field has one source, one cost class, and one unknown value. The classes are O(1) syscalls,
O(copies) stats, and O(bytes) reads; the third one is never on the hot path.

| Field | Source | Class | When | Unknown |
|---|---|---|---|---|
| `UnitID`, `Generation` | directory, members, boot id | O(1) | on change | `0` before the first member |
| `Now` | the clock, once per round | — | every round | — |
| `Step` | the trainer's `step` file, else the newest sealed copy | O(1) | every round | never: the sealed copy is a floor |
| `StepTimeSamples` | consecutive live step observations | memory | on advance | empty |
| `RemainingSteps`, `Remaining` | job configuration, expected end, step time | memory | every round | `-1` |
| `LastSeal` | the moment the seal was observed | O(1) | on seal | zero |
| `SaveInFlight`, `SaveStartedAt` | a directory without a seal | O(copies) | every round | `false`, zero |
| `SaveProgress` | the trainer's progress file | O(1) | every round | `-1` |
| `WriteCostSamples` | bracketed by the last sighting without a manifest and the first with one; the upper bound is used, rolling 16 | memory | on seal | empty |
| `CheckpointBytes` | the manifest's shard sum, else the newest sealed copy | O(1) | on seal | `0` (see decision 0002) |
| `FreeBytes`, `VolumeBytes` | `statfs` | O(1) | every round | `-1` |
| `WriteThroughputBytesPerSecond` | bytes over the last write's cost | memory | on seal | `-1` |
| `Copies` | directory scan; `CreatedAt` is the first sighting | O(copies) | every round | empty |
| `ConsecutiveWriteFailures` | started and not sealed within `max(3·W, 60s)` | memory | every round | `0` (a real value) |
| `WriteSecondsTotal`, `ElapsedSecondsTotal` | the ledger, the unit start | memory | on seal, every round | `-1` |
| `Previous` | the published file, parsed | O(1) | at startup | zero |
| `Evidence` | optional: `step.source`, `verify.last` | memory | as needed | a missing key |

`sealed` means the structure is right: every shard listed in the manifest is present and has the
recorded size. `verified` means the content was read back, and that happens in a **separate,
sampled loop** that runs only when no write is in flight — reading a checkpoint costs more than
writing one, so it cannot be part of the hot path.

### Actuation

The only actuator is the file. Publishing is:

1. serialize the decision; if the bytes equal the last published, do nothing (a rewrite changes the
   mtime, wakes the job and makes the file look new);
2. write a temporary name, `fsync`, `rename`, `fsync` the directory;
3. `expiresAt = now + max(2 × fallback, 60s)`, renewed at `expiresAt/3`.

After a `save` decision, if no seal is observed within three intervals, the advice is recorded as
ignored, repeated no more often than once per cooldown, and stated in the log. An advice nobody
follows is a fact about the job, and the controller stops pretending otherwise.

### The layout it reads

One checkpoint is a directory; the manifest is written last and is the seal:

```
<checkpointDir>/
  step-12000/
    manifest.json        {"step": 12000, "shards": [{"rank": 0, "bytes": 900000000, "sha256": "..."}]}
    rank-0.bin
    rank-1.bin
  .ckpt-governor/
    state.json           published (the contract)
    internal.json        the private ledger
    step                 written by the job: the step it has reached
    boot                 written by the job: a new id on every process start
    progress             written by the job: [0,1] during a write
```

A directory is `sealed` when its manifest lists every shard, each shard's size matches, **and no
shard is newer than the manifest**. A rewrite of an existing step removes the manifest before
touching a shard; a directory whose shard changed after the seal is `broken` (`seal_stale`), because
the sizes can still match while the bytes belong to two different attempts. It is `broken` when a
listed shard is missing or short. It is `unverified` when there is no manifest; if
it was first seen recently it is also the write in flight, and if it stays that way beyond
`max(3·W, 60s)` it counts as a failed write.

### The write lifecycle

The algorithm knows four facts about a write: in flight, cost samples, the newest seal, and a
failure count. A person needs the states in between, and the controller needs them to decide
whether a write is late, slow, dead, or ignored. These states are the implementation's, not the
contract's:

| State | Means | Entered when | Left when |
|---|---|---|---|
| `idle` | the newest seal is younger than the trigger | a seal is observed | the trigger is reached |
| `due` | the plan says save; the file carries it | a `save` decision | a step directory appears |
| `writing` | a step directory without a manifest | the directory appears | the manifest appears, or quiescence |
| `sealed` | the manifest and every shard agree | the manifest is seen | verification, if any |
| `verified` | the content was read back and matches | the verifier finishes | a mismatch makes it `broken` |
| `broken` | a shard is missing or short, or the write died | a check fails | a repair seals, or a person deletes it |
| `stalled` | the plan was published and no seal followed within three intervals | the timer expires | a seal appears |
| `blocked` | two writes failed in a row | the failure counter reaches two | a seal, or the world changes |

`due` and `writing` are kept apart on purpose: the gap between them is the job's read latency, and
it is the only measurement of whether the job is listening. It is shown, not acted on.

### When a write fails

A failure is detected in three ways, and all three end in the same state (`broken`):

1. a step directory with no manifest that has not **changed** for `max(3·W, 60s)` — it is dead, not
   slow (decision 0004);
2. a manifest whose shards are missing or short;
3. a content mismatch found by the verifier.

The reaction is immediate, not scheduled: the newest copy is broken, the policy answers `repair`,
and the next write is requested at the save point right after — a failure must never wait for the
next trigger, because the age keeps growing while it waits.

After **two** consecutive failures the policy answers `infeasible/repair`. The implementation then
stops asking for writes, keeps the broken directories as evidence, records one structured failure,
and reports `blocked`. It resumes on its own when a seal appears or when the free space rises above
what a write needs; it does not retry on a timer, because every retry costs `W` and produces
nothing, and the tax bound would be spent on failures.

### Deleting copies

Deletion is the only destructive thing this controller does, so it has four rules:

1. only copies whose state is `sealed` or `broken` are eligible — never the in-flight directory;
2. a copy is deleted only when its step is below `OldestStepToKeep` **and** it is beyond the newest
   `KeepCopies`;
3. the free space is measured again after deleting; if it is still short, the answer is `capacity`
   with the arithmetic, not another deletion;
4. every deletion is logged with the step and the bytes freed, and the ledger records the newest
   remaining step, so a restart does not delete twice.

### The human view

The published file is for the job. A person needs a different question answered: *what is happening
now, and what happened last*. That lives in the ledger and is rendered by `ckptctl status <dir>`:

```
unit train/llama   generation 3   phase writing
newest sealed   step 12000 at 2026-01-01T09:50:00Z (age 3m10s, bound 10m)
writing         step 12200, 45%, started 09:52:00Z, 2m10s elapsed, 3m expected
advice          save/schedule published 09:52:00Z, seen in the directory 09:52:30Z
last writes     step 12000 sealed 30s 900MB | 11800 broken: shard 1 short | 11600 sealed 28s 900MB
failures        1: shard 1 short at 09:40:12Z
```

The rules for that view are the same as for `Why`: absolute times and measured numbers are stored;
anything relative ("2m10s elapsed") is computed when it is rendered, so a stored line never ages.

Failure codes are structured, not sentences: `write_abandoned`, `manifest_invalid`,
`shard_missing`, `shard_short`, `volume_unreadable`, `advice_ignored`, `capacity_short`,
`clock_jump`. They go into the ledger, the log, and the status view; the job never sees them.

### What the training job writes

Three small files, all optional, each replaced by writing a temporary name and renaming:

| File | Content | Written when | Without it |
|---|---|---|---|
| `step` | one integer, the step reached | at each save point, or every N steps | `Step` is the newest sealed copy's step; a step-based bound degrades to unknown |
| `boot` | one opaque string, new on every process start | at startup | generation falls back to an observed step regression |
| `progress` | one number in [0,1], the running write's progress | during a write | `SaveProgress` is `-1` |

This is the whole job-side contract: one file to read (the state file), three to write, no library
and no network. A job that writes none of them still works; it just gets the coarser answers the
table names.

**A plan is served once.** The job remembers the plan it acted on — its `triggerAt`, or the file's
bytes when that field is empty — and does not start another write for the same plan. A plan says
"save now", not "save at every step until I change my mind": without this rule one advice produced
ten consecutive checkpoints in a real run, and every one of them was a write the controller never
asked for.

### Degradation

| Event | The decision says | The implementation does | The user sees |
|---|---|---|---|
| directory unreadable | unknown, missing "copies" | publishes the fallback only | one warning, the file still expires safely |
| clock jumps backwards | the age is not comparable | keeps the last decision, records the jump | `why` names the time |
| disk full | `capacity` | removes what retention allows, then reports | the arithmetic in `why` |
| a write goes quiet for `max(3·W, 60s)` | `repair` (the newest copy is broken) | marks it `write_abandoned`, rewrites at the next save point | the state view shows the failure |
| a shard is short when the manifest appears | `repair` | keeps the broken directory as evidence | the state view names the shard |
| two writes fail in a row | `infeasible/repair` | stops asking, reports `blocked`, resumes on a seal or more space | a person is asked for, with the code |
| the job ignores the file for three intervals | the same advice | marks it `advice_ignored`, lets the file expire | one line in the log, `stalled` in the view |
| the controller dies | — | — | the file expires; the job's timer holds `L` |
| the controller dies between publishing and recording | — | on start, the file's epoch and advice are adopted (`reconciled`) | one counter in the ledger; nothing re-advised |

### How the implementation is tested

- `observe`: fixture directories in a temporary path, a fixed `Now`. One case per unknown value.
- `advise`: golden `state.json` bytes; publishing twice must be a no-op; expiry with a fake clock.
- `unit`: fake member events; generation transitions; two units on one volume under the gate.
- `ckptctl simulate`: a trace in, the same decisions out twice.
- No cluster in unit tests. The cluster run produces a trace, and the trace is what gets replayed.

### Numbers this layer must print

Each of these is a claim that needs a number before it is written into a document:

1. the cost of one sample round on a directory with 10 copies (target: below 1ms);
2. the same trace replayed twice produces identical decisions (the test asserts it);
3. publishing twice writes once (the test asserts it);
4. a condition that fires is followed by a write inside the warning window the producer promised.

## Extending the algorithm: conditions, not factors

The algorithm is one pure function over the condition catalogue
(`docs/decisions/0013-risk-conditions.md`) plus the typed facts it owns (`Copies`,
`ConsecutiveWriteFailures`). Adding a signal means adding a catalogue row, a producer and a test in
the same commit — not a layer, a weight or a scale.

The seam that makes this work is evidence: a named bag of observations, where a missing key is
"not measured", never zero. A producer can add a condition without changing any struct the policy
exposes.

```go
type Observation struct {
    Kind   Kind      // number | bool | text | unknown
    Number float64
    Bool   bool
    Text   string
    At     time.Time
    Source string    // which producer gave it
}

type Snapshot struct {
    // ... the typed measurements described above ...
    Evidence map[string]Observation // optional; a missing key is unknown, never zero
}
```

The factor layers this page used to describe (`internal/policy/factor.go`, `pattern.go`) were
deleted with the interval algorithm (0015). `docs/decisions/0014-why-the-risk-pivot.md` says why:
the quantity they optimised is one the user cannot observe, and a scale cannot express a condition.

### Why not a weighted sum of conditions

A weighted sum looks flexible and is untestable: the result would depend on weights nobody can
justify, a condition that fires would compensate for one that is calm, and the person reading the
decision could not say what would change it. Every condition is a boolean with a tier and a
precedence, and the decision names the first one that fired.

### Stub and replacement

`policy.Risk` is the only shipped policy. A test stub is a different name in the registry; the state
file records the version that produced each decision, so a trace can be replayed against the same
version.

## Settled for the first version

| Question | Answer |
|---|---|
| what the user must decide | the fallback: `none`, or a period. `unset` is a refusal to start, not a default |
| when a write happens | when a catalogue condition says the work is at risk, or when that timer expires |
| may the job skip a save? | the controller only adds writes |
| module path | `checkpoint-governor` |
| license | Apache-2.0 |
| owner | the project owner; the repository is local for now |
| what one unit is | the checkpoint directory (decision 0001) |
| when a generation changes | members replaced, world size changed, or a new boot id |
| published file vs private ledger | two files: `state.json` is the contract, `internal.json` is ours; the attempts live beside them in `attempts.json` |
| structure check vs content check | structure on the hot path; content read back in a sampled background loop |
| who adds jitter | the policy, never the publisher (decision 0003) |
| unknown values without a sentinel | decision 0002 |
| how often a unit is looked at | on an event, when it has work of its own (a write in flight, a timer, the policy's next check), or at the ten-minute resync floor |

## Still open

- where recorded traces live, and who may publish them;
- the shape of the state file reader that the training job links in;
- whether the first version ships one or two copies by default (`MinCopies` is required, so the
  default is a documentation choice, not a code one).
