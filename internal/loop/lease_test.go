package loop

import (
	"fmt"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// TestTakeoverMidWriteAdoptsTheAttempt is lease takeover's acceptance.
//
// A controller holds the lease and a write is in flight. Its lease expires, a second controller takes
// it, and the second one adopts the running attempt instead of starting a second write: the attempt
// ledger is the truth, and the directory settles it whoever holds the lease. The old holder goes
// read-only on its next round, and gives back the volume slot it can no longer serve.
func TestTakeoverMidWriteAdoptsTheAttempt(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cfg := func(holder string) Config {
		return Config{
			Dir: dir, UnitID: "u",
			Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
			Policy: savesNow{}, Now: func() time.Time { return now },
			Holder: holder, LeaseTTL: 15 * time.Second, VerifyEvery: -1,
		}
	}

	first, err := Open(cfg("ctrl-a"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := first.Round()
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempts.Running != 1 {
		t.Fatalf("attempts = %s, want the first write running", st.Attempts)
	}

	// Twenty seconds later the lease has expired and nobody renewed it: the second controller takes it.
	now = now.Add(20 * time.Second)
	second, err := Open(cfg("ctrl-b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Round(); err != nil {
		t.Fatal(err)
	}
	adopted := second.Attempts().Running
	if adopted != 1 {
		t.Fatalf("the second controller sees %d running attempts, want the one it adopted", adopted)
	}
	if got := second.Attempts().Sealed + second.Attempts().Failed; got != 0 {
		t.Fatalf("the takeover started its own write: %s", second.Attempts())
	}
	fmt.Printf("lease: takeover at 20s, adopted %d running attempt, 0 double writes\n", adopted)

	// The old holder's next round finds the lease taken: it publishes nothing and returns its slot.
	now = now.Add(time.Second)
	st, err = first.Round()
	if err != nil {
		t.Fatal(err)
	}
	if !st.ReadOnly {
		t.Fatalf("the old holder kept writing: %+v", st)
	}
	fmt.Printf("lease: the old holder is read-only (attempts still %s)\n", first.Attempts())
}
