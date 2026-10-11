package frameworkregistry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	frameworkCatalogVersion           = "v2"
	frameworkSelectorAlgorithmVersion = "selector-v5"
)

type Repository interface {
	ListPreferences(owner string) ([]Preference, error)
	UpsertPreference(owner string, preference Preference) (*Preference, error)
	CreateSelection(owner string, decision SelectionDecision, requestHash, requestSummary string) error
	GetSelection(context.Context, string, string) (*SelectionDecision, error)
	ListSelections(owner string, limit int) ([]SelectionDecision, error)
	ListConstitutions(owner string) ([]Constitution, error)
	ListConstitutionHistory(owner string, limit int) ([]Constitution, error)
	CreateConstitution(owner string, constitution Constitution) (*Constitution, error)
	ActivateConstitution(owner, id, approvedBy, approvalNote string, approvedAt time.Time) (*Constitution, error)
}

type Service struct {
	catalog   []Framework
	lifecycle *frameworkCatalogLifecycle
	repo      Repository
	now       func() time.Time
}

func NewService(repo Repository) (*Service, error) {
	return newServiceWithCatalog(repo, BuiltinCatalog(), time.Now)
}

func DefaultService() (*Service, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return NewService(NewGormRepository(db))
}

func (s *Service) Overview(owner string) (*Overview, error) {
	views, err := s.List(owner)
	if err != nil {
		return nil, err
	}
	constitution, source, err := s.ActiveConstitution(owner)
	if err != nil {
		return nil, err
	}
	selections, err := s.Selections(owner, 20)
	if err != nil {
		return nil, err
	}
	result := &Overview{
		GeneratedAt:         s.now().UTC(),
		Total:               len(views),
		Families:            map[string]int{},
		ConstitutionVersion: constitution.Version,
		ConstitutionSource:  source,
		RecentSelections:    len(selections),
		SelectionContract: []string{
			"classify life domain and need or commitment",
			"select the deterministic smallest non-conflicting framework combination that covers every required capability",
			"derive specialist agents, evidence, authority ceiling, and approval requirements",
			"verify completion before learning or external action",
		},
	}
	for _, view := range views {
		result.Families[view.Family]++
		if view.Enabled {
			result.Enabled++
		}
		if view.Status == StatusExperimental {
			result.Experimental++
		}
		if view.Status == StatusDeprecated {
			result.Deprecated++
		}
		if view.Pinned {
			result.Pinned++
		}
	}
	return result, nil
}

func (s *Service) List(owner string) ([]FrameworkView, error) {
	preferences, err := s.repo.ListPreferences(strings.TrimSpace(owner))
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Preference, len(preferences))
	for _, preference := range preferences {
		byID[preference.FrameworkID] = preference
	}
	catalog := s.activeCatalogSnapshot()
	result := make([]FrameworkView, 0, len(catalog))
	for _, framework := range catalog {
		result = append(result, applyPreference(framework, byID[framework.ID]))
	}
	return result, nil
}

func (s *Service) Get(owner, id string) (*FrameworkView, error) {
	id = strings.TrimSpace(id)
	views, err := s.List(owner)
	if err != nil {
		return nil, err
	}
	for _, view := range views {
		if view.ID == id {
			copied := view
			return &copied, nil
		}
	}
	return nil, fmt.Errorf("framework not found")
}

func applyPreference(framework Framework, preference Preference) FrameworkView {
	framework = cloneFramework(framework)
	// The HTTP contract exposes collection fields as arrays. A nil conflict or
	// adaptation set means "none", but encoding it as JSON null makes strict
	// clients reject the complete catalogue. Keep the internal catalogue
	// semantics while presenting stable empty collections at the view boundary.
	framework.ConflictsWith = append([]string{}, framework.ConflictsWith...)
	enabled := framework.Status == StatusActive
	switch preference.State {
	case PreferenceEnabled:
		enabled = true
	case PreferenceDisabled:
		enabled = false
	}
	maxAutonomy := framework.MaximumAutonomyLevel
	if preference.MaximumAutonomyLevel != nil && *preference.MaximumAutonomyLevel < maxAutonomy {
		maxAutonomy = *preference.MaximumAutonomyLevel
	}
	view := FrameworkView{
		Framework:              framework,
		EffectiveStatus:        framework.Status,
		Enabled:                enabled,
		Pinned:                 preference.Pinned,
		EffectiveAutonomyLevel: maxAutonomy,
		Adaptations:            append([]string{}, preference.Adaptations...),
	}
	if !preference.UpdatedAt.IsZero() {
		updated := preference.UpdatedAt
		view.PreferenceUpdatedAt = &updated
	}
	return view
}

