package simulate

import (
	"fmt"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// The comparators are written as policies because that is what they are: a schedule that answers
// "save now?" from the same snapshot the real policy sees. Writing them any other way would create
// a second decision path that could disagree with the one under test.

// FixedPeriod is the configuration almost every job uses today: save every `Period`, measured from
// one seal to the next, with no idea of how long the write takes.
type FixedPeriod struct{ Period time.Duration }

func (f FixedPeriod) Name() string { return "fixed-" + ShortDuration(f.Period) }

func (f FixedPeriod) Decide(b policy.Bounds, s view) plan {
	d := plan{Decision: policy.Decision{
		Action:        policy.ActionSkip,
		Reason:        policy.ReasonSchedule,
		CopiesToWrite: 1,
		KeepCopies:    1,
		Version:       f.Name(),
		UnitID:        s.UnitID,
		Generation:    s.Generation,
	}}
	if s.LastSeal.IsZero() {
		d.Action = policy.ActionSave
		d.Reason = policy.ReasonFirst
		d.SaveNow = true
		d.Why = "no checkpoint yet"
		return d
	}
	// The timer sleeps a full period from the seal, so it seals at age Period + W: nothing is
	// subtracted here on purpose, because that is the habit being measured.
	next := s.LastSeal.Add(f.Period)
	d.NextCheck = next
	if s.Now.Sub(s.LastSeal) >= f.Period {
		d.Action = policy.ActionSave
		d.SaveNow = true
		d.Why = fmt.Sprintf("timer every %s", ShortDuration(f.Period))
	}
	return d
}

// FixedAtBound saves every `L`, the setting a careful user picks by hand: it uses the bound as the
// period without subtracting the write, which is exactly the one-write overrun worth measuring.
type FixedAtBound struct{}

func (FixedAtBound) Name() string { return "fixed-at-bound" }

func (FixedAtBound) Decide(b policy.Bounds, s view) plan {
	period := b.NoUsEvery
	if period <= 0 {
		period = 30 * time.Minute
	}
	return FixedPeriod{Period: period}.Decide(b, s)
}

// FixedSteps saves every N steps: blind to how long a step or a write takes.
type FixedSteps struct{ Steps int64 }

func (f FixedSteps) Name() string { return fmt.Sprintf("every-%d-steps", f.Steps) }

func (f FixedSteps) Decide(b policy.Bounds, s view) plan {
	d := plan{Decision: policy.Decision{
		Action:        policy.ActionSkip,
		Reason:        policy.ReasonSchedule,
		CopiesToWrite: 1,
		KeepCopies:    1,
		Version:       f.Name(),
		UnitID:        s.UnitID,
		Generation:    s.Generation,
	}}
	newest, ok := newestStep(s.Copies)
	if !ok {
		d.Action = policy.ActionSave
		d.Reason = policy.ReasonFirst
		d.SaveNow = true
		d.Why = "no checkpoint yet"
		return d
	}
	if s.Step-newest >= f.Steps {
		d.Action = policy.ActionSave
		d.SaveNow = true
		d.Why = fmt.Sprintf("every %d steps", f.Steps)
	}
	return d
}

// Always saves whenever nothing is in flight: the expensive extreme, and the price of never losing
// more than one write.
type Always struct{}

func (Always) Name() string { return "always" }

func (Always) Decide(b policy.Bounds, s view) plan {
	d := plan{Decision: policy.Decision{
		Action:        policy.ActionSkip,
		Reason:        policy.ReasonSchedule,
		Version:       "always",
		UnitID:        s.UnitID,
		Generation:    s.Generation,
		CopiesToWrite: 1,
		KeepCopies:    1,
	}}
	if s.LastSeal.IsZero() || !s.SaveInFlight {
		d.Action = policy.ActionSave
		d.SaveNow = true
		if s.LastSeal.IsZero() {
			d.Reason = policy.ReasonFirst
		}
	}
	return d
}

func newestStep(copies []policy.Copy) (int64, bool) {
	var newest int64
	found := false
	for _, c := range copies {
		if !found || c.Step > newest {
			newest = c.Step
			found = true
		}
	}
	return newest, found
}

// ShortDuration prints a period the way a person would say it.
func ShortDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0"
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return d.String()
	}
}
