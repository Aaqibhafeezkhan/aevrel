package queue

import (
	"fmt"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// TestAKeyIsProcessedOnceNotOncePerAdd is Q1's acceptance: a thousand events for one unit are one
// reconcile. The queue coalesces by key, and the counters say so.
func TestAKeyIsProcessedOnceNotOncePerAdd(t *testing.T) {
	q := New(Options{})
	for i := 0; i < 1000; i++ {
		q.Add("train/llama")
	}
	if s := q.Stats(); s.Queued != 1 || s.Waiting != 1 {
		t.Fatalf("after 1000 adds: %s, want one queued key", s)
	}
	key := q.Get(t0)
	if key != "train/llama" {
		t.Fatalf("get = %q, want the dirty key", key)
	}
	q.Done(key)
	s := q.Stats()
	if s.Processed != 1 || s.Waiting != 0 {
		t.Fatalf("after one reconcile: %s, want one processed key and nothing waiting", s)
	}
	fmt.Printf("%s\n", s)
}

// TestAChangeDuringAReconcileIsNotLost: an add that arrives while the key is being processed marks
// it dirty again, so the next round picks it up. This is the level-triggered rule: nothing depends
// on the event being delivered before the reconcile ended.
func TestAChangeDuringAReconcileIsNotLost(t *testing.T) {
	q := New(Options{})
	q.Add("u")
	key := q.Get(t0)
	if key != "u" {
		t.Fatal("the key was not handed out")
	}
	q.Add("u") // the world changed while we were looking at it
	q.Done(key)
	if q.Len() != 1 {
		t.Fatalf("waiting = %d after a change during the reconcile, want 1", q.Len())
	}
	if key := q.Get(t0); key != "u" {
		t.Fatalf("second get = %q, want the key again", key)
	}
	q.Done("u")
	if s := q.Stats(); s.Processed != 2 || s.Waiting != 0 {
		t.Fatalf("after two reconciles: %s", s)
	}
}

// TestAFailureIsRetriedWithAGrowingPause: a failed reconcile comes back, and not immediately: the
// pause doubles per failure and is capped, so a stuck unit is retried every interval rather than
// abandoned or hammered.
func TestAFailureIsRetriedWithAGrowingPause(t *testing.T) {
	q := New(Options{BaseDelay: 10 * time.Second, MaxDelay: 100 * time.Second})
	q.Add("u")
	q.Get(t0)
	q.Fail("u", t0)
	if at := q.NotBefore("u"); at != t0.Add(10*time.Second) {
		t.Fatalf("first retry at %s, want %s", at, t0.Add(10*time.Second))
	}
	if key := q.Get(t0.Add(5 * time.Second)); key != "" {
		t.Fatalf("get before the pause = %q, want nothing", key)
	}
	if key := q.Get(t0.Add(10 * time.Second)); key != "u" {
		t.Fatalf("get after the pause = %q, want u", key)
	}

	// Second failure: 20s. A success in between would have reset it.
	q.Fail("u", t0.Add(10*time.Second))
	if at := q.NotBefore("u"); at != t0.Add(30*time.Second) {
		t.Fatalf("second retry at %s, want %s", at, t0.Add(30*time.Second))
	}
	for i := 3; i <= 10; i++ {
		q.Fail("u", t0)
	}
	if at := q.NotBefore("u"); at != t0.Add(100*time.Second) {
		t.Fatalf("capped retry at %s, want the 100s cap", at)
	}

	// A success forgets the pause and the failure count.
	q.Get(t0.Add(100 * time.Second))
	q.Done("u")
	q.Add("u")
	if key := q.Get(t0.Add(100 * time.Second)); key != "u" {
		t.Fatalf("get after a success = %q, want u: Done must forget", key)
	}
	fmt.Printf("backoff: %s, %s, capped at %s\n",
		Backoff(Options{BaseDelay: 10 * time.Second, MaxDelay: 100 * time.Second}, 1),
		Backoff(Options{BaseDelay: 10 * time.Second, MaxDelay: 100 * time.Second}, 2),
		Options{MaxDelay: 100 * time.Second}.maxDelay())
}

// TestAFullQueueDropsInsteadOfGrowing: the bound is what keeps a controller's memory finite, and a
// dropped key is a latency problem, not lost work — the resync floor adds it back.
func TestAFullQueueDropsInsteadOfGrowing(t *testing.T) {
	q := New(Options{Bound: 2})
	q.Add("a")
	q.Add("b")
	if q.Add("c") {
		t.Fatal("a full queue accepted a third key")
	}
	if s := q.Stats(); s.Waiting != 2 || s.Dropped != 1 {
		t.Fatalf("after a refused add: %s", s)
	}
}

// TestTheResyncFloorFindsWhatTheEventsMissed is Q2's acceptance: the unit whose event was never
// delivered is reconciled at the floor, and the floor does not recount what is already queued.
func TestTheResyncFloorFindsWhatTheEventsMissed(t *testing.T) {
	q := New(Options{})
	r := NewResync(10 * time.Minute)
	// Startup: the caller reconciled its known units already.
	r.Pass(t0)

	now := t0.Add(5 * time.Minute)
	if added := r.Sweep(q, []string{"u"}, now); added != 0 {
		t.Fatalf("sweep before the floor added %d keys, want 0", added)
	}

	now = t0.Add(11 * time.Minute)
	added := r.Sweep(q, []string{"u"}, now)
	fmt.Printf("resync=%d\n", added)
	if added != 1 {
		t.Fatalf("resync added %d keys, want 1", added)
	}
	if key := q.Get(now); key != "u" {
		t.Fatalf("after the sweep, get = %q, want u", key)
	}
	q.Done("u")

	// The floor passed, and what was already queued is not recounted: the sweep is due but the queue
	// already holds the key.
	q.Add("u")
	if added := r.Sweep(q, []string{"u"}, now.Add(11*time.Minute)); added != 0 {
		t.Fatalf("sweep recounted a queued key: added %d", added)
	}
}
