package adapters

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// The file's presence is the signal, so absence is a reading (0) and not a gap: a calm job must not
// look blind, because an unknown job.ending is urgent (0013).
func TestEndingReadsTheFileAsTheSignal(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	p := presence{condition: "job.ending", file: "ending", every: time.Second}

	r := p.Read(produce.Env{Dir: dir, Now: t0})
	if r.State != produce.Fresh || r.Value {
		t.Fatalf("no ending file = %+v, want fresh 0", r)
	}

	if err := os.WriteFile(filepath.Join(base, "ending"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = p.Read(produce.Env{Dir: dir, Now: t0})
	if r.State != produce.Fresh || !r.Value {
		t.Fatalf("ending file present = %+v, want fresh 1", r)
	}
}

func TestMemoryPressureFiresAtTheEvictionLine(t *testing.T) {
	p := memoryPressure{every: time.Second}

	cases := []struct {
		name  string
		used  int64
		limit int64
		want  bool
	}{
		{"calm", 500, 1000, false},
		{"warn only", 900, 1000, false}, // 90% is a warning, not the eviction line
		{"at the line", 950, 1000, true},
		{"over", 999, 1000, true},
	}
	for _, c := range cases {
		p.read = func() (int64, int64, bool, error) { return c.used, c.limit, true, nil }
		r := p.Read(produce.Env{Now: t0})
		if r.State != produce.Fresh || r.Value != c.want {
			t.Fatalf("%s = %+v, want %v", c.name, r, c.want)
		}
	}
}

// No limit is an environment without this condition, not a blind one: a bare-metal job must not be
// reported as unable to see its memory.
func TestMemoryPressureIsAbsentWithoutALimit(t *testing.T) {
	p := memoryPressure{every: time.Second, read: func() (int64, int64, bool, error) {
		return 0, 0, false, nil
	}}
	if r := p.Read(produce.Env{Now: t0}); r.State != produce.Absent {
		t.Fatalf("no cgroup limit = %+v, want absent", r)
	}
}

func TestMemoryPressureIsUnknownWhenTheFilesFail(t *testing.T) {
	p := memoryPressure{every: time.Second, read: func() (int64, int64, bool, error) {
		return 0, 0, false, os.ErrPermission
	}}
	if r := p.Read(produce.Env{Now: t0}); r.State != produce.Unknown {
		t.Fatalf("unreadable cgroup = %+v, want unknown", r)
	}
}

// The real reader must not invent a condition on a machine without cgroup v2.
func TestCgroupMemoryIsHonestAboutThisMachine(t *testing.T) {
	_, _, ok, err := cgroupMemory()
	if err != nil {
		t.Skipf("this machine's cgroup files are not readable: %v", err)
	}
	if !ok {
		t.Log("no cgroup v2 limit here: absent is the right answer")
	}
}
