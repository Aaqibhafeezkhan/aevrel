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

// The controller's half of the user's stop, first phase. A job that has said it is stopping is
// asked for exactly one write — the work that exists now is the work that matters — and it is told
// nothing about leaving while that answer is outstanding. The job cannot see what is in flight, so it
// must not be the side that decides.
func TestAStoppingJobIsAskedForItsFinalWrite(t *testing.T) {
	dir := t.TempDir()
	now := t0

	l, err := Open(Config{
		Dir: dir, UnitID: "train/llama", Bounds: bounds(), Policy: policy.Risk{},
		Now:           func() time.Time { return now },
		StopRequested: func() bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	if exec.Released(dir) {
		t.Fatal("may-exit was written while the final write had just been requested")
	}
	st := l.Ledger().Status
	if st.AdviceAction != string(policy.ActionSave) || st.FinalOutcome == "" {
		t.Fatalf("the stopping job was not asked for its final write: action=%q final=%q",
			st.AdviceAction, st.FinalOutcome)
	}
	fmt.Println("stop: intent → one final request (" + st.FinalOutcome + ") → may-exit withheld")
}

// Second phase: once the answer is settled — the copy at the step the job stands on is already on
// disk — the job is released. Nothing in flight means nothing to wait for.
func TestAStoppingJobIsReleasedOnceTheAnswerIsSettled(t *testing.T) {
	dir := t.TempDir()
	now := t0
	testutil.Sealed(t, dir, 12, 100)
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, advise.BaseDir, "step"), []byte("12"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := Open(Config{
		Dir: dir, UnitID: "train/llama", Bounds: bounds(), Policy: policy.Risk{},
		Now:           func() time.Time { return now },
		StopRequested: func() bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	if !exec.Released(dir) {
		t.Fatal("a stopping job was not released: it would sit out its whole grace period")
	}
	fmt.Println("stop: the step it stands on is already saved → may-exit=1 (nothing left to wait for)")
}

// The release is gated on the job actually stopping: a running job must never see the file, or it
// would exit on its own and lose everything.
func TestARunningJobIsNotReleased(t *testing.T) {
	dir := t.TempDir()
	now := t0

	l, err := Open(Config{
		Dir: dir, UnitID: "train/llama", Bounds: bounds(), Policy: policy.Risk{},
		Now:           func() time.Time { return now },
		StopRequested: func() bool { return false },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	if exec.Released(dir) {
		t.Fatal("a running job was released: that is a self-inflicted data loss")
	}
}
