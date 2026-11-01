# Backlog

Things deliberately not in the current work, with the reason and what would unblock them. This is a
list, not a specification: nothing here is promised, and nothing here may contradict `design.md`.

| Item | Why not now | Unblocked by |
|---|---|---|
| **Informer / member discovery** | The delivery layer should not care whether a job is one pod or fifty: membership is an input, not a shape. Today members come from a file the job writes; a cluster adapter would watch pods instead. | a cluster, or a jobs API to watch |
| Multi-job scheduling: quotas, fairness, DRF | The algorithm is single-job by design (its own decision 0010). Delivery could do volume-level fairness today, but there is nothing to be fair between. | a second unit on one volume |
| Preemption | It is an input the algorithm consumes, not a delivery decision. | the algorithm's preemption axis |
| CRD / operator packaging | Single-job deployments need a binary, not a custom resource. | a user asking to install it as an operator |
| Metrics export / dashboards | `design.md` says the product must not be a monitoring system. The status view and the event log are the answers. | a decision reversing that |
| **`store.corrupt` as an evidence condition** | The policy already repairs a broken copy from the typed `Copies` fact (`risk.go`'s safety branch, before the catalogue). A second path through `Evidence` would say the same thing twice, and a probe would have to read checkpoint content every cadence to produce it. | the loop's read-back verify, which sets `Copies[].State = broken` — that is where corruption is detected, and it is already wired |
| **Copy identity (decision 0005)** | A copy has no location in the contract, so two copies of one step are unrepresentable. | accepting 0005 |
| Advice-latency factor | The policy has no input for the job's read latency; the measurement exists (`seenAt − adviceAt`), the compensation does not. | the algorithm adding the factor |
| Delta and asynchronous writes | The decision carries a `kind` (full or delta), and the simulator prices both, but no producer writes deltas yet and the contract carries no field to describe a partial copy. | a writer that can produce a genuine delta, and the field it needs reported through `Evidence` |
| Two-point calibration of the write model | The model's overhead is one number today; separating the per-write overhead from the bytes needs two writes of different sizes, once per deployment. | an operator willing to spend two writes |
| Multi-cluster / federation | One controller governs one directory. Two clusters means two controllers and no shared state today. | a reason to federate |
| Partitioned snapshots (`partsSchema`) | The layout has one manifest per step. Partitioning is cheaper on the write side. | a trainer that spends a lot of time in a single file |
| **A CLI entry for the supervisor** | `internal/supervisor` and `internal/queue` exist and are tested, but no `ckptctl` command reaches them: `run` drives one directory. Multi-unit is a product decision (a members file, a flag), so it stays here rather than guessed. | a user with more than one unit, and a command shape |
| **`unit.Offset` is unused** | The deterministic per-unit delay exists, but nothing staggers starts: the volume gate limits concurrency instead, and jitter belongs to the policy (0003). | a reason to delay one unit's first request after a restart |
