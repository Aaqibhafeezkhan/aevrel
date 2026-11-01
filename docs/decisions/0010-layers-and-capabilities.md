# 0010 — Layers, capabilities, structured explanations

> **Partly superseded (2026-11-01).** The structure below — contract / core / factors / one
> composition / registry, capabilities that can only deny, a closed reason vocabulary, size-free
> ratios, and divergence reported rather than absorbed — still holds and is what the risk core is
> built in. What is dead is its *content*: "core = the period from `L`, `τ` and `W`", the delta/async
> modules as scheduling, and the worklist that implemented them. See 0013 and 0014.

Status: accepted for the algorithm layer. Date: 2026-11-01. This is the structural rule that
decides how the next algorithms are added, and it constrains both layers: the contract types stay
frozen, and the delivery layer only grows optional measurements.

## Context

The request that produced this: the algorithm has to work when the user has *some* of the
measurements and capabilities, not all of them; the next algorithms (multi-job, storage levels,
training-phase weighting) must be added without rewriting the policy or the delivery layer; the
implementation layer needs to show a user *why* this decision and *what would change it*; and the
behaviour must be consistent across model sizes and must survive the difference between a simulated
world and a real one.

## 1. Layers

```
contract      Bounds, Snapshot, Decision          frozen; additions only with a decision record
core          first checkpoint, feasibility,     never optional; the promise lives here
              period from L, tau, measured W
modules       content, shards, async, delta,     each optional, each bounded, each declares
              later: phase weight, peer pressure what it needs
composition   one function, the rules in design.md §"Composition rules"
registry      name -> policy, version in every decision
```

A module is not a second policy: it returns one `FactorResult` (a scale in `(0,1]`, a constraint
interval, a forced action, or `unknown`). The core applies it; a module never decides. That is why
adding one cannot contradict another.

The code and `design.md` §Composition call this object a **factor**; "module" in this record is the
same thing. The order that turns the diagram above into tested code is in
`docs/decisions/0013-risk-conditions.md` (what a factor may decide) and 0014 (why the content
changed).

## 2. Capabilities: missing measurements are the normal case

`Snapshot` gains one field:

| Field | Meaning |
|---|---|
| `Capabilities Capabilities` | what the writer, the storage and the job can actually do |

`Capabilities` is a struct of booleans and small numbers, all defaulting to "cannot":
`WeightsOnly`, `Delta`, `Async`, `Levels`, `MaxStreams`, `MemoryBytes`. Unknown is the zero value.

Each module declares `Requires() []string` over these names. The composition function asks the
capability set once: a module whose requirement is absent is `unknown`, contributes nothing, and its
absence is written into `Decision.Missing` in the same vocabulary. So the answer to "some users have
it, some don't" is not a branch per combination — it is a filter over modules, and the output says
which filters fired.

## 3. Structured explanation, not prose

`Decision` gains:

| Field | Meaning |
|---|---|
| `Reasons []ReasonEntry` | machine-readable cause chain: `{Code, Values map[string]float64, Evidence []string}`, where `Evidence` names the snapshot keys or evidence keys used |
| `Sensitivity []SensitivityEntry` | what would change the answer: `{Field, Threshold, Direction, WouldBe}` — "if the write cost were below 9.0s the period would be 45s" |
| `Missing []string` | already there; the measurements that were absent and changed the answer |

`Why` stays as one sentence for a person. Nothing parses it. The three structured fields are
deterministic functions of the snapshot, so a trace can replay them and a UI can render "why / what
next" without inventing numbers.

## 4. Consistency across model sizes

Every quantity in the arithmetic is a ratio: `W/L`, `tau`, `alpha`, `overhead/L`. Nothing depends on
the absolute size of a model, and the only size-dependent number is `W`, which enters through
`W/L`. So the policy's *decisions* are scale-free by construction, and a model ten times larger
differs only in the same way a slower disk does.

The test that keeps this true: a size ladder (×1, ×10, ×100 in bytes, with the mount and the bounds
held constant) must produce the same qualitative decision (action, kind, whether a module fired),
with the same ratios. A test that fails on a ladder is a test that found an absolute number hiding
in the policy.

## 5. Real world versus the model

Two rules, both already load-bearing:

- **A measured number always wins over a modelled one.** The model is used only where nothing can be
  measured; the moment a sample exists, the plan is recomputed from it.
- **Divergence is reported, not absorbed.** When the realized tax exceeds the plan by more than a
  band, or the realized write cost exceeds the planned one, the decision says so (`Reasons` gets
  `plan-missed`, the plan is recomputed conservatively). A policy that silently keeps the old plan
  after reality disagreed is a policy that will break the promise quietly.

## 6. How the next algorithm is added

1. New measurement needed? One optional `Snapshot` field (or one `Evidence` key), unknown by default.
2. Otherwise only a new module file: `Requires`, `Compute`, one test, one registry entry.
3. The contract, the core, and every other module are untouched.
4. The delivery layer changes only by filling the new measurement when it can; if it cannot, the
   module is `unknown` and the behaviour is today's.

Multi-job is exactly this: peer pressure arrives as an evidence key, a module turns it into a
constraint (shared write budget), and nothing else moves. Storage levels are the same with a
`Capabilities.Levels` flag and a level choice inside the content module.

## Falsifiers

- If adding a module requires touching the core or the contract types, the layering is wrong.
- If two modules can produce contradictory answers with no composition rule covering them, the
  layering is wrong.
- If a size ladder changes a qualitative decision, an absolute number leaked into the policy.
