package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
)

// runEvents prints the controller's own record: what it decided, what it asked for, what happened,
// and which thresholds somebody changed. It is the answer to "why did my job just checkpoint" when
// nobody was watching, so it must be readable without jq.
func runEvents(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	dir := fs.String("dir", "", "the checkpoint directory")
	limit := fs.Int("limit", 20, "how many events to print (0 = all)")
	since := fs.Duration("since", 0, "only events newer than this (e.g. 10m)")
	kind := fs.String("kind", "", "only this kind (decision, config, attempt_end, ...)")
	asJSON := fs.Bool("json", false, "print one JSON object per line")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" {
		fmt.Fprint(out, "usage: ckptctl events --dir=<checkpointDir> [--limit=20] [--since=10m] [--kind=decision]\n")
		return 2
	}

	all, err := advise.ReadEvents(*dir, 0)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}

	cut := time.Time{}
	if *since > 0 {
		cut = time.Now().Add(-*since)
	}
	counts := map[string]int{}
	var shown []advise.Event
	for _, e := range all {
		if *kind != "" && string(e.Kind) != *kind {
			continue
		}
		if !cut.IsZero() && e.At.Before(cut) {
			continue
		}
		counts[string(e.Kind)]++
		shown = append(shown, e)
	}
	if *limit > 0 && len(shown) > *limit {
		shown = shown[len(shown)-*limit:]
	}

	for _, e := range shown {
		if *asJSON {
			raw, _ := json.Marshal(e)
			fmt.Fprintln(out, string(raw))
			continue
		}
		fmt.Fprintf(out, "%s  %-14s %-20s %s\n", e.At.Format("15:04:05"), e.Kind, e.Code, values(e.Values))
	}

	fmt.Fprintf(out, "\nevents: %d shown, %d total%s\n", len(shown), len(all), summary(counts))
	return 0
}

// values prints the event's numbers and short strings in a stable order: a log line whose field
// order changes between runs cannot be diffed.
func values(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func summary(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
	}
	return " · " + strings.Join(parts, ", ")
}
