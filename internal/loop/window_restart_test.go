package loop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// The kill lands between observing a seal and saving the ledger (the `window` fault point).
//
// The window's base must be the checkpoint, not the ledger: a crash would otherwise restart the span
// from the previous seal — or from nothing — and the bound the user was given would quietly become
// "one span from whenever we came back". The copy's own timestamp survives us, so it is the base.
func TestTheWindowIsArmedFromTheCheckpointAfterACrash(t *testing.T) {
	dir := t.TempDir()
	sealedAt := time.Now().Add(-25 * time.Minute).Truncate(time.Second)
	testutil.Sealed(t, dir, 10, 100)
	touchTree(t, filepath.Join(dir, testutil.StepDir(10)), sealedAt)

	span := 30 * time.Minute
	crash := errors.New("simulated crash")
	crashAt := func(point string) error {
		if point == "window" {
			return crash
		}
		return nil
	}

	now := sealedAt.Add(25 * time.Minute)
	first, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, WithUs: span, MinCopies: 1},
		Window: span, Policy: calm{}, Now: func() time.Time { return now }, VerifyEvery: -1,
		Fault: crashAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Round(); !errors.Is(err, crash) {
		t.Fatalf("round = %v, want the injected crash at the seal", err)
	}
	before := first.window.DueAt()
	if before.IsZero() {
		t.Fatal("the window was not armed by the seal the round observed")
	}

	// The restart: the ledger was never saved, so everything about the window must come from the copy.
	restarted, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, WithUs: span, MinCopies: 1},
		Window: span, Policy: calm{}, Now: func() time.Time { return now }, VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := restarted.Round(); err != nil || st.Attempts.Running != 0 {
		t.Fatalf("the restarted round wrote before the window closed: %+v, %v", st.Attempts, err)
	}
	after := restarted.window.DueAt()
	if !after.Equal(before) {
		t.Fatalf("the window moved across the crash: %s → %s", before.Format(time.RFC3339), after.Format(time.RFC3339))
	}
	if want := sealedAt.Add(span); !after.Equal(want) {
		t.Fatalf("the window's base is not the checkpoint: due %s, want %s (armed from %s; sealStep %d)",
			after.Format(time.RFC3339), want.Format(time.RFC3339),
			restarted.window.RefreshedAt().Format(time.RFC3339), restarted.Ledger().Status.SealStep)
	}
	if left := restarted.window.Remaining(now); left <= 0 {
		t.Fatalf("the restarted window is already due (%s left): the crash bought a free span", left)
	}
	fmt.Printf("window after a crash: due stays %s (seal %s plus %s)\n",
		after.Format("15:04:05"), sealedAt.Format("15:04:05"), span)
}

// touchTree gives every file in a directory the same modification time, which is how a checkpoint
// says when it was written to a process that did not see it appear.
func touchTree(t *testing.T, dir string, at time.Time) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Chtimes(filepath.Join(dir, e.Name()), at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dir, at, at); err != nil {
		t.Fatal(err)
	}
}
