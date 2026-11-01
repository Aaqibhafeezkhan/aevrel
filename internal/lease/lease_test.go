package lease

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestAFreeLeaseIsTaken(t *testing.T) {
	dir := t.TempDir()
	ok, err := Acquire(dir, "a", 15*time.Second, t0)
	if err != nil || !ok {
		t.Fatalf("acquire = %v, %v; want true", ok, err)
	}
	s, found := Read(dir)
	if !found || s.Holder != "a" || s.ExpiresAt != t0.Add(15*time.Second).Unix() {
		t.Fatalf("lease = %+v, found=%v", s, found)
	}
}

func TestASecondHolderIsRefused(t *testing.T) {
	dir := t.TempDir()
	if ok, _ := Acquire(dir, "a", 15*time.Second, t0); !ok {
		t.Fatal("the first holder must win")
	}
	if ok, _ := Acquire(dir, "b", 15*time.Second, t0.Add(time.Second)); ok {
		t.Fatal("a live lease must refuse a second holder")
	}
}

func TestAnExpiredLeaseIsTakenOver(t *testing.T) {
	dir := t.TempDir()
	Acquire(dir, "a", 15*time.Second, t0)
	if ok, _ := Acquire(dir, "b", 15*time.Second, t0.Add(16*time.Second)); !ok {
		t.Fatal("an expired lease must be takeable")
	}
	if s, _ := Read(dir); s.Holder != "b" {
		t.Fatalf("holder = %q, want b", s.Holder)
	}
}

func TestTheHolderRenews(t *testing.T) {
	dir := t.TempDir()
	Acquire(dir, "a", 15*time.Second, t0)
	if ok, _ := Acquire(dir, "a", 15*time.Second, t0.Add(10*time.Second)); !ok {
		t.Fatal("the holder must be able to renew")
	}
	if s, _ := Read(dir); s.ExpiresAt != t0.Add(25*time.Second).Unix() {
		t.Fatalf("expiry = %d, want the renewal", s.ExpiresAt)
	}
}

func TestReleaseOnlyWorksForTheHolder(t *testing.T) {
	dir := t.TempDir()
	Acquire(dir, "a", 15*time.Second, t0)
	if err := Release(dir, "b"); err != nil {
		t.Fatal(err)
	}
	if _, found := Read(dir); !found {
		t.Fatal("a stranger released the lease")
	}
	if err := Release(dir, "a"); err != nil {
		t.Fatal(err)
	}
	if _, found := Read(dir); found {
		t.Fatal("the lease survived its holder's release")
	}
}
