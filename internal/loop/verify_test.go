package loop

import (
	"fmt"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// TestVerifiedNeverLookedAndDeferredAreThreeFacts is the acceptance of the three verify facts.
//
// The controller must not let "we have not looked" read like "nothing is broken". Three facts stay
// apart: the age of the newest copy nobody has read back, how many reads the budget pushed away, and
// what the budget is. And a read never happens while a write is in flight: reading a checkpoint the
// job is still writing damages the thing the read is trying to prove.
func TestVerifiedNeverLookedAndDeferredAreThreeFacts(t *testing.T) {
	// 1. A budget too small to afford one read: the copy stays unverified, and the deferral is counted.
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	testutil.Sealed(t, dir, 10, 100)
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Policy: policy.Risk{}, Now: func() time.Time { return now },
		VerifyEvery: 10 * time.Minute, VerifyBytesPerHour: 1, // one byte per hour
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		now = now.Add(21 * time.Minute)
		if _, err := l.Round(); err != nil {
			t.Fatal(err)
		}
	}
	st := l.Ledger().Status
	if st.NewestUnverifiedAt == 0 || st.VerifyDeferred == 0 || st.Overhead.VerifyBytes != 0 {
		t.Fatalf("status = %+v, want an unverified copy, deferrals, and no read", st)
	}
	fmt.Printf("verify: %d deferred, newest unverified %s, %dB used of %dB/hour\n",
		st.VerifyDeferred, short(st.NewestUnverifiedAt, now), st.VerifyUsedBytes, st.VerifyBudgetBytes)

	// 2. A write in flight: no read at all, even with a generous budget and an unverified copy.
	dir2 := t.TempDir()
	now2 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	writeSealedDigest(t, dir2, 10, make([]byte, 100)) // a copy that can actually be checked
	testutil.Unsealed(t, dir2, 11, 10)                // a write is in flight
	l2, err := Open(Config{
		Dir: dir2, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Policy: policy.Risk{}, Now: func() time.Time { return now2 },
		VerifyEvery: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The scanner calls a directory "in flight" when it was first seen recently; the sighting is the
	// ledger's, so seed it the way a previous round would have.
	l2.Ledger().Observe(dirName(11), now2.Add(-10*time.Second))
	if _, err := l2.Round(); err != nil {
		t.Fatal(err)
	}
	if st := l2.Ledger().Status; st.Overhead.VerifyBytes != 0 || l2.Ledger().LastVerifyAt != 0 {
		t.Fatalf("read back while a write was in flight: %+v", st)
	}
	fmt.Printf("verify: 0 reads during a write\n")

	// 3. Nothing in flight and a budget to spend: the newest copy is read back, and the age resets.
	now2 = now2.Add(11 * time.Minute)
	if _, err := l2.Round(); err != nil {
		t.Fatal(err)
	}
	st2 := l2.Ledger().Status
	if st2.Overhead.VerifyBytes == 0 || st2.NewestUnverifiedAt != 0 {
		t.Fatalf("status = %+v, want the copy read back and nothing unverified", st2)
	}
	fmt.Printf("verify: read %dB back, newest unverified none\n", st2.Overhead.VerifyBytes)
}

// short is how a status line says an age: the time since a unix stamp, or "none" when there is none.
func short(stamp int64, now time.Time) string {
	if stamp == 0 {
		return "none"
	}
	d := now.Sub(time.Unix(stamp, 0))
	if d < time.Minute {
		return "under a minute"
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
