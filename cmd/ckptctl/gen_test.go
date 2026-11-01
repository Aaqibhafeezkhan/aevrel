package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/simulate"
)

func genFile(t *testing.T, args ...string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	all := append([]string{"--out=" + path}, args...)
	if code := runGen(all, io.Discard); code != 0 {
		t.Fatalf("gen exited %d", code)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The same seed must produce the same world, byte for byte: a table that cannot be regenerated is
// not a measurement.
func TestGenIsReproducible(t *testing.T) {
	a := genFile(t, "--seed=7", "--duration=1h")
	b := genFile(t, "--seed=7", "--duration=1h")
	if string(a) != string(b) {
		t.Fatal("the same seed produced two different traces")
	}
	c := genFile(t, "--seed=8", "--duration=1h")
	if string(a) == string(c) {
		t.Fatal("a different seed produced the same trace")
	}
}

func TestGenProducesATraceTheSimulatorAccepts(t *testing.T) {
	raw := genFile(t, "--seed=3", "--duration=1h", "--kills=2")
	tr, err := simulate.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the generated trace does not parse: %v", err)
	}
	kinds := map[simulate.Kind]int{}
	for _, e := range tr.Events {
		kinds[e.Kind]++
	}
	for _, want := range []simulate.Kind{simulate.KindStep, simulate.KindSaveStart, simulate.KindSaveEnd, simulate.KindFree, simulate.KindKill} {
		if kinds[want] == 0 {
			t.Fatalf("the trace has no %s event: %v", want, kinds)
		}
	}
}

// The drift axis is what makes a fixed period wrong: the same number of steps must take longer at
// the end of the run than at the beginning.
func TestGenDriftsTheStepTime(t *testing.T) {
	raw := genFile(t, "--seed=5", "--duration=1h", "--drift=4")
	tr, err := simulate.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	type sample struct {
		t time.Time
		s int64
	}
	var steps []sample
	for _, e := range tr.Events {
		if e.Kind == simulate.KindStep {
			steps = append(steps, sample{e.T, e.Step})
		}
	}
	if len(steps) < 10 {
		t.Fatalf("only %d step samples", len(steps))
	}
	perStep := func(a, b sample) float64 {
		return (b.t.Sub(a.t).Seconds()) / float64(b.s-a.s)
	}
	early := perStep(steps[1], steps[2])
	late := perStep(steps[len(steps)-2], steps[len(steps)-1])
	if ratio := late / early; ratio < 2.5 {
		t.Fatalf("late/early step time = %.2fx, want about 4x (drift=4)", ratio)
	}
}

// A hole is 20 minutes with no samples; the policy has to keep the bound without them, so the
// generator has to be able to take them away.
func TestGenCanRemoveTwentyMinutesOfSamples(t *testing.T) {
	with := genFile(t, "--seed=11", "--duration=1h", "--holes=1")
	without := genFile(t, "--seed=11", "--duration=1h", "--holes=0")

	count := func(raw []byte) int {
		tr, err := simulate.Parse(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range tr.Events {
			if e.Kind == simulate.KindStep {
				n++
			}
		}
		return n
	}
	if count(with) >= count(without) {
		t.Fatalf("a hole did not remove any samples: %d with, %d without", count(with), count(without))
	}
}
