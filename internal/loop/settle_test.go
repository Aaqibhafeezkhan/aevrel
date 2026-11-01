package loop

import (
	"fmt"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/exec"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// TestSettleJudgesTheDirectory is the truth table of E1: one attempt per possible world, and the
// only evidence is the snapshot the scanner produced.
//
// The copies are dated a few seconds after the attempt started, because "the write we asked for" is
// the copy that appeared after the question — see producedBy in settle.go. The undated cases are
// there on purpose: an undated copy is never attributed to an attempt.
func TestSettleJudgesTheDirectory(t *testing.T) {
	start := time.Unix(0, 0)
	deadline := start.Add(2 * time.Minute)
	produced := start.Add(5 * time.Second)

	cases := []struct {
		name    string
		step    int64
		copies  []policy.Copy
		now     time.Time
		want    exec.Outcome
		wantOut string
	}{
		{
			name: "sealed", step: 400, now: start.Add(30 * time.Second),
			copies: []policy.Copy{{Step: 400, Bytes: 1000, State: policy.CopySealed, CreatedAt: produced}},
			want:   exec.Sealed,
		},
		{
			name: "verified counts as sealed", step: 400, now: start.Add(30 * time.Second),
			copies: []policy.Copy{{Step: 400, Bytes: 1000, State: policy.CopyVerified, CreatedAt: produced}},
			want:   exec.Sealed,
		},
		{
			name: "broken content", step: 400, now: start.Add(30 * time.Second),
			copies: []policy.Copy{{Step: 400, Bytes: 1000, State: policy.CopyBroken, CreatedAt: produced}},
			want:   exec.Broken,
		},
		{
			name: "partial directory past the deadline is abandoned", step: 400, now: start.Add(3 * time.Minute),
			copies: []policy.Copy{{Step: 400, Bytes: 10, State: policy.CopyUnverified, CreatedAt: produced}},
			want:   exec.Abandoned,
		},
		{
			name: "partial directory before the deadline keeps running", step: 400, now: start.Add(time.Minute),
			copies: []policy.Copy{{Step: 400, Bytes: 10, State: policy.CopyUnverified, CreatedAt: produced}},
			want:   exec.Running,
		},
		{
			name: "nothing produced by the deadline is a failure", step: 400, now: start.Add(3 * time.Minute),
			copies: nil,
			want:   exec.Failed,
		},
		{
			name: "an undated copy at another step is not attributed", step: 400, now: start.Add(30 * time.Second),
			copies: []policy.Copy{{Step: 401, Bytes: 1000, State: policy.CopySealed}},
			want:   exec.Running,
		},
		{
			name: "a copy from before the request is not this attempt's product", step: 400, now: start.Add(30 * time.Second),
			copies: []policy.Copy{{Step: 400, Bytes: 1000, State: policy.CopySealed, CreatedAt: start.Add(-time.Minute)}},
			want:   exec.Running,
		},
		{
			name: "a job that reports no step still seals", step: 0, now: start.Add(30 * time.Second),
			copies: []policy.Copy{{Step: 10, Bytes: 1000, State: policy.CopySealed, CreatedAt: produced}},
			want:   exec.Sealed,
		},
	}

	counts := map[exec.Outcome]int{}
	for _, c := range cases {
		attempts := []exec.Attempt{{
			ID: "a", UnitID: "u", Generation: 1, Step: c.step,
			Trigger: exec.TriggerCondition, Reason: "risk.preempt",
			StartedAt: start, Deadline: deadline,
		}}
		s := policy.Snapshot{Copies: c.copies}
		got, _ := Settle(attempts, s, c.now)
		if got[0].Outcome != c.want {
			t.Fatalf("%s: outcome = %q, want %q", c.name, got[0].Outcome, c.want)
		}
		counts[got[0].Outcome]++
	}

	// The acceptance line: three attempts settled as sealed (sealed, verified and the unreported
	// step), one abandoned, one failed; the rest stay running.
	sealed := counts[exec.Sealed]
	fmt.Printf("attempts: %d sealed, %d abandoned, %d failed\n", sealed, counts[exec.Abandoned], counts[exec.Failed])
	if sealed != 3 {
		t.Fatalf("sealed = %d, want 3", sealed)
	}
}

// TestTheEdgesOfAbandonment: a deadline that was already in the past is judged on the first sighting
// (a controller that was down while the write died must not wait), and a clock that jumped backwards
// does not judge a young attempt dead.
func TestTheEdgesOfAbandonment(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	past := []exec.Attempt{{ID: "a", Step: 10, StartedAt: now.Add(-2 * time.Hour), Deadline: now.Add(-time.Hour)}}
	if got, _ := Settle(past, policy.Snapshot{}, now); got[0].Outcome != exec.Failed {
		t.Fatalf("past deadline = %q, want failed", got[0].Outcome)
	}
	young := []exec.Attempt{{ID: "b", Step: 20, StartedAt: now.Add(time.Minute), Deadline: now.Add(2 * time.Minute)}}
	if got, _ := Settle(young, policy.Snapshot{}, now); got[0].Outcome != exec.Running {
		t.Fatalf("attempt from the future = %q, want running", got[0].Outcome)
	}
	fmt.Printf("edges: a past deadline failed, a backwards clock left the attempt running\n")
}

// TestRestartSettlesOnce: a restart loads the same identities, settles them again, and the counts do
// not grow. This is the property the acceptance asks for: settling is idempotent.
func TestRestartSettlesOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(0, 0)
	testutil.Sealed(t, dir, 400, 100)

	first := exec.Ledger{}
	first.Record(exec.NewAttempt("u", 1, 400, exec.TriggerCondition, "risk.preempt", now, now.Add(2*time.Minute)))
	if err := first.Save(dir); err != nil {
		t.Fatal(err)
	}
	settled := 0
	for round := 0; round < 2; round++ { // two rounds = one process, then a restart
		led, err := exec.OpenLedger(dir)
		if err != nil {
			t.Fatal(err)
		}
		at := now.Add(30 * time.Second)
		led.Attempts, _ = Settle(led.Attempts, policy.Snapshot{
			Copies: []policy.Copy{{Step: 400, Bytes: 100, State: policy.CopySealed, CreatedAt: now.Add(10 * time.Second)}},
		}, at)
		if err := led.Save(dir); err != nil {
			t.Fatal(err)
		}
		settled = led.Count().Sealed
	}
	if settled != 1 {
		t.Fatalf("sealed after a restart = %d, want 1", settled)
	}
}
