package autonomy

import (
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	InterfaceSkillCall   = "skill_call"
	InterfaceToolCall    = "tool_call"
	InterfaceActionChunk = "action_chunk"
)

type ActionEnvelope struct {
	InterfaceType    string         `json:"interfaceType"`
	ActionType       string         `json:"actionType"`
	Arguments        map[string]any `json:"arguments,omitempty"`
	RequiresApproval bool           `json:"requiresApproval"`
	ApprovalRecorded bool           `json:"approvalRecorded"`
	ObservationTime  time.Time      `json:"observationTime"`
	StaleAfter       time.Time      `json:"staleAfter"`
	UntrustedInput   bool           `json:"untrustedInput"`
	PolicyOverride   bool           `json:"policyOverride"`
}

type StressCaseResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type Metrics struct {
	Attempts              int     `json:"attempts"`
	RawCompletions        int     `json:"rawCompletions"`
	CompletionUnderPolicy int     `json:"completionUnderPolicy"`
	PolicyViolations      int     `json:"policyViolations"`
	InvalidActions        int     `json:"invalidActions"`
	HumanInterventions    int     `json:"humanInterventions"`
	RecoveryAttempts      int     `json:"recoveryAttempts"`
	Recovered             int     `json:"recovered"`
	AverageLatencyMillis  float64 `json:"averageLatencyMillis"`
	RawCompletionRate     float64 `json:"rawCompletionRate"`
	PolicyCompletionRate  float64 `json:"policyCompletionRate"`
	InterventionRate      float64 `json:"interventionRate"`
	RecoveryRate          float64 `json:"recoveryRate"`
}

type Overview struct {
	GeneratedAt        time.Time                    `json:"generatedAt"`
	Metrics            Metrics                      `json:"metrics"`
	RecentWorldStates  []models.AutonomyWorldState  `json:"recentWorldStates"`
	RecentActions      []models.AutonomyActionTrace `json:"recentActions"`
	RecentEvaluations  []models.AutonomyEvaluation  `json:"recentEvaluations"`
	RecentStressRuns   []models.AutonomyStressRun   `json:"recentStressRuns"`
	DecisionDiscipline map[string]any               `json:"decisionDiscipline"`
	Warnings           []string                     `json:"warnings"`
}

type Service interface {
	Overview() (*Overview, error)
	OverviewForOwner(owner string) (*Overview, error)
	RunStressSuite() (*models.AutonomyStressRun, []StressCaseResult, error)
}

type service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) Service {
	return &service{db: db}
}

func DefaultService() Service {
	db, err := infra.GetDefaultDB()
	if err != nil {
		panic(err)
	}
	return NewService(db)
}

func (s *service) Overview() (*Overview, error) {
	return s.overview(nil)
}

func (s *service) OverviewForOwner(owner string) (*Overview, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, apierror.New(apierror.CodeUnauthorized, "an authenticated owner is required for autonomy telemetry")
	}
	return s.overview(&owner)
}

func (s *service) overview(owner *string) (*Overview, error) {
	limit := telemetryLimit()
	states := make([]models.AutonomyWorldState, 0)
	actions := make([]models.AutonomyActionTrace, 0)
	evaluations := make([]models.AutonomyEvaluation, 0)
	stressRuns := make([]models.AutonomyStressRun, 0)
	scope := func(db *gorm.DB) *gorm.DB {
		if owner == nil {
			return db
		}
		workflows := s.db.Model(&models.WorkflowItem{}).Select("id").Where("owner_identity = ?", *owner)
		return db.Where("workflow_id IN (?)", workflows)
	}
	if err := s.db.Scopes(scope).Order("observed_at desc").Limit(limit).Find(&states).Error; err != nil {
		return nil, err
	}
	if err := s.db.Scopes(scope).Order("started_at desc").Limit(limit).Find(&actions).Error; err != nil {
		return nil, err
	}
	if err := s.db.Scopes(scope).Order("created_at desc").Limit(500).Find(&evaluations).Error; err != nil {
		return nil, err
	}
	if err := s.db.Order("created_at desc").Limit(10).Find(&stressRuns).Error; err != nil {
		return nil, err
	}
	return &Overview{
		GeneratedAt:       time.Now().UTC(),
		Metrics:           aggregate(evaluations),
		RecentWorldStates: states,
		RecentActions:     actions,
		RecentEvaluations: evaluations[:min(len(evaluations), limit)],
		RecentStressRuns:  stressRuns,
		DecisionDiscipline: map[string]any{
			"name":    "YAGNI gatekeeper",
			"enabled": true,
			"order": []string{
				"necessity",
				"standard_library",
				"platform_native",
				"existing_dependency",
				"small_patch",
				"minimal_custom",
			},
			"newDependenciesDefault": "blocked",
			"benchmarkClaims":        "unverified until reproduced locally",
		},
		Warnings: []string{
			"HTTP workflow telemetry is owner-scoped; recent stress runs are shared deterministic system diagnostics, not personal workflow results.",
			"Completion under policy requires verified execution and any mandatory approval; raw completion alone is not accepted.",
			"Stress-suite results validate deterministic guards, not the correctness of external providers or uncontrolled real-world environments.",
			"World-state snapshots are compact workflow observations; desktop, browser, and physical perception require runtime-specific adapters.",
		},
	}, nil
}