func (s *Service) UpdatePreference(owner, frameworkID string, patch PreferencePatch) (*FrameworkView, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	view, err := s.Get(owner, frameworkID)
	if err != nil {
		return nil, err
	}
	preferences, err := s.repo.ListPreferences(owner)
	if err != nil {
		return nil, err
	}
	existing := Preference{FrameworkID: view.ID, State: PreferenceDefault}
	for _, candidate := range preferences {
		if candidate.FrameworkID == view.ID {
			existing = candidate
			break
		}
	}
	state := strings.ToLower(strings.TrimSpace(patch.State))
	if state == "" {
		state = existing.State
		if state == "" {
			state = PreferenceDefault
		}
	}
	if state != PreferenceDefault && state != PreferenceEnabled && state != PreferenceDisabled {
		return nil, fmt.Errorf("invalid preference state")
	}
	if state == PreferenceDisabled && isProtectedMandatoryFramework(view.ID) {
		return nil, fmt.Errorf("framework %q is a protected safety overlay and cannot be disabled", view.ID)
	}
	maxAutonomy := existing.MaximumAutonomyLevel
	if patch.MaximumAutonomyLevel != nil {
		maxAutonomy = patch.MaximumAutonomyLevel
		if *maxAutonomy < 0 || *maxAutonomy > view.MaximumAutonomyLevel {
			return nil, fmt.Errorf("autonomy override may only lower the built-in ceiling of %d", view.MaximumAutonomyLevel)
		}
	}
	if patch.ClearAutonomyOverride {
		maxAutonomy = nil
	}
	adaptations := existing.Adaptations
	if patch.Adaptations != nil {
		adaptations, err = sanitizeAdaptations(patch.Adaptations)
		if err != nil {
			return nil, err
		}
	}
	pinned := existing.Pinned
	if patch.Pinned != nil {
		pinned = *patch.Pinned
	}
	preference, err := s.repo.UpsertPreference(owner, Preference{
		FrameworkID:          view.ID,
		State:                state,
		Pinned:               pinned,
		MaximumAutonomyLevel: maxAutonomy,
		Adaptations:          adaptations,
		UpdatedAt:            s.now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	updated := applyPreference(view.Framework, *preference)
	return &updated, nil
}

func isProtectedMandatoryFramework(id string) bool {
	switch strings.TrimSpace(id) {
	case "human-sovereignty",
		"intake-triage",
		"autonomy-levels",
		"approval-control",
		"truth-evidence",
		"privacy-protection",
		"security-zero-trust",
		"agent-threat-modeling",
		"reliable-execution",
		"evaluation":
		return true
	default:
		return false
	}
}

func sanitizeAdaptations(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, fmt.Errorf("a framework may have at most 20 adaptations")
	}
	result := make([]string, 0, len(values))
	for _, value := range uniqueStrings(values) {
		value = strings.Join(strings.Fields(strings.TrimSpace(safety.RedactSecrets(value))), " ")
		lower := strings.ToLower(value)
		for _, forbidden := range []string{
			"ignore approval", "bypass approval", "disable safety", "disable policy",
			"override constitution", "ignore constitution", "disable emergency stop",
			"raise autonomy", "grant authority", "reveal secret",
		} {
			if strings.Contains(lower, forbidden) {
				return nil, fmt.Errorf("adaptations cannot modify protected authority or safety rules")
			}
		}
		if len([]rune(value)) > 500 {
			return nil, fmt.Errorf("adaptations may contain at most 500 characters")
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Service) Select(request SelectionRequest) (*SelectionDecision, error) {
	request.OwnerIdentity = strings.TrimSpace(request.OwnerIdentity)
	if request.OwnerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	return s.planSelection(request, true)
}

// PlanSelection is the in-process task-engine boundary. Authenticated work is
// audited through the owner-scoped repository; controlled system work may use
// the same deterministic selector without inventing a user identity.
func (s *Service) PlanSelection(request SelectionRequest) (*SelectionDecision, error) {
	return s.planSelection(request, strings.TrimSpace(request.OwnerIdentity) != "")
}

func (s *Service) planSelection(request SelectionRequest, persist bool) (*SelectionDecision, error) {
	request.Request = strings.TrimSpace(request.Request)
	if err := validateSelectionRequest(request); err != nil {
		return nil, err
	}
	request.SuccessCriteria = redactContractStrings(request.SuccessCriteria)
	views, err := s.List(request.OwnerIdentity)
	if err != nil {
		return nil, err
	}
	constitution, source, err := s.ActiveConstitution(request.OwnerIdentity)
	if err != nil {
		return nil, err
	}
	decision, err := BuildSelection(views, constitution, request, s.now().UTC())
	if err != nil {
		return nil, err
	}
	decision.ConstitutionSource = source
	metadata, err := s.selectionReproducibilityMetadata(views, constitution)
	if err != nil {
		return nil, err
	}
	decision.CatalogVersion = metadata.catalogVersion
	decision.CatalogDigest = metadata.catalogDigest
	decision.SelectorAlgorithmVersion = metadata.selectorAlgorithmVersion
	decision.EffectivePreferenceDigest = metadata.effectivePreferenceDigest
	decision.ConstitutionDigest = metadata.constitutionDigest
	decision.ID = deterministicSelectionID(decision, request)
	if persist {
		requestHash, summary := selectionRequestAudit(request)
		if err := s.repo.CreateSelection(request.OwnerIdentity, decision, requestHash, summary); err != nil {
			return nil, err
		}
	}
	return &decision, nil
}

func redactContractStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Join(strings.Fields(strings.TrimSpace(safety.RedactSecrets(value))), " ")
		if value != "" {
			result = append(result, value)
		}
	}
	return uniqueStrings(result)
}

type selectionReproducibilityMetadata struct {
	catalogVersion            string
	catalogDigest             string
	selectorAlgorithmVersion  string
	effectivePreferenceDigest string
	constitutionDigest        string
}

type effectivePreferenceFingerprint struct {
	FrameworkID          string   `json:"frameworkId"`
	Enabled              bool     `json:"enabled"`
	Pinned               bool     `json:"pinned"`
	MaximumAutonomyLevel int      `json:"maximumAutonomyLevel"`
	Adaptations          []string `json:"adaptations"`
}

type constitutionFingerprint struct {
	ID                  string             `json:"id"`
	Version             int                `json:"version"`
	BaseVersion         int                `json:"baseVersion"`
	Status              string             `json:"status"`
	Values              []string           `json:"values"`
	Prohibitions        []string           `json:"prohibitions"`
	StandingPermissions []string           `json:"standingPermissions"`
	Preferences         []string           `json:"preferences"`
	RelationshipRules   []string           `json:"relationshipRules"`
	FinancialBoundaries []string           `json:"financialBoundaries"`
	CommunicationRules  []string           `json:"communicationRules"`
	EscalationRules     []string           `json:"escalationRules"`
	ProtectedRules      []string           `json:"protectedRules"`
	EffectiveRules      []constitutionRule `json:"effectiveRules"`
}

type constitutionVersionFingerprint struct {
	ID                  string   `json:"id"`
	Version             int      `json:"version"`
	BaseVersion         int      `json:"baseVersion"`
	Values              []string `json:"values"`
	Prohibitions        []string `json:"prohibitions"`
	StandingPermissions []string `json:"standingPermissions"`
	Preferences         []string `json:"preferences"`
	RelationshipRules   []string `json:"relationshipRules"`
	FinancialBoundaries []string `json:"financialBoundaries"`
	CommunicationRules  []string `json:"communicationRules"`
	EscalationRules     []string `json:"escalationRules"`
	ProtectedRules      []string `json:"protectedRules"`
	ChangeSummary       string   `json:"changeSummary"`
}

func (s *Service) selectionReproducibilityMetadata(
	views []FrameworkView,
	constitution Constitution,
) (selectionReproducibilityMetadata, error) {
	catalog := s.activeCatalogSnapshot()
	sort.SliceStable(catalog, func(i, j int) bool {
		return catalog[i].ID < catalog[j].ID
	})
	catalogDigest, err := canonicalSHA256(catalog)
	if err != nil {
		return selectionReproducibilityMetadata{}, fmt.Errorf("digest framework catalog: %w", err)
	}

	preferences := make([]effectivePreferenceFingerprint, 0, len(views))
	for _, view := range views {
		adaptations := append([]string(nil), view.Adaptations...)
		sort.Strings(adaptations)
		preferences = append(preferences, effectivePreferenceFingerprint{
			FrameworkID:          view.ID,
			Enabled:              view.Enabled,
			Pinned:               view.Pinned,
			MaximumAutonomyLevel: view.EffectiveAutonomyLevel,
			Adaptations:          adaptations,
		})
	}
	sort.SliceStable(preferences, func(i, j int) bool {
		return preferences[i].FrameworkID < preferences[j].FrameworkID
	})
	preferenceDigest, err := canonicalSHA256(preferences)
	if err != nil {
		return selectionReproducibilityMetadata{}, fmt.Errorf("digest effective preferences: %w", err)
	}

	constitutionDigest, err := constitutionReproducibilityDigest(constitution)
	if err != nil {
		return selectionReproducibilityMetadata{}, err
	}

	return selectionReproducibilityMetadata{
		catalogVersion:            frameworkCatalogVersion,
		catalogDigest:             catalogDigest,
		selectorAlgorithmVersion:  frameworkSelectorAlgorithmVersion,
		effectivePreferenceDigest: preferenceDigest,
		constitutionDigest:        constitutionDigest,
	}, nil
}

func constitutionReproducibilityDigest(constitution Constitution) (string, error) {
	effectiveRules, err := compileEffectiveConstitutionRules(constitution)
	if err != nil {
		return "", fmt.Errorf("compile effective Constitution rules: %w", err)
	}
	digest, err := canonicalSHA256(constitutionFingerprint{
		ID:                  strings.TrimSpace(constitution.ID),
		Version:             constitution.Version,
		BaseVersion:         constitution.BaseVersion,
		Status:              strings.TrimSpace(constitution.Status),
		Values:              append([]string(nil), constitution.Values...),
		Prohibitions:        append([]string(nil), constitution.Prohibitions...),
		StandingPermissions: append([]string(nil), constitution.StandingPermissions...),
		Preferences:         append([]string(nil), constitution.Preferences...),
		RelationshipRules:   append([]string(nil), constitution.RelationshipRules...),
		FinancialBoundaries: append([]string(nil), constitution.FinancialBoundaries...),
		CommunicationRules:  append([]string(nil), constitution.CommunicationRules...),
		EscalationRules:     append([]string(nil), constitution.EscalationRules...),
		ProtectedRules:      append([]string(nil), constitution.ProtectedRules...),
		EffectiveRules:      append([]constitutionRule(nil), effectiveRules.Rules...),
	})
	if err != nil {
		return "", fmt.Errorf("digest Constitution: %w", err)
	}
	return digest, nil
}

func canonicalSHA256(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

func validateSelectionRequest(request SelectionRequest) error {
	if strings.TrimSpace(request.Request) == "" {
		return fmt.Errorf("request is required")
	}
	if len([]rune(request.Request)) > 20000 {
		return fmt.Errorf("request exceeds the 20000 character planning limit")
	}
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{name: "task plan id", value: request.TaskPlanID, limit: 160},
		{name: "project key", value: request.ProjectKey, limit: 255},
		{name: "pursuit id", value: request.PursuitID, limit: 160},
		{name: "task type", value: request.TaskType, limit: 255},
		{name: "risk level", value: request.RiskLevel, limit: 32},
		{name: "required reasoning", value: request.RequiredReasoning, limit: 255},
	} {
		if len([]rune(strings.TrimSpace(field.value))) > field.limit {
			return fmt.Errorf("%s exceeds %d characters", field.name, field.limit)
		}
	}
	riskLevel := strings.ToLower(strings.TrimSpace(request.RiskLevel))
	if riskLevel != "" && riskLevel != "low" && riskLevel != "medium" && riskLevel != "high" {
		return fmt.Errorf("invalid risk level")
	}
	if request.Difficulty < 0 || request.Difficulty > 10 {
		return fmt.Errorf("difficulty must be between 0 and 10")
	}
	if len(request.SuccessCriteria) > 50 {
		return fmt.Errorf("success criteria may contain at most 50 items")
	}
	for _, criterion := range request.SuccessCriteria {
		if len([]rune(strings.TrimSpace(criterion))) > 1000 {
			return fmt.Errorf("success criterion exceeds 1000 characters")
		}
	}
	return nil
}

func selectionRequestAudit(request SelectionRequest) (string, string) {
	operatingInputs, _ := json.Marshal(struct {
		ObservedNeeds             []NeedStateAssessment `json:"observedNeeds"`
		Capacity                  *CapacitySnapshot     `json:"capacity"`
		AvailableAgents           []AgentCard           `json:"availableAgents"`
		PreferredCoordinationMode string                `json:"preferredCoordinationMode"`
		Deadline                  *time.Time            `json:"deadline"`
	}{
		ObservedNeeds:             request.ObservedNeeds,
		Capacity:                  request.Capacity,
		AvailableAgents:           request.AvailableAgents,
		PreferredCoordinationMode: request.PreferredCoordinationMode,
		Deadline:                  request.Deadline,
	})
	operatingInputHash := sha256.Sum256(operatingInputs)
	normalized := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(request.OwnerIdentity)),
		strings.ToLower(strings.Join(strings.Fields(request.Request), " ")),
		strings.ToLower(strings.TrimSpace(request.ProjectKey)),
		strings.ToLower(strings.TrimSpace(request.PursuitID)),
		strings.ToLower(strings.TrimSpace(request.TaskType)),
		strings.ToLower(strings.TrimSpace(request.RiskLevel)),
		fmt.Sprintf("difficulty=%d", request.Difficulty),
		strings.ToLower(strings.TrimSpace(request.RequiredReasoning)),
		strings.ToLower(strings.Join(request.SuccessCriteria, "\x1f")),
		fmt.Sprintf(
			"memory=%t;tools=%t;documents=%t;web=%t;local=%t;approval=%t;execute=%t;approved=%t",
			request.NeedsMemory,
			request.NeedsTools,
			request.NeedsDocuments,
			request.NeedsWebAccess,
			request.NeedsLocalExecution,
			request.NeedsApproval,
			request.ExecuteRequested,
			request.HumanApproved,
		),
		fmt.Sprintf("operating_inputs=%x", operatingInputHash[:]),
	}, "\n")
	sum := sha256.Sum256([]byte(normalized))
	flags := make([]string, 0, 8)
	if request.NeedsMemory {
		flags = append(flags, "memory")
	}
	if request.NeedsTools {
		flags = append(flags, "tools")
	}
	if request.NeedsDocuments {
		flags = append(flags, "documents")
	}
	if request.NeedsWebAccess {
		flags = append(flags, "web")
	}
	if request.NeedsLocalExecution {
		flags = append(flags, "local_execution")
	}
	if request.NeedsApproval {
		flags = append(flags, "approval_hint")
	}
	if request.ExecuteRequested {
		flags = append(flags, "execution_requested")
	}
	if len(flags) == 0 {
		flags = append(flags, "planning_only")
	}
	summary := fmt.Sprintf(
		"intent=%s; difficulty=%d; success_criteria=%d",
		strings.Join(flags, ","),
		request.Difficulty,
		len(request.SuccessCriteria),
	)
	return fmt.Sprintf("%x", sum[:]), summary
}

