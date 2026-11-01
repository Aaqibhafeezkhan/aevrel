package adapters

import (
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// notHere is a condition this deployment does not have.
//
// It is declared rather than omitted on purpose: the catalogue is a closed list (0013), and the
// difference between "this environment has no GPU" and "nobody wrote the adapter" must be visible.
// An absent condition is inert; an undeclared one is a hole nobody can see.
type notHere struct {
	name  string
	every time.Duration
}

func (p notHere) Name() string { return p.name }

func (p notHere) Every() time.Duration { return p.every }

func (p notHere) Cost() produce.Cost { return produce.Cheap }

func (p notHere) Read(env produce.Env) produce.Reading {
	return produce.Reading{State: produce.Absent, At: env.Now}
}

// absentOnThisBuild lists what a single-node deployment without a scheduler, without a GPU and
// without remote storage cannot have. Each one would become a trigger the moment an environment
// provides it, which is why it is declared here instead of being left out.
func absentOnThisBuild(every time.Duration) []produce.Probe {
	return []produce.Probe{
		notHere{name: "host.gpu-fault", every: every}, // no GPU
		notHere{name: "host.gpu-hot", every: every},   // no GPU
		notHere{name: "net.long-outage", every: every},
	}
}
