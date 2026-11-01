package world

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func node(name, cpu, mem string, unschedulable bool) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.NodeSpec{Unschedulable: unschedulable},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(mem),
		}},
	}
}

func bound(name, node, cpu string, prio int32) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team"},
		Spec: corev1.PodSpec{
			NodeName: node,
			Containers: []corev1.Container{{
				Name: "train",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse(cpu),
				}},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if prio != 0 {
		p.Spec.Priority = &prio
	}
	return p
}

func opts() Options {
	return Options{Cluster: "a", StaleAfter: 30 * time.Second, Units: "all"}
}

// Quantities arrive as strings with units ("16373440Ki", "24"); the summary must carry numbers, and
// the conversion must happen here, once.
func TestFoldNormalisesQuantities(t *testing.T) {
	now := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	s := Fold(
		[]corev1.Node{node("worker-1", "24", "16373440Ki", false)},
		[]corev1.Pod{bound("train-0", "worker-1", "22", 0)},
		opts(), now,
	)
	n := s.Summary.Nodes[0]
	if n.Allocatable.CPUMilli != 24000 {
		t.Fatalf("allocatable cpu = %d", n.Allocatable.CPUMilli)
	}
	if n.Allocatable.MemBytes != 16766402560 {
		t.Fatalf("allocatable mem = %d", n.Allocatable.MemBytes)
	}
	if n.Requested.CPUMilli != 22000 {
		t.Fatalf("requested cpu = %d", n.Requested.CPUMilli)
	}
	if n.RequestsUnknown {
		t.Fatal("a pod declared a request; the node must not be marked unknown")
	}
}

// The trap: pods that declare nothing read as zero, so a full node looks empty. The observer is the
// only one who can tell "nothing declared" from "nothing used", so it says so.
func TestFoldMarksANodeWithNoDeclaredRequests(t *testing.T) {
	now := time.Now().UTC()
	p := bound("train-0", "worker-1", "1", 0)
	p.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
	s := Fold([]corev1.Node{node("worker-1", "24", "16Gi", false)}, []corev1.Pod{p}, opts(), now)
	if !s.Summary.Nodes[0].RequestsUnknown {
		t.Fatal("a node whose pods declare nothing must be marked unknown")
	}

	// An empty node is a different fact: it really is 0%.
	empty := Fold([]corev1.Node{node("worker-1", "24", "16Gi", false)}, nil, opts(), now)
	if empty.Summary.Nodes[0].RequestsUnknown {
		t.Fatal("an empty node must not be marked unknown")
	}
}

func TestFoldCountsPendingHighPriorityPods(t *testing.T) {
	now := time.Now().UTC()
	pending := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "urgent", Namespace: "team"},
		Spec:       corev1.PodSpec{Priority: ptr(int32(10000))},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	s := Fold([]corev1.Node{node("worker-1", "24", "16Gi", false)}, []corev1.Pod{pending}, opts(), now)
	if got := s.Summary.Nodes[0].PendingHigherPriority; got != 1 {
		t.Fatalf("pendingHigherPriority = %d", got)
	}
	if !s.Summary.Capabilities["preemption"].Exists {
		t.Fatal("a cluster with priorities configured must report a preemption upstream")
	}
}

func TestFoldRecordsUnitsAndAnnouncements(t *testing.T) {
	now := time.Now().UTC()
	p := bound("train-0", "worker-1", "1", 1000)
	del := metav1.NewTime(now.Add(-time.Second))
	p.DeletionTimestamp = &del
	p.Labels = map[string]string{"ckpt-governor.io/unit": "true"}

	s := Fold([]corev1.Node{node("worker-1", "24", "16Gi", false)}, []corev1.Pod{p},
		Options{Cluster: "a", StaleAfter: time.Second, Units: "labeled", UnitLabel: "ckpt-governor.io/unit"}, now)
	if len(s.Units) != 1 {
		t.Fatalf("units = %d", len(s.Units))
	}
	u := s.Units[0]
	if u.UnitID != "team/train-0" || u.Node != "worker-1" || u.Priority != 1000 {
		t.Fatalf("record = %+v", u)
	}
	if u.DeletionTimestamp == nil {
		t.Fatal("the announcement was dropped")
	}
}

func ptr[T any](v T) *T { return &v }
