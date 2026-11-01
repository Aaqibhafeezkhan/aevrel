package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"time"
)

// genConfig is one generated world. Every knob is an axis the algorithm's evaluation names
// (the axes the recorded traces are drawn from), and every one is seeded so that the same run can
// be replayed.
type genConfig struct {
	Duration   time.Duration
	EventEvery time.Duration
	SaveEvery  time.Duration
	StepMS     int
	Drift      float64 // how much slower the last phase is, 1 = flat
	WriteMS    int
	Spike      float64 // worst write cost relative to the base, 1 = flat
	Holes      int     // 20-minute holes in the samples
	Kills      int
	Seed       int64
	// Capabilities includes the declared capability events. It is opt-in until the replay reader
	// ignores event kinds it does not know (a one-line change on the algorithm side): a trace with
	// an unknown kind is refused today, and a trace nobody can read is not a rehearsal.
	Capabilities bool
}

type timed struct {
	at   time.Time
	line map[string]any
}

// generate builds the trace. It is a pure function of its config and the registered capabilities: no
// clock, no environment. A capability that declares a rehearsal series appears here without this
// function knowing its name.
func generate(c genConfig) []timed {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := t0.Add(c.Duration)
	rng := rand.New(rand.NewSource(c.Seed))

	// Step time rises across the run (phase change, data stalls, throttling). A ramp with a little
	// noise is enough: the point is that the period has to follow it, not that it is pretty.
	stepAt := func(t time.Time) time.Duration {
		frac := t.Sub(t0).Seconds() / c.Duration.Seconds()
		factor := 1 + (c.Drift-1)*frac
		factor *= 1 + 0.05*(rng.Float64()*2-1)
		return time.Duration(float64(c.StepMS) * factor * float64(time.Millisecond))
	}

	// Write cost spikes with storage contention: a random walk in [1, spike], seeded so the trace
	// is reproducible.
	wFactor := 1.0
	writeAt := func(time.Time) time.Duration {
		wFactor += (rng.Float64()*2 - 1) * 0.15
		if wFactor < 1 {
			wFactor = 1
		}
		if wFactor > c.Spike {
			wFactor = c.Spike
		}
		return time.Duration(float64(c.WriteMS) * wFactor * float64(time.Millisecond))
	}

	// Holes: 20 minutes of no samples, at seeded positions. The policy must survive not knowing.
	if c.Duration <= 21*time.Minute {
		c.Holes = 0 // a hole that does not fit is not a hole
	}
	holes := make([][2]time.Time, 0, c.Holes)
	for i := 0; i < c.Holes; i++ {
		start := t0.Add(time.Duration(rng.Int63n(int64(c.Duration - 21*time.Minute))))
		holes = append(holes, [2]time.Time{start, start.Add(20 * time.Minute)})
	}
	inHole := func(t time.Time) bool {
		for _, h := range holes {
			if !t.Before(h[0]) && t.Before(h[1]) {
				return true
			}
		}
		return false
	}

	var out []timed

	// Steps: the counter accumulates dt/stepTime, so a slower phase advances fewer steps per second.
	step := 0.0
	for t := t0; t.Before(end); t = t.Add(c.EventEvery) {
		step += c.EventEvery.Seconds() / stepAt(t).Seconds()
		if inHole(t) {
			continue
		}
		out = append(out, timed{at: t, line: map[string]any{"t": stamp(t), "event": "step", "step": int64(step)}})
	}

	// The environment's own writes: they carry the write cost the policy plans with. A trace with no
	// completed write says nothing about W, and the simulator refuses it.
	for t := t0; t.Before(end); t = t.Add(c.SaveEvery) {
		w := writeAt(t)
		if inHole(t) {
			continue
		}
		out = append(out,
			timed{at: t, line: map[string]any{"t": stamp(t), "event": "save_start", "step": int64(step)}},
			timed{at: t.Add(w), line: map[string]any{"t": stamp(t.Add(w)), "event": "save_end", "bytes": 900_000_000, "sealed": true}},
		)
	}

	// Free space falls linearly: retention cannot keep up with writes.
	const total = int64(1) << 40
	for t := t0; t.Before(end); t = t.Add(time.Minute) {
		if inHole(t) {
			continue
		}
		frac := t.Sub(t0).Seconds() / c.Duration.Seconds()
		free := int64(float64(total) * (0.8 - 0.6*frac))
		out = append(out, timed{at: t, line: map[string]any{"t": stamp(t), "event": "free", "bytes": free}})
	}

	// Kills: only the scorer reads them, which is why a trace can be recorded without a controller.
	for i := 0; i < c.Kills; i++ {
		at := t0.Add(time.Duration(rng.Int63n(int64(c.Duration))))
		out = append(out, timed{at: at, line: map[string]any{"t": stamp(at), "event": "kill"}})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// runTrace dispatches the trace subcommands.
func runTrace(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] != "gen" {
		fmt.Fprintln(os.Stderr, "usage: ckptctl trace gen [flags]")
		return 2
	}
	return runGen(args[1:], out)
}

func runGen(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("trace gen", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	duration := fs.Duration("duration", time.Hour, "how long the run lasts")
	eventEvery := fs.Duration("event-every", 5*time.Second, "how often a step sample is recorded")
	saveEvery := fs.Duration("save-every", 10*time.Minute, "the environment's own write cadence")
	stepMS := fs.Int("step-ms", 100, "base milliseconds per step")
	drift := fs.Float64("drift", 3, "how much slower the last phase is (1 = flat)")
	writeMS := fs.Int("write-ms", 30000, "base milliseconds per write")
	spike := fs.Float64("spike", 2, "worst write cost relative to the base (1 = flat)")
	holes := fs.Int("holes", 1, "20-minute holes in the samples")
	kills := fs.Int("kills", 0, "kill events, for scoring only")
	seed := fs.Int64("seed", 1, "same seed, same trace")
	dest := fs.String("out", "-", "write here, or - for stdout")
	withCapabilities := fs.Bool("capability-events", false, "include capability events (needs a reader that ignores unknown kinds)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ckptctl trace gen [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *drift < 1 {
		*drift = 1
	}
	if *spike < 1 {
		*spike = 1
	}

	c := genConfig{
		Duration: *duration, EventEvery: *eventEvery, SaveEvery: *saveEvery,
		StepMS: *stepMS, Drift: *drift, WriteMS: *writeMS, Spike: *spike,
		Holes: *holes, Kills: *kills, Seed: *seed, Capabilities: *withCapabilities,
	}
	lines := generate(c)

	var w io.Writer = out
	var f *os.File
	if *dest != "-" {
		var err error
		f, err = os.Create(*dest)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer f.Close()
		w = f
	}
	for _, l := range lines {
		raw, err := json.Marshal(l.line)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintln(w, string(raw))
	}
	fmt.Fprintf(os.Stderr, "%d events · drift=%.1fx spike=%.1fx holes=%d kills=%d seed=%d · step %.0fms→%.0fms\n",
		len(lines), *drift, *spike, *holes, *kills, *seed,
		float64(*stepMS), float64(*stepMS)*math.Max(1, *drift))
	return 0
}
