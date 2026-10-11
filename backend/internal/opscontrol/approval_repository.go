package opscontrol

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

type MemoryControlApprovalRepository struct {
	mu        sync.RWMutex
	requests  map[string]ControlApprovalRequest
	decisions map[string]ControlApprovalDecision
	byRequest map[string]string
}

func NewMemoryControlApprovalRepository() *MemoryControlApprovalRepository {
	return &MemoryControlApprovalRepository{
		requests:  make(map[string]ControlApprovalRequest),
		decisions: make(map[string]ControlApprovalDecision),
		byRequest: make(map[string]string),
	}
}

func (r *MemoryControlApprovalRepository) CreateRequest(_ context.Context, value ControlApprovalRequest) error {
	if err := validateControlApprovalRequest(value); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := controlApprovalKey(value.OwnerIdentity, value.ID)
	if _, exists := r.requests[key]; exists {
		return ErrControlApprovalDecided
	}
	r.requests[key] = value
	return nil
}

func (r *MemoryControlApprovalRepository) FindRequest(_ context.Context, owner string, id uuid.UUID) (ControlApprovalRequest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.requests[controlApprovalKey(owner, id)]
	if !ok {
		return ControlApprovalRequest{}, ErrControlApprovalNotFound
	}
	return value, nil
}

func (r *MemoryControlApprovalRepository) CreateDecision(_ context.Context, value ControlApprovalDecision) error {
	if err := validateControlApprovalDecision(value); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	requestKey := controlApprovalKey(value.OwnerIdentity, value.RequestID)
	if _, exists := r.requests[requestKey]; !exists {
		return ErrControlApprovalNotFound
	}
	if _, exists := r.byRequest[requestKey]; exists {
		return ErrControlApprovalDecided
	}
	decisionKey := controlApprovalKey(value.OwnerIdentity, value.ID)
	r.decisions[decisionKey] = value
	r.byRequest[requestKey] = decisionKey
	return nil
}

func (r *MemoryControlApprovalRepository) FindDecision(_ context.Context, owner string, id uuid.UUID) (ControlApprovalDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.decisions[controlApprovalKey(owner, id)]
	if !ok {
		return ControlApprovalDecision{}, ErrControlApprovalNotFound
	}
	return value, nil
}

func controlApprovalKey(owner string, id uuid.UUID) string {
	return owner + "\x00" + id.String()
}
