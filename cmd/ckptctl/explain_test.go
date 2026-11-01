package main

import (
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// explain answers "why did my job just checkpoint" from the plan the job was given, not from a log
// that may have rotated.
func TestExplainReadsThePublishedPlan(t *testing.T) {
	dir := t.TempDir()
	d := policy.Decision{
		UnitID: "u", Generation: 1, Action: policy.ActionSave, Reason: policy.ReasonRisk,
		Why:  "job.ending: training is finishing or a stop was requested",
		Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1, Version: "risk-v1",
	}
	if _, err := advise.Publish(dir, d, 1, time.Now(), 0); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runExplain([]string{"--dir", dir}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	text := out.String()
	for _, want := range []string{"decision   save (risk)", "job.ending: training is finishing", "write      full, keep 1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("explain is missing %q:\n%s", want, text)
		}
	}
}

func TestExplainSaysWhenThereIsNoPlan(t *testing.T) {
	var out strings.Builder
	if code := runExplain([]string{"--dir", t.TempDir()}, &out); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "no plan") {
		t.Fatalf("output = %q", out.String())
	}
}
