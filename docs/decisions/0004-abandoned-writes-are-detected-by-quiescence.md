# 0004 — An abandoned write is detected by quiescence, not by duration

Status: accepted. Date: 2026-10-31.

## Context

A write can be slow for good reasons: a large checkpoint, a contended disk, a storage node that is
momentarily busy. The controller has to decide when a directory without a manifest has stopped being
a write in progress and become a failed one, because only then can it ask for a repair.

The obvious rule is a duration: "no manifest after `3·W`, it is dead". That rule kills a write that
is legitimately slower than the 90th percentile — which is exactly the write the bound must survive,
since `W` is a percentile and not a ceiling.

## Decision

**A write is abandoned when the directory has not changed for `max(3·W, 60s)`, not when it has been
open for that long.**

- "changed" is observed on the controller's clock: the newest modification time of the directory or
  any file inside it, sampled once per round;
- a write that keeps writing bytes is never abandoned, however long it takes;
- a write that has stopped writing bytes for that window is dead, whatever its step.

The state becomes `broken` with the code `write_abandoned`; the policy then answers `repair`, and the
next save point rewrites the checkpoint.

## Consequences

- A slow write is allowed to finish; the cost of waiting is bounded by the lost-work bound, which
  keeps growing anyway, so the controller may also decide to keep waiting and say so.
- A hung write (a dead NFS handle, a stuck process) is detected within the window instead of never.
- The rule needs the directory's modification time, which is cheap (`stat` per file, already part of
  the scan).
