# The backstop: two numbers, one boolean, one window

Status: design. Date: 2026-11-01. Supersedes the "optimiser" framing in `README.md`, the second
pass (a controller-chosen `auto` period) and the third pass (no timer at all while alive). §2
restates the frozen rule in `docs/policy.md` §1 and does not redefine it. The engineering design
built on this page is `docs/design-v1.md`.

## 0. The saving, in one paragraph

Without the governor a user writes every 10 minutes because he is afraid of what he cannot see. With
it he configures 30 minutes, because the exceptions are covered. The saving is the difference
between those two periods: 3× fewer scheduled writes for the same protection. Everything else
follows from it:

- The window's refresh rule (§1) saves nothing. It only refuses to charge the user twice for a
  window that an exception write has already served. An exception write in the middle is not a
  saving: it would have happened anyway.
- Configuration convenience is real value, but it is not the saving: it is why the longer period is
  reachable without trusting a guess.
- The report is real value: it is how the user checks that the longer period is safe.

## 1. Two numbers, two questions

| | `no-us` (required answer) | `with-us` (optional) |
|---|---|---|
| the question | "without the governor, how often would you have written?" | "with the governor, how long may a checkpoint be stale?" |
| while we run | nobody uses it | **the executor** keeps the window |
| when we die | **the job's own timer** | nobody |
| in the report | **the baseline: `run / no-us`** | the comparison: `run / with-us` |
| valid answers | a duration, or `none` (you would not have written at all) | a duration, or unset (no time-based write) |

**The window is a deadman, and its refresh is bookkeeping.** It is measured seal to seal, and any
write restarts it — an exception at 09:00 means the window that was due at 10:00 is now due at 10:05.

```
written at 09:00 (exception, sealed 09:05) → the window is served; next due 10:05
written at 10:05 (window,   sealed 10:10) → next due 11:10
written at 10:40 (exception, sealed 10:45) → next due 11:45
```

The middle line is *not* a saving: had the exception not happened, the window write at 10:00–10:05
would have happened anyway. The refresh only stops the window from billing the user twice.

The window bounds staleness of the newest **sealed** copy, so a request goes out one write-time early
(seal by `due − W`); if `W ≥ with-us` the window is infeasible and is reported as such.

`no-us` is asked once at onboarding and the CLI refuses to start without an answer, because both the
job's own timer and the saving are computed from it.

## 2. The algorithm: the frozen rule (`docs/policy.md` §1), unchanged

> Is there an event after which this job's work would be lost, and is there still time to save it?

It answers with a named boolean. It computes no interval, no tax and no probability.

1. Any **urgent** condition that is `1` → one write now; the decision names the first such condition
   in catalogue order.
2. A **required** condition that cannot be read is itself urgent: blindness is a risk, not a zero.
3. A condition the environment does not have is **absent** and changes nothing.
4. A **warn** or **note** condition never triggers a write.
5. Job-local facts are triggers too: training is ending, and two writes failed in a row.
6. The algorithm contains **no clock**. Backoff, timers, retries, sealing and read-back are the
   implementation's.

Everything else this design talks about — the `with-us` window, the announced-deadline fit test, the
space and duplicate gates — is the **implementation**, not the algorithm. The algorithm's input is
the catalogue (`internal/policy/condition.go`: 14 rows, three tiers, two missing policies, precedence
order) and the evidence those rows produce.

## 3. Who owns what

| Plane | Owns | Must not |
|---|---|---|
| the job | its own `no-us` timer (used only when we are gone); its save point; `ending`/`stopping`; the final write | decide when it is at risk |
| us (the loop) | conditions; the exception rule; the `with-us` window (armed at the seal, one write-time early, refreshed by any write); the deadline fit test; space/duplicate/backoff gates; publishing both numbers and the plan's expiry; the final-write request; the report | invent a period; guess a probability |
| the world (observer) | cluster and unit facts, as booleans at the border | cross L3 with numbers |

## 4. What changed in the code

**The window timer was redefined, not added.** `internal/exec/window.go` is driven by `with-us`
(unset → no timer), arms at the **seal**, requests one `W` early, and reports infeasibility when
`W ≥ with-us`. The timer it replaced armed at the start of an attempt, which made peak staleness
`with-us + W`.

**The plan carries both numbers** plus `requestId` (the attempt id) in place of `TriggerAt`, which
was always zero under risk-v1 and could not serve as a key. `expiresAt` is "last publish + one
round": it means "a live plan exists".