func (s *Service) Selections(owner string, limit int) ([]SelectionDecision, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.ListSelections(strings.TrimSpace(owner), limit)
}

// Selection returns one immutable, owner-scoped framework selection. This
// exact lookup is used by execution authorization; bounded history scans are
// not a safe substitute because older valid selections may fall outside the
// visible history window.
func (s *Service) Selection(
	ctx context.Context,
	owner,
	id string,
) (*SelectionDecision, error) {
	owner = strings.TrimSpace(owner)
	id = strings.TrimSpace(id)
	if owner == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if id == "" {
		return nil, fmt.Errorf("framework selection id is required")
	}
	return s.repo.GetSelection(ctx, owner, id)
}

func (s *Service) ConstitutionHistory(owner string, limit int) (*ConstitutionHistoryPage, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	limit = boundedConstitutionHistoryLimit(limit)
	records, err := s.repo.ListConstitutionHistory(owner, limit+1)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Version != records[j].Version {
			return records[i].Version > records[j].Version
		}
		if !records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].CreatedAt.After(records[j].CreatedAt)
		}
		return records[i].ID < records[j].ID
	})

	truncated := len(records) > limit
	if truncated {
		records = records[:limit]
	}
	history := make([]ConstitutionHistoryEntry, 0, len(records))
	for _, record := range records {
		if !isConstitutionLifecycleStatus(record.Status) {
			return nil, fmt.Errorf("invalid Constitution lifecycle status")
		}
		digest, err := constitutionVersionDigest(record)
		if err != nil {
			return nil, fmt.Errorf("digest Constitution version %d: %w", record.Version, err)
		}
		history = append(history, ConstitutionHistoryEntry{
			ID:            record.ID,
			Version:       record.Version,
			BaseVersion:   record.BaseVersion,
			Status:        record.Status,
			ChangeSummary: record.ChangeSummary,
			ApprovedBy:    record.ApprovedBy,
			ApprovedAt:    cloneTimePointer(record.ApprovedAt),
			CreatedAt:     record.CreatedAt,
			Digest:        digest,
		})
	}
	return &ConstitutionHistoryPage{
		History:   history,
		Limit:     limit,
		Truncated: truncated,
	}, nil
}

