package adapters

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// slowFactor is how many times the job's own normal speed counts as slow. Twice is the catalogue's
// number: half speed is the point where a person would ask why.
const slowFactor = 2

// slow is a condition that compares what the job is doing now with what it called normal.
//
// Both numbers come from the job, in the same units, written by the same process: the controller
// never has to decide what "fast" means, which is the whole reason these conditions can exist
// without the algorithm knowing anything about the machine.
type slow struct {
	condition string
	current   string // file holding the rate now
	baseline  string // file holding the rate the job calls normal
	factor    float64
	every     time.Duration
	read      func(dir, name string) (int64, bool, error)
}

func (p slow) Name() string { return p.condition }

func (p slow) Every() time.Duration { return p.every }

func (p slow) Cost() produce.Cost { return produce.Cheap }

func (p slow) Read(env produce.Env) produce.Reading {
	cur, okCur, err := p.read(env.Dir, p.current)
	if err != nil {
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: err}
	}
	base, okBase, err := p.read(env.Dir, p.baseline)
	if err != nil {
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: err}
	}
	if !okCur || !okBase || base <= 0 {
		// No calibration and no measurement: the condition has nothing to compare, which is an
		// environment without it rather than a blind one.
		return produce.Reading{State: produce.Absent, At: env.Now}
	}
	return produce.Reading{
		State: produce.Fresh,
		Value: float64(cur) >= p.factor*float64(base),
		At:    env.Now,
	}
}

// readMillis reads one number from .ckpt-governor. ok=false means the file is not there.
func readMillis(dir, name string) (int64, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ".ckpt-governor", name))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	v, convErr := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if convErr != nil {
		return 0, false, convErr
	}
	return v, true, nil
}

// stepSlow is `job.step-slow`: the training step takes twice as long as the job said it should.
type stepSlow struct{}

func (stepSlow) with(c Config) slow {
	return slow{condition: "job.step-slow", current: "step-rate", baseline: "step-baseline",
		factor: c.slowFactor(), every: 10 * time.Second, read: readMillis}
}

// storeSlow is `store.slow`: writing a checkpoint takes twice as long as the job measured.
type storeSlow struct{}

func (storeSlow) with(c Config) slow {
	return slow{condition: "store.slow", current: "write-rate", baseline: "write-baseline",
		factor: c.slowFactor(), every: 10 * time.Second, read: readMillis}
}
