package loop

import (
	"time"

	"github.com/bolubo/checkpoint-governor/internal/exec"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// Settle turns every running attempt into a final outcome, using only what the directory says.
//
// The four verdicts, in the order they are decided:
//
//	Sealed     a copy this attempt produced is complete and, when checked, its content matches.
//	Broken     the copy is complete but the content does not match: the write happened, the
//	           checkpoint is not usable.
//	Abandoned  the deadline passed while a partial copy exists: something was written and will
//	           never be finished.
//	Failed     the deadline passed and nothing appeared: the write produced nothing.
//
// "Produced" is about time, not only about the step number (see producedBy). A finished attempt is
// never re-judged. That is what makes a restart idempotent: the same history, settled again, yields
// the same outcomes and the same counts.
func Settle(attempts []exec.Attempt, s policy.Snapshot, now time.Time) ([]exec.Attempt, exec.Counts) {
	for i := range attempts {
		a := attempts[i]
		if a.Done() {
			continue
		}
		state, bytes, found := producedBy(s.Copies, a)
		switch {
		case found && (state == policy.CopySealed || state == policy.CopyVerified):
			a.Outcome = exec.Sealed
			a.Bytes = bytes
			a.EndedAt = now
		case found && state == policy.CopyBroken:
			a.Outcome = exec.Broken
			a.EndedAt = now
		case found:
			// A partial directory: the write is still in flight until its deadline.
			if pastDeadline(a, now) {
				a.Outcome = exec.Abandoned
				a.EndedAt = now
			}
		default:
			if pastDeadline(a, now) {
				a.Outcome = exec.Failed
				a.EndedAt = now
			}
		}
		attempts[i] = a
	}
	return attempts, exec.CountOf(attempts)
}

// producedBy finds the copy this attempt produced.
//
// The exact step wins: the request named it, and a job that reports its step produces exactly that
// directory. When the job reports no step the controller asked for step 0 and the job still seals
// step 10, so the fallback is the newest copy that appeared after the attempt started. Marking such
// a write "failed" is the one mistake a governor must never make: it turns a good checkpoint into
// `write.failed-twice`, and the unit blocks itself over a write that succeeded.
//
// A copy that existed before the request is never this attempt's product, which is what stops a
// request at a step that is already sealed from being counted as a fresh write. An undated copy is
// attributed, because "we cannot date it" is not evidence that nothing happened.
func producedBy(copies []policy.Copy, a exec.Attempt) (policy.CopyState, int64, bool) {
	for _, c := range copies {
		if c.Step == a.Step && appearedAfter(a, c) {
			return c.State, c.Bytes, true
		}
	}
	var best policy.Copy
	found := false
	for _, c := range copies {
		if !appearedAfter(a, c) {
			continue
		}
		if !found || c.Step > best.Step {
			best, found = c, true
		}
	}
	if !found {
		return "", 0, false
	}
	return best.State, best.Bytes, true
}

// appearedAfter says whether a copy can be the product of this attempt: it was first seen no earlier
// than the request. An undated copy is not attributed: "we cannot date it" is not evidence that it
// appeared after we asked, and the snapshot always carries a date in practice.
func appearedAfter(a exec.Attempt, c policy.Copy) bool {
	return !c.CreatedAt.IsZero() && !c.CreatedAt.Before(a.StartedAt)
}

// pastDeadline is true when the attempt has a deadline and the clock has reached it. An attempt
// with no deadline is never judged dead by time alone.
func pastDeadline(a exec.Attempt, now time.Time) bool {
	return !a.Deadline.IsZero() && !now.Before(a.Deadline)
}

// windowDecision is the write the user's window asks for when it closes: a trigger, not a second
// policy. The condition catalogue has nothing to say about it — the span is the user's own number.
func windowDecision(now time.Time) policy.Decision {
	return policy.Decision{
		Action: policy.ActionSave, Reason: policy.ReasonSchedule, SaveNow: true,
		Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1,
		Reasons: []policy.ReasonEntry{{Code: exec.ReasonWindow, Values: map[string]float64{}, Evidence: []string{}}},
		Why:     "the window is about to close",
	}
}

// finalDecision is the last write of a run. It is not a condition: the job said it is finished (or
// the platform told it to stop), and both mean the same thing — the work that exists now is the work
// that matters. The catalogue already names `job.ending` as urgent, so when a probe is feeding it the
// policy has said this too; this is what makes it true for a deployment that has no probe at all.
//
// It is asked exactly once (`FinalOutcome` is what stops a second one), and it survives the two
// throttles the gate applies to everything else (exec.Env.Final): a final request has no next round.
func finalDecision(now time.Time) policy.Decision {
	return policy.Decision{
		Action: policy.ActionSave, Reason: policy.ReasonEnd, SaveNow: true,
		Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1,
		Reasons: []policy.ReasonEntry{{
			Code: "job.ending", Values: map[string]float64{}, Evidence: []string{"glue:ending|stopping"},
		}},
		Why: "the job is finished; this is the last state worth having",
	}
}

// heldDecision is what the job is told when the gate stops a write: not now, and why. The condition
// stays in the reasons, so a view still shows what is wrong.
func heldDecision(d policy.Decision, gate exec.Result, now time.Time) policy.Decision {
	d.Action, d.SaveNow = policy.ActionSkip, false
	d.Reason = policy.ReasonCalm
	d.Why = "not starting a write: " + gate.Reason
	// How long to wait is the gate's answer, and the caller applies it: a decision is what to do, not
	// when to look again.
	return d
}
