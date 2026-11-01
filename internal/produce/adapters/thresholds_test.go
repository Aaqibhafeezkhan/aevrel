package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

// The record must survive the process that wrote it: a plan from last week has to be explainable
// with the numbers that were in force then, including the ones a flag set.
func TestThresholdsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WriteThresholds(dir, Config{FreeSpaceFactor: 3, SlowFactor: 4}); err != nil {
		t.Fatal(err)
	}

	th, ok := ReadThresholds(dir)
	if !ok {
		t.Fatal("the record was not read back")
	}
	if th["freeSpaceFactor"] != "3" || th["slowFactor"] != "4" {
		t.Fatalf("thresholds = %v", th)
	}

	factor, ok := RecordedFreeFactor(dir)
	if !ok || factor != 3 {
		t.Fatalf("free factor = %v/%v", factor, ok)
	}
}

// A directory whose controller never recorded anything must not look like it recorded a default.
func TestNoRecordIsNotAFactor(t *testing.T) {
	if _, ok := ReadThresholds(t.TempDir()); ok {
		t.Fatal("an empty directory reported a record")
	}
	if _, ok := RecordedFreeFactor(t.TempDir()); ok {
		t.Fatal("an empty directory reported a factor")
	}
}

func TestACorruptRecordIsIgnored(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, thresholdsFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadThresholds(dir); ok {
		t.Fatal("a corrupt record was accepted")
	}
}

// LowSpaceNumbers is what the explanation quotes: it must refuse when there is nothing to compare,
// and it must use the same factor the rule used.
func TestLowSpaceNumbersNeedsAScale(t *testing.T) {
	dir := t.TempDir()
	if _, _, ok := LowSpaceNumbers(dir, 0, 2); ok {
		t.Fatal("no checkpoint must mean no numbers")
	}
	free, need, ok := LowSpaceNumbers(dir, 1<<20, 2)
	if !ok {
		t.Fatal("a checkpoint and a factor are enough to compute")
	}
	if need != 2<<20 || free <= 0 {
		t.Fatalf("free=%d need=%d", free, need)
	}
}
