package simulate

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

// updateGolden rewrites testdata/golden from whatever the policy does today. It exists so that a
// deliberate change can re-pin; a failing test never rewrites its own expectation.
var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/golden from the current policy")

// goldenRecord is one decision of one replayed run. The decision is stored whole — Reasons,
// Sensitivity and Missing included — because the pin has to catch a change in the explanation as
// well as in the schedule. The clock is stored so a diff says when, not which index.
type goldenRecord struct {
	At       time.Time       `json:"at"`
	Decision policy.Decision `json:"decision"`
}

// tap is a policy that records what it was asked and what it answered. It changes nothing else:
// the replay sees exactly the decisions the wrapped policy makes, and every decision is written
// down, including the ones at every step event between two seals.
type tap struct {
	inner comparator
	out   *[]goldenRecord
}

func (t tap) Name() string { return t.inner.Name() }

func (t tap) Decide(b policy.Bounds, v view) plan {
	d := t.inner.Decide(b, v)
	*t.out = append(*t.out, goldenRecord{At: v.Now, Decision: d.Decision})
	return d
}

// TestGoldenDecisions pins the behaviour of every registered policy on every family of the
// generator. It runs before the refactor that splits the policy into a core and factors, and must
// print the same counts afterwards: a refactor that changes one field of one decision is a
// rewrite, and this test is what tells the two apart.
//
// It lives here, not in internal/policy, for two reasons: the pin has to be driven by a trace, and
// internal/policy is not allowed to import anything but the standard library (imports_test.go).
func TestGoldenDecisions(t *testing.T) {
	policies := []string{"risk-v1"}

	families := Families()
	decisions, diffs := 0, 0
	dir := filepath.Join("testdata", "golden")

	for _, name := range policies {
		p, ok := policy.Lookup(name)
		if !ok {
			t.Fatalf("%q is missing from the registry", name)
		}
		for _, fam := range families {
			lines := replay(t, fam, p)
			decisions += len(lines)

			path := filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl.gz", fam, name))
			if *updateGolden {
				if err := writeGolden(path, lines); err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				continue
			}
			want, err := readGolden(path)
			if err != nil {
				t.Fatalf("%s: %v (run with -update-golden to create it)", path, err)
			}
			if len(want) != len(lines) {
				diffs++
				t.Errorf("%s: %d decisions, pinned %d", path, len(lines), len(want))
				continue
			}
			for i := range lines {
				if lines[i] == want[i] {
					continue
				}
				diffs++
				if diffs <= 3 {
					t.Errorf("%s decision %d differs:\n got %s\nwant %s",
						path, i, clip(lines[i]), clip(want[i]))
				}
			}
		}
	}

	fmt.Printf("golden: %d policies, %d families, %d decisions, %d diff\n",
		len(policies), len(families), decisions, diffs)
	if diffs > 0 {
		t.Fatalf("%d decisions differ from testdata/golden; re-pin only if the change is intended", diffs)
	}
}

// replay drives one family through one policy and returns the decisions as canonical JSON lines.
// The lines are compared as text, so the pin is the same bytes a reviewer could read after
// gunzip, and a diff is a pair of strings rather than a wall of field names.
func replay(t *testing.T, fam Family, p policy.Policy) []string {
	t.Helper()
	records := replayRecords(t, fam, p)
	lines := make([]string, len(records))
	for i, r := range records {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("%s decision %d: %v", fam, i, err)
		}
		lines[i] = string(b)
	}
	return lines
}

// replayRecords is the same run as replay, without the encoding: a test that asks a question about
// the decisions themselves (which reason codes fired, whether every decision explains itself).
func replayRecords(t *testing.T, fam Family, p policy.Policy) []goldenRecord {
	t.Helper()
	trace, env, err := Generate(1, fam)
	if err != nil {
		t.Fatalf("%s: %v", fam, err)
	}
	b, e := BoundsFor(fam)
	var records []goldenRecord
	_, err = Run(trace, Options{
		Bounds:     b,
		Eval:       e,
		Policy:     tap{inner: Boundary(p), out: &records},
		CostAt:     env.WriteCost,
		DeltaShare: env.DeltaShare,
		Model:      env.Model,
		// The pin has to exercise the risk core, so the replay stands in for a producer with a
		// modelled 90-second warning. The numbers are an assumption about lead time, not a
		// measurement; what is pinned is the policy's behaviour given them.
		WarnBefore: 90 * time.Second,
	})
	if err != nil {
		t.Fatalf("%s: %v", fam, err)
	}
	return records
}

func writeGolden(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := gzip.NewWriter(f)
	// A zero modification time keeps the file byte-identical across runs, so re-pinning an
	// unchanged policy produces no diff at all.
	zw.ModTime = time.Time{}
	if _, err := zw.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		return err
	}
	return zw.Close()
}

func readGolden(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	text, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSuffix(string(text), "\n")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

func clip(s string) string {
	const max = 240
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
