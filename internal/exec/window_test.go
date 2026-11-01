package exec

import (
	"fmt"
	"testing"
	"time"
)

var w0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

// A window that is not set is inert: no clock, no signal, nothing to print but the state.
func TestAnOffWindowNeverAsks(t *testing.T) {
	w := NewWindow(0)
	w.Arm(w0, time.Minute)
	if w.Due(w0.Add(72 * time.Hour)) {
		t.Fatal("an off window must never be due")
	}
	if w.On() || w.Infeasible() {
		t.Fatalf("off window = on %v infeasible %v", w.On(), w.Infeasible())
	}
	fmt.Printf("%s · nothing time-based will write\n", w)
}

// The window is armed at the seal it just observed, and it goes out one write-time early so that the
// seal lands inside the span.
func TestTheWindowIsArmedAtTheSealAndLeavesRoomForTheWrite(t *testing.T) {
	w := NewWindow(30 * time.Minute)
	seal := w0.Add(5 * time.Minute) // an exception sealed at 09:05
	w.Arm(seal, 4*time.Minute)

	if got, want := w.DueAt(), seal.Add(26*time.Minute); !got.Equal(want) {
		t.Fatalf("due = %s, want %s (seal + span − W)", got, want)
	}
	if w.Due(seal.Add(20 * time.Minute)) {
		t.Fatal("due a write-time early")
	}
	if !w.Due(seal.Add(26 * time.Minute)) {
		t.Fatal("not due when the request must go out")
	}
	fmt.Printf("%s\n", w)
}

// Without a measured write time the request goes out at the deadline: a shop with no measurement gets
// the span, not an invented lead.
func TestAnUnknownWriteTimeUsesTheWholeSpan(t *testing.T) {
	w := NewWindow(30 * time.Minute)
	w.Arm(w0, 0)
	if got, want := w.DueAt(), w0.Add(30*time.Minute); !got.Equal(want) {
		t.Fatalf("due = %s, want %s", got, want)
	}
}

// The deadman property: any write that seals restarts the window from that seal, so the span is a
// bound on staleness and not a periodic request.
func TestEverySealRestartsTheWindow(t *testing.T) {
	w := NewWindow(30 * time.Minute)
	w.Arm(w0, 0)
	if !w.Due(w0.Add(30 * time.Minute)) {
		t.Fatal("the window must close after the span")
	}
	// An exception sealed at 09:20, before the window closed.
	w.Arm(w0.Add(20*time.Minute), 0)
	if w.Due(w0.Add(30 * time.Minute)) {
		t.Fatal("a seal before the deadline must move the window, not leave it due")
	}
	if got, want := w.DueAt(), w0.Add(50*time.Minute); !got.Equal(want) {
		t.Fatalf("due = %s, want %s", got, want)
	}
	fmt.Printf("refresh: exception sealed 09:20 moved the due from 09:30 to 09:50\n")
}

// A span shorter than one write cannot hold. The window is due immediately — and the status says the
// bound cannot hold instead of pretending.
func TestASpanShorterThanOneWriteCannotHold(t *testing.T) {
	w := NewWindow(30 * time.Minute)
	w.Arm(w0, 43*time.Minute)
	if !w.Infeasible() {
		t.Fatal("30m span with a 43m write must be infeasible")
	}
	if !w.Due(w0) {
		t.Fatal("an infeasible window is due immediately")
	}
	if got, want := w.String(), "window 30m00s is shorter than one write (43m00s) — this bound cannot hold"; got != want {
		t.Fatalf("string = %q, want %q", got, want)
	}
	fmt.Println(w)
}

// Cold start: a process that opens on a directory with nothing in it aims the window from its own
// start, not from zero, and an existing seal aims it from that seal.
func TestColdStartAndRestart(t *testing.T) {
	w := NewWindow(30 * time.Minute)
	w.Arm(w0, 0)
	if got, want := w.DueAt(), w0.Add(30*time.Minute); !got.Equal(want) {
		t.Fatalf("cold start due = %s, want %s", got, want)
	}
	// A restart that finds a seal 10 minutes ago aims from the seal, not from the restart.
	w2 := NewWindow(30 * time.Minute)
	w2.Arm(w0.Add(-10*time.Minute), 0)
	if got, want := w2.DueAt(), w0.Add(20*time.Minute); !got.Equal(want) {
		t.Fatalf("restart due = %s, want %s", got, want)
	}
}

// A window that has never been armed is not due, however long the process runs: nothing has been
// written yet, and the span is about the newest checkpoint.
func TestAnUnarmedWindowIsNotDue(t *testing.T) {
	w := NewWindow(30 * time.Minute)
	if w.Due(w0.Add(72 * time.Hour)) {
		t.Fatal("an unarmed window must not be due")
	}
	if !w.DueAt().IsZero() {
		t.Fatal("an unarmed window has no due date")
	}
	if got, want := w.String(), "window 30m00s (not armed yet)"; got != want {
		t.Fatalf("string = %q, want %q", got, want)
	}
}
