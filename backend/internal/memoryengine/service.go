package memoryengine

import (
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	pursuitpkg "automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/workflow"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const maxConversationBytes = 2 * 1024 * 1024

const (
	insightStatusCandidate       = "candidate"
	insightStatusVerified        = "verified"
	insightStatusSourceSupported = "source_supported"
	insightStatusOwnerApproved   = "owner_approved"
)

type ChatMessage struct {
	ExternalID string `json:"externalId,omitempty"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp,omitempty"`
}

type ImportRequest struct {
	OwnerIdentity string        `json:"-"`
	Platform      string        `json:"platform"`
	ExternalID    string        `json:"externalId,omitempty"`
	Title         string        `json:"title"`
	SourceURI     string        `json:"sourceUri"`
	ProjectKey    string        `json:"projectKey,omitempty"`
	CapturedAt    string        `json:"capturedAt,omitempty"`
	LastMessageAt string        `json:"lastMessageAt,omitempty"`
	Messages      []ChatMessage `json:"messages"`
}

type ImportResult struct {
	Conversation models.AIConversationArchive `json:"conversation"`
	Insights     []models.AIMemoryInsight     `json:"insights"`
	WorkflowIDs  []uuid.UUID                  `json:"workflowIds"`
	PursuitLinks []pursuitpkg.AutoLinkResult  `json:"pursuitLinks,omitempty"`
	Deduplicated bool                         `json:"deduplicated"`
	Warnings     []string                     `json:"warnings,omitempty"`
}

type ConversationDetail struct {
	Conversation models.AIConversationArchive `json:"conversation"`
	Messages     []ChatMessage                `json:"messages"`
}

type CommandDashboard struct {
	GeneratedAt       time.Time                      `json:"generatedAt"`
	ConversationCount int                            `json:"conversationCount"`
	InsightCount      int                            `json:"insightCount"`
	NeedsRobert       []models.WorkflowItem          `json:"needsRobert"`
	DelegateToVA      []models.AIMemoryInsight       `json:"delegateToVA"`
	OpenLoops         []models.WorkflowOpenLoop      `json:"openLoops"`
	Contradictions    []models.AIMemoryInsight       `json:"contradictions"`
	RecentDecisions   []models.AIMemoryInsight       `json:"recentDecisions"`
	SourceCorrections []models.ContextMemory         `json:"sourceCorrections"`
	Projects          []ProjectSummary               `json:"projects"`
	RecentArchives    []models.AIConversationArchive `json:"recentArchives"`
	Warnings          []string                       `json:"warnings"`
}

type ProjectSummary struct {
	ProjectKey  string `json:"projectKey"`
	Actions     int    `json:"actions"`
	Decisions   int    `json:"decisions"`
	Risks       int    `json:"risks"`
	Corrections int    `json:"corrections"`
	Open        int    `json:"open"`
}

type SearchResult struct {
	Memory *memory.RetrieveResult   `json:"memory"`
	Facts  []models.AIMemoryInsight `json:"facts"`
}

type Service interface {
	Import(request ImportRequest) (*ImportResult, error)
	Conversations(limit int) ([]models.AIConversationArchive, error)
	ConversationsForOwner(ownerIdentity string, limit int) ([]models.AIConversationArchive, error)
	Conversation(id uuid.UUID) (*ConversationDetail, error)
	ConversationForOwner(ownerIdentity string, id uuid.UUID) (*ConversationDetail, error)
	DeleteConversation(id uuid.UUID) error
	DeleteConversationForOwner(ownerIdentity string, id uuid.UUID) error
	Insights(kind, projectKey string, needsReview *bool, limit int) ([]models.AIMemoryInsight, error)
	InsightsForOwner(ownerIdentity, kind, projectKey string, needsReview *bool, limit int) ([]models.AIMemoryInsight, error)
	Dashboard() (*CommandDashboard, error)
	DashboardForOwner(ownerIdentity string) (*CommandDashboard, error)
	Search(query, projectKey string, limit int) (*SearchResult, error)
	SearchForOwner(ownerIdentity, query, projectKey string, limit int) (*SearchResult, error)
}

type service struct {
	repo            Repository
	memoryService   memory.Service
	workflowService workflow.Service
	pursuitLinker   pursuitAutoLinker
	encryptionKey   []byte
}

type pursuitAutoLinker interface {
	AutoLinkWorkflow(request pursuitpkg.AutoLinkWorkflowRequest) (*pursuitpkg.AutoLinkResult, error)
	AutoLinkMemory(request pursuitpkg.AutoLinkMemoryRequest) (*pursuitpkg.AutoLinkResult, error)
}

// pursuitWorkflowIntakeRouter ensures imported chat insights cannot create
// workflow work under a closed pursuit by bypassing the pursuit lifecycle gate.
type pursuitWorkflowIntakeRouter interface {
	RouteWorkflowIntake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error)
}

