package adapters

import (
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// An absent condition must never look like a calm one: the catalogue says absence is inert, and the
// view has to be able to print "absent" instead of "0".
func TestAbsentConditionsSayTheyAreAbsent(t *testing.T) {
	for _, p := range absentOnThisBuild(time.Second) {
		r := p.Read(produce.Env{Now: t0})
		if r.State != produce.Absent {
			t.Fatalf("%s = %+v, want absent", p.Name(), r)
		}
	}
}
