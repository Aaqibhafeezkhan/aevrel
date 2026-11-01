# Why it did not write

A controller that says nothing is indistinguishable from one that is broken. This page is the
answer to "why is there no new checkpoint", in the order the controller itself decides.

## Ask it

```
ckptctl explain --dir=/path/to/checkpoint
```

It reads the plan the job was given, not a log, so the answer is exactly what the job saw:

```
decision   skip (calm)
why        no condition says the work is at risk
write      full, keep 1
```

`decision save (risk)` followed by a `reason` line means it *did* decide to write; the question then
becomes whether the job obeyed.

## The doors, in order

A save request has to pass every one of these. The first that refuses is the answer, and `explain`
names it through the reason and the history.

| Door | What it means | What to do |
|---|---|---|
| `not-a-save` | the decision is `skip` or `hold` | read `why`: calm, or waiting for the job |
| `in-flight` | a write from this unit is still running | wait; a second write would protect nothing |
| `headroom` | less free space than the next write needs | free space, or lower `freeSpaceFactor` deliberately |
| `duplicate` | the same trigger at the same step was already attempted | nothing: the attempt is recorded once |
| `cooldown` | too soon after the previous attempt | nothing: this is the cost of writing |
| `backoff` | the last attempts failed | look at the attempts, not the timer |
| `volume-busy` | another unit on the same volume is writing | wait: one write per volume at a time |
| `no-time` | the announced deadline is shorter than one write | accept the loss, or make the write cheaper |

## A refusal is not a debt

A near miss does not raise the probability of the next one. The controller does not "make up" a
write later because it skipped one earlier; writing because you owe a write is cost without a
reason. The protection against near misses is the job's own timer, which keeps running whether the
controller is alive or not.

## If the reason is a condition

Each condition has one card with its threshold and what "absent" and "unknown" mean:
[conditions.md](conditions.md). A condition that is `unknown` is never `0`: it means the controller
could not look, and it says so (`ckptctl doctor` shows the same three-state answer without waiting
for a decision).
