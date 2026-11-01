package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// sealsFrom lays down one sealed copy every period, ending at the given moment, and stamps each
// copy's own time (observe reads the newest change inside the directory, not the directory's mtime).
func sealsFrom(t *testing.T, dir string, last time.Time, first int, period time.Duration, n int) {
	t.Helper()
	start := last.Add(-time.Duration(n-1) * period)
	for i := 0; i < n; i++ {
		step := int64(first + i*100)
		testutil.Sealed(t, dir, step, 1_000_000)
		at := start.Add(time.Duration(i) * period)
		setTreeTimes(t, filepath.Join(dir, testutil.StepDir(step)), at)
	}
}

// A run that wrote every ten minutes holds a fifteen-minute bound, and the certificate says so with
// zero seconds over it. This is the sentence a compliance review asks for.
func TestAuditCertifiesAHeldBound(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	start := now.Add(-65 * time.Minute)
	sealsFrom(t, dir, now.Add(-5*time.Minute), 100, 10*time.Minute, 7)

	led, err := advise.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	led.Status.StartedAt = start.Unix()
	led.Status.WindowSpanSeconds = int64((15 * time.Minute) / time.Second)
	led.Status.Kinds = map[string]int{"window": 6, "final": 1}
	led.WriteSecondsTotal = 6 * 30
	if err := led.Save(dir); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if code := runAudit([]string{dir}, &buf); code != 0 {
		t.Fatalf("audit on a held run exited %d:\n%s", code, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"bound           15m (from the window the ledger last ran with)",
		"seals           7 complete copies",
		"over-bound      0s",
		"writes          7 (6 window · 0 exception · 1 final · 0 repair)",
		"verdict         HELD",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	fmt.Printf("audit: 7 seals every 10m · 15m bound → over-bound 0s · held · 180s of writing\n")
}

// One 25-minute gap breaks a fifteen-minute bound, and the certificate counts the seconds the
// promise was broken rather than reporting an average. The command exits non-zero so CI can fail.
func TestAuditFailsWhenTheBoundWasBroken(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	seals := []time.Time{
		now.Add(-60 * time.Minute), now.Add(-50 * time.Minute),
		now.Add(-25 * time.Minute), now.Add(-10 * time.Minute),
	}
	for i, at := range seals {
		step := int64(100 * (i + 1))
		testutil.Sealed(t, dir, step, 1_000_000)
		setTreeTimes(t, filepath.Join(dir, testutil.StepDir(step)), at)
	}
	led, err := advise.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	led.Status.StartedAt = seals[0].Unix()
	if err := led.Save(dir); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if code := runAudit([]string{"--bound=15m", dir}, &buf); code != 1 {
		t.Fatalf("audit on a broken bound exited %d, want 1:\n%s", code, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"max staleness   25m",
		"over-bound      10m",
		"verdict         NOT HELD — the newest copy was older than the bound",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	fmt.Printf("audit: one 25m gap under 15m → over-bound 10m · not held · exit 1\n")
}

// A run that began without a copy has no coverage until its first seal. That is reported as its own
// line, not folded into an average, and the verdict is not held.
func TestAuditReportsAnUncoveredStart(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	sealsFrom(t, dir, now.Add(-5*time.Minute), 100, 10*time.Minute, 3)
	led, err := advise.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The run started ten minutes before the first copy landed: that interval had nothing on disk.
	led.Status.StartedAt = now.Add(-35 * time.Minute).Unix()
	if err := led.Save(dir); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := runAudit([]string{"--bound=15m", dir}, &buf); code != 1 {
		t.Fatalf("audit with no copy at the start exited %d:\n%s", code, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "uncovered       10m") {
		t.Fatalf("the uncovered interval is missing from:\n%s", out)
	}
	if !strings.Contains(out, "NOT HELD — the run began with no complete copy") {
		t.Fatalf("the verdict is wrong:\n%s", out)
	}
	fmt.Printf("audit: no copy at the start → uncovered 10m reported · not held\n")
}

// A bound is the user's number: with none in the ledger and none on the command line there is
// nothing to certify, and the command says that instead of inventing one.
func TestAuditWithoutABoundRefuses(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if code := runAudit([]string{dir}, &buf); code != 2 {
		t.Fatalf("audit with no bound exited %d, want 2:\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "no bound to certify") {
		t.Fatalf("missing the refusal:\n%s", buf.String())
	}
	fmt.Printf("audit: no bound → nothing certified, exit 2\n")
}
