package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// The stop, second half: the file the job writes on SIGTERM is a *trigger*. This is the acceptance line
// printed by the test that proves it, from the moment the file appears to the plan on disk.
func TestRiskShutdownTurnsAStopIntoASave(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	// What the job writes when it is told to stop.
	if err := os.WriteFile(filepath.Join(base, "stopping"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, &out); code != 0 {
		t.Fatalf("run exited %d:\n%s", code, out.String())
	}

	d, ok := advise.LastDecision(dir)
	if !ok {
		t.Fatal("no plan was published")
	}
	if d.Action != policy.ActionSave || d.Reason != policy.ReasonRisk {
		t.Fatalf("decision = %s/%s, want save/risk", d.Action, d.Reason)
	}
	if len(d.Reasons) == 0 || d.Reasons[0].Code != "risk.shutdown" {
		t.Fatalf("the decision does not name the condition: %+v", d.Reasons)
	}
	fmt.Println("risk.shutdown 1 after SIGTERM → save")
}
