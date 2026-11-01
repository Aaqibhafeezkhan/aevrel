package main

import (
	"fmt"
	"strings"
	"testing"
)

// A precedence chain nobody can see is a chain nobody trusts. This prints which value won and where
// it came from, per key — the part of "file < flag < default" that a person has to be able to check.
func TestConfigSaysWhereEachValueCameFrom(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "conditions.json", `{"memoryLine": 0.9, "slowFactor": 3}`)

	var out strings.Builder
	if code := runConfig([]string{"--dir", dir}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	text := out.String()
	for _, want := range []string{
		"memoryLine       0.9      file     0.95",
		"slowFactor       3        file     2",
		"freeSpaceFactor  2        default  2",
		"precedence flag > file > default",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config is missing %q:\n%s", want, text)
		}
	}
	fmt.Println("config: file 2, default 4 · precedence flag > file > default")
}

// A flag the operator actually typed beats the file; a flag left at its default does not.
func TestAFlagThatWasSetBeatsTheFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "conditions.json", `{"freeSpaceFactor": 3}`)

	var out strings.Builder
	if code := runConfig([]string{"--dir", dir, "--free-factor", "5"}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "freeSpaceFactor  5        flag     2") {
		t.Fatalf("the flag did not win:\n%s", out.String())
	}
}
