package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGlue(t *testing.T, dir, name, body string) {
	t.Helper()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A job that has written nothing has not been stopped and has not declared a size: every flag is
// false, and no field pretends to be a measurement.
func TestGlueIsEmptyWithoutFiles(t *testing.T) {
	g := ReadGlue(t.TempDir())
	if g.HasExpected || g.HasSeconds || g.StopRequested || g.MayExit {
		t.Fatalf("empty glue = %+v", g)
	}
}

// The four facts in one read, each with its own presence flag.
func TestGlueReadsTheFourFacts(t *testing.T) {
	dir := t.TempDir()
	writeGlue(t, dir, "expected-bytes", "1258291200")
	writeGlue(t, dir, "stopping", "1")
	writeGlue(t, dir, "may-exit", "1")
	writeGlue(t, dir, "seconds-left", "27")

	g := ReadGlue(dir)
	if !g.HasExpected || g.ExpectedBytes != 1258291200 {
		t.Fatalf("expected = %+v", g)
	}
	if !g.StopRequested || !g.MayExit {
		t.Fatalf("stop/exit = %+v", g)
	}
	if !g.HasSeconds || g.SecondsLeft != 27 {
		t.Fatalf("secondsLeft = %+v", g)
	}
}

// The two phases must be distinguishable: a stop request without `may-exit` is a job that still has
// to write, and that is the state the executor must not mistake for "safe to kill".
func TestAStopWithoutMayExitIsNotAnExit(t *testing.T) {
	dir := t.TempDir()
	writeGlue(t, dir, "stopping", "1")
	g := ReadGlue(dir)
	if !g.StopRequested {
		t.Fatal("the stop request was not seen")
	}
	if g.MayExit {
		t.Fatal("a job that has not finished its final write must not look safe to kill")
	}
}

// The third state, and the one the handshake turns on: the job has said training is over (`ending`),
// which is not the same fact as "the platform told it to stop". A reader that conflates them either
// asks a finished run for another checkpoint or releases a stopping one before its last write exists.
func TestEndingIsItsOwnFact(t *testing.T) {
	dir := t.TempDir()
	writeGlue(t, dir, "ending", "1")
	g := ReadGlue(dir)
	if !g.Ending {
		t.Fatal("`ending` was not seen: the finish handshake would never start")
	}
	if g.StopRequested {
		t.Fatal("`ending` was read as a stop request: two different facts, one answer")
	}
}
