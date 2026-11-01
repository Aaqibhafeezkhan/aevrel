package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/bolubo/checkpoint-governor/internal/produce/adapters"
)

// runGlue prints what the job told us about its size and its stop.
//
// It is one line because it is meant to be pasted into a bug report: "the job said its checkpoint is
// 1.2 GiB, it had been asked to stop, and it had not yet said it was safe to kill".
func runGlue(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("glue", flag.ContinueOnError)
	dir := fs.String("dir", "", "the checkpoint directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" {
		fmt.Fprint(out, "usage: ckptctl glue --dir=<checkpointDir>\n")
		return 2
	}

	g := adapters.ReadGlue(*dir)
	expected := "unknown"
	if g.HasExpected {
		expected = humanBytes(g.ExpectedBytes)
	}
	line := fmt.Sprintf("glue: expected=%s stopRequested=%d mayExit=%d",
		expected, b2i(g.StopRequested), b2i(g.MayExit))
	if g.HasSeconds {
		line += fmt.Sprintf(" seconds-left=%d", g.SecondsLeft)
	}
	fmt.Fprintln(out, line)

	if g.StopRequested && !g.MayExit {
		fmt.Fprintln(out, "note: stopped but not safe to exit — the final write has not finished")
	}
	return 0
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// humanBytes is the shortest string that does not hide the magnitude: 1.2GiB is a size, 1258291200
// is a number someone has to divide.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGiB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKiB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
