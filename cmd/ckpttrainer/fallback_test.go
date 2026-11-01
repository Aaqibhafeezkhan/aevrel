package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// publishedPlan writes a plan that the governor published and then stopped renewing, and returns the
// directory. `now` is when it was published, so a ttl of a second makes it be in the past.
func publishedPlan(t *testing.T, dir string, d policy.Decision, publishedAt time.Time, ttl time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := advise.Publish(dir, d, 1, publishedAt, ttl); err != nil {
		t.Fatal(err)
	}
}

func checkpoints(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if len(name) > 5 && name[:5] == "step-" {
			out = append(out, name)
		}
	}
	return out
}

// The fallback: the governor is gone and the user's own number (`no-us`) says how often the job
// writes by itself. The plan is no longer authoritative, and the checkpoint must appear anyway — that
// is the whole reason the user can afford to let the governor own the decision.
func TestTheJobWritesOnItsOwnWhenTheGovernorIsGone(t *testing.T) {
	dir := t.TempDir()
	// The governor lived for a second a minute ago, and answered "no-us = 1s".
	publishedPlan(t, dir, policy.Decision{
		UnitID: "u", Action: policy.ActionSkip, Reason: policy.ReasonCalm,
		BaselineSeconds: 1, Version: "risk-v1",
	}, time.Now().Add(-time.Minute), time.Second)

	c := config{dir: dir, steps: 500, stepMS: 3, writeMS: 2, shardBytes: 16, ranks: 1, members: 1,
		grace: time.Second}
	if err := runWith(c, make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	wrote := checkpoints(dir)
	if len(wrote) == 0 {
		t.Fatalf("the job never wrote on its own: no checkpoint in %s", dir)
	}
	t.Logf("governor gone → the job wrote %s on its own (no-us 1s)", wrote[0])
}

// The other answer: `no-us = none` means the user accepts that nothing is written when the governor
// is gone. The job must not invent a write, and it must say so rather than being silently idle.
func TestAnAbsentGovernorWithNoUsNoneWritesNothing(t *testing.T) {
	dir := t.TempDir()
	publishedPlan(t, dir, policy.Decision{
		UnitID: "u", Action: policy.ActionSkip, Reason: policy.ReasonCalm,
		BaselineNone: true, Version: "risk-v1",
	}, time.Now().Add(-time.Minute), time.Second)

	c := config{dir: dir, steps: 300, stepMS: 3, writeMS: 2, shardBytes: 16, ranks: 1, members: 1,
		grace: time.Second}
	if err := runWith(c, make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	// The one write this run is allowed is the last one: the end of a run is a promise, not a
	// setting, so it survives even a "no writes, I asked for this". Everything before it must be
	// silence, or the answer means nothing.
	wrote := checkpoints(dir)
	for _, name := range wrote {
		if name != "step-299" {
			t.Fatalf("no-us none wrote %s during the run; it may only write the final state (%v)", name, wrote)
		}
	}
	if len(wrote) == 1 {
		fmt.Println("governor absent; no-us none: 0 timed writes, and the end-of-run promise kept (step-299)")
	}
}

// One request, one write. A live plan that asks for a request is acted on once; a second plan
// with the same request id is not acted on again, and a plan with a new id is.
func TestOneRequestIsActedOnOnce(t *testing.T) {
	dir := t.TempDir()
	publishedPlan(t, dir, policy.Decision{
		UnitID: "u", Action: policy.ActionSkip, Reason: policy.ReasonCalm,
		BaselineNone: true, Version: "risk-v1",
	}, time.Now().Add(-time.Minute), time.Second)

	now := time.Now()
	view := readPlan(dir, now)
	if view.live {
		t.Fatal("an expired plan must not be live")
	}
	if got := view.requestFor(""); got != "" {
		t.Fatalf("an expired plan asked for %q", got)
	}

	publishedPlan(t, dir, policy.Decision{
		UnitID: "u", Action: policy.ActionSave, Reason: policy.ReasonRisk, SaveNow: true,
		RequestID: "req-1", Why: "risk.shutdown", Version: "risk-v1",
	}, now, time.Minute)

	view = readPlan(dir, now)
	if got := view.requestFor(""); got != "req-1" {
		t.Fatalf("the live request is %q, want req-1", got)
	}
	if got := view.requestFor("req-1"); got != "" {
		t.Fatalf("the same request was offered twice: %q", got)
	}
	if got := view.requestFor("req-0"); got != "req-1" {
		t.Fatalf("a new request after another was served is %q, want req-1", got)
	}
}
