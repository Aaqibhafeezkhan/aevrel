package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// glueProbe reads one file: the file the job writes for the controller. Deleting it makes the
// condition unreadable, which is exactly the failure the debounce exists for.
type glueProbe struct{ path string }

func (p glueProbe) Name() string         { return "risk.preempt" }
func (p glueProbe) Every() time.Duration { return time.Second }
func (p glueProbe) Cost() produce.Cost   { return produce.Cheap }

func (p glueProbe) Read(env produce.Env) produce.Reading {
	if _, err := os.Stat(p.path); err != nil {
		return produce.Reading{State: produce.Unknown, Err: err}
	}
	return produce.Reading{State: produce.Fresh, Value: false, At: env.Now}
}

// TestABlindWatchdogSaysSoAndClears is E5's acceptance: the glue file disappears, the reading is
// carried for two rounds, the third makes the condition blind and the policy treats that as urgent
// — the same answer an unreadable required condition always gets. One good read clears it.
func TestABlindWatchdogSaysSoAndClears(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	glue := filepath.Join(dir, "glue")
	if err := os.WriteFile(glue, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Policy: policy.Risk{}, Now: func() time.Time { return now },
		Probes: []produce.Probe{glueProbe{glue}}, BlindAfter: 3,
		VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Round 1: readable, calm (the probe answers 0).
	st, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Blind != 0 || st.Conditions["risk.preempt"].Value != "0" {
		t.Fatalf("round 1: blind %d, conditions %v", st.Blind, st.Conditions)
	}

	// The glue file disappears: two rounds of hiccup, the third without eyes.
	if err := os.Remove(glue); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 3; i++ {
		now = now.Add(2 * time.Second)
		if st, err = l.Round(); err != nil {
			t.Fatal(err)
		}
		if st.Blind != 0 || st.Conditions["risk.preempt"].Value != "0" {
			t.Fatalf("round %d inside the failure budget: blind %d, conditions %v", i, st.Blind, st.Conditions)
		}
	}
	now = now.Add(2 * time.Second)
	if st, err = l.Round(); err != nil {
		t.Fatal(err)
	}
	if c := st.Conditions["risk.preempt"]; st.Blind != 1 || st.Conditions["watch.broken"].Value != "1" || c.Value != "unknown" || c.Reason == "" {
		t.Fatalf("round 4: blind %d, watch.broken %v, conditions %v", st.Blind, st.Conditions["watch.broken"], st.Conditions)
	}
	if st.Action != policy.ActionSave {
		t.Fatalf("round 4: action %q, want a save: a required condition that cannot be read is urgent", st.Action)
	}
	fmt.Printf("round 4: blind=%d watch.broken=%v action=%s\n", st.Blind, st.Conditions["watch.broken"], st.Action)

	// One good read is enough to be unblind again.
	if err := os.WriteFile(glue, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if st, err = l.Round(); err != nil {
		t.Fatal(err)
	}
	if st.Blind != 0 || st.Conditions["watch.broken"].Value != "0" || st.Conditions["risk.preempt"].Value != "0" {
		t.Fatalf("round 5: blind %d, watch.broken %v, conditions %v", st.Blind, st.Conditions["watch.broken"], st.Conditions)
	}
	fmt.Printf("round 5: blind=%d watch.broken=%v action=%s\n", st.Blind, st.Conditions["watch.broken"], st.Action)
}
