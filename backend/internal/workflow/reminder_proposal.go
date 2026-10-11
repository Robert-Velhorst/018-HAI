package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

const (
	ReminderProposalAuthority = "reminder_proposal_only"
	ReminderProposalFreshness = "current_internal_reminder_snapshot"
	// Delivery authorization covers the proposal horizon plus its post-due retry grace.
	workflowReminderMaxHorizon = 30 * 24 * time.Hour
	workflowReminderDeliveryGrace = 24 * time.Hour
	workflowReminderMaxAuthorizationLifetime = workflowReminderMaxHorizon + workflowReminderDeliveryGrace
)

type WorkflowReminderCandidate struct {
	Workflow models.WorkflowItem
	Reminder models.WorkflowChecklistItem
}

type WorkflowReminderProposal struct {
	ID               uuid.UUID  `json:"id"`
	WorkflowID       uuid.UUID  `json:"workflowId"`
	ChecklistItemID  uuid.UUID  `json:"checklistItemId"`
	Title            string     `json:"title"`
	Label            string     `json:"label"`
	ProjectKey       string     `json:"projectKey,omitempty"`
	WorkflowState    string     `json:"workflowState"`
	RiskLevel        string     `json:"riskLevel"`
	RequiresApproval bool       `json:"requiresApproval"`
	ReminderAt       time.Time  `json:"reminderAt"`
	DueAt            *time.Time `json:"dueAt,omitempty"`
	Status           string     `json:"status"`
	SourceURI        string     `json:"sourceUri,omitempty"`
	SourceLabel      string     `json:"sourceLabel,omitempty"`
	NextAction       string     `json:"nextAction"`
	EvidenceDigest   string     `json:"evidenceDigest"`
	Authority        string     `json:"authority"`
	CanExecute       bool       `json:"canExecute"`
}

type WorkflowReminderProposalFreshness struct {
	Status               string    `json:"status"`
	RevalidationRequired bool      `json:"revalidationRequired"`
	CheckedAt            time.Time `json:"checkedAt"`
	Reason               string    `json:"reason"`
}

type WorkflowReminderProposalSnapshot struct {
	Items      []WorkflowReminderProposal        `json:"items"`
	Due        int                               `json:"due"`
	Upcoming   int                               `json:"upcoming"`
	Authority  string                            `json:"authority"`
	CanExecute bool                              `json:"canExecute"`
	Freshness  WorkflowReminderProposalFreshness `json:"freshness"`
}

type ReminderProposalService interface {
	ReminderProposalsForOwner(ownerIdentity string, now time.Time, horizonHours int, limit int) (*WorkflowReminderProposalSnapshot, error)
}

