package simulate

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// Families are the shapes a run takes. Each one moves one of the axes that are known to move the
// optimal period, so a fixed period can be judged against them instead of against a friendly
// constant.
type Family string

const (
	// Steady: step time and write cost are constant. The control case.
	Steady Family = "steady"
	// Drift: step time grows 4x, so a lost-work bound expressed in steps converts to four times the
	// time late in the run.
	Drift Family = "drift"
	// Storm: write cost spikes 5x for a few minutes, the "every job saves at once" case.
	Storm Family = "storm"
	// Capacity: a big checkpoint on a small volume; retention has to delete before writing.
	Capacity Family = "capacity"
	// Killed: the job is taken away three times, at a rate that changes between bursts.
	Killed Family = "killed"
	// Tiny: a small model — a 20s write, a 1.2s step.
	Tiny Family = "tiny"
	// Big: ten times the model — a 180s write. The same tax budget forces a much longer period.
	Big Family = "big"
	// Delta: a full write of 30s plus a delta that costs 2% of the training time it covers, which is
	// what the local storage measured. The family exists to price the pattern axis.
	Delta Family = "delta"
	// DeltaStorm: the same job, but the storage's per-write overhead triples for five minutes. A
	// pattern tuned on the calm family overspends the tax budget there; the question is whether an
	// online plan can stay inside it without paying for that calm.
	DeltaStorm Family = "delta-storm"
	// SmallTeam: the target user of V1 — one job, a 300 MB checkpoint with optimizer state, a mount
	// that moves about 10 MB/s and charges one second per write. It is priced by WriteModel rather
	// than by a recorded curve, so the shard axis can be studied on it.
	SmallTeam Family = "small-team"
	// SmallTeamLatency and SmallTeamShards are the same job on a mount whose cost is round trips:
	// one stream against four, so the shard axis has numbers instead of being a claim.
	SmallTeamLatency Family = "small-team-latency"
	SmallTeamShards  Family = "small-team-shards"
)

// Families lists every family in the order the table prints them.
func Families() []Family {
	return []Family{Steady, Drift, Storm, Capacity, Killed, Tiny, Big, Delta, DeltaStorm,
		SmallTeam, SmallTeamLatency, SmallTeamShards}
}

// BoundsFor gives each family the bound its owner would ask for: a small model asks for ten
// minutes, a big one for forty-five, and the tax budget is the same for every job, which is the
// situation the fleet table is about. It lives here rather than in the command so that the table
// and the tests that pin the policy read one definition.
func BoundsFor(fam Family) (policy.Bounds, EvalBounds) {
	// The replay's user has signed for no periodic writes: that is the state the product ships,
	// and it is what makes "nothing fires, nothing is written" the honest default here too.
	b := DefaultBounds()
	e := DefaultEval()
	b.NoUs = policy.NoUsNone
	switch fam {
	case Tiny:
		e.MaxLostWork = 10 * time.Minute
	case Big:
		e.MaxLostWork = 45 * time.Minute
	case Capacity:
		// One copy: the family is about a volume that cannot hold two, and capacity is the question.
		b.MinCopies = 1
	case Delta, DeltaStorm, SmallTeam, SmallTeamLatency, SmallTeamShards:
		// The shared numbers of the retired interval model (0014), so the table and the derivation
		// can be compared line by line; small-team is the same configuration on a slower mount.
		e.MaxLostWork = 10 * time.Minute
		e.MaxTax = 0.05
	}
	return b, e
}

// Generate returns the trace a recording run would produce, and the environment it ran in.
//
// The trace is written by a job that saves on a fixed 30-minute timer, which is what most jobs do
// today. That matters: a trace is the evidence a user could have collected without this project,
// so a policy may only use what such a trace contains.
//
// The steps in a generated trace stop while a write runs, because that is what happens to a real
// job. The replay itself takes the recorded step timeline as given (see the package comment), so a
// policy that writes more often than the recorded job is charged its write time but is not charged
// for the steps those extra writes would have delayed. The violation ratio does not depend on that;
// the step count does.
func Generate(seed int64, fam Family) (Trace, Env, error) {
	rng := rand.New(rand.NewSource(seed))
	env := baseEnv(fam, rng)
	tr, err := recordFixed(env, 30*time.Minute)
	if err != nil {
		return Trace{}, Env{}, err
	}
	return tr, env, nil
}

