// Package assemble maps what was measured onto the contract.
//
// It is deliberately dumb and pure: no reads, no clock, no errors. The point is that the mapping
// from measurements to policy.Snapshot lives in one function, and it fills only the fields the risk
// core and the executor read (policy.SnapshotFields). Everything the interval line wanted is
// deprecated: filling it would say "measured" about a number nobody looks at.
package assemble

import (
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// State is what a copy is, before the contract names it.
type State int

const (
	Sealed State = iota
	Broken
	Unverified
	Verified
)

// Copy is one checkpoint as measured.
type Copy struct {
	Step      int64
	Bytes     int64
	State     State
	CreatedAt time.Time
}

// Inputs is everything the measurements produced, already in the units the contract wants. It holds
// exactly what Snapshot holds: an input with no output is a measurement the contract does not want.
type Inputs struct {
	UnitID     string
	Generation int64

	Copies          []Copy
	SaveInFlight    bool
	SaveStartedAt   time.Time
	SaveProgress    float64 // -1 unknown
	CheckpointBytes int64

	FreeBytes int64

	Failures int

	Step       int64
	StepSource string

	// Evidence is the conditions, exactly as the probes reported them.
	Evidence map[string]policy.Observation
}

// Build is the single place the contract is filled.
func Build(in Inputs) policy.Snapshot {
	return policy.Snapshot{
		UnitID:     in.UnitID,
		Generation: in.Generation,

		Step:            in.Step,
		SaveInFlight:    in.SaveInFlight,
		SaveStartedAt:   in.SaveStartedAt,
		SaveProgress:    in.SaveProgress,
		CheckpointBytes: in.CheckpointBytes,

		FreeBytes: in.FreeBytes,

		Copies:                   copies(in.Copies),
		ConsecutiveWriteFailures: in.Failures,

		Evidence: evidence(in),
	}
}

func copies(in []Copy) []policy.Copy {
	if len(in) == 0 {
		return nil
	}
	out := make([]policy.Copy, 0, len(in))
	for _, c := range in {
		state := policy.CopySealed
		switch c.State {
		case Broken:
			state = policy.CopyBroken
		case Unverified:
			state = policy.CopyUnverified
		case Verified:
			state = policy.CopyVerified
		}
		out = append(out, policy.Copy{Step: c.Step, Bytes: c.Bytes, State: state, CreatedAt: c.CreatedAt})
	}
	return out
}

func evidence(in Inputs) map[string]policy.Observation {
	out := make(map[string]policy.Observation, len(in.Evidence)+1)
	for k, v := range in.Evidence {
		out[k] = v
	}
	source := in.StepSource
	if source == "" {
		source = "checkpoint"
	}
	out["step.source"] = policy.Observation{Kind: policy.Text, Text: source, Source: "assemble"}
	return out
}
