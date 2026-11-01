# 0001 — The unit is a directory, not a pod

Status: accepted. Date: 2026-10-31.

## Context

A training job may run as one pod, as a gang of ranks, or as a batch of jobs. The algorithm sees
only `UnitID` and `Generation`, and the contract already says the implementation decides what one
write unit is (design.md, "The line between the algorithm and the implementation", rule 4).

The choice matters because one checkpoint is sealed by a group: shards, then a manifest, then a
seal. If a unit were a pod, an eight-rank gang would produce eight decisions, eight triggers, eight
concurrent writes on one volume and eight retention plans. The write tax would multiply by eight
while each decision still looked compliant.

## Decision

**A unit is the set of processes that seal one checkpoint directory together. The directory is the
identity; a pod is only a member.**

- one pod, one directory: a unit with one member;
- a gang: every rank writes the same directory, so the gang is the unit;
- a batch: every job has its own directory, so every job is a unit.

`Generation` increases when the writing set changes (members replaced, world size changed, or the
trainer reports a new boot id), because a decision from an older set must not be applied to a newer
one.

Members are discovered by watching workloads, but the directory remains the source of truth: if the
watch fails, the unit is still the directory, and generation falls back to an observed step
regression.

## Consequences

- The algorithm never learns what a pod, a rank, or a gang is. Grouping is one package.
- Several units may share a volume; the implementation limits concurrent writes per volume and
  staggers starts. That is a resource limit in the executor, not a second decision.
- Two workloads writing one directory, or one workload writing several, are reported, not resolved
  by guessing a "main" directory.
