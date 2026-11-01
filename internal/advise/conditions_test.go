package advise

import (
	"strings"
	"testing"
	"time"
)

// A condition that has been true for two minutes and one that just flipped lead to different
// decisions about whether to trust it, so the view has to say which one it is looking at.
func TestTheViewShowsHowLongAConditionHasBeenTrue(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := Status{Conditions: map[string]Condition{
		"store.low-space": {Value: "1", Since: now.Add(-2*time.Minute - 13*time.Second).Unix()},
		"job.ending":      {Value: "0", Since: now.Add(-time.Hour).Unix()},
	}}
	var b strings.Builder
	Render(&b, s, now, 20*time.Second)
	out := b.String()

	if !strings.Contains(out, "store.low-space 1 (2m13s)") {
		t.Fatalf("a true condition must show its age:\n%s", out)
	}
	// A condition that is false does not need an age: nobody decides anything on it.
	if strings.Contains(out, "job.ending 0 (") {
		t.Fatalf("a false condition should not carry an age:\n%s", out)
	}
}

// "I cannot see" without the reason is the least useful line a view can print.
func TestAnUnknownConditionCarriesItsReason(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := Status{Conditions: map[string]Condition{
		"risk.preempt": {Value: "unknown", Reason: "probe timed out", Since: now.Unix()},
	}}
	var b strings.Builder
	Render(&b, s, now, 20*time.Second)
	if !strings.Contains(b.String(), "risk.preempt unknown (probe timed out)") {
		t.Fatalf("the reason was dropped:\n%s", b.String())
	}
}
