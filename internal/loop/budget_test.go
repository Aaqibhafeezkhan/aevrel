package loop

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// never is a probe that says no: the budget test is about what a round costs, not about which
// condition fired.
type never struct{ name string }

func (p never) Name() string         { return p.name }
func (p never) Every() time.Duration { return 5 * time.Second }
func (p never) Cost() produce.Cost   { return produce.Cheap }
func (p never) Read(env produce.Env) produce.Reading {
	return produce.Reading{State: produce.Fresh, At: env.Now}
}

// TestARoundCostsWhatItShould is the round-overhead acceptance.
//
// The cost of watching is part of the product: a round on ten copies stays under a millisecond on
// a warm local disk, a probe is read at most once per cadence, a plan is published at most once per
// round, and a publish costs exactly the two syncs the publisher performs. Numbers that are printed
// but never asserted are numbers that drift.
func TestARoundCostsWhatItShould(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for step := int64(1); step <= 10; step++ {
		testutil.Sealed(t, dir, step, 100)
	}
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		// Ten copies kept, so retention has nothing to do and the round measures only watching.
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 10},
		Policy: policy.Risk{}, Now: func() time.Time { return now },
		Probes:      []produce.Probe{never{"store.low-space"}, never{"host.cpu-throttled"}},
		VerifyEvery: -1, // read-back is measured by its own test
	})
	if err != nil {
		t.Fatal(err)
	}

	const rounds = 5
	var worst time.Duration
	publishes, fsyncs := 0, 0
	for i := 0; i < rounds; i++ {
		now = now.Add(5 * time.Second)
		st, err := l.Round()
		if err != nil {
			t.Fatal(err)
		}
		if d := time.Duration(st.Overhead.ScanNS); d > worst {
			worst = d
		}
		if st.Published {
			publishes++
			fsyncs += st.Overhead.Fsyncs
		}
		if len(st.Deleted) > 0 {
			t.Fatalf("round %d deleted copies with retention keeping them", i+1)
		}
	}

	reads := l.Ledger().Status.Overhead.Signals
	for name, n := range reads {
		if n > rounds {
			t.Fatalf("%s was read %d times in %d rounds, want at most once per round", name, n, rounds)
		}
		if n < 1 {
			t.Fatalf("%s was never read in %d rounds", name, rounds)
		}
	}
	if publishes > rounds || fsyncs != 2*publishes {
		t.Fatalf("publishes=%d fsyncs=%d, want at most one plan per round and two syncs per publish", publishes, fsyncs)
	}
	fmt.Printf("budget: round %s, reads %d/%d once per cadence, publish %d/%d, fsync %d\n",
		worst, len(reads), len(reads), publishes, rounds, fsyncs)

	// A millisecond on a warm local disk. A shared CI runner is not a benchmark: there the number
	// only has to catch a change of magnitude, so it gets a wider tripwire. The structural
	// assertions above hold on every machine.
	limit := time.Millisecond
	if os.Getenv("CI") != "" {
		limit = 50 * time.Millisecond
	}
	if worst >= limit {
		t.Fatalf("the worst round took %s on ten copies, want under %s", worst, limit)
	}
}
