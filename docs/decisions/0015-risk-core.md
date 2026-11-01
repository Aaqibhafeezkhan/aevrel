# 0015 — The risk core: types, rules, contract delta, and the order of work

Status: accepted as the design; the code follows in the steps at the end. Date: 2026-11-01.
`0013` is the catalogue of conditions, `0014` is why the interval work is gone, `docs/policy.md`
is the one-page rule. This record is the layer between them: the exact shapes, what happens in
each tri-state, which facts beat which, and what the implementation and the algorithm each own.

> **One part is superseded.** The `Fallback`/`FallbackEvery` sketch (§3) was replaced by
> `no-us`/`with-us` in `0017-two-numbers.md`; the current `Bounds` type has no `Fallback` field.
> The condition table, the tri-state rules and the precedence order in this record still hold.

The shape is deliberately the one the top controllers use: **a table of named conditions, one
stateless advisor, one stateful executor.** kubelet expresses node health as boolean conditions and
acts on them from a separate eviction manager; Slurm does it with node states; Borg with priority
and preemption. None of them score, and none of them predict.

## 1. Types

```go
// internal/policy/condition.go
type Tier int
const (
    TierUrgent Tier = iota // 1 → advise one write now
    TierWarn               // 1 → show it, never write
    TierNote               // 1 → show it, never write
)

// MissingPolicy says what the absence of a reading means.
type MissingPolicy int
const (
    MissingInert   MissingPolicy = iota // absent because the environment has no such thing
    MissingUrgent                       // present but unreadable is itself urgent
)

type Condition struct {
    Name    string // the frozen name from 0013, also the Evidence key
    Tier    Tier
    Missing MissingPolicy
    Why     string // one sentence, copied into the decision's reason values
}
```

The catalogue is **data**, ordered: when several conditions are true, the first in order is the one
named in the decision. Order is by consequence, not by alphabet:

```
risk.preempt, risk.shutdown, store.low-space, store.corrupt, host.memory-pressure,
host.gpu-fault, watch.blind, watch.broken, job.ending, write.failed-twice
```

Diagnostics are in the same table with `TierWarn`/`TierNote` and never in the trigger branch:
`store.blocked`, `store.slow`, `host.gpu-hot`, `host.cpu-throttled`, `job.step-slow`,
`net.long-outage`.

## 2. The input channel

No new `Snapshot` field. Each condition arrives in `Snapshot.Evidence`, which already exists for
exactly this ("a named bag of observations"). Three states, and only three:

| Wire state | Meaning | Effect |
|---|---|---|
| key **absent** | the environment has no such condition | inert, and `Missing` does not mention it |
| key present, `Kind == Unknown` | it has it, but we cannot read it | `MissingUrgent` → advise write, name it in `Missing`; `MissingInert` → ignore |
| key present, `Kind == Boolean` | a fresh reading | `Bool == true` → its tier applies; false → nothing |

`Observation.At` and `Source` travel with the reading and are copied into the decision's
`Reasons[].Evidence`, so a replayed decision says which adapter it trusted.

## 3. The rule, in the order it runs

1. **Safety first, as today:** a write in flight → `hold`; two consecutive failed writes → `repair`.
   A broken copy is rewritten. These are job-local facts and beat every condition.
2. **Any `TierUrgent` condition with a fresh `true`** → `ActionSave`, `ReasonRisk`,
   `Reasons[0].Code` = that name.
3. **Any `MissingUrgent` condition with `Unknown`** → same, and its name goes into `Missing` as
   `blind:<name>`.
4. **Only `TierWarn`/`TierNote` true** → `ActionSkip`, `ReasonCalm`; nothing is written and the
   reasons list what is being watched.
5. **Nothing at all** → `ActionSkip`, `ReasonCalm`.

There is no clock, no threshold, and no arithmetic. (The fallback timer is the executor's; the
algorithm never sees time.)

## 4. Contract delta

`Bounds` gains exactly one field, and `Snapshot`/`Decision` gain none:

```go
type FallbackMode string
const (
    FallbackUnset FallbackMode = ""     // no decision yet → the executor must refuse to start
    FallbackNone  FallbackMode = "none" // the user signed: no periodic writes
    FallbackEvery FallbackMode = "every"
)
type Bounds struct {
    // ... existing fields ...
    Fallback FallbackMode // unset | none | every
    FallbackEvery time.Duration
}
```

- `FallbackUnset` is the only state in which the executor refuses to run without the confirmation
  dialog. `none` is the user's signature and must never be inferred from an empty value.
- `Decision` keeps `Action`, `Reason`, `Reasons`, `Sensitivity`, `Missing`, `Kind`, `WeightsOnly`,
  `CopiesToWrite`, `KeepCopies`, `OldestStepToKeep`, and `Why`. `Interval`, `TriggerAt`,
  `NextCheck`, `Tax` and `TightestBound` become **deprecated**: still in the type until the interval
  code is deleted, never read by the risk core and not written by it.
  `FallbackInterval` stays: it is the period the job uses when the controller is away, which is
  exactly the fallback the user chose (`FallbackEvery`), and `risk.go` sets it in that mode.
- `job.ending` stays a producer condition, not a bound: if the user turns the final write off, the
  producer does not report it. Absence is inert, which is the behaviour we want.

## 5. What happens to the old policy

- The interval implementation is **deleted**: `baseline.go`, `pattern.go`, `core.go`, `factor*.go`,
  their tests, the pattern oracle, the arithmetic checks and their goldens. The registry ships
  `risk-v1` only, and a test fails if the name ever comes back.
- The delivery layer's default is now `policy.Risk{}`: with no producers reporting conditions it
  writes nothing, which is the state its user signed for. `internal/loop`'s tests use a local
  `testPolicy` for "a clock that writes", because they are about the loop, not about what makes a
  write worth doing.
