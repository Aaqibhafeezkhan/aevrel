package unit

import (
	"reflect"
	"testing"
	"time"
)

func member(id, dir string) Member {
	return Member{ID: id, Name: "train/llama", Dir: dir}
}

func TestUnitsAreDirectories(t *testing.T) {
	r := NewRegistry()
	units := r.Sync([]Member{
		{ID: "pod-a", Dir: "/ckpt/llama"},
		{ID: "pod-b", Dir: "/ckpt/llama"},
		{ID: "pod-c", Dir: "/ckpt/other"},
	})
	if len(units) != 2 {
		t.Fatalf("units = %d, want 2 (one per directory)", len(units))
	}
	if units[0].Dir != "/ckpt/llama" || len(units[0].Members) != 2 {
		t.Fatalf("first unit = %+v, want the two ranks of /ckpt/llama", units[0])
	}
	if !reflect.DeepEqual(units[0].Members, []string{"pod-a", "pod-b"}) {
		t.Fatalf("members = %v, want sorted", units[0].Members)
	}
}

func TestTheGenerationFollowsTheWritingSet(t *testing.T) {
	r := NewRegistry()
	dir := "/ckpt/llama"

	first := r.Sync([]Member{member("pod-a", dir), member("pod-b", dir)})
	if first[0].Generation != 1 {
		t.Fatalf("generation = %d, want 1", first[0].Generation)
	}

	// Re-listing the same set is not a restart: bumping here would invalidate a decision every
	// round and the job would rewrite the checkpoint for nothing.
	again := r.Sync([]Member{member("pod-a", dir), member("pod-b", dir)})
	if again[0].Generation != 1 {
		t.Fatalf("generation = %d after an identical sync, want 1", again[0].Generation)
	}

	// A rank is replaced: the writing set changed, so decisions from before are stale.
	changed := r.Sync([]Member{member("pod-a", dir), member("pod-c", dir)})
	if changed[0].Generation != 2 {
		t.Fatalf("generation = %d after a member change, want 2", changed[0].Generation)
	}
}

func TestANewBootIdIsANewGeneration(t *testing.T) {
	r := NewRegistry()
	dir := "/ckpt/llama"
	m := func(boot string) []Member {
		return []Member{{ID: "pod-a", Dir: dir, BootID: boot}}
	}
	r.Sync(m("boot-1"))
	second := r.Sync(m("boot-2"))
	if second[0].Generation != 2 {
		t.Fatalf("generation = %d after a new boot id, want 2", second[0].Generation)
	}
	// An unknown boot id says nothing; it must not be treated as a change.
	unknown := r.Sync([]Member{{ID: "pod-a", Dir: dir}})
	if unknown[0].Generation != 2 {
		t.Fatalf("generation = %d after the boot id became unknown, want 2", unknown[0].Generation)
	}
}

func TestAUnitThatDisappearsAndReturnsIsANewGeneration(t *testing.T) {
	r := NewRegistry()
	dir := "/ckpt/llama"
	r.Sync([]Member{member("pod-a", dir)})
	r.Sync(nil) // the unit vanished
	back := r.Sync([]Member{member("pod-a", dir)})
	if back[0].Generation != 2 {
		t.Fatalf("generation = %d after the unit returned, want 2 (the old decisions are stale)", back[0].Generation)
	}
}

func TestGateLimitsConcurrentWritesPerVolume(t *testing.T) {
	g := NewGate(1)
	if !g.TryAcquire("/ckpt") {
		t.Fatal("the first write must be allowed")
	}
	if g.TryAcquire("/ckpt") {
		t.Fatal("a second write on the same volume must wait")
	}
	if !g.TryAcquire("/other") {
		t.Fatal("another volume is independent")
	}
	g.Release("/ckpt")
	if !g.TryAcquire("/ckpt") {
		t.Fatal("after a release the volume takes a write again")
	}
	if g.InUse("/ckpt") != 1 {
		t.Fatalf("in use = %d, want 1", g.InUse("/ckpt"))
	}
}

func TestGateIgnoresADoubleRelease(t *testing.T) {
	g := NewGate(1)
	g.TryAcquire("/ckpt")
	g.Release("/ckpt")
	g.Release("/ckpt") // must not go negative
	if !g.TryAcquire("/ckpt") {
		t.Fatal("a double release must not lock the volume")
	}
	if g.TryAcquire("/ckpt") {
		t.Fatal("a double release must not allow two writers")
	}
}

func TestOffsetIsStableAndInsideTheWindow(t *testing.T) {
	window := 5 * time.Second
	a := Offset("train/llama", window)
	if a < 0 || a >= window {
		t.Fatalf("offset = %s, want inside [0, %s)", a, window)
	}
	if again := Offset("train/llama", window); again != a {
		t.Fatalf("offset = %s then %s: it must not change between restarts", a, again)
	}
	if Offset("train/other", window) == a && window > time.Nanosecond {
		// Not a correctness requirement, but a collision between two units is worth knowing about.
		t.Logf("two unit ids share the same offset in a %s window", window)
	}
}
