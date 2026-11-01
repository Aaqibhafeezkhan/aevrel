# 0002 — Unknown values where the contract has no sentinel

Status: accepted. Date: 2026-10-31.

## Context

`Snapshot` uses `-1` for unknown on `FreeBytes`, `VolumeBytes`, `Remaining`, `RemainingSteps`,
`WriteThroughputBytesPerSecond` and `SaveProgress`. Four fields have no such convention:

`Step`, `CheckpointBytes`, `ConsecutiveWriteFailures`, `WriteSecondsTotal`, `ElapsedSecondsTotal`.

The rule "unknown is not zero" cannot be applied by inventing a sentinel in a frozen contract.

## Decision

| Field | Unknown value | Why it is safe |
|---|---|---|
| `CheckpointBytes` | `0` | `capacityPlan` already treats `<= 0` as "capacity cannot be planned" and records "checkpoint size" in `Missing` when free space is known. Zero here means "not measured", and the existing code path says so. |
| `ConsecutiveWriteFailures` | `0` | It is a counter the implementation maintains, not a measurement. Zero means no failure has been observed, which is a fact. |
| `WriteSecondsTotal`, `ElapsedSecondsTotal` | `-1` | Nothing in `baseline-v1` reads them; they exist for later policies and for reporting. A negative value keeps "no data" distinguishable from "no time". |
| `Step` | never unknown | The newest sealed copy's step is a floor that always exists once a checkpoint does. Without a live step the implementation reports the floor and marks `Evidence["step.source"] = "checkpoint"`, which also means `StepTimeSamples` is empty and a step-based bound degrades to unknown by the policy's own rule. |

## Consequences

No struct changes, no schema change. The four rows are documented here so that a later reader does
not "fix" zero into a measurement.
