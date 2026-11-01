package main

import (
	"strings"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// The explanation must not invent numbers when there is nothing to compare: a directory with no
// checkpoint has no scale, and a sentence with two invented sizes is worse than a shorter one.
func TestEnrichLeavesTheReasonAloneWithoutAScale(t *testing.T) {
	dir := t.TempDir()
	got := enrich(dir, policy.ReasonEntry{Code: "store.low-space"})
	if len(got.Values) != 0 {
		t.Fatalf("numbers were invented: %v", got.Values)
	}
	if !strings.Contains(reasonSentence(got), "below the checkpoint scale") {
		t.Fatalf("sentence = %q", reasonSentence(got))
	}
}

// Other codes are left exactly as the plan wrote them: enrichment is for one condition that has a
// number worth quoting, not a hook for the CLI to rewrite the algorithm's explanation.
func TestEnrichIsNarrow(t *testing.T) {
	in := policy.ReasonEntry{Code: "job.ending", Evidence: []string{"Evidence[job.ending]"}}
	got := enrich(t.TempDir(), in)
	if got.Code != in.Code || len(got.Values) != 0 || len(got.Evidence) != 1 {
		t.Fatalf("enrich changed another reason: %+v", got)
	}
}
