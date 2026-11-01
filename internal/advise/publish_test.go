package advise

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestPublishWritesOnceAndThenSkips(t *testing.T) {
	dir := t.TempDir()
	d := exampleDecision()

	wrote, err := Publish(dir, d, 7, t0, 0)
	if err != nil || !wrote {
		t.Fatalf("first publish = %v, %v; want true, nil", wrote, err)
	}
	wrote, err = Publish(dir, d, 7, t0, 0)
	if err != nil || wrote {
		t.Fatalf("second publish = %v, %v; want false, nil (nothing changed)", wrote, err)
	}
}

func TestPublishRenewsWhenTheFileIsNearExpiry(t *testing.T) {
	dir := t.TempDir()
	d := exampleDecision()
	ttl := time.Minute

	if _, err := Publish(dir, d, 7, t0, ttl); err != nil {
		t.Fatal(err)
	}
	// Fifty seconds later: ten seconds of life left, below a third of the minute. This must renew.
	later := t0.Add(50 * time.Second)
	wrote, err := Publish(dir, d, 7, later, ttl)
	if err != nil || !wrote {
		t.Fatalf("renewal = %v, %v; want true, nil", wrote, err)
	}
	s, ok := readState(filepath.Join(dir, BaseDir, stateName))
	if !ok {
		t.Fatal("the file is gone after a renewal")
	}
	if want := later.Add(ttl).Unix(); s.ExpiresAt != want {
		t.Fatalf("expiresAt = %d, want %d", s.ExpiresAt, want)
	}
}

func TestPublishWritesWhenTheDecisionChanges(t *testing.T) {
	dir := t.TempDir()
	d := exampleDecision()
	if _, err := Publish(dir, d, 7, t0, 0); err != nil {
		t.Fatal(err)
	}

	d.Action = policy.ActionSkip
	d.Reason = policy.ReasonEnd
	wrote, err := Publish(dir, d, 7, t0, 0)
	if err != nil || !wrote {
		t.Fatalf("a changed action must be written: %v, %v", wrote, err)
	}
}

func TestPublishLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Publish(dir, exampleDecision(), 7, t0, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, BaseDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != stateName {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %v, want only %s", names, stateName)
	}
}

func TestPublishCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "checkpoints")
	if _, err := Publish(dir, exampleDecision(), 7, t0, 0); err != nil {
		t.Fatalf("publish into a missing directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, BaseDir, stateName)); err != nil {
		t.Fatal(err)
	}
}
