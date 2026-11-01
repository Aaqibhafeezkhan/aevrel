package loop

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// The report answers the three questions in the order a person asks them, and the two numbers are
// printed as evidence. The window write is the one time-based write the product has, so it is the
// cheapest way to produce a receipt that is not an exception.
func TestTheReportPrintsTheWindowAndTheSaving(t *testing.T) {
	dir := t.TempDir()
	now := t0
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsEvery, NoUsEvery: 10 * time.Minute, WithUs: 30 * time.Minute, MinCopies: 1},
		Window: 30 * time.Minute,
		Policy: calm{}, Now: func() time.Time { return now }, VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}

	// The window closes: no condition fired, and the span is the only reason to write.
	now = now.Add(31 * time.Minute)
	if st, err := l.Round(); err != nil || st.Attempts.Running != 1 {
		t.Fatalf("window round = %+v, %v; want one running request", st.Attempts, err)
	}
	// The job writes, and the seal is what the receipt is about.
	testutil.Sealed(t, dir, 1, 100)
	now = now.Add(time.Minute)
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}

	status := l.Ledger().Status
	if got := status.Kinds["window"]; got != 1 {
		t.Fatalf("window writes = %d, want 1 (kinds %v)", got, status.Kinds)
	}
	if status.LastReceipt == nil || status.LastReceipt.Kind != "window" {
		t.Fatalf("receipt = %+v, want a window receipt", status.LastReceipt)
	}

	var b strings.Builder
	advise.Render(&b, status, now, 0)
	out := b.String()
	for _, want := range []string{
		"window          30m0s · due",
		"receipt         stale 32m0s → 0s, cost 0s (window)",
		"writes          window 1 · exception 0 · final 0 · repair 0",
		"saving          no-us 10m0s → 3 · with-us 30m0s → 1 · wrote 1 (1 window + 0 exception) · saved 2 (67%)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the view is missing %q:\n%s", want, out)
		}
	}
	fmt.Println(out)
}

// A run whose user would not have written at all: the saving is not a saving, and the view says so
// instead of inventing a number.
func TestSavingWithNoUsNoneIsStatedAsCoverage(t *testing.T) {
	dir := t.TempDir()
	now := t0
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, WithUs: time.Minute, MinCopies: 1},
		Window: time.Minute,
		Policy: calm{}, Now: func() time.Time { return now }, VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	// The job answers the window request; the receipt is about a sealed write, not a request.
	testutil.Sealed(t, dir, 1, 100)
	now = now.Add(time.Minute)
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	advise.Render(&b, l.Ledger().Status, now, 0)
	if !strings.Contains(b.String(), "saving          no-us none: you would not have written; we wrote 1") {
		t.Fatalf("the view invents a saving:\n%s", b.String())
	}
}
