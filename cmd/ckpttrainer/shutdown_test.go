package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// The stop, first half: a job that is told to stop says so, before it starts spending its grace period.
// The channel is the seam onSignal fills in the real binary, so this test never signals itself.
func TestStopWritesTheStoppingFile(t *testing.T) {
	dir := t.TempDir()
	c := config{
		dir: dir, steps: 100000, stepMS: 5, writeMS: 10,
		shardBytes: 16, ranks: 1, members: 1, grace: 300 * time.Millisecond,
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runWith(c, stop) }()

	// Let it run a few steps, then stop it the way a platform would.
	time.Sleep(50 * time.Millisecond)
	close(stop)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job kept running after the stop signal: a grace period must end")
	}

	if _, err := os.Stat(filepath.Join(dir, ".ckpt-governor", "stopping")); err != nil {
		t.Fatalf("no stopping file after the signal: %v", err)
	}
}

// The stop, the second phase: after the final write lands, the job says it is safe to kill instead of
// burning the rest of its grace period. The plan is published *after* the stop, so the write only
// happens because the controller answered the stop.
// A job that releases itself has no protocol, only a habit. The final write makes it say it is done
// (`ending`); whether it may leave is the controller's answer (`may-exit`), and until that answer
// arrives the job waits — bounded by its own grace period.
func TestAFinalWriteWaitsForTheController(t *testing.T) {
	dir := t.TempDir()
	c := config{
		dir: dir, steps: 100000, stepMS: 5, writeMS: 10,
		shardBytes: 16, ranks: 1, members: 1, grace: 3 * time.Second,
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runWith(c, stop) }()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	time.Sleep(50 * time.Millisecond) // the job has written the stop request by now

	d := policy.Decision{
		UnitID: "u", Generation: 1, Action: policy.ActionSave, Reason: policy.ReasonRisk,
		Why: "risk.shutdown", Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1, Version: "risk-v1",
		RequestID: "req-1",
	}
	if _, err := advise.Publish(dir, d, 1, time.Now(), 0); err != nil {
		t.Fatal(err)
	}

	// The job finishes its final write and says so.
	waitForFile(t, dir, "ending", 2*time.Second)

	// It must not leave on its own: nobody has told it the write was the last one worth waiting for.
	select {
	case <-done:
		t.Fatal("the job left without being released: that is a job inventing its own protocol")
	case <-time.After(100 * time.Millisecond):
	}

	// The controller answers.
	if err := os.WriteFile(filepath.Join(dir, ".ckpt-governor", "may-exit"), []byte("1"), 0o644); err != nil {
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
	if _, err := os.Stat(filepath.Join(dir, ".ckpt-governor", "release-timeout")); err == nil {
		t.Fatal("a released job recorded a release timeout")
	}
}

// If nobody ever answers, the job leaves when its grace period ends and records that nobody did.
// Leaving silently is how a last write is lost without anybody noticing.
func TestAnUnansweredReleaseLeavesATrace(t *testing.T) {
	dir := t.TempDir()
	c := config{
		dir: dir, steps: 100000, stepMS: 5, writeMS: 10,
		shardBytes: 16, ranks: 1, members: 1, grace: 300 * time.Millisecond,
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runWith(c, stop) }()

	time.Sleep(30 * time.Millisecond)
	close(stop)
	time.Sleep(30 * time.Millisecond)

	// A final write has to happen, or there is nothing to wait for and the job leaves.
	d := policy.Decision{
		UnitID: "u", Generation: 1, Action: policy.ActionSave, Reason: policy.ReasonRisk,
		Why: "risk.shutdown", Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1, Version: "risk-v1",
	}
	if _, err := advise.Publish(dir, d, 1, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job never left, even after its grace period")
	}
	if _, err := os.Stat(filepath.Join(dir, ".ckpt-governor", "release-timeout")); err != nil {
		t.Fatalf("no release-timeout marker: %v", err)
	}
}

// waitForFile polls for a governor file, because the job writes it on its own schedule.
func waitForFile(t *testing.T, dir, name string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".ckpt-governor", name)); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared within %s", name, within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
