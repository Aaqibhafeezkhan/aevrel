// Package exec turns a decision into a write, and remembers what happened.
//
// The algorithm is a stateless advisor; everything that waits, retries or can fail lives here. The
// unit of accounting is the Attempt: one request, one identity, one immutable outcome (decision
// 0016).
package exec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SchemaVersion is the shape of the attempts file. A reader that sees another version ignores the
// file rather than guessing: a ledger nobody can read is better than a ledger that lies.
const SchemaVersion = 1

// maxAttempts bounds the history. Enough to answer "what happened in the last few hours" and small
// enough that the file stays a summary, not a log.
const maxAttempts = 32

// Ledger is the attempt history of one unit. Outcomes are written once and never changed, so every
// counter in the system is a query over this list rather than a separate number that can drift.
type Ledger struct {
	Schema   int       `json:"schema"`
	Attempts []Attempt `json:"attempts"`
}

// Counts is how many attempts ended each way. It is derived, never incremented: that is what makes
// a restart idempotent.
type Counts struct {
	Running, Sealed, Broken, Abandoned, Failed, Refused int
}

// String is the line the acceptance test prints.
func (c Counts) String() string {
	s := fmt.Sprintf("attempts: %d sealed", c.Sealed)
	if c.Broken > 0 {
		s += fmt.Sprintf(", %d broken", c.Broken)
	}
	s += fmt.Sprintf(", %d abandoned, %d failed", c.Abandoned, c.Failed)
	if c.Running > 0 {
		s += fmt.Sprintf(", %d running", c.Running)
	}
	if c.Refused > 0 {
		s += fmt.Sprintf(", %d refused", c.Refused)
	}
	return s
}

// Count returns the derived counts.
func (l Ledger) Count() Counts { return CountOf(l.Attempts) }

// CountOf derives the counts from a list, which is how a restart stays idempotent: nothing is
// incremented on load, everything is counted from the immutable outcomes.
func CountOf(attempts []Attempt) Counts {
	var c Counts
	for _, a := range attempts {
		switch a.Outcome {
		case Running:
			c.Running++
		case Sealed:
			c.Sealed++
		case Broken:
			c.Broken++
		case Abandoned:
			c.Abandoned++
		case Failed:
			c.Failed++
		case Refused:
			c.Refused++
		}
	}
	return c
}

// Find returns one attempt by ID.
func (l Ledger) Find(id string) (Attempt, bool) {
	for _, a := range l.Attempts {
		if a.ID == id {
			return a, true
		}
	}
	return Attempt{}, false
}

// Record writes one attempt into the history. An attempt with an identity the ledger already knows
// is replaced only while it is still Running or while its outcome is Refused: a real outcome is
// immutable, and a refusal is a request that was not served, so asking again may replace the record
// with the answer the gate gave this time.
func (l *Ledger) Record(a Attempt) {
	if l.Schema == 0 {
		l.Schema = SchemaVersion
	}
	for i := range l.Attempts {
		if l.Attempts[i].ID != a.ID {
			continue
		}
		if l.Attempts[i].Done() && l.Attempts[i].Outcome != Refused {
			// The same request arriving twice must not rewrite history.
			return
		}
		l.Attempts[i] = a
		l.trim()
		return
	}
	l.Attempts = append(l.Attempts, a)
	l.trim()
}

// DropOlderGeneration removes the attempts of generations that no longer exist. A restart mid-write
// leaves a running attempt that describes a dead world: keeping it would make the new generation
// inherit a failure it did not cause, and settling it would claim a copy nobody asked for this
// incarnation to produce. It returns how many were dropped, for the log.
func (l *Ledger) DropOlderGeneration(gen int64) int {
	kept := l.Attempts[:0]
	dropped := 0
	for _, a := range l.Attempts {
		if a.Generation < gen {
			dropped++
			continue
		}
		kept = append(kept, a)
	}
	l.Attempts = kept
	return dropped
}

// trim keeps the newest attempts. "Newest" is by end, then start: a running attempt is the newest
// thing there is.
func (l *Ledger) trim() {
	if len(l.Attempts) <= maxAttempts {
		return
	}
	sort.Slice(l.Attempts, func(i, j int) bool {
		ei, ej := l.Attempts[i].EndedAt, l.Attempts[j].EndedAt
		if ei.IsZero() {
			ei = time.Unix(1<<62, 0)
		}
		if ej.IsZero() {
			ej = time.Unix(1<<62, 0)
		}
		return ei.Before(ej)
	})
	l.Attempts = l.Attempts[len(l.Attempts)-maxAttempts:]
}

// attemptsFile is where the history lives, beside the plan and the private ledger.
func attemptsFile(dir string) string { return filepath.Join(dir, ".ckpt-governor", "attempts.json") }

// OpenLedger loads the attempts of one unit. A missing file is an empty history, not an error.
func OpenLedger(dir string) (Ledger, error) {
	raw, err := os.ReadFile(attemptsFile(dir))
	if errors.Is(err, os.ErrNotExist) {
		return Ledger{Schema: SchemaVersion}, nil
	}
	if err != nil {
		return Ledger{}, err
	}
	var l Ledger
	if err := json.Unmarshal(raw, &l); err != nil {
		// A corrupt history is dropped deliberately: a controller that refuses to run because it
		// cannot read yesterday's attempts is a controller that stops working after a crash.
		return Ledger{Schema: SchemaVersion}, nil
	}
	if l.Schema != SchemaVersion {
		return Ledger{Schema: SchemaVersion}, nil
	}
	if l.Attempts == nil {
		l.Attempts = []Attempt{}
	}
	return l, nil
}

// Save writes the history atomically: a reader never sees half of it.
func (l Ledger) Save(dir string) error {
	if err := os.MkdirAll(filepath.Dir(attemptsFile(dir)), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(attemptsFile(dir)), "attempts-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, attemptsFile(dir))
}
