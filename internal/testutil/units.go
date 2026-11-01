// Package testutil builds the worlds the tests inspect.
//
// Two implementers work on this repository at once, and a fixture is only shared if both sides agree
// on what it means. So the builders live here, and a test beside them asserts the reader's verdict
// on each one: sealed means sealed, broken means broken, unsealed means a write in flight.
package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// StepDir is the name of a checkpoint directory.
func StepDir(step int64) string { return "step-" + itoa(step) }

// Sealed writes a step directory whose manifest agrees with its shards: a complete copy.
func Sealed(t *testing.T, dir string, step int64, sizes ...int64) {
	t.Helper()
	d := filepath.Join(dir, StepDir(step))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	shards := make([]map[string]any, 0, len(sizes))
	for rank, size := range sizes {
		if err := os.WriteFile(filepath.Join(d, "rank-"+itoa(int64(rank))+".bin"), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		shards = append(shards, map[string]any{"rank": rank, "bytes": size})
	}
	writeManifest(t, d, step, shards)
}

// Broken writes a step directory whose manifest claims more bytes than the shards hold: the write
// said it finished and it did not.
func Broken(t *testing.T, dir string, step int64) {
	t.Helper()
	d := filepath.Join(dir, StepDir(step))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "rank-0.bin"), make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, d, step, []map[string]any{{"rank": 0, "bytes": 999}})
}

// Unsealed writes a step directory with a shard and no manifest: a write in flight.
func Unsealed(t *testing.T, dir string, step int64, size int64) {
	t.Helper()
	d := filepath.Join(dir, StepDir(step))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "rank-0.bin"), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeManifest(t *testing.T, d string, step int64, shards []map[string]any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"step": step, "shards": shards})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}
