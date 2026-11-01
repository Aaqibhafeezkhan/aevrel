package main

import (
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce/adapters"
)

// The announcement's acceptance: a deletion timestamp is an announcement, and an announcement is a save — without
// any pressure in the world, because the platform has already decided.
func TestAnAnnouncementDecidesOnItsOwn(t *testing.T) {
	dir := t.TempDir()
	writeAnnouncedWorld(t, dir, true, 10)

	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, io.Discard); code != 0 {
		t.Fatalf("run exited %d", code)
	}

	d, ok := advise.LastDecision(dir)
	if !ok {
		t.Fatal("no plan was published")
	}
	if d.Action != policy.ActionSave || d.Reason != policy.ReasonRisk {
		t.Fatalf("decision = %s/%s, want save/risk", d.Action, d.Reason)
	}
	if len(d.Reasons) == 0 || d.Reasons[0].Code != "risk.preempt" {
		t.Fatalf("the decision does not name the announcement: %+v", d.Reasons)
	}
	fmt.Println("preempt 1 after deletionTimestamp → save")
}

// The same world without the timestamp is quiet: nothing is at risk, so nothing is written.
func TestAWorldWithoutAnAnnouncementStaysQuiet(t *testing.T) {
	dir := t.TempDir()
	writeAnnouncedWorld(t, dir, false, 10)

	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, io.Discard); code != 0 {
		t.Fatalf("run exited %d", code)
	}
	d, _ := advise.LastDecision(dir)
	if d.Action == policy.ActionSave {
		t.Fatalf("a calm world asked for a write: %+v", d)
	}
}

// writeAnnouncedWorld builds the smallest complete world: one summary, one unit record, and the
// platform's decision (or not) on that unit.
func writeAnnouncedWorld(t *testing.T, dir string, announced bool, cpuPct int) {
	t.Helper()
	cfg := adapters.Config{}.WithWorld(dir)
	now := time.Now().UTC()
	if err := adapters.WriteSummary(cfg.WorldDir, adapters.WorldSummary{
		Schema: 1, Cluster: "a", At: now, StaleAfterSeconds: 30,
		Capabilities: map[string]adapters.CapabilityWire{
			"preemption": {Exists: true, Source: "test"},
			"eviction":   {Exists: true, Source: "test"},
		},
		Nodes: []adapters.NodeState{{
			Name:        "worker-1",
			Allocatable: adapters.Resources{CPUMilli: 24000, MemBytes: 16 << 30},
			Requested:   adapters.Resources{CPUMilli: int64(24000 * cpuPct / 100)},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	u := adapters.UnitRecord{Cluster: "a", UnitID: cfg.UnitID, At: now, Node: "worker-1", Priority: 1000}
	if announced {
		d := now.Add(-time.Second)
		u.DeletionTimestamp = &d
	}
	if err := adapters.WriteUnit(cfg.WorldDir, "a", u); err != nil {
		t.Fatal(err)
	}
	if cfg.UnitID != filepath.Base(dir) {
		t.Fatalf("unit id = %q, want the directory name %q", cfg.UnitID, filepath.Base(dir))
	}
}
