package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// writeStep is one write, and one round: the soak's clock. A write takes a round to become a seal
// (a shard appears first, then a manifest), which is what makes W measurable and the window's base a
// seal rather than a request.
const writeStep = 4 * time.Second

// timedProbe is a condition that is true only while the clock is inside a window, so the soak can put
// two exceptions at chosen hours instead of hoping the random world produces one.
type timedProbe struct {
	name  string
	fires func(now time.Time) bool
}

func (p timedProbe) Name() string { return p.name }
func (p timedProbe) Every() time.Duration {
	// One round: an announcement that lasts a round has to be read in that round, or the soak would
	// measure its own sampling laziness instead of the window.
	return writeStep
}
func (p timedProbe) Cost() produce.Cost { return produce.Cheap }
func (p timedProbe) Read(env produce.Env) produce.Reading {
	return produce.Reading{State: produce.Fresh, Value: p.fires(env.Now), At: env.Now}
}

// TestATwentyFourHourRunKeepsTheWindow is what the 24-hour soak has to keep true.
//
// A virtual day, one number (with-us = 30m), one write time (W = 4s), and two urgent conditions at
// known hours. The job is modelled faithfully: it reads the published plan, writes when the plan
// names a request, at most once per request, and its write takes two rounds.
//
// Three claims, and they are the product's:
//  1. nobody waits longer than the span (plus one write) between checkpoints — the window is a bound,
//     not a schedule that can drift;
//  2. an exception is a write the window would have paid for anyway: it moves the next window write
//     later instead of adding one, so the day costs what the span says;
//  3. the two exceptions are visibly different from the window writes in the report.
func TestATwentyFourHourRunKeepsTheWindow(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	now := start
	span := 30 * time.Minute

	// The two exceptions: an announcement at 04:00 and another at 13:00, each lasting one round.
	fires := func(at time.Time) func(time.Time) bool {
		return func(n time.Time) bool { return !n.Before(at) && n.Before(at.Add(writeStep)) }
	}
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsEvery, NoUsEvery: 10 * time.Minute, WithUs: span, MinCopies: 1},
		Window: span, Policy: policy.Risk{}, Now: func() time.Time { return now },
		Probes: []produce.Probe{
			timedProbe{name: "risk.preempt", fires: fires(start.Add(4 * time.Hour))},
			timedProbe{name: "host.gpu-fault", fires: fires(start.Add(13 * time.Hour))},
		},
		VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}

	var seals []time.Time
	served, pending, step := "", int64(0), int64(0)
	end := start.Add(24 * time.Hour)
	for now.Before(end) {
		// The job tells the clock which step it is on; without it every request in a day would carry
		// the same step, and "the same request twice" would be the gate's answer to the second one.
		step = int64(now.Sub(start) / writeStep)
		if err := os.WriteFile(filepath.Join(dir, advise.BaseDir, "step"), []byte(strconv.FormatInt(step, 10)), 0o644); err != nil {
			t.Fatal(err)
		}
		switch {
		case pending != 0:
			testutil.Sealed(t, dir, pending, 100)
			seals = append(seals, now)
			pending = 0
		default:
			if p, ok := advise.ReadPublished(dir); ok && p.Decision.RequestID != "" && p.Decision.RequestID != served {
				step++
				testutil.Unsealed(t, dir, step, 100)
				pending, served = step, p.Decision.RequestID
			}
		}
		now = now.Add(writeStep)
		if _, err := l.Round(); err != nil {
			t.Fatalf("round at %s: %v", now.Format("15:04"), err)
		}
	}

	// 1. The bound, measured seal to seal: that is what the user's number is about.
	maxStale := time.Duration(0)
	for i := 1; i < len(seals); i++ {
		if gap := seals[i].Sub(seals[i-1]); gap > maxStale {
			maxStale = gap
		}
	}
	limit := span + 2*writeStep // requested one W early, and the seal lands a round after that
	if maxStale > limit {
		t.Fatalf("the window was broken: the longest gap was %s, the bound is %s", maxStale, limit)
	}
	if len(seals) < 2 {
		t.Fatalf("only %d seals in a day: the window is not driving writes", len(seals))
	}

	// 2. What the day cost: 24h at 30m is 48 scheduled writes, and two announcements each pay for one.
	kinds := l.Ledger().Status.Kinds
	// Both announcements produced a write of their own. One may be classed `repair` rather than
	// `exception` if the copy it found was torn — the classification says *why*, the count says *that*.
	condition := kinds["exception"] + kinds["repair"]
	if condition != 2 {
		t.Fatalf("condition-triggered writes = %d, want the two announcements (kinds %v)", condition, kinds)
	}
	if n := len(seals); n < 46 || n > 50 {
		t.Fatalf("a 24h day at %s cost %d writes, want 48 ± 2 (kinds %v)", span, n, kinds)
	}
	if kinds["window"]+condition != len(seals) {
		t.Fatalf("the kinds do not add up to the seals: %v vs %d", kinds, len(seals))
	}

	// 3. The saving is printable, and it is the gap between the two numbers: what the user would have
	// written by himself (no-us = 10m) against what actually happened.
	noUs := int(24 * time.Hour / (10 * time.Minute))
	saved := noUs - len(seals)
	fmt.Printf("soak 24h: %d writes (%d window + %d condition), longest gap %s (window %s + %s) · "+
		"no-us 10m → %d scheduled · saved %d (%d%%)\n",
		len(seals), kinds["window"], condition, maxStale.Round(time.Second), span, writeStep,
		noUs, saved, saved*100/noUs)
}
