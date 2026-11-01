package simulate

import (
	"fmt"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// TestTheReplayOnlySpeaksTheCatalog closes the vocabulary from the outside: every decision the
// replay produces names a reason code that the catalogue or a condition declares, and every
// decision either carries such a code or says plainly that it is calm.
func TestTheReplayOnlySpeaksTheCatalog(t *testing.T) {
	known := map[string]bool{}
	for _, spec := range policy.ReasonCatalog() {
		known[spec.Code] = true
	}
	for _, c := range policy.Conditions() {
		known[c.Name] = true
	}

	seen := map[string]bool{}
	decisions, calm := 0, 0
	policies := []policy.Policy{policy.Risk{}}
	for _, fam := range Families() {
		for _, p := range policies {
			for _, r := range replayRecords(t, fam, p) {
				decisions++
				d := r.Decision
				if d.Reason == "" {
					t.Errorf("%s: decision %q has no reason", fam, d.Action)
				}
				if len(d.Reasons) == 0 {
					if d.Reason != policy.ReasonCalm {
						t.Errorf("%s: decision %q/%q carries no structured reason", fam, d.Action, d.Reason)
					}
					calm++
					continue
				}
				for _, reason := range d.Reasons {
					if !known[reason.Code] {
						t.Errorf("%s: %q is neither a catalogue code nor a condition", fam, reason.Code)
					}
					seen[reason.Code] = true
				}
			}
		}
	}

	var missing []string
	for _, c := range policy.Conditions() {
		if !seen[c.Name] {
			missing = append(missing, c.Name)
		}
	}
	fmt.Printf("replay: %d decisions, %d calm, %d codes seen; conditions not reached: %v\n",
		decisions, calm, len(seen), missing)
}
