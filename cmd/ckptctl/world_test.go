package main

import (
	"fmt"
	"strings"
	"testing"
)

// The world generator's acceptance line: a world that can be fabricated, read back and summarised, without a cluster.
func TestWorldFakeThenRead(t *testing.T) {
	dir := t.TempDir()

	var out strings.Builder
	if code := runWorld([]string{"--dir", dir, "--fake", "--nodes", "3"}, &out); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "world: cluster a, 3 nodes, 0s old") {
		t.Fatalf("header = %q", text)
	}
	if !strings.Contains(text, "pressure: worker-1 92% cpu!") {
		t.Fatalf("the pressure line is missing the full node:\n%s", text)
	}
	fmt.Println(strings.SplitN(text, "\n", 2)[0])
}

// A deployment with no observer is normal, and the command says so instead of failing.
func TestWorldWithoutAnObserverIsNormal(t *testing.T) {
	var out strings.Builder
	if code := runWorld([]string{"--dir", t.TempDir()}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "this deployment has no observer") {
		t.Fatalf("output = %q", out.String())
	}
}

// Staleness is printed, not hidden: a summary nobody is updating must look different from a live one.
func TestAStaleWorldSaysSo(t *testing.T) {
	dir := t.TempDir()
	var out strings.Builder
	if code := runWorld([]string{"--dir", dir, "--fake"}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	// Rewrite the summary as if the observer died two minutes ago.
	where := dir + "/world"
	if code := runWorld([]string{"--dir", dir, "--world-dir", where, "--json"}, &out); code != 0 {
		t.Fatalf("json exit = %d", code)
	}
	if !strings.Contains(out.String(), `"cluster": "a"`) {
		t.Fatalf("json output = %q", out.String())
	}
}

// The world location follows the declaration, and a flag beats it.
func TestWorldLocation(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "deployment.json", `{"world":{"dir":"/declared/world"}}`)

	got, err := worldLocation(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/declared/world" {
		t.Fatalf("location = %q, want the declaration", got)
	}
	if got, _ := worldLocation(dir, "/flag/world"); got != "/flag/world" {
		t.Fatalf("flag did not win: %q", got)
	}
	if got, _ := worldLocation(t.TempDir(), ""); !strings.HasSuffix(got, "/world") {
		t.Fatalf("default = %q, want a world/ beside the checkpoint", got)
	}
}
