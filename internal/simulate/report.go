package simulate

import (
	"fmt"
	"io"
	"time"
)

// PrintTable writes the evidence table: one row per trace, one cell per controller, then one line
// per trace with the best fixed period overall and per window.
//
// The cell is V/T/S because those three are the promise and its price; the refusal marker is there
// because a policy that refuses to write looks identical to one that never needs to.
func PrintTable(w io.Writer, rows []Row, ctls []comparator) {
	fmt.Fprintf(w, "%-18s", "trace")
	for _, c := range ctls {
		fmt.Fprintf(w, " %-16s", c.Name())
	}
	fmt.Fprintf(w, "  %-10s %-6s %s\n", "best-fixed", "G", "kill-age")

	for _, r := range rows {
		fmt.Fprintf(w, "%-18s", r.Trace)
		for _, res := range r.Results {
			fmt.Fprintf(w, " %-16s", cell(res))
		}
		fmt.Fprintf(w, "  %-10s %-6s %s\n",
			ShortDuration(r.BestFixed), FormatGain(r.Gain), killAge(r.Results))
	}

	fmt.Fprintf(w, "\ncell = V/T/S   V = share of time past the bound, T = tax, S = saves, * = refused to plan\n")
	fmt.Fprintf(w, "spread of best fixed periods across jobs: %.1fx\n", Spread(rows))
	fmt.Fprintf(w, "first seal is excluded from V and reported separately; see docs/experiments-matrix.md\n")

	for _, r := range rows {
		fmt.Fprintf(w, "  %-18s best fixed %-6s per window: ", r.Trace, ShortDuration(r.BestFixed))
		for _, p := range r.WindowBest {
			fmt.Fprintf(w, "%-6s", ShortDuration(p))
		}
		fmt.Fprintf(w, "  spread %.1fx\n", r.WithinSpread)
		if online := findResult(r.Results, "risk-v1"); online != nil {
			fmt.Fprintf(w, "  %-18s risk-v1 %d writes, %.1f%% tax | worst lost work at a kill %s\n",
				r.Trace, online.Saves, online.Tax*100, ShortDuration(online.MeanKillAge.Round(time.Second)))
		}
	}
}

func cell(r Result) string {
	s := fmt.Sprintf("%.1f%%/%.1f%%/%d", r.Violation*100, r.Tax*100, r.Saves)
	if r.Infeasible > 0 {
		s += "*"
	}
	return s
}

func killAge(results []Result) string {
	if r := findResult(results, "risk-v1"); r != nil {
		if r.MeanKillAge <= 0 {
			return "-"
		}
		return ShortDuration(r.MeanKillAge.Round(time.Second))
	}
	return "-"
}

func findResult(results []Result, name string) *Result {
	for i := range results {
		if results[i].Policy == name {
			return &results[i]
		}
	}
	return nil
}
