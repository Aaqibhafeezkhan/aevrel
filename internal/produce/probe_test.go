package produce

import (
	"testing"
	"time"
)

// fakeProbe is the compile-time assertion that the interface is what the adapters will implement.
type fakeProbe struct{}

func (fakeProbe) Name() string         { return "store.low-space" }
func (fakeProbe) Read(Env) Reading     { return Reading{State: Absent} }
func (fakeProbe) Every() time.Duration { return time.Second }
func (fakeProbe) Cost() Cost           { return Cheap }

var _ Probe = fakeProbe{}

// The three answers must stay three. The bug this prevents is the one 0013 warns about: an absent
// condition rendered as a zero, which makes a CPU job look calm in the same way a blind GPU does.
func TestTheThreeAnswersAreDistinct(t *testing.T) {
	if Absent == Unknown || Unknown == Fresh || Absent == Fresh {
		t.Fatal("the three states must be distinct")
	}
	for want, s := range map[string]State{"absent": Absent, "unknown": Unknown, "fresh": Fresh} {
		if s.String() != want {
			t.Fatalf("State(%d).String() = %q, want %q", s, s.String(), want)
		}
	}
}
