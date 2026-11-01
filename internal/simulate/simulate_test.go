package simulate

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// synthetic builds a trace of a run: a step every stepEvery, a completed write every saveEvery
// taking saveCost. The writes are the environment's own history; the replay only reads their cost.
func synthetic(d, stepEvery, saveEvery, saveCost time.Duration) Trace {
	var ev []Event
	ev = append(ev, Event{T: t0, Kind: KindFree, Bytes: 1 << 40})
	for t, step := t0, int64(0); t.Before(t0.Add(d)); t, step = t.Add(stepEvery), step+1 {
		ev = append(ev, Event{T: t, Kind: KindStep, Step: step})
	}
	// The first write starts at t0: without at least one completed write early, the replay plans
	// with an unknown write cost and, correctly, falls back to half the bound. That is a different
	// experiment, not the one this trace is for.
	for t := t0; t.Before(t0.Add(d)); t = t.Add(saveEvery) {
		ev = append(ev,
			Event{T: t, Kind: KindSaveStart},
			Event{T: t.Add(saveCost), Kind: KindSaveEnd, Sealed: true},
		)
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].T.Before(ev[j].T) })
	return Trace{Events: ev}
}

func TestAFixedTimerThatLeavesRoomForTheWriteKeepsTheBound(t *testing.T) {
	// A write takes 30s, so a timer that starts every 9m30s still seals inside a 10-minute bound.
	// The extra 30s of the period is what the write costs; the tax is therefore W/(L-W), a little
	// over the naive W/L. This is the simulator's control case, not a policy claim.
	tr := synthetic(2*time.Hour, 2*time.Second, 30*time.Minute, 30*time.Second)
	bounds := policy.Bounds{MinCopies: 1}

	res, err := Run(tr, Options{Bounds: bounds, FixedPeriod: 9*time.Minute + 30*time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Violation > 1e-9 {
		t.Fatalf("violation = %.4f, want 0 (the write still fits the bound)", res.Violation)
	}
	if res.Tax < 0.05 || res.Tax > 0.056 {
		t.Fatalf("tax = %.4f, want about 5.3%% (30s of write per 9m30s)", res.Tax)
	}
}

func TestAFixedThirtyMinuteTimerBreaksTheBound(t *testing.T) {
	tr := synthetic(2*time.Hour, 2*time.Second, 30*time.Minute, 30*time.Second)
	bounds := policy.Bounds{MinCopies: 1}
	// The yardstick is the user's own number: a 30-minute timer against a 10-minute bound is the
	// overrun the whole product is about, so the evaluation has to name it.
	eval := EvalBounds{MaxLostWork: 10 * time.Minute}

	res, err := Run(tr, Options{Bounds: bounds, Eval: eval, FixedPeriod: 30 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if res.Violation < 0.6 {
		t.Fatalf("violation = %.4f, want above 0.6 (two thirds of every half hour is out of contract)", res.Violation)
	}
}

func TestRunIsDeterministic(t *testing.T) {
	tr := synthetic(time.Hour, 2*time.Second, 30*time.Minute, 30*time.Second)
	opts := Options{
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Policy: Boundary(policy.Risk{}),
	}
	first, err := Run(tr, opts)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := Run(tr, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("the same trace produced two answers:\n%+v\n%+v", first, again)
		}
	}
}

func TestTheReplayStopsWhenTheWorldEnds(t *testing.T) {
	// A policy that saves all the time asks for a write at the trace's last moment. That write
	// cannot complete, and asking again at the same instant cannot change the answer, so the
	// replay must end: before this was checked it repeated the truncated save until the 1,000,000
	// iteration cap, which made a 40-minute trace take seconds and counted a million truncations.
	tr := synthetic(time.Hour, 2*time.Second, 30*time.Minute, 30*time.Second)
	res, err := Run(tr, Options{
		Bounds: policy.Bounds{MinCopies: 1},
		Policy: Always{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated > 1 {
		t.Fatalf("truncated = %d, want at most one cut-off write at the end", res.Truncated)
	}
	if again, err := Run(tr, Options{
		Bounds: policy.Bounds{MinCopies: 1},
		Policy: Always{},
	}); err != nil || res.Saves != again.Saves {
		t.Fatalf("the end of the trace is not deterministic: %d then %d saves", res.Saves, again.Saves)
	}
}

func TestATraceWithoutACompletedWriteIsRefused(t *testing.T) {
	tr := Trace{Events: []Event{{T: t0, Kind: KindStep, Step: 1}}}
	if _, err := Run(tr, Options{Bounds: policy.Bounds{MinCopies: 1}, Eval: EvalBounds{MaxLostWork: time.Minute}}); err == nil {
		t.Fatal("a trace with no completed write cannot say what a write costs")
	}
}

func TestParseNamesTheBrokenLine(t *testing.T) {
	text := `{"t":"2026-01-01T00:00:00Z","event":"step","step":1}
{"t":"2026-01-01T00:00:02Z","event":"nonsense"}
`
	_, err := Parse(strings.NewReader(text))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error = %v, want it to name line 2", err)
	}
}

func TestParseReadsTheFiveEvents(t *testing.T) {
	text := `{"t":"2026-01-01T00:00:00Z","event":"free","bytes":10}
{"t":"2026-01-01T00:00:01Z","event":"step","step":7}
{"t":"2026-01-01T00:00:02Z","event":"save_start","step":7}
{"t":"2026-01-01T00:00:32Z","event":"save_end","step":7,"bytes":100,"sealed":true}
{"t":"2026-01-01T00:00:40Z","event":"kill"}
`
	tr, err := Parse(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != 5 {
		t.Fatalf("events = %d, want 5", len(tr.Events))
	}
	if tr.Events[3].Bytes != 100 || !tr.Events[3].Sealed {
		t.Fatalf("save_end = %+v", tr.Events[3])
	}
}
