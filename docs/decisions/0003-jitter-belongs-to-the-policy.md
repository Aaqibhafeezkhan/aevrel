# 0003 — Jitter belongs to the policy, not the state file

Status: accepted. Date: 2026-10-31.

## Context

`docs/policy.md` §5 asks for a ±10% jitter on `fallbackIntervalSeconds` so that many jobs do not
fall back at the same moment. `baseline-v1` does not implement it, and
`TestFallbackPeriodIsTheBound` asserts the exact values (10m with a known write cost, 5m without).

The implementation publishes a `Decision`. If it added jitter while writing the file, the published
bytes would no longer be a function of the `Snapshot`, and a replay of the same trace would produce
a different file. Determinism is what makes every decision reviewable offline.

## Decision

- The implementation publishes `FallbackInterval` exactly as the policy returns it.
- Jitter, when it is wanted, is computed by the policy and returned in `FallbackInterval`, where a
  trace can reproduce it.

## Consequences

- Until a policy implements jitter, jobs fall back at the same instant. The volume write gate
  already limits how many writes start together, so the practical cost is a queue, not a storm.
- Adding jitter later is a change in `internal/policy` plus one test, and no change in delivery.
