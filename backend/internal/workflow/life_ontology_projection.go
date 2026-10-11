package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const maximumWorkflowProjectionLinks = 12

type LifeOntologyProjector interface {
	ProjectOperationalRecord(context.Context, lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error)
}

// WithLifeOntologyProjection attaches the append-only operational graph to
// the concrete workflow engine in place. Existing users of the workflow
// service retain the same durable engine rather than receiving a second
// wrapper or state store.
func WithLifeOntologyProjection(base Service, projector LifeOntologyProjector) (Service, error) {
	implementation, ok := base.(*service)
	if !ok || implementation == nil {
		return nil, fmt.Errorf("workflow service does not support life ontology projection")
	}
	if projector == nil {
		return nil, fmt.Errorf("life ontology projector is required")
	}
	implementation.lifeOntologyProjector = projector
	return implementation, nil
}

func (s *service) projectWorkflowToLifeGraph(ctx context.Context, workflowID uuid.UUID) error {
	if s == nil || s.lifeOntologyProjector == nil {
		return nil
	}
	item, err := s.repo.FindItem(workflowID)
	if err != nil {
		return fmt.Errorf("load workflow projection state: %w", err)
	}
	owner := strings.TrimSpace(item.OwnerIdentity)
	if owner == "" {
		return nil
	}
	observedAt, err := workflowProjectionObservedAt(item, time.Now().UTC())
	if err != nil {
		return err
	}

	localOnly := workflowProjectionLocalOnly(item)
	sensitivity := lifeontology.SensitivityInternal
	if item.RequiresApproval || item.RiskLevel == "high" || item.RiskLevel == "critical" {
		sensitivity = lifeontology.SensitivitySensitive
		localOnly = true
	}
	verification := workflowProjectionVerification(item)
	confidence := item.Confidence
	if confidence <= 0 {
		confidence = 0.5
	}
	if confidence > 1 {
		confidence = 1
	}
	digest := workflowProjectionDigest(item)
	provenance := []lifeontology.Provenance{{
		ReferenceID:   item.SourceID,
		URI:           safety.RedactURL(firstNonEmpty(item.SourceURI, "workflow://"+item.ID.String())),
		ContentDigest: digest,
		Authority:     firstNonEmpty(item.SourceType, "hai/workflow"),
		CapturedAt:    observedAt,
		LocalOnly:     localOnly,
	}}
	links := s.workflowProjectionLinks(item)
	_, err = s.lifeOntologyProjector.ProjectOperationalRecord(ctx, lifeontology.OperationalProjectionRequest{
		OwnerIdentity:      owner,
		Type:               lifeontology.EntityWorkflow,
		RecordID:           item.ID.String(),
		Domain:             workflowProjectionDomain(item),
		Name:               compactWorkflowProjectionText(safety.RedactSecrets(firstNonEmpty(item.Title, "Workflow")), 256),
		Summary:            compactWorkflowProjectionText(safety.RedactSecrets(firstNonEmpty(item.Description, item.NextAction, "Durable workflow state")), 1800),
		Status:             workflowProjectionStatus(item.CurrentState),
		Priority:           boundedWorkflowPriority(item.PriorityScore),
		DueAt:              cloneWorkflowTime(item.DueAt),
		ObservedAt:         observedAt,
		Confidence:         confidence,
		VerificationStatus: verification,
		Attributes: map[string]string{
			"workflow_state":      compactWorkflowProjectionText(item.CurrentState, 80),
			"task_type":           compactWorkflowProjectionText(item.TaskType, 80),
			"risk_level":          compactWorkflowProjectionText(item.RiskLevel, 80),
			"autonomy_level":      compactWorkflowProjectionText(item.AutonomyLevel, 80),
			"approval_status":     compactWorkflowProjectionText(item.ApprovalStatus, 80),
			"source_revision":     compactWorkflowProjectionText(item.SourceRevision, 64),
			"verification_status": compactWorkflowProjectionText(item.VerificationStatus, 80),
		},
		Provenance:  provenance,
		Sensitivity: sensitivity,
		LocalOnly:   localOnly,
		Links:       links,
	})
	if err != nil {
		return fmt.Errorf("project workflow %s: %w", item.ID, err)
	}
	return nil
}

