package adapters

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// ErrTimeout and ErrBusy are the two ways a probe can fail without failing the round.
var (
	ErrTimeout = errors.New("probe timed out")
	ErrBusy    = errors.New("previous probe read is still running")
)

// defaultProbeTimeout is how long one probe may take. It is the kubelet's default for the same
// reason: a probe that cannot answer in a second is not slow, it is stuck, and the round must not
// wait for it.
const defaultProbeTimeout = time.Second

// guard gives a probe a deadline and a "one at a time" rule.
//
// Go cannot cancel a blocked syscall, so a stuck probe leaks one goroutine. What the guard prevents
// is the unbounded case: a probe that hangs is not started again until its previous call returns, so
// the process accumulates at most one stranded goroutine per probe, not one per round.
type guard struct {
	inner   produce.Probe
	timeout time.Duration
	busy    atomic.Bool
}

func guardAll(probes []produce.Probe, timeout time.Duration) []produce.Probe {
	out := make([]produce.Probe, 0, len(probes))
	for _, p := range probes {
		out = append(out, &guard{inner: p, timeout: timeout})
	}
	return out
}

func (g *guard) Name() string         { return g.inner.Name() }
func (g *guard) Every() time.Duration { return g.inner.Every() }
func (g *guard) Cost() produce.Cost   { return g.inner.Cost() }

func (g *guard) Read(env produce.Env) produce.Reading {
	if !g.busy.CompareAndSwap(false, true) {
		// The previous read has not returned. Reporting unknown is honest: we could not take a
		// fresh reading, and pretending the old one is current would be a measurement that did not
		// happen.
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: ErrBusy}
	}
	done := make(chan produce.Reading, 1)
	go func() {
		defer g.busy.Store(false)
		done <- g.inner.Read(env)
	}()

	timer := time.NewTimer(g.timeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r
	case <-timer.C:
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: ErrTimeout}
	}
}
