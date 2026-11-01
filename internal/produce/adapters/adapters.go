// Package adapters is one file per condition.
//
// Each adapter owns its raw signal, its thresholds and its "unreadable" answer; the algorithm owns
// none of them (decision 0013). Nothing here knows a policy, a decision or a clock.
package adapters

import (
	"path/filepath"
	"syscall"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// Config is what an operator may tune. The zero value is the catalogue's answer, because a default
// that has to be configured is a default that will be wrong on the first deployment.
type Config struct {
	// FreeSpaceFactor is the multiple of one checkpoint below which the storage is at risk. Zero
	// means 2: the old copy and the new one have to coexist until the new one is sealed (0013).
	FreeSpaceFactor float64
	// StoreEvery is how often the storage conditions are read. Zero means five seconds.
	StoreEvery time.Duration
	// MemoryLine is the share of the limit at which memory is at the eviction line. Zero means
	// 0.95, which is where the kernel starts killing.
	MemoryLine float64
	// CPUThrottleLine is the share of CPU time lost to the quota above which the job is throttled.
	// Zero means 0.2.
	CPUThrottleLine float64
	// SlowFactor is how many times the job's own baseline counts as slow. Zero means 2: half speed
	// is where a person asks why. It is deployment configuration, never a runtime knob.
	SlowFactor float64
	// EvictionLine is the share of a node's allocatable resources above which the inference says the
	// node is filling up. Zero means 0.90.
	EvictionLine float64
	// Deployment is what the operator declared: where the world lives, and which upstreams exist at
	// all. Its zero value assumes every upstream is present, which is the safe default (a missed
	// preemption costs the job; a false one costs a write).
	Deployment Deployment
	// InferEviction switches the inference off when set to false. The default (nil) is on: with no
	// observer the condition is absent anyway, and a deployment that has one usually wants it.
	InferEviction *bool
	// Unreadable is set when conditions.json could not be read at all (a directory that is not a
	// directory, a permission wall). The controller runs with the defaults and reports it: refusing
	// to start would leave the job with no controller, and would make `store.blocked` unreachable in
	// precisely the failure it exists to describe.
	Unreadable string

	// WorldDir, Cluster and UnitID are this controller's coordinates in the world. WithWorld fills
	// them from a checkpoint directory; a caller that leaves them empty gets no world, which is the
	// single-machine default.
	WorldDir string
	Cluster  string
	UnitID   string

	// ProbeTimeout is the deadline for one probe read. Zero means one second: a probe that cannot
	// answer in a second is stuck, and the round must not wait for it.
	ProbeTimeout time.Duration
}

func (c Config) slowFactor() float64 {
	if c.SlowFactor <= 0 {
		return slowFactor
	}
	return c.SlowFactor
}

func (c Config) evictionLikelyLine() float64 {
	if c.EvictionLine <= 0 {
		return evictionLikelyLine
	}
	return c.EvictionLine
}

// EvictionLineOrDefault is the line the inference uses, for the display: a reader asking why must be
// told the same number the rule used.
func (c Config) EvictionLineOrDefault() float64 { return c.evictionLikelyLine() }

func (c Config) inferEviction() bool {
	return c.InferEviction == nil || *c.InferEviction
}

// Capability is the operator's declaration, or the assumed-on default.
func (c Config) Capability(name string) Capability { return c.Deployment.Capability(name) }

// WorldLoc is where this controller sits in the world: which directory to read, which cluster, which
// unit, and what the operator declared.
func (c Config) worldLoc() WorldLoc {
	return WorldLoc{Dir: c.WorldDir, Cluster: c.Cluster, UnitID: c.UnitID, Declared: c.Deployment}
}

// WithWorld resolves the world coordinates from a checkpoint directory: the flag, then the
// declaration, then a world/ beside the checkpoint; cluster defaults to the only one a fake or a
// single cluster deployment has; the unit id defaults to the directory's name, which is the identity
// the job already uses everywhere else.
func (c Config) WithWorld(ckptDir string) Config {
	if c.WorldDir == "" {
		if c.Deployment.WorldDir != "" {
			c.WorldDir = c.Deployment.WorldDir
		} else {
			c.WorldDir = filepath.Join(ckptDir, "world")
		}
	}
	if c.Cluster == "" {
		c.Cluster = "a"
	}
	if c.UnitID == "" {
		c.UnitID = filepath.Base(filepath.Clean(ckptDir))
	}
	return c
}

// evictionAuthorized is the conjunction the inference needs: somebody may preempt here, and nobody
// switched the guess off. Note what is *not* in it — whether a source is readable. That is the
// probe's business, and its absence is "absent", not "unknown".
func (c Config) evictionAuthorized() bool {
	return c.inferEviction() && c.Capability(CapEviction).Exists
}

func (c Config) probeTimeout() time.Duration {
	if c.ProbeTimeout <= 0 {
		return defaultProbeTimeout
	}
	return c.ProbeTimeout
}

func (c Config) memoryLine() float64 {
	if c.MemoryLine <= 0 {
		return evictionLine
	}
	return c.MemoryLine
}

func (c Config) cpuThrottleLine() float64 {
	if c.CPUThrottleLine <= 0 {
		return throttledShare
	}
	return c.CPUThrottleLine
}

func (c Config) freeFactor() float64 {
	if c.FreeSpaceFactor <= 0 {
		return 2
	}
	return c.FreeSpaceFactor
}

func (c Config) storeEvery() time.Duration {
	if c.StoreEvery <= 0 {
		return 5 * time.Second
	}
	return c.StoreEvery
}

// All returns the conditions this build can watch.
func All(c Config) []produce.Probe {
	out := []produce.Probe{
		lowSpace{}.with(c),
		blocked{}.with(c),
		ending{}.with(c),
		shutdown{}.with(c),
		memoryPressure{}.with(c),
		cpuThrottled{}.with(c),
		stepSlow{}.with(c),
		storeSlow{}.with(c),
		evictionLikely{}.with(c),
		preemption{}.with(c),
	}
	out = append(out, absentOnThisBuild(c.storeEvery())...)
	return guardAll(out, c.probeTimeout())
}

// statfs is the free and total bytes of the filesystem holding dir.
func statfs(dir string) (free, total int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0, err
	}
	bsize := int64(st.Bsize)
	return int64(st.Bavail) * bsize, int64(st.Blocks) * bsize, nil
}
