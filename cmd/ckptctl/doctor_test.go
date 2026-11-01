package main

import (
	"strings"
	"testing"
)

// doctor is the answer to "is it watching me, or is it blind?", so it must always be able to run and
// must always print a summary, even on a machine where most conditions are absent.
func TestDoctorAlwaysPrintsASummary(t *testing.T) {
	var out strings.Builder
	code := runDoctor([]string{"--dir", t.TempDir()}, &out)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	text := out.String()
	for _, want := range []string{"store.low-space", "job.ending", "doctor:", "absent"} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor is missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "no complete checkpoint yet") {
		t.Fatalf("a directory without a checkpoint must say so:\n%s", text)
	}
}

func TestDoctorNeedsADirectory(t *testing.T) {
	var out strings.Builder
	if code := runDoctor(nil, &out); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(out.String(), "--dir") {
		t.Fatalf("usage was not printed:\n%s", out.String())
	}
}
