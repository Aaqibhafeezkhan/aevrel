package adapters

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// evictionLine is where the kernel starts killing. The catalogue says 1 means "at the eviction
// line"; 90% is a warning, and a warning is a number a human reads, not a condition that fires.
const evictionLine = 0.95

// memoryPressure is `host.memory-pressure`: the cgroup is at the line where the OOM killer runs.
//
// It is absent, not unknown, when the machine has no cgroup v2 memory controller: a bare-metal job
// without a limit is an environment that does not have this condition, and treating it as unknown
// would make every such job look blind.
type memoryPressure struct {
	every time.Duration
	line  float64
	read  func() (used, limit int64, ok bool, err error)
}

func (p memoryPressure) with(c Config) memoryPressure {
	return memoryPressure{every: 10 * time.Second, line: c.memoryLine(), read: cgroupMemory}
}

func (p memoryPressure) Name() string { return "host.memory-pressure" }

func (p memoryPressure) Every() time.Duration { return p.every }

func (p memoryPressure) Cost() produce.Cost { return produce.Cheap }

func (p memoryPressure) Read(env produce.Env) produce.Reading {
	used, limit, ok, err := p.read()
	if err != nil {
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: err}
	}
	if !ok || limit <= 0 {
		return produce.Reading{State: produce.Absent, At: env.Now}
	}
	line := p.line
	if line <= 0 {
		line = evictionLine
	}
	return produce.Reading{State: produce.Fresh, Value: float64(used)/float64(limit) >= line, At: env.Now}
}

// cgroupMemory reads the container's memory. ok=false means "this machine has no such limit", which
// is the absent case; an error means the files exist but could not be read, which is blindness.
func cgroupMemory() (used, limit int64, ok bool, err error) {
	cur, err := readInt("/sys/fs/cgroup/memory.current")
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	raw, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	text := strings.TrimSpace(string(raw))
	if text == "max" {
		return 0, 0, false, nil // unlimited: no eviction line to be near
	}
	lim, convErr := strconv.ParseInt(text, 10, 64)
	if convErr != nil {
		return 0, 0, false, convErr
	}
	return cur, lim, true, nil
}

func readInt(path string) (int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
}
