package adapters

import (
	"fmt"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// evictionLikelyLine is the share of allocatable resources above which the node is likely to need
// this unit's room. Deployment configuration (`conditions.json`), never a runtime knob.
const evictionLikelyLine = 0.90

// evictionLikely is `risk.eviction-likely`: an *inference*, not an announcement, and it reads only
// the world projection — never the cluster itself. The four guardrails (0013, design §5) are all
// visible here: its own name, an answer that names the signals, `unknown` when the world cannot be
// read, and a capability switch to turn it off.
type evictionLikely struct {
	every      time.Duration
	line       float64
	authorized bool
	world      WorldLoc
}

func (evictionLikely) with(c Config) evictionLikely {
	return evictionLikely{
		every:      15 * time.Second,
		line:       c.evictionLikelyLine(),
		authorized: c.evictionAuthorized(),
		world:      c.worldLoc(),
	}
}

func (p evictionLikely) Name() string { return "risk.eviction-likely" }

func (p evictionLikely) Every() time.Duration { return p.every }

func (p evictionLikely) Cost() produce.Cost { return produce.Cheap }

func (p evictionLikely) Read(env produce.Env) produce.Reading {
	if !p.authorized {
		// Switched off, or the operator declared there is no eviction upstream. Absent, not calm: a
		// deployment that turned the inference off has no such condition, and saying "0" would claim
		// it looked and found nothing.
		return produce.Reading{State: produce.Absent, At: env.Now}
	}

	v, err := p.world.project(env.Now)
	if err != nil {
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: err}
	}
	return judgeEviction(v, p.line, p.signals(v), env.Now)
}

// judgeEviction is the shared decision, so the probe and the display cannot drift apart.
func judgeEviction(v UnitView, line float64, fired []string, now time.Time) produce.Reading {
	switch {
	case !v.SummaryPresent || !v.Tracking:
		// No observer, or an observer that reports nodes but not units. Either way this deployment
		// does not have the condition, which is different from failing to read it.
		return produce.Reading{State: produce.Absent, At: now}
	case !v.UnitPresent:
		return produce.Reading{State: produce.Unknown, At: now,
			Err: fmt.Errorf("the observer tracks units but has not placed this one")}
	case v.Stale:
		return produce.Reading{State: produce.Unknown, At: now,
			Err: fmt.Errorf("the world is %s old, past its own limit", v.Age.Round(time.Second))}
	case !v.RatiosKnown:
		return produce.Reading{State: produce.Unknown, At: now,
			Err: fmt.Errorf("no allocation ratio is readable on %s", v.Node)}
	}
	return produce.Reading{State: produce.Fresh, Value: len(fired) > 0, At: now}
}

// signals names what fired, which is the guardrail "a 1 must be able to say what it saw".
func (p evictionLikely) signals(v UnitView) []string {
	var out []string
	if v.CPUUse >= p.line {
		out = append(out, fmt.Sprintf("allocatable %d%%", pct(v.CPUUse)))
	}
	if v.MemUse >= p.line {
		out = append(out, fmt.Sprintf("allocatable memory %d%%", pct(v.MemUse)))
	}
	if v.PendingHigherPriority > 0 {
		out = append(out, "pending-priority-pod")
	}
	if v.Cordoned {
		out = append(out, "cordoned")
	}
	return out
}

// EvictionSignals is what the display prints behind a 1. It projects the world again, with the same
// rules as the probe: a condition that fires without being able to say which signal fired is exactly
// what 0013 forbids.
func EvictionSignals(worldDir, cluster, unitID string, declared Deployment, line float64, now time.Time) ([]string, error) {
	v, err := Project(worldDir, cluster, unitID, declared, now)
	if err != nil {
		return nil, err
	}
	if !v.SummaryPresent || !v.Tracking || !v.UnitPresent || v.Stale || !v.RatiosKnown {
		return nil, nil
	}
	return evictionLikely{line: line}.signals(v), nil
}

func pct(f float64) int { return int(f*100 + 0.5) }
