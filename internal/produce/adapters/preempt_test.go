package adapters

import (
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// The two worlds a missing summary can mean. Telling them apart is the reason the declaration exists:
//
//   - nobody declared preemption: there is no announcement channel to miss, so "absent" is the truth
//     and a laptop deployment is not nagged about an observer it never needed.
//   - the deployment declared preemption upstream: the channel exists and is silent — an observer
//     that has not written yet, or has stopped. Calling that "absent" is claiming calm about a
//     platform we cannot see, which is the one failure this whole thing is built to avoid.
func TestAMissingWorldMeansDifferentThingsWithAndWithoutTheDeclaration(t *testing.T) {
	now := time.Now()
	empty := UnitView{SummaryPresent: false}

	if got := judgePreemption(empty, now, false); got.State != produce.Absent {
		t.Fatalf("undeclared, no observer: state = %q, want absent", got.State)
	}
	got := judgePreemption(empty, now, true)
	if got.State != produce.Unknown {
		t.Fatalf("declared, no observer: state = %q, want unknown — a silent channel is not calm", got.State)
	}
	if got.Err == nil {
		t.Fatal("declared, no observer: the reading must carry the reason it cannot see")
	}
}

// The same split for an observer that reports nodes but not units: with a declaration it is blind,
// without one it is a deployment that never had the channel.
func TestAnObserverThatTracksNoUnitsIsBlindOnlyWhenDeclared(t *testing.T) {
	now := time.Now()
	view := UnitView{SummaryPresent: true, Tracking: false}

	if got := judgePreemption(view, now, false); got.State != produce.Absent {
		t.Fatalf("undeclared, no unit tracking: state = %q, want absent", got.State)
	}
	if got := judgePreemption(view, now, true); got.State != produce.Unknown {
		t.Fatalf("declared, no unit tracking: state = %q, want unknown", got.State)
	}
}
