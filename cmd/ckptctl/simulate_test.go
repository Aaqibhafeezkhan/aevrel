package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A short trace: a step every two seconds, one completed write of thirty seconds.
const traceText = `{"t":"2026-01-01T00:00:00Z","event":"free","bytes":1000000000}
{"t":"2026-01-01T00:00:00Z","event":"step","step":0}
{"t":"2026-01-01T00:00:02Z","event":"step","step":1}
{"t":"2026-01-01T00:00:04Z","event":"step","step":2}
{"t":"2026-01-01T00:00:06Z","event":"step","step":3}
{"t":"2026-01-01T00:00:08Z","event":"step","step":4}
{"t":"2026-01-01T00:00:10Z","event":"save_start","step":5}
{"t":"2026-01-01T00:00:40Z","event":"save_end","step":5,"bytes":1000,"sealed":true}
{"t":"2026-01-01T00:00:42Z","event":"step","step":6}
{"t":"2026-01-01T00:00:44Z","event":"step","step":7}
`

func TestSimulatePrintsTheComparisonTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	if err := os.WriteFile(path, []byte(traceText), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runSimulate([]string{path}, &out); code != 0 {
		t.Fatalf("simulate exited %d", code)
	}
	text := out.String()
	for _, want := range []string{"risk-v1", "fixed-10m0s", "fixed-30m0s", "fixed-1h0m0s", "saves/h"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the table is missing %q:\n%s", want, text)
		}
	}
}

func TestSimulateRefusesAMissingTrace(t *testing.T) {
	var out strings.Builder
	if code := runSimulate([]string{filepath.Join(t.TempDir(), "missing.jsonl")}, &out); code == 0 {
		t.Fatal("a missing trace must not succeed")
	}
}
