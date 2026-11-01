// Package exec turns a decision into a write, and remembers what happened.
//
// The algorithm is a stateless advisor; everything that waits, retries or can fail lives here. The
// unit of accounting is the Attempt: one request, one identity, one immutable outcome (decision
// 0016).
package exec

import "time"

// Outcome is how an attempt ended. Zero means it is still running.
type Outcome string

const (
	// Running is the only non-final state.
	Running Outcome = ""
	// Sealed means the write produced a checkpoint that verifies.
	Sealed Outcome = "sealed"
	// Broken means the write finished but the checkpoint does not verify.
	Broken Outcome = "broken"
	// Abandoned means the write stopped making progress and is now dead.
	Abandoned Outcome = "abandoned"
	// Failed means the write reported a failure, or the deadline passed with no progress.
	Failed Outcome = "failed"
	// Refused means the gate stopped the attempt before it started; it is recorded so that a
	// condition that keeps firing has a visible history instead of a silent one.
	Refused = Outcome("refused")
)

// Trigger says what asked for the write. It is the decision's reason, kept for accounting.
type Trigger string

const (
	TriggerCondition Trigger = "condition"
	TriggerWindow    Trigger = "window"
	TriggerRepair    Trigger = "repair"
	TriggerFirst     Trigger = "first"
)

// Attempt is one write request and its life.
type Attempt struct {
	ID         string    // unique within a unit, stable across restarts
	UnitID     string    //
	Generation int64     // the generation the decision was made for
	Trigger    Trigger   // what asked for it
	Reason     string    // the condition name, when Trigger is TriggerCondition
	Step       int64     // the step the write claims to produce
	StartedAt  time.Time //
	Deadline   time.Time // after this, no progress means Failed
	Bytes      int64     // what the write produced, once known
	Outcome    Outcome   // written once, never changed
	EndedAt    time.Time // zero while Running
}

// Done reports whether the outcome is final.
func (a Attempt) Done() bool { return a.Outcome != Running }
