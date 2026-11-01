package loop

import (
	"fmt"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// TestRetainedCopiesFollowsRetention pins the number the space rule is computed from: the copies that
// will still be on disk when the new one arrives, capped by what the decision asks to keep.
func TestRetainedCopiesFollowsRetention(t *testing.T) {
	copies := []policy.Copy{
		{Step: 1, State: policy.CopySealed},
		{Step: 2, State: policy.CopyVerified},
		{Step: 3, State: policy.CopyUnverified}, // a write in flight is not a copy that stays
	}
	kept := retainedCopies(copies, 1)
	all := retainedCopies(copies, 0)
	cold := retainedCopies(nil, 2)
	fmt.Printf("retained: keep 1 → %d, keep all → %d, cold start → %d\n", kept, all, cold)
	if kept != 1 {
		t.Fatalf("keep 1 → %d, want 1", kept)
	}
	if all != 2 {
		t.Fatalf("keep all → %d, want the two complete copies", all)
	}
	if cold != 0 {
		t.Fatalf("cold start → %d, want 0: the first write has nothing to coexist with", cold)
	}
}
