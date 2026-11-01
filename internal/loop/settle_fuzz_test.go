package loop

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/exec"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// TestSettleInvariants is H1's fuzz half. Settlement is the one function that turns the directory into
// verdicts, so it is fuzzed over snapshots nobody would write by hand, and the properties asked of it
// are the ones the rest of the system relies on:
//
//	1 the counts are a query over the list, exactly;
//	2 a finished attempt is never re-judged — settling twice is settling once;
//	3 nothing is left running past a deadline it had already missed;
//	4 a sealed verdict is always backed by a copy that appeared after the attempt started.
func TestSettleInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	states := []policy.CopyState{policy.CopySealed, policy.CopyVerified, policy.CopyBroken, policy.CopyUnverified}
	outcomes := []exec.Outcome{exec.Sealed, exec.Broken, exec.Failed, exec.Abandoned, exec.Refused}

	const snapshots = 1000
	breaks := 0
	breach := func(what string, i int, v any) {
		breaks++
		if breaks <= 3 {
			t.Errorf("snapshot %d: %s (%v)", i, what, v)
		}
	}

	for i := 0; i < snapshots; i++ {
		now := base.Add(time.Duration(rng.Intn(7200)) * time.Second)

		var copies []policy.Copy
		for n := rng.Intn(5); n > 0; n-- {
			c := policy.Copy{
				Step:  int64(rng.Intn(50)),
				Bytes: int64(rng.Intn(1000)),
				State: states[rng.Intn(len(states))],
			}
			if rng.Intn(4) > 0 {
				c.CreatedAt = base.Add(time.Duration(rng.Intn(7200)) * time.Second)
			}
			copies = append(copies, c)
		}

		var attempts []exec.Attempt
		for n := rng.Intn(4) + 1; n > 0; n-- {
			a := exec.Attempt{ID: fmt.Sprintf("a%d-%d", i, n), Generation: int64(rng.Intn(3)), Step: int64(rng.Intn(50))}
			a.StartedAt = base.Add(time.Duration(rng.Intn(7200)) * time.Second)
			if rng.Intn(4) > 0 {
				a.Deadline = a.StartedAt.Add(time.Duration(rng.Intn(3600)) * time.Second)
			}
			if rng.Intn(3) == 0 {
				a.Outcome = outcomes[rng.Intn(len(outcomes))]
				a.EndedAt = a.StartedAt.Add(time.Second)
			}
			attempts = append(attempts, a)
		}

		s := policy.Snapshot{Copies: copies}
		wasRunning := make([]bool, len(attempts))
		for j, a := range attempts {
			wasRunning[j] = !a.Done()
		}
		got, counts := Settle(attempts, s, now)
		if sum := counts.Running + counts.Sealed + counts.Broken + counts.Abandoned + counts.Failed + counts.Refused; sum != len(got) {
			breach("counts do not add up to the list", i, counts)
		}
		again, _ := Settle(got, s, now)
		for j := range got {
			if got[j] != again[j] {
				breach("a finished attempt was re-judged", i, got[j])
			}
			a := got[j]
			if !a.Done() && !a.Deadline.IsZero() && !now.Before(a.Deadline) {
				breach("running past its deadline", i, a)
			}
			// Only a verdict this call produced is checked against the copies: an attempt that was
			// already finished came from the ledger, not from this snapshot.
			if wasRunning[j] && a.Outcome == exec.Sealed && !sealedAfterStart(copies, a.StartedAt) {
				breach("sealed without a copy that appeared after the request", i, a)
			}
		}
	}
	fmt.Printf("fuzz: %d snapshots, %d invariant breaks\n", snapshots, breaks)
	if breaks != 0 {
		t.Fatalf("%d invariant breaks", breaks)
	}
}

// sealedAfterStart is the copy rule again, written independently of producedBy: a verdict of "sealed"
// must be backed by a complete copy whose date is no earlier than the request.
func sealedAfterStart(copies []policy.Copy, started time.Time) bool {
	for _, c := range copies {
		if c.State != policy.CopySealed && c.State != policy.CopyVerified {
			continue
		}
		if !c.CreatedAt.IsZero() && !c.CreatedAt.Before(started) {
			return true
		}
	}
	return false
}
