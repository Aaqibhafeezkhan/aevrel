GO ?= go

# How long the real-clock smoke runs (make smoke MINUTES=2 for a quick check).
MINUTES ?= 10

.PHONY: all check fmt vet build test verify clean

all: check

check: fmt vet build test

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

build:
	$(GO) build ./...

# The observer is its own module: it carries client-go, and the core must never grow that dependency.
# It is built and tested on demand rather than on every `make check`, so a laptop without the module
# cache still passes the gate.
observer:
	cd contrib/observer && $(GO) build ./... && $(GO) test ./...

observer-against: 
	cd contrib/observer && $(GO) run . --world-dir=$(WORLD) --cluster=$(CLUSTER) --once

# The demo: three acts, fifteen seconds, ending with the receipt. See docs/product-v1.md §5.
demo:
	sh experiments/demo/demo.sh

# The three situations a user meets, asserted in one line each: the executable version of the README.
scenarios:
	sh experiments/demo/scenarios.sh

# The shortest path for someone who already has Go: the two binaries on their PATH.
install:
	$(GO) install ./cmd/ckptctl ./cmd/ckpttrainer

# The experiment rig: paired arms, one fault, one claim. It fails when
# the baseline also survives, because a fault that does not break the baseline proves nothing.
exp: exp-d exp-x

# D: does the system DECIDE to write when the fault arrives? Milliseconds, no write at all.
exp-d:
	sh experiments/rig/decide.sh all

# X: is the write it performs safe? Seconds, real files, state-triggered faults.
exp-x:
	sh experiments/rig/run.sh l1 kill-during-write
	sh experiments/rig/run.sh l1 user-stop
	sh experiments/rig/run.sh l1 disk-full
	sh experiments/rig/run.sh l1 disk-full

# The real-clock smoke: MINUTES=10 by default, a 60s window, the real binaries on the wall
# clock. It is not part of `verify`: `verify` must stay fast, and a bound that only a machine with
# real time can break is checked exactly (and in 5s) by the virtual day in internal/loop.
smoke:
	MINUTES=$(MINUTES) sh experiments/rig/smoke.sh

test:
	$(GO) test ./...

# verify is the gate: `check` plus the pins that keep the contract and the delivery sheet on one
# field set, and the golden decisions that catch an unintended behaviour change.
verify: check
	$(GO) test ./internal/loop ./cmd/ckptctl
	$(GO) test ./internal/simulate -run TestGoldenDecisions
	$(GO) test ./internal/policy -run TestEveryContractFieldHasAnOwner
	$(GO) test ./internal/observe -run TestTheDeliverySheetHonoursTheRequiredFields

# /bin/rm by absolute path so a shadowing `rm` on PATH cannot change what clean deletes.
clean:
	/bin/rm -rf bin
