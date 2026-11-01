# 0006 — The boundary: a capability is declared once, produced once, replayed once

Status: accepted. Date: 2026-10-31.

> **Partly historical.** Package names have moved since: `internal/signal` is `internal/observe`,
> `internal/producer` is `internal/produce`, `internal/trace` was removed with the interval algorithm,
> and `capabilities`/`factor` no longer exist. The rule this record states — one declaration, one
> producer, one replay — still holds.

## Context

The algorithm layer has become a registry: modules propose bounded effects over *capabilities*, and a
capability that is absent is `unknown` — it contributes nothing and is named in `Missing`, instead of
every module branching on every combination (decision 0010, algorithm repo). Adding a factor there is
one file.

On this side every capability costs seven places: a reader, a cost class, a cadence, an unknown
value, a staleness rule, a ledger slot, a status line — plus a test for each. One declaration there
is two hundred lines here, and the asymmetry grows with every factor they try.

`internal/simulate` has already been rewritten on the `policy` branch (≈4.4k lines). Two people
editing one package is how the previous project lost weeks.

## Decision

**1. Ownership.**

| Owner | Packages |
|---|---|
| algorithm | `internal/policy`, `internal/simulate`, `docs/algorithm/` |
| delivery | `internal/signal`, `internal/trace`, `internal/observe`, `internal/advise`, `internal/loop`, `internal/unit`, `cmd/*` |

Neither edits the other's packages. A change that must cross is a decision record, as before.

**2. One shared contract.** The trace event schema lives in `internal/trace`, and a capability is
declared once in `internal/signal` — including how it appears in a trace. Both sides import these;
neither owns the other's implementation.

**3. A capability costs one file.** Adding one is: a `signal.Spec` (reader, cost, cadence, unknown,
staleness, trace field) plus one registration plus one test. Nothing in `loop`, `advise` or `observe`
changes. That is the acceptance for the refactor, not a hope.

**4. Rehearsal is part of the declaration.** A capability declared with a trace field can be
generated and replayed by the algorithm side without asking delivery: `trace gen` and the replay read
the same declaration. Experimentation stops being a coordination event.

**5. One line owed by the replay reader.** A capability's event is a line with an event name the
reader has never seen. The reader must skip kinds it does not know instead of refusing the trace;
until it does, `trace gen --capability-events` stays opt-in, because a trace nobody can read is not a
rehearsal.

## Consequences

- The algorithm can try a factor by adding a spec and a trace field; the delivery side inherits the
  measurement, the cost accounting, the status line and the replay input from that one declaration.
- A capability that needs a *new kind of probe* (network, permissions, another namespace) still costs
  a new `Env`; that is a new category, paid once, not once per capability.
- A capability that needs a new *lifecycle* (sample only during a write) needs one more field on the
  spec (`When`). It is reserved, not implemented.
- The status view renders whatever structured fields the published file carries, so a new decision
  field (reasons, sensitivity) appears there without a code change on this side.
