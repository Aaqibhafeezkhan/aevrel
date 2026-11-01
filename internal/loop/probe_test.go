package loop

import (
	"errors"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// fakeProbe is a condition that answers whatever the test says, at a cadence the test chooses.
type fakeProbe struct {
	name  string
	every time.Duration
	state produce.State
	value bool
	calls int
}

func (p *fakeProbe) Name() string         { return p.name }
func (p *fakeProbe) Every() time.Duration { return p.every }
func (p *fakeProbe) Cost() produce.Cost   { return produce.Cheap }
func (p *fakeProbe) Read(produce.Env) produce.Reading {
	p.calls++
	if p.state == produce.Unknown {
		return produce.Reading{State: produce.Unknown, Err: errors.New("unreadable")}
	}
	return produce.Reading{State: p.state, Value: p.value}
}

// A fresh condition reaches the snapshot as a boolean, and a probe is only asked when its cadence
// says so: watching must be cheaper than the thing it watches.
func TestAConditionReachesTheSnapshotAtItsOwnCadence(t *testing.T) {
	dir := t.TempDir()
	probe := &fakeProbe{name: "store.low-space", every: 30 * time.Second, state: produce.Fresh, value: true}
	now := t0
	l, err := Open(Config{
		Dir: dir, UnitID: "u", Bounds: bounds(), Policy: policy.Risk{},
		Now: func() time.Time { return now }, Probes: []produce.Probe{probe},
		VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}

	st1, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	o, ok := st1.Evidence["store.low-space"]
	if !ok || o.Kind != policy.Boolean || !o.Bool {
		t.Fatalf("evidence = %+v, want a true boolean", o)
	}
	if probe.calls != 1 {
		t.Fatalf("reads = %d, want 1", probe.calls)
	}

	now = now.Add(time.Second)
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 1 {
		t.Fatalf("the probe was read again before its cadence: %d", probe.calls)
	}
}

// Absent and unknown must not collapse into each other. A CPU job has no GPU condition (no key);
// a GPU job that cannot read it is blind (the key is there, and it says unknown).
func TestAbsentIsNotTheSameAsUnknown(t *testing.T) {
	dir := t.TempDir()
	absent := &fakeProbe{name: "host.gpu-fault", every: time.Second, state: produce.Absent}
	unknown := &fakeProbe{name: "risk.preempt", every: time.Second, state: produce.Unknown}
	now := t0
	l, err := Open(Config{
		Dir: dir, UnitID: "u", Bounds: bounds(), Policy: policy.Risk{},
		Now: func() time.Time { return now }, Probes: []produce.Probe{absent, unknown},
		VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Evidence["host.gpu-fault"]; ok {
		t.Fatal("an absent condition must not appear in Evidence")
	}
	o, ok := st.Evidence["risk.preempt"]
	if !ok {
		t.Fatal("an unreadable condition must appear in Evidence")
	}
	if o.Kind != policy.Unknown {
		t.Fatalf("risk.preempt kind = %v, want unknown", o.Kind)
	}
}

// With nothing reported and no fallback, the answer is calm: that is the product's promise, and a
// delivery change that breaks it must fail here first.
func TestNothingReportedMeansNoWrite(t *testing.T) {
	dir := t.TempDir()
	now := t0
	l, err := Open(Config{
		Dir: dir, UnitID: "u", Bounds: bounds(), Policy: policy.Risk{},
		Now: func() time.Time { return now }, VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Action != policy.ActionSkip {
		t.Fatalf("action = %s, want skip", st.Action)
	}
}
