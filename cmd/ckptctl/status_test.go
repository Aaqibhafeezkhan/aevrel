package main

import (
	"strings"
	"testing"

	"github.com/bolubo/checkpoint-governor/internal/advise"
)

func TestStatusReadsTheLedger(t *testing.T) {
	dir := t.TempDir()
	l, err := advise.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Status = advise.Status{UnitID: "train/llama", Generation: 2, Phase: advise.PhaseIdle}
	if err := l.Save(dir); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runStatus([]string{dir}, &out); code != 0 {
		t.Fatalf("status exited %d", code)
	}
	if !strings.Contains(out.String(), "phase idle") || !strings.Contains(out.String(), "generation 2") {
		t.Fatalf("the view is missing the state:\n%s", out.String())
	}
}

func TestStatusSaysWhenNothingHasBeenGoverned(t *testing.T) {
	var out strings.Builder
	if code := runStatus([]string{t.TempDir()}, &out); code != 0 {
		t.Fatalf("status exited %d", code)
	}
	if !strings.Contains(out.String(), "has not been governed") {
		t.Fatalf("a directory with no ledger must say so:\n%s", out.String())
	}
}
