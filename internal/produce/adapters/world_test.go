package adapters

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixtureSummary(now time.Time) WorldSummary {
	return WorldSummary{
		Schema: 1, Cluster: "a", At: now, StaleAfterSeconds: 30,
		Capabilities: map[string]CapabilityWire{"preemption": {Exists: true, Source: "test"}},
		Nodes: []NodeState{
			{Name: "worker-1", Allocatable: Resources{CPUMilli: 24000, MemBytes: 16 << 30},
				Requested: Resources{CPUMilli: 22000, MemBytes: 8 << 30}, PendingHigherPriority: 2},
			{Name: "worker-2", Allocatable: Resources{CPUMilli: 8000}, Cordoned: true},
		},
	}
}

// The summary survives a round trip through the same code the observer will use, so a change to the
// writer cannot silently break the reader.
func TestSummaryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	if err := WriteSummary(dir, fixtureSummary(now)); err != nil {
		t.Fatal(err)
	}

	s, err := ReadSummary(dir, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes) != 2 || s.Nodes[0].Name != "worker-1" {
		t.Fatalf("nodes = %+v", s.Nodes)
	}
	if s.Nodes[0].PendingHigherPriority != 2 {
		t.Fatalf("pendingHigherPriority = %d", s.Nodes[0].PendingHigherPriority)
	}
	if !s.Nodes[1].Cordoned {
		t.Fatal("cordoned was lost")
	}
	if n, ok := s.Node("worker-2"); !ok || n.Name != "worker-2" {
		t.Fatalf("Node lookup = %+v/%v", n, ok)
	}
	if _, ok := s.Node("nope"); ok {
		t.Fatal("Node found a node that does not exist")
	}
}

// A frozen file is not calm: the condition layer must be able to ask this question and get "yes".
func TestStaleness(t *testing.T) {
	now := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	s := fixtureSummary(now.Add(-10 * time.Second))
	if s.Stale(now) {
		t.Fatal("10s old is fresh with a 30s limit")
	}
	if !fixtureSummary(now.Add(-2 * time.Minute)).Stale(now) {
		t.Fatal("2m old must be stale")
	}
	// A file that forgot to say, or said zero, gets the default rather than "never stale".
	s.StaleAfterSeconds = 0
	if !s.Stale(now.Add(2 * time.Minute)) {
		t.Fatal("a missing limit must fall back to the default")
	}
}

// A unit record that names a different unit is a hash collision or a rename. Either way it is not a
// match, and pretending otherwise would attach one job's risk to another.
func TestUnitRecordMustMatchItsID(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	if err := WriteUnit(dir, "a", UnitRecord{UnitID: "team/train-0", Cluster: "a", At: now, Node: "worker-1"}); err != nil {
		t.Fatal(err)
	}
	u, err := ReadUnit(dir, "a", "team/train-0")
	if err != nil {
		t.Fatal(err)
	}
	if u.Node != "worker-1" {
		t.Fatalf("node = %q", u.Node)
	}

	// Same file, different id asked for: the file still says the other one.
	raw, err := os.ReadFile(filepath.Join(dir, "a", "units", UnitFileName("team/train-0")))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "units", UnitFileName("team/train-1")), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadUnit(dir, "a", "team/train-1"); err == nil {
		t.Fatal("a record naming another unit was accepted")
	}
}

func TestUnitsAreListedByCluster(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	if err := WriteSummary(dir, fixtureSummary(now)); err != nil {
		t.Fatal(err)
	}
	b := fixtureSummary(now)
	b.Cluster = "b"
	if err := WriteSummary(dir, b); err != nil {
		t.Fatal(err)
	}
	if got := Clusters(dir); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("clusters = %v", got)
	}
}

// An empty directory is not an error: it is a deployment without an observer.
func TestNoWorldIsNotAnError(t *testing.T) {
	if _, err := ReadSummary(t.TempDir(), "a"); !os.IsNotExist(err) {
		t.Fatalf("err = %v, want a not-exist", err)
	}
	if got := Clusters(t.TempDir()); len(got) != 0 {
		t.Fatalf("clusters = %v", got)
	}
}