func NewService(repo Repository, memoryService memory.Service, workflowService workflow.Service, encryptionSecret string) Service {
	return NewServiceWithPursuitLinker(repo, memoryService, workflowService, encryptionSecret, nil)
}

func NewServiceWithPursuitLinker(repo Repository, memoryService memory.Service, workflowService workflow.Service, encryptionSecret string, pursuitLinker pursuitAutoLinker) Service {
	var key []byte
	if secret := strings.TrimSpace(encryptionSecret); secret != "" {
		sum := sha256.Sum256([]byte(secret))
		key = sum[:]
	}
	return &service{
		repo:            repo,
		memoryService:   memoryService,
		workflowService: workflowService,
		pursuitLinker:   pursuitLinker,
		encryptionKey:   key,
	}
}

func (s *service) intakeWorkflow(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	if router, ok := s.pursuitLinker.(pursuitWorkflowIntakeRouter); ok {
		return router.RouteWorkflowIntake(request)
	}
	if s.pursuitLinker != nil {
		return nil, pursuitpkg.ErrLifecycleRouterRequired
	}
	if s.workflowService == nil {
		return nil, fmt.Errorf("workflow service is not configured")
	}
	return s.workflowService.Intake(request)
}

func (s *service) routesWorkflowThroughPursuits() bool {
	_, ok := s.pursuitLinker.(pursuitWorkflowIntakeRouter)
	return ok
}

