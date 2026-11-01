package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

// fileName is where an operator may override a threshold. It sits in the checkpoint directory so it
// travels with the job: a threshold that lives only on the machine that started the controller is a
// threshold nobody can audit later.
const fileName = "conditions.json"

// fileConfig is the file's shape. Every field is a pointer so that "absent" and "zero" are
// different: a file that sets freeSpaceFactor to 0 is an error, not a request for the default.
type fileConfig struct {
	FreeSpaceFactor *float64 `json:"freeSpaceFactor"`
	StoreEvery      *string  `json:"storeEvery"`
	MemoryLine      *float64 `json:"memoryLine"`
	CPUThrottleLine *float64 `json:"cpuThrottleLine"`
	SlowFactor      *float64 `json:"slowFactor"`
	ProbeTimeout    *string  `json:"probeTimeout"`
}

// LoadConfig reads the overrides, returning the configuration and the names of the keys the file
// set, so the caller can record one event per override.
//
// A missing file is not an error: the defaults are the catalogue's numbers. A malformed file is an
// error, and it must fail startup — silently falling back would leave an operator believing a
// threshold is in force when it is not.
func LoadConfig(dir string) (Config, []string, error) {
	var c Config
	raw, err := os.ReadFile(filepath.Join(dir, ".ckpt-governor", fileName))
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil, nil
		}
		// A directory that cannot be read is the failure `store.blocked` exists to report. Refusing
		// to start would make that condition unreachable in its own scenario — and would leave a job
		// with no controller at all because one file is missing. Start with the defaults and say so;
		// a malformed file still stops us, because that is an operator mistake, not damage.
		if errors.Is(err, syscall.ENOTDIR) || os.IsPermission(err) {
			c.Unreadable = err.Error()
			return c, nil, nil
		}
		return c, nil, err
	}

	var f fileConfig
	if err := json.Unmarshal(raw, &f); err != nil {
		return c, nil, fmt.Errorf("%s: %w", fileName, err)
	}

	var keys []string
	if f.FreeSpaceFactor != nil {
		if *f.FreeSpaceFactor <= 0 {
			return c, nil, fmt.Errorf("%s: freeSpaceFactor must be > 0, got %v", fileName, *f.FreeSpaceFactor)
		}
		c.FreeSpaceFactor = *f.FreeSpaceFactor
		keys = append(keys, "freeSpaceFactor")
	}
	if f.MemoryLine != nil {
		if *f.MemoryLine <= 0 || *f.MemoryLine > 1 {
			return c, nil, fmt.Errorf("%s: memoryLine must be in (0,1], got %v", fileName, *f.MemoryLine)
		}
		c.MemoryLine = *f.MemoryLine
		keys = append(keys, "memoryLine")
	}
	if f.CPUThrottleLine != nil {
		if *f.CPUThrottleLine < 0 || *f.CPUThrottleLine > 1 {
			return c, nil, fmt.Errorf("%s: cpuThrottleLine must be in [0,1], got %v", fileName, *f.CPUThrottleLine)
		}
		c.CPUThrottleLine = *f.CPUThrottleLine
		keys = append(keys, "cpuThrottleLine")
	}
	if f.SlowFactor != nil {
		if *f.SlowFactor <= 0 {
			return c, nil, fmt.Errorf("%s: slowFactor must be > 0, got %v", fileName, *f.SlowFactor)
		}
		c.SlowFactor = *f.SlowFactor
		keys = append(keys, "slowFactor")
	}
	if f.StoreEvery != nil {
		d, err := time.ParseDuration(*f.StoreEvery)
		if err != nil || d <= 0 {
			return c, nil, fmt.Errorf("%s: storeEvery must be a positive duration, got %q", fileName, *f.StoreEvery)
		}
		c.StoreEvery = d
		keys = append(keys, "storeEvery")
	}
	if f.ProbeTimeout != nil {
		d, err := time.ParseDuration(*f.ProbeTimeout)
		if err != nil || d <= 0 {
			return c, nil, fmt.Errorf("%s: probeTimeout must be a positive duration, got %q", fileName, *f.ProbeTimeout)
		}
		c.ProbeTimeout = d
		keys = append(keys, "probeTimeout")
	}

	sort.Strings(keys)
	return c, keys, nil
}

// FreeFactor is the space rule's multiple of one checkpoint, exported for the explanation: a reader
// asking "why" must be told the same number the rule used.
//
// Effective is what the probes will actually use: a file value where it exists, the default
// elsewhere. It exists so that a caller can print the numbers in force without re-deriving them.
func (c Config) FreeFactor() float64 { return c.freeFactor() }

func (c Config) Effective() map[string]string {
	return map[string]string{
		"freeSpaceFactor": fmt.Sprintf("%g", c.freeFactor()),
		"storeEvery":      c.storeEvery().String(),
		"memoryLine":      fmt.Sprintf("%g", c.memoryLine()),
		"cpuThrottleLine": fmt.Sprintf("%g", c.cpuThrottleLine()),
		"probeTimeout":    c.probeTimeout().String(),
		"slowFactor":      fmt.Sprintf("%g", c.slowFactor()),
	}
}
