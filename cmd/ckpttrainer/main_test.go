package main

import (
	"testing"
)

// The membership file is read by a machine and diffed by a person: one rank per line, no header, and
// never empty (a job with no members is still one rank).
func TestMemberList(t *testing.T) {
	if got, want := memberList(3), "rank-0\nrank-1\nrank-2\n"; got != want {
		t.Fatalf("memberList(3) = %q, want %q", got, want)
	}
	if got, want := memberList(0), "rank-0\n"; got != want {
		t.Fatalf("memberList(0) = %q, want %q", got, want)
	}
}
