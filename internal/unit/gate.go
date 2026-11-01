package unit

import (
	"hash/fnv"
	"time"
)

// Gate limits how many writes run at once on one volume.
//
// The algorithm cannot see the other units writing to the same disk, so each of its decisions can
// be right while the volume is overwhelmed. The gate is a resource limit in the executor, not a
// second decision: the policy still says when, and the gate says "not all at once".
type Gate struct {
	limit int
	used  map[string]int
}

// NewGate creates a gate. A limit below one means one: a volume with no limit is a volume that will
// be asked to do everything at the worst moment.
func NewGate(limit int) *Gate {
	if limit < 1 {
		limit = 1
	}
	return &Gate{limit: limit, used: map[string]int{}}
}

// TryAcquire reserves a write slot on the volume, reporting false when the slot has to wait.
func (g *Gate) TryAcquire(volume string) bool {
	if g.used[volume] >= g.limit {
		return false
	}
	g.used[volume]++
	return true
}

// Release returns a slot. Releasing a volume with no slot is a no-op rather than a negative count:
// a double release must not silently allow one extra writer.
func (g *Gate) Release(volume string) {
	if g.used[volume] > 0 {
		g.used[volume]--
	}
}

// InUse is how many writes the volume is running now.
func (g *Gate) InUse(volume string) int { return g.used[volume] }

// Offset is a deterministic delay in [0, window) for one unit.
//
// Several units share a trigger moment after a restart, a clock step or a fallback expiry. Spreading
// them by a hash of the unit id keeps the order stable across restarts without any shared state, and
// without adding jitter to the published numbers (decision 0003).
func Offset(id string, window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	h := fnv.New64a()
	h.Write([]byte(id))
	return time.Duration(h.Sum64() % uint64(window))
}
