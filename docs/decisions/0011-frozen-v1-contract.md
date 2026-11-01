# 0011 — The frozen V1 contract: every field, why it exists, what fills it

**Superseded for the product claim by 0013/0014 (2026-11-01).** The types below are still what the
code has; the fields tied to the interval optimisation (`MaxLostWork`/`MaxLostSteps` as a target,
`MaxTax`, `Interval`, `TriggerAt`, `Tax`, `TightestBound`, `FallbackInterval`, `DeltaShare`,
`FullWriteSeconds`, `FixedOverheadSeconds` as planning inputs) are dead and have been removed. The parts that survive
are the unknown-versus-zero rule, the job-local facts, and the `Evidence` channel that carries the
risk conditions.

Status (original): accepted. Date: 2026-11-01. This is the authority for the V1 input and output. Adding a
field later needs its own record; removing or changing the meaning of one is not allowed. The types
live in `internal/policy/policy.go`; this page says why each exists and what the implementation
layer must produce.

## Bounds — what the user commits to

| Field | Why it exists | Required | Default |
|---|---|---|---|
| `MaxLostWork` | the promise, in time: the newest complete checkpoint is never older than this | one of the two | — |
| `MaxLostSteps` | the same promise in steps, because work per second changes during training | one of the two | — |
| `MaxTax` | the price ceiling: writing never takes more than this share of elapsed time | no | 0.05 |
| `MinCopies` | durability: at least this many copies exist | yes | 1 |
| `Retention` | how long a copy is kept before it may be deleted | no | keep all |
| `StorageLimitBytes` | the space the copies may use; zero means the volume's free space | no | volume |
| `ExpectedEnd` | when training ends, so the last checkpoint is the useful one | no | unknown |
| `Guarantee` | `worst-case` (default) or `average`; changes the period by 2x, never silently | no | worst-case |
| `OnConflict` | when both bounds cannot hold, which one is reported as kept | no | prefer-loss |
| `Resume` | `exact` or `weights`: what a checkpoint must contain to be usable | no | exact |
| `MemoryBudgetBytes` | how much memory the user allows the controller to hold for snapshots | no | none |

## Snapshot — what the implementation measures

Identity and clock (always required): `UnitID`, `Generation`, `Now`, `Capabilities`.

| Group | Fields | Why | Unknown |
|---|---|---|---|
| progress | `Step`, `StepTimeSamples` | the step unit, and the conversion when the bound is in steps | empty samples |
| remaining | `RemainingSteps`, `Remaining` | the end-of-training rule: one final write, not a useless one | -1 |
| seal | `LastSeal` | the age of the newest complete checkpoint is the whole promise | zero = none |
| write in flight | `SaveInFlight`, `SaveStartedAt`, `SaveProgress` | do not start a second write; detect a stuck one | -1 for progress |
| write cost | `WriteCostSamples`, `FullWriteSeconds` | the period is a function of it; a delta sample is not a full write | empty, -1 |
| overhead | `FixedOverheadSeconds` | at a short period the fixed part, not bytes, spends the budget | -1 |
| size | `CheckpointBytes`, `WeightsOnlyBytes` | the content axis; the ratio is the saving | -1 |
| delta | `DeltaShare` | the cost of a delta per second it covers; zero means it is no cheaper | -1 |
| chain | `LastKind`, `LastFullStep`, `DeltaChainLength`, `BytesSinceFull` | a resume needs the chain, and the chain length is a decision | empty, 0 |
| storage | `FreeBytes`, `VolumeBytes`, `WriteThroughputBytesPerSecond` | capacity refuses instead of failing a write; throughput is the early storm signal | -1 |
| copies | `Copies` | retention, a broken copy, the newest step | empty |
| failures | `ConsecutiveWriteFailures` | stop writing into a broken mount and say so | 0 |
| totals | `WriteSecondsTotal`, `ElapsedSecondsTotal` | the realized tax, to compare with the planned one | -1 |
| memory | `BuffersInUse`, `BufferBytes` | async needs the number of snapshots already in flight | 0 |
| recovery | `ResumeSeconds` | the chain length is a function of replay cost, not a constant | -1 |
| streams | `AvailableStreams` | how many parallel writes the writer can do | 0 |
| evidence | `Evidence` | delayed signals (peer pressure, maintenance window) without new fields | missing key |
| previous | `Previous` | the stability rule keeps the old period instead of chasing noise | zero |

`Capabilities` is the only group that is a *denial*: `WeightsOnly`, `Delta`, `Async`, `Levels`,
`MaxStreams`, `MemoryBytes`. A zero set means "not filled" and leaves the measured fields in charge;
a filled set removes whatever it says is unavailable, and the decision names it in `Missing`.

**Rule that makes these fields honest:** a negative number, an empty slice or a zero timestamp means
unknown, never zero. A measurement the implementation cannot take is reported as unknown; the policy
then degrades explicitly instead of planning on a guess.

## Decision — what the algorithm returns

| Field | Why |
|---|---|
| `UnitID`, `Generation` | a decision is valid only for the unit and the restart it was computed for |
| `Action`, `Reason`, `SaveNow` | what to do now, and the machine-readable cause |
| `Kind`, `WeightsOnly`, `Shards`, `Async`, `Buffers` | what to write, how many streams, and whether the copy runs behind the loop |
| `Interval`, `TriggerAt`, `NextCheck` | when to write, when to start, when to ask again (absolute, zero means now) |
| `Tax`, `FallbackInterval` | the planned price, and the period the job uses when the controller is absent |
| `CopiesToWrite`, `KeepCopies`, `OldestStepToKeep` | durability and retention, as a floor and a step |
| `TightestBound` | what the budget could buy, when it is materially tighter than what was asked |
| `Missing` | which measurements were absent and changed the answer |
| `Reasons`, `Sensitivity` | the structured "why" and "what would change it" a UI renders |
| `Why`, `Version` | one sentence for a person; the policy that produced it |

`Reasons` uses a closed vocabulary: the codes, the numbers each may carry, and the fields each may
cite are in `internal/policy/reasons.go`. Every decision carries at least one code, refusals
included. Adding a code is a line there plus the decision that emits it; the replay test checks that
nothing outside the vocabulary is ever said.

## What the implementation layer must do

1. Fill the snapshot honestly; unknown is `-1`, empty, or zero time — never a plausible zero.
2. Fill `Capabilities` once per unit when the writer knows what it can do; leaving it zero is legal
   and means "no denials".
3. Keep `Now` as an input, not a call inside the policy; the policy never reads a clock.
4. Put the same snapshot through the same policy version and get the same decision, including
   `Reasons` and `Sensitivity`.
5. Report the realized tax (`WriteSecondsTotal`/`ElapsedSecondsTotal`) so a divergence between the
   plan and reality is visible instead of absorbed.

## Deliberately absent from V1

`Levels` in the decision (which storage path to use), `FullEvery`/`ChainToKeep` as explicit numbers,
phase weighting, peer pressure, and any prediction of interruptions. Each arrives with its own
module and its own optional measurement; none of them needs a change to the fields above.
