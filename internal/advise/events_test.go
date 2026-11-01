package advise

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestEventsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	in := []Event{
		{At: at, Unit: "u", Kind: EventDecision, Code: "store.low-space", Values: map[string]any{"free": 1.0}},
		{At: at.Add(time.Second), Unit: "u", Kind: EventAttemptStart, Code: "condition"},
		{At: at.Add(2 * time.Second), Unit: "u", Kind: EventAttemptEnd, Code: "sealed"},
	}
	for _, e := range in {
		if err := Record(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadEvents(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(in) {
		t.Fatalf("read %d events, want %d", len(got), len(in))
	}
	if got[0].Code != "store.low-space" || got[0].Values["free"] != 1.0 {
		t.Fatalf("first event = %+v", got[0])
	}
	if !got[2].At.Equal(at.Add(2 * time.Second)) {
		t.Fatalf("last event time = %s", got[2].At)
	}
}

func TestReadEventsKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := Record(dir, Event{Kind: EventDecision, Code: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadEvents(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Code != "d" || got[1].Code != "e" {
		t.Fatalf("tail = %+v, want d,e", got)
	}
}

// The file is a record, not an archive: it must never grow without bound, and the newest lines are
// the ones worth keeping.
func TestTheEventFileStaysBounded(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 256)
	for i := 0; i < 5000; i++ {
		if err := Record(dir, Event{Kind: EventDecision, Code: long}); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(eventsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > eventsMaxBytes {
		t.Fatalf("file = %d bytes, want at most %d", info.Size(), eventsMaxBytes)
	}
	all, err := ReadEvents(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("the trim removed everything")
	}
	if all[len(all)-1].Code != long {
		t.Fatal("the newest event was lost")
	}
}
