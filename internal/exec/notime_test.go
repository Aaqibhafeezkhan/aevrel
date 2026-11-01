package exec

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// X2: the deadline door. The job says how long it has; we say how long a write takes; if the second
// does not fit in the first, the write would be killed halfway — a partial write and no checkpoint.
// Refusing is the only answer that is not a lie, and the refusal names the arithmetic. The price (the
// work since the last checkpoint) is added by the loop, which is the side that can read the directory.
func TestGateRefusesWhenOneWriteDoesNotFit(t *testing.T) {
	d := policy.Decision{
		UnitID: "u", Generation: 1, Action: policy.ActionSave, Reason: policy.ReasonRisk,
		Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1,
		Reasons: []policy.ReasonEntry{{Code: "risk.preempt"}},
	}
	now := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)

	r := Gate(d, nil, Env{
		HaveSeconds:  true,
		SecondsLeft:  30,
		WriteSeconds: 240, // 4m, plus the default 10s teardown margin
	}, now)

	if r.Verdict != Refuse || r.Reason != "no-time" {
		t.Fatalf("verdict = %s/%s, want refuse/no-time", r.Verdict, r.Reason)
	}
	want := "no-time (need 4m10s, have 30s)"
	if r.Detail != want {
		t.Fatalf("detail = %q, want %q", r.Detail, want)
	}
	fmt.Printf("gate: %s\n", r.Detail)
}

// The job's own teardown is part of the margin: a write that ends exactly when the job dies leaves no
// time to flush anything, so the arithmetic adds it, and the job's own number wins over the default.
func TestGateAddsTheTeardownToTheMargin(t *testing.T) {
	d := policy.Decision{Action: policy.ActionSave, Reason: policy.ReasonRisk, KeepCopies: 1}
	now := time.Now()
	r := Gate(d, nil, Env{
		HaveSeconds: true, SecondsLeft: 245,
		WriteSeconds: 240, Teardown: 10 * time.Second,
	}, now)
	if r.Reason != "no-time" {
		t.Fatalf("4m write + 10s teardown against 4m5s = %s, want no-time", r.Reason)
	}
	// Exactly enough time and it fits.
	r = Gate(d, nil, Env{
		HaveSeconds: true, SecondsLeft: 250,
		WriteSeconds: 240, Teardown: 10 * time.Second,
	}, now)
	if r.Verdict != Start {
		t.Fatalf("exactly enough time = %s/%s, want start", r.Verdict, r.Reason)
	}
}

// Without a job deadline, or without a duration we can trust, the gate refuses nothing: a refusal on
// ignorance would guarantee the loss the window was the last chance to avoid. It starts and says that
// the check was skipped.
func TestGateSkipsTheDoorWhenItHasNothingToCompare(t *testing.T) {
	d := policy.Decision{Action: policy.ActionSave, Reason: policy.ReasonRisk, KeepCopies: 1}
	now := time.Now()

	if r := Gate(d, nil, Env{SecondsLeft: 60}, now); r.Reason == "no-time" {
		t.Fatal("no write duration must not produce a no-time refusal")
	}
	r := Gate(d, nil, Env{HaveSeconds: true, SecondsLeft: 30}, now)
	if r.Reason == "no-time" {
		t.Fatal("no write duration must not produce a no-time refusal")
	}
	if !strings.Contains(r.Warning, "write cost unknown") {
		t.Fatalf("warning = %q, want the skipped check named", r.Warning)
	}
	fmt.Printf("gate: %s\n", r.Warning)
}

// A no-time refusal is a refusal, not a promise: the gate must not also hand out a wait, because
// waiting cannot create time.
func TestNoTimeDoesNotAskTheCallerToWait(t *testing.T) {
	d := policy.Decision{Action: policy.ActionSave, Reason: policy.ReasonRisk, KeepCopies: 1}
	r := Gate(d, nil, Env{
		HaveSeconds: true, SecondsLeft: 1, WriteSeconds: 60,
	}, time.Now())
	if r.Wait != 0 {
		t.Fatalf("wait = %s, want none: time does not come back", r.Wait)
	}
}
