# 0005 — A copy has a location, and retention counts steps

Status: **proposed**. Needs agreement from the algorithm maintainer: it changes `policy.Copy`, which is
frozen.

## Context

The promise includes "at least `N` copies exist". Today a copy is `{Step, Bytes, State, CreatedAt}`,
which quietly assumes **one copy per step**. Two consequences follow:

1. a second copy on another volume cannot be described — the scan would report two entries with the
   same step, and `retentionPlan` would count them as two steps;
2. the only way to have `N` copies is for the job to write the bytes `N` times, which multiplies the
   write cost by `N` and therefore multiplies the tax by `N`. That makes "more copies" and "a low tax"
   mutually exclusive, which is not what a user asking for durability means.

## Proposal

Add one field and one rule.

```go
type Copy struct {
    Step      int64
    Bytes     int64
    State     CopyState
    CreatedAt time.Time
    Location  string // new: which root/volume this copy lives on; opaque to the algorithm
}
```

Rule: **retention counts steps, not copies.** A step is kept while any copy of it is kept; the
implementation deletes whole (step, location) pairs, never one location's copy while another's
survives the same decision. `KeepCopies` keeps meaning "how many steps", so its meaning does not
change for existing users.

## Consequences

- `copiesToWrite` becomes "how many locations should hold this step", which is the only reading that
  makes `N` affordable: one write plus cheap mirrors.
- `retentionPlan` and `capacityPlan` need the location when they choose what to delete; both are the
  algorithm's, so this is a change in `internal/policy`, not in delivery.
- The state file does not change: the job still writes one checkpoint.
- A missing `Location` (an old trace, or a single-root deployment) is treated as one location named
  `""`, so replays of recorded traces keep working.

## What the implementation will do once this is accepted

- scan every configured root and fill `Location`;
- after a seal, mirror the step to the other roots (a real copy across volumes; a same-volume
  reflink is labelled as a shortcut, not a copy);
- charge the mirror's bytes and time to the tax, because a copy that is not paid for is a copy the
  promise does not know about.
