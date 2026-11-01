package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stoppingDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".ckpt-governor"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func running(writeSeconds float64, at time.Time) Attempt {
	a := NewAttempt("u", 1, 7, TriggerCondition, "risk.shutdown", at, Deadline(writeSeconds, at))
	a.StartedAt = at
	return a
}

// The user's stop, executor half. The handshake exists so the job does not leave while its last
// write is still running, and does not wait forever if that write hangs.
func TestReleaseWaitsForTheFinalWriteThenOpensTheDoor(t *testing.T) {
	dir := stoppingDir(t)
	now := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)

	// A write is running with 4m10s of budget: the job waits, because this is what the grace period
	// was for.
	a := running(250, now)
	released, why, err := Release(dir, []Attempt{a}, now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if released || why != "writing" {
		t.Fatalf("mid-write = %v/%q, want held", released, why)
	}
	if Released(dir) {
		t.Fatal("may-exit was written while a write was still running")
	}

	// The write finished. The job may go, and the cost it spent is the number a person asks for.
	a.EndedAt = now.Add(250 * time.Second)
	a.Outcome = Sealed
	released, why, err = Release(dir, []Attempt{a}, a.EndedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !released || why != "no write in flight" {
		t.Fatalf("after the write = %v/%q", released, why)
	}
	if !Released(dir) {
		t.Fatal("may-exit was not written after the last write finished")
	}
	fmt.Printf("stop: intent → save → sealed %s → mayExit=1\n",
		time.Duration(float64(time.Second)*a.Cost()).Round(time.Second))
}

// A hung write must not hold the user: its own deadline is the bound.
func TestReleaseDoesNotHoldTheUserPastTheDeadline(t *testing.T) {
	dir := stoppingDir(t)
	now := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	a := running(60, now)

	// Past the attempt's own deadline, whatever factor the deadline rule applied.
	released, why, err := Release(dir, []Attempt{a}, a.Deadline.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !released || why != "deadline passed" {
		t.Fatalf("past the deadline = %v/%q, want released", released, why)
	}
}

// Nothing in flight means nothing to wait for, even if the job is stopping for the first time.
func TestReleaseWithNothingRunning(t *testing.T) {
	dir := stoppingDir(t)
	released, _, err := Release(dir, nil, time.Now())
	if err != nil || !released || !Released(dir) {
		t.Fatalf("released=%v err=%v file=%v", released, err, Released(dir))
	}
}

// Writing the release twice must be safe: a controller that restarts mid-handshake writes it again.
func TestReleaseIsIdempotent(t *testing.T) {
	dir := stoppingDir(t)
	for i := 0; i < 3; i++ {
		if _, _, err := Release(dir, nil, time.Now()); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".ckpt-governor", MayExitFile))
	if err != nil || string(raw) != "1" {
		t.Fatalf("file = %q, err = %v", raw, err)
	}
}
