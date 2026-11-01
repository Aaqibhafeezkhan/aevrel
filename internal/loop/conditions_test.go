package loop

import (
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// A condition that keeps saying 1 must keep its original "since": the age is the point, and a
// reading that refreshes it every round would make every condition look like it just flipped.
func TestSinceOnlyMovesWhenTheValueChanges(t *testing.T) {
	dir := t.TempDir()
	now := t0
	probe := &fakeProbe{name: "risk.shutdown", every: time.Second, state: produce.Fresh, value: true}
	l := openWith(t, dir, &now, probe)

	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	first := l.Ledger().Status.Conditions["risk.shutdown"]
	if first.Value != "1" || first.Since != now.Unix() {
		t.Fatalf("first reading = %+v", first)
	}

	now = now.Add(2 * time.Minute)
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	if got := l.Ledger().Status.Conditions["risk.shutdown"]; got.Since != first.Since {
		t.Fatalf("since moved while the value did not: %+v", got)
	}

	probe.value = false
	now = now.Add(time.Minute)
	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	if got := l.Ledger().Status.Conditions["risk.shutdown"]; got.Value != "0" || got.Since != now.Unix() {
		t.Fatalf("after the flip = %+v", got)
	}
}

// An unreadable condition must carry the probe's error into the status: "unknown" with no reason is
// the least useful line a person can be shown.
func TestAnUnreadableConditionCarriesTheReason(t *testing.T) {
	dir := t.TempDir()
	now := t0
	probe := &fakeProbe{name: "store.low-space", every: time.Second, state: produce.Unknown}
	l := openWith(t, dir, &now, probe)

	if _, err := l.Round(); err != nil {
		t.Fatal(err)
	}
	c := l.Ledger().Status.Conditions["store.low-space"]
	if c.Value != "unknown" || c.Reason == "" {
		t.Fatalf("unknown condition = %+v, want a reason", c)
	}
}