func constitutionVersionDigest(record Constitution) (string, error) {
	return canonicalSHA256(constitutionVersionFingerprint{
		ID:                  strings.TrimSpace(record.ID),
		Version:             record.Version,
		BaseVersion:         record.BaseVersion,
		Values:              append([]string(nil), record.Values...),
		Prohibitions:        append([]string(nil), record.Prohibitions...),
		StandingPermissions: append([]string(nil), record.StandingPermissions...),
		Preferences:         append([]string(nil), record.Preferences...),
		RelationshipRules:   append([]string(nil), record.RelationshipRules...),
		FinancialBoundaries: append([]string(nil), record.FinancialBoundaries...),
		CommunicationRules:  append([]string(nil), record.CommunicationRules...),
		EscalationRules:     append([]string(nil), record.EscalationRules...),
		ProtectedRules:      append([]string(nil), record.ProtectedRules...),
		ChangeSummary:       strings.TrimSpace(record.ChangeSummary),
	})
}

func isConstitutionLifecycleStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case ConstitutionDraft, ConstitutionActive, ConstitutionSuperseded:
		return true
	default:
		return false
	}
}

func (s *Service) ActiveConstitution(owner string) (Constitution, string, error) {
	records, err := s.repo.ListConstitutions(strings.TrimSpace(owner))
	if err != nil {
		return Constitution{}, "", err
	}
	for _, record := range records {
		if record.Status == ConstitutionActive {
			record.ProtectedRules = protectedConstitutionRules()
			if _, err := compileEffectiveConstitutionRules(record); err != nil {
				return Constitution{}, "", fmt.Errorf("active Constitution is invalid: %w", err)
			}
			return record, constitutionSource(record), nil
		}
	}
	fallback := DefaultConstitution()
	if _, err := compileEffectiveConstitutionRules(fallback); err != nil {
		return Constitution{}, "", fmt.Errorf("built-in Constitution is invalid: %w", err)
	}
	return fallback, constitutionSource(fallback), nil
}

