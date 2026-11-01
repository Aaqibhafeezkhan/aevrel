package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/bolubo/checkpoint-governor/internal/produce/adapters"
)

// runWorld prints what the implementation side knows about the cluster, and can fabricate one so the
// rest of the system is testable without a cluster.
//
// The header is the first of the three display levels: the row shows
// decisions, the expansion shows facts, and this shows the world they all live in.
func runWorld(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("world", flag.ContinueOnError)
	dir := fs.String("dir", "", "the checkpoint directory (reads deployment.json for the world location)")
	worldDir := fs.String("world-dir", "", "where the observer writes (overrides deployment.json)")
	cluster := fs.String("cluster", "a", "which cluster to read")
	asJSON := fs.Bool("json", false, "print the summary as JSON")
	fake := fs.Bool("fake", false, "write a synthetic world for this deployment, then print it")
	fakeNodes := fs.Int("nodes", 3, "how many nodes the fake world has")
	unitName := fs.String("unit", "", "also project this unit (default: the checkpoint directory name)")
	announce := fs.Bool("announce", false, "with --fake: write the unit as if the platform had announced it is going")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" {
		fmt.Fprint(out, "usage: ckptctl world --dir=<checkpointDir> [--cluster=a] [--fake] [--json]\n")
		return 2
	}

	where, err := worldLocation(*dir, *worldDir)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 2
	}

	if *fake {
		if err := writeFakeWorld(where, *cluster, fakeUnitName(*dir, *unitName), *fakeNodes, *announce, time.Now()); err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1
		}
	}

	s, err := adapters.ReadSummary(where, *cluster)
	if err != nil {
		if os.IsNotExist(err) {
			// A deployment without an observer is a normal deployment, not a fault: everything that
			// needs the world stays absent and the rest of the product works.
			fmt.Fprintf(out, "no world in %s: this deployment has no observer\n", filepath.Join(where, *cluster))
			return 0
		}
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}

	if *asJSON {
		raw, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1
		}
		fmt.Fprintln(out, string(raw))
		return 0
	}

	now := time.Now()
	fmt.Fprintf(out, "world: cluster %s, %d nodes, %s old\n",
		s.Cluster, len(s.Nodes), age(s.Age(now)))
	if s.Stale(now) {
		fmt.Fprintf(out, "STALE: older than %ds — every world-derived condition is unknown\n",
			s.StaleAfterSeconds)
	}
	if caps := capabilityLine(s.Capabilities); caps != "" {
		fmt.Fprintf(out, "capabilities: %s\n", caps)
	}
	fmt.Fprintf(out, "pressure: %s\n", pressureLine(s.Nodes))

	// The projection: this unit's slice of the world, and the only thing the conditions may read.
	name := *unitName
	if name == "" {
		name = filepath.Base(filepath.Clean(*dir))
	}
	dep, err := adapters.LoadDeployment(*dir)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 2
	}
	v, err := adapters.Project(where, *cluster, name, dep, now)
	if err != nil {
		fmt.Fprintf(out, "unit: unknown (%v)\n", err)
		return 1
	}
	fmt.Fprintf(out, "unit: %s\n", v.Line())
	return 0
}

// worldLocation resolves where the observer writes: the flag, then the declaration, then a directory
// beside the checkpoint (which is what a single machine wants and what tests can create).
func worldLocation(ckptDir, flagDir string) (string, error) {
	if flagDir != "" {
		return flagDir, nil
	}
	dep, err := adapters.LoadDeployment(ckptDir)
	if err != nil {
		return "", err
	}
	if dep.WorldDir != "" {
		return dep.WorldDir, nil
	}
	return filepath.Join(ckptDir, "world"), nil
}

// fakeUnitName is the unit id the fake world writes: the flag, or the checkpoint directory's name,
// which is the same default the controller uses for its own identity.
func fakeUnitName(ckptDir, flagUnit string) string {
	if flagUnit != "" {
		return flagUnit
	}
	return filepath.Base(filepath.Clean(ckptDir))
}

func age(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	return d.Round(time.Second).String()
}

// capabilityLine says which upstreams exist, in the same words the doctor uses.
func capabilityLine(caps map[string]adapters.CapabilityWire) string {
	names := make([]string, 0, len(caps))
	for name := range caps {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		c := caps[name]
		if c.Exists {
			parts = append(parts, name+" on")
		} else {
			parts = append(parts, name+" off")
		}
	}
	return joinDot(parts)
}

// pressureLine is the one number a person actually scans: how full each node is.
func pressureLine(nodes []adapters.NodeState) string {
	if len(nodes) == 0 {
		return "no nodes"
	}
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		cpu := share(n.Requested.CPUMilli, n.Allocatable.CPUMilli)
		mark := ""
		if cpu >= 90 {
			mark = "!"
		}
		parts = append(parts, fmt.Sprintf("%s %d%% cpu%s", n.Name, cpu, mark))
	}
	return joinDot(parts)
}

func share(used, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(float64(used)/float64(total)*100 + 0.5)
}

func joinDot(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " · "
		}
		out += p
	}
	return out
}

// writeFakeWorld fabricates a summary so every layer above it can be exercised without a cluster.
// It is deliberately boring: three nodes at 92%, 41% and 12%, so a test can assert on each.
func writeFakeWorld(worldDir, cluster, unitID string, nodes int, announce bool, now time.Time) error {
	if nodes < 1 {
		nodes = 1
	}
	s := adapters.WorldSummary{
		Schema: 1, Cluster: cluster, At: now.UTC(), StaleAfterSeconds: 30,
		Capabilities: map[string]adapters.CapabilityWire{
			"preemption": {Exists: true, Source: "fake"},
			"eviction":   {Exists: true, Source: "fake"},
		},
	}
	shares := []int{92, 41, 12}
	for i := 0; i < nodes; i++ {
		pct := shares[i%len(shares)]
		// The first node carries a queue: the fake unit runs there, and a world where nothing is
		// ever queued could not exercise the inference.
		pending := 0
		if i == 0 {
			pending = 2
		}
		s.Nodes = append(s.Nodes, adapters.NodeState{
			Name:                  fmt.Sprintf("worker-%d", i+1),
			Allocatable:           adapters.Resources{CPUMilli: 24000, MemBytes: 16 << 30, GPUCount: 8},
			Requested:             adapters.Resources{CPUMilli: int64(24000 * pct / 100), MemBytes: 8 << 30},
			PendingHigherPriority: pending,
		})
	}
	if err := adapters.WriteSummary(worldDir, s); err != nil {
		return err
	}
	// The unit itself, so the projection has something to project. With announce the platform has
	// already decided to take it, which is the one signal the whole design is built around.
	u := adapters.UnitRecord{
		Cluster: cluster, UnitID: unitID, At: now.UTC(), Node: "worker-1",
		Priority: 1000, QoS: "Burstable",
	}
	if announce {
		at := now.UTC()
		u.DeletionTimestamp = &at
	}
	return adapters.WriteUnit(worldDir, cluster, u)
}
