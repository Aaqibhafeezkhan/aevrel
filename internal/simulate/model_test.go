package simulate

import (
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// TestTheModelPricesAWriteFromItsParts: the cost model is a property of the storage, not of a
// policy, so it survives the pivot. Everything else that used to live here tested the interval
// policy and is gone with it.
func TestTheModelPricesAWriteFromItsParts(t *testing.T) {
	m := SmallTeamModel()
	// One second of overhead plus 300 MB at 10 MB/s.
	if got := m.FullCost(); got < 30*time.Second || got > 32*time.Second {
		t.Fatalf("full cost = %s, want about 31s", got)
	}
	// Four streams with no aggregate cap divide the bandwidth part, not the overhead.
	four := m
	four.Streams = 4
	if got := four.FullCost(); got < 8*time.Second || got > 10*time.Second {
		t.Fatalf("four-stream cost = %s, want about 8.6s", got)
	}
	// The aggregate cap decides when more streams stop helping.
	capped := four
	capped.Aggregate = 25 * 1000 * 1000
	if got := capped.FullCost(); got < 12*time.Second || got > 14*time.Second {
		t.Fatalf("capped four-stream cost = %s, want about 13s", got)
	}
	// A delta covering 300s at 50 KB/s is 15 MB, which is 1.5s of bandwidth plus the overhead.
	if got := m.Cost(policy.WriteDelta, 300*time.Second); got < 2*time.Second || got > 3*time.Second {
		t.Fatalf("delta cost over 300s = %s, want about 2.5s", got)
	}
}
