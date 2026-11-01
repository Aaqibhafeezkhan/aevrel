package main

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// The expansion shows the world behind the decision — the node, the numbers, their age, and
// whether the answer was a guess or an announcement.
func TestExplainShowsTheWorldBehindTheDecision(t *testing.T) {
	dir := t.TempDir()
	// A full node, so the inference has a signal to name.
	writeAnnouncedWorld(t, dir, false, 92)
	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, io.Discard); code != 0 {
		t.Fatalf("run exited %d", code)
	}

	var out strings.Builder
	if code := runExplain([]string{"--dir", dir}, &out); code != 0 {
		t.Fatalf("explain exited %d", code)
	}
	text := out.String()
	if !strings.Contains(text, "world      node a/worker-1 92% cpu (") {
		t.Fatalf("no world line:\n%s", text)
	}
	if !strings.Contains(text, "basis      inference:") {
		t.Fatalf("the inference does not name itself:\n%s", text)
	}
}

// An announcement is labelled as one: the user must be able to tell which answer a switch can turn
// off, and which one the platform already made.
func TestExplainCallsAnAnnouncementAnAnnouncement(t *testing.T) {
	dir := t.TempDir()
	writeAnnouncedWorld(t, dir, true, 10)
	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, io.Discard); code != 0 {
		t.Fatalf("run exited %d", code)
	}

	var out strings.Builder
	if code := runExplain([]string{"--dir", dir}, &out); code != 0 {
		t.Fatalf("explain exited %d", code)
	}
	if !strings.Contains(out.String(), "basis      announcement") {
		t.Fatalf("the announcement is not labelled:\n%s", out.String())
	}
}

// The row gets one line, not a block: the world belongs to the row in the same way a phase does.
func TestStatusCarriesTheWorldAsOneRow(t *testing.T) {
	dir := t.TempDir()
	writeAnnouncedWorld(t, dir, false, 10)
	if code := runRun([]string{"--dir", dir, "--once", "--no-us=none"}, io.Discard); code != 0 {
		t.Fatalf("run exited %d", code)
	}

	var out strings.Builder
	if code := runStatus([]string{dir}, &out); code != 0 {
		t.Fatalf("status exited %d", code)
	}
	var found bool
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "world      ") {
			found = true
			fmt.Println(strings.TrimSpace(line))
		}
	}
	if !found {
		t.Fatalf("status has no world row:\n%s", out.String())
	}
}
