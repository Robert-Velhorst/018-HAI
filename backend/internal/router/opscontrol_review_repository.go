package router

import (
	"errors"
	"time"

	"automation-hub-backend/internal/opscontrol"
	"automation-hub-backend/internal/task"
)

type opsControlTaskReviewRepository struct {
	repository task.TaskStateRepository
}

func newOpsControlTaskReviewRepository(repository task.TaskStateRepository) opscontrol.ControlReviewRepository {
	return &opsControlTaskReviewRepository{repository: repository}
}

func (r *opsControlTaskReviewRepository) ListPendingControlReviews(owner string, limit int) ([]opscontrol.ControlReviewItem, error) {
	var items []task.ReviewQueueItem
	var err error
	if pending, ok := r.repository.(task.PendingReviewStateRepository); ok {
		items, err = pending.ListPendingReviewItems(owner, limit)
	} else {
		items, err = r.repository.ListReviewItems(owner, limit)
	}
	if err != nil {
		return nil, err
	}
	result := make([]opscontrol.ControlReviewItem, 0, len(items))
	for _, item := range items {
		if item.Status == "open" {
			result = append(result, mapOpsControlReviewItem(item))
		}
	}
	return result, nil
}

func (r *opsControlTaskReviewRepository) CreateControlReview(owner string, item opscontrol.ControlReviewItem) (*opscontrol.ControlReviewItem, error) {
	if item.OwnerIdentity != owner {
		return nil, errors.New("control review owner does not match repository scope")
	}
	created, err := r.repository.CreateReviewItem(owner, task.ReviewQueueItem{
		ID:     item.ID,
		TaskID: item.TaskID,
		Request: task.IntakeRequest{
			OwnerIdentity:   item.OwnerIdentity,
			Request:         item.Request,
			ProjectKey:      item.ProjectKey,
			SuccessCriteria: append([]string(nil), item.SuccessCriteria...),
		},
		Reason:    item.Reason,
		Priority:  "high",
		Status:    item.Status,
		CreatedAt: item.CreatedAt,
	})
	if err != nil {
		return nil, err
	}
	result := mapOpsControlReviewItem(*created)
	return &result, nil
}

func (r *opsControlTaskReviewRepository) FindControlReview(owner, id string) (*opscontrol.ControlReviewItem, error) {
	item, err := r.repository.FindReviewItem(owner, id)
	if err != nil || item == nil {
		return nil, err
	}
	result := mapOpsControlReviewItem(*item)
	return &result, nil
}

func (r *opsControlTaskReviewRepository) ApproveControlReview(owner, id, note string, resolvedAt time.Time) error {
	_, err := r.repository.ResolveReviewItem(owner, id, task.ReviewResolution{
		Decision:   "approved",
		Note:       note,
		ResolvedAt: resolvedAt,
	})
	return err
}

func (r *opsControlTaskReviewRepository) FindApprovedControlReview(owner, id string) (*opscontrol.ControlReviewDecision, error) {
	decision, err := r.repository.FindApprovedReviewDecision(owner, id)
	if err != nil || decision == nil {
		return nil, err
	}
	return &opscontrol.ControlReviewDecision{ResolvedBy: decision.ResolvedBy}, nil
}

func mapOpsControlReviewItem(item task.ReviewQueueItem) opscontrol.ControlReviewItem {
	return opscontrol.ControlReviewItem{
		ID:              item.ID,
		TaskID:          item.TaskID,
		OwnerIdentity:   item.Request.OwnerIdentity,
		Request:         item.Request.Request,
		ProjectKey:      item.Request.ProjectKey,
		SuccessCriteria: append([]string(nil), item.Request.SuccessCriteria...),
		Reason:          item.Reason,
		Status:          item.Status,
		CreatedAt:       item.CreatedAt,
	}
}
