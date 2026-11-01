package policy

import (
	"go/build"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// TestImportsOnlyStandardLibrary is the mechanical guard for the boundary between the algorithm
// and the implementation. If this test ever needs to be changed to let an import through, the
// boundary is being broken: the algorithm must stay a pure function that knows nothing about
// Kubernetes, directories, or job groups.
func TestImportsOnlyStandardLibrary(t *testing.T) {
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}

	for _, pkg := range packages {
		for name, file := range pkg.Files {
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("%s: reading import %s: %v", name, imported.Path.Value, err)
				}
				found, err := build.Default.Import(path, ".", build.FindOnly)
				if err != nil {
					t.Errorf("%s imports %q, which is not found in the standard library", name, path)
					continue
				}
				if !found.Goroot {
					t.Errorf("%s imports %q, which is outside the standard library", name, path)
				}
			}
		}
	}
}
