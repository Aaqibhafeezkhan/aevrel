package loop

import (
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

// savesNow is the E4 test's policy: it always asks for a write, so the gate is the only thing that
// can stop one. The real policy's conditions are covered by its own truth table.
type savesNow struct{}

func (savesNow) Name() string { return "saves-now" }

func (savesNow) Decide(b policy.Bounds, s policy.Snapshot) policy.Decision {
	return policy.Decision{
		UnitID: s.UnitID, Generation: s.Generation, Version: "saves-now",
		Action: policy.ActionSave, Reason: policy.ReasonSchedule, SaveNow: true,
		Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1,
		Reasons: []policy.ReasonEntry{{Code: "risk.preempt", Values: map[string]float64{}}},
		Why:     "test",
	}
}

// TestAWriteSpansThreeRounds is E4's acceptance: a round starts an attempt and returns, the next
// round sees it still running, and the third sees it sealed — with a scan in every round.
func TestAWriteSpansThreeRounds(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// The job reports its own step; the request is for step 10, and the write that answers it is the
	// step-10 directory.
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, advise.BaseDir, "step"), []byte("10"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Open(Config{
		Dir: dir, UnitID: "u", Holder: "",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Policy: savesNow{}, Now: func() time.Time { return now },
		VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Round 1: the gate starts a write; the round returns without waiting for it.
	st, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempts.Running != 1 {
		t.Fatalf("round 1 attempts = %+v, want one running", st.Attempts)
	}
	fmt.Printf("round 1: %s\n", st.Attempts)

	// The job starts writing: a directory appears with a shard and no manifest.
	testutil.Unsealed(t, dir, 10, 100)

	// Round 2: still running, and the scan ran.
	now = now.Add(10 * time.Second)
	st, err = l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempts.Running != 1 || st.Attempts.Sealed != 0 {
		t.Fatalf("round 2 attempts = %+v, want still running", st.Attempts)
	}
	fmt.Printf("round 2: %s\n", st.Attempts)

	// Round 3: the write sealed; the attempt settles from the directory.
	testutil.Sealed(t, dir, 10, 100)
	now = now.Add(30 * time.Second)
	st, err = l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempts.Sealed != 1 || st.Attempts.Running != 0 {
		t.Fatalf("round 3 attempts = %+v, want one sealed", st.Attempts)
	}
	fmt.Printf("round 3: %s\n", st.Attempts)
}

// TestTheGateStopsAWriteAndLeavesATrace: a refused save is not published as a save, and the refusal
// itself is recorded, so a condition that keeps firing has history instead of silence.
//
// The refusal used here is the cooldown door, because it is the one a test can build honestly: the
// job sealed step 10 five seconds after it started and thirty seconds ago, and it is now at step 20.
// Free space cannot be faked without faking the disk, and the other doors are reached through the
// loop's own rules, so the trace of a refusal is what this test is about, not the door.
func TestTheGateStopsAWriteAndLeavesATrace(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	writeSealed(t, dir, 10, 100)
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, advise.BaseDir, "step"), []byte("20"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := exec.NewAttempt("u", 1, 10, exec.TriggerCondition, "risk.preempt", now.Add(-35*time.Second), now.Add(-30*time.Second))
	prev.Outcome, prev.EndedAt, prev.Bytes = exec.Sealed, now.Add(-30*time.Second), 100
	if err := (exec.Ledger{Schema: exec.SchemaVersion, Attempts: []exec.Attempt{prev}}).Save(dir); err != nil {
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
	st, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Action != policy.ActionSkip {
		t.Fatalf("action = %q, want a skip when the gate refuses", st.Action)
	}
	if st.Attempts.Refused != 1 {
		t.Fatalf("attempts = %+v, want the refusal recorded", st.Attempts)
	}
	if st.Gate != "cooldown" {
		t.Fatalf("gate reason = %q, want cooldown", st.Gate)
	}
	fmt.Printf("gate: %s (%s)\n", st.Gate, st.Attempts)
}

// TestTheWindowStartsAWriteWhenNothingElseDoes: with the user's window set and closed, a round starts
// an attempt even though the policy is calm. The window is the user's number, not ours: the policy
// still says "no condition fired", and the executor adds the one time-based reason there is.
func TestTheWindowStartsAWriteWhenNothingElseDoes(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Window: time.Minute,
		Policy: calm{}, Now: func() time.Time { return now },
		VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := l.Round(); st.Attempts.Running != 0 {
		t.Fatalf("a round before the window closed started a write: %+v", st.Attempts)
	}
	now = now.Add(61 * time.Second)
	st, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Action != policy.ActionSave || st.Attempts.Running != 1 {
		t.Fatalf("window closed: action %q, attempts %+v, want one running save", st.Action, st.Attempts)
	}
	if len(st.Notes) > 0 && st.Notes[0] == "" {
		t.Fatalf("notes = %v", st.Notes)
	}
	fmt.Printf("window: %s\n", st.Attempts)

	// The seal restarts the window: a round right after it must not ask again.
	testutil.Sealed(t, dir, 1, 100)
	now = now.Add(2 * time.Second)
	st, err = l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempts.Running != 0 {
		t.Fatalf("a seal must restart the window, not leave it due: %+v", st.Attempts)
	}
	fmt.Printf("window: sealed → next due %s\n", l.window.DueAt().Format("15:04:05"))
}

type calm struct{}

func (calm) Name() string { return "calm" }

func (calm) Decide(b policy.Bounds, s policy.Snapshot) policy.Decision {
	return policy.Decision{
		UnitID: s.UnitID, Generation: s.Generation, Version: "calm",
		Action: policy.ActionSkip, Reason: policy.ReasonCalm,
	}
}

var _ = exec.Counts{}
