package produce

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// flaky is a probe the test drives: readable, failing, or dead.
type flaky struct {
	name  string
	state State
	err   error
}

func (p *flaky) Name() string { return p.name }
func (p *flaky) Read(Env) Reading {
	return Reading{State: p.state, Value: p.state == Fresh, Err: p.err}
}
func (p *flaky) Every() time.Duration { return time.Second }
func (p *flaky) Cost() Cost           { return Cheap }

// TestTheFailureBudgetOutlastsAHiccup is E5's sampler half: one unreadable read is a hiccup, three
// make the condition blind, and a good read clears it. The carried answer keeps its own At, because
// a value taken two rounds ago is not a value taken now.
func TestTheFailureBudgetOutlastsAHiccup(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	p := &flaky{name: "risk.preempt", state: Fresh}
	s := NewSampler(p)

	r := s.Sample(Env{Now: now})["risk.preempt"]
	if r.State != Fresh || r.At != now {
		t.Fatalf("first reading = %+v, want fresh at the first sample", r)
	}
	first := now

	p.state, p.err = Unknown, errors.New("the glue file is gone")
	picks := []struct {
		wantState  State
		wantMisses int
	}{{Fresh, 1}, {Fresh, 2}, {Unknown, 3}}
	for i, want := range picks {
		now = now.Add(2 * time.Second) // past the cadence, so the probe is really read
		r = s.Sample(Env{Now: now})["risk.preempt"]
		if r.State != want.wantState || r.Misses != want.wantMisses {
			t.Fatalf("miss %d: reading = %+v (state %s), want %s with %d misses",
				i+1, r, r.State, want.wantState, want.wantMisses)
		}
		if want.wantState == Fresh && r.At != first {
			t.Fatalf("miss %d: the carried answer moved its At to %s, want %s", i+1, r.At, first)
		}
	}
	if len(s.Blind()) != 1 || s.Blind()[0] != "risk.preempt" {
		t.Fatalf("blind = %v, want one blind condition", s.Blind())
	}

	p.state, p.err = Fresh, nil
	now = now.Add(2 * time.Second)
	if r = s.Sample(Env{Now: now})["risk.preempt"]; r.State != Fresh || len(s.Blind()) != 0 {
		t.Fatalf("after one good read: %+v, blind %v, want fresh and nothing blind", r, s.Blind())
	}
	fmt.Printf("sampler: blind after 3 misses = 1, cleared by one good read\n")
}

// TestTheWatchdogSaysItIsDead: watch.broken is one: when a probe wraps ErrProbeBroken, and when
// every declared probe is blind. It is zero for a readable environment, including one where the
// condition is absent.
func TestTheWatchdogSaysItIsDead(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	dead := NewSampler(&flaky{name: "risk.preempt", state: Unknown, err: fmt.Errorf("%w: it panicked", ErrProbeBroken)})
	out := dead.Sample(Env{Now: now})
	if !dead.Broken() || !out[ConditionBroken].Value || out[ConditionBroken].State != Fresh {
		t.Fatalf("sentinel: broken=%v, watch.broken=%+v", dead.Broken(), out[ConditionBroken])
	}

	// Two probes, both unreadable, no sentinel: the sampler cannot see anything either way.
	blind := NewSampler(&flaky{name: "risk.preempt", state: Unknown}, &flaky{name: "store.low-space", state: Unknown}).BlindAfter(-1)
	if out := blind.Sample(Env{Now: now}); !blind.Broken() || !out[ConditionBroken].Value {
		t.Fatalf("all unknown: broken=%v, watch.broken=%+v", blind.Broken(), out[ConditionBroken])
	}

	// An absent condition is not blindness: a CPU job with no GPU probe is calm, not blind.
	absent := NewSampler(&flaky{name: "host.gpu-fault", state: Absent})
	if out := absent.Sample(Env{Now: now}); absent.Broken() || out[ConditionBroken].Value || len(absent.Blind()) != 0 {
		t.Fatalf("absent: broken=%v, watch.broken=%+v, blind %v", absent.Broken(), out[ConditionBroken], absent.Blind())
	}
	fmt.Printf("watch.broken: sentinel 1, all-unknown 1, absent 0\n")
}
