package background

import (
	"sync"

	"automation-hub-backend/internal/operations"
)

// lease is a process-local, non-blocking run lease. Its owner token prevents a
// stale release from unlocking a later run that reused the same lease object.
type lease struct {
	mu         sync.Mutex
	owner      uint64
	generation uint64
}

func newLease() *lease { return &lease{} }

func (l *lease) acquire() (uint64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != 0 {
		return 0, false
	}
	l.generation++
	if l.generation == 0 {
		l.generation++
	}
	l.owner = l.generation
	return l.owner, true
}

func (l *lease) release(owner uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if owner == 0 || l.owner != owner {
		return false
	}
	l.owner = 0
	return true
}

type leaseKey struct {
	service     *operations.Service
	ownerUserID string
	workspaceID string
}

// runLeases prevent overlapping runs only within this process and only when
// workers share the exact same operations.Service pointer and owner/workspace.
// They are not durable distributed claims: another service wrapper, process,
// or backend replica can independently acquire a lease for the same operation.
var runLeases = struct {
	sync.Mutex
	active map[leaseKey]*lease
}{active: make(map[leaseKey]*lease)}

func acquireRunLease(key leaseKey) (*lease, uint64, bool) {
	runLeases.Lock()
	defer runLeases.Unlock()

	current := runLeases.active[key]
	if current == nil {
		current = newLease()
		runLeases.active[key] = current
	}
	owner, ok := current.acquire()
	if !ok {
		return nil, 0, false
	}
	return current, owner, true
}

func releaseRunLease(key leaseKey, acquired *lease, owner uint64) {
	runLeases.Lock()
	defer runLeases.Unlock()
	if current := runLeases.active[key]; current == acquired && current.release(owner) {
		delete(runLeases.active, key)
	}
}
