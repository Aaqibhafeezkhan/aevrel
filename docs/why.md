# Why a separate project

An earlier project asked "is this checkpoint safe to resume from?" and grew to cover reading
checkpoints, choosing one, holding a pod's start, and reacting to preemption. This project starts
from a narrower question with a clearer answer.

## What changed

1. **The write side is where the unique data is.** The only information nobody else has is the
   record of what happened while a checkpoint was written: when it started, when it finished, how
   many bytes landed, whether every part arrived. Everything else can be read from the cluster by
   anyone.
2. **Deciding when to write is the product.** The earlier project could only observe and react.
   Here the controller decides: when to write, how often, and later how. That decision matters on
   every checkpoint, not only when something goes wrong.
3. **The read side belongs elsewhere.** Choosing a resume point, blocking a start, and rescuing a
   pod during preemption are scheduler and platform jobs. Keeping them in the same project made
   every change expensive and put us in conflict with scheduling tools.
4. **The core is an algorithm, and algorithms need experiments.** Experiments inside a product
   repository are accidental: they look like features and accumulate. Here the algorithm is a
   separate package fed by recorded traces, so experiments are cheap and the rest of the code does
   not move.
5. **Two implementations diverged once.** A previous split produced two versions of the same
   feature on two branches. This repository is written in English from the first line and has one
   canonical description of each thing.

## What we keep from the earlier project

- the commit protocol for a checkpoint: shards, then a manifest, then a seal;
- the rule that a number without samples is unknown, never zero;
- replaying decisions from recorded data instead of trusting memory;
- the habit of printing the number that proves a claim.

## What we do not bring over

- resuming, admission, pod gating, preemption rescue;
- any code that depends on a specific scheduler;
- the accumulated documents of the earlier repository.

## What this means for the name

The project is named for what it does: it governs checkpoint writes. It is not named after the old
project, so that a reader does not inherit the old scope.