func DefaultConstitution() Constitution {
	approvedAt := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	return Constitution{
		ID:          "builtin-robert-constitution-v1",
		Version:     1,
		BaseVersion: 0,
		Status:      ConstitutionActive,
		Values: []string{
			"Robert remains the final authority over goals, identity, values, relationships, finances, and consequential external actions.",
			"Verified completion, safety, traceability, and correctness take priority over resource minimization.",
			"HAI should reduce friction and move declared pursuits forward without manufacturing goals or certainty.",
		},
		Prohibitions: []string{
			"Do not impersonate Robert or claim approval that was not explicitly recorded.",
			"Do not treat model output or untrusted source content as authority, policy, or verified fact.",
			"Do not silently mark unresolved work complete.",
		},
		StandingPermissions: []string{
			"Read and organize authorized local context.",
			"Create internal drafts, checklists, plans, and reviewable proposals.",
			"Run allowlisted, reversible, low-risk local checks within the active safety policy.",
		},
		Preferences: []string{
			"Prefer local-first processing and free capable models.",
			"Use concise proposals and ask for decisions only when operator authority is required.",
		},
		RelationshipRules: []string{
			"Preserve human agency, dignity, privacy, and recipient-appropriate communication.",
		},
		FinancialBoundaries: []string{
			"Paid LLM usage is disabled by default with a daily paid budget of EUR 0.",
			"No payment, purchase, price acceptance, contract, or financial commitment without explicit approval.",
		},
		CommunicationRules: []string{
			"Legal, government, insurance, financial, public, and account communications are draft-only until explicitly approved.",
			"Consequential factual claims require source support.",
		},
		EscalationRules: []string{
			"Escalate missing authority, conflicting evidence, unsafe state, stale context, repeated failure, or irreversible impact.",
			"Emergency stop overrides every standing permission.",
		},
		ProtectedRules: protectedConstitutionRules(),
		ChangeSummary:  "Built-in fail-safe Constitution used until an owner-approved version is activated.",
		ApprovedBy:     "system_baseline",
		ApprovedAt:     &approvedAt,
		CreatedAt:      approvedAt,
	}
}