func (s *service) ReminderProposalsForOwner(
	ownerIdentity string,
	now time.Time,
	horizonHours int,
	limit int,
) (*WorkflowReminderProposalSnapshot, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("an authenticated owner is required")
	}
	if now.IsZero() {
		return nil, fmt.Errorf("a current reminder check time is required")
	}
	if horizonHours < 1 || horizonHours > int(workflowReminderMaxHorizon/time.Hour) {
		return nil, fmt.Errorf("horizonHours must be between 1 and 720")
	}
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("limit must be between 1 and 200")
	}
	now = now.UTC()
	candidates, err := s.repo.FindReminderCandidatesForOwner(
		ownerIdentity, now.Add(time.Duration(horizonHours)*time.Hour), limit,
	)
	if err != nil {
		return nil, err
	}

	result := &WorkflowReminderProposalSnapshot{
		Items:      []WorkflowReminderProposal{},
		Authority:  ReminderProposalAuthority,
		CanExecute: false,
		Freshness: WorkflowReminderProposalFreshness{
			Status:               ReminderProposalFreshness,
			RevalidationRequired: true,
			CheckedAt:            now,
			Reason:               "Reminder timing and workflow state must be revalidated before any internal notification or external calendar effect.",
		},
	}
	seen := make(map[uuid.UUID]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Reminder.ID == uuid.Nil || candidate.Workflow.ID == uuid.Nil ||
			candidate.Reminder.WorkflowID != candidate.Workflow.ID || candidate.Reminder.ReminderAt == nil {
			return nil, fmt.Errorf("reminder evidence crossed its workflow boundary")
		}
		if _, duplicate := seen[candidate.Reminder.ID]; duplicate {
			return nil, fmt.Errorf("reminder evidence contains duplicate checklist items")
		}
		seen[candidate.Reminder.ID] = struct{}{}
		status := "upcoming"
		if !candidate.Reminder.ReminderAt.After(now) {
			status = "due"
			result.Due++
		} else {
			result.Upcoming++
		}
		proposal := WorkflowReminderProposal{
			ID:               candidate.Reminder.ID,
			WorkflowID:       candidate.Workflow.ID,
			ChecklistItemID:  candidate.Reminder.ID,
			Title:            candidate.Workflow.Title,
			Label:            candidate.Reminder.Label,
			ProjectKey:       candidate.Workflow.ProjectKey,
			WorkflowState:    candidate.Workflow.CurrentState,
			RiskLevel:        candidate.Workflow.RiskLevel,
			RequiresApproval: candidate.Reminder.RequiresApproval || candidate.Workflow.RequiresApproval,
			ReminderAt:       candidate.Reminder.ReminderAt.UTC(),
			DueAt:            utcTimePointer(candidate.Reminder.DueAt),
			Status:           status,
			SourceURI:        candidate.Workflow.SourceURI,
			SourceLabel:      candidate.Workflow.SourceLabel,
			NextAction:       "Review this internal reminder before any external follow-up or calendar write.",
			Authority:        ReminderProposalAuthority,
			CanExecute:       false,
		}
		proposal.EvidenceDigest, err = reminderEvidenceDigest(candidate)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, proposal)
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		if result.Items[i].ReminderAt.Equal(result.Items[j].ReminderAt) {
			return result.Items[i].ID.String() < result.Items[j].ID.String()
		}
		return result.Items[i].ReminderAt.Before(result.Items[j].ReminderAt)
	})
	return result, nil
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func (r *GormRepository) FindReminderCandidatesForOwner(
	ownerIdentity string,
	before time.Time,
	limit int,
) ([]WorkflowReminderCandidate, error) {
	if strings.TrimSpace(ownerIdentity) == "" || before.IsZero() || limit < 1 || limit > 200 {
		return nil, fmt.Errorf("valid owner, horizon, and reminder limit are required")
	}
	type reminderRow struct {
		ChecklistID      uuid.UUID  `gorm:"column:checklist_id"`
		WorkflowID       uuid.UUID  `gorm:"column:workflow_id"`
		Label            string     `gorm:"column:label"`
		ChecklistStatus  string     `gorm:"column:checklist_status"`
		RequiresApproval bool       `gorm:"column:checklist_requires_approval"`
		DueAt            *time.Time `gorm:"column:due_at"`
		ReminderAt       *time.Time `gorm:"column:reminder_at"`
		Title            string     `gorm:"column:title"`
		ProjectKey       string     `gorm:"column:project_key"`
		WorkflowState    string     `gorm:"column:workflow_state"`
		RiskLevel        string     `gorm:"column:risk_level"`
		WorkflowApproval bool       `gorm:"column:workflow_requires_approval"`
		SourceURI        string     `gorm:"column:source_uri"`
		SourceLabel      string     `gorm:"column:source_label"`
	}
	rows := []reminderRow{}
	err := r.DB.Table("workflow_checklist_items AS checklist").
		Select(`checklist.id AS checklist_id, checklist.workflow_id, checklist.label,
			checklist.status AS checklist_status, checklist.requires_approval AS checklist_requires_approval,
			checklist.due_at, checklist.reminder_at, workflow.title, workflow.project_key,
			workflow.current_state AS workflow_state, workflow.risk_level,
			workflow.requires_approval AS workflow_requires_approval,
			workflow.source_uri, workflow.source_label`).
		Joins("JOIN workflow_items AS workflow ON workflow.id = checklist.workflow_id").
		Where("workflow.owner_identity = ?", strings.TrimSpace(ownerIdentity)).
		Where("workflow.archived = ?", false).
		Where("workflow.current_state NOT IN ?", []string{StateCompleted, StateArchived}).
		Where("checklist.status = ?", "open").
		Where("checklist.reminder_at IS NOT NULL AND checklist.reminder_at <= ?", before.UTC()).
		Order("checklist.reminder_at ASC, checklist.id ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]WorkflowReminderCandidate, 0, len(rows))
	for _, row := range rows {
		result = append(result, WorkflowReminderCandidate{
			Workflow: models.WorkflowItem{
				ID:               row.WorkflowID,
				Title:            row.Title,
				ProjectKey:       row.ProjectKey,
				CurrentState:     row.WorkflowState,
				RiskLevel:        row.RiskLevel,
				RequiresApproval: row.WorkflowApproval,
				SourceURI:        row.SourceURI,
				SourceLabel:      row.SourceLabel,
			},
			Reminder: models.WorkflowChecklistItem{
				ID:               row.ChecklistID,
				WorkflowID:       row.WorkflowID,
				Label:            row.Label,
				Status:           row.ChecklistStatus,
				RequiresApproval: row.RequiresApproval,
				DueAt:            row.DueAt,
				ReminderAt:       row.ReminderAt,
			},
		})
	}
	return result, nil
}
