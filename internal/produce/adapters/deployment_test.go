package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDeployment(t *testing.T, dir, body string) {
	t.Helper()
	base := filepath.Join(dir, ".ckpt-governor")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, deploymentFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A file that is not there is a deployment that declared nothing. It must be an empty declaration,
// not an error: most deployments will never write this file.
func TestNoDeploymentFileDeclaresNothing(t *testing.T) {
	d, err := LoadDeployment(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Capabilities) != 0 || d.WorldDir != "" {
		t.Fatalf("deployment = %+v", d)
	}
	// And an undeclared capability is assumed on: a missed preemption costs the job.
	if c := d.Capability(CapPreemption); !c.Exists || c.Declared {
		t.Fatalf("undeclared capability = %+v, want assumed on", c)
	}
}

func TestDeclaredCapabilitiesAreRead(t *testing.T) {
	dir := t.TempDir()
	writeDeployment(t, dir, `{"schema":1,"world":{"dir":"/world"},
		"capabilities":{"preemption":{"exists":false,"why":"no scheduler"},
		                "eviction":{"exists":true}}}`)
	d, err := LoadDeployment(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.WorldDir != "/world" {
		t.Fatalf("worldDir = %q", d.WorldDir)
	}
	if c := d.Capability(CapPreemption); c.Exists || !c.Declared || c.Why != "no scheduler" {
		t.Fatalf("preemption = %+v", c)
	}
	if c := d.Capability(CapEviction); !c.Exists || !c.Declared {
		t.Fatalf("eviction = %+v", c)
	}
}

// A typo must stop the controller: "exists": tru would silently leave the operator believing
// preemption is on, which is exactly the belief that loses a job.
func TestBadDeploymentFilesAreErrors(t *testing.T) {
	cases := map[string]string{
		"bad json":           `{"capabilities":`,
		"unknown capability": `{"capabilities":{"preemt":{"exists":true}}}`,
		"missing exists":     `{"capabilities":{"preemption":{"why":"because"}}}`,
		"future schema":      `{"schema":9}`,
	}
	for name, body := range cases {
		dir := t.TempDir()
		writeDeployment(t, dir, body)
		if _, err := LoadDeployment(dir); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}
