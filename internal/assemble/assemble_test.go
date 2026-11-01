package assemble

import (
	"reflect"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// sentinel fills every input with something non-zero: if the contract has a field this function
// should fill and does not, the reflection test below finds it.
func sentinel() Inputs {
	return Inputs{
		UnitID: "train/llama", Generation: 1,
		Copies:       []Copy{{Step: 1, Bytes: 1, State: Sealed, CreatedAt: t0}},
		SaveInFlight: true, SaveStartedAt: t0, SaveProgress: -1, CheckpointBytes: 1,
		FreeBytes:  1,
		Failures:   1,
		Step:       1,
		StepSource: "job",
		Evidence:   map[string]policy.Observation{"store.low-space": {Kind: policy.Boolean, Bool: true}},
	}
}

// The status of every field comes from the algorithm's own table (policy.SnapshotFields): required
// and optional fields must be produced, deprecated fields must stay empty. A field the algorithm
// cannot live without and this side leaves zero is a decision made on a lie.
func TestBuildHonoursEveryFieldStatus(t *testing.T) {
	s := Build(sentinel())
	v := reflect.ValueOf(s)
	typ := v.Type()
	statuses := policy.SnapshotFields()

	for i := 0; i < v.NumField(); i++ {
		name := typ.Field(i).Name
		status, known := statuses[name]
		if !known {
			t.Fatalf("%s is not in policy.SnapshotFields: the algorithm must say what it needs", name)
		}
		zero := v.Field(i).IsZero()
		switch status {
		case policy.FieldRequired, policy.FieldOptional:
			if zero {
				t.Errorf("%s is %s but Build leaves it zero", name, status)
			}
		case policy.FieldDeprecated:
			if !zero {
				t.Errorf("%s is deprecated but Build fills it", name)
			}
		}
	}
}

func TestCopyStatesMapOntoTheContract(t *testing.T) {
	in := sentinel()
	in.Copies = []Copy{
		{Step: 1, State: Sealed},
		{Step: 2, State: Broken},
		{Step: 3, State: Unverified},
		{Step: 4, State: Verified},
	}
	s := Build(in)
	want := []policy.CopyState{policy.CopySealed, policy.CopyBroken, policy.CopyUnverified, policy.CopyVerified}
	for i, w := range want {
		if s.Copies[i].State != w {
			t.Fatalf("copy %d state = %v, want %v", i, s.Copies[i].State, w)
		}
	}
}

// A condition absent from Evidence is how the algorithm is told the environment has no such thing;
// what must never happen is a field silently carrying a plausible zero.
func TestEvidenceCarriesTheConditionsAndTheStepSource(t *testing.T) {
	in := sentinel()
	s := Build(in)
	if got := s.Evidence["step.source"].Text; got != "job" {
		t.Fatalf("step.source = %q, want job", got)
	}
	if o, ok := s.Evidence["store.low-space"]; !ok || !o.Bool {
		t.Fatal("the condition was dropped")
	}
	in.StepSource = ""
	if got := Build(in).Evidence["step.source"].Text; got != "checkpoint" {
		t.Fatalf("default step.source = %q, want checkpoint", got)
	}
}
