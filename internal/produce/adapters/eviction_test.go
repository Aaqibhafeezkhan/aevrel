package adapters

import (
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// The fixtures below are the world, not a single file: a summary every observer writes, and a unit
// record per job. Tests that need "a full node" say so in one place, so every condition sees the same
// world when they assert about it.

func at(now time.Time, d time.Duration) time.Time { return now.Add(-d) }

func nowFn() time.Time { return time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC) }

type worldOpts struct {
	cpuPct     int
	memPct     int
	pending    int
	cordoned   bool
	noRequests bool
	tracking   bool
	announced  bool
	age        time.Duration
	noUnit     bool
}

func writeWorldFor(t *testing.T, opts worldOpts) Config {
	t.Helper()
	return writeWorldAt(t, opts, nowFn())
}

// writeWorldAt pins the clock, so the contract table can build a world that is fresh at the moment
// the harness says it is.
func writeWorldAt(t *testing.T, opts worldOpts, now time.Time) Config {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{}.WithWorld(dir)
	where := cfg.WorldDir
	summary := WorldSummary{
		Schema: 1, Cluster: "a", At: at(now, opts.age), StaleAfterSeconds: 30,
		Capabilities: map[string]CapabilityWire{
			"preemption": {Exists: true, Source: "test"},
			"eviction":   {Exists: true, Source: "test"},
		},
		Nodes: []NodeState{{
			Name:                  "worker-1",
			Allocatable:           Resources{CPUMilli: 24000, MemBytes: 16 << 30, GPUCount: 8},
			Requested:             Resources{CPUMilli: int64(24000 * opts.cpuPct / 100), MemBytes: int64((16 << 30) * int64(opts.memPct) / 100)},
			PendingHigherPriority: opts.pending,
			Cordoned:              opts.cordoned,
			RequestsUnknown:       opts.noRequests,
		}},
	}
	if err := WriteSummary(where, summary); err != nil {
		t.Fatal(err)
	}

	if opts.tracking && !opts.noUnit {
		u := UnitRecord{Cluster: "a", UnitID: cfg.UnitID, At: at(now, opts.age), Node: "worker-1",
			Priority: 1000, QoS: "Burstable"}
		if opts.announced {
			d := at(now, time.Second)
			u.DeletionTimestamp = &d
		}
		if err := WriteUnit(where, "a", u); err != nil {
			t.Fatal(err)
		}
	}
	// A tracking directory that holds some other unit: the observer tracks units, just not ours.
	if opts.tracking && opts.noUnit {
		if err := WriteUnit(where, "a", UnitRecord{Cluster: "a", UnitID: "someone/else", At: at(now, opts.age), Node: "worker-1"}); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func TestEvictionFiresWhenTheNodeIsFillingUp(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 92, memPct: 40, tracking: true})
	r := (evictionLikely{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()})
	if r.State != produce.Fresh || !r.Value {
		t.Fatalf("92%% = %+v, want the inference to fire", r)
	}
	sig, err := EvictionSignals(cfg.WorldDir, "a", cfg.UnitID, Deployment{}, 0.9, nowFn())
	if err != nil || len(sig) != 1 || sig[0] != "allocatable 92%" {
		t.Fatalf("signals = %v/%v", sig, err)
	}
}

// The trap this condition exists to avoid: a node whose pods declare no requests reads as 0%, and a
// full node would look empty. Requests that were never declared are not small requests.
func TestEvictionIsUnknownWhenRequestsWereNeverDeclared(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 0, noRequests: true, tracking: true})
	if r := (evictionLikely{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Unknown {
		t.Fatalf("no declared requests = %+v, want unknown", r)
	}
}

func TestEvictionFiresOnPendingPriorityPodsAndCordon(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 10, memPct: 10, pending: 2, cordoned: true, tracking: true})
	if r := (evictionLikely{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); !r.Value {
		t.Fatalf("pending+cordon = %+v, want 1", r)
	}
}

// A frozen observer is blindness, not calm.
func TestEvictionIsUnknownWhenTheObserverIsStale(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 10, memPct: 10, tracking: true, age: 10 * time.Minute})
	if r := (evictionLikely{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Unknown {
		t.Fatalf("stale = %+v, want unknown", r)
	}
}

// No observer at all, or one that does not track units: absent, because this deployment does not have
// the condition. It is not a fault and must not look like one.
func TestEvictionIsAbsentWithoutAnObserver(t *testing.T) {
	cfg := Config{}.WithWorld(t.TempDir())
	if r := (evictionLikely{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Absent {
		t.Fatalf("no observer = %+v, want absent", r)
	}

	summaryOnly := writeWorldFor(t, worldOpts{cpuPct: 10, tracking: false})
	if r := (evictionLikely{}).with(summaryOnly).Read(produce.Env{Dir: summaryOnly.WorldDir, Now: nowFn()}); r.State != produce.Absent {
		t.Fatalf("no unit tracking = %+v, want absent", r)
	}
}

// A deployment that tracks units and has lost mine is a different fact from one that never tracked
// them: the first is unknown, and unknown is urgent.
func TestEvictionIsUnknownWhenOurUnitIsMissing(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 10, tracking: true, noUnit: true})
	if r := (evictionLikely{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Unknown {
		t.Fatalf("missing unit = %+v, want unknown", r)
	}
}

// The guardrail: a switch off, or a declaration of no upstream, wins over a full node.
func TestADeclaredOffCapabilitySilencesTheInference(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 99, memPct: 99, pending: 5, cordoned: true, tracking: true})
	off := false
	cfg.InferEviction = &off
	if r := (evictionLikely{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Absent {
		t.Fatalf("switched off = %+v, want absent", r)
	}
	cfg.InferEviction = nil
	cfg.Deployment = Deployment{Capabilities: map[string]Capability{
		CapEviction: {Exists: false, Why: "systemd", Declared: true},
	}}
	if r := (evictionLikely{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Absent {
		t.Fatalf("declared off = %+v, want absent", r)
	}
}

// The announcement. A deletion timestamp is not a guess, and it must fire on its own.
func TestPreemptionFiresOnAnAnnouncement(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 10, memPct: 10, tracking: true, announced: true})
	if r := (preemption{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Fresh || !r.Value {
		t.Fatalf("announced = %+v, want 1", r)
	}
}

// Quiet means quiet: an observer that tracks units and says nothing about ours is a 0, not an unknown.
func TestPreemptionIsQuietWithoutAnAnnouncement(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 10, memPct: 10, tracking: true})
	if r := (preemption{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Fresh || r.Value {
		t.Fatalf("not announced = %+v, want 0", r)
	}
}

func TestPreemptionIsAbsentWithoutAWayToHear(t *testing.T) {
	cfg := Config{}.WithWorld(t.TempDir())
	if r := (preemption{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Absent {
		t.Fatalf("no observer = %+v, want absent", r)
	}

	summaryOnly := writeWorldFor(t, worldOpts{cpuPct: 10, tracking: false})
	if r := (preemption{}).with(summaryOnly).Read(produce.Env{Dir: summaryOnly.WorldDir, Now: nowFn()}); r.State != produce.Absent {
		t.Fatalf("no unit tracking = %+v, want absent", r)
	}
}

func TestPreemptionIsUnknownWhenTheObserverLostUs(t *testing.T) {
	cfg := writeWorldFor(t, worldOpts{cpuPct: 10, tracking: true, noUnit: true})
	if r := (preemption{}).with(cfg).Read(produce.Env{Dir: cfg.WorldDir, Now: nowFn()}); r.State != produce.Unknown {
		t.Fatalf("missing unit = %+v, want unknown", r)
	}
}
