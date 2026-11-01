package audit

import (
	"fmt"
	"testing"
	"time"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 1, 1, hour, minute, 0, 0, time.UTC)
}

// A run that began with a copy and wrote every ten minutes holds a fifteen-minute bound: the worst
// staleness is one period (the age just before a seal), and no second is spent over the bound.
func TestAHeldBoundHasNoSecondsOverIt(t *testing.T) {
	seals := []time.Time{}
	for m := 0; m <= 60; m += 10 {
		seals = append(seals, at(9, m%60).Add(time.Duration(m/60)*time.Hour))
	}
	got := Certify(15*time.Minute, at(9, 0), at(10, 5), seals)
	if !got.Held() {
		t.Fatalf("verdict not held: over %s, covered %v", got.OverBound, got.Covered)
	}
	if got.MaxStaleness != 10*time.Minute || got.MaxAt != at(9, 10) {
		t.Fatalf("worst staleness %s at %s, want one 10m period ending at 09:10", got.MaxStaleness, got.MaxAt)
	}
	if got.OverBound != 0 {
		t.Fatalf("over-bound %s, want zero", got.OverBound)
	}
	if got.Seals != 7 {
		t.Fatalf("seals %d, want 7", got.Seals)
	}
	worst, ok := got.Worst()
	if !ok || worst.Age != 10*time.Minute {
		t.Fatalf("worst segment %+v", worst)
	}
	fmt.Printf("audit: 7 seals every 10m · bound 15m · worst %s · over-bound 0s · held\n", got.MaxStaleness)
}

// One long gap is not averaged away: it is the seconds the promise was actually broken, and the
// verdict is not held.
func TestAGapWiderThanTheBoundIsCountedInSeconds(t *testing.T) {
	seals := []time.Time{at(9, 0), at(9, 10), at(9, 35), at(9, 45)}
	got := Certify(15*time.Minute, at(9, 0), at(9, 50), seals)
	if got.Held() {
		t.Fatal("a 25m gap under a 15m bound must not be held")
	}
	if got.MaxStaleness != 25*time.Minute {
		t.Fatalf("worst staleness %s, want the 25m gap", got.MaxStaleness)
	}
	// The bound breaks 15m after 09:10 and the next seal lands at 09:35: ten minutes over.
	if got.OverBound != 10*time.Minute {
		t.Fatalf("over-bound %s, want 10m", got.OverBound)
	}
	if got.MaxAt != at(9, 35) {
		t.Fatalf("worst moment %s, want just before the 09:35 seal", got.MaxAt)
	}
	fmt.Printf("audit: 25m gap under 15m · over-bound %s · not held\n", got.OverBound)
}

// A run that started with nothing has no bound until the first seal. That is a fact about the run,
// not an average, and the certificate reports it as an uncovered interval.
func TestAnUncoveredStartIsReportedNotSmoothed(t *testing.T) {
	seals := []time.Time{at(9, 30), at(9, 40)}
	got := Certify(15*time.Minute, at(9, 0), at(9, 45), seals)
	if got.Covered {
		t.Fatal("a run that began without a copy is not covered")
	}
	if got.Held() {
		t.Fatal("an uncovered start cannot hold a bound")
	}
	if got.Uncovered() != 30*time.Minute {
		t.Fatalf("uncovered %s, want the 30m before the first seal", got.Uncovered())
	}
	if got.GapFrom != at(9, 0) || got.GapTo != at(9, 30) {
		t.Fatalf("gap %s → %s", got.GapFrom, got.GapTo)
	}
	// The staleness measured after the first seal is still reported: it is what the window can fix.
	if got.MaxStaleness != 10*time.Minute {
		t.Fatalf("staleness after the first seal %s, want one 10m period", got.MaxStaleness)
	}
	fmt.Printf("audit: run began with no copy → uncovered %s, then worst %s\n", got.Uncovered(), got.MaxStaleness)
}

// With no bound there is nothing to certify: "exceptions only" is a different promise, and a
// certificate that called it held would be selling the absence of a promise as one.
func TestWithoutABoundNothingIsClaimed(t *testing.T) {
	got := Certify(0, at(9, 0), at(10, 0), []time.Time{at(9, 10), at(9, 20)})
	if got.Held() {
		t.Fatal("a zero bound is not a promise that can be held")
	}
	if got.OverBound != 0 || got.MaxStaleness != 40*time.Minute {
		t.Fatalf("over %s, worst %s", got.OverBound, got.MaxStaleness)
	}
	fmt.Printf("audit: no bound → no verdict, and the worst staleness is still reported (%s)\n", got.MaxStaleness)
}

// A seal stamped in the future is not a moment the run lived through; clamping it would invent
// coverage, so it is ignored.
func TestAFutureStampDoesNotCount(t *testing.T) {
	seals := []time.Time{at(9, 0), at(12, 0)} // the second one is after the audited end
	got := Certify(15*time.Minute, at(9, 0), at(9, 30), seals)
	if got.Seals != 1 {
		t.Fatalf("seals %d, want only the one before the end", got.Seals)
	}
	if got.MaxStaleness != 30*time.Minute || got.OverBound != 15*time.Minute {
		t.Fatalf("worst %s, over %s", got.MaxStaleness, got.OverBound)
	}
	fmt.Printf("audit: a stamp after the audited moment is ignored (%d seal counted)\n", got.Seals)
}
