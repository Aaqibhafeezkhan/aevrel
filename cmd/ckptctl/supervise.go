package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce/adapters"
	"github.com/bolubo/checkpoint-governor/internal/supervisor"
	"github.com/bolubo/checkpoint-governor/internal/unit"
)

// runSupervise drives one supervisor over several checkpoint directories and rolls the result up.
//
// It exists because the unit of work is not the process: a node runs many jobs, they share volumes,
// and a person watching them wants one line, not N terminals. The rollup is deliberately thin — it
// reads what each loop published rather than re-deciding anything.
func runSupervise(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("supervise", flag.ContinueOnError)
	members := fs.String("members", "", "comma-separated checkpoint directories")
	rounds := fs.Int("rounds", 6, "how many ticks to run")
	every := fs.Duration("every", 200*time.Millisecond, "between ticks")
	freeFactor := fs.Float64("free-factor", 2, "free space below this multiple of one checkpoint is at risk")
	storeEvery := fs.Duration("store-every", 5*time.Second, "how often the storage conditions are read")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dirs := splitMembers(*members)
	if len(dirs) == 0 {
		fmt.Fprint(out, "usage: ckptctl supervise --members=<dir1,dir2,...>\n")
		return 2
	}

	cfg, _, err := adapters.LoadConfig(dirs[0])
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 2
	}
	cfg.FreeSpaceFactor, cfg.StoreEvery = *freeFactor, *storeEvery

	host, _ := os.Hostname()
	sup := supervisor.Open(supervisor.Config{
		Policy: policy.Risk{},
		Bounds: policy.Bounds{
			MinCopies: 1,
			NoUs:      policy.NoUsNone,
		},
		Probes: adapters.All(cfg),
		// One holder for the whole supervisor: the lease is per directory, and every unit has its own.
		Holder: fmt.Sprintf("%s-%d", host, os.Getpid()),
	})
	defer sup.Close()

	ms := make([]unit.Member, 0, len(dirs))
	for _, d := range dirs {
		ms = append(ms, unit.Member{ID: d, Name: filepath.Base(d), Dir: d})
	}
	if err := sup.Sync(ms); err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}

	for i := 0; i < *rounds; i++ {
		if _, err := sup.Tick(); err != nil {
			fmt.Fprintf(out, "error: tick: %v\n", err)
			return 1
		}
		time.Sleep(*every)
	}

	// The rollup: one row per unit, then one line that adds them up.
	save, skip, sealed, decided := 0, 0, 0, 0
	dirs = append([]string(nil), sup.Dirs()...)
	sort.Strings(dirs)
	for _, d := range dirs {
		action := "-"
		if dec, ok := advise.LastDecision(d); ok {
			decided++
			action = string(dec.Action)
			if dec.Action == policy.ActionSave {
				save++
			} else {
				skip++
			}
		}
		if n := sealedCopies(d); n > 0 {
			sealed++
		}
		fmt.Fprintf(out, "%-40s %-8s %s\n", d, action, sup.Queue().String())
	}
	fmt.Fprintf(out, "\nsupervise: units=%d sealed=%d decided=%d save=%d skip=%d\n",
		len(dirs), sealed, decided, save, skip)
	return 0
}

// splitMembers accepts "a,b,c" and ignores empty entries, which is what a shell leaves behind after
// a trailing comma.
func splitMembers(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// sealedCopies counts the directories a writer sealed. A unit with one is a unit whose work is on
// disk, which is the only thing the rollup claims about it.
func sealedCopies(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "step-") {
			n++
		}
	}
	return n
}
