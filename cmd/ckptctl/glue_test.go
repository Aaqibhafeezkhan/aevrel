package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The stop line's acceptance: the four facts a stop is made of, in one line a person can paste.
func TestGluePrintsTheFourFacts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "expected-bytes", "1258291200")
	write(t, dir, "stopping", "1")
	write(t, dir, "may-exit", "1")
	write(t, dir, "seconds-left", "27")

	var out strings.Builder
	if code := runGlue([]string{"--dir", dir}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	got := strings.TrimSpace(out.String())
	want := "glue: expected=1.2GiB stopRequested=1 mayExit=1 seconds-left=27"
	if got != want {
		t.Fatalf("glue = %q, want %q", got, want)
	}
	fmt.Println(want)
}

// "Stopped but not safe to kill" is a distinct state, and the command must say which one it sees.
func TestGlueDistinguishesAStopFromAnExit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "stopping", "1")

	var out strings.Builder
	if code := runGlue([]string{"--dir", dir}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	text := out.String()
	if !strings.Contains(text, "stopRequested=1 mayExit=0") {
		t.Fatalf("output = %q", text)
	}
	if !strings.Contains(text, "stopped but not safe to exit") {
		t.Fatalf("the note is missing: %q", text)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		1258291200: "1.2GiB",
		10485760:   "10.0MiB",
		2048:       "2.0KiB",
		512:        "512B",
	}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
