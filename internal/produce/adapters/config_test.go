package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, fileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// No file is the normal case: the defaults are the catalogue's numbers, and nothing is recorded as
// an override because nothing was overridden.
func TestNoFileMeansTheCatalogueNumbers(t *testing.T) {
	c, keys, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("keys = %v, want none", keys)
	}
	if c.freeFactor() != 2 || c.memoryLine() != evictionLine || c.cpuThrottleLine() != throttledShare {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestAFileOverridesAndNamesItsKeys(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"freeSpaceFactor": 3, "memoryLine": 0.9, "storeEvery": "2s"}`)
	c, keys, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.freeFactor() != 3 || c.memoryLine() != 0.9 || c.storeEvery() != 2*time.Second {
		t.Fatalf("config = %+v", c)
	}
	if strings.Join(keys, ",") != "freeSpaceFactor,memoryLine,storeEvery" {
		t.Fatalf("keys = %v, want every overridden key named", keys)
	}
}

// A number the code cannot mean must stop the controller, not be rounded into something plausible.
func TestAnImpossibleThresholdIsAnError(t *testing.T) {
	cases := map[string]string{
		"memoryLine over 1": `{"memoryLine": 1.5}`,
		"memoryLine zero":   `{"memoryLine": 0}`,
		"factor zero":       `{"freeSpaceFactor": 0}`,
		"bad duration":      `{"storeEvery": "soon"}`,
		"negative duration": `{"probeTimeout": "-1s"}`,
	}
	for name, body := range cases {
		dir := t.TempDir()
		writeConfig(t, dir, body)
		if _, _, err := LoadConfig(dir); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

// Every key must land in its own field. Without this, a wrong struct tag is silent: the file parses,
// the value goes somewhere else or nowhere, and the threshold the operator set never takes effect.
func TestEveryKeyLandsInItsOwnField(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"freeSpaceFactor": 3, "storeEvery": "2s", "memoryLine": 0.9,
		"cpuThrottleLine": 0.3, "slowFactor": 4, "probeTimeout": "3s"}`)
	c, keys, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.FreeSpaceFactor != 3 {
		t.Fatalf("freeSpaceFactor = %v", c.FreeSpaceFactor)
	}
	if c.StoreEvery != 2*time.Second {
		t.Fatalf("storeEvery = %v", c.StoreEvery)
	}
	if c.MemoryLine != 0.9 {
		t.Fatalf("memoryLine = %v", c.MemoryLine)
	}
	if c.CPUThrottleLine != 0.3 {
		t.Fatalf("cpuThrottleLine = %v", c.CPUThrottleLine)
	}
	if c.SlowFactor != 4 {
		t.Fatalf("slowFactor = %v", c.SlowFactor)
	}
	if c.ProbeTimeout != 3*time.Second {
		t.Fatalf("probeTimeout = %v", c.ProbeTimeout)
	}
	if len(keys) != 6 {
		t.Fatalf("keys = %v, want all six named", keys)
	}
}

func TestMalformedJSONIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"freeSpaceFactor": }`)
	if _, _, err := LoadConfig(dir); err == nil {
		t.Fatal("malformed JSON must be an error")
	}
}

// What is in force has to be printable: an operator must be able to see the numbers without reading
// the code that derived them.
func TestEffectivePrintsTheNumbersInForce(t *testing.T) {
	e := Config{FreeSpaceFactor: 4}.Effective()
	if e["freeSpaceFactor"] != "4" || e["memoryLine"] != "0.95" || e["probeTimeout"] != "1s" {
		t.Fatalf("effective = %v", e)
	}
}
