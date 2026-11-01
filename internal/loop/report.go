package loop

import (
	"fmt"
	"strings"

	"github.com/bolubo/checkpoint-governor/internal/exec"
)

// Report is what an operator reads about one unit: the counts by outcome, the same counts by trigger,
// the last attempt, the conditions that cannot be read, and the reason the gate gave last.
//
// It is derived from the loop's own state and stored nowhere: a second copy of the truth is a second
// truth. The one field that is memory rather than a file is the gate reason; a restart forgets it, and
// the attempts still say what was refused and why.
type Report struct {
	Generation int64
	Attempts   exec.Counts
	ByTrigger  map[exec.Trigger]exec.Counts
	Last       *exec.Attempt
	Blind      []string
	Gate       string
	Window     string
	// EstimateSeconds and EstimateSource are the last round's answer to "how long does one write
	// take", with its provenance. Zero and "unknown" mean nobody has said.
	EstimateSeconds float64
	EstimateSource  string
}

// Report renders the state of this unit. It reads memory only.
func (l *Loop) Report() Report {
	r := Report{
		Generation: l.ledger.Generation,
		Attempts:   l.attempts.Count(),
		ByTrigger:  map[exec.Trigger]exec.Counts{},
		Blind:      []string{},
		Gate:       l.lastGate,
		Window:     l.window.String(),
		// Read from the ledger, not computed here: a view call must not probe the disk.
		EstimateSeconds: l.ledger.Status.EstimateSeconds,
		EstimateSource:  l.ledger.Status.EstimateSource,
	}
	for _, a := range l.attempts.Attempts {
		c := r.ByTrigger[a.Trigger]
		switch a.Outcome {
		case exec.Running:
			c.Running++
		case exec.Sealed:
			c.Sealed++
		case exec.Broken:
			c.Broken++
		case exec.Abandoned:
			c.Abandoned++
		case exec.Failed:
			c.Failed++
		case exec.Refused:
			c.Refused++
		}
		r.ByTrigger[a.Trigger] = c
		if r.Last == nil || attemptIsNewer(a, *r.Last) {
			last := a
			r.Last = &last
		}
	}
	if l.sampler != nil {
		r.Blind = l.sampler.Blind()
	}
	return r
}

// attemptIsNewer orders attempts the way the history does: by the end, and a running attempt is the
// newest thing there is.
func attemptIsNewer(a, b exec.Attempt) bool {
	if a.EndedAt.IsZero() != b.EndedAt.IsZero() {
		return a.EndedAt.IsZero()
	}
	if a.EndedAt.After(b.EndedAt) {
		return true
	}
	return a.EndedAt.Equal(b.EndedAt) && a.StartedAt.After(b.StartedAt)
}

// Total is how many attempts the history holds.
func (r Report) Total() int {
	c := r.Attempts
	return c.Running + c.Sealed + c.Broken + c.Abandoned + c.Failed + c.Refused
}

// String is the line the acceptance test prints: what an operator would paste into an incident.
func (r Report) String() string {
	var parts []string
	for _, p := range []struct {
		name string
		n    int
	}{
		{"sealed", r.Attempts.Sealed}, {"broken", r.Attempts.Broken},
		{"abandoned", r.Attempts.Abandoned}, {"failed", r.Attempts.Failed},
		{"refused", r.Attempts.Refused}, {"running", r.Attempts.Running},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", p.name, p.n))
		}
	}
	s := fmt.Sprintf("status: attempts %d", r.Total())
	if len(parts) > 0 {
		s += " (" + strings.Join(parts, ", ") + ")"
	}
	s += fmt.Sprintf(", blind %d", len(r.Blind))
	if r.Gate != "" {
		s += ", last gate " + r.Gate
	}
	return s
}