- `ckptctl run` and `ckptctl table` use the risk core; the table's risk column is replayed with a
  modelled warning window (`Options.WarnBefore`): `risk.preempt` before each recorded kill,
  `job.ending` before the trace ends, and `store.low-space` below two checkpoints. That window is an
  assumption about lead time, printed in the header; the real one is the measurement 0014 §8 asks
  for.
- The first table run states the rule the whole design rests on: **the lead time has to cover one
  write.** With a modelled 90-second warning, the small models write three times where a fixed
  15-minute timer writes eight, but the 180-second model writes *zero* and loses everything, because
  no write can finish inside the warning. That is why the content, shard and asynchronous axes still
  matter: not to save tax, but to make the write short enough that a warning can save the work.

## 6. Tests: truth tables, not statistics

| Test | Acceptance |
|---|---|
| `TestTheCatalogueMatchesTheRecord` | the name set, tiers and missing policies equal 0013 exactly; a rename fails |
| `TestEachConditionTruthTable` | for every name: absent does nothing; `0` does nothing; `1` fires only if urgent; `Unknown` fires only if `MissingUrgent` and is named in `Missing` |
| `TestDiagnosticsNeverTrigger` | each of the six diagnostics set true changes no action |
| `TestTheAlgorithmIsEnvironmentBlind` | a CPU-shaped snapshot (GPU condition absent) and a GPU-shaped one (GPU condition `0`) decide identically; all-absent equals all-zero |
| `TestSafetyBeatsConditions` | in-flight beats a firing condition; two failures beat a firing condition |
| `TestTheSameSnapshotDecidesTwice` | ten identical calls, identical decisions |
| `TestTheRegistryShipsOnlyRisk` | `risk-v1` present; `pattern-v1`, `baseline-v1` absent |

## 7. Order of work (algorithm side)

| # | Step | Box | Acceptance |
|---|---|---|---|
| R1 | `condition.go`: catalogue as data, tiers, missing policy, `Why` | 1h | `TestTheCatalogueMatchesTheRecord` prints `conditions: 13 total, 7 urgent, 6 show-only` |
| R2 | `risk.go`: tri-state reader + the five rules, no clock | 2h | `TestEachConditionTruthTable` green for all 13 × 4 states (52 cases) |
| R3 | precedence: safety first, then conditions; `ReasonRisk`, `ReasonCalm` | 1h | `TestSafetyBeatsConditions` green |
| R4 | contract delta: `Fallback` three-state; `FallbackUnset` → `ActionUnknown` | 1h | a snapshot with an unset fallback refuses; with `none` it never writes without a condition |
| R5 | registry swap + `ckptctl table` + re-pin | 2h | `TestPatternIsRetiredFromTheRegistry`; golden prints `1 policies, 14 families, 5560 decisions, 0 diff` |
| R6 | delete the interval policy, its tests and goldens, once the delivery layer stops constructing it | 2h | `make check` green; `Available()` returns one name; no `Interval`/`Tax` field written anywhere |
| R7 | handoff note for the producers and the executor | 1h | `docs/conditions.md` matches the catalogue one-to-one |

Total about ten hours. R1–R3 need nothing from delivery; R4 is a contract change the delivery
layer must be told about before it lands; R5–R6 are the removal of the old line.

## 8. Division of labour

- **Algorithm side (this branch):** the catalogue, the rule, the truth tables, the contract delta
  and the registry. Small, and provable without a cluster.
- **Implementation side:** the producers (one adapter per condition, each owning its own raw
  threshold and its "unknown after N") and the executor (backoff, fallback timer, atomic seal,
  read-back verification, retention of our own copies, the 200% storage rule).
- If they are short-handed, the two simplest producers (`store.low-space` from `statfs`,
  `host.memory-pressure` from cgroup) can be written here as reference adapters, since they are
  one file each and they exercise the tri-state contract.

## Falsifiers

- A condition that has to be told apart from another condition to decide means the tiers are wrong.
- Any number in `internal/policy` other than a tier is an arithmetic entry point that has to be
  justified or deleted.
- If a producer has to send a raw number for the algorithm to interpret, the abstraction leaked.

## 9. Closure

The algorithm layer's surface is now fixed, and this is the test of it:

| Surface | Final shape | Pinned by |
|---|---|---|
| condition names and order | 13 rows, 7 urgent | `TestTheCatalogueMatchesTheRecord` |
| what a condition means | `0`, `1`, absent (inert), unknown (urgent or inert) | `TestEachConditionTruthTable` |
| the rule | safety facts, then catalogue, no clock | `TestSafetyFactsBeatConditions`, `TestABrokenProbeOutranksEverything` |
| the code space | 13 condition names + 3 repair reasons, nothing else | `TestTheCodeSpaceIsClosed` |
| the shipped policy | `risk-v1` only | `TestPatternIsRetiredFromTheRegistry` |
| the fallback | three states, unset refuses | `TestTheFallbackHasThreeStates` |
| environment blindness | CPU and GPU jobs decide the same; absent equals zero | `TestTheAlgorithmIsEnvironmentBlind` |
| the contract | one bounds addition, no snapshot or decision field | `TestEveryContractFieldHasAnOwner` |
| the field list | 34 snapshot + 13 bounds, each with an owner, 20 marked deprecated | `TestEveryContractFieldHasAnOwner` |

Anything beyond this needs a new record, exactly as 0011 required for a field. What is deliberately
outside the closed surface: the producers, the executor, and the deletion of the deprecated types —
those belong to the delivery layer's migration, and none of them can change what the algorithm
decides.

