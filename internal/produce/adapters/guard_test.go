package adapters

import (
	"errors"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

type slowProbe struct {
	delay time.Duration
	block chan struct{} // when non-nil, Read waits on it forever
	calls int
}

func (p *slowProbe) Name() string         { return "test.slow" }
func (p *slowProbe) Every() time.Duration { return time.Second }
func (p *slowProbe) Cost() produce.Cost   { return produce.Cheap }
func (p *slowProbe) Read(produce.Env) produce.Reading {
	p.calls++
	if p.block != nil {
		<-p.block
	}
	time.Sleep(p.delay)
	return produce.Reading{State: produce.Fresh, Value: true}
}

func TestAFastProbePassesThrough(t *testing.T) {
	g := &guard{inner: &slowProbe{}, timeout: 100 * time.Millisecond}
	r := g.Read(produce.Env{Now: t0})
	if r.State != produce.Fresh || !r.Value {
		t.Fatalf("fast probe = %+v", r)
	}
}

// A probe that cannot answer in time must not stall the round: the answer is unknown, with a reason.
func TestASlowProbeBecomesUnknownNotABlock(t *testing.T) {
	g := &guard{inner: &slowProbe{delay: 200 * time.Millisecond}, timeout: 10 * time.Millisecond}
	start := time.Now()
	r := g.Read(produce.Env{Now: t0})
	if r.State != produce.Unknown || !errors.Is(r.Err, ErrTimeout) {
		t.Fatalf("slow probe = %+v", r)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("the round waited %s for a stuck probe", elapsed)
	}
}

// One stranded goroutine per probe is acceptable; one per round is not. While the first read is
// still running, a second must not be started.
func TestAStuckProbeIsNotStartedTwice(t *testing.T) {
	block := make(chan struct{})
	inner := &slowProbe{block: block}
	g := &guard{inner: inner, timeout: 5 * time.Millisecond}

	if r := g.Read(produce.Env{Now: t0}); !errors.Is(r.Err, ErrTimeout) {
		t.Fatalf("first read = %+v", r)
	}
	if r := g.Read(produce.Env{Now: t0}); !errors.Is(r.Err, ErrBusy) {
		t.Fatalf("second read while stuck = %+v, want busy", r)
	}
	if inner.calls != 1 {
		t.Fatalf("the probe was entered %d times, want 1", inner.calls)
	}

	close(block) // let the stranded goroutine finish
	time.Sleep(20 * time.Millisecond)
	if r := g.Read(produce.Env{Now: t0}); r.State != produce.Fresh {
		t.Fatalf("after the probe returns = %+v, want a fresh reading", r)
	}
}

// The wrapper must not change what the probe says it is: a guard that renamed a condition would
// break the catalogue silently.
func TestTheGuardKeepsTheProbeIdentity(t *testing.T) {
	inner := &slowProbe{}
	g := &guard{inner: inner, timeout: time.Second}
	if g.Name() != inner.Name() || g.Every() != inner.Every() || g.Cost() != inner.Cost() {
		t.Fatal("the guard changed the probe's identity")
	}
}

// Every probe the build offers must be guarded: a single unguarded adapter is enough to stall a
// round in production.
func TestEveryAdapterIsGuarded(t *testing.T) {
	for _, p := range All(Config{}) {
		if _, ok := p.(*guard); !ok {
			t.Fatalf("%s is not guarded", p.Name())
		}
	}
}
