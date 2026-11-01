package loop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// TestEveryCrashWindowReconciles is X10's acceptance.
//
// Five places a process can die: after publishing a plan, after recording the attempt it produced,
// after recording a seal, after renaming a copy into the trash, and after sweeping the trash. For each,
// a restart must produce the same world: no request counted twice, and the good checkpoint still there.
func TestEveryCrashWindowReconciles(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	crash := errors.New("simulated crash")

	cases := []struct {
		point string
		// setup lays out the world the point needs, and says what the job does after the crash.
		setup func(t *testing.T, dir string)
		// write is true when the crashed round had asked for a write, so the job answers it afterwards.
		write bool
	}{
		{"publish", nil, true},
		{"record", nil, true},
		{"seal", func(t *testing.T, dir string) { testutil.Sealed(t, dir, 10, 100) }, false},
		// The kill lands between "we know when the checkpoint landed" and "the ledger remembers it":
		// the window's due must come back from the copy, not from a ledger that was never written.
		{"window", func(t *testing.T, dir string) { testutil.Sealed(t, dir, 10, 100) }, false},
		{"retire", func(t *testing.T, dir string) {
			testutil.Sealed(t, dir, 8, 100)
			testutil.Sealed(t, dir, 9, 100)
			testutil.Sealed(t, dir, 10, 100)
		}, false},
		{"sweep", func(t *testing.T, dir string) {
			testutil.Sealed(t, dir, 10, 100)
			if err := os.MkdirAll(filepath.Join(dir, trashDir, "step-7-1"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, false},
	}

	for _, c := range cases {
		t.Run(c.point, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
				t.Fatal(err)
			}
			if c.setup != nil {
				c.setup(t, dir)
			}
			if c.write {
				if err := os.WriteFile(filepath.Join(dir, advise.BaseDir, "step"), []byte("11\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg := Config{
				Dir: dir, UnitID: "u",
				Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
				Policy: savesNow{}, Now: func() time.Time { return now },
				VerifyEvery: -1,
				Fault: func(point string) error {
					if point == c.point {
						return crash
					}
					return nil
				},
			}
			l, err := Open(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.Round(); !errors.Is(err, crash) {
				t.Fatalf("round at %s = %v, want the injected crash", c.point, err)
			}

			// The job answers the write the crashed round asked for, if it asked for one.
			if c.write {
				testutil.Sealed(t, dir, 11, 100)
			}

			// The restart: same directory, no fault, one round.
			restarted, err := Open(Config{
				Dir: dir, UnitID: "u",
				Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
				Policy: savesNow{}, Now: func() time.Time { return now },
				VerifyEvery: -1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.Round(); err != nil {
				t.Fatalf("restart after %s: %v", c.point, err)
			}

			// No request counted twice: identities are unique.
			seen := map[string]bool{}
			for _, a := range restarted.attempts.Attempts {
				if seen[a.ID] {
					t.Fatalf("%s: the same request was counted twice (%s)", c.point, a.ID)
				}
				seen[a.ID] = true
			}
			if c.write && restarted.Attempts().Sealed != 1 {
				t.Fatalf("%s: sealed = %d, want the answered write counted exactly once", c.point, restarted.Attempts().Sealed)
			}
			// The good checkpoint is still there.
			if !hasComplete(dir) {
				t.Fatalf("%s: the good checkpoint did not survive", c.point)
			}
		})
	}
	fmt.Printf("crash: %d points, 0 double counts, last copy intact\n", len(cases))
}
