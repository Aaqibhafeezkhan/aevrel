package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/simulate"
)

// runSimulate prints the comparison table: the policy against a few fixed timers, on one trace.
func runSimulate(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	lost := fs.Duration("lost", 10*time.Minute, "the lost-work bound")
	tax := fs.Float64("tax", 0.05, "the write-tax bound, in (0,1]")
	fs.Usage = func() {
		// Flags come first: Go's flag package stops at the first positional argument.
		fmt.Fprintln(os.Stderr, "usage: ckptctl simulate [--lost=10m] [--tax=0.05] <trace.jsonl>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()

	tr, err := simulate.Parse(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	bounds := policy.Bounds{MinCopies: 1, NoUs: policy.NoUsNone}
	eval := simulate.EvalBounds{MaxLostWork: *lost, MaxTax: *tax}
	p, ok := policy.Lookup("risk-v1")
	if !ok {
		fmt.Fprintln(os.Stderr, "the risk policy is missing from the registry")
		return 1
	}

	rows := make([]simulate.Result, 0, 4)
	first, err := simulate.Run(tr, simulate.Options{Bounds: bounds, Eval: eval, Policy: simulate.Boundary(p)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rows = append(rows, first)
	for _, period := range []time.Duration{10 * time.Minute, 30 * time.Minute, 60 * time.Minute} {
		r, err := simulate.Run(tr, simulate.Options{Bounds: bounds, FixedPeriod: period})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		rows = append(rows, r)
	}

	fmt.Fprintf(out, "trace: %d events over %s, write cost %s, bound %s\n\n",
		first.Events, first.Elapsed.Round(time.Second), first.WriteCost.Round(time.Second), lost.String())
	fmt.Fprintf(out, "%-14s %7s %7s %9s\n", "policy", "V", "T", "saves/h")
	for _, r := range rows {
		fmt.Fprintf(out, "%-14s %6.2f%% %6.2f%% %9.2f\n",
			r.Policy, r.Violation*100, r.Tax*100, r.SavesPerHour)
	}
	return 0
}
