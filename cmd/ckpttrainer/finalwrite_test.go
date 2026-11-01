package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// The last state of a finished run is on disk, and the job waits for the controller's answer
// instead of releasing itself. The job writes `ending`, the governor asks for one write, the job
// makes it — and only the governor says `may-exit`, because only the governor can see whether a
// write is still in flight. Exiting first is how the last state is lost.
func TestTrainingEndWaitsForTheFinalWrite(t *testing.T) {
	dir := t.TempDir()
	// A live plan that asks for nothing: the job has a governor, so it must wait for the request
	// instead of keeping the promise itself.
	publishedPlan(t, dir, policy.Decision{
		UnitID: "u", Generation: 1, Reason: policy.ReasonEnd, Version: "risk-v1",
	}, time.Now(), 20*time.Second)

	c := config{dir: dir, steps: 8, stepMS: 3, writeMS: 2, shardBytes: 16, ranks: 1, members: 1,
		grace: 3 * time.Second}
	done := make(chan error, 1)
	go func() { done <- runWith(c, make(chan struct{})) }()

	state := filepath.Join(dir, advise.BaseDir)
	waitForFile(t, dir, "ending", 2*time.Second)
	if wrote := checkpoints(dir); len(wrote) > 0 {
		t.Fatalf("the job wrote %v before anybody asked it to", wrote)
	}

	// The controller answers the ending with one request, the way it does in production.
	publishedPlan(t, dir, policy.Decision{
		UnitID: "u", Generation: 1, Action: policy.ActionSave, Reason: policy.ReasonEnd, SaveNow: true,
		RequestID: "final-1", Why: "job.ending: training is finishing", Version: "risk-v1",
	}, time.Now(), 20*time.Second)

	// The write lands...
	deadline := time.Now().Add(2 * time.Second)
	for len(checkpoints(dir)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if wrote := checkpoints(dir); len(wrote) == 0 {
		t.Fatalf("no final checkpoint in %s", dir)
	}
	// ...and the job still does not leave: nobody has answered yet.
	select {
	case <-done:
		t.Fatal("the job left without being released: that is a job inventing its own protocol")
	case <-time.After(100 * time.Millisecond):
	}

	// The controller answers.
	if err := os.WriteFile(filepath.Join(state, "may-exit"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the job did not leave after being released")
	}
	if _, err := os.Stat(filepath.Join(state, "release-timeout")); err == nil {
		t.Fatal("a released job recorded a release timeout")
	}
	fmt.Printf("ending → final write %s → may-exit\n", checkpoints(dir)[0])
}

// The wait is bounded: a job whose controller never answers must still exit, or a finished training
// would hang for ever. The grace period is the bound, and leaving late is recorded.
func TestTrainingEndDoesNotHangWhenNobodyAnswers(t *testing.T) {
	dir := t.TempDir()
	c := config{dir: dir, steps: 5, stepMS: 2, writeMS: 2, shardBytes: 16, ranks: 1, members: 1,
		grace: 200 * time.Millisecond}
	start := time.Now()
	if err := runWith(c, make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the job waited %s for an answer that never came", took)
	}
	if _, err := os.Stat(filepath.Join(dir, advise.BaseDir, "release-timeout")); err == nil {
		t.Fatal("nobody was asked for an answer, so there was none to time out")
	}
}

// The final write has no configuration: with no governor at all, the end of the run is still saved. The job keeps
// the promise itself, and says so, because "the last state is on disk" is the one thing a finished
// training may not lose.
func TestTheEndOfARunIsSavedEvenWithNoGovernor(t *testing.T) {
	dir := t.TempDir()
	if err := runWith(config{dir: dir, steps: 5, stepMS: 2, writeMS: 2, shardBytes: 16, ranks: 1,
		members: 1, grace: 200 * time.Millisecond}, make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	wrote := checkpoints(dir)
	if len(wrote) == 0 {
		t.Fatal("nobody asked and there was no governor: the last state is still a promise")
	}
	fmt.Printf("no governor → the job kept the end-of-run promise itself (%s)\n", wrote[0])
}
