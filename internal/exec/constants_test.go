package exec

import (
	"fmt"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
	"github.com/bolubo/checkpoint-governor/internal/queue"
)

// TestTheConstantsArePinned is H1's constants half. Every floor and cap that a later tuning pass may
// touch is asserted here, so a change is a deliberate change with a test that says what moved. The
// numbers are the ones the documents argue for; if one of them has to move, the argument moves first.
func TestTheConstantsArePinned(t *testing.T) {
	if MinCooldown != 60*time.Second {
		t.Fatalf("MinCooldown = %s, want 60s", MinCooldown)
	}
	if CooldownFactor != 3 {
		t.Fatalf("CooldownFactor = %d, want 3", CooldownFactor)
	}
	if MaxBackoff != 10*time.Minute {
		t.Fatalf("MaxBackoff = %s, want 10m", MaxBackoff)
	}
	if UnknownWindow != 10*time.Minute {
		t.Fatalf("UnknownWindow = %s, want 10m", UnknownWindow)
	}
	if maxAttempts != 32 {
		t.Fatalf("maxAttempts = %d, want 32", maxAttempts)
	}
	if SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", SchemaVersion)
	}
	if produce.DefaultBlindAfter != 3 {
		t.Fatalf("produce.DefaultBlindAfter = %d, want 3", produce.DefaultBlindAfter)
	}
	if queue.DefaultBound != 128 {
		t.Fatalf("queue.DefaultBound = %d, want 128", queue.DefaultBound)
	}
	if queue.DefaultBaseDelay != time.Second {
		t.Fatalf("queue.DefaultBaseDelay = %s, want 1s", queue.DefaultBaseDelay)
	}
	if queue.DefaultMaxDelay != 10*time.Minute {
		t.Fatalf("queue.DefaultMaxDelay = %s, want 10m", queue.DefaultMaxDelay)
	}
	if queue.DefaultResyncEvery != 10*time.Minute {
		t.Fatalf("queue.DefaultResyncEvery = %s, want 10m", queue.DefaultResyncEvery)
	}
	// The two "ten minutes" are on purpose the same number: a job with no controller writes every ten
	// minutes, so the controller's own retry cap never becomes the slower cadence.
	if MaxBackoff != queue.DefaultMaxDelay {
		t.Fatalf("MaxBackoff %s and the queue's retry cap %s must stay equal", MaxBackoff, queue.DefaultMaxDelay)
	}
	fmt.Printf("constants: cooldown %s, backoff cap %s, unknown window %s, history %d, blind after %d, queue bound %d, resync %s\n",
		MinCooldown, MaxBackoff, UnknownWindow, maxAttempts, produce.DefaultBlindAfter, queue.DefaultBound, queue.DefaultResyncEvery)
}
