package workflow

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/privacyfilter"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ProjectDossierWorkflowLimit = 20
	ProjectDossierMemoryLimit   = 30
	ProjectDossierContextLimit  = 50
	projectDossierTextLimit     = 2048
)

var (
	ErrProjectDossierOwnerRequired      = errors.New("project dossier owner is required")
	ErrProjectDossierProjectKeyRequired = errors.New("project dossier project key is required")
	ErrProjectDossierUnavailable        = errors.New("project dossier is unavailable")
	ErrProjectDossierMemoryUnavailable  = errors.New("owner-scoped project memory is unavailable")
	ErrProjectDossierWorkflowNotFound   = errors.New("active owner-scoped project workflow not found")

	projectDossierCredentialPattern = regexp.MustCompile(`(?i)\b(password|passphrase|api[_-]?key|secret|access[_-]?token|refresh[_-]?token|auth[_-]?token|client[_-]?secret|credential)\b(\s*[:=]\s*)("[^"]*"|'[^']*'|[^,\s&;]+)`)
	projectDossierBearerPattern     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	projectDossierJWTpattern        = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{0,}`)
	projectDossierCredentialTokens  = []*regexp.Regexp{
		regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`),
		regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`),
		regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
		regexp.MustCompile(`(?i)\bAuthorization\s*:\s*Basic\s+[A-Za-z0-9+/=]+`),
	}
)

// ProjectDossierWorkflowRepository is an optional, deliberately narrow query
// surface. It keeps dossier reads owner/project scoped and bounded without
// widening the workflow service or repository contracts used by workers.
type ProjectDossierWorkflowRepository interface {
	FindProjectDossierItemsForOwner(ownerIdentity, projectKey string, limit int) ([]models.WorkflowItem, int64, error)
	FindProjectDossierContextForOwner(ownerIdentity, projectKey string, workflowID uuid.UUID, limit int) (ProjectDossierWorkflowContext, error)
}

type ProjectDossierWorkflowContext struct {
	Item        models.WorkflowItem
	Checklist   []models.WorkflowChecklistItem
	OpenLoops   []models.WorkflowOpenLoop
	SourceLinks []models.WorkflowSourceLink
	Evidence    []models.WorkflowEvidenceClaim
	Decisions   []models.WorkflowDecision
	Truncated   bool
}

type ProjectDossierService interface {
	ProjectDossierForOwner(ownerIdentity, projectKey string) (*ProjectDossier, error)
}

type projectDossierService struct {
	repository    Repository
	memoryService memory.Service
}

func NewProjectDossierService(repository Repository, memoryService memory.Service) ProjectDossierService {
	return &projectDossierService{repository: repository, memoryService: memoryService}
}

type ProjectDossier struct {
	ProjectKey  string                   `json:"projectKey"`
	GeneratedAt time.Time                `json:"generatedAt"`
	Counts      ProjectDossierCounts     `json:"counts"`
	Truncated   ProjectDossierTruncation `json:"truncated"`
	Workflows   []ProjectDossierWorkflow `json:"workflows"`
	Memories    []ProjectDossierMemory   `json:"memories"`
}

type ProjectDossierCounts struct {
	MatchingWorkflows      int64 `json:"matchingWorkflows"`
	ReturnedWorkflows      int   `json:"returnedWorkflows"`
	ReturnedMemories       int   `json:"returnedMemories"`
	ChecklistItemsReturned int   `json:"checklistItemsReturned"`
	OpenLoopsReturned      int   `json:"openLoopsReturned"`
	SourceLinksReturned    int   `json:"sourceLinksReturned"`
	EvidenceClaimsReturned int   `json:"evidenceClaimsReturned"`
	DecisionsReturned      int   `json:"decisionsReturned"`
}

type ProjectDossierTruncation struct {
	Workflows       bool `json:"workflows"`
	Memories        bool `json:"memories"`
	WorkflowContext bool `json:"workflowContext"`
	Text            bool `json:"text"`
}

type ProjectDossierWorkflow struct {
	ID               uuid.UUID                     `json:"id"`
	Title            string                        `json:"title"`
	State            string                        `json:"state"`
	TaskType         string                        `json:"taskType,omitempty"`
	RiskLevel        string                        `json:"riskLevel,omitempty"`
	PriorityScore    int                           `json:"priorityScore"`
	Confidence       float64                       `json:"confidence"`
	AutonomyLevel    string                        `json:"autonomyLevel,omitempty"`
	RequiresApproval bool                          `json:"requiresApproval"`
	ApprovalStatus   string                        `json:"approvalStatus,omitempty"`
	ApprovalReason   string                        `json:"approvalReason,omitempty"`
	BlockedReason    string                        `json:"blockedReason,omitempty"`
	NextAction       string                        `json:"nextAction,omitempty"`
	DueAt            *time.Time                    `json:"dueAt,omitempty"`
	UpdatedAt        time.Time                     `json:"updatedAt"`
	Checklist        []ProjectDossierChecklistItem `json:"checklist"`
	OpenLoops        []ProjectDossierOpenLoop      `json:"openLoops"`
	SourceLinks      []ProjectDossierSourceLink    `json:"sourceLinks"`
	Evidence         []ProjectDossierEvidence      `json:"evidence"`
	Decisions        []ProjectDossierDecision      `json:"decisions"`
}

type ProjectDossierChecklistItem struct {
	ID               uuid.UUID  `json:"id"`
	Label            string     `json:"label"`
	Status           string     `json:"status"`
	RequiresApproval bool       `json:"requiresApproval"`
	DueAt            *time.Time `json:"dueAt,omitempty"`
	ReminderAt       *time.Time `json:"reminderAt,omitempty"`
}

type ProjectDossierOpenLoop struct {
	ID               uuid.UUID  `json:"id"`
	ResponsibleParty string     `json:"responsibleParty,omitempty"`
	WaitingFor       string     `json:"waitingFor,omitempty"`
	NextAction       string     `json:"nextAction,omitempty"`
	FollowUpAt       *time.Time `json:"followUpAt,omitempty"`
	Status           string     `json:"status"`
}

type ProjectDossierSourceLink struct {
	SourceType   string    `json:"sourceType,omitempty"`
	SourceURI    string    `json:"sourceUri,omitempty"`
	SourceLabel  string    `json:"sourceLabel,omitempty"`
	Relationship string    `json:"relationship,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type ProjectDossierEvidence struct {
	ClaimText   string    `json:"claimText"`
	SourceURI   string    `json:"sourceUri,omitempty"`
	SourceLabel string    `json:"sourceLabel,omitempty"`
	Reliability string    `json:"reliability"`
	Status      string    `json:"status"`
	NeedsReview bool      `json:"needsReview"`
	CreatedAt   time.Time `json:"createdAt"`
}

