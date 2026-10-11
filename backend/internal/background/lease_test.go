package background

import (
	"testing"

	"automation-hub-backend/internal/operations"
)

func TestLeaseReleaseRequiresCurrentOwner(t *testing.T) {
	l := newLease()
	firstOwner, ok := l.acquire()
	if !ok {
		t.Fatal("first owner could not acquire lease")
	}
	if l.release(firstOwner + 1) {
		t.Fatal("lease accepted release from a non-owner")
	}
	if _, ok := l.acquire(); ok {
		t.Fatal("lease was reacquired while still owned")
	}
	if !l.release(firstOwner) {
		t.Fatal("current owner could not release lease")
	}
	secondOwner, ok := l.acquire()
	if !ok || secondOwner == firstOwner {
		t.Fatalf("second owner = %d, acquired = %v; want a fresh lease generation", secondOwner, ok)
	}
	if l.release(firstOwner) {
		t.Fatal("stale owner released a later lease")
	}
	if _, ok := l.acquire(); ok {
		t.Fatal("stale release unlocked the current lease")
	}
	if !l.release(secondOwner) {
		t.Fatal("current second owner could not release lease")
	}
}

func TestRunLeaseDoesNotCoordinateDifferentServiceInstances(t *testing.T) {
	repository := operations.NewMemoryRepository()
	firstService := operations.NewService(repository)
	secondService := operations.NewService(repository)
	firstKey := leaseKey{service: firstService, ownerUserID: "same-user", workspaceID: "same-workspace"}
	secondKey := leaseKey{service: secondService, ownerUserID: "same-user", workspaceID: "same-workspace"}

	firstLease, firstOwner, ok := acquireRunLease(firstKey)
	if !ok {
		t.Fatal("first service instance could not acquire lease")
	}
	defer releaseRunLease(firstKey, firstLease, firstOwner)

	secondLease, secondOwner, ok := acquireRunLease(secondKey)
	if !ok {
		t.Fatal("different service instance unexpectedly shared the first instance's lease")
	}
	defer releaseRunLease(secondKey, secondLease, secondOwner)
}
