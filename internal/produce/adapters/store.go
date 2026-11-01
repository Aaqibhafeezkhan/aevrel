package adapters

import (
	"os"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// lowSpace is `store.low-space`: free space is below 2× one checkpoint, so the next write may be the
// last that can complete.
//
// Two copies of one checkpoint must coexist until the new one is sealed and verified; below that
// line the window is closing. It is a trigger, not a diagnostic, because there is still time.
type lowSpace struct {
	every  time.Duration
	factor float64
	stat   func(dir string) (free, total int64, err error)
}

func (p lowSpace) with(c Config) lowSpace {
	return lowSpace{every: c.storeEvery(), factor: c.freeFactor(), stat: statfs}
}

func (p lowSpace) Name() string { return "store.low-space" }

func (p lowSpace) Every() time.Duration { return p.every }

func (p lowSpace) Cost() produce.Cost { return produce.Cheap }

func (p lowSpace) Read(env produce.Env) produce.Reading {
	free, _, err := p.stat(env.Dir)
	if err != nil {
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: err}
	}
	if env.CheckpointBytes <= 0 {
		// With no checkpoint, "2× one checkpoint" has no scale: the rule says nothing, and the
		// first write is the fallback's business. Reporting a number here would be inventing one.
		return produce.Reading{State: produce.Fresh, Value: false, At: env.Now}
	}
	need := int64(p.factor * float64(env.CheckpointBytes))
	return produce.Reading{State: produce.Fresh, Value: free < need, At: env.Now}
}

// blocked is `store.blocked`: the write target refuses right now. It is a diagnostic and never
// triggers a write — a write now would fail, so the honest answer is to say the window has closed.
type blocked struct {
	every time.Duration
	probe func(dir string) error
}

func (p blocked) with(c Config) blocked {
	return blocked{every: c.storeEvery(), probe: writable}
}

func (p blocked) Name() string { return "store.blocked" }

func (p blocked) Every() time.Duration { return p.every }

func (p blocked) Cost() produce.Cost { return produce.Cheap }

func (p blocked) Read(env produce.Env) produce.Reading {
	err := p.probe(env.Dir)
	if err == nil {
		return produce.Reading{State: produce.Fresh, Value: false, At: env.Now}
	}
	if os.IsNotExist(err) {
		// The directory is not there yet. That is not a refusal: it is a job that has not started.
		return produce.Reading{State: produce.Fresh, Value: false, At: env.Now}
	}
	return produce.Reading{State: produce.Fresh, Value: true, At: env.Now, Err: err}
}

// writable asks the target the only question that matters: can a checkpoint be created here?
//
// The probe writes into the checkpoint directory itself, not into .ckpt-governor. Probing the
// governor directory made this condition unreachable in its own scenario: when that directory is
// unusable the controller cannot start at all, so it could never report "storage is blocked". The
// volume that has to accept shards is the volume to ask about — and a read-only volume refuses this
// probe while leaving the controller's own state writable, which is exactly the failure it names.
func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

// LowSpaceNumbers reports what the storage rule compared, for the sentence  prints.
//
// The decision cannot carry these numbers and should not try: the policy sees booleans, and risk.go
// builds its reason with an empty Values map for exactly that reason. The producer is where the
// numbers live, so the producer answers when a person asks — same rule, same factor, no second
// opinion about what low means.
func LowSpaceNumbers(dir string, checkpointBytes int64, factor float64) (free, need int64, ok bool) {
	if checkpointBytes <= 0 || factor <= 0 {
		return 0, 0, false
	}
	free, _, err := statfs(dir)
	if err != nil {
		return 0, 0, false
	}
	return free, int64(factor * float64(checkpointBytes)), true
}
