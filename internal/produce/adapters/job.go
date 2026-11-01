package adapters

import (
	"os"
	"path/filepath"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce"
)

// presence is a condition whose signal is the existence of a file the job writes.
//
// The file's **presence** is the reading and its absence is 0, not "unknown": a job that has not
// written it is not blind: it has not reached that state. An unknown here would be urgent
// (0013), which would make a calm job look like one that cannot be watched.
type presence struct {
	condition string
	file      string
	every     time.Duration
}

func (p presence) Name() string { return p.condition }

func (p presence) Every() time.Duration { return p.every }

func (p presence) Cost() produce.Cost { return produce.Cheap }

func (p presence) Read(env produce.Env) produce.Reading {
	_, err := os.Stat(filepath.Join(env.Dir, ".ckpt-governor", p.file))
	switch {
	case err == nil:
		return produce.Reading{State: produce.Fresh, Value: true, At: env.Now}
	case os.IsNotExist(err):
		return produce.Reading{State: produce.Fresh, Value: false, At: env.Now}
	default:
		return produce.Reading{State: produce.Unknown, At: env.Now, Err: err}
	}
}

// ending is `job.ending`: training is finishing, so the last write is the useful one.
type ending struct{}

func (ending) with(c Config) presence {
	return presence{condition: "job.ending", file: "ending", every: 10 * time.Second}
}

// shutdown is `risk.shutdown`: the job has been told to stop and has a grace period to spend.
//
// It reads a different file from `ending` on purpose. "Training is over" and "you have thirty
// seconds before the process dies" lead to different decisions: the first says the work is already
// complete, the second says the work will be lost at a known moment.
type shutdown struct{}

func (shutdown) with(c Config) presence {
	return presence{condition: "risk.shutdown", file: "stopping", every: 5 * time.Second}
}
