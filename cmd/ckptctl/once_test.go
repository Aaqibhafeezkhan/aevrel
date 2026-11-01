package main

import (
	"io"
	"strings"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// A one-shot process has no next round to throttle. The bug this pins: the second `run --once` in a
// directory reported `decided=false` because the ledger still held the first run's next-decision
// time, so a script that asked twice got yesterday's answer.
func TestASecondOneShotDecidesAgain(t *testing.T) {
	dir := t.TempDir()

	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, io.Discard); code != 0 {
		t.Fatalf("first run exited %d", code)
	}
	first, ok := advise.LastDecision(dir)
	if !ok || first.Action != policy.ActionSkip {
		t.Fatalf("first decision = %+v, want calm", first)
	}

	// The world changes the way it does between two invocations of a script.
	write(t, dir, "ending", "1")

	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, io.Discard); code != 0 {
		t.Fatalf("second run exited %d", code)
	}
	second, ok := advise.LastDecision(dir)
	if !ok {
		t.Fatal("the second run published no plan")
	}
	if second.Action != policy.ActionSave || second.Reason != policy.ReasonRisk {
		t.Fatalf("second decision = %s/%s, want save/risk: a one-shot must answer the question it was asked",
			second.Action, second.Reason)
	}
	if !strings.Contains(second.Why, "job.ending") {
		t.Fatalf("the second decision does not name the new condition: %q", second.Why)
	}
}
