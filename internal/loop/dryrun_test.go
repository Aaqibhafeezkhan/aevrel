package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
	"github.com/bolubo/checkpoint-governor/internal/testutil"
)

// tree is the whole directory as a person auditing a dry run would look at it: every path, and every
// size and mtime. Comparing two trees is how "nothing was written" is proven rather than claimed.
type tree map[string]string

func snapshotTree(t *testing.T, dir string) tree {
	t.Helper()
	out := tree{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			out[rel] = "dir"
			return nil
		}
		out[rel] = fmt.Sprintf("%d bytes %d", info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diffTrees(before, after tree) []string {
	var diffs []string
	for p := range after {
		if _, ok := before[p]; !ok {
			diffs = append(diffs, "created "+p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			diffs = append(diffs, "removed "+p)
			continue
		}
		if before[p] != after[p] {
			diffs = append(diffs, "changed "+p)
		}
	}
	sort.Strings(diffs)
	return diffs
}

// A dry run observes, decides and previews, and the directory afterwards is byte-for-byte the one it
// found: no plan, no ledger, no attempt, no trash, no `may-exit`, no probe. This is the plan/apply
// split every careful control plane has, and it is asserted as a diff rather than promised in prose.
func TestADryRunWritesNothingAtAll(t *testing.T) {
	dir := t.TempDir()
	for _, step := range []int64{1, 2, 3, 4, 5} {
		testutil.Sealed(t, dir, step*100, 1000)
		setModTime(t, filepath.Join(dir, dirName(step*100)), t0.Add(-time.Hour))
	}
	before := snapshotTree(t, dir)

	l, err := Open(Config{
		Dir: dir, UnitID: "train/llama", Bounds: policy.Bounds{MinCopies: 1, NoUs: policy.NoUsNone},
		Policy: policy.Risk{}, Now: func() time.Time { return t0 },
		DryRun: true, Holder: "dry", VerifyEvery: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := l.Round()
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if diffs := diffTrees(before, snapshotTree(t, dir)); len(diffs) > 0 {
		t.Fatalf("a dry run changed the directory:\n%v", diffs)
	}
	// It still answered the question: the decision and the retirement preview are reported.
	if !st.Decided {
		t.Fatal("a dry run must still decide")
	}
	fmt.Printf("dryrun: decided %s/%s · would retire %d copies · directory unchanged (%d entries)\n",
		st.Action, st.Reason, len(st.Deleted), len(before))
}

// copiesOf is the scan's answer for the steps these tests lay down: sealed, one hour old, same size.
func copiesOf(steps ...int64) []policy.Copy {
	at := t0.Add(-time.Hour)
	out := make([]policy.Copy, 0, len(steps))
	for _, s := range steps {
		out = append(out, policy.Copy{Step: s, Bytes: 1000, State: policy.CopySealed, CreatedAt: at})
	}
	return out
}

// The preview must match the act: the same selection function decides both, so a dry run that lists
// a copy the real round would keep (or vice versa) is impossible by construction. This test drives
// both paths on one directory and compares the lists.
func TestTheDryRunPreviewIsTheSameRetirement(t *testing.T) {
	dir := t.TempDir()
	for _, step := range []int64{1, 2, 3, 4} {
		testutil.Sealed(t, dir, step*100, 1000)
		setModTime(t, filepath.Join(dir, dirName(step*100)), t0.Add(-time.Hour))
	}
	now := t0
	// A decision that keeps the newest copy only: three are retirable.
	d := policy.Decision{KeepCopies: 1}

	copies := copiesOf(100, 200, 300, 400)
	preview := retirableBelow(d.OldestStepToKeep, d.KeepCopies, copies)
	acted, err := retireBelow(dir, d.OldestStepToKeep, d.KeepCopies, copies, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != len(acted) {
		t.Fatalf("preview retired %d, act retired %d", len(preview), len(acted))
	}
	for i := range preview {
		if preview[i].Step != acted[i].Step {
			t.Fatalf("preview step %d, act step %d", preview[i].Step, acted[i].Step)
		}
	}
	fmt.Printf("dryrun: preview and act agree on %d copies (steps %v)\n", len(acted), acted)
}