// Env is what the generator used, so a caller can print the assumptions and, later, compare a real
// trace against the synthetic one it was modelled on.
type Env struct {
	Duration        time.Duration
	CheckpointBytes int64
	VolumeBytes     int64
	// WriteCost and StepTime are the true curves; a trace only shows them where they were sampled.
	WriteCost func(time.Duration) time.Duration
	StepTime  func(time.Duration) time.Duration
	// DeltaShare is the cost of a delta per second of training it covers, when the storage can
	// write deltas at all. Nil means it cannot.
	DeltaShare func(time.Duration) float64
	// Model, when set, is the storage model the family was built from: overhead, bandwidth,
	// streams, and the bytes of each content. It is what the V2 axes are priced with.
	Model *WriteModel
	Kills []time.Duration
}

func baseEnv(fam Family, rng *rand.Rand) Env {
	ckpt := int64(400 * 1000 * 1000)
	volume := int64(40 * 1000 * 1000 * 1000)
	stepCurve := knots{{at: 0, value: 2.0}}
	costCurve := knots{{at: 0, value: 30.0}}
	deltaShareCurve := knots(nil)
	var model *WriteModel
	duration := 40 * time.Minute
	var kills []time.Duration

	switch fam {
	case Drift:
		stepCurve = knots{
			{at: 0, value: 2.0},
			{at: 10 * time.Minute, value: 2.2},
			{at: 25 * time.Minute, value: 5.5},
			{at: 40 * time.Minute, value: 8.0},
		}
	case Storm:
		costCurve = knots{
			{at: 0, value: 30},
			{at: 22 * time.Minute, value: 34},
			{at: 24 * time.Minute, value: 150},
			{at: 29 * time.Minute, value: 150},
			{at: 31 * time.Minute, value: 40},
			{at: 45 * time.Minute, value: 36},
		}
		duration = 45 * time.Minute
	case Capacity:
		ckpt = int64(900 * 1000 * 1000)
		volume = int64(2 * 1000 * 1000 * 1000)
	case Killed:
		duration = 60 * time.Minute
		for _, at := range []float64{0.15, 0.45, 0.8} {
			kills = append(kills, time.Duration(float64(duration)*at)+time.Duration(rng.Intn(120))*time.Second)
		}
	case Tiny:
		ckpt = int64(150 * 1000 * 1000)
		volume = int64(8 * 1000 * 1000 * 1000)
		stepCurve = knots{{at: 0, value: 1.2}}
		costCurve = knots{{at: 0, value: 20.0}}
	case Big:
		ckpt = int64(2 * 1000 * 1000 * 1000)
		volume = int64(20 * 1000 * 1000 * 1000)
		stepCurve = knots{{at: 0, value: 6.0}}
		costCurve = knots{{at: 0, value: 180.0}}
		duration = 300 * time.Minute
	case Delta:
		ckpt = int64(400 * 1000 * 1000)
		costCurve = knots{{at: 0, value: 30.0}}
		duration = 60 * time.Minute
		deltaShareCurve = knots{{at: 0, value: 0.02}}
		for _, at := range []float64{0.2, 0.55, 0.9} {
			kills = append(kills, time.Duration(float64(duration)*at))
		}
	case DeltaStorm:
		ckpt = int64(400 * 1000 * 1000)
		costCurve = knots{{at: 0, value: 30.0}}
		duration = 60 * time.Minute
		// The per-write overhead triples for five minutes: per-byte cost barely moves, per-write
		// cost jumps, which is what a burst of metadata operations looks like.
		deltaShareCurve = knots{
			{at: 0, value: 0.02},
			{at: 20 * time.Minute, value: 0.02},
			{at: 21 * time.Minute, value: 0.06},
			{at: 25 * time.Minute, value: 0.06},
			{at: 26 * time.Minute, value: 0.02},
			{at: 60 * time.Minute, value: 0.02},
		}
		for _, at := range []float64{0.2, 0.55, 0.9} {
			kills = append(kills, time.Duration(float64(duration)*at))
		}
	case SmallTeam:
		m := SmallTeamModel()
		model = &m
		ckpt = m.FullBytes
		duration = 60 * time.Minute
		// The recorded trace is what a fixed-timer job would have produced: a full write every
		// 30 minutes at the model's cost, plus one second of overhead each time.
		costCurve = knots{{at: 0, value: float64(m.FullCost()) / float64(time.Second)}}
		deltaShareCurve = knots{{at: 0, value: m.DeltaShareAt()}}
		for _, at := range []float64{0.2, 0.55, 0.9} {
			kills = append(kills, time.Duration(float64(duration)*at))
		}
	case SmallTeamLatency, SmallTeamShards:
		streams := 1
		if fam == SmallTeamShards {
			streams = 4
		}
		m := LatencyModel(streams)
		model = &m
		ckpt = m.FullBytes
		duration = 60 * time.Minute
		costCurve = knots{{at: 0, value: float64(m.FullCost()) / float64(time.Second)}}
		deltaShareCurve = knots{{at: 0, value: m.DeltaShareAt()}}
		for _, at := range []float64{0.2, 0.55, 0.9} {
			kills = append(kills, time.Duration(float64(duration)*at))
		}
	}

	env := Env{
		Duration:        duration,
		CheckpointBytes: ckpt,
		VolumeBytes:     volume,
		Model:           model,
		Kills:           kills,
	}
	env.WriteCost = func(d time.Duration) time.Duration {
		return time.Duration(costCurve.at(d) * float64(time.Second))
	}
	env.StepTime = func(d time.Duration) time.Duration {
		return time.Duration(stepCurve.at(d) * float64(time.Second))
	}
	if len(deltaShareCurve) > 0 {
		env.DeltaShare = func(d time.Duration) float64 { return deltaShareCurve.at(d) }
	}
	return env
}

