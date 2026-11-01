# The experiment matrix

Status: specification, second design. Date: 2026-11-01.

This page is the work order. It is organised around one decision: **"should we write" and "did the
write survive" are two different experiments**, and mixing them is what made the first version slow
and weak.

Rows carry a short key so a result line can name its case (`D` = a decision, `X` = an exactness
check, `F` = a case on a real cluster, `W` = the world projection). The key is only a label; the
second column of each row is the case, and that sentence — not the letter — is what the row proves.

## 0. The two questions, kept apart

| Question | Layer | What it costs | How many experiments |
|---|---|---|---|
| Does the system **decide** to write at the right moment? | **D** | milliseconds, no write happens | one per fault (8) |
| Is the write it performs **safe** (atomic, verified, bounded)? | **X** | seconds, real files | four, once |
| What does the whole thing look like end to end for a user? | **F** | seconds to minutes | two |

The first version ran every fault end to end. That is why `make exp` took tens of seconds and why each
experiment re-tested the write protocol — the same assertion, eight times, at the price of a real
write. It also hid the actual claim: **the product's value is the decision**, not the file.

The write protocol has to be proven once, thoroughly, under the four ways a write can be attacked.
Every other experiment may then assume it — the way a database's durability is proven once and every
query test assumes it.

## 1. Blind by construction: what the baseline's decision actually is

A team's checkpointing is a **timer**. At any instant `t`, its answer to "should I write now" is:

```
timer(t) = write   if  t mod period = 0
           no      otherwise
```

So when a fault arrives — almost surely *between* ticks — the baseline's decision is **no**, and its
next opportunity is `period − (t mod period)` away. With the common `period = 10m` that is up to ten
minutes during which the fault can kill the job with nothing saved.

This is not a straw man: it is the definition of a timer, and it is why the comparison is a
**scheduling** comparison, not a throughput one.

- we do **not** claim "we wrote more checkpoints than they did in the same second";
- we claim "at the instant the fault arrived, we had a reason to write and they did not, and their
  next reason is `Δ = period − (t mod period)` away".

`Δ` is printed as a number in every D experiment. It is not measured by waiting — waiting ten minutes
would prove something that is true by construction. It is computed from the baseline's own period.

## 2. Layer D — the decision experiments (milliseconds, no write)

Setup is a **state injection**, not a workload: write the world snapshot and glue files that describe
the fault, run **one round**, read the published plan. No trainer, no checkpoint written, nothing to
wait for.

```
d-<fault>:
  inject  the state that fault produces   (world summary / unit record / glue file)
  run     ckptctl run --once              (one round, ~30–80ms)
  assert  the plan says save, with the right reason and the right condition value
  report  decision=yes  rounds=1  latency=XXms  baseline_next_tick=Δ
```

| ID | Injected state | Must produce | Baseline at that instant |
|---|---|---|---|
| `D1` | unit record with `deletionTimestamp` set | `save / risk.preempt`, `risk.preempt = 1` | no write; next tick in `Δ` |
| `D2` | summary: node 92% requested, 2 pending high-priority | `save / risk.eviction-likely` | no write |
| `D3` | summary: node cordoned | `save / risk.eviction-likely` (cordoned) | no write |
| `D4` | free space below 2 × one checkpoint | `save / store.low-space`, before the volume fills | no write |
| `D5` | glue: `stopping` present | `save / risk.shutdown` | no write until a handler exists — and only `b3` has one |
| `D6` | write cost p90 jumps 10× | the decision changes within 2 rounds; `no-time` when it cannot fit | no write |
| `D7` | governance read error (`stats` fails) | `save / store.blocked`, no attempt started | no write |
| `D8` | `preemption.exists = false` with the same announcement as `D1` | **no write** (the condition is absent) | no write |

`D8` is in the table on purpose: a system that fires on a fault its deployment does not have is broken
in the other direction, and it belongs next to `D1` so nobody "fixes" it by accident.

Each D experiment prints one line:

```
d-preempt  decision=yes rounds=1 latency=41ms · baseline: next tick in 600s (period 10m)
```

**What D may not claim**: that anything was written, or that a write would fit. That is X's job.

## 3. Layer X — the execution experiments (seconds, real writes), four of them

These exist to prove one thing: **when we do write, the write is safe**. They run once, not per fault.

Both arms write, because a fair comparison needs two jobs that save. The governed arm is driven the
way the product drives it — the `with-us` window (decision 0017) — not by a timer we invented for the
experiment; the naive arm is a job with its own period and a copy script, which is what a team
actually ships.

| ID | Fault | Pass |
|---|---|---|
| `X1` | `kill -9` mid-write | the newest checkpoint always verifies; a torn directory is never the newest; the previous copy survives |
| `X2` | the disk fills during a write | **headroom is kept**: the governed arm stops with room for one more copy while the naive arm runs its volume to 0 KiB, and the newest copy still verifies. (Not "lost seconds": a compressed timer loses little by construction, so that number cannot carry this experiment.) |
| `X3` | SIGTERM during a run | one final write, `ending` then `may-exit`, the job leaves; a running job is never released |
| `X4` | controller `kill -9` mid-write, restarted | the job's own timer still seals; the restarted controller adopts the running attempt (1 attempt, 0 double writes) |