func workflowProjectionObservedAt(item *models.WorkflowItem, now time.Time) (time.Time, error) {
	if item == nil {
		return time.Time{}, fmt.Errorf("workflow projection item is required")
	}
	observedAt := item.UpdatedAt.UTC()
	if observedAt.IsZero() {
		observedAt = item.CreatedAt.UTC()
	}
	if observedAt.IsZero() {
		return time.Time{}, fmt.Errorf("workflow %s has no durable observation timestamp", item.ID)
	}
	// UpdatedAt can also be a monotonic CAS revision. Repositories may advance
	// that revision by a microsecond when the wall clock has not ticked yet.
	// It is not valid as a future observation time for the advisory graph.
	if !now.IsZero() && observedAt.After(now.UTC()) {
		observedAt = now.UTC()
	}
	return observedAt, nil
}

func (s *service) workflowProjectionLinks(item *models.WorkflowItem) []lifeontology.OperationalLinkRequest {
	if s == nil || item == nil {
		return nil
	}
	links := make([]lifeontology.OperationalLinkRequest, 0, 6)
	seen := map[string]bool{}
	appendLink := func(link lifeontology.OperationalLinkRequest) {
		key := string(link.Type) + "\x00" + link.RecordID + "\x00" + string(link.Relation)
		if link.RecordID == "" || seen[key] || len(links) >= maximumWorkflowProjectionLinks {
			return
		}
		seen[key] = true
		links = append(links, link)
	}
	if projectKey := strings.TrimSpace(item.ProjectKey); projectKey != "" {
		appendLink(lifeontology.OperationalLinkRequest{
			Type: lifeontology.EntityProject, RecordID: projectKey,
			Name:       compactWorkflowProjectionText(projectKey, 256),
			Summary:    "Project linked by the workflow engine.",
			Relation:   lifeontology.RelationBelongsToProject,
			Status:     lifeontology.StatusActive,
			Attributes: map[string]string{"project_key": compactWorkflowProjectionText(projectKey, 256)},
		})
	}
	if sourceID := strings.TrimSpace(item.SourceID); sourceID != "" {
		entityType, relation := workflowSourceEntityType(item.SourceType)
		appendLink(lifeontology.OperationalLinkRequest{
			Type: entityType, RecordID: sourceID,
			Name:    compactWorkflowProjectionText(firstNonEmpty(item.SourceLabel, item.SourceType, "Workflow source"), 256),
			Summary: "Source record linked by workflow intake.", Relation: relation,
			Status:     lifeontology.StatusActive,
			Attributes: map[string]string{"source_type": compactWorkflowProjectionText(item.SourceType, 80)},
		})
	}
	pursuits, err := s.repo.FindLinkedPursuits(item.ID)
	if err == nil {
		for _, pursuit := range pursuits {
			appendLink(lifeontology.OperationalLinkRequest{
				Type: lifeontology.EntityPursuit, RecordID: pursuit.ID.String(),
				Name:     compactWorkflowProjectionText(firstNonEmpty(pursuit.Title, "Pursuit"), 256),
				Summary:  compactWorkflowProjectionText(pursuit.DesiredOutcome, 600),
				Relation: lifeontology.RelationBelongsToPursuit,
				Status:   workflowProjectionStatus(pursuit.Status), Priority: boundedWorkflowPriority(pursuit.PriorityScore),
				Attributes: map[string]string{"risk_level": compactWorkflowProjectionText(pursuit.RiskLevel, 80)},
			})
		}
	}
	return links
}

