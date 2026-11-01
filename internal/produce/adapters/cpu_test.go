package adapters

import (
	"os"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// Throttling is a ratio of cumulative counters, so one reading is enough: no history, no state, and
// a job that has never been throttled says 0 forever.
func TestCPUThrottledIsARatioOfTheQuotaLost(t *testing.T) {
	p := cpuThrottled{every: time.Second}
	cases := []struct {
		name      string
		throttled int64
		used      int64
		want      bool
	}{
		{"never throttled", 0, 1_000_000, false},
		{"a blip", 5000, 1_000_000, false},    // 0.5%
		{"binding", 300_000, 1_000_000, true}, // 30%
		{"half the time", 1, 1, true},         // 100%
	}
	for _, c := range cases {
		p.read = func() (int64, int64, bool, error) { return c.throttled, c.used, true, nil }
		r := p.Read(produce.Env{Now: t0})
		if r.State != produce.Fresh || r.Value != c.want {
			t.Fatalf("%s = %+v, want %v", c.name, r, c.want)
		}
	}
}

// A job with no CPU quota cannot be throttled by one: that is absent, not calm.
func TestCPUThrottledIsAbsentWithoutAController(t *testing.T) {
	p := cpuThrottled{every: time.Second, read: func() (int64, int64, bool, error) {
		return 0, 0, false, nil
	}}
	if r := p.Read(produce.Env{Now: t0}); r.State != produce.Absent {
		t.Fatalf("no cpu controller = %+v, want absent", r)
	}
}

func TestCPUThrottledIsUnknownWhenTheStatsFail(t *testing.T) {
	p := cpuThrottled{every: time.Second, read: func() (int64, int64, bool, error) {
		return 0, 0, false, os.ErrPermission
	}}
	if r := p.Read(produce.Env{Now: t0}); r.State != produce.Unknown {
		t.Fatalf("unreadable cpu.stat = %+v, want unknown", r)
	}
}
