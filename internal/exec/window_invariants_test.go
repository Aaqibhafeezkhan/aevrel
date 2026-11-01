package exec

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// The window is the only clock this side owns, so its arithmetic is pinned: a due date never lands
// before the seal it was armed from, never beyond the span, and an infeasible span is due now and
// says so. A measured write that gets longer moves the due date earlier — that is correct, not a
// regression, because the promise is about the seal and not about the request.
func TestTheWindowInvariantsHold(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	w := NewWindow(30 * time.Minute)
	base := w0
	infeasible, feasible, refreshed := 0, 0, 0
	for i := 0; i < 1000; i++ {
		base = base.Add(time.Duration(rng.Intn(600)) * time.Second)
		cost := time.Duration(rng.Intn(3000)) * time.Second
		w.Arm(base, cost)

		if w.Infeasible() {
			infeasible++
			if !w.Due(base) {
				t.Fatalf("round %d: infeasible span is not due immediately", i)
			}
			if got := w.DueAt(); !got.Equal(base) {
				t.Fatalf("round %d: infeasible due %s, want the seal %s", i, got, base)
			}
			continue
		}
		feasible++

		due := w.DueAt()
		if due.Before(base) {
			t.Fatalf("round %d: due %s is before the seal %s", i, due, base)
		}
		if max := base.Add(w.Span()); due.After(max) {
			t.Fatalf("round %d: due %s is beyond the span %s", i, due, max)
		}
		if w.Span() <= 0 {
			t.Fatalf("round %d: an off window is never due, and it is due at %s", i, due)
		}
		// Due a second before the due date, due exactly at it: the boundary is the seal's, not a
		// rounding error.
		if w.Due(due.Add(-time.Second)) && !due.Equal(base) {
			t.Fatalf("round %d: due before the due date %s", i, due)
		}
		if !w.Due(due) {
			t.Fatalf("round %d: not due at the due date %s", i, due)
		}
		refreshed++
	}
	fmt.Printf("window invariants: 3/3 over 1000 rounds (%d feasible, %d infeasible, %d boundary checks), 0 violations\n",
		feasible, infeasible, refreshed)
}

// A window that is off is inert under every arm: no due date, no signal. This is the "exceptions
// only" deployment, and a stray arm must not quietly turn it into a schedule.
func TestAnOffWindowStaysOffUnderAnyArm(t *testing.T) {
	w := NewWindow(0)
	base := w0
	for i := 0; i < 100; i++ {
		base = base.Add(time.Duration(i) * time.Minute)
		w.Arm(base, time.Duration(i)*time.Second)
		if w.Due(base) || !w.DueAt().IsZero() || w.Infeasible() {
			t.Fatalf("round %d: an off window answered (%s)", i, w)
		}
	}
	fmt.Println("window invariants: an off window stays off over 100 arms")
}
