package loop

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/advise"
	"github.com/bolubo/checkpoint-governor/internal/exec"
	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/produce"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// randomPolicy decides at random, so the fuzz below exercises every path a round has: save, skip, and
// the refusal the gate produces when the world is not ready.
type randomPolicy struct{ rng *rand.Rand }

func (p *randomPolicy) Name() string { return "random" }

func (p *randomPolicy) Decide(b policy.Bounds, s policy.Snapshot) policy.Decision {
	d := policy.Decision{
		UnitID: s.UnitID, Generation: s.Generation, Version: "random",
		Action: policy.ActionSkip, Reason: policy.ReasonCalm, KeepCopies: 1,
	}
	if p.rng.Intn(3) == 0 {
		d.Action, d.Reason, d.SaveNow = policy.ActionSave, policy.ReasonRisk, true
		d.Kind, d.CopiesToWrite = policy.WriteFull, 1
	}
	return d
}

// TestInvariantsHoldOverAThousandRounds is the invariants' acceptance.
//
// Six things must be true after every random round, whatever the world did and whichever path the
// round took: the counters add up, a verdict is never re-judged, a good checkpoint survives, a partial
// directory survives, the newest directory survives, and the history stays bounded.
func TestInvariantsHoldOverAThousandRounds(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := os.MkdirAll(filepath.Join(dir, advise.BaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(7))
	l, err := Open(Config{
		Dir: dir, UnitID: "u",
		Bounds: policy.Bounds{NoUs: policy.NoUsNone, MinCopies: 1},
		Policy: &randomPolicy{rng}, Now: func() time.Time { return now },
		Probes: []produce.Probe{never{"store.low-space"}}, VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}

	const rounds = 1000
	breaks := 0
	for i := 0; i < rounds; i++ {
		// The world moves: a new sealed copy, a broken one, or a write that stops half way.
		if step := int64(i + 1); rng.Intn(3) == 0 {
			switch rng.Intn(3) {
			case 0:
				testutil.Sealed(t, dir, step, 100)
			case 1:
				testutil.Broken(t, dir, step)
			default:
				testutil.Unsealed(t, dir, step, 10)
			}
		}
		now = now.Add(time.Duration(1+rng.Intn(30)) * time.Second)

		before, _ := os.ReadDir(dir)
		// What was partial before the round is judged by what it was before: the job may finish that
		// write during this round, and then the directory is a copy, not a partial.
		var partialBefore []string
		for _, e := range before {
			if isStepDir(e.Name()) && !hasManifest(dir, e.Name()) {
				partialBefore = append(partialBefore, e.Name())
			}
		}
		doneBefore := map[string]exec.Outcome{}
		for _, a := range l.attempts.Attempts {
			if a.Done() {
				doneBefore[a.ID] = a.Outcome
			}
		}
		st, err := l.Round()
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}

		// 1. The counters are a query over the list.
		c := st.Attempts
		if sum := c.Running + c.Sealed + c.Broken + c.Abandoned + c.Failed + c.Refused; sum != len(l.attempts.Attempts) {
			breakIt(t, &breaks, i, "counts do not add up", c)
		}
		// 2. A verdict is never re-judged.
		for _, a := range l.attempts.Attempts {
			if was, ok := doneBefore[a.ID]; ok && was != a.Outcome {
				breakIt(t, &breaks, i, "a finished attempt changed its outcome", a)
			}
		}
		// 3. A good checkpoint survives: if there was one before, there is one now.
		if hadComplete(dir, before) && !hasComplete(dir) {
			breakIt(t, &breaks, i, "the last good checkpoint was removed", nil)
		}
		// 4. A partial directory survives: a write in flight is not garbage.
		for _, name := range partialBefore {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				breakIt(t, &breaks, i, "a partial directory was removed", name)
			}
		}
		// 5. The newest directory survives: it is evidence, not clutter.
		if newest := newestStepDir(before); newest != "" {
			if _, err := os.Stat(filepath.Join(dir, newest)); err != nil {
				breakIt(t, &breaks, i, "the newest directory was removed", newest)
			}
		}
		// 6. The history stays bounded.
		if len(l.attempts.Attempts) > 32 {
			breakIt(t, &breaks, i, "the history outgrew its bound", len(l.attempts.Attempts))
		}
	}
	fmt.Printf("invariants: 6/6 held over %d rounds\n", rounds)
	if breaks != 0 {
		t.Fatalf("%d invariant breaks", breaks)
	}
}

func breakIt(t *testing.T, breaks *int, round int, what string, detail any) {
	*breaks++
	if *breaks <= 3 {
		t.Errorf("round %d: %s (%v)", round, what, detail)
	}
}

func isStepDir(name string) bool {
	if len(name) < 6 || name[:5] != "step-" {
		return false
	}
	for _, r := range name[5:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hasManifest(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name, "manifest.json"))
	return err == nil
}

func hadComplete(dir string, dirs []os.DirEntry) bool {
	for _, e := range dirs {
		if isStepDir(e.Name()) && hasManifest(dir, e.Name()) {
			return true
		}
	}
	return false
}

func hasComplete(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if isStepDir(e.Name()) && hasManifest(dir, e.Name()) {
			return true
		}
	}
	return false
}

func newestStepDir(dirs []os.DirEntry) string {
	newest, best := "", int64(-1)
	for _, e := range dirs {
		if !isStepDir(e.Name()) {
			continue
		}
		n := int64(0)
		for _, r := range e.Name()[5:] {
			n = n*10 + int64(r-'0')
		}
		if n > best {
			newest, best = e.Name(), n
		}
	}
	return newest
}
