# The configured user — the bound you already promised, proved

Status: design, first pass. Date: 2026-11-01. Companion: `docs/product-v1.md` (the zero-config
surface). This page only covers what is not in that page.

## 0. The split, in one sentence

The zero-config user is *"never configured it"*: the product is a default and a receipt. The
configured user is *"configured it carefully"*: **they already have a number, and they need it held
and proved, not replaced.** The two surfaces share one engine and touch different files: the product
surface owns `init`, the run's lines, the receipt and the container; this page owns the arithmetic
above the controller (`internal/bound`, `ckptctl price`) and the after-the-fact certificate
(`ckptctl audit`).

## 1. Two measured facts

Run `ckptctl table` on the families in this repository:

```
spread of best fixed periods across jobs: 12.0x
tiny / small-team-latency / small-team-shards   best fixed  5m
small-team / delta / delta-storm                best fixed 10m
steady / drift / storm / capacity / killed      best fixed 15m
big                                             best fixed  1h
```

One fleet-wide number is either 12× too expensive for the big jobs or 12× too lossy for the small
ones. That is the saving this design can prove, before any claim about being cleverer.

And one equation, exact because `W` is the measured p90 of sealed writes (`docs/write-cost.md`):

```
writes = run / bound                 tax = writes · W / run = W / bound
```

The tax does not depend on the run, which is why it is a number a person can carry between jobs.

## 2. What the configured user buys, ranked by size, with what proves it

| saving | size | prove it with |
|---|---|---|
| per-job bound instead of one fleet number | up to 12× (above) | `ckptctl price` per directory; the table's spread line |
| no double billing: an exception write serves the window | up to the exception count | `Kinds` split (window vs exception) in the run's receipt |
| the write-time subtraction (`W`) at an equal bound | `W/(P+W)`, ~5% at 10m/30s | `docs/experiments-matrix.md` D layer; the price sheet |
| what is *not* a saving | the refresh alone | stated in `docs/design-backstop.md` §0: "the middle line is not a saving" |

The last row is not a saving. A team that already runs a schedule has been offered intervals before,
and a claim that inflates 5% into 12× loses the room in one question.

## 3. The two questions, both exact

**"What does holding 15 minutes cost?"** `tax = W/15m` — 3.3% at a 30-second write, 16 writes in 4h.

**"We may spend at most 1% of training on writes — how often may we save?"** This is the same
equation solved the other way: `bound ≥ W/T`. With a 30-second write, 1% allows no tighter than
50 minutes. That sentence ends a meeting that would otherwise be about preferences, and it is
arithmetic rather than an opinion.

The left edge is one write: a bound shorter than `W` cannot hold, and the executor already refuses
it (`internal/exec`, "this bound cannot hold"). The price sheet must print the same refusal, and it
does (`internal/bound` prints `infeasible` instead of a price).

## 4. The number is measured from their own history

`internal/bound/observed.go` reads a directory and reports the **habit** (median seal-to-seal gap),
the **worst staleness the job has actually lived with** (the only bound its history proves), and the
measured write cost. Unsealed copies, copies without a known time, and two seals inside the same
second are all excluded: a period invented from a burst is exactly the kind of number this page
exists to avoid.

So the first question to a configured user is not "what bound do you want" but "what has your job
been living with", and the answer is read from their disk. The zero-config surface asks and prices;
this one measures and quotes against what already exists.

## 5. What a top control plane would add (and what we already have)

| practice | where it comes from | ours |
|---|---|---|
| fast safety loop independent of the slow optimiser | Borg/Omega | done: frozen rule + window; `internal/bound` is deletable without changing one write |
| declarative intent + epoch fencing | Kubernetes controllers | done: plan file with `epoch`, `requestId`, `generation` |
| SLO + error budget measured from the event stream | Prometheus | **next**: `ckptctl audit` — SLI is "seconds over the bound", from the seal history |
| plan/apply separation, a diff before the change | Terraform | **next**: `--dry-run` for the writes and deletions |
| guardrail, not command | K8s LimitRange, Envoy | done: gates, breaker, backoff; we never choose their number |
| irreversible operations explicit; the last copy is never deleted | ZFS, PVCs | done: rename→trash→sweep, `KeepCopies` floor; needs a one-page failure table for auditors |
| append-only events + idempotency keys | K8s events, Stripe | done: `events.jsonl` + `requestId`; **next**: the certificate reads them |

## 6. The deliverable that is not a feature: a certificate

For a compliance bound ("no more than 15 minutes may be lost"), a team cannot answer an auditor
today with anything but a hope. The certificate is one page, computed after the fact, read-only:

```
bound 15m · max observed staleness 14m52s · over-bound 0s · 16 writes · 16s (0.1%)
  each write: time · cause · cost          (from events.jsonl)
  deletions: 0 below keepCopies; the last copy was never removed
```

`over-bound > 0s` exits non-zero, so the claim is tested rather than asserted.

Landed: `internal/audit` (pure, times only) and `ckptctl audit --dir=<dir> [--bound=15m]`. The clock
is the ledger's whole-second clock, so copies are compared on it too; a seal stamped after the
audited moment is ignored rather than clamped, because clamping would invent coverage. An uncovered
start is its own line, not an average.

## 6b. Plan before apply

`ckptctl run --once --dry-run` is the second half of that discipline: it observes, decides and
prints what it *would* publish and retire, and the directory afterwards is byte-for-byte the one it
found — no plan, no ledger, no attempt, no trash, no `may-exit`, and no write probe (`throughput()`
answers "unknown" rather than spending 32 MiB). The preview and the act share one selection function
(`loop.retirableBelow`), so the preview cannot drift from the act; a test compares them on the same
directory (`internal/loop/dryrun_test.go`).

## 7. Non-goals

No new decision logic, no interval, no probability, no second policy. No dashboard. No operator or
CRD. No recommendation applied without a yes (A's rule applies here too). Nothing in this page runs
inside the loop: deleting `internal/bound` must leave every write exactly where it is, and a test
(`TestThePriceMatchesTheWindowTheExecutorKeeps`) is what keeps the two arithmetics honest.

## 8. Falsifiers — when this design is wrong

1. **Their number is already at the optimum** (`P ≈ L`, chosen with `W` subtracted): the saving is
   `W/L`, about 5%. Then the value is the certificate, not the count, and the copy must say so.
2. **Exceptions dominate** (the `killed` and `delta` families: 12 writes at 10% tax, all
   exception-driven): the window has nothing to save; the value is "no double billing" and
   "no loss during the announcement".
3. **`W` is unknown**: the sheet must print `unknown` and propose nothing
   (`TestAnUnknownWriteCostIsNotPriced`). A price built on a guessed write cost is the failure mode
   this page exists to prevent.
