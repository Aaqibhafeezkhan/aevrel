package observe

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// TestTheDeliverySheetHonoursTheRequiredFields is the seam between the two tables: the policy says
// which fields it cannot answer without (internal/policy/fields.go), the delivery layer says where
// each field comes from and who consumes it (delivery.go). A field the policy requires cannot be
// declared unknown — that would be a contract this side has decided not to honour, and the decision
// would silently be a fraction of what it claims.
func TestTheDeliverySheetHonoursTheRequiredFields(t *testing.T) {
	sheet := Delivery()
	checked := 0
	for name, status := range policy.SnapshotFields() {
		if status != policy.FieldRequired {
			continue
		}
		checked++
		d, ok := sheet[name]
		if !ok {
			t.Errorf("%s is required by the policy and has no source in the delivery sheet", name)
			continue
		}
		if strings.Contains(d.Where, "unknown") || d.Where == "" {
			t.Errorf("%s is required by the policy but the delivery layer declares it %q", name, d.Where)
		}
		if !strings.Contains(d.Consumer, "policy") {
			t.Errorf("%s is required by the policy but nobody reads it: %q", name, d.Consumer)
		}
	}
	fmt.Printf("delivery: %d required fields, all produced and read\n", checked)
}

// A deprecated field must be produced by nobody and consumed by nobody. Filling one would say
// "measured" about a number the algorithm stopped looking at (0014).
func TestDeprecatedFieldsHaveNoProducerAndNoConsumer(t *testing.T) {
	sheet := Delivery()
	for name, status := range policy.SnapshotFields() {
		if status != policy.FieldDeprecated {
			continue
		}
		d := sheet[name]
		if d.Consumer != "nobody" {
			t.Errorf("%s is deprecated but still has a consumer: %q", name, d.Consumer)
		}
	}
}

// Every consumer must be one of a small closed set, so that "who acts on this" never becomes a
// sentence nobody can check.
func TestEveryConsumerIsAWordWeKnow(t *testing.T) {
	known := map[string]bool{"policy": true, "executor": true, "job": true, "human": true, "nobody": true}
	for name, d := range Delivery() {
		for _, c := range strings.Split(d.Consumer, "+") {
			if !known[c] {
				t.Errorf("%s has an unknown consumer %q", name, c)
			}
		}
	}
}
