package policy

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

func TestTheReasonVocabularyIsClosed(t *testing.T) {
	// A code written as a string literal is a code nobody can enumerate. Every emitter goes through
	// `reason` with a constant from reasons.go or a condition name, and this fails on a literal.
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Code" {
						continue
					}
					if value, ok := kv.Value.(*ast.BasicLit); ok && value.Kind == token.STRING {
						t.Errorf("%s: reason code %s is a literal; use a constant or a condition name", name, value.Value)
					}
				}
				return true
			})
		}
	}

	seen := map[string]bool{}
	for _, spec := range reasonCatalog {
		if spec.Code == "" {
			t.Fatal("a catalog entry has no code")
		}
		if seen[spec.Code] {
			t.Fatalf("code %q appears twice in the catalog", spec.Code)
		}
		seen[spec.Code] = true
		if len(spec.Values) == 0 {
			t.Fatalf("%q carries no values; a reason with no numbers is prose", spec.Code)
		}
	}
}

func TestReasonsCiteOnlyRealFields(t *testing.T) {
	// Evidence names the snapshot fields a reason read. A name that matches no field is a promise
	// nobody can honour: the UI would look up a column that does not exist.
	snapshot := reflect.TypeOf(Snapshot{})

	resolve := func(name string) bool {
		if strings.HasPrefix(name, "Evidence[") {
			return true
		}
		_, ok := snapshot.FieldByName(name)
		return ok
	}

	for _, spec := range reasonCatalog {
		for _, evidence := range spec.Evidence {
			if !resolve(evidence) {
				t.Fatalf("%q cites %q, which is not a field of Snapshot", spec.Code, evidence)
			}
		}
	}
	// A condition's evidence is its own key in the bag, which is the one form that is always valid.
	for _, c := range Conditions() {
		if !resolve("Evidence[" + c.Name + "]") {
			t.Fatalf("%s cannot cite its evidence key", c.Name)
		}
	}
}

// TestTheCodeSpaceIsClosed: a decision may carry a repair reason or a condition name, and nothing
// else. It is the one question a UI asks before it renders a decision, and the one the replay asks
// of every decision it produces.
func TestTheCodeSpaceIsClosed(t *testing.T) {
	for _, spec := range ReasonCatalog() {
		if !IsKnownCode(spec.Code) {
			t.Fatalf("%q is in the reason catalogue and not known", spec.Code)
		}
	}
	for _, c := range Conditions() {
		if !IsKnownCode(c.Name) {
			t.Fatalf("condition %q is not a known code", c.Name)
		}
	}
	if IsKnownCode("not-a-code") {
		t.Fatal("an unknown string was accepted as a code")
	}
	fmt.Printf("codes: %d repair reasons + %d conditions\n", len(ReasonCatalog()), len(Conditions()))
}

// TestEveryDecisionExplainsItself: a decision always says why it is what it is. Calm says so in
// Reason; a save names the fact that fired; a hold and a repair name their repair reason.
func TestEveryDecisionExplainsItself(t *testing.T) {
	calm := (Risk{}).Decide(calmBounds(), calmSnapshot())
	if calm.Reason != ReasonCalm {
		t.Fatalf("a quiet decision gave reason %q, want calm", calm.Reason)
	}

	firing := report(calmSnapshot(), "risk.preempt", boolean(true))
	save := (Risk{}).Decide(calmBounds(), firing)
	if save.Reason != ReasonRisk || len(save.Reasons) == 0 || save.Reasons[0].Code != "risk.preempt" {
		t.Fatalf("a save gave %q / %v", save.Reason, save.Reasons)
	}

	inFlight := calmSnapshot()
	inFlight.SaveInFlight = true
	hold := (Risk{}).Decide(calmBounds(), inFlight)
	if hold.Reason == "" || len(hold.Reasons) == 0 || hold.Reasons[0].Code != codeWriteInFlight {
		t.Fatalf("a hold gave %q / %v", hold.Reason, hold.Reasons)
	}

	broken := calmSnapshot()
	broken.Copies = []Copy{{Step: 1, Bytes: 10, State: CopyBroken}}
	repair := (Risk{}).Decide(calmBounds(), broken)
	if repair.Reason == "" || len(repair.Reasons) == 0 || repair.Reasons[0].Code != codeRepairBrokenCopy {
		t.Fatalf("a repair gave %q / %v", repair.Reason, repair.Reasons)
	}

	for _, d := range []Decision{calm, save, hold, repair} {
		for _, r := range d.Reasons {
			if r.Code == "" || r.Values == nil {
				t.Fatalf("reason %+v is not structured", r)
			}
		}
	}
}
