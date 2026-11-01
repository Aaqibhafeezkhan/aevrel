package adapters

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// throttledShare is how much of the job's CPU time has to be lost to the quota before it counts as
// throttled. A few microseconds of throttling at startup is noise; a fifth of the CPU time is the
// quota actually binding.
const throttledShare = 0.2

// cpuThrottled is `host.cpu-throttled`: the CPU quota is throttling the job.
//
// It reads a **ratio of cumulative counters** (throttled time over used time), so it needs no
// history of its own: every reading is computed from the same two files, and a job that has never
// been throttled says 0 forever.
type cpuThrottled struct {
	every time.Duration
	line  float64
	read  func() (throttled, used int64, ok bool, err error)
}

func (p cpuThrottled) with(c Config) cpuThrottled {
	return cpuThrottled{every: 10 * time.Second, line: c.cpuThrottleLine(), read: cgroupCPU}
}

func (p cpuThrottled) Name() string { return "host.cpu-throttled" }

func (p cpuThrottled) Every() time.Duration { return p.every }

func (p cpuThrottled) Cost() produce.Cost { return produce.Cheap }

func (p cpuThrottled) Read(env produce.Env) produce.Reading {
	throttled, used, ok, err := p.read()
	if err != nil {
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: err}
	}
	if !ok {
		// No cpu controller: a bare-metal job with no quota cannot be throttled by one.
		return produce.Reading{State: produce.Absent, At: env.Now}
	}
	if used <= 0 {
		return produce.Reading{State: produce.Fresh, Value: false, At: env.Now}
	}
	line := p.line
	if line <= 0 {
		line = throttledShare
	}
	return produce.Reading{State: produce.Fresh, Value: float64(throttled)/float64(used) >= line, At: env.Now}
}

// cgroupCPU reads throttled_usec and usage_usec from the cgroup's cpu.stat.
func cgroupCPU() (throttled, used int64, ok bool, err error) {
	raw, err := os.ReadFile("/sys/fs/cgroup/cpu.stat")
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	var haveThrottled, haveUsed bool
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		v, convErr := strconv.ParseInt(fields[1], 10, 64)
		if convErr != nil {
			continue
		}
		switch fields[0] {
		case "throttled_usec":
			throttled, haveThrottled = v, true
		case "usage_usec":
			used, haveUsed = v, true
		}
	}
	if !haveThrottled || !haveUsed {
		return 0, 0, false, nil
	}
	return throttled, used, true, nil
}
