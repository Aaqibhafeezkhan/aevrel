package advise

import (
	"testing"
	"time"
)

func TestLedgerSurvivesARoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Epoch = 3
	l.WriteSecondsTotal = 42.5
	l.BootID = "boot-1"
	l.Generation = 2
	l.AddWriteCost(30)
	l.AddStepTime(2)
	l.Observe("step-8", t0)

	if err := l.Save(dir); err != nil {
		t.Fatal(err)
	}
	back, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if back.Epoch != 3 || back.WriteSecondsTotal != 42.5 || back.BootID != "boot-1" || back.Generation != 2 {
		t.Fatalf("round trip lost data: %+v", back)
	}
	if len(back.WriteCosts) != 1 || back.WriteCosts[0] != 30 {
		t.Fatalf("write costs = %v, want [30]", back.WriteCosts)
	}
	if got := back.Seen()["step-8"]; !got.Equal(t0) {
		t.Fatalf("first sighting = %s, want %s", got, t0)
	}
}

// A write's cost is "seal seen minus first seen". If the start moved on every round, every write
// would look instantaneous.
func TestObserveKeepsTheFirstSighting(t *testing.T) {
	l, _ := Load(t.TempDir())
	if !l.Observe("step-8", t0) {
		t.Fatal("the first sighting must report that it was new")
	}
	if l.Observe("step-8", t0.Add(time.Minute)) {
		t.Fatal("a second sighting must not report that it was new")
	}
	if got := l.Seen()["step-8"]; !got.Equal(t0) {
		t.Fatalf("first sighting = %s, want %s", got, t0)
	}
}

func TestSamplesKeepTheWindowBounded(t *testing.T) {
	l, _ := Load(t.TempDir())
	for i := 0; i < sampleWindow+5; i++ {
		l.AddWriteCost(float64(i))
	}
	if len(l.WriteCosts) != sampleWindow {
		t.Fatalf("window = %d, want %d", len(l.WriteCosts), sampleWindow)
	}
	if l.WriteCosts[len(l.WriteCosts)-1] != float64(sampleWindow+4) {
		t.Fatalf("the newest sample was dropped: %v", l.WriteCosts)
	}
}

func TestLoadOfAMissingLedgerIsEmptyNotAnError(t *testing.T) {
	l, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("a missing ledger must not be an error: %v", err)
	}
	if l.FirstSeen == nil {
		t.Fatal("FirstSeen must be usable without a save")
	}
}
