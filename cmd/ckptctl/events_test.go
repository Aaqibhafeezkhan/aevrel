package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
)

func seedEvents(t *testing.T, dir string) {
	t.Helper()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	in := []advise.Event{
		{At: base, Kind: advise.EventConfig, Code: "threshold-override", Values: map[string]any{"key": "memoryLine", "value": "0.9"}},
		{At: base.Add(time.Second), Kind: advise.EventDecision, Code: "save", Values: map[string]any{"reason": "risk"}},
		{At: base.Add(2 * time.Second), Kind: advise.EventAttemptEnd, Code: "sealed"},
	}
	for _, e := range in {
		if err := advise.Record(dir, e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEventsPrintsTheRecordInOrder(t *testing.T) {
	dir := t.TempDir()
	seedEvents(t, dir)
	var out strings.Builder
	if code := runEvents([]string{"--dir", dir}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	text := out.String()
	for _, want := range []string{"threshold-override", "key=memoryLine", "decision", "attempt_end", "events: 3 shown, 3 total"} {
		if !strings.Contains(text, want) {
			t.Fatalf("events is missing %q:\n%s", want, text)
		}
	}
}

func TestEventsCanFilterByKind(t *testing.T) {
	dir := t.TempDir()
	seedEvents(t, dir)
	var out strings.Builder
	if code := runEvents([]string{"--dir", dir, "--kind", "config"}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out.String(), "attempt_end") {
		t.Fatalf("the filter let another kind through:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "1 shown") {
		t.Fatalf("summary = %s", out.String())
	}
}

func TestEventsKeepsTheNewestWhenLimited(t *testing.T) {
	dir := t.TempDir()
	seedEvents(t, dir)
	var out strings.Builder
	if code := runEvents([]string{"--dir", dir, "--limit", "1"}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	text := out.String()
	if !strings.Contains(text, "sealed") || strings.Contains(text, "threshold-override") {
		t.Fatalf("the limit kept the wrong end:\n%s", text)
	}
}

// The JSON form is for scripts, so every line must parse on its own.
func TestEventsJSONIsOneObjectPerLine(t *testing.T) {
	dir := t.TempDir()
	seedEvents(t, dir)
	var out strings.Builder
	if code := runEvents([]string{"--dir", dir, "--json"}, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" || strings.HasPrefix(line, "events:") {
			continue
		}
		var e advise.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line is not JSON: %q (%v)", line, err)
		}
		lines++
	}
	if lines != 3 {
		t.Fatalf("parsed %d events, want 3:\n%s", lines, out.String())
	}
}
