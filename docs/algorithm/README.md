# The algorithm: measurements, not an interval search

Status: reset on 2026-11-01. The interval optimisation this folder used to describe is abandoned;
`docs/decisions/0014-why-the-risk-pivot.md` says why, and `docs/decisions/0013-risk-conditions.md`
is the design that replaced it. What remains here is the measuring apparatus, which is still the
right way to prove the next claim.

## What is live

| File | What it holds |
|---|---|
| `README.md` | this page: what survived, and what the tests are for |

The metric definitions, the trace format and the acceptance rules live with the code that
implements them: `internal/simulate` and `docs/experiments-matrix.md`.

The algorithm itself is one page: `docs/policy.md`.

## What was deleted, and why it must stay deleted

`math.md`, `worklist.md`, and records `0006-the-decision-space`, `0007`, `0008`, `0009`, `0012`
described an optimisation of the checkpoint interval against an assumed failure rate. They were
internally consistent and externally wrong: the quantity they optimise (expected lost work against
a tax budget) is one the user cannot observe, and the arithmetic showed the best a constant can be
is a few percent away from the best an oracle could do. Do not resurrect them; if a new claim needs
arithmetic, derive it again against the risk model.

## What still has to be proven

Only these, and each with a mechanical check:

1. **A condition fires exactly when it should.** A truth table per condition over `0`, `1`, absent
   and unknown; no condition may trigger a write when it is a diagnostic.
2. **Blindness is urgent.** A required condition that becomes unreadable produces a write, and its
   name appears in `Missing`.
3. **The algorithm stays environment-blind.** The same snapshot with all conditions absent behaves
   exactly like the same job with all conditions `0`; a CPU-shaped and a GPU-shaped snapshot with
   the same conditions decide the same.
4. **The old policy stays dead.** `pattern-v1` is deleted, not just unregistered; a decision that
   still carries `Interval`/`Tax` is a bug.
5. **The replay is the evidence.** The recorded replay over the generated families still runs, because
   the executor's behaviour (backoff, sealing, verification) is what the traces exercise.

## Order of work

All four steps are done: the risk core (`risk-v1`) with its catalogue, rule and truth tables; the
contract revision (0015) with `Bounds.NoUs` in three states; the producers, one condition at a
time, each with its own "unreadable after N"; and the deletion of the interval code.
What the execution plane added on top — attempts, the four gates, the window, the failure
budget, the queue, the supervisor — is `docs/design-v1.md` §12, with the line each
acceptance test prints.
