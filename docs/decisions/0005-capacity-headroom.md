# 0005 — The capacity headroom is 1.1, not 1.5

Status: accepted. Date: 2026-10-31.

## Context

`capacityPlan` refused to plan when free space was below `CopiesToWrite · CheckpointBytes · 1.5`.
The factor was a guess for temporary shards and the manifest. Replaying a 2 GB volume with 900 MB
checkpoints and one required copy exposed it: after one copy there were 1.1 GB free, the rule asked
for 1.35 GB, and the policy answered `infeasible`/`capacity` at every save point. The result was
one checkpoint for the whole run and the newest complete checkpoint older than the bound for 49.2%
of elapsed time — a refusal that broke the promise it was protecting.

## Decision

- The headroom is 1.1. A write of `N` copies needs `N · B` bytes plus a tenth for the temporary
  parts of the copy being written; that covers the manifest and the shards, and it stops refusing
  writes that fit.
- The rest of the rule is unchanged: delete copies beyond `KeepCopies` first, refuse only when
   `free + reclaimable` is still below the need, and always state the numbers in `Why`.

## Evidence

`ckptctl table` on the `capacity` family, 2 GB volume, 900 MB copy, 1 required copy:

| rule | V | saves | refusal |
|---|---|---|---|
| 1.5 | 49.2% | 1 | every save point after the first |
| 1.1 | 0.0% | 3 | none |

## Consequences

- A refusal now happens only when the write genuinely cannot fit, which is the case the action was
  named for.
- Any future change to the factor needs the same replay evidence, because both directions are
  expensive: too small risks a failed write that costs `W`, too large silently stops checkpointing.