func (s *service) Import(request ImportRequest) (*ImportResult, error) {
	if len(s.encryptionKey) != 32 {
		return nil, fmt.Errorf("HAI_MEMORY_ENCRYPTION_KEY or BACKEND_API_SHARED_KEY must be configured before importing private chat history")
	}
	normalized, rawPayload, contentHash, err := normalizeImport(request)
	if err != nil {
		return nil, err
	}
	normalized.OwnerIdentity = strings.TrimSpace(normalized.OwnerIdentity)
	existing, err := s.repo.FindConversationForOwner(normalized.OwnerIdentity, normalized.Platform, normalized.ExternalID)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	// A legacy ownerless archive may be readable by the configured migration
	// owner, but cannot be adopted by an authenticated import. Create a distinct
	// owner-scoped archive so new private content is never attached to it.
	if existing != nil && normalized.OwnerIdentity != "" && strings.TrimSpace(existing.OwnerIdentity) == "" {
		existing = nil
	}
	if existing != nil && existing.ContentHash == contentHash {
		insights, _ := s.repo.FindInsightsForOwner(normalized.OwnerIdentity, "", normalized.ProjectKey, nil, 100)
		return &ImportResult{
			Conversation: *existing,
			Insights:     filterConversationInsights(insights, existing.ID, existing.Revision),
			Deduplicated: true,
		}, nil
	}
	if existing != nil {
		oldInsights, errInsights := s.repo.FindInsightsForOwner(normalized.OwnerIdentity, "", "", nil, 500)
		if errInsights != nil {
			return nil, errInsights
		}
		for _, insight := range filterConversationInsights(oldInsights, existing.ID, existing.Revision) {
			if workflowEligibleInsight(insight) && s.workflowService != nil {
				sourceID := existing.ID.String() + ":" + insight.ID.String()
				if errRetract := s.workflowService.RetractSource("ai_chat", sourceID, "AI conversation revision was superseded"); errRetract != nil {
					return nil, fmt.Errorf("retract superseded AI insight workflow: %w", errRetract)
				}
			}
		}
		if errDeleteMemory := s.repo.DeleteMemoriesBySourceURI(existing.OwnerIdentity, existing.SourceURI); errDeleteMemory != nil {
			return nil, fmt.Errorf("remove superseded searchable memory: %w", errDeleteMemory)
		}
	}

	encrypted, nonce, err := encryptPayload(s.encryptionKey, rawPayload)
	if err != nil {
		return nil, err
	}
	capturedAt := parseTime(normalized.CapturedAt, time.Now().UTC())
	lastMessageAt := parseOptionalTime(normalized.LastMessageAt)
	conversation := existing
	if conversation == nil {
		conversation = &models.AIConversationArchive{
			ID:            uuid.New(),
			OwnerIdentity: normalized.OwnerIdentity,
			Platform:      normalized.Platform,
			ExternalID:    normalized.ExternalID,
			Revision:      1,
		}
	} else {
		conversation.Revision++
	}
	conversation.Title = normalized.Title
	conversation.SourceURI = safety.RedactURL(normalized.SourceURI)
	conversation.ContentHash = contentHash
	conversation.MessageCount = len(normalized.Messages)
	conversation.EncryptedPayload = encrypted
	conversation.EncryptionNonce = nonce
	conversation.Preview = fmt.Sprintf("%d encrypted messages captured from %s", len(normalized.Messages), normalized.Platform)
	conversation.CapturedAt = capturedAt
	conversation.LastMessageAt = lastMessageAt
	conversation.Archived = false
	saved, err := s.repo.SaveConversation(conversation)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ArchiveInsights(saved.ID, saved.Revision); err != nil {
		return nil, err
	}

	insights := extractInsights(*saved, normalized)
	workflowIDs := []uuid.UUID{}
	pursuitLinks := []pursuitpkg.AutoLinkResult{}
	warnings := []string{}
	for index := range insights {
		insights[index].ConversationID = saved.ID
		insights[index].Revision = saved.Revision
		stored, errInsight := s.repo.SaveInsight(&insights[index])
		if errInsight != nil {
			warnings = append(warnings, "failed to store "+insights[index].Kind+" insight")
			continue
		}
		insights[index] = *stored
		if s.memoryService != nil && memoryEligible(*stored) {
			memoryRecord, errMemory := memory.CreateForOwner(s.memoryService, saved.OwnerIdentity, memory.CreateRequest{
				ProjectKey:  stored.ProjectKey,
				Kind:        stored.Kind,
				Content:     stored.Text,
				Summary:     stored.Text,
				Tags:        []string{"ai-history", saved.Platform, stored.Kind},
				Confidence:  stored.Confidence,
				SourceURI:   stored.SourceURI,
				SourceLabel: stored.SourceLabel,
			})
			if errMemory != nil {
				warnings = append(warnings, "failed to update searchable memory for "+stored.Kind)
			} else if linkResult, errLink := s.autoLinkPursuitMemory(*saved, *stored, memoryRecord); errLink != nil {
				warnings = append(warnings, "failed to link "+stored.Kind+" memory to pursuit")
			} else if linkResult != nil {
				pursuitLinks = append(pursuitLinks, *linkResult)
			}
		}
		if s.workflowService != nil && workflowEligibleInsight(*stored) {
			requiresReview := workflowInsightRequiresReview(*stored)
			reviewReasonValue := reviewReason(*stored)
			record, errWorkflow := s.intakeWorkflow(workflow.IntakeRequest{
				OwnerIdentity:  saved.OwnerIdentity,
				Input:          stored.Text,
				ProjectKey:     stored.ProjectKey,
				SourceType:     "ai_chat",
				SourceID:       saved.ID.String() + ":" + stored.ID.String(),
				SourceURI:      stored.SourceURI,
				SourceLabel:    stored.SourceLabel,
				ContentType:    "ai_chat_" + stored.Kind,
				Trigger:        "memory_engine.import",
				Actor:          "memory-engine",
				RequiresReview: requiresReview,
				ReviewReason:   reviewReasonValue,
			})
			if errWorkflow != nil {
				if routed, pending := pursuitpkg.IsCandidatePending(errWorkflow); pending {
					if routed != nil && routed.AutoLink != nil {
						pursuitLinks = append(pursuitLinks, *routed.AutoLink)
					}
					warnings = append(warnings, "workflow for "+stored.Kind+" insight awaits explicit pursuit candidate acceptance")
					continue
				}
				if errors.Is(errWorkflow, pursuitpkg.ErrLifecycleRouterRequired) {
					warnings = append(warnings, "workflow for "+stored.Kind+" insight was deferred because the configured pursuit linker is missing the lifecycle router")
					continue
				}
				warnings = append(warnings, "failed to create workflow for "+stored.Kind+" insight")
			} else if record == nil || record.Item.ID == uuid.Nil {
				warnings = append(warnings, "workflow intake for "+stored.Kind+" insight did not return a workflow record")
			} else {
				workflowIDs = append(workflowIDs, record.Item.ID)
				if !s.routesWorkflowThroughPursuits() {
					if linkResult, errLink := s.autoLinkPursuitWorkflow(*saved, *stored, record); errLink != nil {
						warnings = append(warnings, "failed to link "+stored.Kind+" insight workflow to pursuit")
					} else if linkResult != nil {
						pursuitLinks = append(pursuitLinks, *linkResult)
					}
				}
			}
		}
	}
	return &ImportResult{
		Conversation: *saved,
		Insights:     insights,
		WorkflowIDs:  workflowIDs,
		PursuitLinks: pursuitLinks,
		Warnings:     uniqueStrings(warnings),
	}, nil
}

