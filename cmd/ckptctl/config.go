package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/bolubo/checkpoint-governor/internal/produce/adapters"
)

// runConfig prints every threshold in force and where it came from.
//
// A precedence chain nobody can see is a chain nobody trusts: "file < flag < default" is only true
// if a person can ask which one won, per key.
func runConfig(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	dir := fs.String("dir", "", "the checkpoint directory (reads .ckpt-governor/conditions.json)")
	freeFactor := fs.Float64("free-factor", 2, "free space below this multiple of one checkpoint is at risk")
	storeEvery := fs.Duration("store-every", 5_000_000_000, "how often the storage conditions are read")
	slowFactor := fs.Float64("slow-factor", 2, "how many times the job baseline counts as slow")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" {
		fmt.Fprint(out, "usage: ckptctl config --dir=<checkpointDir>\n")
		return 2
	}

	cfg, fileKeys, err := adapters.LoadConfig(*dir)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 2
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["free-factor"] {
		cfg.FreeSpaceFactor = *freeFactor
	}
	if set["store-every"] {
		cfg.StoreEvery = *storeEvery
	}
	if set["slow-factor"] {
		cfg.SlowFactor = *slowFactor
	}

	fromFile := map[string]bool{}
	for _, k := range fileKeys {
		fromFile[k] = true
	}
	flagsByKey := map[string]bool{
		"freeSpaceFactor": set["free-factor"],
		"storeEvery":      set["store-every"],
		"slowFactor":      set["slow-factor"],
	}

	defaults := adapters.Config{}.Effective()
	effective := cfg.Effective()
	keys := make([]string, 0, len(effective))
	for k := range effective {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Fprintf(out, "%-16s %-8s %-8s %s\n", "key", "value", "source", "default")
	changed := 0
	for _, k := range keys {
		source := "default"
		switch {
		case flagsByKey[k]:
			source = "flag"
		case fromFile[k]:
			source = "file"
		}
		if source != "default" {
			changed++
		}
		fmt.Fprintf(out, "%-16s %-8s %-8s %s\n", k, effective[k], source, defaults[k])
	}
	fmt.Fprintf(out, "\nconfig: %d keys, %d overridden; precedence flag > file > default\n", len(keys), changed)

	// Capabilities are not thresholds: they say which upstreams exist, and they decide whether a
	// condition can fire at all. Printing them beside the numbers keeps "why did it not write"
	// answerable from one screen.
	if dep, err := adapters.LoadDeployment(*dir); err == nil {
		parts := make([]string, 0, 2)
		for _, name := range []string{adapters.CapPreemption, adapters.CapEviction} {
			c := dep.Capability(name)
			switch {
			case !c.Declared:
				parts = append(parts, name+" assumed on (not declared)")
			case c.Exists:
				parts = append(parts, name+" on")
			default:
				parts = append(parts, name+" off ("+c.Why+")")
			}
		}
		fmt.Fprintf(out, "capabilities: %s\n", strings.Join(parts, " · "))
	}
	return 0
}
