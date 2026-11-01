package bound

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/exec"
)

// The price is one write time per unit of staleness, and the run's length cannot change it. That
// independence is the whole reason the number can be carried from one job to another, so it is
// asserted rather than assumed.
func TestTheTaxIsOneWriteTimePerUnitOfStaleness(t *testing.T) {
	mount := 30 * time.Second
	for _, c := range []struct {
		bound time.Duration
		want  float64
	}{
		{5 * time.Minute, 0.1},
		{10 * time.Minute, 0.05},
		{50 * time.Minute, 0.01},
		{time.Hour, 30.0 / 3600.0},
	} {
		got := Price(mount, 4*time.Hour, c.bound)
		if !got.Known || !got.Feasible {
			t.Fatalf("%s: not priced (%q)", c.bound, got.Why)
		}
		if math.Abs(got.Tax-c.want) > 1e-9 {
			t.Fatalf("%s: tax %.6f, want %.6f", c.bound, got.Tax, c.want)
		}
	}
	shortRun := Price(mount, time.Hour, 10*time.Minute)
	longRun := Price(mount, 24*time.Hour, 10*time.Minute)
	if shortRun.Tax != longRun.Tax {
		t.Fatalf("the tax depends on the run length: %v vs %v", shortRun.Tax, longRun.Tax)
	}
	if shortRun.Writes != 6 || longRun.Writes != 144 {
		t.Fatalf("writes %d and %d, want 6 and 144", shortRun.Writes, longRun.Writes)
	}
	fmt.Printf("bound: 30s of writing per 10m held = 5.0%% of the run, whatever its length (%d vs %d writes)\n",
		shortRun.Writes, longRun.Writes)
}

// A tax budget is a bound, solved the other way: at most 1% of the run on writes means no tighter
// than fifty minutes on this mount. It is the question a platform team asks, and it has an answer.
func TestATaxBudgetOnlyAllowsLooseBounds(t *testing.T) {
	mount := 30 * time.Second
	for _, c := range []struct {
		tax  float64
		want time.Duration
	}{
		{0.01, 50 * time.Minute},
		{0.05, 10 * time.Minute},
		{0.5, time.Minute},
	} {
		got, ok := Floor(mount, c.tax)
		if !ok || got != c.want {
			t.Fatalf("floor at %.0f%% = %s (ok=%v), want %s", c.tax*100, got, ok, c.want)
		}
	}
	if _, ok := Floor(0, 0.01); ok {
		t.Fatal("an unknown write cost must not produce a bound")
	}
	if _, ok := Floor(mount, 0); ok {
		t.Fatal("a zero budget is not a budget")
	}
	if _, ok := Floor(mount, 1.5); ok {
		t.Fatal("a budget above the whole run is not a budget")
	}
	fmt.Printf("bound: W=30s · 1%% → 50m · 5%% → 10m · 50%% → 1m\n")
}

// A bound shorter than one write cannot hold, and it is refused rather than priced: the executor
// says the same thing ("this bound cannot hold"), and a price sheet must not sell it.
func TestABoundShorterThanOneWriteCannotHold(t *testing.T) {
	mount := 30 * time.Second
	for _, bound := range []time.Duration{29 * time.Second, 30 * time.Second} {
		got := Price(mount, time.Hour, bound)
		if got.Feasible || !got.Known {
			t.Fatalf("%s: feasible=%v known=%v, want known but infeasible", bound, got.Feasible, got.Known)
		}
		if !strings.Contains(got.Why, "shorter than one write") {
			t.Fatalf("%s: why = %q", bound, got.Why)
		}
	}
	if got := Price(mount, time.Hour, 31*time.Second); !got.Feasible {
		t.Fatalf("31s is one second longer than a write and must be feasible: %q", got.Why)
	}
	fmt.Printf("bound: a 30s write makes a 30s bound infeasible (%s)\n", Price(mount, time.Hour, 30*time.Second).Why)
}

// No measurement, no price. This is the same rule the controller follows for its estimates: an
// unknown cost never becomes a plausible number.
func TestAnUnknownWriteCostIsNotPriced(t *testing.T) {
	for _, mount := range []time.Duration{0, -time.Second} {
		got := Price(mount, 4*time.Hour, 15*time.Minute)
		if got.Known || got.Feasible || got.Tax != 0 || got.Writes != 0 {
			t.Fatalf("%s: priced an unknown cost: %+v", mount, got)
		}
		if !strings.Contains(got.Why, "unknown") {
			t.Fatalf("%s: why = %q", mount, got.Why)
		}
	}
	// A window that is off is a different promise, not a zero price.
	if got := Price(30*time.Second, 4*time.Hour, 0); got.Known || got.Feasible {
		t.Fatalf("a zero bound must be reported as exceptions only: %+v", got)
	}
	fmt.Printf("bound: unknown write cost → not priced (%s)\n", Price(0, 4*time.Hour, 15*time.Minute).Why)
}

// The price has to describe the window the executor actually keeps, or it is a different product's
// arithmetic. The due date is the link: seal plus span minus one write, so the write lands at the
// span, and the run spends mount/span of its time writing.
func TestThePriceMatchesTheWindowTheExecutorKeeps(t *testing.T) {
	mount, span := 30*time.Second, 15*time.Minute
	seal := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	w := exec.NewWindow(span)
	w.Arm(seal, mount)
	if got, want := w.DueAt(), seal.Add(span-mount); got != want {
		t.Fatalf("due %s, want %s", got, want)
	}
	if w.Infeasible() {
		t.Fatal("a 15m window with a 30s write is feasible")
	}
	priced := Price(mount, 4*time.Hour, span)
	if !priced.Feasible || math.Abs(priced.Tax-float64(mount)/float64(span)) > 1e-9 {
		t.Fatalf("price %+v disagrees with the window's lead %s", priced, w.Lead())
	}
	// The infeasible edge is the same edge in both places.
	edge := exec.NewWindow(mount)
	edge.Arm(seal, mount)
	if !edge.Infeasible() {
		t.Fatal("the executor must call a span equal to one write infeasible")
	}
	if Price(mount, time.Hour, mount).Feasible {
		t.Fatal("the price sheet must refuse a bound the executor calls infeasible")
	}
	fmt.Printf("bound: 15m held with a 30s write → due at seal+14m30s, %.1f%% of the run; both sides refuse a 30s bound\n",
		priced.Tax*100)
}