**The job's rule**: a live plan is obeyed; when it expired, the job runs its own `no-us` timer (or
nothing, if `no-us` is `none`). `ckpttrainer` implements it, and the demo prints the line.

**The final write is a protocol**: `ending` and `stopping` share it; `may-exit` only after the final
write landed; a final write that cannot be made is a loud failure.

**Deleted**: the seven dead `Bounds` fields (`internal/policy/policy.go`), the fifteen dead
`Snapshot` fields, `ReasonEntry.Values` numbers, and `Decision`'s superseded interval fields
(`Interval`, `TriggerAt`, `NextCheck`, `Tax`, `FallbackInterval`, plus unused `Kind`/`WeightsOnly`/
`Shards`/`Async`/`Buffers`). `docs/policy.md` §2 lists them as superseded.

## 5. The saving: the gap between the two numbers

```
no-us = 10m   → 8h/10m  = 48 scheduled writes
with-us = 30m → 8h/30m  = 16 scheduled writes
exceptions in that run: 2   (each one eats a window slot it would have paid anyway)
actual: 16 + 2 − 2 = 16, plus the final write (excluded)
saved: 32 writes; time: 32 × W
```

The arithmetic has one baseline: **the number the user stated as his own habit.** It is not a claim
about what he would have chosen — he chose it. If `no-us` is `none`, the baseline is zero writes and
the saving is zero, and the report says the value there is the coverage, not the count.

The report separates the three write kinds, because they have three different meanings:

```
writes 18 (16 window + 2 exception + final excluded)
no-us 10m → 48 scheduled · with-us 30m → 16 scheduled · saved 32 (66%), 32 × write time
window refreshed by an exception 2 times (not a saving; the window saved twice)
```

## 6. The hole, and where it lives

With `with-us` **unset**, while we are alive and every condition reads false, nothing is written: an
environment failure that produces no boolean loses everything since the last exception. That is the
explicit trade, and it is displayed (`protecting: exceptions only · oldest safe point: step 0 (2h14m)`).

With `with-us` set, the hole is bounded by that number — which is the only reason to set it.

## 7. What we never add

1. No controller-invented period, in any disguise (no `auto`, no `k × write time`).
2. No second role for the algorithm: it stays boolean, clock-free, arithmetic-free.
3. No severity or probability in the plan; no prediction of the platform.
4. No third number.
5. No silent absence: "governor gone", "blind", "read-only", "final write failed", "baseline unset"
   are named states.

## 8. Acceptance: the v1 exit criteria

| Promise | Line |
|---|---|
| the fallback: if we die and the job lives, the job writes every `no-us` | `governor stopped 61s ago → job wrote at 10m04s on its own` |
| the exception write: a condition at 1 → one write requested in the same round | `risk.shutdown 1 → request in 0s → sealed 92s (window 300s)` |
| presence: every round says why it did not write; every write says what it bought | `presence: watching 14/14, 0 blind · nothing fired · window due in 51m` |
| the final write: the last state of a finished run is on disk, with no configuration | `ending → final write requested → sealed 1m08s → may-exit (step 12000)` |
| the refresh: the window restarts on any write, and it is not counted as a saving | `exception at 09:00 (sealed 09:05) → window due 10:05, counts 1 write, saves 0` |
| the saving: the run reports the gap between the two numbers | `16 window + 2 exception · no-us 10m → 48 · saved 32 (66%)` |

## 9. What landed, item by item

| Item | Touches | Depends |
|---|---|---|
| the window: `with-us`, armed at the seal, one write-time early, refreshed by any write | `internal/exec`, `internal/loop` | this side; can start now |
| the fallback: `no-us` published, the job writes on expiry | `internal/advise`, `cmd/ckpttrainer`, `docs/glue.md` | one coordinated contract commit; decides whether the fallback is true |
| the dead fields and the interval fields, `values` | `internal/policy`, `internal/assemble` | this side; can start now |
| `requestId` in the plan | `internal/advise`, `loop`, `ckptctl`/trainer | after the fallback |
| the presence line, the receipt and the two-number report | `internal/advise`, `cmd/ckptctl` | data first, display with the delivery layer |
| the final-write protocol | `internal/loop`, `internal/produce`, `cmd/ckpttrainer`, `docs/glue.md` | needs the job half |