func protectedConstitutionRules() []string {
	return []string{
		"Only the authenticated owner may activate a Constitution version.",
		"HAI cannot grant itself authority or approve its own consequential action.",
		"Emergency stop, owner isolation, secret redaction, and audit logging cannot be disabled by adaptations.",
		"High-risk external, legal, government, financial, account, destructive, or public actions require explicit scoped approval.",
		"Unsupported or uncertain consequential claims cannot become facts, memory, or action triggers.",
	}
}

func (s *Service) CreateConstitutionDraft(owner string, request ConstitutionDraftRequest) (*Constitution, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	active, _, err := s.ActiveConstitution(owner)
	if err != nil {
		return nil, err
	}
	if request.BaseVersion != 0 && request.BaseVersion != active.Version {
		return nil, fmt.Errorf("base Constitution version is stale; current version is %d", active.Version)
	}
	if strings.TrimSpace(request.ChangeSummary) == "" {
		return nil, fmt.Errorf("change summary is required")
	}
	records, err := s.repo.ListConstitutions(owner)
	if err != nil {
		return nil, err
	}
	nextVersion := active.Version + 1
	for _, record := range records {
		if record.Version >= nextVersion {
			nextVersion = record.Version + 1
		}
	}
	record := Constitution{
		ID:                  uuid.NewString(),
		Version:             nextVersion,
		BaseVersion:         active.Version,
		Status:              ConstitutionDraft,
		Values:              sanitizeConstitutionItems(request.Values, active.Values),
		Prohibitions:        sanitizeConstitutionItems(request.Prohibitions, active.Prohibitions),
		StandingPermissions: sanitizeConstitutionItems(request.StandingPermissions, active.StandingPermissions),
		Preferences:         sanitizeConstitutionItems(request.Preferences, active.Preferences),
		RelationshipRules:   sanitizeConstitutionItems(request.RelationshipRules, active.RelationshipRules),
		FinancialBoundaries: sanitizeConstitutionItems(request.FinancialBoundaries, active.FinancialBoundaries),
		CommunicationRules:  sanitizeConstitutionItems(request.CommunicationRules, active.CommunicationRules),
		EscalationRules:     sanitizeConstitutionItems(request.EscalationRules, active.EscalationRules),
		ProtectedRules:      protectedConstitutionRules(),
		ChangeSummary:       strings.Join(strings.Fields(safety.RedactSecrets(request.ChangeSummary)), " "),
		CreatedAt:           s.now().UTC(),
	}
	if err := validateConstitutionDraft(record); err != nil {
		return nil, err
	}
	return s.repo.CreateConstitution(owner, record)
}