func (s *service) autoLinkPursuitWorkflow(conversation models.AIConversationArchive, insight models.AIMemoryInsight, record *workflow.WorkflowRecord) (*pursuitpkg.AutoLinkResult, error) {
	if s.pursuitLinker == nil || record == nil || record.Item.ID == uuid.Nil {
		return nil, nil
	}
	result, err := s.pursuitLinker.AutoLinkWorkflow(pursuitpkg.AutoLinkWorkflowRequest{
		OwnerIdentity:         conversation.OwnerIdentity,
		WorkflowID:            record.Item.ID,
		Input:                 strings.Join([]string{conversation.Title, insight.Text}, "\n"),
		ProjectKey:            insight.ProjectKey,
		SourceType:            "ai_chat",
		SourceID:              conversation.ID.String() + ":" + insight.ID.String(),
		SourceURI:             insight.SourceURI,
		SourceLabel:           insight.SourceLabel,
		ConversationID:        conversation.ID,
		ConversationSourceURI: conversation.SourceURI,
		ConversationLabel:     conversation.Title,
		Actor:                 "memory-engine",
		AllowCreateCandidate:  true,
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *service) autoLinkPursuitMemory(conversation models.AIConversationArchive, insight models.AIMemoryInsight, memoryRecord *models.ContextMemory) (*pursuitpkg.AutoLinkResult, error) {
	if s.pursuitLinker == nil || memoryRecord == nil || memoryRecord.ID == uuid.Nil {
		return nil, nil
	}
	projectKey := insight.ProjectKey
	if strings.TrimSpace(projectKey) == "" {
		projectKey = memoryRecord.ProjectKey
	}
	sourceURI := insight.SourceURI
	if strings.TrimSpace(sourceURI) == "" {
		sourceURI = memoryRecord.SourceURI
	}
	sourceLabel := insight.SourceLabel
	if strings.TrimSpace(sourceLabel) == "" {
		sourceLabel = memoryRecord.SourceLabel
	}
	result, err := s.pursuitLinker.AutoLinkMemory(pursuitpkg.AutoLinkMemoryRequest{
		OwnerIdentity:         conversation.OwnerIdentity,
		MemoryID:              memoryRecord.ID,
		Input:                 strings.Join([]string{conversation.Title, insight.Text, memoryRecord.Summary}, "\n"),
		ProjectKey:            projectKey,
		SourceURI:             sourceURI,
		SourceLabel:           sourceLabel,
		ConversationID:        conversation.ID,
		ConversationSourceURI: conversation.SourceURI,
		ConversationLabel:     conversation.Title,
		Actor:                 "memory-engine",
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *service) Conversations(limit int) ([]models.AIConversationArchive, error) {
	return s.ConversationsForOwner("", limit)
}

func (s *service) ConversationsForOwner(ownerIdentity string, limit int) ([]models.AIConversationArchive, error) {
	return s.repo.FindConversationsForOwner(strings.TrimSpace(ownerIdentity), limit)
}

func (s *service) Conversation(id uuid.UUID) (*ConversationDetail, error) {
	return s.ConversationForOwner("", id)
}

func (s *service) ConversationForOwner(ownerIdentity string, id uuid.UUID) (*ConversationDetail, error) {
	if len(s.encryptionKey) != 32 {
		return nil, fmt.Errorf("memory encryption key is not configured")
	}
	conversation, err := s.repo.FindConversationByIDForOwner(strings.TrimSpace(ownerIdentity), id)
	if err != nil {
		return nil, err
	}
	raw, err := decryptPayload(s.encryptionKey, conversation.EncryptionNonce, conversation.EncryptedPayload)
	if err != nil {
		return nil, err
	}
	var request ImportRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	return &ConversationDetail{Conversation: *conversation, Messages: request.Messages}, nil
}

func (s *service) DeleteConversation(id uuid.UUID) error {
	return s.DeleteConversationForOwner("", id)
}

func (s *service) DeleteConversationForOwner(ownerIdentity string, id uuid.UUID) error {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	conversation, err := s.repo.FindConversationByIDForOwner(ownerIdentity, id)
	if err != nil {
		return err
	}
	// The configured migration owner may read a legacy ownerless archive, but an
	// authenticated caller must never delete or retract work from it. Only an
	// ownerless in-process maintenance call may operate on that data.
	if ownerIdentity != "" && strings.TrimSpace(conversation.OwnerIdentity) != ownerIdentity {
		return gorm.ErrRecordNotFound
	}
	insights, err := s.repo.FindInsightsForOwner(ownerIdentity, "", "", nil, 500)
	if err != nil {
		return err
	}
	for _, insight := range insights {
		if insight.ConversationID != id || !workflowEligibleInsight(insight) || s.workflowService == nil {
			continue
		}
		sourceID := id.String() + ":" + insight.ID.String()
		if err := s.workflowService.RetractSource("ai_chat", sourceID, "AI conversation archive was deleted"); err != nil {
			return fmt.Errorf("retract derived workflow: %w", err)
		}
	}
	if err := s.repo.DeleteMemoriesBySourceURI(conversation.OwnerIdentity, conversation.SourceURI); err != nil {
		return fmt.Errorf("delete derived memory: %w", err)
	}
	return s.repo.DeleteConversation(id)
}

func (s *service) Insights(kind, projectKey string, needsReview *bool, limit int) ([]models.AIMemoryInsight, error) {
	return s.InsightsForOwner("", kind, projectKey, needsReview, limit)
}

func (s *service) InsightsForOwner(ownerIdentity, kind, projectKey string, needsReview *bool, limit int) ([]models.AIMemoryInsight, error) {
	return s.repo.FindInsightsForOwner(strings.TrimSpace(ownerIdentity), strings.TrimSpace(kind), strings.TrimSpace(projectKey), needsReview, limit)
}

func (s *service) Dashboard() (*CommandDashboard, error) {
	return s.DashboardForOwner("")
}

func (s *service) DashboardForOwner(ownerIdentity string) (*CommandDashboard, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	conversations, err := s.repo.FindConversationsForOwner(ownerIdentity, 50)
	if err != nil {
		return nil, err
	}
	insights, err := s.repo.FindInsightsForOwner(ownerIdentity, "", "", nil, 500)
	if err != nil {
		return nil, err
	}
	workflowDashboard := &workflow.WorkflowDashboard{}
	if ownerIdentity == "" && s.workflowService != nil {
		workflowDashboard, err = s.workflowService.Dashboard()
		if err != nil {
			return nil, err
		}
	}
	result := &CommandDashboard{
		GeneratedAt:       time.Now().UTC(),
		ConversationCount: len(conversations),
		InsightCount:      len(insights),
		NeedsRobert:       append([]models.WorkflowItem{}, workflowDashboard.ApprovalItems...),
		DelegateToVA:      []models.AIMemoryInsight{},
		OpenLoops:         append([]models.WorkflowOpenLoop{}, workflowDashboard.DueOpenLoops...),
		Contradictions:    []models.AIMemoryInsight{},
		RecentDecisions:   []models.AIMemoryInsight{},
		SourceCorrections: []models.ContextMemory{},
		Projects:          []ProjectSummary{},
		RecentArchives:    append([]models.AIConversationArchive{}, conversations...),
		Warnings: []string{
			"Browser capture only reads the current page after an explicit click.",
			"Passwords, tokens, and authorization values are redacted from indexed memory.",
			"AI-extracted actions and uncertain facts remain approval-gated.",
		},
	}
	if ownerIdentity == "" && s.memoryService != nil {
		memories, errMemory := s.memoryService.FindAll("", false)
		if errMemory != nil {
			result.Warnings = append(result.Warnings, "Correction memory review is unavailable: "+errMemory.Error())
		} else {
			result.SourceCorrections = sourceCorrectionMemories(memories)
		}
	}
	if ownerIdentity != "" {
		result.Warnings = append(result.Warnings, "Workflow queues are shown through the owner-scoped Pursuits dashboard until workflow ownership is persisted.")
	}
	projects := map[string]*ProjectSummary{}
	for _, insight := range insights {
		switch insight.Kind {
		case "action":
			if !insight.RobertNeeded && !insight.NeedsReview && insight.RiskLevel == "low" {
				result.DelegateToVA = append(result.DelegateToVA, insight)
			}
		case "contradiction":
			result.Contradictions = append(result.Contradictions, insight)
		case "decision":
			if reusableInsightEligible(insight) {
				result.RecentDecisions = append(result.RecentDecisions, insight)
			}
		}
		if insight.ProjectKey != "" {
			project := projects[insight.ProjectKey]
			if project == nil {
				project = &ProjectSummary{ProjectKey: insight.ProjectKey}
				projects[insight.ProjectKey] = project
			}
			switch insight.Kind {
			case "action":
				project.Actions++
				if insight.Status == "open" {
					project.Open++
				}
			case "decision":
				project.Decisions++
			case "risk", "contradiction":
				project.Risks++
			}
		}
	}
	for _, correction := range result.SourceCorrections {
		if correction.ProjectKey == "" {
			continue
		}
		project := projects[correction.ProjectKey]
		if project == nil {
			project = &ProjectSummary{ProjectKey: correction.ProjectKey}
			projects[correction.ProjectKey] = project
		}
		project.Corrections++
	}
	for _, project := range projects {
		result.Projects = append(result.Projects, *project)
	}
	sort.SliceStable(result.Projects, func(i, j int) bool {
		return result.Projects[i].Open > result.Projects[j].Open
	})
	result.DelegateToVA = limitInsights(result.DelegateToVA, 20)
	result.Contradictions = limitInsights(result.Contradictions, 20)
	result.RecentDecisions = limitInsights(result.RecentDecisions, 20)
	return result, nil
}

func (s *service) Search(query, projectKey string, limit int) (*SearchResult, error) {
	return s.SearchForOwner("", query, projectKey, limit)
}

func (s *service) SearchForOwner(ownerIdentity, query, projectKey string, limit int) (*SearchResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 || limit > 30 {
		limit = 12
	}
	retrieved := &memory.RetrieveResult{
		Query:       query,
		ProjectKey:  projectKey,
		UsedContext: []memory.RankedMemory{},
		Explanation: "Context-memory retrieval is unavailable because the native memory service is not configured.",
	}
	if s.memoryService != nil {
		var err error
		retrieved, err = memory.RetrieveForOwner(s.memoryService, ownerIdentity, memory.RetrieveRequest{
			Query:      query,
			ProjectKey: projectKey,
			Limit:      limit,
		})
		if err != nil {
			return nil, err
		}
		if ownerIdentity != "" {
			retrieved.UsedContext = ownerScopedRankedMemories(retrieved.UsedContext, ownerIdentity)
		}
	}
	all, err := s.repo.FindInsightsForOwner(ownerIdentity, "", projectKey, nil, 500)
	if err != nil {
		return nil, err
	}
	queryTokens := tokenSet(query)
	type scored struct {
		item  models.AIMemoryInsight
		score int
	}
	ranked := []scored{}
	for _, insight := range all {
		if !reusableInsightEligible(insight) {
			continue
		}
		score := tokenOverlap(queryTokens, tokenSet(insight.Text+" "+insight.Kind+" "+insight.ProjectKey))
		if score > 0 {
			ranked = append(ranked, scored{item: insight, score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	facts := []models.AIMemoryInsight{}
	for _, candidate := range ranked {
		facts = append(facts, candidate.item)
		if len(facts) >= limit {
			break
		}
	}
	return &SearchResult{Memory: retrieved, Facts: facts}, nil
}

func sourceCorrectionMemories(memories []models.ContextMemory) []models.ContextMemory {
	result := []models.ContextMemory{}
	for _, item := range memories {
		if item.Archived || !contextMemoryHasTag(item, "source-correction") {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Kind)) {
		case "lesson", "preference", "procedural":
			result = append(result, item)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := result[i]
		right := result[j]
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.Confidence > right.Confidence
	})
	if len(result) > 20 {
		return result[:20]
	}
	return result
}

func contextMemoryHasTag(item models.ContextMemory, tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	for _, value := range strings.Split(item.Tags, ",") {
		if strings.ToLower(strings.TrimSpace(value)) == tag {
			return true
		}
	}
	return false
}

func normalizeImport(request ImportRequest) (ImportRequest, []byte, string, error) {
	request.Platform = strings.ToLower(strings.TrimSpace(request.Platform))
	request.Title = strings.TrimSpace(request.Title)
	request.SourceURI = strings.TrimSpace(request.SourceURI)
	request.ProjectKey = strings.TrimSpace(request.ProjectKey)
	if !supportedPlatform(request.Platform) {
		return request, nil, "", fmt.Errorf("unsupported platform")
	}
	if err := validateSourceURI(request.Platform, request.SourceURI); err != nil {
		return request, nil, "", err
	}
	if len(request.Messages) == 0 || len(request.Messages) > 1000 {
		return request, nil, "", fmt.Errorf("messages must contain between 1 and 1000 entries")
	}
	if request.ExternalID = strings.TrimSpace(request.ExternalID); request.ExternalID == "" {
		request.ExternalID = hashString(request.SourceURI)
	}
	total := 0
	for index := range request.Messages {
		request.Messages[index].Role = normalizeRole(request.Messages[index].Role)
		request.Messages[index].Content = strings.TrimSpace(request.Messages[index].Content)
		total += len(request.Messages[index].Content)
		if request.Messages[index].Content == "" {
			return request, nil, "", fmt.Errorf("message %d has empty content", index+1)
		}
	}
	if total > maxConversationBytes {
		return request, nil, "", fmt.Errorf("conversation exceeds %d byte capture limit", maxConversationBytes)
	}
	if request.Title == "" {
		request.Title = compact(request.Messages[0].Content, 120)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return request, nil, "", err
	}
	return request, raw, hashBytes(raw), nil
}

func extractInsights(conversation models.AIConversationArchive, request ImportRequest) []models.AIMemoryInsight {
	result := []models.AIMemoryInsight{}
	seen := map[string]bool{}
	for _, message := range request.Messages {
		for _, sentence := range splitSentences(safety.RedactSecrets(message.Content)) {
			kind := classifySentence(sentence)
			if kind == "" {
				continue
			}
			key := kind + "|" + strings.ToLower(sentence)
			if seen[key] {
				continue
			}
			seen[key] = true
			risk := riskLevel(sentence)
			owner := insightOwner(sentence)
			robertNeeded := owner == "Robert" || risk != "low" || kind == "decision"
			confidence := 0.78
			needsReview := true
			confidence = 0.55
			result = append(result, models.AIMemoryInsight{
				OwnerIdentity: conversation.OwnerIdentity,
				Kind:          kind,
				Text:          sentence,
				ProjectKey:    request.ProjectKey,
				Owner:         owner,
				RobertNeeded:  robertNeeded,
				RiskLevel:     risk,
				Confidence:    confidence,
				SourceURI:     conversation.SourceURI,
				SourceLabel:   conversation.Platform + ": " + conversation.Title,
				NeedsReview:   needsReview,
				Status:        insightStatusCandidate,
			})
		}
	}
	return limitInsights(result, 100)
}

func classifySentence(sentence string) string {
	lower := strings.ToLower(sentence)
	switch {
	case containsAny(lower, "contradiction", "conflicts with", "inconsistent", "disagrees with"):
		return "contradiction"
	case containsAny(lower, "decided", "decision:", "approved", "rejected", "agreed", "we will"):
		return "decision"
	case containsAny(lower, "risk:", "warning:", "danger", "may break", "could fail", "blocked"):
		return "risk"
	case containsAny(lower, "todo", "to do", "action:", "must ", "need to", "should ", "follow up", "next step", "build ", "create ", "implement "):
		return "action"
	case containsAny(lower, "preference:", "always ", "never ", "rule:", "non-negotiable"):
		return "rule"
	default:
		return ""
	}
}

func riskLevel(value string) string {
	lower := strings.ToLower(value)
	if containsAny(lower, "legal", "lawyer", "government", "financial", "payment", "publish", "public", "delete", "password", "credential", "contract", "insurance") {
		return "high"
	}
	if containsAny(lower, "email", "send", "account", "deadline", "client", "external") {
		return "medium"
	}
	return "low"
}

func insightOwner(value string) string {
	lower := strings.ToLower(value)
	switch {
	case containsAny(lower, "robert", "i need to", "i must"):
		return "Robert"
	case containsAny(lower, "va ", "assistant", "delegate"):
		return "VA"
	case containsAny(lower, "developer", "programmer", "engineer"):
		return "Developer"
	default:
		return "HAI"
	}
}

func reviewReason(insight models.AIMemoryInsight) string {
	reasons := []string{}
	switch insight.Kind {
	case "contradiction":
		reasons = append(reasons, "AI conversation contains conflicting or unsupported information that needs source review")
	case "risk":
		reasons = append(reasons, "AI conversation surfaced a risk that must be assessed before action")
	}
	if insight.RobertNeeded {
		reasons = append(reasons, "extracted action requires Robert")
	}
	if insight.NeedsReview {
		reasons = append(reasons, "extraction confidence or risk requires review")
	}
	return strings.Join(reasons, "; ")
}

func workflowEligibleInsight(insight models.AIMemoryInsight) bool {
	switch strings.ToLower(strings.TrimSpace(insight.Kind)) {
	case "action", "contradiction":
		return true
	case "risk":
		return strings.EqualFold(insight.RiskLevel, "high") || insight.NeedsReview || insight.RobertNeeded
	default:
		return false
	}
}

func workflowInsightRequiresReview(insight models.AIMemoryInsight) bool {
	return insight.NeedsReview ||
		insight.RobertNeeded ||
		strings.EqualFold(insight.Kind, "contradiction") ||
		strings.EqualFold(insight.RiskLevel, "high")
}

func memoryEligible(insight models.AIMemoryInsight) bool {
	return reusableInsightEligible(insight) && insight.Confidence >= 0.65 && insight.Kind != "action"
}

func reusableInsightEligible(insight models.AIMemoryInsight) bool {
	if insight.NeedsReview {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(insight.Status)) {
	case insightStatusVerified, insightStatusSourceSupported, insightStatusOwnerApproved:
		return true
	default:
		return false
	}
}

func ownerScopedRankedMemories(values []memory.RankedMemory, ownerIdentity string) []memory.RankedMemory {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return values
	}
	result := make([]memory.RankedMemory, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.Memory.OwnerIdentity) == ownerIdentity {
			result = append(result, value)
		}
	}
	return result
}

func supportedPlatform(platform string) bool {
	switch platform {
	case "chatgpt", "gemini", "copilot", "deepseek":
		return true
	default:
		return false
	}
}

func validateSourceURI(platform, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("sourceUri must be a valid https URL")
	}
	host := strings.ToLower(parsed.Hostname())
	allowed := map[string][]string{
		"chatgpt":  {"chatgpt.com", "chat.openai.com"},
		"gemini":   {"gemini.google.com"},
		"copilot":  {"copilot.microsoft.com"},
		"deepseek": {"chat.deepseek.com"},
	}
	for _, candidate := range allowed[platform] {
		if host == candidate {
			return nil
		}
	}
	return fmt.Errorf("sourceUri host does not match platform")
}

func encryptPayload(key, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nonce, nil
}

func decryptPayload(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func filterConversationInsights(values []models.AIMemoryInsight, conversationID uuid.UUID, revision int) []models.AIMemoryInsight {
	result := []models.AIMemoryInsight{}
	for _, value := range values {
		if value.ConversationID == conversationID && value.Revision == revision {
			result = append(result, value)
		}
	}
	return result
}

func splitSentences(value string) []string {
	value = strings.NewReplacer("\r", "\n", ";", ". ", "\n", ". ").Replace(value)
	result := []string{}
	for _, part := range strings.Split(value, ".") {
		part = strings.TrimSpace(part)
		if len(part) >= 8 {
			result = append(result, compact(part, 500))
		}
	}
	return result
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant", "ai", "model":
		return "assistant"
	case "system":
		return "system"
	default:
		return "user"
	}
}

func parseOptionalTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed := parseTime(value, time.Time{})
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func parseTime(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return fallback
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hashString(value string) string {
	return hashBytes([]byte(strings.TrimSpace(value)))
}

func compact(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func tokenSet(value string) map[string]bool {
	result := map[string]bool{}
	value = strings.ToLower(value)
	value = strings.NewReplacer(",", " ", ".", " ", ";", " ", ":", " ", "/", " ", "\\", " ", "\n", " ").Replace(value)
	for _, token := range strings.Fields(value) {
		if len(token) >= 3 {
			result[token] = true
		}
	}
	return result
}

func tokenOverlap(left, right map[string]bool) int {
	score := 0
	for token := range left {
		if right[token] {
			score++
		}
	}
	return score
}

func limitInsights(values []models.AIMemoryInsight, limit int) []models.AIMemoryInsight {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
