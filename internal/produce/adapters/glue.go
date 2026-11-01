package adapters

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Glue is everything a job can tell the controller about itself, read in one place.
//
// It exists because round 2 needs these as *facts for the executor* (headroom before the first seal,
// the duration of one write, and the two-phase exit), not as conditions: none of these is a 0/1 that
// should trigger a write on its own. Keeping them here rather than in a probe is the same discipline
// as `Copies`: one fact, one representation, and the one that has a consumer decides the shape.
//
// This is the only reader of the glue files. The executor (`loop`) and the CLI both call it, so the
// two cannot drift into disagreeing about what a file means — which is exactly what would happen
// with a second parser, and it would happen silently.
//
// One fact, one file, is also why there is no `stop-requested` file: `.ckpt-governor/stopping` (the
// file the job writes on SIGTERM) **is** the stop request, and a second name for it would let the
// two disagree.
type Glue struct {
	// ExpectedBytes is what the job says its next checkpoint will weigh. The job knows its own
	// parameter count; we do not, and guessing is how a headroom check becomes a lie.
	ExpectedBytes int64
	HasExpected   bool

	// LastSaveSeconds is how long the job's own previous save took. It is the second-best estimate of
	// one write, after the measured p90 and before size over probed throughput.
	LastSaveSeconds float64
	HasLastSave     bool

	// StopRequested is `.ckpt-governor/stopping` being present.
	StopRequested bool

	// Ending is the file "ending": training is over, so whatever exists now is the work that matters.
	// It is also the honest moment to answer the stop handshake.
	Ending bool

	// MayExit is the controller's answer to the handshake: the job may leave.
	MayExit bool

	// SecondsLeft is how much of the user's grace period the job thinks remains. The job owns the
	// deadline because the job was given it; the controller must not invent a second one.
	SecondsLeft int64
	HasSeconds  bool
}

// ReadGlue reads what the job left behind. A missing file is zero values with its Has flag false:
// a job that never wrote these is a job that never had a stop, not one hiding something.
func ReadGlue(dir string) Glue {
	var g Glue
	base := filepath.Join(dir, ".ckpt-governor")
	if n, ok := readNumber(base, "expected-bytes"); ok {
		g.ExpectedBytes, g.HasExpected = n, true
	}
	if f, ok := readDecimal(base, "last-save-seconds"); ok {
		g.LastSaveSeconds, g.HasLastSave = f, true
	}
	if _, err := os.Stat(filepath.Join(base, "stopping")); err == nil {
		g.StopRequested = true
	}
	if _, err := os.Stat(filepath.Join(base, "ending")); err == nil {
		g.Ending = true
	}
	if _, err := os.Stat(filepath.Join(base, "may-exit")); err == nil {
		g.MayExit = true
	}
	if n, ok := readNumber(base, "seconds-left"); ok {
		g.SecondsLeft, g.HasSeconds = n, true
	}
	return g
}

func readNumber(base, name string) (int64, bool) {
	raw, err := os.ReadFile(filepath.Join(base, name))
	if err != nil {
		return 0, false
	}
	n, convErr := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if convErr != nil {
		return 0, false
	}
	return n, true
}

// readDecimal reads the files that carry fractions (seconds), so a job that reports 13.7 seconds is
// not silently rounded to 13 and then treated as exact.
func readDecimal(base, name string) (float64, bool) {
	raw, err := os.ReadFile(filepath.Join(base, name))
	if err != nil {
		return 0, false
	}
	f, convErr := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if convErr != nil {
		return 0, false
	}
	return f, true
}
