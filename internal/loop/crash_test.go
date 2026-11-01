package loop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/exec"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// TestACrashAfterPublishingIsReconciled is R1's acceptance: the plan reached the disk, the process
// died before the attempt was recorded, and the job wrote anyway. On the restart the first file is
// adopted, the missing attempt is rebuilt from it, and the copy that answers it is counted once.
func TestACrashAfterPublishingIsReconciled(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, advise.BaseDir, "step"), []byte("10"), 0o644); err != nil {
		t.Fatal(err)
	}
	crash := errors.New("simulated crash between publish and record")
	before := Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Policy: savesNow{}, Now: func() time.Time { return now },
		VerifyEvery: -1, Fault: faulty("publish", crash),
	}
	l, err := Open(before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Round(); !errors.Is(err, crash) {
		t.Fatalf("round err = %v, want the injected crash", err)
	}
	if n := l.Attempts().Running; n != 0 {
		t.Fatalf("the crashed round recorded %d attempts, want none: the write is the crash window", n)
	}

	// The job read the plan and answered it before the controller came back.
	testutil.Sealed(t, dir, 10, 100)
	now = now.Add(10 * time.Second)
	after := before
	after.Fault = nil
	l2, err := Open(after)
	if err != nil {
		t.Fatal(err)
	}
	if l2.Ledger().Reconciled != 1 {
		t.Fatalf("reconciled = %d, want 1: the published plan is newer than the ledger", l2.Ledger().Reconciled)
	}
	st, err := l2.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempts.Sealed != 1 || st.Attempts.Running != 0 {
		t.Fatalf("after the restart: %s, want the rebuilt attempt sealed exactly once", st.Attempts)
	}
	fmt.Printf("reconciled=%d sealed=%d\n", l2.Ledger().Reconciled, st.Attempts.Sealed)
}

// TestTheHistoryIsBoundedUnderALongRun is R2's acceptance: ten thousand rounds, every one of them
// asking for a write at a new step, and the history stays a summary instead of growing without end.
// The clock passes each deadline, so every attempt ends as a failure and none is left running.
func TestTheHistoryIsBoundedUnderALongRun(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Policy: savesNow{}, Now: func() time.Time { return now },
		VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	const rounds = 10000
	for i := 1; i <= rounds; i++ {
		// A new step each round, so every request is a new identity; and past the previous deadline,
		// so nothing is left running behind us.
		if err := os.WriteFile(filepath.Join(dir, advise.BaseDir, "step"), []byte(fmt.Sprint(i)), 0o644); err != nil {
			t.Fatal(err)
		}
		now = now.Add(11 * time.Minute)
		if _, err := l.Round(); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	// Two closing rounds. The first asks for a write past the previous deadline: it is allowed, and
	// that attempt is the one left in flight. The second judges it — nothing appeared, so it failed —
	// and then refuses the identical request as a duplicate, because this time it was served (and
	// missed its deadline) rather than refused. The history therefore ends with nothing running.
	now = now.Add(11 * time.Minute)
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	st, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	attempts := len(l.attempts.Attempts)
	running := st.Attempts.Running
	fmt.Printf("attempts=%d running=%d\n", attempts, running)
	if attempts != 32 || running != 0 {
		t.Fatalf("after %d rounds: attempts=%d running=%d, want 32 and 0", rounds, attempts, running)
	}
	// The file on disk is the same summary: a restart reads what the long run left behind.
	if saved, err := exec.OpenLedger(dir); err != nil || len(saved.Attempts) != 32 {
		t.Fatalf("saved attempts = %d (%v), want 32", len(saved.Attempts), err)
	}
}