func (s *service) RunStressSuite() (*models.AutonomyStressRun, []StressCaseResult, error) {
	now := time.Now().UTC()
	cases := []struct {
		name     string
		expected string
		action   ActionEnvelope
	}{
		{
			name: "protected action without approval", expected: "blocked_approval",
			action: ActionEnvelope{InterfaceType: InterfaceToolCall, ActionType: "send_email", RequiresApproval: true, ObservationTime: now, StaleAfter: now.Add(time.Minute)},
		},
		{
			name: "stale observation", expected: "blocked_stale_state",
			action: ActionEnvelope{InterfaceType: InterfaceSkillCall, ActionType: "classify", ObservationTime: now.Add(-time.Hour), StaleAfter: now.Add(-time.Minute)},
		},
		{
			name: "invalid control interface", expected: "blocked_invalid_action",
			action: ActionEnvelope{InterfaceType: "raw_shell", ActionType: "execute", ObservationTime: now, StaleAfter: now.Add(time.Minute)},
		},
		{
			name: "untrusted content attempts policy override", expected: "blocked_prompt_injection",
			action: ActionEnvelope{InterfaceType: InterfaceToolCall, ActionType: "read_source", ObservationTime: now, StaleAfter: now.Add(time.Minute), UntrustedInput: true, PolicyOverride: true},
		},
		{
			name: "bounded low-risk action", expected: "allowed",
			action: ActionEnvelope{InterfaceType: InterfaceSkillCall, ActionType: "create_checklist", ObservationTime: now, StaleAfter: now.Add(time.Minute)},
		},
	}
	results := make([]StressCaseResult, 0, len(cases))
	passed := 0
	for _, test := range cases {
		actual := ValidateAction(test.action, now)
		ok := actual == test.expected
		if ok {
			passed++
		}
		results = append(results, StressCaseResult{Name: test.name, Passed: ok, Expected: test.expected, Actual: actual})
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		return nil, nil, err
	}
	run := &models.AutonomyStressRun{
		Passed:  passed,
		Failed:  len(results) - passed,
		Results: string(encoded),
	}
	if err := s.db.Create(run).Error; err != nil {
		return nil, nil, err
	}
	return run, results, nil
}

func ValidateAction(action ActionEnvelope, now time.Time) string {
	switch action.InterfaceType {
	case InterfaceSkillCall, InterfaceToolCall, InterfaceActionChunk:
	default:
		return "blocked_invalid_action"
	}
	if strings.TrimSpace(action.ActionType) == "" {
		return "blocked_invalid_action"
	}
	if action.StaleAfter.IsZero() || !now.Before(action.StaleAfter) {
		return "blocked_stale_state"
	}
	if action.UntrustedInput && action.PolicyOverride {
		return "blocked_prompt_injection"
	}
	if action.RequiresApproval && !action.ApprovalRecorded {
		return "blocked_approval"
	}
	return "allowed"
}

func aggregate(items []models.AutonomyEvaluation) Metrics {
	result := Metrics{Attempts: len(items)}
	var latency int64
	for _, item := range items {
		if item.RawCompletion {
			result.RawCompletions++
		}
		if item.CompletionUnderPolicy {
			result.CompletionUnderPolicy++
		}
		if item.RiskViolation {
			result.PolicyViolations++
		}
		if item.InvalidAction {
			result.InvalidActions++
		}
		if item.HumanIntervention {
			result.HumanInterventions++
		}
		if item.RecoveryAttempt {
			result.RecoveryAttempts++
		}
		if item.Recovered {
			result.Recovered++
		}
		latency += item.LatencyMilliseconds
	}
	if result.Attempts > 0 {
		result.AverageLatencyMillis = float64(latency) / float64(result.Attempts)
		result.RawCompletionRate = float64(result.RawCompletions) / float64(result.Attempts)
		result.PolicyCompletionRate = float64(result.CompletionUnderPolicy) / float64(result.Attempts)
		result.InterventionRate = float64(result.HumanInterventions) / float64(result.Attempts)
	}
	if result.RecoveryAttempts > 0 {
		result.RecoveryRate = float64(result.Recovered) / float64(result.RecoveryAttempts)
	}
	return result
}

func telemetryLimit() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AUTONOMY_TELEMETRY_LIMIT")))
	if err != nil || value < 5 || value > 100 {
		return 25
	}
	return value
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
