package executionauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	TaskReviewApprovalFreshnessLimit = 15 * time.Minute
	TaskReviewApprovalFutureSkew     = 5 * time.Second
)

// TaskReviewDecisionEvidence is the immutable, versioned identity hashed by
// task-review approval resolution and rechecked at the execution claim.
type TaskReviewDecisionEvidence struct {
	ID               string    `json:"id"`
	ReviewItemID     string    `json:"reviewItemId"`
	ReviewRevision   int       `json:"reviewRevision"`
	TaskPlanID       string    `json:"taskPlanId"`
	Decision         string    `json:"decision"`
	ResolutionNote   string    `json:"resolutionNote"`
	ResolvedBy       string    `json:"resolvedBy"`
	ApprovalSource   string    `json:"approvalSource"`
	ApprovalSourceID string    `json:"approvalSourceId"`
	RequestDigest    string    `json:"requestDigest"`
	ResolvedAt       time.Time `json:"resolvedAt"`
}

// DigestTaskReviewDecision returns the stable digest used to bind an execution
// receipt to the exact immutable task-review decision that authorized it.
func DigestTaskReviewDecision(value TaskReviewDecisionEvidence) (string, error) {
	resolvedAt := value.ResolvedAt.UTC()
	payload, err := json.Marshal(struct {
		ContractVersion  int    `json:"contractVersion"`
		ID               string `json:"id"`
		ReviewItemID     string `json:"reviewItemId"`
		ReviewRevision   int    `json:"reviewRevision"`
		TaskPlanID       string `json:"taskPlanId"`
		Decision         string `json:"decision"`
		ResolutionNote   string `json:"resolutionNote"`
		ResolvedBy       string `json:"resolvedBy"`
		ApprovalSource   string `json:"approvalSource"`
		ApprovalSourceID string `json:"approvalSourceId"`
		RequestDigest    string `json:"requestDigest"`
		ResolvedAt       string `json:"resolvedAt"`
	}{
		ContractVersion:  1,
		ID:               value.ID,
		ReviewItemID:     value.ReviewItemID,
		ReviewRevision:   value.ReviewRevision,
		TaskPlanID:       value.TaskPlanID,
		Decision:         value.Decision,
		ResolutionNote:   value.ResolutionNote,
		ResolvedBy:       value.ResolvedBy,
		ApprovalSource:   value.ApprovalSource,
		ApprovalSourceID: value.ApprovalSourceID,
		RequestDigest:    value.RequestDigest,
		ResolvedAt:       resolvedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
