package advise

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/policy"
)

func writeState(dir, content string) (string, error) {
	base := filepath.Join(dir, BaseDir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(base, stateName)
	return path, os.WriteFile(path, []byte(content), 0o644)
}

// A structured explanation the decision gains tomorrow must show up today, without a release on this
// side. "Known" comes from the struct's tags, so this only ever holds genuinely unknown fields.
//
// `reasons` and `sensitivity` used to land here; the day they became first-class fields they left
// this view. That graduation is what the test now pins: a field is either carried with a known shape
// or shown raw, never both.
func TestExtrasAreTheFieldsThisBuildDoesNotKnow(t *testing.T) {
	dir := t.TempDir()
	raw := "{\n" +
		"  \"schema\": 1,\n" +
		"  \"epoch\": 3,\n" +
		"  \"unitId\": \"train/llama\",\n" +
		"  \"action\": \"skip\",\n" +
		"  \"reason\": \"calm\",\n" +
		"  \"missing\": [\"freeBytes\"],\n" +
		"  \"reasons\": [{\"code\": \"store.low-space\", \"values\": {\"free\": 1, \"need\": 2}}],\n" +
		"  \"futureField\": {\"x\": 1}\n" +
		"}"
	if _, err := writeState(dir, raw); err != nil {
		t.Fatal(err)
	}

	extras, ok := Extras(dir)
	if !ok {
		t.Fatal("no file was read")
	}
	if _, known := extras["action"]; known {
		t.Fatal("a known field was reported as an extra")
	}
	if _, known := extras["reasons"]; known {
		t.Fatal("reasons is a known field now: it must be carried, not shown raw")
	}
	if _, ok := extras["futureField"]; !ok {
		t.Fatalf("an unknown field was not reported: %v", extras)
	}

	var b strings.Builder
	RenderExtras(&b, extras)
	out := b.String()
	if !strings.Contains(out, "futureField") {
		t.Fatalf("the extras view is missing the unknown field:\n%s", out)
	}
}

// The reasons a plan carries must survive a round trip: they answer "which fact caused this write",
// and a reader cannot recover them from the sentence in Why.
func TestReasonsSurviveTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := policy.Decision{
		UnitID: "u", Generation: 1, Action: policy.ActionSave, Reason: policy.ReasonRisk,
		Why:  "store.low-space: free space is below two checkpoints",
		Kind: policy.WriteFull, CopiesToWrite: 1, KeepCopies: 1, Version: "risk-v1",
		Reasons: []policy.ReasonEntry{{
			Code:     "store.low-space",
			Values:   map[string]float64{"free": 1.9e9, "need": 2e9},
			Evidence: []string{"Evidence[store.low-space]"},
		}},
	}
	if _, err := Publish(dir, d, 1, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	back, ok := LastDecision(dir)
	if !ok {
		t.Fatal("the plan was not read back")
	}
	if len(back.Reasons) != 1 || back.Reasons[0].Code != "store.low-space" {
		t.Fatalf("reasons = %+v", back.Reasons)
	}
	if back.Reasons[0].Values["need"] != 2e9 {
		t.Fatalf("values = %v", back.Reasons[0].Values)
	}
	if len(back.Reasons[0].Evidence) != 1 {
		t.Fatalf("evidence = %v", back.Reasons[0].Evidence)
	}
}