func recordFixed(env Env, period time.Duration) (Trace, error) {
	const tick = 10 * time.Second
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)

	var events []Event
	at := time.Duration(0)
	step := int64(0)
	sealAt := time.Duration(0)
	haveSeal := false
	copies := int64(0)
	saves := 0

	for at < env.Duration {
		due := !haveSeal || at-sealAt >= period
		if due && saves < 200 {
			cost := env.WriteCost(at)
			if cost <= 0 {
				cost = time.Second
			}
			events = append(events, Event{T: start.Add(at), Kind: KindSaveStart, Step: step})
			at += cost
			saves++
			copies++
			events = append(events, Event{T: start.Add(at), Kind: KindSaveEnd, Step: step, Bytes: env.CheckpointBytes, Sealed: true})
			events = append(events, Event{T: start.Add(at), Kind: KindFree, Bytes: env.VolumeBytes - copies*env.CheckpointBytes})
			sealAt = at
			haveSeal = true
			continue
		}
		events = append(events, Event{T: start.Add(at), Kind: KindStep, Step: step})
		at += tick
		secs := env.StepTime(at)
		if secs <= 0 {
			return Trace{}, fmt.Errorf("step time is zero at %s", at)
		}
		step += int64(tick / secs)
	}

	events = append(events, Event{T: start.Add(env.Duration), Kind: KindStep, Step: step})
	for _, k := range env.Kills {
		events = append(events, Event{T: start.Add(k), Kind: KindKill})
	}
	sortEvents(events)
	return Trace{Events: events}, nil
}

func sortEvents(events []Event) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].T.Before(events[j-1].T); j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
}
