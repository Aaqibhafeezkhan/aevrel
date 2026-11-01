# How long one write takes

Every decision that involves a deadline needs this number: "can a write finish before the job is
killed?" is arithmetic on it, and arithmetic on a guess is worse than saying "I do not know".

## Where the number comes from, in order of authority

1. **Measured** — after two sealed writes, the p90 of the durations we have actually seen. This is
   the only number that ever enters `measured`, and the only one used for a deadline.
2. **Declared by the job** — `expected-bytes` in `.ckpt-governor/`: the job knows its own parameter
   count. It is an estimate and is printed as `(estimated)`.
3. **One bounded probe** — at start, if neither exists: write 32 MiB into `.ckpt-governor`, fsync,
   remove, measure. Once per process, counted as overhead.
4. **Nothing** — and then the controller says so. It does not invent a duration to look decisive.

## What the numbers mean

- A write's duration is only the duration of a **finished** write. `sealed` and `broken` carry a
  cost; `failed`, `abandoned` and `refused` carry none, because they measured the failure, not the
  work.
- Estimates never enter `measured`. Mixing them would let a guess become the p90 by repetition.
- A stop is a fit test: the job reports `seconds-left` (see [glue.md](glue.md)), and the executor
  compares it with the write it is about to ask for. If it does not fit, the answer is `no-time`
  and a reported loss — not a write that will be killed halfway.

## What you can see

```
ckptctl glue --dir=/path/to/checkpoint
glue: expected=1.2GiB stopRequested=1 mayExit=1 seconds-left=27
```

`expected` is the job's own estimate until the first seal; after that the measured p90 is what the
decision uses, and the estimate stays labelled as one.
