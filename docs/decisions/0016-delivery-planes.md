# 0016 — The delivery side is five planes, and an attempt is the unit

Status: accepted for the implementation layer. Date: 2026-11-01. `0013` is the condition catalogue,
`0015` is the risk core, `design.md` §Implementation design is the description. This record says why
the delivery layer is shaped the way it is, and which decisions here are load-bearing.

## Context

Under the risk pivot the algorithm is a table of named booleans plus a pure function. Everything
that can fail, wait, retry, or be observed now lives on this side — the same distribution every top
controller has (kubelet's eviction manager, controller-manager's queue and reconcile loop, Borg's
allocs and attempts).

The shape we had was built for a different question: one synchronous round that samples, decides,
publishes and records. Three things in it are wrong for risk conditions, and each one is expensive to
fix later:

1. **A synchronous round cannot see during a write.** A write lasts minutes; a preemption can arrive
   in the middle of it. If the round blocks on the write, the controller is blind exactly when it
   matters.
2. **There is no object for "the write we asked for".** Without it, "did it happen, how long did it
   take, did it fail" is inferred from directory facts, and every counter (backoff, cooldown,
   `write.failed-twice`, `store.slow`) becomes a guess.
3. **There is no place for a second unit.** One process governs one directory. Multi-card arrives as
   a unit with more members, and multi-job as more units: without a per-unit boundary the loop has
   nowhere to grow.

## Decision

**Five planes, one direction of data.**
`producer → Evidence → advisor → Decision → executor → (state file, ledger, events) → view`.
No plane reads another's internals; only `Evidence`, `Decision` and the files cross.

**Three rules that are not negotiable.**

- **R1 Time belongs to the executor.** Ticks, cooldowns, backoff, deadlines, the window and
  staleness live in one place. The advisor receives `Now` as data. A clock inside the advisor is a
  defect, not a style choice.
- **R2 An attempt is the unit of accounting.** One write request produces one `Attempt`, which has an
  identity, a trigger, a start, a deadline and an immutable outcome (`sealed`, `broken`,
  `abandoned`, `failed`). Every counter and every gate is expressed in attempts, not in steps.
- **R3 Execution is asynchronous.** A round starts an attempt and returns; later rounds settle it.
  Sampling never stops during a write.

**Closed loop per field.** Every contract field has a producer and a consumer, and the consumer is
declared: this side, the job, or the human view. A field that only one of us expects is a field that
will silently rot.

| Consumer | Fields |
|---|---|
| the executor (this side) | `Action`, `Reasons`, `CopiesToWrite` (verified and retained, not written), `KeepCopies`, `OldestStepToKeep`, `Missing` |
| the job (through the state file) | `Kind`, `WeightsOnly`, `Shards`, `CopiesToWrite` (it writes them) |
| a human (status, events) | `Why`, `Sensitivity`, the diagnostics |

## Consequences

- `internal/produce` replaces `internal/capabilities`: one file per condition, tri-state, its own
  thresholds and its own "unreadable after N". `internal/observe` stays as the mechanism beneath it
  (cadence, staleness, registry) and grows the missing third state.
- `internal/exec` is new: attempts, gates (cooldown, backoff, dedupe, the headroom rule, retention of our
  own copies), and the window.
- `internal/loop` keeps its name and shrinks to `Reconcile(unit)`: observe, sample, settle, decide,
  gate, publish, persist. It never blocks on a write.
- The interval vocabulary stops being written: `Interval`, `TriggerAt`, `NextCheck`, `Tax`,
  `TightestBound`, `FallbackInterval`, `DeltaShare`, `FullWriteSeconds`, `AvailableStreams`,
  `MemoryBudgetBytes` as planning inputs. They are removed from the state file when `0015` R6 removes
  them from the contract; until then they are published as empty and read by nobody.
- Not built yet, but the boundary allows them without a rewrite: a work queue over units, gang
  membership, and a second storage root. `docs/backlog.md` holds them.