func validateConstitutionDraft(record Constitution) error {
	if _, err := compileEffectiveConstitutionRules(record); err != nil {
		return err
	}
	all := append([]string{}, record.Values...)
	all = append(all, record.Prohibitions...)
	all = append(all, record.StandingPermissions...)
	all = append(all, record.Preferences...)
	all = append(all, record.RelationshipRules...)
	all = append(all, record.FinancialBoundaries...)
	all = append(all, record.CommunicationRules...)
	all = append(all, record.EscalationRules...)
	for _, value := range all {
		lower := strings.ToLower(value)
		for _, forbidden := range []string{
			"disable emergency stop",
			"bypass approval",
			"ignore approval",
			"share secrets",
			"reveal secrets",
			"cross-user",
			"mark unsupported claims as verified",
			"self-approve",
		} {
			if strings.Contains(lower, forbidden) {
				return fmt.Errorf("Constitution draft conflicts with a protected rule")
			}
		}
	}
	return nil
}

func sanitizeConstitutionItems(values, fallback []string) []string {
	if len(values) == 0 {
		values = fallback
	}
	result := make([]string, 0, len(values))
	for _, value := range uniqueStrings(values) {
		value = strings.Join(strings.Fields(safety.RedactSecrets(value)), " ")
		if value == "" {
			continue
		}
		runes := []rune(value)
		if len(runes) > 1000 {
			value = string(runes[:1000])
		}
		result = append(result, value)
		if len(result) == 50 {
			break
		}
	}
	return result
}

