# 0014 — Why the product moved from intervals to risk conditions

Status: accepted. Date: 2026-11-01. This is the record the implementation layer reads to understand
*why* the algorithm changed, so that the code it is asked to write later does not look arbitrary.
0013 is the resulting design; this page is the reasoning that produced it.

## 1. The starting point

We set out to answer one question: **when should a training job checkpoint?** The first answer was
arithmetic: keep the newest checkpoint younger than `L`, spend at most `τ` of elapsed time writing,
and if a delta is cheaper than the budget, write a pattern instead of a single period. That is V1:
`baseline-v1` plus `pattern-v1`, a contract, a simulator, and a golden pin over 2 policies × 14
families × 11,527 decisions.

The product claim attached to it was: *the optimal interval moves, so no fixed constant is close
everywhere, and tracking it saves money.*

## 2. What the evidence said

We tested that claim instead of assuming it, and it failed in a specific way:

- The feasible tax of *any* schedule lies between `W/L` and `τ`, and the average age between
  `(L−W)/2` and `L/2`. So whatever the interval does, it can only move a few percent of a number
  that is already small.
- In a stationary world, a well-tuned constant sits at the optimum, and the second-order cost of a
  constant being in the wrong place is `≈ 1 + ½δ²` — a few percent for a 4× swing in `W`.
- Replay confirmed the arithmetic: the online policy essentially matched the offline pattern. V1's
  own record (`0007`) therefore withdrew the adaptivity claim and sold "the pattern" instead — a
  weaker product than the one we wanted.

In parallel we found that the axes *inside* a write (weights-only, asynchronous copy, shards) do
carry real multiples (3×, 2–4×), but those are properties of the write path, not of *when* to write.

## 3. The turning point

The owner asked a question the arithmetic could not answer: if the machine never fails, the best
schedule is "never write", so **where does the value come from?** The honest answer is: from the
user's fear. A checkpoint exists only because the work might disappear. Nobody — including us —
knows when a machine will die; a policy that predicts the failure rate is inventing a number, and
once the interval is tuned against that invention, adaptivity has nothing real to track.

That reframing changes the object of the algorithm:

- **Before:** minimise lost work subject to a tax budget, with an assumed failure rate.
- **Now:** notice when the work is about to be lost, and save it while there is still time.

The second question is a boolean, and it does not need a failure rate. It needs **observations**:
is this job about to be preempted, is the node going away, is the disk about to fill, is the
machine out of memory, did we lose sight of any of it.

## 4. Why this is stronger, in the user's terms

A user writes checkpoints because they do not trust the environment. If they trust us to watch it,
they can stop paying for that distrust: the fallback interval can be long, or absent entirely. With
no periodic writes, the write time drops to zero; with a warning, we write once before the danger
arrives. The product is **trust, not speed** — and it holds in every environment, because "watch
the environment and act on the facts" needs no assumptions about how that environment looks.

Two consequences the first design got wrong, both fixed in 0013:

1. A trigger must still leave time to act. "The storage refuses writes right now" is a
   *diagnostic*, not a trigger: writing cannot help. The trigger is "free space is below 2× one
   checkpoint" — the last moment a write can still complete, because the old and the new copy have
   to coexist until the new one is sealed.
2. "Unknown" is not zero and "not applicable" is not unknown. A CPU-only job has no GPU condition
   (absent, inert); a GPU job that cannot read its GPU is blind (urgent). That distinction is what
   lets one algorithm serve every environment without naming any of them.

## 5. What we deleted, and why it must stay deleted

- `0006-the-decision-space`, `0007-v1-sells-the-pattern`, `0008-single-job-fields`,
  `0009-a-delta-needs-a-mechanism`, `0012-composition`, `algorithm/math.md`, `algorithm/worklist.md`:
  the interval optimisation, the delta axis, the "seconds are a decision" framing. They are
  internally consistent and externally wrong: they optimise a quantity the user cannot observe.
- From the code's vocabulary (not yet deleted, marked superseded): `MaxTax`, `TightestBound`,
  `Guarantee`, `OnConflict`, the pattern and its chain, `DeltaShare`, most of the reason codes.

We keep the machinery that made this conclusion possible, because it is still the right way to
prove things: the contract with unknown values, the trace replay, the recorded replay, the capability
matrix, the size ladder. Those are how the next claim will be tested too.

## 6. What survives from V1, unchanged

- The promise machinery: one writer per directory, expiry, atomic seal, read-back verification,
  repair of a broken copy, retention of *our own* copies.
- The channel: conditions arrive as optional `Evidence` entries, so the algorithm gains a
  vocabulary without gaining a contract field. The job-local facts (is there a checkpoint, is a
  write in flight, are the copies intact) stay typed.
- The discipline: a decision is a pure function of the snapshot, replayable, explainable, and the
  same inputs produce the same decision.

## 7. What we will not claim any more

- That we beat a well-tuned fixed interval by a percentage. We do not; we proved we cannot.
- That we predict failures. We do not; we observe conditions and act on them.
- That the write path is our product. It is a tool that makes a longer fallback safe.

## 8. The one number that still decides the product

**Warning lead time**: how long between the first observable condition and the machine actually
being lost. If most interruptions have minute-scale warning, the product is strong. If most have
none (power loss, hard faults), then "no fallback" is a genuine risk the user must accept, and the
product is "automatic, safe, explained" rather than "saves you money". Measure it before promising
anything.

## 9. How the discussion went, in one line each

- "Why is a fixed constant enough?" → because the feasible range is narrow; proved, then accepted.
- "Where is the money then?" → not in the interval; in the risk the user accepts and in how small
  the write can be.
- "If we cannot observe failures, is this meaningless?" → yes for prediction, no for observation.
- "So we should let the user be brave?" → yes, and we must then be the ones watching.
- "Can we not score it instead of boolean?" → a score is a model of severity; a boolean is a fact,
  and facts are what we can be trusted with.
- "What if the disk is already full?" → then it is too late; fire at 2× the checkpoint instead.
