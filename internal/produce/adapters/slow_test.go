package adapters

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce"
)

func TestSlowFiresAtTwiceTheJobsOwnBaseline(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, v string) {
		if err := os.WriteFile(filepath.Join(base, name), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := stepSlow{}.with(Config{})

	write("step-baseline", "100")
	write("step-rate", "150")
	if r := p.Read(produce.Env{Dir: dir, Now: t0}); r.Value {
		t.Fatalf("1.5x normal = %+v, want calm", r)
	}

	write("step-rate", "200")
	if r := p.Read(produce.Env{Dir: dir, Now: t0}); !r.Value {
		t.Fatalf("2x normal = %+v, want the diagnostic", r)
	}
}

// A job that never calibrated has no normal to compare against: that is an environment without the
// condition, not a blind one.
func TestSlowIsAbsentWithoutABaseline(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".ckpt-governor"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := storeSlow{}.with(Config{})
	if r := p.Read(produce.Env{Dir: dir, Now: t0}); r.State != produce.Absent {
		t.Fatalf("no baseline = %+v, want absent", r)
	}
}

func TestSlowIsUnknownWhenTheFileIsNotANumber(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "write-baseline"), []byte("fast"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "write-rate"), []byte("120"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := storeSlow{}.with(Config{})
	if r := p.Read(produce.Env{Dir: dir, Now: t0}); r.State != produce.Unknown {
		t.Fatalf("a baseline that is not a number = %+v, want unknown", r)
	}
}

// The one test that keeps this side honest: every condition the algorithm's catalogue names must be
// watched here, or be derived by the sampler, or be declared absent. A new catalogue row without a
// producer fails this, and that is the point.
func TestEveryCatalogueConditionHasAProducer(t *testing.T) {
	derived := map[string]bool{
		// The sampler derives these from the probes it already runs; they are not probes themselves.
		"watch.broken": true,
	}
	have := map[string]bool{}
	for _, p := range All(Config{}) {
		have[p.Name()] = true
	}

	var missing []string
	for _, c := range policy.Conditions() {
		if derived[c.Name] {
			continue
		}
		if !have[c.Name] {
			missing = append(missing, c.Name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("catalogue conditions with no producer: %v", missing)
	}

	catalogue := map[string]bool{}
	for _, c := range policy.Conditions() {
		catalogue[c.Name] = true
	}
	for name := range have {
		if !catalogue[name] {
			t.Errorf("%s is watched but is not in the catalogue", name)
		}
	}
}

var _ = time.Second
