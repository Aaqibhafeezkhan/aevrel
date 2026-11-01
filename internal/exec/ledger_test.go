package exec

import (
	"testing"
	"time"
)

func finished(id string, outcome Outcome, at time.Time) Attempt {
	return Attempt{ID: id, Outcome: outcome, StartedAt: at, EndedAt: at}
}

func TestTheHistoryIsBounded(t *testing.T) {
	l := Ledger{}
	base := time.Unix(0, 0)
	for i := 0; i < maxAttempts+8; i++ {
		l.Record(finished("a"+itoa(int64(i)), Sealed, base.Add(time.Duration(i)*time.Second)))
	}
	if len(l.Attempts) != maxAttempts {
		t.Fatalf("kept %d attempts, want %d", len(l.Attempts), maxAttempts)
	}
	// The newest ones survive: the oldest eight are gone.
	if _, ok := l.Find("a0"); ok {
		t.Fatal("the oldest attempt survived the bound")
	}
	if _, ok := l.Find("a" + itoa(int64(maxAttempts+7))); !ok {
		t.Fatal("the newest attempt was dropped")
	}
}

func TestAFinishedOutcomeIsImmutable(t *testing.T) {
	at := time.Unix(0, 0)
	l := Ledger{}
	l.Record(finished("x", Sealed, at))
	l.Record(Attempt{ID: "x", Outcome: Running, StartedAt: at})
	got, _ := l.Find("x")
	if got.Outcome != Sealed {
		t.Fatalf("outcome rewritten to %q; a finished attempt must not change", got.Outcome)
	}
}

func TestCountsAreDerivedFromOutcomes(t *testing.T) {
	at := time.Unix(0, 0)
	l := Ledger{}
	for i := 0; i < 3; i++ {
		l.Record(finished("s"+itoa(int64(i)), Sealed, at))
	}
	l.Record(finished("ab", Abandoned, at))
	l.Record(Attempt{ID: "run", StartedAt: at})
	c := l.Count()
	if c.Sealed != 3 || c.Abandoned != 1 || c.Running != 1 || c.Failed != 0 {
		t.Fatalf("counts = %+v", c)
	}
	if want := "attempts: 3 sealed, 1 abandoned, 0 failed, 1 running"; c.String() != want {
		t.Fatalf("counts print %q, want %q", c.String(), want)
	}
}

func TestARestartDoesNotDoubleCount(t *testing.T) {
	dir := t.TempDir()
	at := time.Unix(0, 0)
	l := Ledger{}
	l.Record(finished("a", Sealed, at))
	l.Record(finished("b", Abandoned, at))
	if err := l.Save(dir); err != nil {
		t.Fatal(err)
	}

	again, err := OpenLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Count(); got.Sealed != 1 || got.Abandoned != 1 {
		t.Fatalf("reloaded counts = %+v, want one sealed and one abandoned", got)
	}
	// Recording the same identities again changes nothing: the request is the same attempt.
	again.Record(finished("a", Sealed, at))
	again.Record(finished("b", Abandoned, at))
	if got := again.Count(); got.Sealed != 1 || got.Abandoned != 1 {
		t.Fatalf("counts after re-recording = %+v", got)
	}
}

func TestTheIdentityIsStableAcrossRestarts(t *testing.T) {
	at := time.Unix(0, 0)
	deadline := Deadline(30, at)
	a := NewAttempt("u", 7, 400, TriggerCondition, "risk.preempt", at, deadline)
	b := NewAttempt("u", 7, 400, TriggerCondition, "risk.preempt", at.Add(time.Second), deadline)
	if a.ID != b.ID {
		t.Fatalf("the same request got two identities: %s and %s", a.ID, b.ID)
	}
	if c := NewAttempt("u", 8, 400, TriggerCondition, "risk.preempt", at, deadline); c.ID == a.ID {
		t.Fatal("a new generation must be a new attempt")
	}
	if d := NewAttempt("u", 7, 401, TriggerCondition, "risk.preempt", at, deadline); d.ID == a.ID {
		t.Fatal("a new step must be a new attempt")
	}
}

func TestDeadlineIsTheCooldown(t *testing.T) {
	at := time.Unix(0, 0)
	if d := Deadline(30, at); d.Sub(at) != 90*time.Second {
		t.Fatalf("deadline = %s after start, want 3x30s", d.Sub(at))
	}
	if d := Deadline(1, at); d.Sub(at) != MinCooldown {
		t.Fatalf("deadline = %s for a fast write, want the 60s floor", d.Sub(at))
	}
	if d := Deadline(0, at); d.Sub(at) != UnknownWindow {
		t.Fatalf("deadline = %s with no measurement, want %s", d.Sub(at), UnknownWindow)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var out []byte
	for v > 0 {
		out = append([]byte{byte('0' + v%10)}, out...)
		v /= 10
	}
	return string(out)
}
