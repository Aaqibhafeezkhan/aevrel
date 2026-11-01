package advise

import (
	"strings"
	"testing"
	"time"
)

func TestRenderAnswersWhatAPersonAsks(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := Status{
		UnitID:     "train/llama",
		Generation: 3,
		Phase:      PhaseWriting,

		SealStep: 12000,
		SealAt:   now.Add(-3*time.Minute - 10*time.Second).Unix(),

		WriteStep:      12200,
		WriteStartedAt: now.Add(-2*time.Minute - 10*time.Second).Unix(),
		WriteProgress:  0.45,
		WriteExpected:  180,

		AdviceAt: now.Add(-2*time.Minute - 40*time.Second).Unix(),
		SeenAt:   now.Add(-2*time.Minute - 10*time.Second).Unix(),

		Writes:   []WriteRecord{{Step: 12000, Bytes: 900_000_000, State: "sealed", Seconds: 30}},
		Failures: 1,
		Last:     &Failure{Code: "shard_short", At: now.Add(-20 * time.Minute).Unix(), Detail: "shard 1 short"},
	}

	var b strings.Builder
	Render(&b, s, now, 10*time.Minute)
	out := b.String()

	for _, want := range []string{
		"generation 3", "phase writing",
		"step 12000", "age 3m10s", "bound 10m0s",
		"45%", "2m10s elapsed", "3m0s expected",
		"seen in the directory",
		"step 12000 sealed 30s 900MB",
		"shard_short", "shard 1 short",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the view is missing %q:\n%s", want, out)
		}
	}
}

// A capability that is read once every thirty seconds must look like that in the view; otherwise
// nobody can tell an expensive capability from a cheap one.
func TestRenderShowsHowOftenEachCapabilityWasRead(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := Status{
		UnitID: "train/llama", Phase: PhaseSealed,
		Overhead: Overhead{Signals: map[string]int{"gpu.contention": 2, "free": 41}},
	}
	var b strings.Builder
	Render(&b, s, now, 10*time.Minute)
	out := b.String()
	for _, want := range []string{"signals", "gpu.contention 2", "free 41"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the view is missing %q:\n%s", want, out)
		}
	}
}

// A stored relative time would age; this test pins the rule by moving the clock and checking that
// the absolute timestamps do not move.
func TestRenderComputesRelativeTimeAtRender(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := Status{UnitID: "u", Phase: PhaseSealed, SealStep: 1, SealAt: base.Add(-time.Minute).Unix()}

	var first, later strings.Builder
	Render(&first, s, base, 10*time.Minute)
	Render(&later, s, base.Add(time.Hour), 10*time.Minute)

	if !strings.Contains(first.String(), "age 1m0s") {
		t.Fatalf("first view:\n%s", first.String())
	}
	if !strings.Contains(later.String(), "age 1h1m0s") {
		t.Fatalf("later view:\n%s", later.String())
	}
	if !strings.Contains(later.String(), base.Add(-time.Minute).UTC().Format(time.RFC3339)) {
		t.Fatalf("the absolute time must not move:\n%s", later.String())
	}
}
