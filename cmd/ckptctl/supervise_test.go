package main

import (
	"strings"
	"testing"
	"time"
)

// The unit of work is not the process. One supervisor drives N directories, and the rollup is
// one line — the person watching three jobs should not need three terminals.
func TestSuperviseRollsUpEveryUnit(t *testing.T) {
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	for _, dir := range []string{a, b, c} {
		write(t, dir, "ending", "1")
	}

	var out strings.Builder
	code := runSupervise([]string{
		"--members", strings.Join([]string{a, b, c}, ","),
		"--rounds", "2", "--every", "10ms",
	}, &out)
	if code != 0 {
		t.Fatalf("exit = %d:\n%s", code, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "supervise: units=3") {
		t.Fatalf("the rollup does not count the units:\n%s", text)
	}
	// Every unit was told to stop by its own `ending`, so every unit must have a plan saying save.
	if !strings.Contains(text, "decided=3 save=3 skip=0") {
		t.Fatalf("the rollup does not add up:\n%s", text)
	}
	t.Logf("%s", strings.TrimSpace(text[strings.LastIndex(text, "supervise:"):]))
}

// One directory is a legal deployment, and it is also the case where a bug in the rollup hides.
func TestSuperviseAcceptsASingleUnit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ending", "1")

	var out strings.Builder
	if code := runSupervise([]string{"--members", dir, "--rounds", "1", "--every", "1ms"}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "supervise: units=1") {
		t.Fatalf("output = %s", out.String())
	}
}

// A member list with a trailing comma is what a shell leaves behind; it must not create an empty
// unit that reads as a directory called "".
func TestSplitMembersIgnoresEmptyEntries(t *testing.T) {
	got := splitMembers(" a , b ,, c ,")
	if strings.Join(got, "|") != "a|b|c" {
		t.Fatalf("split = %v", got)
	}
	_ = time.Millisecond
}
