# ckpt-observer

One process per cluster. It reads the platform and writes the world the controller reads. It makes
no decisions.

This is its own Go module on purpose: the dependency on `client-go` stays here, and the core binary
(`cmd/ckptctl`) keeps depending on nothing but the standard library, so it still runs on a laptop, on
Slurm, and on bare metal.

## Run it

```sh
go run . --world-dir=/var/lib/ckpt/world --cluster=a --every=5s
```

- `--world-dir` — where to write. Point `deployment.json`'s `world.dir` at the same path so the
  controller finds it.
- `--cluster` — the subdirectory name. Defaults to the kubeconfig context.
- `--units=labeled|all|none` — which pods get a unit record. `labeled` (the default) records only
  pods carrying `ckpt-governor.io/unit`; `all` is for trying it out on a cluster where nothing is
  labelled yet.
- `--once` — read once, write once, exit. Useful for a cron job or a test.

Credentials come from the in-cluster service account, then from the usual kubeconfig locations. The
process needs `get`/`list` on `nodes` and `pods` across all namespaces, and nothing else.

## What it writes

```
<world-dir>/<cluster>/summary.json          one file, node-sized
<world-dir>/<cluster>/units/<sha1>.json     one tiny file per unit
```

Both are written through a temporary file and renamed, so a reader never sees half a snapshot. The
schema is defined in `contrib/observer/world/world.go`; the controller reads it in
`internal/produce/adapters/world.go`.

## The three decisions inside

1. **Quantities are normalised here.** Kubernetes hands out `"24"` and `"16373440Ki"`; the summary
   carries milli-cores and bytes, because a conversion done by every consumer is a conversion done
   differently by every consumer.
2. **`requestsUnknown` is a real field.** If a node has pods and none of them declares a request, the
   sum is zero for a reason that has nothing to do with being idle. The observer is the only one who
   can tell the difference, so it writes it down and the condition refuses to call that node calm.
3. **Pending high-priority pods are counted cluster-wide.** The scheduler has not chosen a node yet;
   counting them per node would pretend it had.

## Why a LIST and not an informer

A LIST per interval costs one request per cluster per few seconds regardless of how many jobs run,
which is the whole point of the design. When a LIST starts taking longer than the interval, or the
cluster grows past a few thousand pods, the same `Fold` can be fed from an informer cache — the
folding code does not know where its lists came from.

## Test it without a cluster

`world.Fold` takes `[]corev1.Node` and `[]corev1.Pod`, so the tests build objects by hand:

```sh
go test ./...
```