func workflowSourceEntityType(sourceType string) (lifeontology.EntityType, lifeontology.RelationType) {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "memory", "project_memory", "semantic_memory":
		return lifeontology.EntityMemory, lifeontology.RelationRequires
	case "source", "source_sync":
		return lifeontology.EntitySource, lifeontology.RelationDerivedFrom
	default:
		return lifeontology.EntityDocument, lifeontology.RelationDerivedFrom
	}
}

func workflowProjectionDigest(item *models.WorkflowItem) string {
	value := strings.Join([]string{
		item.ID.String(), item.SourceRevision, item.CurrentState, item.Description,
		item.NextAction, item.VerificationStatus, item.ApprovalStatus,
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func workflowProjectionDomain(item *models.WorkflowItem) lifeontology.Domain {
	if item == nil {
		return lifeontology.DomainPersonalAdmin
	}
	value := strings.ToLower(strings.Join([]string{item.TaskType, item.ProjectKey, item.Title}, " "))
	switch {
	case workflowContains(value, "legal", "lawyer", "government", "court", "municipality", "juridisch", "overheid"):
		return lifeontology.DomainLegalGovernment
	case workflowContains(value, "invoice", "receipt", "bank", "finance", "accounting", "factuur", "rekening"):
		return lifeontology.DomainFinancial
	case workflowContains(value, "medical", "health", "wellbeing", "medisch", "gezondheid"):
		return lifeontology.DomainHealthWellbeing
	case workflowContains(value, "code", "software", "work", "trello", "odoo", "project", "client"):
		return lifeontology.DomainWorkVenture
	default:
		return lifeontology.DomainPersonalAdmin
	}
}

func workflowProjectionStatus(state string) lifeontology.LifecycleStatus {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case StateCompleted:
		return lifeontology.StatusCompleted
	case StateArchived:
		return lifeontology.StatusArchived
	case StateWaitingInput, StateNeedsApproval:
		return lifeontology.StatusWaiting
	case StateBlocked:
		return lifeontology.StatusOpen
	case StateReady, StateInProgress, StateClassified, StateLinked, StateChecklistGenerated:
		return lifeontology.StatusActive
	case StateNewInput:
		return lifeontology.StatusOpen
	default:
		return lifeontology.StatusUnknown
	}
}

func workflowProjectionVerification(item *models.WorkflowItem) lifeontology.VerificationStatus {
	if item == nil {
		return lifeontology.VerificationUnverified
	}
	switch strings.ToLower(strings.TrimSpace(item.VerificationStatus)) {
	case "verified", "test_passed", "passed":
		return lifeontology.VerificationVerified
	case "human_approved":
		return lifeontology.VerificationHumanApproved
	case "source_supported":
		return lifeontology.VerificationSourceSupported
	case "needs_review", "uncertain", "review_required":
		return lifeontology.VerificationNeedsReview
	case "unsupported", "failed":
		return lifeontology.VerificationUnsupported
	}
	if item.RequiresApproval || item.CurrentState == StateBlocked || item.CurrentState == StateNeedsApproval {
		return lifeontology.VerificationNeedsReview
	}
	if strings.TrimSpace(item.SourceRevision) != "" {
		return lifeontology.VerificationSourceSupported
	}
	return lifeontology.VerificationSchemaValidated
}

func workflowProjectionLocalOnly(item *models.WorkflowItem) bool {
	if item == nil {
		return false
	}
	uri := strings.ToLower(strings.TrimSpace(item.SourceURI))
	return strings.HasPrefix(uri, "file:") || strings.HasPrefix(uri, "local:") ||
		strings.HasPrefix(uri, "source-extraction:") || strings.EqualFold(item.SourceType, "memory")
}

func boundedWorkflowPriority(priority int) int {
	if priority < 0 {
		return 0
	}
	if priority > 100 {
		return 100
	}
	return priority
}

func cloneWorkflowTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func compactWorkflowProjectionText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func workflowContains(value string, options ...string) bool {
	for _, option := range options {
		if strings.Contains(value, option) {
			return true
		}
	}
	return false
}
