package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
	"github.com/bolubo/checkpoint-governor/internal/unit"
)

// calm is a policy that never asks for a write on its own: whatever happens in these tests happens
// because of the window or a generation change.
type calm struct{}

func (calm) Name() string { return "calm" }

func (calm) Decide(b policy.Bounds, s policy.Snapshot) policy.Decision {
	return policy.Decision{
		UnitID: s.UnitID, Generation: s.Generation, Version: "calm",
		Action: policy.ActionSkip, Reason: policy.ReasonCalm,
	}
}

func newUnit(t *testing.T, dir string, step int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, advise.BaseDir, "step"), []byte(fmt.Sprint(step)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestThreeUnitsWriteIndependently is U1's acceptance: three directories, three loops, one queue.
// Each unit's own timer asks for a write, each write seals, and no loop needed to know about the
// other two.
func TestThreeUnitsWriteIndependently(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var members []unit.Member
	var dirs []string
	for i := 1; i <= 3; i++ {
		dir := filepath.Join(base, fmt.Sprintf("job-%d", i))
		newUnit(t, dir, 10)
		dirs = append(dirs, dir)
		members = append(members, unit.Member{ID: fmt.Sprintf("pod-%d", i), Name: fmt.Sprintf("job-%d", i), Dir: dir})
	}
	s := Open(Config{
		Policy:      calm{},
		Bounds:      policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Window:      time.Minute,
		Now:         func() time.Time { return now },
		VerifyEvery: -1,
		Holder:      "supervisor",
	})
	if err := s.Sync(members); err != nil {
		t.Fatal(err)
	}

	// The first tick reconciles all three units: they exist, which is a change.
	steps, err := s.Tick()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("first tick reconciled %d units, want 3: %v", len(steps), steps)
	}

	// Every unit's own timer expires, and each one asks for its own write.
	now = now.Add(61 * time.Second)
	if _, err := s.Tick(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		l := s.Loop(dir)
		if l == nil {
			t.Fatalf("%s has no loop", dir)
		}
		if running := l.Attempts().Running; running != 1 {
			t.Fatalf("%s: running attempts = %d, want 1 after the timer expired", dir, running)
		}
	}

	// The job answers all three requests.
	for _, dir := range dirs {
		testutil.Sealed(t, dir, 10, 100)
	}
	now = now.Add(10 * time.Second)
	steps, err = s.Tick()
	if err != nil {
		t.Fatal(err)
	}
	sealed := 0
	for _, st := range steps {
		sealed += st.Attempts.Sealed
	}
	fmt.Printf("units=%d sealed=%d\n", len(steps), sealed)
	if len(steps) != 3 || sealed != 3 {
		t.Fatalf("units=%d sealed=%d, want 3 and 3", len(steps), sealed)
	}
}

// TestANewGenerationDropsTheOldAttempts is U2's acceptance: the pod is replaced mid-write, so the
// membership registry counts a new generation. The old generation's attempt is dropped rather than
// settled — it described a process that no longer exists — and the new generation starts its own
// timer instead of inheriting the old one.
func TestANewGenerationDropsTheOldAttempts(t *testing.T) {
	dir := t.TempDir()
	newUnit(t, dir, 10)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := Open(Config{
		Policy:      calm{},
		Bounds:      policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Window:      time.Minute,
		Now:         func() time.Time { return now },
		VerifyEvery: -1,
	})
	if err := s.Sync([]unit.Member{{ID: "pod-a", Name: "job", Dir: dir}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tick(); err != nil {
		t.Fatal(err)
	}

	// The timer expires and a write is asked for, but the job restarts before it answers.
	now = now.Add(61 * time.Second)
	if _, err := s.Tick(); err != nil {
		t.Fatal(err)
	}
	if running := s.Loop(dir).Attempts().Running; running != 1 {
		t.Fatalf("before the restart: running = %d, want 1", running)
	}

	if err := s.Sync([]unit.Member{{ID: "pod-b", Name: "job", Dir: dir}}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	steps, err := s.Tick()
	if err != nil {
		t.Fatal(err)
	}
	l := s.Loop(dir)
	if l.Ledger().Generation != 2 {
		t.Fatalf("generation = %d, want 2 after the pod was replaced", l.Ledger().Generation)
	}
	if st := steps[dir]; st.Attempts.Running != 0 {
		t.Fatalf("the old generation's attempt survived: %s", st.Attempts)
	}

	// The new generation's own timer expires, and it asks once — not once per incarnation.
	now = now.Add(2 * time.Minute)
	steps, err = s.Tick()
	if err != nil {
		t.Fatal(err)
	}
	st := steps[dir]
	total := st.Attempts.Running + st.Attempts.Sealed + st.Attempts.Broken + st.Attempts.Abandoned + st.Attempts.Failed + st.Attempts.Refused
	fmt.Printf("generation=%d attempts=%d\n", l.Ledger().Generation, total)
	if total != 1 || st.Attempts.Running != 1 {
		t.Fatalf("attempts = %d (running %d), want exactly the new generation's one", total, st.Attempts.Running)
	}
}
