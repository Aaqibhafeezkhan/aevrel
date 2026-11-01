package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// A reason is not a code, it is a number a person can act on. "store.low-space fired" tells you
// which rule; the two sizes tell you whether to delete something or raise the limit.
//
// Units are binary (GiB) and spelled out: 1.9e9 bytes is 1.8GiB, and a line that says 1.9GB next to
// a byte count that is not 1.9e9 is the kind of rounding that erodes trust in every other number.
func TestExplainTurnsAReasonIntoASentence(t *testing.T) {
	dir := t.TempDir()
	gib := float64(1 << 30)
	d := policy.Decision{
		UnitID: "u", Generation: 1, Action: policy.ActionSave, Reason: policy.ReasonRisk,
		Why:  "store.low-space: free space is below two checkpoints",
		Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1, Version: "risk-v1",
		Reasons: []policy.ReasonEntry{{
			Code:     "store.low-space",
			Values:   map[string]float64{"free": 1.9 * gib, "need": 2.0 * gib, "factor": 2},
			Evidence: []string{"Evidence[store.low-space]"},
		}},
	}
	if _, err := advise.Publish(dir, d, 1, time.Now(), 0); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runExplain([]string{"--dir", dir}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	want := "store.low-space fired; free 1.9GiB < need 2.0GiB (2 × 1.0GiB)"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("explain did not say why:\n%s", out.String())
	}
	fmt.Println(want)
}

// A condition this build has never heard of must still be printable: the plan is a contract, and a
// newer controller may write a reason this binary does not know.
func TestAnUnknownReasonStillPrints(t *testing.T) {
	got := reasonSentence(policy.ReasonEntry{
		Code: "future.condition", Values: map[string]float64{"x": 2},
	})
	if !strings.Contains(got, "future.condition fired") || !strings.Contains(got, "x=2") {
		t.Fatalf("unknown reason = %q", got)
	}
}

// The inference names its signals in the sentence, which is the guardrail "1 must be able to say
// what fired" reaching the person who has to decide whether to trust it.
func TestTheInferenceNamesItsSignals(t *testing.T) {
	got := reasonSentence(policy.ReasonEntry{
		Code: "risk.eviction-likely", Evidence: []string{"allocatable 92%"},
	})
	if !strings.Contains(got, "allocatable 92%") {
		t.Fatalf("the sentence hides the signal: %q", got)
	}
}
