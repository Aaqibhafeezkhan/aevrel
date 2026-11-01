// Package audit answers the one question a person is asked after the run: was the bound held?
//
// It is the after-the-fact half of the promise. `internal/bound` prices what a bound costs before a
// run; this package measures what it actually cost during one, from the seal times the directory
// carries and nothing else. Deleting it cannot change a write: it only reads times that already
// happened.
//
// Staleness is defined exactly as the executor's window defines it: the age of the newest complete
// checkpoint. The audit walks the run from its start to the moment it is asked about, and every
// interval between two seals is one segment whose age grows linearly, so both the worst staleness and
// the seconds spent over the bound are integrals, not samples.
package audit

import (
	"sort"
	"time"
)

// Segment is one stretch of the run between two moments when the newest copy changed.
type Segment struct {
	From, To time.Time
	// Age is how old the newest copy was at To, just before the next seal landed.
	Age time.Duration
	// Over is the part of the segment during which the age exceeded the bound.
	Over time.Duration
}

// Certificate is the answer: the bound, the run it was measured over, and what the copies did.
type Certificate struct {
	Bound time.Duration
	Start time.Time
	End   time.Time
	// Seals is how many complete copies exist by the end of the run.
	Seals int
	// Covered is false when the run began with no complete copy on disk. The bound holds nothing
	// until the first seal, and saying so is part of the certificate: an uncovered start is a fact,
	// not an average.
	Covered bool
	// GapFrom and GapTo bracket the uncovered interval; GapTo is zero while the gap is still open.
	GapFrom, GapTo time.Time
	// MaxStaleness is the worst age the newest complete copy reached, and MaxAt is when.
	MaxStaleness time.Duration
	MaxAt        time.Time
	// OverBound is the total time the newest copy was older than the bound. Zero is the only
	// acceptable number for a held bound.
	OverBound time.Duration
	// Segments is every stretch, kept so a surprising certificate can be explained rather than
	// believed.
	Segments []Segment
}

// Certify measures one run against one bound.
//
// start is when the run began (the first round's clock), end is the moment being certified (usually
// now), and seals are the times the complete copies landed. A seal stamped after end is ignored
// rather than clamped: clamping would let a future timestamp invent coverage the run never had.
func Certify(bound time.Duration, start, end time.Time, seals []time.Time) Certificate {
	c := Certificate{Bound: bound, Start: start, End: end}
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return c
	}
	ordered := make([]time.Time, 0, len(seals))
	for _, t := range seals {
		if t.IsZero() || t.After(end) {
			continue
		}
		ordered = append(ordered, t)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Before(ordered[j]) })
	c.Seals = len(ordered)
	if len(ordered) == 0 {
		c.Covered = false
		c.GapFrom = start
		return c
	}

	// A copy that landed before the run started still covers the run's first moment: a resumed job
	// begins with a checkpoint, and pretending otherwise would report a gap that never existed.
	last := time.Time{}
	next := 0
	for ; next < len(ordered) && !ordered[next].After(start); next++ {
		last = ordered[next]
	}
	if last.IsZero() {
		c.Covered = false
		c.GapFrom = start
		c.GapTo = ordered[0]
	} else {
		c.Covered = true
	}

	add := func(from, to, seal time.Time) {
		if !to.After(from) {
			return
		}
		age := to.Sub(seal)
		over := time.Duration(0)
		if limit := seal.Add(bound); bound > 0 && to.After(limit) {
			begin := limit
			if begin.Before(from) {
				begin = from
			}
			over = to.Sub(begin)
		}
		c.Segments = append(c.Segments, Segment{From: from, To: to, Age: age, Over: over})
		c.OverBound += over
		if age > c.MaxStaleness || c.MaxAt.IsZero() {
			c.MaxStaleness = age
			c.MaxAt = to
		}
	}

	prev := start
	for i := next; i < len(ordered); i++ {
		seal := ordered[i]
		if last.IsZero() {
			// Nothing was covered before the first seal, and the age clock starts at that seal.
			c.GapTo = seal
		} else {
			add(prev, seal, last)
		}
		last = seal
		prev = seal
	}
	add(prev, end, last)
	return c
}

// Held is the verdict. A bound of zero is not a promise ("exceptions only"), so a run without one
// cannot claim to have held it, however good its copies look.
func (c Certificate) Held() bool {
	return c.Bound > 0 && c.Covered && c.OverBound == 0
}

// Run is how long the certificate covers.
func (c Certificate) Run() time.Duration {
	if c.Start.IsZero() || c.End.IsZero() {
		return 0
	}
	return c.End.Sub(c.Start)
}

// Uncovered is how long the run went without any complete copy at all: zero when it began with one.
func (c Certificate) Uncovered() time.Duration {
	if c.Covered {
		return 0
	}
	to := c.GapTo
	if to.IsZero() {
		to = c.End
	}
	if to.IsZero() || c.GapFrom.IsZero() || to.Before(c.GapFrom) {
		return 0
	}
	return to.Sub(c.GapFrom)
}

// Worst returns the segment with the oldest copy, so a certificate can name the moment it was closest
// to breaking the promise rather than only its total.
func (c Certificate) Worst() (Segment, bool) {
	var worst Segment
	found := false
	for _, s := range c.Segments {
		if !found || s.Age > worst.Age {
			worst = s
			found = true
		}
	}
	return worst, found
}
