package simulate

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// jobBounds gives each family the bound its owner would ask for, matching cmd/ckptctl/table.go.
func jobBounds(fam Family) (policy.Bounds, EvalBounds) {
	b, e := DefaultBounds(), DefaultEval()
	switch fam {
	case Tiny:
		e.MaxLostWork = 10 * time.Minute
	case Big:
		e.MaxLostWork = 45 * time.Minute
	case Capacity:
		b.MinCopies = 1
	case Delta, DeltaStorm, SmallTeam, SmallTeamLatency, SmallTeamShards:
		e.MaxLostWork = 10 * time.Minute
		e.MaxTax = 0.05
	}
	return b, e
}

func TestGeneratedTracesAreDeterministic(t *testing.T) {
	a, _, err := Generate(7, Killed)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := Generate(7, Killed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same seed produced two different traces")
	}

	_, envA, _ := Generate(7, Killed)
	_, envB, _ := Generate(8, Killed)
	if envA.Kills[0] == envB.Kills[0] {
		t.Fatalf("two seeds produced the same kill time: %s", envA.Kills[0])
	}
}

func TestGeneratedTraceSurvivesARoundTrip(t *testing.T) {
	tr, _, err := Generate(1, Steady)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Encode(tr, &buf); err != nil {
		t.Fatal(err)
	}
	back, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Events) != len(tr.Events) {
		t.Fatalf("encoded %d events, parsed %d", len(tr.Events), len(back.Events))
	}
	for i := range tr.Events {
		if !back.Events[i].T.Equal(tr.Events[i].T) || back.Events[i].Kind != tr.Events[i].Kind {
			t.Fatalf("event %d changed: %+v -> %+v", i, tr.Events[i], back.Events[i])
		}
	}
}

func TestThePolicyKeepsThePromiseOnEveryFamily(t *testing.T) {
	// The risk core has no interval to keep: its proof is the truth table in internal/policy. What
	// this test checks is that the replay can drive it at all — with a modelled warning before each
	// recorded kill and before the end of the trace, it writes at those moments and nowhere else.
	ours := Boundary(policy.Risk{})
	for _, fam := range Families() {
		tr, env, err := Generate(1, fam)
		if err != nil {
			t.Fatal(err)
		}
		b, e := jobBounds(fam)
		res, err := Run(tr, Options{Bounds: b, Eval: e, Policy: ours, CostAt: env.WriteCost, DeltaShare: env.DeltaShare, Model: env.Model, WarnBefore: 90 * time.Second})
		if err != nil {
			t.Fatalf("%s: %v", fam, err)
		}
		// One write per warning, and one at the end: more means it fired on something else.
		if res.Saves > len(env.Kills)+1 {
			t.Fatalf("%s: %d writes for %d kills", fam, res.Saves, len(env.Kills))
		}
		if res.Infeasible > 0 {
			t.Fatalf("%s: refused %d times, but the risk core has nothing to refuse", fam, res.Infeasible)
		}
	}
}

func TestSavingEveryBoundBreaksTheBound(t *testing.T) {
	tr, _, err := Generate(1, Steady)
	if err != nil {
		t.Fatal(err)
	}
	b, e := jobBounds(Steady)
	res, err := Run(tr, Options{Bounds: b, Eval: e, Policy: FixedAtBound{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Violation < 0.005 {
		t.Fatalf("violation = %.2f%%, want the one-write overrun to be visible", res.Violation*100)
	}
}

func TestAShortFixedTimerBreaksTheTaxBudget(t *testing.T) {
	tr, _, err := Generate(1, Steady)
	if err != nil {
		t.Fatal(err)
	}
	b, e := jobBounds(Steady)
	res, err := Run(tr, Options{Bounds: b, Eval: e, Policy: FixedPeriod{Period: 5 * time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Violation > 0.005 {
		t.Fatalf("violation = %.2f%%, want a 5m timer to keep the age bound", res.Violation*100)
	}
	if res.Tax <= e.MaxTax+0.01 {
		t.Fatalf("tax = %.2f%%, want the 5m timer to spend more than the budget", res.Tax*100)
	}
}

func TestWindowsCoverTheRun(t *testing.T) {
	tr, _, err := Generate(1, Steady)
	if err != nil {
		t.Fatal(err)
	}
	b, e := jobBounds(Steady)
	res, err := Run(tr, Options{Bounds: b, Eval: e, FixedPeriod: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.WindowViolation) != len(res.WindowTax) {
		t.Fatalf("windows: %d violation, %d tax", len(res.WindowViolation), len(res.WindowTax))
	}
	if len(res.WindowViolation) < 2 {
		t.Fatalf("windows = %d, want at least 2", len(res.WindowViolation))
	}
}

func TestBestFixedStaysInsideTheBudget(t *testing.T) {
	tr, _, err := Generate(1, Drift)
	if err != nil {
		t.Fatal(err)
	}
	b, e := jobBounds(Drift)
	ctls := Grid()
	row, err := Score("drift-1", tr, ctls, Options{Bounds: b, Eval: e})
	if err != nil {
		t.Fatal(err)
	}
	if row.BestFixed <= 0 {
		t.Fatal("no best fixed period was found")
	}
	if row.BestFixedV > 0.5 {
		t.Fatalf("best fixed violates %.1f%%, so nothing inside the budget keeps the bound", row.BestFixedV*100)
	}
}
