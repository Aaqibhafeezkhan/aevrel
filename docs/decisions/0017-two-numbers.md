# 0017 — Two numbers, one boolean, one window

Date: 2026-11-01. Status: accepted. Supersedes the "promises" table in `README.md` (as of
2026-10-31), the `MaxLostWork`/`MaxTax` contract in decision 0011 (they are evaluation targets now),
and the three-state fallback in decision 0015 §4.

## Context

Writing a checkpoint is pure cost and no benefit, except in the tail: 99% of users never lose a
machine, and the ones who do lose hours. A fixed interval is therefore a bet, picked from experience
or from a blog post, and it is either too short (writing costs GPU time) or too long (the tail is
uncovered). The earlier interval optimiser (0014) tried to compute a better bet; the risk core
(0015) deleted it and answered a boolean instead. What stayed was the guess: the user still had to
pick one number, and the contract still pretended to promise two bounds.

## Decision

**The product is a backstop, not an optimiser.** There are exactly two numbers, and they answer two
different questions:

| Number | Question | While we run | If we die |
|---|---|---|---|
| `no-us` (required; `none` is an answer) | what would the job have done if the governor were gone? | nothing — the controller never writes on it | the job's own timer at that period |
| `with-us` (optional) | with us in the room, how long may a checkpoint stay stale? | the window (see below) | nothing — it belongs to the controller |

`no-us` is also the baseline of the saving: `elapsed / no-us` is what the user's own habit would have
written, and the report says so by name.

**While we run, a write has exactly two causes**: an urgent condition is `1` (or cannot be read, when
its absence is a declaration and its unreadability is a risk), or the `with-us` window closes. There
is no third cause and no period we invent.

**The window is a deadman**: `due = last seal + with-us − W`, armed by the seal and restarted by any
write. It bounds the staleness of the newest complete copy, so the request goes out one write-time
early. A span shorter than one write is infeasible: due now, and said out loud, never silently
degraded into back-to-back writes.

**The algorithm does not change.** It is the frozen rule of `docs/policy.md` §1: a named boolean,
no clock, no arithmetic, no thresholds. The window is the executor's, because time is the executor's
(0016 R1).

**The plan names its request.** `requestId` replaces `TriggerAt` as the job's dedupe key, so "have I
acted on this request" and "which request did this write answer" are facts, not inferences from
timestamps.

**The plan's expiry means "a live governor exists"**: a few rounds, not the two `no-us` periods the
old arithmetic published by accident. When it expires the job follows its `no-us`, which is the
fallback and the only time-based promise the product makes.

**`may-exit` is the controller's to write.** It can observe whether a write is still in flight; asking
the job to report what the controller can see is the mistake the handshake exists to avoid. The job's
rule is "wait for `may-exit`, or for my own grace period to run out".

**The end of training is written once, always, with no configuration**: the job says `ending`, waits
for the request (bounded by its grace period), makes the write, and then says `may-exit`.

## Consequences

- Deleted from `policy.Bounds`: `MaxLostWork`, `MaxLostSteps`, `MaxTax`, `ExpectedEnd`, `Guarantee`,
  `OnConflict`, `MemoryBudgetBytes`, and the `Fallback`/`FallbackEvery` pair. The simulator keeps its
  targets as its own type (`simulate.EvalBounds`).
- Deleted: `exec.Fallback` (the controller's timer) and its arming at the start of an attempt.
- Deleted from the CLI: `run --lost/--tax`, `status --bound`, the fallback flags.
- Added to the plan: `requestId`, `baselineSeconds`/`baselineNone`, `windowSeconds`; schema 2.
- Added to the status: the window, the receipt (staleness at request → at seal), the write kinds, one
  named failure state, the presence line, and the saving computed from the two numbers.
- The hole is explicit: with `with-us` unset, a calm run writes nothing, and an environment failure
  that produces no condition loses everything since the last exception. The status prints it.

## What this does not promise

No lost-work bound, no tax budget, no probability, no period the controller chose. Those were the
second promise, and the second promise is what made the first one opaque.
