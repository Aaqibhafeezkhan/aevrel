package testutil

import (
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/observe"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// The fixtures are shared, so their meaning has to be agreed: this test asks the reader what each
// one is, and the answer is the contract both implementers rely on.
func TestTheFixturesMeanWhatTheReaderThinks(t *testing.T) {
	now := time.Now()
	scan := func(dir string) policy.Snapshot {
		t.Helper()
		s, _, err := observe.Scan(observe.Request{
			Dir: dir, UnitID: "u", Now: now,
			Seen: map[string]time.Time{}, Changed: map[string]time.Time{},
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	sealedDir := t.TempDir()
	Sealed(t, sealedDir, 4, 100)
	s := scan(sealedDir)
	if len(s.Copies) != 1 || s.Copies[0].State != policy.CopySealed {
		t.Fatalf("sealed fixture = %+v", s.Copies)
	}

	brokenDir := t.TempDir()
	Broken(t, brokenDir, 7)
	s = scan(brokenDir)
	if len(s.Copies) != 1 || s.Copies[0].State != policy.CopyBroken {
		t.Fatalf("broken fixture = %+v", s.Copies)
	}

	liveDir := t.TempDir()
	Unsealed(t, liveDir, 9, 100)
	s = scan(liveDir)
	if len(s.Copies) != 1 || s.Copies[0].State != policy.CopyUnverified {
		t.Fatalf("unsealed fixture = %+v", s.Copies)
	}
	// A directory with no manifest is only a write in flight once the controller has watched it
	// appear: on a first sighting it could be anything, which is why the ledger records first seen.
	s, _, err := observe.Scan(observe.Request{
		Dir: liveDir, UnitID: "u", Now: now, Stat: func(string) (int64, int64, error) { return 1, 1, nil },
		Seen:    map[string]time.Time{StepDir(9): now.Add(-time.Second)},
		Changed: map[string]time.Time{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.SaveInFlight {
		t.Fatalf("a write that was just seen starting is not in flight: %+v", s.Copies)
	}
}
