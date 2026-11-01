package simulate

import (
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// view is what a simulated controller may look at: the product's snapshot, plus the one number this
// offline model carries and the contract no longer does — when the newest seal happened.
//
// The interval line retired with decision 0014, and with it those fields left `policy.Snapshot`: a
// contract nobody reads is a promise nobody keeps. It stays here because the scoreboard's whole job
// is to model *other people's* controllers (a timer, a fixed step count), and those controllers
// genuinely see such numbers. The product's own policy does not: `Boundary` hands it the snapshot
// alone, so a definition of our behaviour cannot quietly start reading a retired word.
type view struct {
	policy.Snapshot

	// Now is the clock. A comparator needs it; the shipped policy does not (the executor owns time).
	Now time.Time
	// LastSeal is when the newest complete checkpoint landed; zero when there is none.
	LastSeal time.Time
}

// plan is one answer from a simulated controller: what the product would do (`policy.Decision`), plus
// the one thing the simulator models and the contract deliberately does not carry — *when to ask
// again*.
//
// The product's decision has no clock in it (the executor owns time). A comparison of *other
// people's* controllers needs both, so it lives here: a vocabulary for modelling, not a field the
// product promises.
type plan struct {
	policy.Decision
	NextCheck time.Time
}

// comparator is a controller the scoreboard can run: either a shipped policy (through Boundary) or a
// model of what a team writes by hand.
type comparator interface {
	Name() string
	Decide(policy.Bounds, view) plan
}

// Boundary adapts a shipped policy to the comparator shape. It passes the product's snapshot and
// nothing else, which is the point: a comparison must measure the product as it is, not a version of
// it that can see more than the contract carries.
func Boundary(p policy.Policy) comparator { return boundary{inner: p} }

type boundary struct{ inner policy.Policy }

func (b boundary) Name() string { return b.inner.Name() }

func (b boundary) Decide(bounds policy.Bounds, v view) plan {
	return plan{Decision: b.inner.Decide(bounds, v.Snapshot)}
}
