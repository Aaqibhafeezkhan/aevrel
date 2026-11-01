package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// A slow condition is a diagnostic. It must show up in the status as a 1, and it must **not**
// by itself ask for a write — a job that is slow is not a job that is about to lose work.
//
// The line is derived from the two numbers the job wrote, not typed in by hand: if the ratio or the
// threshold changes, the line changes with it.
func TestASlowDiagnosticNeverAsksForAWrite(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	const baseline, rate = 1000, 3200
	for name, v := range map[string]int{"write-baseline": baseline, "write-rate": rate} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(strconv.Itoa(v)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, io.Discard); code != 0 {
		t.Fatalf("run exited %d", code)
	}

	// The condition itself fires.
	l, err := advise.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Status.Conditions["store.slow"].Value; got != "1" {
		t.Fatalf("store.slow = %q, want 1 (rate %d vs baseline %d)", got, rate, baseline)
	}

	// The decision does not.
	d, ok := advise.LastDecision(dir)
	if !ok {
		t.Fatal("no plan was published")
	}
	if d.Action == policy.ActionSave {
		t.Fatalf("a diagnostic turned into a write request: %s/%s (%s)", d.Action, d.Reason, d.Why)
	}

	fmt.Printf("store.slow 1 (%.1f× baseline), no write\n", float64(rate)/float64(baseline))
}
