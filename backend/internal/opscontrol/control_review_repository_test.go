package opscontrol

import (
	"errors"
	"time"
)

type memoryControlReviews struct {
	items     map[string]ControlReviewItem
	decisions map[string]ControlReviewDecision
}

func newMemoryControlReviews() *memoryControlReviews {
	return &memoryControlReviews{
		items:     make(map[string]ControlReviewItem),
		decisions: make(map[string]ControlReviewDecision),
	}
}

func (r *memoryControlReviews) ListPendingControlReviews(owner string, limit int) ([]ControlReviewItem, error) {
	items := make([]ControlReviewItem, 0)
	for _, item := range r.items {
		if item.OwnerIdentity == owner && item.Status == "open" {
			items = append(items, item)
			if limit > 0 && len(items) >= limit {
				break
			}
		}
	}
	return items, nil
}

func (r *memoryControlReviews) CreateControlReview(_ string, item ControlReviewItem) (*ControlReviewItem, error) {
	if item.ID == "" || item.OwnerIdentity == "" || item.Status != "open" || item.CreatedAt.IsZero() || item.Reason == "" {
		return nil, errors.New("invalid control review")
	}
	if _, exists := r.items[item.ID]; exists {
		return nil, errors.New("duplicate review id")
	}
	item.SuccessCriteria = append([]string(nil), item.SuccessCriteria...)
	r.items[item.ID] = item
	return &item, nil
}

func (r *memoryControlReviews) FindControlReview(owner, id string) (*ControlReviewItem, error) {
	item, exists := r.items[id]
	if !exists || item.OwnerIdentity != owner {
		return nil, errors.New("review unavailable")
	}
	item.SuccessCriteria = append([]string(nil), item.SuccessCriteria...)
	return &item, nil
}

func (r *memoryControlReviews) ApproveControlReview(owner, id, note string, at time.Time) error {
	item, exists := r.items[id]
	if !exists || item.OwnerIdentity != owner || item.Status != "open" || at.IsZero() {
		return errors.New("review unavailable")
	}
	item.Status = "approved"
	r.items[id] = item
	r.decisions[id] = ControlReviewDecision{ResolvedBy: owner}
	return nil
}

func (r *memoryControlReviews) FindApprovedControlReview(owner, id string) (*ControlReviewDecision, error) {
	item, exists := r.items[id]
	decision, decided := r.decisions[id]
	if !exists || item.OwnerIdentity != owner || item.Status != "approved" || !decided {
		return nil, errors.New("approved review unavailable")
	}
	return &decision, nil
}
