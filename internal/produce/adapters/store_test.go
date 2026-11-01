package adapters

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

const gb = int64(1) << 30

func TestLowSpaceFiresBelowTwiceTheCheckpoint(t *testing.T) {
	free := 3 * gb
	p := lowSpace{every: time.Second, factor: 2, stat: func(string) (int64, int64, error) {
		return free, 100 * gb, nil
	}}

	r := p.Read(produce.Env{Now: t0, CheckpointBytes: 1 * gb})
	if r.State != produce.Fresh || r.Value {
		t.Fatalf("3 GB free, 1 GB checkpoint = %+v, want calm", r)
	}

	free = 1900 * 1000 * 1000 // 1.9 GB, below 2×1 GB
	r = p.Read(produce.Env{Now: t0, CheckpointBytes: 1 * gb})
	if r.State != produce.Fresh || !r.Value {
		t.Fatalf("1.9 GB free, 1 GB checkpoint = %+v, want the trigger", r)
	}
}

// A rule about "2× one checkpoint" has no meaning before there is a checkpoint. Saying 1 there would
// ask for a write on an empty disk; saying 0 is the only answer that does not invent a scale.
func TestLowSpaceSaysNothingWithoutACheckpoint(t *testing.T) {
	p := lowSpace{every: time.Second, factor: 2, stat: func(string) (int64, int64, error) {
		return 1, 100 * gb, nil
	}}
	r := p.Read(produce.Env{Now: t0})
	if r.State != produce.Fresh || r.Value {
		t.Fatalf("no checkpoint = %+v, want calm", r)
	}
}

func TestLowSpaceIsUnknownWhenTheStatFails(t *testing.T) {
	p := lowSpace{every: time.Second, factor: 2, stat: func(string) (int64, int64, error) {
		return 0, 0, errors.New("statfs: permission denied")
	}}
	r := p.Read(produce.Env{Now: t0, CheckpointBytes: 1})
	if r.State != produce.Unknown || r.Err == nil {
		t.Fatalf("a failed stat = %+v, want unknown with the reason", r)
	}
}

// blocked is a diagnostic: it must be able to say "yes" (the target refuses) and "no" (it does not),
// and a directory that does not exist yet is not a refusal.
func TestBlockedMapsTheProbeAnswer(t *testing.T) {
	refused := errors.New("read-only file system")
	p := blocked{every: time.Second, probe: func(string) error { return refused }}
	if r := p.Read(produce.Env{Now: t0}); !r.Value || r.State != produce.Fresh {
		t.Fatalf("a refused target = %+v, want blocked", r)
	}

	p.probe = func(string) error { return nil }
	if r := p.Read(produce.Env{Now: t0}); r.Value {
		t.Fatalf("a writable target = %+v, want calm", r)
	}

	p.probe = func(string) error { return os.ErrNotExist }
	if r := p.Read(produce.Env{Now: t0}); r.Value {
		t.Fatalf("a missing directory = %+v, want calm (the job has not started)", r)
	}
}

func TestWritableSucceedsOnARealDirectory(t *testing.T) {
	if err := writable(t.TempDir()); err != nil {
		t.Fatalf("a fresh directory should be writable: %v", err)
	}
}

// The registry is the one place a build says what it can watch. A duplicate name or a zero cadence
// would make the sampler silently useless.
func TestAllDeclaresUsableProbes(t *testing.T) {
	probes := All(Config{})
	if len(probes) == 0 {
		t.Fatal("no conditions declared")
	}
	seen := map[string]bool{}
	for _, p := range probes {
		if seen[p.Name()] {
			t.Fatalf("%s is declared twice", p.Name())
		}
		seen[p.Name()] = true
		if p.Every() <= 0 {
			t.Fatalf("%s has no cadence", p.Name())
		}
	}
}