The baseline appears only where it is meaningful: `X1` and `X3` show that a timer-plus-copy protocol
leaves a torn copy and holds the user, while the sealed protocol does not. `X2` and `X4` are ours
alone — there is no baseline behaviour to compare, and the experiment says so instead of inventing one.

## 4. Layer F — end to end, twice

The money lines: fault → decision → write → survive → **lost seconds**, with the baseline's own
protocol in the other arm. They exist because a person buying this asks "how much work do I lose", and
that answer has to come from a run, not from the sum of two experiments.

| ID | Scenario | Reported |
|---|---|---|
| `F1` | preemption announced mid-run (kind, real `PriorityClass`) | `lost` per arm, `Δ` from the baseline's period, whether the plan was published before the kill |
| `F2` | user stops the job (`kubectl delete --grace-period=20`) | `intact` per arm — the parity row, since b3 has a SIGTERM handler too — and **seconds until each pod is gone**: measured 21.0s for the baseline against 5.3s for us, for the same final save |

## 5. The baselines, as a real team writes them

| ID | What it is | Where teams actually are |
|---|---|---|
| `b1` | save every N minutes, nothing else | most teams |
| `b2` | `b1` + keep the last two copies | teams that have hit a full disk |
| `b3` | `b2` + a SIGTERM handler that saves once | the careful teams, and the honest strong baseline |

The rig runs `b3` by default. Beating `b1` would prove nothing: nobody defends `b1`.

`b3` already covers **half of the parity claim**: it saves on a clean SIGTERM. We must match that
(`X3`) and then beat it where a signal never arrives (`D1`, `D2`, and the `kill -9` in `X1`).

## 6. Parity then superiority, stated as a table

The claim is two rows, not one.

| Capability | `b1` | `b2` | `b3` | ours | Evidence |
|---|---|---|---|---|---|
| saves on a timer | ✓ | ✓ | ✓ | ✓ (fallback) | `X1` |
| keeps the last N copies | ✗ | ✓ | ✓ | ✓ | `X2` |
| one final save on a clean SIGTERM | ✗ | ✗ | ✓ | ✓ | `X3` |
| writes before the disk is full | ✗ | ✗ | ✗ | ✓ | `D4` |
| writes when preemption is announced | ✗ | ✗ | ✗ | ✓ | `D1`, `F1` |
| writes when the node is filling up | ✗ | ✗ | ✗ | ✓ | `D2` |
| refuses a write that cannot finish | ✗ | ✗ | ✗ | ✓ | `D6` |
| survives being killed mid-write | ✗ | ✗ | ✗ | ✓ | `X1` |
| stays silent when it has no reason | n/a | n/a | n/a | ✓ | `D8` |

The first three rows are **parity**: whatever a team does today, we do too. Everything below them is
the product. A row we cannot fill is a claim we do not make.

## 6.5 What the X layer found

Two findings, both about the rig before they were about the product:

1. **The naive arm was not checkpointing at all.** The trainer only writes when a plan says so, and
   the naive arm has no controller — so the first version of `X1` compared "a governed job" against
   "a job that never saved", and reported `lost` for a baseline that had no checkpoints to lose. The
   fix is `--save-every`: the baseline's own timer, in the same writer, so the two arms differ in
   *who decides when*, not in *whether anybody saved*.
2. **A compressed timer cannot lose much.** With the baseline's period shrunk to 400 ms for the sake
   of speed, "lost seconds" is a few hundred milliseconds for both arms. That is why `X2` is scored on
   headroom and intactness instead: the honest question at a wall is "is there room for one more
   copy", not "how many seconds did each lose".

## 7. What each layer may not claim

| Layer | Cannot claim |
|---|---|
| D | that a write happened, or that it would have fitted |
| X | that the decision was right — X starts from a decision somebody else made |
| F | generality: two scenarios are anecdotes, and are reported as such |

## 8. Determinism and speed

- **D has no clock dependence**: one round, injected state, read the plan. Under 100 ms each; all
  eight in under a second.
- **X triggers on state, not on time**: "kill when a step directory exists without a manifest" is a
  20 ms poll, not a `sleep 2`.
- **No bare `wait`**, no grace period above 2 s in the rig, binaries cached under
  `experiments/rig/.bin/`.
- **N = 3 for X and F** (they involve the real clock and the OS); N = 1 for D, which is a pure
  function of its inputs.

## 9. Order of work

1. `D1`–`D8` — one afternoon, all millisecond-scale, and together they cover the whole "can we decide"
   claim.
2. `X1`, `X3` — the current end-to-end rig, re-cut into the X shape.
3. `X2`, `X4`.
4. `F1`, `F2`.
5. Cluster variants of `D1`–`D3` inside kind, with the `world` written by the real observer.

## 10. What would make this rig dishonest

- reporting a D experiment as if a write happened;
- measuring `Δ` by waiting ten minutes instead of reading the baseline's period;
- letting the baseline use an atomic rename;
- a `pass` whose condition never fired;
- deleting a `flaky` line instead of keeping it.
