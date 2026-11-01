package loop

import (
	"strings"
	"testing"
	"time"
)

// The receipt must never say "no writes" while checkpoints sit on disk. A job with its own timer
// produces them, and a receipt that hides them is the kind of lie this product exists to replace.
func TestTheReceiptCountsWhatIsOnDiskWhenNoneWereOurs(t *testing.T) {
	r := Receipt{Duration: 8 * time.Second, Asked: 0, OnDisk: 9, Window: 2 * time.Second}
	got := strings.Join(r.Lines(), "\n")
	if strings.Contains(got, "no writes") {
		t.Fatalf("the receipt claims no writes while nine checkpoints exist:\n%s", got)
	}
	if !strings.Contains(got, "9 checkpoints") {
		t.Fatalf("the receipt does not say what is on disk:\n%s", got)
	}
}

// The bound is printed whether or not anything was written: the hole does not depend on the run
// having been busy, and it is the one thing the user actually bought.
func TestTheReceiptAlwaysStatesTheBound(t *testing.T) {
	for _, r := range []Receipt{
		{Duration: time.Minute},
		{Duration: time.Minute, Asked: 2, Sealed: 2, OnDisk: 2, Window: 30 * time.Minute},
	} {
		got := strings.Join(r.Lines(), "\n")
		if !strings.Contains(got, "silent death") {
			t.Fatalf("no bound in the receipt:\n%s", got)
		}
	}
}

// Without a window the receipt says what an unset window costs, rather than staying quiet.
func TestTheReceiptSaysWhatAnUnsetWindowCosts(t *testing.T) {
	r := Receipt{Duration: time.Minute, Asked: 1, Sealed: 1, OnDisk: 1}
	got := strings.Join(r.Lines(), "\n")
	if !strings.Contains(got, "costs everything") {
		t.Fatalf("an unset window is not described:\n%s", got)
	}
}
