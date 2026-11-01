package loop

import (
	"fmt"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
)

// A plan that expires while the controller is alive is the one lie this file must never tell: the job
// reads "the governor is gone" and (with `no-us`) falls back to its own timer — or with
// `no-us none` stops writing altogether, having been told that nothing would.
//
// The publish breaker exists to stop a flapping decision rewriting a file the job reads five times a
// second. It must not be able to starve the **renewal**, which is the heartbeat that keeps the file
// alive. A real-clock smoke with a 200ms cadence caught the old behaviour: 103s of silence where the
// span was 60s, because twelve renewals filled the breaker's minute and the plan died for the rest.
func TestThePlanNeverExpiresWhileTheControllerIsAlive(t *testing.T) {
	dir := t.TempDir()
	now := t0
	l, err := Open(Config{
		Dir: dir, UnitID: "u", Bounds: bounds(), Policy: calm{},
		Now:     func() time.Time { return now },
		PlanTTL: 3 * time.Second,
		// Two publishes per minute: a breaker that counted renewals would strangle the plan in the
		// first second and the job would spend the rest of the test believing we were dead.
		MaxPublishPerMinute: 2,
		VerifyEvery:         -1,
	})
	if err != nil {
		t.Fatal(err)
	}

	expired, rounds, published := 0, 0, 0
	deadline := now.Add(30 * time.Second)
	for now.Before(deadline) {
		now = now.Add(200 * time.Millisecond)
		if _, err := l.Round(); err != nil {
			t.Fatalf("round at %s: %v", now.Format("15:04:05"), err)
		}
		rounds++
		p, ok := advise.ReadPublished(dir)
		if !ok {
			continue
		}
		published++
		if !now.Before(p.ExpiresAt) {
			// The job's rule at this instant: no live plan. Saying that while we are rounding is the lie.
			expired++
			if expired == 1 {
				t.Errorf("the plan expired at %s while the controller was alive (round %d)",
					now.Format("15:04:05"), rounds)
			}
		}
	}
	if expired > 0 {
		t.Fatalf("%d of %d rounds saw an expired plan: the heartbeat is being rate-limited", expired, rounds)
	}
	if published == 0 {
		t.Fatal("no plan was ever published")
	}
	if n := len(l.ledger.PublishTimes); n > 2 {
		t.Fatalf("renewals were counted against the breaker: %d entries, the limit is 2", n)
	}
	fmt.Printf("plan: alive in all %d rounds over 30s (ttl 3s, breaker 2/min, %d counted changes)\n",
		rounds, len(l.ledger.PublishTimes))
}
