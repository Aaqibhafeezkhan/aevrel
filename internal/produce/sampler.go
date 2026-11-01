package produce

import (
	"errors"
	"time"
)

// DefaultBlindAfter is how many consecutive unreadable readings make a condition blind. It is
// kubelet's failureThreshold: one failed read is a hiccup, three is a watchdog that cannot see.
const DefaultBlindAfter = 3

// ConditionBroken is the synthetic condition that says sampling itself is dead. It is in the frozen
// catalogue (0013) and is produced here, not by a probe, because it is a fact about the sampler and
// not about the world: the world it describes is "we have no eyes".
const ConditionBroken = "watch.broken"

// Sampler reads a set of probes at their own cadences and remembers the last answer.
//
// It is the only stateful part of this package: probes are pure reads. What it adds is cadence (a
// probe is not asked more often than it asked to be), the last answer (a reading taken three
// seconds ago is still the answer until the next one is due), and the failure budget (a condition
// becomes blind after N consecutive unreadable reads, and says so instead of blinking).
type Sampler struct {
	probes     []Probe
	blindAfter int
	next       map[string]time.Time
	last       map[string]Reading
	misses     map[string]int
	reads      map[string]int
	// blind is the conditions that answered "unknown" in the last sample, and broken is the
	// synthetic verdict derived from them; both are what a view prints.
	blind  []string
	broken bool
}

func NewSampler(probes ...Probe) *Sampler {
	return &Sampler{
		probes: probes,
		next:   map[string]time.Time{},
		last:   map[string]Reading{},
		misses: map[string]int{},
		reads:  map[string]int{},
	}
}

// BlindAfter sets the failure budget. Zero means DefaultBlindAfter; a negative value makes every
// unreadable read blind immediately (only a test wants that).
func (s *Sampler) BlindAfter(n int) *Sampler {
	s.blindAfter = n
	return s
}

// Reads is how many times each probe was actually read. It is the cost of watching, per condition.
func (s *Sampler) Reads() map[string]int {
	out := make(map[string]int, len(s.reads))
	for k, v := range s.reads {
		out[k] = v
	}
	return out
}

// Blind is the conditions that could not be read in the last sample, in catalogue order.
func (s *Sampler) Blind() []string { return append([]string(nil), s.blind...) }

// Broken says whether the sampling itself is dead: a probe reported ErrProbeBroken, or every
// declared probe is blind. Both mean the same thing to a person — nothing is being watched — and a
// single dead probe in a set that otherwise reads is exactly the case the sentinel exists for.
func (s *Sampler) Broken() bool { return s.broken }

// Sample reads what is due and carries forward what is not.
//
// A probe that is not due keeps its last answer (including "absent"), so a consumer never sees a
// condition blink just because its cadence is slower than the round. A probe that is due and
// unreadable spends one unit of its failure budget: inside the budget the last good answer stands
// with its original At, and when the budget runs out the answer becomes Unknown.
func (s *Sampler) Sample(env Env) map[string]Reading {
	out := make(map[string]Reading, len(s.probes)+1)
	for _, p := range s.probes {
		name := p.Name()
		if due, seen := s.next[name]; seen && env.Now.Before(due) {
			out[name] = s.last[name]
			continue
		}
		r := p.Read(env)
		r.Condition = name
		if r.At.IsZero() && r.State != Absent {
			// The sampler owns the clock, so a probe that leaves At empty still gets the moment the
			// read happened. An absent reading has no moment: nothing was read.
			r.At = env.Now
		}
		s.reads[name]++
		s.next[name] = env.Now.Add(p.Every())
		r = s.settle(name, r)
		s.last[name] = r
		out[name] = r
	}

	// Blindness and death are derived from what the probes just said, never stored as an input.
	s.blind = s.blind[:0]
	for _, name := range s.Names() {
		if out[name].State == Unknown {
			s.blind = append(s.blind, name)
		}
	}
	s.broken = len(s.blind) == len(s.probes) && len(s.probes) > 0
	for _, r := range out {
		if errors.Is(r.Err, ErrProbeBroken) {
			s.broken = true
		}
	}

	// The catalogue has a name for "we have no eyes", so the sampler says it in the same vocabulary
	// as every other condition. A deployment with no probes has no watchdog, and then the condition
	// is absent, not zero.
	if len(s.probes) > 0 {
		out[ConditionBroken] = Reading{Condition: ConditionBroken, State: Fresh, Value: s.broken, At: env.Now}
	}
	return out
}

// Names is the declared condition names, in the order the caller gave them, which is the
// catalogue's order (adapters.All follows it).
func (s *Sampler) Names() []string {
	out := make([]string, len(s.probes))
	for i, p := range s.probes {
		out[i] = p.Name()
	}
	return out
}

// settle applies the failure budget to one reading.
func (s *Sampler) settle(name string, r Reading) Reading {
	if r.State != Unknown {
		// A readable answer, even "this environment has no such condition", ends a failure streak.
		s.misses[name] = 0
		return r
	}
	s.misses[name]++
	r.Misses = s.misses[name]
	budget := s.blindAfter
	if budget == 0 {
		budget = DefaultBlindAfter
	}
	last, seen := s.last[name]
	if budget > 0 && s.misses[name] < budget && seen && last.State != Unknown {
		// Inside the budget the last good answer stands. Its At says when it was taken, and its
		// Misses says how many reads have failed since — the value is never invented.
		last.Err = r.Err
		last.Misses = s.misses[name]
		return last
	}
	return r
}
