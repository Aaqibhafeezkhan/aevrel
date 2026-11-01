package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// X4: the finish handshake, controller half (decision 0017).
//
// The job says what it is (`stopping` when it was told to stop, `ending` when training finished) and
// then keeps running. The controller asks for exactly one last write and answers `may-exit` once that
// answer is settled — it is the side that can see what is in flight, so the job cannot be the side
// that decides. What the controller says about the phase is one sentence, once per phase change:
//
//	finish: the job is done and no final write has landed (12m of work at risk)
//	finish: the job is done; its final write is in flight
//	finish: the final write is settled, the job may go
func TestTheFinishHandshakeIsReportedInPhases(t *testing.T) {
	t.Run("done, no final write yet", func(t *testing.T) {
		dir := t.TempDir()
		now := t0
		writeSealed(t, dir, 10, 100)

		l := open(t, dir, &now)
		if _, err := l.Round(); err != nil { // the seal is observed at t0: that is what is at risk
			t.Fatal(err)
		}
		now = now.Add(12 * time.Minute)
		writePresence(t, dir, GlueStopping)
		st, err := l.Round()
		if err != nil {
			t.Fatal(err)
		}
		note := noteWith(st.Notes, "finish: the job is done and no final write has landed")
		if note == "" {
			t.Fatalf("no note about the finish: %v", st.Notes)
		}
		if !strings.Contains(note, "12m of work at risk") {
			t.Fatalf("the note does not say what is at risk: %q", note)
		}
		// Nothing has been asked for in this fixture (no stop function, no condition), so there is
		// nothing to answer yet — and an answer nobody asked for is a job that leaves too early.
		if mayExitExists(dir) {
			t.Fatal("may-exit was written before the final write was asked for")
		}
		fmt.Printf("%s → nothing asked yet, so nothing released\n", note)

		// A minute later, nothing new has happened: the sentence would only differ in its age.
		now = now.Add(time.Minute)
		again, err := l.Round()
		if err != nil {
			t.Fatal(err)
		}
		if noteWith(again.Notes, "finish:") != "" {
			t.Fatalf("the same phase was said twice: %v", again.Notes)
		}
	})

	t.Run("the final write is in flight", func(t *testing.T) {
		dir := t.TempDir()
		now := t0
		writeSealed(t, dir, 10, 100)
		writeGlue(t, dir, "step", "11") // the job has moved past the checkpoint it has
		writePresence(t, dir, GlueStopping)

		l, err := Open(Config{
			Dir: dir, UnitID: "train/llama", Bounds: bounds(), Policy: policy.Risk{},
			Now: func() time.Time { return now }, Probes: []produce.Probe{&always{"risk.shutdown"}},
			StopRequested: func() bool { return true },
			VerifyEvery:   -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		st, err := l.Round()
		if err != nil {
			t.Fatal(err)
		}
		if st.Attempts.Running != 1 {
			t.Fatalf("attempts = %s, want the final write running", st.Attempts)
		}
		note := noteWith(st.Notes, "finish: the job is done; its final write is in flight")
		if note == "" {
			t.Fatalf("no note about the write in flight: %v", st.Notes)
		}
		if mayExitExists(dir) {
			t.Fatal("may-exit was written while the final write was still running")
		}
		fmt.Printf("%s → %s\n", note, st.Attempts)
	})

	t.Run("the final write is settled and the job is released", func(t *testing.T) {
		dir := t.TempDir()
		now := t0
		writeSealed(t, dir, 10, 100)
		writeGlue(t, dir, "step", "10")
		writePresence(t, dir, GlueStopping)

		l, err := Open(Config{
			Dir: dir, UnitID: "train/llama", Bounds: bounds(), Policy: policy.Risk{},
			Now:           func() time.Time { return now },
			StopRequested: func() bool { return true },
			VerifyEvery:   -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		st, err := l.Round()
		if err != nil {
			t.Fatal(err)
		}
		// The step the job stands on is already on disk: asking for it again is a write nobody needs,
		// so the answer is "already on disk" and the job may go.
		if got := l.Ledger().Status.FinalOutcome; got != "already on disk at the current step" {
			t.Fatalf("final outcome = %q, want the step it stands on", got)
		}
		note := noteWith(st.Notes, "finish: the final write is settled, the job may go")
		if note == "" {
			t.Fatalf("no note about the release: %v", st.Notes)
		}
		// The permission is ours to write now: we are the side that can see there is nothing in flight.
		if body := mustRead(t, filepath.Join(dir, advise.BaseDir, GlueMayExit)); body != "1" {
			t.Fatalf("may-exit = %q, want 1 (written by the controller)", body)
		}
		fmt.Printf("%s → may-exit=1\n", note)
	})

	t.Run("no finish, no note", func(t *testing.T) {
		dir := t.TempDir()
		now := t0
		writeSealed(t, dir, 10, 100)
		l := open(t, dir, &now)
		st, err := l.Round()
		if err != nil {
			t.Fatal(err)
		}
		if noteWith(st.Notes, "finish:") != "" {
			t.Fatalf("a note about a finish that never happened: %v", st.Notes)
		}
	})
}

// writePresence creates the presence files the job writes; several at once when a phase needs two.
// The number files (`expected-bytes`, …) use `writeGlue` next door.
func writePresence(t *testing.T, dir string, names ...string) {
	t.Helper()
	base := filepath.Join(dir, advise.BaseDir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(base, name), []byte("1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func mayExitExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, advise.BaseDir, GlueMayExit))
	return err == nil
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func noteWith(notes []string, prefix string) string {
	for _, n := range notes {
		if strings.HasPrefix(n, prefix) {
			return n
		}
	}
	return ""
}
