package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// thresholdsFile records the numbers a running controller is actually using, so a plan from last
// week can still be explained with the thresholds that produced it.
//
// The operator's file cannot do this job. conditions.json holds what somebody asked for, while a
// flag on the command line can override it for one run and leave no trace there; a plan explained
// with the wrong factor is worse than a plan with no explanation, because it reads as authoritative.
const thresholdsFile = "thresholds.json"

// WriteThresholds records the effective configuration beside the checkpoint, atomically.
func WriteThresholds(dir string, c Config) error {
	raw, err := json.MarshalIndent(c.Effective(), "", "  ")
	if err != nil {
		return err
	}
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(base, "."+thresholdsFile+".tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(base, thresholdsFile))
}

// ReadThresholds returns the numbers the running controller recorded, if it recorded any.
func ReadThresholds(dir string) (map[string]string, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, ".ckpt-governor", thresholdsFile))
	if err != nil {
		return nil, false
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, len(out) > 0
}

// RecordedFreeFactor is the space factor the running controller used, when it recorded one. The
// boolean is false for a directory whose controller never wrote the record: older runs, or a
// controller that was started by hand.
func RecordedFreeFactor(dir string) (float64, bool) {
	th, ok := ReadThresholds(dir)
	if !ok {
		return 0, false
	}
	raw, ok := th["freeSpaceFactor"]
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return f, true
}