func (s *Service) ActivateConstitution(owner, id, actor string, request ActivateConstitutionRequest) (*Constitution, error) {
	owner = strings.TrimSpace(owner)
	id = strings.TrimSpace(id)
	actor = strings.TrimSpace(actor)
	if owner == "" || actor == "" {
		return nil, fmt.Errorf("authenticated owner approval is required")
	}
	if actor != owner {
		return nil, fmt.Errorf("only the authenticated owner may activate a Constitution version")
	}
	if request.Confirmation != "ACTIVATE CONSTITUTION" {
		return nil, fmt.Errorf("exact confirmation ACTIVATE CONSTITUTION is required")
	}
	approvalNote := compactRedactedText(request.ApprovalNote, maxApprovalNoteRunes)
	if len([]rune(approvalNote)) < 10 {
		return nil, fmt.Errorf("an approval note of at least 10 characters is required")
	}
	records, err := s.repo.ListConstitutions(owner)
	if err != nil {
		return nil, err
	}
	var target *Constitution
	for index := range records {
		if strings.TrimSpace(records[index].ID) == id {
			candidate := records[index]
			target = &candidate
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("Constitution version not found")
	}
	active, _, err := s.ActiveConstitution(owner)
	if err != nil {
		return nil, err
	}
	if target.Status != ConstitutionActive && target.BaseVersion != active.Version {
		return nil, fmt.Errorf(
			"Constitution draft is stale; it is based on version %d while version %d is active",
			target.BaseVersion,
			active.Version,
		)
	}
	if err := validateConstitutionDraft(*target); err != nil {
		return nil, fmt.Errorf("Constitution cannot be activated: %w", err)
	}
	record, err := s.repo.ActivateConstitution(owner, id, actor, approvalNote, s.now().UTC())
	if err != nil {
		return nil, err
	}
	record.ProtectedRules = protectedConstitutionRules()
	return record, nil
}