type ProjectDossierDecision struct {
	DecisionType string    `json:"decisionType"`
	Decision     string    `json:"decision"`
	Reason       string    `json:"reason,omitempty"`
	RuleApplied  string    `json:"ruleApplied,omitempty"`
	Approved     bool      `json:"approved"`
	CreatedAt    time.Time `json:"createdAt"`
}

type ProjectDossierMemory struct {
	ID          uuid.UUID  `json:"id"`
	Kind        string     `json:"kind"`
	Content     string     `json:"content"`
	Summary     string     `json:"summary,omitempty"`
	Tags        string     `json:"tags,omitempty"`
	Confidence  float64    `json:"confidence"`
	SourceURI   string     `json:"sourceUri,omitempty"`
	SourceLabel string     `json:"sourceLabel,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

func (s *projectDossierService) ProjectDossierForOwner(ownerIdentity, projectKey string) (*ProjectDossier, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, ErrProjectDossierOwnerRequired
	}
	if projectKey == "" || strings.TrimSpace(projectKey) == "" {
		return nil, ErrProjectDossierProjectKeyRequired
	}
	if s == nil {
		return nil, ErrProjectDossierUnavailable
	}
	repository, ok := s.repository.(ProjectDossierWorkflowRepository)
	if !ok || repository == nil {
		return nil, ErrProjectDossierUnavailable
	}
	if s.memoryService == nil {
		return nil, ErrProjectDossierMemoryUnavailable
	}
	if _, ok := s.memoryService.(memory.OwnerScopedService); !ok {
		return nil, ErrProjectDossierMemoryUnavailable
	}
	recentMemories, ok := s.memoryService.(memory.RecentMemoryService)
	if !ok || recentMemories == nil {
		return nil, ErrProjectDossierMemoryUnavailable
	}

	textTruncated := false
	dossier := &ProjectDossier{
		ProjectKey:  safeProjectDossierText(projectKey, &textTruncated),
		GeneratedAt: time.Now().UTC(),
		Truncated:   ProjectDossierTruncation{Text: textTruncated},
		Workflows:   []ProjectDossierWorkflow{},
		Memories:    []ProjectDossierMemory{},
	}
	items, matchingWorkflows, err := repository.FindProjectDossierItemsForOwner(ownerIdentity, projectKey, ProjectDossierWorkflowLimit)
	if err != nil || matchingWorkflows < 0 || matchingWorkflows < int64(len(items)) {
		return nil, ErrProjectDossierUnavailable
	}
	if len(items) > ProjectDossierWorkflowLimit {
		items = items[:ProjectDossierWorkflowLimit]
	}
	dossier.Counts.MatchingWorkflows = matchingWorkflows
	dossier.Counts.ReturnedWorkflows = len(items)
	dossier.Truncated.Workflows = matchingWorkflows > int64(len(items))
	for _, item := range items {
		if item.OwnerIdentity != ownerIdentity || item.ProjectKey != projectKey || item.Archived ||
			item.CurrentState == StateCompleted || item.CurrentState == StateArchived || item.ID == uuid.Nil {
			return nil, ErrProjectDossierUnavailable
		}
		context, err := repository.FindProjectDossierContextForOwner(ownerIdentity, projectKey, item.ID, ProjectDossierContextLimit)
		if err != nil || context.Item.ID != item.ID || context.Item.OwnerIdentity != ownerIdentity ||
			context.Item.ProjectKey != projectKey || context.Item.Archived ||
			context.Item.CurrentState == StateCompleted || context.Item.CurrentState == StateArchived {
			return nil, ErrProjectDossierUnavailable
		}
		workflow, textTruncated := projectDossierWorkflow(context)
		dossier.Workflows = append(dossier.Workflows, workflow)
		dossier.Counts.ChecklistItemsReturned += len(workflow.Checklist)
		dossier.Counts.OpenLoopsReturned += len(workflow.OpenLoops)
		dossier.Counts.SourceLinksReturned += len(workflow.SourceLinks)
		dossier.Counts.EvidenceClaimsReturned += len(workflow.Evidence)
		dossier.Counts.DecisionsReturned += len(workflow.Decisions)
		dossier.Truncated.WorkflowContext = dossier.Truncated.WorkflowContext || context.Truncated
		dossier.Truncated.Text = dossier.Truncated.Text || textTruncated
	}

	memories, err := recentMemories.RecentForOwner(ownerIdentity, projectKey, false, ProjectDossierMemoryLimit+1)
	if err != nil {
		return nil, ErrProjectDossierMemoryUnavailable
	}
	if len(memories) > ProjectDossierMemoryLimit+1 {
		memories = memories[:ProjectDossierMemoryLimit+1]
	}
	if len(memories) > ProjectDossierMemoryLimit {
		dossier.Truncated.Memories = true
		memories = memories[:ProjectDossierMemoryLimit]
	}
	for _, item := range memories {
		if item.OwnerIdentity != ownerIdentity || item.ProjectKey != projectKey || item.Archived {
			return nil, ErrProjectDossierMemoryUnavailable
		}
		projected, textTruncated := projectDossierMemory(item)
		dossier.Memories = append(dossier.Memories, projected)
		dossier.Truncated.Text = dossier.Truncated.Text || textTruncated
	}
	dossier.Counts.ReturnedMemories = len(dossier.Memories)
	return dossier, nil
}

func (r *GormRepository) FindProjectDossierItemsForOwner(ownerIdentity, projectKey string, limit int) ([]models.WorkflowItem, int64, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if r == nil || r.DB == nil || ownerIdentity == "" || projectKey == "" || strings.TrimSpace(projectKey) == "" {
		return nil, 0, ErrProjectDossierUnavailable
	}
	if limit <= 0 || limit > ProjectDossierWorkflowLimit {
		limit = ProjectDossierWorkflowLimit
	}
	query := projectDossierActiveItems(r.DB, ownerIdentity, projectKey)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, ErrProjectDossierUnavailable
	}
	var items []models.WorkflowItem
	err := projectDossierActiveItems(r.DB, ownerIdentity, projectKey).
		Select("id", "owner_identity", "project_key", "archived", "current_state", "priority_score", "updated_at").
		Order("priority_score DESC, updated_at DESC, id ASC").
		Limit(limit).
		Find(&items).Error
	if err != nil {
		return nil, 0, ErrProjectDossierUnavailable
	}
	return items, total, nil
}

func (r *GormRepository) FindProjectDossierContextForOwner(ownerIdentity, projectKey string, workflowID uuid.UUID, limit int) (ProjectDossierWorkflowContext, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if r == nil || r.DB == nil || ownerIdentity == "" || projectKey == "" || strings.TrimSpace(projectKey) == "" || workflowID == uuid.Nil {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierUnavailable
	}
	if limit <= 0 || limit > ProjectDossierContextLimit {
		limit = ProjectDossierContextLimit
	}
	var result ProjectDossierWorkflowContext
	err := projectDossierActiveItems(r.DB, ownerIdentity, projectKey).
		Select("id", "owner_identity", "project_key", "archived", "current_state", "title", "task_type", "risk_level", "priority_score", "confidence", "autonomy_level", "requires_approval", "approval_status", "approval_reason", "blocked_reason", "next_action", "source_type", "source_uri", "source_label", "due_at", "created_at", "updated_at").
		Where("id = ?", workflowID).
		Take(&result.Item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierWorkflowNotFound
	}
	if err != nil {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierUnavailable
	}

	activeWorkflowIDs := projectDossierActiveItems(r.DB, ownerIdentity, projectKey).Select("id")
	if err := r.DB.Where("workflow_id = ? AND workflow_id IN (?)", workflowID, activeWorkflowIDs).Order("position ASC, created_at ASC, id ASC").Limit(limit + 1).Find(&result.Checklist).Error; err != nil {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierUnavailable
	}
	if len(result.Checklist) > limit {
		result.Truncated = true
		result.Checklist = result.Checklist[:limit]
	}
	if err := r.DB.Where("workflow_id = ? AND workflow_id IN (?)", workflowID, activeWorkflowIDs).Order("created_at DESC, id ASC").Limit(limit + 1).Find(&result.OpenLoops).Error; err != nil {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierUnavailable
	}
	if len(result.OpenLoops) > limit {
		result.Truncated = true
		result.OpenLoops = result.OpenLoops[:limit]
	}
	if err := r.DB.Where("workflow_id = ? AND workflow_id IN (?)", workflowID, activeWorkflowIDs).Order("created_at DESC, id ASC").Limit(limit + 1).Find(&result.SourceLinks).Error; err != nil {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierUnavailable
	}
	if len(result.SourceLinks) > limit {
		result.Truncated = true
		result.SourceLinks = result.SourceLinks[:limit]
	}
	if err := r.DB.Where("workflow_id = ? AND workflow_id IN (?)", workflowID, activeWorkflowIDs).Order("created_at DESC, id ASC").Limit(limit + 1).Find(&result.Evidence).Error; err != nil {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierUnavailable
	}
	if len(result.Evidence) > limit {
		result.Truncated = true
		result.Evidence = result.Evidence[:limit]
	}
	if err := r.DB.Where("workflow_id = ? AND workflow_id IN (?)", workflowID, activeWorkflowIDs).Order("created_at DESC, id ASC").Limit(limit + 1).Find(&result.Decisions).Error; err != nil {
		return ProjectDossierWorkflowContext{}, ErrProjectDossierUnavailable
	}
	if len(result.Decisions) > limit {
		result.Truncated = true
		result.Decisions = result.Decisions[:limit]
	}
	return result, nil
}

func projectDossierActiveItems(db *gorm.DB, ownerIdentity, projectKey string) *gorm.DB {
	return db.Model(&models.WorkflowItem{}).
		Where("owner_identity = ?", ownerIdentity).
		Where("project_key = ?", projectKey).
		Where("archived = ?", false).
		Where("current_state NOT IN ?", []string{StateCompleted, StateArchived})
}

func projectDossierWorkflow(context ProjectDossierWorkflowContext) (ProjectDossierWorkflow, bool) {
	item := context.Item
	truncated := false
	text := func(value string) string { return safeProjectDossierText(value, &truncated) }
	result := ProjectDossierWorkflow{
		ID: item.ID, Title: text(item.Title), State: text(item.CurrentState), TaskType: text(item.TaskType),
		RiskLevel: text(item.RiskLevel), PriorityScore: item.PriorityScore, Confidence: item.Confidence,
		AutonomyLevel: text(item.AutonomyLevel), RequiresApproval: item.RequiresApproval,
		ApprovalStatus: text(item.ApprovalStatus), ApprovalReason: text(item.ApprovalReason),
		BlockedReason: text(item.BlockedReason), NextAction: text(item.NextAction), DueAt: item.DueAt,
		UpdatedAt: item.UpdatedAt,
		Checklist: []ProjectDossierChecklistItem{}, OpenLoops: []ProjectDossierOpenLoop{},
		SourceLinks: []ProjectDossierSourceLink{}, Evidence: []ProjectDossierEvidence{}, Decisions: []ProjectDossierDecision{},
	}
	if item.SourceType != "" || item.SourceURI != "" || item.SourceLabel != "" {
		result.SourceLinks = append(result.SourceLinks, ProjectDossierSourceLink{
			SourceType: text(item.SourceType), SourceURI: safeProjectDossierURI(item.SourceURI, &truncated),
			SourceLabel: text(item.SourceLabel), CreatedAt: item.CreatedAt,
		})
	}
	for _, item := range context.Checklist {
		result.Checklist = append(result.Checklist, ProjectDossierChecklistItem{
			ID: item.ID, Label: text(item.Label), Status: text(item.Status), RequiresApproval: item.RequiresApproval,
			DueAt: item.DueAt, ReminderAt: item.ReminderAt,
		})
	}
	for _, item := range context.OpenLoops {
		result.OpenLoops = append(result.OpenLoops, ProjectDossierOpenLoop{
			ID: item.ID, ResponsibleParty: text(item.ResponsibleParty), WaitingFor: text(item.WaitingFor),
			NextAction: text(item.NextAction), FollowUpAt: item.FollowUpAt, Status: text(item.Status),
		})
	}
	for _, item := range context.SourceLinks {
		result.SourceLinks = append(result.SourceLinks, ProjectDossierSourceLink{
			SourceType: text(item.SourceType), SourceURI: safeProjectDossierURI(item.SourceURI, &truncated),
			SourceLabel: text(item.SourceLabel), Relationship: text(item.Relationship), CreatedAt: item.CreatedAt,
		})
	}
	if len(result.SourceLinks) > ProjectDossierContextLimit {
		result.SourceLinks = result.SourceLinks[:ProjectDossierContextLimit]
		truncated = true
	}
	for _, item := range context.Evidence {
		result.Evidence = append(result.Evidence, ProjectDossierEvidence{
			ClaimText: text(item.ClaimText), SourceURI: safeProjectDossierURI(item.SourceURI, &truncated),
			SourceLabel: text(item.SourceLabel), Reliability: text(item.Reliability), Status: text(item.Status),
			NeedsReview: item.NeedsReview, CreatedAt: item.CreatedAt,
		})
	}
	for _, item := range context.Decisions {
		result.Decisions = append(result.Decisions, ProjectDossierDecision{
			DecisionType: text(item.DecisionType), Decision: text(item.Decision), Reason: text(item.Reason),
			RuleApplied: text(item.RuleApplied), Approved: item.Approved, CreatedAt: item.CreatedAt,
		})
	}
	return result, truncated
}

func projectDossierMemory(item models.ContextMemory) (ProjectDossierMemory, bool) {
	truncated := false
	text := func(value string) string { return safeProjectDossierText(value, &truncated) }
	return ProjectDossierMemory{
		ID: item.ID, Kind: text(item.Kind), Content: text(item.Content), Summary: text(item.Summary),
		Tags: text(item.Tags), Confidence: item.Confidence, SourceURI: safeProjectDossierURI(item.SourceURI, &truncated),
		SourceLabel: text(item.SourceLabel), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		LastUsedAt: item.LastUsedAt,
	}, truncated
}

func safeProjectDossierText(value string, truncated *bool) string {
	value = strings.TrimSpace(value)
	value = projectDossierCredentialPattern.ReplaceAllString(value, "$1$2[REDACTED]")
	value = projectDossierBearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = projectDossierJWTpattern.ReplaceAllString(value, "[REDACTED:jwt]")
	for _, pattern := range projectDossierCredentialTokens {
		value = pattern.ReplaceAllString(value, "[REDACTED:credential]")
	}
	if len(value) > projectDossierTextLimit && truncated != nil {
		*truncated = true
	}
	return privacyfilter.Scan(value, projectDossierTextLimit).RedactedPreview
}

func safeProjectDossierURI(value string, truncated *bool) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil {
		if parsed.User != nil {
			parsed.User = nil
		}
		query := parsed.Query()
		for key := range query {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key") ||
				strings.Contains(lower, "password") || strings.Contains(lower, "auth") || strings.Contains(lower, "session") ||
				strings.Contains(lower, "credential") || strings.Contains(lower, "signature") {
				query.Set(key, "[REDACTED]")
			}
		}
		parsed.RawQuery = query.Encode()
		value = parsed.String()
	}
	return safeProjectDossierText(value, truncated)
}
