package bound

import (
	"fmt"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// sealsAt builds a history of one copy every period, from a fixed origin.
func sealsAt(period time.Duration, n int) []policy.Copy {
	origin := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	out := make([]policy.Copy, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, policy.Copy{
			Step:      int64(i) * 100,
			Bytes:     300 * 1000 * 1000,
			State:     policy.CopySealed,
			CreatedAt: origin.Add(time.Duration(i) * period),
		})
	}
	return out
}

// The habit is the median gap, and the bound the history proves is the longest gap. Averaging the
// worst day away would produce a bound the job has already broken.
func TestTheHistoryProvesAPeriodAndABound(t *testing.T) {
	copies := sealsAt(10*time.Minute, 8)
	// One incident: the job went 25 minutes without a seal.
	copies[4].CreatedAt = copies[3].CreatedAt.Add(25 * time.Minute)
	for i := 5; i < len(copies); i++ {
		copies[i].CreatedAt = copies[4].CreatedAt.Add(time.Duration(i-4) * 10 * time.Minute)
	}
	got, ok := Observe(copies, []float64{28, 30, 31, 33, 30})
	if !ok {
		t.Fatal("eight seals must be enough to read a habit")
	}
	if got.Period != 10*time.Minute {
		t.Fatalf("period %s, want the 10m habit", got.Period)
	}
	if got.Longest != 25*time.Minute {
		t.Fatalf("longest staleness %s, want the 25m incident", got.Longest)
	}
	if got.WriteCost < 30*time.Second || got.WriteCost > 34*time.Second {
		t.Fatalf("write cost %s, want about the p90 of the samples", got.WriteCost)
	}
	fmt.Printf("bound: 9 seals · habit %s · worst the job lived with %s · write cost %s\n",
		got.Period, got.Longest, got.WriteCost.Round(time.Second))
}

// A half-written copy is not evidence, and two copies at the same moment say nothing: both are the
// difference between "measured" and "invented".
func TestOnlyFinishedCopiesWithKnownTimesCount(t *testing.T) {
	copies := sealsAt(10*time.Minute, 3)
	copies = append(copies,
		policy.Copy{Step: 400, State: policy.CopyUnverified, CreatedAt: copies[2].CreatedAt.Add(20 * time.Minute)},
		policy.Copy{Step: 500, State: policy.CopyBroken, CreatedAt: copies[2].CreatedAt.Add(40 * time.Minute)},
		policy.Copy{Step: 600, State: policy.CopySealed}, // no time at all
	)
	got, ok := Observe(copies, nil)
	if !ok {
		t.Fatal("three sealed copies with times are enough")
	}
	if got.Copies != 3 || got.Period != 10*time.Minute || got.Longest != 10*time.Minute {
		t.Fatalf("got %+v: unsealed or timeless copies leaked into the measurement", got)
	}
	if got.WriteCost != 0 {
		t.Fatalf("write cost %s with no samples, want unknown", got.WriteCost)
	}
	// Two copies with the same timestamp are not a period.
	same := []policy.Copy{
		{Step: 1, State: policy.CopySealed, CreatedAt: time.Unix(100, 0)},
		{Step: 1, State: policy.CopySealed, CreatedAt: time.Unix(100, 0)},
	}
	if _, ok := Observe(same, nil); ok {
		t.Fatal("identical timestamps produced a period")
	}
	fmt.Printf("bound: one sealed copy is not a history (no period, no bound)\n")
}

// The measured bound is the number the price sheet should be built from, so the two halves have to
// agree on one example end to end: a 10m habit with a 30s write buys a 10m30s bound at 4.8%.
func TestTheMeasuredHabitIsWhatThePriceSheetPrices(t *testing.T) {
	copies := sealsAt(10*time.Minute, 6)
	got, ok := Observe(copies, []float64{30, 30, 30})
	if !ok {
		t.Fatal("six seals must be enough")
	}
	bound := got.Longest + got.WriteCost
	priced := Price(got.WriteCost, 4*time.Hour, bound)
	if !priced.Known || !priced.Feasible {
		t.Fatalf("the derived bound is not priceable: %q", priced.Why)
	}
	if priced.Writes != 22 {
		t.Fatalf("writes %d at a 10m30s bound in 4h, want 22", priced.Writes)
	}
	if priced.Tax < 0.045 || priced.Tax > 0.050 {
		t.Fatalf("tax %.3f%% at a derived bound, want about 4.8%%", priced.Tax*100)
	}
	fmt.Printf("bound: habit 10m + 30s write → bound 10m30s · 22 writes in 4h · %.1f%% of the run\n", priced.Tax*100)
}
