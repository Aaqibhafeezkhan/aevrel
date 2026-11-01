package policy

import (
	"fmt"
	"reflect"
	"testing"
)

// TestEveryContractFieldHasAnOwner fails when a field exists in the contract and the policy table
// says nothing about what it needs, or when the table names a field the contract no longer has. It
// is the mechanical half of the handoff sheet; the delivery side holds the other half.
func TestEveryContractFieldHasAnOwner(t *testing.T) {
	check := func(name string, typ reflect.Type, table map[string]FieldStatus) int {
		t.Helper()
		count := 0
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			status, ok := table[field.Name]
			if !ok {
				t.Errorf("%s.%s has no entry: required, optional, or unknown-ok", name, field.Name)
				continue
			}
			switch status {
			case FieldRequired, FieldOptional, FieldUnknownOK, FieldDeprecated:
			default:
				t.Errorf("%s.%s has an unknown status %q", name, field.Name, status)
			}
			count++
		}
		for field := range table {
			if _, ok := typ.FieldByName(field); !ok {
				t.Errorf("the table names %s.%s, which the contract does not have", name, field)
			}
		}
		return count
	}

	snapshotCount := check("Snapshot", reflect.TypeOf(Snapshot{}), SnapshotFields())
	boundsCount := check("Bounds", reflect.TypeOf(Bounds{}), BoundsFields())
	fmt.Printf("fields: %d snapshot, %d bounds, all with an owner\n", snapshotCount, boundsCount)
}
