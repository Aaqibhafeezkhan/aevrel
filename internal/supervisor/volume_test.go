package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
	"github.com/bolubo/checkpoint-governor/internal/unit"
)

// TestTwoUnitsShareOneVolume is the volume gate's acceptance: two units on one disk ask for a write
// in the same tick, one holds the slot, the other is refused with `volume-busy`, and the moment the
// first write's verdict arrives the slot is free and the second write starts.
func TestTwoUnitsShareOneVolume(t *testing.T) {
	base := t.TempDir()
	volume := filepath.Join(base, "volume")
	if err := os.MkdirAll(volume, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var dirs []string
	var members []unit.Member
	for i := 1; i <= 2; i++ {
		dir := filepath.Join(volume, fmt.Sprintf("job-%d", i))
		newUnit(t, dir, 10)
		dirs = append(dirs, dir)
		members = append(members, unit.Member{
			ID: fmt.Sprintf("pod-%d", i), Name: fmt.Sprintf("job-%d", i), Dir: dir, Volume: volume,
		})
	}
	s := Open(Config{
		Policy:      calm{},
		Bounds:      policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Window:      time.Minute,
		Now:         func() time.Time { return now },
		VerifyEvery: -1,
	})
	if err := s.Sync(members); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tick(); err != nil {
		t.Fatal(err)
	}

	// Both units' timers expire in the same tick: one write starts, the other is refused.
	now = now.Add(61 * time.Second)
	steps, err := s.Tick()
	if err != nil {
		t.Fatal(err)
	}
	started, refused := 0, 0
	for _, st := range steps {
		started += st.Attempts.Running
		refused += st.Attempts.Refused
		if st.Attempts.Refused > 0 && st.Gate != "volume-busy" {
			t.Fatalf("the refused unit's gate reason = %q, want volume-busy", st.Gate)
		}
	}
	if started != 1 || refused != 1 {
		t.Fatalf("started=%d refused=%d, want one of each", started, refused)
	}
	if n := s.InUse(volume); n != 1 {
		t.Fatalf("writes on the volume = %d, want 1", n)
	}
	fmt.Printf("volume: started=%d refused=%d in-use=%d\n", started, refused, s.InUse(volume))

	// The job answers the write that started.
	var first string
	for _, dir := range dirs {
		if steps[dir].Attempts.Running > 0 {
			first = dir
			testutil.Sealed(t, dir, 10, 100)
		}
	}
	if first == "" {
		t.Fatal("no unit started a write")
	}

	// The first write answers, and its verdict frees the slot. One more tick so the order the two
	// keys were drained in cannot decide the outcome: the refused unit is due again the moment the
	// slot is free, and this time it is allowed.
	now = now.Add(10 * time.Second)
	if _, err := s.Tick(); err != nil {
		t.Fatal(err)
	}
	sealed := 0
	for _, dir := range dirs {
		sealed += s.Loop(dir).Attempts().Sealed
	}
	if sealed != 1 {
		t.Fatalf("after the first verdict: sealed=%d, want 1", sealed)
	}

	now = now.Add(time.Second)
	if _, err := s.Tick(); err != nil {
		t.Fatal(err)
	}
	var waiting string
	for _, dir := range dirs {
		if s.Loop(dir).Attempts().Running > 0 {
			waiting = dir
		}
	}
	if waiting == "" {
		t.Fatalf("the refused unit did not start once the slot was free: %v", s.Queue())
	}
	fmt.Printf("volume: sealed=%d in-use=%d\n", sealed, s.InUse(volume))

	// The second write answers too: one volume, two units, one write at a time, both sealed. The
	// refusal is gone from the history at this point, and that is the rule: the same request is one
	// row, so its eventual outcome replaces the refusal that came before it.
	testutil.Sealed(t, waiting, 10, 100)
	now = now.Add(10 * time.Second)
	if _, err := s.Tick(); err != nil {
		t.Fatal(err)
	}
	sealed = 0
	for _, dir := range dirs {
		sealed += s.Loop(dir).Attempts().Sealed
	}
	fmt.Printf("volume: units=%d sealed=%d in-use=%d\n", len(dirs), sealed, s.InUse(volume))
	if sealed != 2 || s.InUse(volume) != 0 {
		t.Fatalf("sealed=%d in-use=%d, want two sealed and a free volume", sealed, s.InUse(volume))
	}
}
