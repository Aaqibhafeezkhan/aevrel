package adapters

import (
	"testing"
	"time"
)

func projectFixture(t *testing.T, staleBy time.Duration) (string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	at := now.Add(-staleBy)
	s := WorldSummary{
		Schema: 1, Cluster: "a", At: at, StaleAfterSeconds: 30,
		Capabilities: map[string]CapabilityWire{"eviction": {Exists: true, Source: "kubelet"}},
		Nodes: []NodeState{{
			Name:                  "worker-1",
			Allocatable:           Resources{CPUMilli: 24000, MemBytes: 16 << 30, GPUCount: 8},
			Requested:             Resources{CPUMilli: 22080, MemBytes: 8 << 30, GPUCount: 4},
			PendingHigherPriority: 2,
		}},
	}
	if err := WriteSummary(dir, s); err != nil {
		t.Fatal(err)
	}
	if err := WriteUnit(dir, "a", UnitRecord{
		Cluster: "a", UnitID: "team/train-0", At: at, Node: "worker-1",
		Priority: 1000, QoS: "Burstable",
	}); err != nil {
		t.Fatal(err)
	}
	return dir, now
}

// The projection flattens two files and a clock into the only shape the conditions may read.
func TestProjectionOfAFreshWorld(t *testing.T) {
	dir, now := projectFixture(t, 10*time.Second)

	v, err := Project(dir, "a", "team/train-0", Deployment{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !v.SummaryPresent || !v.UnitPresent || !v.RatiosKnown {
		t.Fatalf("presence = %+v", v)
	}
	if v.Node != "worker-1" || v.PendingHigherPriority != 2 {
		t.Fatalf("node = %q pending = %d", v.Node, v.PendingHigherPriority)
	}
	if got := int(v.CPUUse*100 + 0.5); got != 92 {
		t.Fatalf("cpu use = %d%%", got)
	}
	if got := int(v.MemUse*100 + 0.5); got != 50 {
		t.Fatalf("mem use = %d%%", got)
	}
	if v.Priority != 1000 || v.QoS != "Burstable" || v.Announced {
		t.Fatalf("unit facts = %+v", v)
	}
	if !v.Usable() {
		t.Fatal("a fresh, fully answered world must be usable")
	}
	if v.Line() != "a/worker-1 92% cpu, 2 higher-priority pending" {
		t.Fatalf("line = %q", v.Line())
	}
}

// A stale summary answers, but its answers may not be used: this is the difference between "calm"
// and "we have not looked recently".
func TestAStaleWorldIsReadableButNotUsable(t *testing.T) {
	dir, now := projectFixture(t, 2*time.Minute)

	v, err := Project(dir, "a", "team/train-0", Deployment{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Stale || v.Usable() {
		t.Fatalf("stale = %v usable = %v", v.Stale, v.Usable())
	}
	if want := "a/worker-1 92% cpu, 2 higher-priority pending (STALE 2m0s)"; v.Line() != want {
		t.Fatalf("line = %q, want %q", v.Line(), want)
	}
}

// The observer may track summaries and not units. That is a world where we know the node but not
// which unit is ours — and it must not be mistaken for "our node is fine".
func TestAnUntrackedUnitHasNoNode(t *testing.T) {
	dir, now := projectFixture(t, time.Second)

	v, err := Project(dir, "a", "team/other", Deployment{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !v.SummaryPresent || v.UnitPresent || v.Node != "" || v.Usable() {
		t.Fatalf("view = %+v", v)
	}
	if v.Line() != "a: unit not tracked" {
		t.Fatalf("line = %q", v.Line())
	}
}

// An announcement is a unit fact and survives the projection: it is the difference between "we
// think something might happen" and "the platform has already decided".
func TestAnAnnouncementSurvivesTheProjection(t *testing.T) {
	dir, now := projectFixture(t, time.Second)
	deleted := now.Add(-time.Second)
	if err := WriteUnit(dir, "a", UnitRecord{
		Cluster: "a", UnitID: "team/train-0", At: now, Node: "worker-1",
		DeletionTimestamp: &deleted,
	}); err != nil {
		t.Fatal(err)
	}
	v, err := Project(dir, "a", "team/train-0", Deployment{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Announced {
		t.Fatal("a deletion timestamp did not survive")
	}
}

// The operator's declaration beats the observer's: a cluster that claims an upstream the operator
// said does not exist must not re-enable the condition.
func TestTheOperatorWinsOverTheObserver(t *testing.T) {
	dir, now := projectFixture(t, time.Second)
	declared := Deployment{Capabilities: map[string]Capability{
		CapEviction: {Exists: false, Why: "operator says no", Declared: true},
	}}
	v, err := Project(dir, "a", "team/train-0", declared, now)
	if err != nil {
		t.Fatal(err)
	}
	if c := v.Capabilities[CapEviction]; c.Exists || c.Why != "operator says no" {
		t.Fatalf("eviction capability = %+v", c)
	}
	// And the observer's answer is used for the one the operator did not mention.
	if c := v.Capabilities[CapPreemption]; !c.Exists || c.Declared {
		t.Fatalf("preemption capability = %+v", c)
	}
}

// No observer at all: not an error, and every field says so rather than pretending zero.
func TestNoWorldProjectsToNothing(t *testing.T) {
	v, err := Project(t.TempDir(), "a", "team/train-0", Deployment{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if v.SummaryPresent || v.UnitPresent || v.Usable() {
		t.Fatalf("view = %+v", v)
	}
	if v.Line() != "no observer" {
		t.Fatalf("line = %q", v.Line())
	}
}
