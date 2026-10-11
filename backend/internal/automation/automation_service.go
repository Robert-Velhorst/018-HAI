package automation

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/processcontrol"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/util"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultAPILaunchAllowedHosts = "localhost,127.0.0.1,::1,backend,frontend,gateway,generic-auto,idp"
)

type HealthResult struct {
	AutomationID        uuid.UUID `json:"automationId"`
	Status              string    `json:"status"`
	CheckedAt           time.Time `json:"checkedAt"`
	LatencyMs           int64     `json:"latencyMs"`
	FailureReason       string    `json:"failureReason,omitempty"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
}

type HealthSummary struct {
	Total     int       `json:"total"`
	Healthy   int       `json:"healthy"`
	Warning   int       `json:"warning"`
	Degraded  int       `json:"degraded"`
	Broken    int       `json:"broken"`
	Unknown   int       `json:"unknown"`
	CheckedAt time.Time `json:"checkedAt"`
}

type LaunchResult struct {
	AutomationID      uuid.UUID                           `json:"automationId"`
	LaunchEventID     uuid.UUID                           `json:"launchEventId,omitempty"`
	RuntimeTaskID     string                              `json:"runtimeTaskId,omitempty"`
	RuntimeType       string                              `json:"runtimeType,omitempty"`
	LaunchType        string                              `json:"launchType"`
	Target            string                              `json:"target"`
	Status            string                              `json:"status"`
	Message           string                              `json:"message,omitempty"`
	Output            string                              `json:"output,omitempty"`
	RuntimeRouteTrace *models.AutomationRuntimeRouteTrace `json:"runtimeRouteTrace,omitempty"`
	ExitCode          int                                 `json:"exitCode"`
	DurationMs        int64                               `json:"durationMs"`
	RequiresApproval  bool                                `json:"requiresApproval"`
	AuditEvents       []string                            `json:"auditEvents"`
	LaunchedAt        time.Time                           `json:"launchedAt"`
}

type DiagnosticResult struct {
	AutomationID      uuid.UUID                      `json:"automationId"`
	Name              string                         `json:"name"`
	Status            string                         `json:"status"`
	LaunchTarget      string                         `json:"launchTarget"`
	HealthCheckTarget string                         `json:"healthCheckTarget"`
	RoutePath         string                         `json:"routePath"`
	Host              string                         `json:"host"`
	Port              int                            `json:"port"`
	LastCheckedAt     *time.Time                     `json:"lastCheckedAt,omitempty"`
	LastSuccessAt     *time.Time                     `json:"lastSuccessAt,omitempty"`
	LastFailureAt     *time.Time                     `json:"lastFailureAt,omitempty"`
	LastFailureReason string                         `json:"lastFailureReason,omitempty"`
	Checks            map[string]string              `json:"checks"`
	RecentEvents      []models.AutomationHealthEvent `json:"recentEvents"`
	RecentLaunches    []models.AutomationLaunchEvent `json:"recentLaunches"`
}

type launchExecution struct {
	Status            string
	Message           string
	Output            string
	RuntimeRouteTrace *models.AutomationRuntimeRouteTrace
	ExitCode          int
	DurationMs        int64
	RequiresApproval  bool
	RuntimeTaskID     string
	AuditEvents       []string
}

type TaskLaunchRequest struct {
	OwnerIdentity string                  `json:"-"`
	ActorIdentity string                  `json:"-"`
	ActorKind     executionauth.ActorKind `json:"-"`
	TaskID        string                  `json:"-"`
	Task          string                  `json:"task,omitempty"`
	ProjectKey    string                  `json:"projectKey,omitempty"`
	// MandateID is a reference only. The execution-authorization service
	// resolves it by verified owner and evaluates its exact bounded scope.
	MandateID             string                           `json:"mandateId,omitempty"`
	ApprovalSourceID      string                           `json:"-"`
	ApprovalBindingDigest string                           `json:"-"`
	Governance            executionauth.GovernanceEvidence `json:"-"`
	ExecutionContext      context.Context                  `json:"-"`
	ApprovalProof         *ApprovalProof                   `json:"-"`
}

type ExecutionAuthorizer interface {
	AuthorizeAndConsume(
		context.Context,
		executionauth.Request,
		string,
		string,
	) (executionauth.Receipt, error)
}

type Service interface {
	FindByID(id uuid.UUID) (*models.Automation, error)
	Create(automation *models.Automation) (*models.Automation, error)
	Update(automation *models.Automation) (*models.Automation, error)
	Delete(id uuid.UUID) error
	FindAll() ([]*models.Automation, error)
	SwapOrder(id1 uuid.UUID, id2 uuid.UUID) error
	RunHealthCheck(id uuid.UUID) (*HealthResult, error)
	HealthSummary() (*HealthSummary, error)
	Launch(id uuid.UUID) (*LaunchResult, error)
	LaunchTask(id uuid.UUID, request TaskLaunchRequest) (*LaunchResult, error)
	PrepareWorkflowApprovalBinding(id uuid.UUID, request TaskLaunchRequest) (string, error)
	StopRuntimeTask(id uuid.UUID) (*agentruntime.StopResult, error)
	StopRuntimeTaskForOwner(id uuid.UUID, ownerIdentity string) (*agentruntime.StopResult, error)
	Diagnostics(id uuid.UUID) (*DiagnosticResult, error)
}

type service struct {
	repo            Repository
	publisher       events.Publisher
	runtimeRegistry *agentruntime.Registry
	approvalProofs  ApprovalProofService
	executionAuth   ExecutionAuthorizer
	finalEffects    *executionauth.FinalEffectBridge
	httpClient      *http.Client
}

func NewService(repo Repository, publisher events.Publisher) Service {
	return NewServiceWithRuntimeRegistry(repo, publisher, agentruntime.DefaultRegistry())
}

func NewServiceWithRuntimeRegistry(repo Repository, publisher events.Publisher, runtimeRegistry *agentruntime.Registry) Service {
	return NewServiceWithRuntimeRegistryAndApprovalProofs(
		repo,
		publisher,
		runtimeRegistry,
		newDefaultApprovalProofService(),
	)
}

func NewServiceWithRuntimeRegistryAndExecutionAuthorization(
	repo Repository,
	publisher events.Publisher,
	runtimeRegistry *agentruntime.Registry,
	executionAuthorizer ExecutionAuthorizer,
) Service {
	return NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(
		repo,
		publisher,
		runtimeRegistry,
		newDefaultApprovalProofService(),
		executionAuthorizer,
	)
}

func NewServiceWithRuntimeRegistryExecutionAuthorizationAndFinalEffects(
	repo Repository,
	publisher events.Publisher,
	runtimeRegistry *agentruntime.Registry,
	executionAuthorizer ExecutionAuthorizer,
	finalEffects *executionauth.FinalEffectBridge,
) Service {
	return NewServiceWithRuntimeRegistryApprovalProofsExecutionAuthorizationAndFinalEffects(
		repo,
		publisher,
		runtimeRegistry,
		newDefaultApprovalProofService(),
		executionAuthorizer,
		finalEffects,
	)
}

func NewServiceWithRuntimeRegistryAndApprovalProofs(
	repo Repository,
	publisher events.Publisher,
	runtimeRegistry *agentruntime.Registry,
	approvalProofs ApprovalProofService,
) Service {
	return NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(
		repo,
		publisher,
		runtimeRegistry,
		approvalProofs,
		nil,
	)
}

func NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(
	repo Repository,
	publisher events.Publisher,
	runtimeRegistry *agentruntime.Registry,
	approvalProofs ApprovalProofService,
	executionAuthorizer ExecutionAuthorizer,
) Service {
	return NewServiceWithRuntimeRegistryApprovalProofsExecutionAuthorizationAndFinalEffects(
		repo,
		publisher,
		runtimeRegistry,
		approvalProofs,
		executionAuthorizer,
		nil,
	)
}

func NewServiceWithRuntimeRegistryApprovalProofsExecutionAuthorizationAndFinalEffects(
	repo Repository,
	publisher events.Publisher,
	runtimeRegistry *agentruntime.Registry,
	approvalProofs ApprovalProofService,
	executionAuthorizer ExecutionAuthorizer,
	finalEffects *executionauth.FinalEffectBridge,
) Service {
	if approvalProofs == nil {
		approvalProofs = unavailableApprovalProofService{err: errors.New("approval proof service was not configured")}
	}
	return &service{
		repo:            repo,
		publisher:       publisher,
		runtimeRegistry: runtimeRegistry,
		approvalProofs:  approvalProofs,
		executionAuth:   executionAuthorizer,
		finalEffects:    finalEffects,
		httpClient:      noRedirectHTTPClient(10 * time.Second),
	}
}

func DefaultService() Service {
	repo := DefaultRepository()
	pub := events.DefaultPublisher()
	proofs, err := DefaultDurableApprovalProofService(
		[]byte(config.AppConfig.ApprovalProofSigningKey),
	)
	if err != nil {
		panic(fmt.Errorf("initialize automation approval proofs: %w", err))
	}
	return NewServiceWithRuntimeRegistryAndApprovalProofs(
		repo,
		*pub,
		agentruntime.DefaultRegistry(),
		proofs,
	)
}

func (s *service) FindByID(id uuid.UUID) (*models.Automation, error) {
	return s.repo.FindByID(id)
}

func (s *service) Create(automation *models.Automation) (*models.Automation, error) {
	automation.ID = uuid.UUID{} // reset ID

	if automation.ImageFile != nil {
		newFileName, err := s.processImageFile(automation.ImageFile)
		if err != nil {
			return nil, err
		}
		automation.Image = newFileName
	}

	maxPosition, err := s.repo.MaxPosition()
	if err != nil {
		return nil, err
	}
	automation.Position = maxPosition + 1

	err = s.ensureUniqueURLPath(automation)
	if err != nil {
		return nil, err
	}
	s.applyAutomationDefaults(automation)

	if err := automation.Validate(); err != nil {
		return nil, err
	}

	event := &events.AutomationEvent{
		Type:       events.CreateEvent,
		Automation: automation,
	}
	if durableRepo, ok := s.repo.(DurableEventRepository); ok {
		return durableRepo.CreateWithEvent(automation, event)
	}

	automationCreated, err := s.repo.Create(automation)
	if err != nil {
		return nil, err
	}
	event.Automation = automationCreated
	err = s.publisher.Publish(event)
	if err != nil {
		log.Printf("Failed to publish create event to Kafka: %v", err)
		return nil, err
	}
	return automationCreated, nil
}

func (s *service) Update(automation *models.Automation) (*models.Automation, error) {
	currentAutomation, err := s.repo.FindByID(automation.ID)
	if err != nil {
		return nil, err
	}

	automation.Position = currentAutomation.Position
	automation.LastCheckedAt = currentAutomation.LastCheckedAt
	automation.LastSuccessAt = currentAutomation.LastSuccessAt
	automation.LastFailureAt = currentAutomation.LastFailureAt
	automation.LastFailureReason = currentAutomation.LastFailureReason
	automation.ConsecutiveFailures = currentAutomation.ConsecutiveFailures
	automation.AverageLatencyMs = currentAutomation.AverageLatencyMs
	automation.LastLaunchAt = currentAutomation.LastLaunchAt

	if automation.ImageFile != nil {
		newFileName, errIf := s.processImageFile(automation.ImageFile)
		if errIf != nil {
			return nil, errIf
		}
		if ok := s.deleteImage(currentAutomation.Image); ok != nil {
			return nil, ok
		}
		automation.Image = newFileName
	} else if automation.RemoveImage {
		if noDeleted := s.deleteImage(currentAutomation.Image); noDeleted != nil {
			return nil, noDeleted
		}
		automation.Image = ""
	} else {
		automation.Image = currentAutomation.Image
	}
	var oldUrlPath string
	if currentAutomation.Name != automation.Name {
		oldUrlPath = currentAutomation.URLPath
		err = s.ensureUniqueURLPath(automation)
		if err != nil {
			return nil, err
		}
	} else {
		oldUrlPath = currentAutomation.URLPath
		automation.URLPath = currentAutomation.URLPath
	}

	s.applyAutomationDefaults(automation)
	if errValidate := automation.Validate(); errValidate != nil {
		return nil, errValidate
	}

	automation.OldUrlPath = oldUrlPath
	event := &events.AutomationEvent{
		Type:       events.UpdateEvent,
		Automation: automation,
	}
	if durableRepo, ok := s.repo.(DurableEventRepository); ok {
		return durableRepo.UpdateWithEvent(automation, event)
	}

	automationUpdated, err := s.repo.Update(automation)
	if err != nil {
		return nil, err
	}
	automationUpdated.OldUrlPath = oldUrlPath
	event.Automation = automationUpdated

	err = s.publisher.Publish(event)
	if err != nil {
		log.Printf("Failed to publish update event to Kafka: %v", err)
		return nil, err
	}

	return automationUpdated, nil
}

func (s *service) Delete(id uuid.UUID) error {
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	event := &events.AutomationEvent{
		Type:       events.DeleteEvent,
		Automation: automation,
	}
	if durableRepo, ok := s.repo.(DurableEventRepository); ok {
		return durableRepo.DeleteWithEvent(id, event)
	}

	err = s.repo.Delete(id)
	if err != nil {
		return err
	}

	err = s.publisher.Publish(event)
	if err != nil {
		log.Printf("Failed to publish delete event to Kafka: %v", err)
		return err
	}

	return nil
}

func (s *service) FindAll() ([]*models.Automation, error) {
	return s.repo.FindAll()
}

func (s *service) SwapOrder(id1 uuid.UUID, id2 uuid.UUID) error {
	return s.repo.Transaction(func(tx *gorm.DB) error {
		automation1, err := s.repo.FindByID(id1)
		if err != nil {
			return err
		}
		automation2, err := s.repo.FindByID(id2)
		if err != nil {
			return err
		}

		pos1 := automation1.Position
		pos2 := automation2.Position

		maxPosition, err := s.repo.MaxPosition()
		if err != nil {
			return err
		}
		tempPosition := maxPosition + 1

		automation1.Position = tempPosition
		if err := tx.Save(automation1).Error; err != nil {
			return err
		}

		automation2.Position = pos1
		if err := tx.Save(automation2).Error; err != nil {
			return err
		}

		automation1.Position = pos2
		if err := tx.Save(automation1).Error; err != nil {
			return err
		}

		return nil
	})
}

func (s *service) RunHealthCheck(id uuid.UUID) (*HealthResult, error) {
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	started := time.Now().UTC()
	status := "healthy"
	failureReason := ""
	target := ""

	s.applyAutomationDefaults(automation)

	checkType := strings.ToLower(automation.HealthCheckType)
	switch checkType {
	case "tcp":
		target = fmt.Sprintf("%s:%d", automation.Host, automation.Port)
		if reason := networkTargetBlockedReason(automation.Host, "AUTOMATION_HEALTH_ALLOWED_HOSTS", defaultAPILaunchAllowedHosts, "AUTOMATION_HEALTH_ALLOW_LINK_LOCAL"); reason != "" {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = reason
			break
		}
		conn, errDial := net.DialTimeout("tcp", target, 5*time.Second)
		if errDial != nil {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = errDial.Error()
		} else {
			_ = conn.Close()
		}
	case "manual", "disabled":
		status = "unknown"
		failureReason = "automatic health checks are disabled for this automation"
	default:
		target = automation.HealthCheckURL
		if target == "" {
			target = fmt.Sprintf("http://%s:%d", automation.Host, automation.Port)
		}
		parsed, errParse := url.Parse(target)
		if errParse != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = "health check target must be an absolute http or https URL"
			break
		}
		if parsed.User != nil {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = "health check target must not contain credentials"
			break
		}
		if reason := networkTargetBlockedReason(parsed.Hostname(), "AUTOMATION_HEALTH_ALLOWED_HOSTS", defaultAPILaunchAllowedHosts, "AUTOMATION_HEALTH_ALLOW_LINK_LOCAL"); reason != "" {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = reason
			break
		}
		client := s.directHTTPClient()
		resp, errGet := client.Get(target)
		if errGet != nil {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = errGet.Error()
		} else {
			defer resp.Body.Close()
			expected := automation.ExpectedHTTPStatus
			if expected == 0 {
				expected = http.StatusOK
			}
			if resp.StatusCode != expected {
				status = classifyFailure(automation.ConsecutiveFailures + 1)
				failureReason = fmt.Sprintf("unexpected HTTP status: got %d, expected %d", resp.StatusCode, expected)
			}
		}
	}

	latency := time.Since(started).Milliseconds()
	checkedAt := time.Now().UTC()
	automation.LastCheckedAt = &checkedAt
	automation.AverageLatencyMs = latency
	if status == "healthy" {
		automation.LastSuccessAt = &checkedAt
		automation.LastFailureReason = ""
		automation.ConsecutiveFailures = 0
	} else if status == "unknown" {
		automation.LastFailureReason = failureReason
	} else {
		automation.LastFailureAt = &checkedAt
		automation.LastFailureReason = failureReason
		automation.ConsecutiveFailures++
	}
	automation.Status = status

	if _, errUpdate := s.repo.Update(automation); errUpdate != nil {
		return nil, errUpdate
	}

	// Persist the check as a health-history event. A history write failure
	// must not fail the check itself, so it is only logged.
	event := &models.AutomationHealthEvent{
		AutomationID:        automation.ID,
		Status:              status,
		CheckType:           checkType,
		Target:              target,
		LatencyMs:           latency,
		FailureReason:       failureReason,
		ConsecutiveFailures: automation.ConsecutiveFailures,
		CheckedAt:           checkedAt,
	}
	if errEvent := s.repo.SaveHealthEvent(event); errEvent != nil {
		log.Printf("Failed to persist health event for automation %s: %v", automation.ID, errEvent)
	}

	return &HealthResult{
		AutomationID:        automation.ID,
		Status:              status,
		CheckedAt:           checkedAt,
		LatencyMs:           latency,
		FailureReason:       failureReason,
		ConsecutiveFailures: automation.ConsecutiveFailures,
	}, nil
}

func (s *service) HealthSummary() (*HealthSummary, error) {
	automations, err := s.repo.FindAll()
	if err != nil {
		return nil, err
	}
	summary := &HealthSummary{CheckedAt: time.Now().UTC()}
	summary.Total = len(automations)
	for _, automation := range automations {
		switch strings.ToLower(automation.Status) {
		case "healthy":
			summary.Healthy++
		case "warning":
			summary.Warning++
		case "degraded":
			summary.Degraded++
		case "broken":
			summary.Broken++
		default:
			summary.Unknown++
		}
	}
	return summary, nil
}

func (s *service) Launch(id uuid.UUID) (*LaunchResult, error) {
	return s.launch(id, TaskLaunchRequest{})
}

func (s *service) LaunchTask(id uuid.UUID, request TaskLaunchRequest) (*LaunchResult, error) {
	return s.launch(id, request)
}

func (s *service) PrepareWorkflowApprovalBinding(id uuid.UUID, request TaskLaunchRequest) (string, error) {
	if s.repo == nil {
		return "", fmt.Errorf("automation repository is unavailable")
	}
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return "", err
	}
	s.applyAutomationDefaults(automation)
	scope, _ := approvalScopeForAutomation(automation)
	if scope == "" {
		return "", fmt.Errorf("automation action does not have a supported approval scope")
	}
	request = TaskLaunchRequest{
		OwnerIdentity: strings.TrimSpace(request.OwnerIdentity),
		Task:          strings.TrimSpace(request.Task),
		ProjectKey:    strings.TrimSpace(request.ProjectKey),
		MandateID:     strings.TrimSpace(request.MandateID),
	}
	if request.OwnerIdentity == "" {
		return "", fmt.Errorf("workflow approval binding requires an owner identity")
	}
	digest := automationActionDigest(automation, request)
	return "automation-action:" + string(scope) + ":" + digest, nil
}

func (s *service) ActionApprovalRequired(id uuid.UUID) (bool, error) {
	if s.repo == nil {
		return false, fmt.Errorf("automation repository is unavailable")
	}
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return false, err
	}
	s.applyAutomationDefaults(automation)
	scope, required := approvalScopeForAutomation(automation)
	if scope == "" {
		return false, fmt.Errorf("automation action does not have a supported approval scope")
	}
	return required, nil
}

func (s *service) RecordApprovalDecision(id uuid.UUID, request TaskApprovalDecisionRequest) error {
	if s.repo == nil {
		return fmt.Errorf("automation repository is unavailable")
	}
	kind, _, err := approvalSourceKind(request.ApprovalSourceID)
	if err != nil {
		return err
	}
	if kind != "task-review" {
		return fmt.Errorf("only a verified task-review decision can be registered")
	}
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	s.applyAutomationDefaults(automation)
	scope, required := approvalScopeForAutomation(automation)
	if !required {
		return fmt.Errorf("automation action does not have a supported approval scope")
	}
	approvedAt := request.ApprovedAt.UTC()
	if request.ApprovedAt.IsZero() {
		return fmt.Errorf("approval decision time is required")
	}
	launchRequest := TaskLaunchRequest{
		OwnerIdentity:    strings.TrimSpace(request.OwnerIdentity),
		Task:             strings.TrimSpace(request.Task),
		ProjectKey:       strings.TrimSpace(request.ProjectKey),
		MandateID:        strings.TrimSpace(request.MandateID),
		ApprovalSourceID: strings.TrimSpace(request.ApprovalSourceID),
	}
	record := &ApprovalDecisionRecord{
		SourceID:      launchRequest.ApprovalSourceID,
		DecisionType:  kind,
		OwnerIdentity: launchRequest.OwnerIdentity,
		AutomationID:  automation.ID,
		ActionDigest:  automationActionDigest(automation, launchRequest),
		Scope:         scope,
		ApprovedAt:    approvedAt,
	}
	if err := validateApprovalDecisionFreshness(record, time.Now().UTC()); err != nil {
		return err
	}
	return s.repo.SaveApprovalDecision(record)
}

func (s *service) IssueApprovalProof(id uuid.UUID, request TaskApprovalProofRequest) (*ApprovalProof, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("automation repository is unavailable")
	}
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	s.applyAutomationDefaults(automation)
	scope, required := approvalScopeForAutomation(automation)
	if !required {
		return nil, fmt.Errorf("automation action does not require an execution approval proof")
	}
	launchRequest := TaskLaunchRequest{
		OwnerIdentity:    strings.TrimSpace(request.OwnerIdentity),
		Task:             strings.TrimSpace(request.Task),
		ProjectKey:       strings.TrimSpace(request.ProjectKey),
		MandateID:        strings.TrimSpace(request.MandateID),
		ApprovalSourceID: strings.TrimSpace(request.ApprovalSourceID),
	}
	kind, _, err := approvalSourceKind(launchRequest.ApprovalSourceID)
	if err != nil {
		return nil, err
	}
	record, err := s.repo.FindApprovalDecision(launchRequest.ApprovalSourceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApprovalDecisionMissing
		}
		return nil, fmt.Errorf("verify recorded approval decision: %w", err)
	}
	expectedDigest := automationActionDigest(automation, launchRequest)
	decisionNow := time.Now().UTC()
	if err := validateApprovalDecisionFreshness(record, decisionNow); err != nil {
		return nil, fmt.Errorf("verify recorded approval decision: %w", err)
	}
	if record.SourceID != launchRequest.ApprovalSourceID ||
		record.DecisionType != kind ||
		record.OwnerIdentity != launchRequest.OwnerIdentity ||
		record.AutomationID != automation.ID ||
		record.ActionDigest != expectedDigest ||
		record.Scope != scope {
		return nil, fmt.Errorf("recorded approval decision does not match the exact requested action")
	}
	if kind == "workflow-decision" {
		workflowID, parseErr := uuid.Parse(strings.TrimSpace(request.WorkflowID))
		if parseErr != nil || workflowID != record.WorkflowID {
			return nil, fmt.Errorf("recorded workflow approval decision does not match the workflow")
		}
	}
	proofTTL := request.TTL
	if proofTTL == 0 {
		proofTTL = defaultApprovalProofTTL
	}
	if proofTTL <= 0 || proofTTL > maximumApprovalProofTTL {
		return nil, fmt.Errorf("approval proof TTL must be between 1ns and %s", maximumApprovalProofTTL)
	}
	remainingDecisionLifetime := record.ApprovedAt.UTC().
		Add(maximumApprovalDecisionAge).
		Sub(decisionNow)
	if remainingDecisionLifetime <= 0 {
		return nil, fmt.Errorf("verify recorded approval decision: approval decision is stale")
	}
	if proofTTL > remainingDecisionLifetime {
		proofTTL = remainingDecisionLifetime
	}
	return s.approvalProofs.Issue(ApprovalProofIssueRequest{
		OwnerIdentity:    launchRequest.OwnerIdentity,
		AutomationID:     automation.ID,
		ActionDigest:     expectedDigest,
		Scope:            scope,
		ApprovalSourceID: launchRequest.ApprovalSourceID,
		TTL:              proofTTL,
	})
}

func (s *service) StopRuntimeTask(id uuid.UUID) (*agentruntime.StopResult, error) {
	return s.stopRuntimeTask(id, "")
}

func (s *service) StopRuntimeTaskForOwner(id uuid.UUID, ownerIdentity string) (*agentruntime.StopResult, error) {
	return s.stopRuntimeTask(id, ownerIdentity)
}

func (s *service) stopRuntimeTask(id uuid.UUID, ownerIdentity string) (*agentruntime.StopResult, error) {
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	s.applyAutomationDefaults(automation)
	started := time.Now().UTC()
	runtimeID := strings.ToLower(strings.TrimSpace(automation.RuntimeType))
	taskID := automation.ID.String()
	if automation.LaunchType != "agent_runtime" || (runtimeID != "hermes" && runtimeID != "odysseus" && runtimeID != "openclaw") {
		result := &agentruntime.StopResult{
			RuntimeID: runtimeID,
			TaskID:    taskID,
			Status:    "blocked",
			Message:   "automation is not configured as a controlled agent runtime",
			AuditEvents: []string{
				"automation runtime stop rejected",
				"launch type is not agent_runtime or runtime type is unsupported",
			},
		}
		_, _ = s.persistRuntimeStopEvent(automation, result, started, ownerIdentity)
		return result, nil
	}
	if s.runtimeRegistry == nil {
		result := &agentruntime.StopResult{
			RuntimeID:   runtimeID,
			TaskID:      taskID,
			Status:      "blocked",
			Message:     "agent runtime registry is not configured",
			AuditEvents: []string{"agent runtime registry unavailable"},
		}
		_, _ = s.persistRuntimeStopEvent(automation, result, started, ownerIdentity)
		return result, nil
	}
	intent := &models.AutomationLaunchEvent{
		ID:            uuid.New(),
		AutomationID:  automation.ID,
		OwnerIdentity: strings.TrimSpace(ownerIdentity),
		RuntimeType:   runtimeID,
		LaunchType:    "agent_runtime_stop_intent",
		RuntimeTaskID: taskID,
		Target:        redactLaunchTarget(automation.LaunchTarget),
		Status:        "pending",
		Message:       "immutable runtime stop intent recorded",
		AuditEvents: []string{
			"owner-bound runtime stop intent persisted before cancellation",
		},
		StartedAt:   started,
		CompletedAt: started,
	}
	if errIntent := s.repo.SaveLaunchIntent(intent); errIntent != nil {
		return nil, fmt.Errorf("persist runtime stop intent: %w", errIntent)
	}
	result := s.runtimeRegistry.StopTask(context.Background(), runtimeID, taskID, ownerIdentity)
	if _, errEvent := s.persistRuntimeStopEvent(automation, &result, started, ownerIdentity); errEvent != nil {
		result.Status = "indeterminate"
		result.Message = "runtime stop outcome audit could not be persisted; inspect the immutable stop intent before retrying"
		result.EvidenceURI = "automation-launch://" + intent.ID.String()
		result.AuditEvents = append(
			result.AuditEvents,
			"runtime stop outcome audit persistence failed",
			"stop completion was not claimed",
		)
	}
	return &result, nil
}

func (s *service) persistRuntimeStopEvent(automation *models.Automation, result *agentruntime.StopResult, started time.Time, ownerIdentity string) (uuid.UUID, error) {
	if automation == nil || result == nil {
		return uuid.Nil, fmt.Errorf("runtime stop audit requires automation and result")
	}
	audit := append([]string{"runtime stop requested"}, result.AuditEvents...)
	exitCode := 0
	if result.Status == "blocked" || result.Status == "failed" {
		exitCode = -1
		automation.LastFailureReason = safety.RedactSecrets(result.Message)
		if _, errUpdate := s.repo.Update(automation); errUpdate != nil {
			log.Printf("Failed to update automation %s after runtime stop: %v", automation.ID, errUpdate)
		}
	}
	event := &models.AutomationLaunchEvent{
		ID:            uuid.New(),
		AutomationID:  automation.ID,
		OwnerIdentity: strings.TrimSpace(ownerIdentity),
		RuntimeType:   automation.RuntimeType,
		LaunchType:    "agent_runtime_stop",
		RuntimeTaskID: result.TaskID,
		Target:        redactLaunchTarget(automation.LaunchTarget),
		Status:        result.Status,
		Message:       safety.RedactSecrets(result.Message),
		AuditEvents:   redactAuditEvents(audit),
		ExitCode:      exitCode,
		DurationMs:    time.Since(started).Milliseconds(),
		StartedAt:     started,
		CompletedAt:   time.Now().UTC(),
	}
	if errEvent := s.repo.SaveLaunchEvent(event); errEvent != nil {
		log.Printf("Failed to persist runtime stop event for automation %s: %v", automation.ID, errEvent)
		return uuid.Nil, errEvent
	}
	result.EvidenceURI = "automation-launch://" + event.ID.String()
	return event.ID, nil
}

func (s *service) launch(id uuid.UUID, request TaskLaunchRequest) (*LaunchResult, error) {
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	s.applyAutomationDefaults(automation)
	launchedAt := time.Now().UTC()
	intent := &models.AutomationLaunchEvent{
		ID:            uuid.New(),
		AutomationID:  automation.ID,
		OwnerIdentity: strings.TrimSpace(request.OwnerIdentity),
		RuntimeType:   automation.RuntimeType,
		LaunchType:    strings.ToLower(strings.TrimSpace(automation.LaunchType)) + "_intent",
		RuntimeTaskID: automationRuntimeTaskID(automation),
		Target:        redactLaunchTarget(automation.LaunchTarget),
		Status:        "pending",
		Message:       "immutable pre-execution intent recorded",
		AuditEvents: redactAuditEvents([]string{
			"controlled launch intent persisted before approval consumption or external access",
			"exact action digest " + automationActionDigest(automation, request),
		}),
		StartedAt:   launchedAt,
		CompletedAt: launchedAt,
	}
	if errIntent := s.repo.SaveLaunchIntent(intent); errIntent != nil {
		return nil, fmt.Errorf("persist pre-execution launch intent: %w", errIntent)
	}
	execution := s.executeLaunch(automation, request, launchedAt, intent.ID)
	execution.AuditEvents = append(
		[]string{"immutable pre-execution intent " + intent.ID.String() + " persisted"},
		execution.AuditEvents...,
	)
	automation.LastLaunchAt = &launchedAt
	if execution.Status == "failed" || execution.Status == "blocked" {
		automation.LastFailureReason = safety.RedactSecrets(execution.Message)
	}
	event := &models.AutomationLaunchEvent{
		ID:            uuid.New(),
		AutomationID:  automation.ID,
		OwnerIdentity: strings.TrimSpace(request.OwnerIdentity),
		RuntimeType:   automation.RuntimeType,
		LaunchType:    automation.LaunchType,
		RuntimeTaskID: execution.RuntimeTaskID,
		Target:        redactLaunchTarget(automation.LaunchTarget),
		Status:        execution.Status,
		Message:       safety.RedactSecrets(execution.Message),
		Output:        safety.RedactSecrets(execution.Output),
		AuditEvents:   redactAuditEvents(execution.AuditEvents),
		RuntimeRouteTrace: redactRuntimeRouteTrace(
			execution.RuntimeRouteTrace,
		),
		ExitCode:    execution.ExitCode,
		DurationMs:  execution.DurationMs,
		StartedAt:   launchedAt,
		CompletedAt: time.Now().UTC(),
	}
	if errEvent := s.repo.SaveLaunchEvent(event); errEvent != nil {
		log.Printf("Failed to persist launch event for automation %s: %v", automation.ID, errEvent)
		return &LaunchResult{
			AutomationID:     automation.ID,
			LaunchEventID:    intent.ID,
			RuntimeTaskID:    execution.RuntimeTaskID,
			RuntimeType:      automation.RuntimeType,
			LaunchType:       automation.LaunchType,
			Target:           redactLaunchTarget(automation.LaunchTarget),
			Status:           "indeterminate",
			Message:          "execution outcome audit could not be persisted; inspect the immutable pre-execution intent before retrying",
			ExitCode:         -1,
			DurationMs:       execution.DurationMs,
			RequiresApproval: execution.RequiresApproval,
			AuditEvents: redactAuditEvents(append(
				execution.AuditEvents,
				"execution outcome audit persistence failed",
				"completion was not claimed",
			)),
			LaunchedAt: launchedAt,
		}, nil
	}
	if _, errUpdate := s.repo.Update(automation); errUpdate != nil {
		return nil, errUpdate
	}
	return &LaunchResult{
		AutomationID:      automation.ID,
		LaunchEventID:     event.ID,
		RuntimeTaskID:     execution.RuntimeTaskID,
		RuntimeType:       automation.RuntimeType,
		LaunchType:        automation.LaunchType,
		Target:            redactLaunchTarget(automation.LaunchTarget),
		Status:            execution.Status,
		Message:           safety.RedactSecrets(execution.Message),
		Output:            safety.RedactSecrets(execution.Output),
		RuntimeRouteTrace: redactRuntimeRouteTrace(execution.RuntimeRouteTrace),
		ExitCode:          execution.ExitCode,
		DurationMs:        execution.DurationMs,
		RequiresApproval:  execution.RequiresApproval,
		AuditEvents:       redactAuditEvents(execution.AuditEvents),
		LaunchedAt:        launchedAt,
	}, nil
}

func automationRuntimeTaskID(automation *models.Automation) string {
	if automation == nil ||
		strings.ToLower(strings.TrimSpace(automation.LaunchType)) != "agent_runtime" {
		return ""
	}
	return automation.ID.String()
}

func (s *service) Diagnostics(id uuid.UUID) (*DiagnosticResult, error) {
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	s.applyAutomationDefaults(automation)
	checks := map[string]string{
		"launchTargetConfigured": boolStatus(automation.LaunchTarget != ""),
		"healthCheckConfigured":  boolStatus(automation.HealthCheckType != ""),
		"routePathConfigured":    boolStatus(automation.RoutePath != "" || automation.URLPath != ""),
		"hostConfigured":         boolStatus(automation.Host != ""),
		"portConfigured":         boolStatus(automation.Port > 0 && automation.Port <= 65535),
		"dependencyNotesPresent": boolStatus(automation.DependencyNotes != ""),
	}
	recentEvents, errEvents := s.repo.FindHealthEvents(automation.ID, 10)
	if errEvents != nil {
		log.Printf("Failed to load health history for automation %s: %v", automation.ID, errEvents)
		recentEvents = []models.AutomationHealthEvent{}
	}
	recentLaunches, errLaunches := s.repo.FindLaunchEvents(automation.ID, 10)
	if errLaunches != nil {
		log.Printf("Failed to load launch history for automation %s: %v", automation.ID, errLaunches)
		recentLaunches = []models.AutomationLaunchEvent{}
	}

	return &DiagnosticResult{
		AutomationID:      automation.ID,
		Name:              automation.Name,
		Status:            automation.Status,
		LaunchTarget:      automation.LaunchTarget,
		HealthCheckTarget: automation.HealthCheckURL,
		RoutePath:         firstNonEmpty(automation.RoutePath, automation.URLPath),
		Host:              automation.Host,
		Port:              automation.Port,
		LastCheckedAt:     automation.LastCheckedAt,
		LastSuccessAt:     automation.LastSuccessAt,
		LastFailureAt:     automation.LastFailureAt,
		LastFailureReason: automation.LastFailureReason,
		Checks:            checks,
		RecentEvents:      recentEvents,
		RecentLaunches:    recentLaunches,
	}, nil
}

func redactAuditEvents(events []string) []string {
	if len(events) == 0 {
		return nil
	}
	const (
		maxEvents     = 64
		maxEventRunes = 512
	)
	result := make([]string, 0, minInt(len(events), maxEvents))
	for _, event := range events {
		event = boundedSingleLine(safety.RedactSecrets(event), maxEventRunes)
		if event != "" {
			result = append(result, event)
		}
		if len(result) == maxEvents {
			break
		}
	}
	return result
}

func boundedSingleLine(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func automationRuntimeRouteTrace(trace *agentruntime.RouteTrace) *models.AutomationRuntimeRouteTrace {
	if trace == nil {
		return nil
	}
	return redactRuntimeRouteTrace(&models.AutomationRuntimeRouteTrace{
		RuntimeID:           trace.RuntimeID,
		Intent:              trace.Intent,
		ExecutionMode:       trace.ExecutionMode,
		RiskLevel:           trace.RiskLevel,
		RecommendedSkills:   append([]string{}, trace.RecommendedSkills...),
		VisibleProviders:    append([]string{}, trace.VisibleProviders...),
		VisibleTools:        append([]string{}, trace.VisibleTools...),
		RelevantMaps:        append([]string{}, trace.RelevantMaps...),
		BlockedSurfaces:     append([]string{}, trace.BlockedSurfaces...),
		RequiredControls:    append([]string{}, trace.RequiredControls...),
		ValidationChecklist: append([]string{}, trace.ValidationChecklist...),
	})
}

func redactRuntimeRouteTrace(trace *models.AutomationRuntimeRouteTrace) *models.AutomationRuntimeRouteTrace {
	if trace == nil {
		return nil
	}
	return &models.AutomationRuntimeRouteTrace{
		RuntimeID:           safety.RedactSecrets(trace.RuntimeID),
		Intent:              safety.RedactSecrets(trace.Intent),
		ExecutionMode:       safety.RedactSecrets(trace.ExecutionMode),
		RiskLevel:           safety.RedactSecrets(trace.RiskLevel),
		RecommendedSkills:   redactStringSlice(trace.RecommendedSkills),
		VisibleProviders:    redactStringSlice(trace.VisibleProviders),
		VisibleTools:        redactStringSlice(trace.VisibleTools),
		RelevantMaps:        redactStringSlice(trace.RelevantMaps),
		BlockedSurfaces:     redactStringSlice(trace.BlockedSurfaces),
		RequiredControls:    redactStringSlice(trace.RequiredControls),
		ValidationChecklist: redactStringSlice(trace.ValidationChecklist),
	}
}

func redactStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(safety.RedactSecrets(value))
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func (s *service) executeLaunch(
	automation *models.Automation,
	request TaskLaunchRequest,
	started time.Time,
	intentID uuid.UUID,
) launchExecution {
	launchType := strings.ToLower(strings.TrimSpace(automation.LaunchType))
	if launchType == "" {
		launchType = "browser_url"
	}
	audit := []string{
		"launch requested",
		"automation configuration loaded",
		"runtime safety policy evaluated",
	}
	if safety.EmergencyStopActive() {
		return blockedLaunch(safety.EmergencyStopReason(), started, append(audit, "emergency stop blocked runtime launch"))
	}
	switch launchType {
	case "browser_url":
		return launchExecution{
			Status:      "ready",
			Message:     "browser target prepared for client-side opening",
			DurationMs:  time.Since(started).Milliseconds(),
			AuditEvents: append(audit, "no server-side device action was performed"),
		}
	case "api":
		method, _ := parseLaunchMethodTarget(automation.LaunchTarget, http.MethodPost)
		if method != http.MethodGet && method != http.MethodHead {
			verifiedAudit, err := s.verifyAndConsumeApproval(automation, request)
			if err != nil {
				return blockedLaunch(
					"action-bound human approval is required at the launcher boundary for mutating API requests: "+err.Error(),
					started,
					append(audit, "action-bound approval proof rejected before network access"),
				)
			}
			audit = append(audit, verifiedAudit...)
		}
		return s.executeAPILaunch(automation, request, intentID, started, audit)
	case "script":
		verifiedAudit, err := s.verifyAndConsumeApproval(automation, request)
		if err != nil {
			return blockedLaunch(
				"action-bound human approval is required at the launcher boundary for local script execution: "+err.Error(),
				started,
				append(audit, "action-bound approval proof rejected before process or filesystem access"),
			)
		}
		audit = append(audit, verifiedAudit...)
		return s.executeScriptLaunch(automation, request, intentID, started, audit)
	case "docker_service":
		verifiedAudit, err := s.verifyAndConsumeApproval(automation, request)
		if err != nil {
			return blockedLaunch(
				"action-bound human approval is required at the launcher boundary for Docker container starts: "+err.Error(),
				started,
				append(audit, "action-bound approval proof rejected before docker socket access"),
			)
		}
		audit = append(audit, verifiedAudit...)
		return s.executeDockerLaunch(automation, request, intentID, started, audit)
	case "agent_runtime":
		verifiedAudit, err := s.verifyAndConsumeApproval(automation, request)
		if err != nil {
			return blockedLaunch(
				"action-bound human approval is required at the launcher boundary for agent runtime execution: "+err.Error(),
				started,
				append(audit, "action-bound approval proof rejected before agent runtime access"),
			)
		}
		audit = append(audit, verifiedAudit...)
		runtimeTask, authorizationAudit, err := s.authorizeAgentRuntimeLaunch(
			automation,
			request,
			intentID,
		)
		if err != nil {
			return blockedLaunch(
				"unified execution authorization blocked agent runtime execution: "+err.Error(),
				started,
				append(audit, "execution authorization rejected before agent runtime access"),
			)
		}
		audit = append(audit, authorizationAudit...)
		return s.executeAgentRuntime(automation, request, runtimeTask, started, audit)
	default:
		return launchExecution{
			Status:           "blocked",
			Message:          fmt.Sprintf("launch type %q is not supported by the controlled runtime executor", launchType),
			ExitCode:         -1,
			DurationMs:       time.Since(started).Milliseconds(),
			RequiresApproval: true,
			AuditEvents:      append(audit, "unsupported runtime blocked"),
		}
	}
}

func (s *service) authorizeExternalLaunch(
	automation *models.Automation,
	request TaskLaunchRequest,
	intentID uuid.UUID,
	apiMethod string,
) (executionauth.Receipt, []string, error) {
	if s.executionAuth == nil {
		return executionauth.Receipt{}, nil, fmt.Errorf("unified execution authorization service is unavailable")
	}
	if automation == nil || automation.ID == uuid.Nil || intentID == uuid.Nil {
		return executionauth.Receipt{}, nil, fmt.Errorf("automation and immutable launch intent are required")
	}
	owner := strings.TrimSpace(request.OwnerIdentity)
	if owner == "" {
		return executionauth.Receipt{}, nil, fmt.Errorf("verified owner identity is required")
	}
	taskID := strings.TrimSpace(request.TaskID)
	if taskID == "" {
		taskID = "automation-intent:" + intentID.String()
	}
	ctx := request.ExecutionContext
	if ctx == nil {
		ctx = context.Background()
	}
	actorIdentity := strings.TrimSpace(request.ActorIdentity)
	actorKind := request.ActorKind
	if actorIdentity == "" {
		actorIdentity = owner
	}
	if actorKind == "" {
		actorKind = executionauth.ActorHuman
	}
	action, stage, risk, reversible, authority, autonomy, toolID, target :=
		executionAuthorizationProfile(automation, apiMethod)
	if strings.TrimSpace(request.ApprovalSourceID) != "" &&
		strings.TrimSpace(request.ApprovalBindingDigest) != "" &&
		risk == executionauth.RiskLow && reversible {
		// A reviewed, exact low-risk action runs at case-approved level 6.
		// Without the decision it remains autonomous-safe level 8.
		authority = 6
		autonomy = 6
	}
	sourceReferences := append(
		[]string{"automation-intent://" + intentID.String()},
		request.Governance.EvidenceReferences...,
	)
	receipt, err := s.executionAuth.AuthorizeAndConsume(
		ctx,
		executionauth.Request{
			OwnerIdentity:         owner,
			IdempotencyKey:        "automation-launch:" + intentID.String(),
			ActorIdentity:         actorIdentity,
			ActorKind:             actorKind,
			TaskID:                taskID,
			Action:                action,
			Stage:                 stage,
			ResourceType:          "automation",
			ResourceID:            automation.ID.String(),
			ProjectKey:            strings.TrimSpace(request.ProjectKey),
			MandateID:             strings.TrimSpace(request.MandateID),
			ToolID:                toolID,
			RuntimeID:             strings.ToLower(strings.TrimSpace(automation.RuntimeType)),
			RequiredAuthority:     authority,
			RequestedAutonomy:     autonomy,
			Risk:                  risk,
			Reversible:            reversible,
			ApprovalSourceID:      strings.TrimSpace(request.ApprovalSourceID),
			ApprovalBindingDigest: strings.ToLower(strings.TrimSpace(request.ApprovalBindingDigest)),
			EffectDigest:          automationActionDigest(automation, request),
			Governance:            &request.Governance,
			SourceReferences:      sourceReferences,
		},
		"automation-launcher",
		target,
	)
	if err != nil {
		return receipt, nil, err
	}
	if receipt.Outcome != executionauth.OutcomeAuthorized {
		return receipt, nil, executionauth.ErrNotAuthorized
	}
	return receipt, []string{
		"unified execution authorization receipt " + receipt.ID.String() + " consumed",
		"Constitution " + receipt.Evidence.Constitution.Source + " evaluated",
	}, nil
}

func (s *service) authorizeAgentRuntimeLaunch(
	automation *models.Automation,
	request TaskLaunchRequest,
	intentID uuid.UUID,
) (agentruntime.Task, []string, error) {
	if s.executionAuth == nil || s.finalEffects == nil {
		return agentruntime.Task{}, nil, fmt.Errorf(
			"agent runtime execution authorization bridge is unavailable",
		)
	}
	if s.runtimeRegistry == nil || automation == nil ||
		automation.ID == uuid.Nil || intentID == uuid.Nil {
		return agentruntime.Task{}, nil, fmt.Errorf(
			"agent runtime registry, automation, and immutable launch intent are required",
		)
	}
	runtimeID := strings.ToLower(strings.TrimSpace(automation.RuntimeType))
	requiresApproval, registered := runtimeApprovalRequirement(
		s.runtimeRegistry,
		runtimeID,
	)
	if !registered {
		return agentruntime.Task{}, nil, fmt.Errorf(
			"agent runtime %q is not registered",
			runtimeID,
		)
	}
	owner := strings.TrimSpace(request.OwnerIdentity)
	if owner == "" {
		return agentruntime.Task{}, nil, fmt.Errorf("verified owner identity is required")
	}
	runtimeTask := agentruntime.Task{
		ID:               automationRuntimeTaskID(automation),
		Prompt:           strings.TrimSpace(request.Task),
		ProjectKey:       strings.TrimSpace(request.ProjectKey),
		OwnerIdentity:    owner,
		ApprovalSourceID: strings.TrimSpace(request.ApprovalSourceID),
		// The action-bound proof remains an independent defense-in-depth gate.
		HumanApproved: true,
	}
	finalRequest, err := executionauth.BuildAgentRuntimeFinalEffectRequest(
		runtimeID,
		runtimeTask.ID,
		runtimeTask.OwnerIdentity,
		runtimeTask.ProjectKey,
		runtimeTask.Prompt,
		runtimeTask.ApprovalSourceID,
		requiresApproval,
	)
	if err != nil {
		return agentruntime.Task{}, nil, err
	}
	effectDigest, err := executionauth.FinalEffectDigest(finalRequest)
	if err != nil {
		return agentruntime.Task{}, nil, err
	}
	executionTarget, err := executionauth.FinalEffectExecutionTarget(effectDigest)
	if err != nil {
		return agentruntime.Task{}, nil, err
	}
	ctx := request.ExecutionContext
	if ctx == nil {
		ctx = context.Background()
	}
	actorIdentity := strings.TrimSpace(request.ActorIdentity)
	if actorIdentity == "" {
		actorIdentity = owner
	}
	actorKind := request.ActorKind
	if actorKind == "" {
		actorKind = executionauth.ActorHuman
	}
	sourceReferences := []string{
		"automation-intent://" + intentID.String(),
	}
	sourceReferences = append(
		sourceReferences,
		request.Governance.EvidenceReferences...,
	)
	if parentTaskID := strings.TrimSpace(request.TaskID); parentTaskID != "" &&
		parentTaskID != runtimeTask.ID {
		sourceReferences = append(sourceReferences, "task://"+parentTaskID)
	}
	receipt, err := s.executionAuth.AuthorizeAndConsume(
		ctx,
		executionauth.Request{
			OwnerIdentity:         owner,
			IdempotencyKey:        "automation-launch:" + intentID.String(),
			ActorIdentity:         actorIdentity,
			ActorKind:             actorKind,
			TaskID:                runtimeTask.ID,
			Action:                executionauth.AgentRuntimeExecuteAction,
			Stage:                 executionauth.StageExecution,
			ResourceType:          executionauth.AgentRuntimeResourceType,
			ResourceID:            runtimeTask.ID,
			ProjectKey:            runtimeTask.ProjectKey,
			MandateID:             strings.TrimSpace(request.MandateID),
			ToolID:                "automation-agent-runtime",
			RuntimeID:             runtimeID,
			RequiredAuthority:     6,
			RequestedAutonomy:     6,
			Risk:                  executionauth.RiskHigh,
			Reversible:            false,
			ApprovalSourceID:      runtimeTask.ApprovalSourceID,
			ApprovalBindingDigest: strings.ToLower(strings.TrimSpace(request.ApprovalBindingDigest)),
			EffectDigest:          effectDigest,
			Governance:            &request.Governance,
			SourceReferences:      sourceReferences,
		},
		"automation-launcher",
		executionTarget,
	)
	if err != nil {
		return agentruntime.Task{}, nil, err
	}
	if receipt.Outcome != executionauth.OutcomeAuthorized {
		return agentruntime.Task{}, nil, executionauth.ErrNotAuthorized
	}
	binding, err := s.finalEffects.BindConsumedFinalEffect(
		ctx,
		finalRequest,
		receipt.ID,
	)
	if err != nil {
		return agentruntime.Task{}, nil, err
	}
	runtimeTask, err = s.runtimeRegistry.BindConsumedAuthorizationProof(
		runtimeID,
		runtimeTask,
		binding.ReceiptID,
		binding.AuthorizationRequestDigest,
		binding.DecisionDigest,
		binding.RuntimeProof,
	)
	if err != nil {
		return agentruntime.Task{}, nil, err
	}
	return runtimeTask, []string{
		"unified execution authorization receipt " + receipt.ID.String() + " consumed",
		"Constitution " + receipt.Evidence.Constitution.Source + " evaluated",
		"runtime final-effect proof bound to " + effectDigest,
	}, nil
}

func runtimeApprovalRequirement(
	registry *agentruntime.Registry,
	runtimeID string,
) (bool, bool) {
	if registry == nil {
		return false, false
	}
	for _, info := range registry.List() {
		if info.ID == runtimeID {
			return info.RequiresApproval, true
		}
	}
	return false, false
}

func executionAuthorizationProfile(
	automation *models.Automation,
	apiMethod string,
) (
	string,
	executionauth.Stage,
	executionauth.RiskLevel,
	bool,
	int,
	int,
	string,
	string,
) {
	launchType := strings.ToLower(strings.TrimSpace(automation.LaunchType))
	// Consumption targets are bounded audit identifiers, not raw URLs, paths,
	// or command strings. The separately validated effect digest binds the full
	// stored automation configuration to this authorization decision.
	target := "automation:" + automation.ID.String()
	switch launchType {
	case "api":
		method := strings.ToUpper(strings.TrimSpace(apiMethod))
		if method == http.MethodGet || method == http.MethodHead {
			return "automation.api.read",
				executionauth.StageDataAccess,
				executionauth.RiskLow,
				true,
				8,
				8,
				"automation-api-client",
				target
		}
		return "automation.api.mutate",
			executionauth.StageCommitment,
			executionauth.RiskHigh,
			false,
			6,
			6,
			"automation-api-client",
			target
	case "script":
		return "automation.script.execute",
			executionauth.StageExecution,
			executionauth.RiskHigh,
			false,
			6,
			6,
			"automation-script-runner",
			target
	case "docker_service":
		return "automation.docker.start",
			executionauth.StageExecution,
			executionauth.RiskHigh,
			true,
			6,
			6,
			"automation-docker-client",
			target
	case "agent_runtime":
		return "automation.agent-runtime.execute",
			executionauth.StageExecution,
			executionauth.RiskHigh,
			false,
			6,
			6,
			"automation-agent-runtime",
			target
	default:
		return "automation.unsupported",
			executionauth.StageExecution,
			executionauth.RiskCritical,
			false,
			10,
			10,
			"automation-unsupported",
			target
	}
}

func (s *service) executeAgentRuntime(
	automation *models.Automation,
	request TaskLaunchRequest,
	runtimeTask agentruntime.Task,
	started time.Time,
	audit []string,
) launchExecution {
	runtimeID := strings.ToLower(strings.TrimSpace(automation.RuntimeType))
	if runtimeID != "hermes" && runtimeID != "odysseus" && runtimeID != "openclaw" {
		return blockedLaunch("agent_runtime launch type requires runtimeType hermes, odysseus, or openclaw", started, append(audit, "agent runtime type rejected"))
	}
	if s.runtimeRegistry == nil {
		return blockedLaunch("agent runtime registry is not configured", started, append(audit, "agent runtime registry unavailable"))
	}
	executionContext := request.ExecutionContext
	if executionContext == nil {
		executionContext = context.Background()
	}
	result := s.runtimeRegistry.Execute(executionContext, runtimeID, runtimeTask)
	return launchExecution{
		Status:            result.Status,
		Message:           result.Message,
		Output:            result.Output,
		RuntimeRouteTrace: automationRuntimeRouteTrace(result.RouteTrace),
		ExitCode:          result.ExitCode,
		DurationMs:        result.DurationMs,
		RequiresApproval:  result.Status == "blocked",
		RuntimeTaskID:     runtimeTask.ID,
		AuditEvents:       append(audit, result.AuditEvents...),
	}
}

func (s *service) verifyAndConsumeApproval(automation *models.Automation, request TaskLaunchRequest) ([]string, error) {
	if s.approvalProofs == nil {
		return nil, fmt.Errorf("approval proof service is unavailable")
	}
	scope, required := approvalScopeForAutomation(automation)
	if !required {
		return nil, fmt.Errorf("automation action has no supported approval scope")
	}
	executionContext := request.ExecutionContext
	if executionContext == nil {
		executionContext = context.Background()
	}
	err := s.approvalProofs.VerifyAndConsume(executionContext, request.ApprovalProof, ApprovalProofExpectation{
		OwnerIdentity:    strings.TrimSpace(request.OwnerIdentity),
		AutomationID:     automation.ID,
		ActionDigest:     automationActionDigest(automation, request),
		Scope:            scope,
		ApprovalSourceID: strings.TrimSpace(request.ApprovalSourceID),
	})
	if err != nil {
		return nil, err
	}
	return []string{
		"action-bound approval proof verified and consumed",
		"repository-backed approval decision verified",
		"approval scope " + string(request.ApprovalProof.Scope),
	}, nil
}

func (s *service) executeAPILaunch(
	automation *models.Automation,
	request TaskLaunchRequest,
	intentID uuid.UUID,
	started time.Time,
	audit []string,
) launchExecution {
	method, target := parseLaunchMethodTarget(automation.LaunchTarget, http.MethodPost)
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return blockedLaunch("API launch target must be an absolute http or https URL", started, append(audit, "api target rejected"))
	}
	if parsed.User != nil {
		return blockedLaunch("API launch target must not contain credentials", started, append(audit, "api target credentials rejected"))
	}
	host := parsed.Hostname()
	allowedHosts := allowedCSVEnv("AUTOMATION_API_ALLOWED_HOSTS", defaultAPILaunchAllowedHosts)
	if !hostAllowed(host, allowedHosts) {
		return blockedLaunch("API launch host is not allowlisted; set AUTOMATION_API_ALLOWED_HOSTS deliberately to enable this target", started, append(audit, "api host rejected by allowlist"))
	}
	if unsafeAPILaunchHost(host) && !envEnabled("AUTOMATION_API_ALLOW_LINK_LOCAL") {
		return blockedLaunch("API launch target uses link-local, metadata, or unspecified address space", started, append(audit, "api network target rejected"))
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodPost {
		return blockedLaunch("API launch supports only GET, HEAD, or POST without a request body", started, append(audit, "api method rejected"))
	}
	client := s.directHTTPClient()
	executionContext := request.ExecutionContext
	if executionContext == nil {
		executionContext = context.Background()
	}
	req, err := http.NewRequestWithContext(executionContext, method, target, nil)
	if err != nil {
		return failedLaunch(err.Error(), started, append(audit, "api request creation failed"))
	}
	req.Header.Set("User-Agent", "018-HAI-Controlled-Launcher/1.0")
	_, authorizationAudit, err := s.authorizeExternalLaunch(
		automation,
		request,
		intentID,
		method,
	)
	if err != nil {
		return blockedLaunch(
			"unified execution authorization blocked API access: "+err.Error(),
			started,
			append(audit, "execution authorization rejected at the final network boundary"),
		)
	}
	audit = append(audit, authorizationAudit...)
	if safety.EmergencyStopActive() {
		return blockedLaunch(safety.EmergencyStopReason(), started, append(audit, "emergency stop rechecked before API network access"))
	}
	resp, err := client.Do(req)
	if err != nil {
		if executionContext.Err() != nil {
			return failedLaunch("API launch canceled before completion", started, append(audit, "api request canceled with its caller context"))
		}
		return failedLaunch(err.Error(), started, append(audit, "api request failed"))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	status := "completed"
	message := fmt.Sprintf("%s %s returned HTTP %d", method, safety.RedactURL(target), resp.StatusCode)
	expected := automation.ExpectedHTTPStatus
	if expected > 0 {
		if resp.StatusCode != expected {
			status = "failed"
			message = fmt.Sprintf("%s; expected HTTP %d", message, expected)
		}
	} else if resp.StatusCode >= 400 {
		status = "failed"
	}
	return launchExecution{
		Status:      status,
		Message:     message,
		Output:      trimOutput(body, 4096),
		ExitCode:    resp.StatusCode,
		DurationMs:  time.Since(started).Milliseconds(),
		AuditEvents: append(audit, "api request executed", "response captured with bounded output"),
	}
}

func (s *service) executeScriptLaunch(
	automation *models.Automation,
	request TaskLaunchRequest,
	intentID uuid.UUID,
	started time.Time,
	audit []string,
) launchExecution {
	if !envEnabled("AUTOMATION_SCRIPT_EXECUTION_ENABLED") {
		return blockedLaunch("Script execution is disabled; set AUTOMATION_SCRIPT_EXECUTION_ENABLED=true only after reviewing the allowlisted script folder", started, append(audit, "script execution blocked by policy"))
	}
	root := firstNonEmpty(os.Getenv("AUTOMATION_SCRIPT_DIR"), "/root/automation-scripts")
	scriptPath, err := resolveAllowedScriptPath(root, automation.LaunchTarget)
	if err != nil {
		return blockedLaunch(err.Error(), started, append(audit, "script target rejected"))
	}
	info, err := os.Stat(scriptPath)
	if err != nil {
		return failedLaunch(err.Error(), started, append(audit, "script file not found"))
	}
	if info.IsDir() {
		return blockedLaunch("script target is a directory", started, append(audit, "script target rejected"))
	}
	if !info.Mode().IsRegular() {
		return blockedLaunch("script target must be a regular file", started, append(audit, "script target rejected"))
	}
	if err := verifyPinnedScript(scriptPath); err != nil {
		return blockedLaunch(err.Error(), started, append(audit, "script hash pin rejected"))
	}
	timeoutSeconds := intEnv("AUTOMATION_SCRIPT_TIMEOUT_SECONDS", 30)
	executionContext := request.ExecutionContext
	if executionContext == nil {
		executionContext = context.Background()
	}
	ctx, cancel := context.WithTimeout(executionContext, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, scriptPath)
	processcontrol.Configure(cmd)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Env = safeScriptEnvironment(automation)
	_, authorizationAudit, err := s.authorizeExternalLaunch(
		automation,
		request,
		intentID,
		"",
	)
	if err != nil {
		return blockedLaunch(
			"unified execution authorization blocked local script execution: "+err.Error(),
			started,
			append(audit, "execution authorization rejected at the final process boundary"),
		)
	}
	audit = append(audit, authorizationAudit...)
	if safety.EmergencyStopActive() {
		return blockedLaunch(safety.EmergencyStopReason(), started, append(audit, "emergency stop rechecked before script process start"))
	}
	outputLimit := intEnv("AUTOMATION_SCRIPT_OUTPUT_LIMIT_BYTES", 4096)
	if outputLimit < 1024 {
		outputLimit = 1024
	}
	if outputLimit > 65536 {
		outputLimit = 65536
	}
	output := newBoundedOutput(outputLimit)
	cmd.Stdout = output
	cmd.Stderr = output
	err = cmd.Run()
	outputText := trimOutput(output.Bytes(), int64(outputLimit))
	if output.Truncated() {
		audit = append(audit, fmt.Sprintf("script output truncated at %d bytes", outputLimit))
	}
	if executionContext.Err() != nil {
		return failedLaunch("script execution canceled before completion", started, append(audit, "script process canceled with its caller context"))
	}
	if ctx.Err() == context.DeadlineExceeded {
		return launchExecution{
			Status:      "failed",
			Message:     fmt.Sprintf("script exceeded %d second timeout", timeoutSeconds),
			Output:      outputText,
			ExitCode:    -1,
			DurationMs:  time.Since(started).Milliseconds(),
			AuditEvents: append(audit, "script executed without shell", "script timed out"),
		}
	}
	if err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return launchExecution{
			Status:      "failed",
			Message:     err.Error(),
			Output:      outputText,
			ExitCode:    exitCode,
			DurationMs:  time.Since(started).Milliseconds(),
			AuditEvents: append(audit, "script executed without shell", "script returned non-zero exit"),
		}
	}
	return launchExecution{
		Status:      "completed",
		Message:     "script executed from allowlisted folder without shell expansion",
		Output:      outputText,
		ExitCode:    0,
		DurationMs:  time.Since(started).Milliseconds(),
		AuditEvents: append(audit, "script SHA-256 pin verified", "script executed without shell", "script completed"),
	}
}

func verifyPinnedScript(scriptPath string) error {
	expected, err := configuredScriptHash(filepath.Base(scriptPath))
	if err != nil {
		return err
	}
	file, err := os.Open(scriptPath)
	if err != nil {
		return fmt.Errorf("could not read script for SHA-256 verification: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("could not hash script for SHA-256 verification: %w", err)
	}
	actual := hash.Sum(nil)
	want, _ := hex.DecodeString(expected)
	if subtle.ConstantTimeCompare(actual, want) != 1 {
		return fmt.Errorf("script SHA-256 does not match the configured pin for %s", filepath.Base(scriptPath))
	}
	return nil
}

func configuredScriptHash(name string) (string, error) {
	const envName = "AUTOMATION_SCRIPT_SHA256_ALLOWLIST"
	raw := strings.TrimSpace(os.Getenv(envName))
	if raw == "" {
		return "", fmt.Errorf("script execution requires a reviewed SHA-256 pin in %s", envName)
	}
	name = filepath.Base(strings.TrimSpace(name))
	for _, entry := range strings.Split(raw, ",") {
		file, hash, ok := strings.Cut(strings.TrimSpace(entry), "=")
		file = filepath.Base(strings.TrimSpace(file))
		hash = strings.ToLower(strings.TrimSpace(hash))
		if !ok || file == "." || file == "" || len(hash) != 64 {
			return "", fmt.Errorf("%s must contain comma-separated basename=SHA-256 pins", envName)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return "", fmt.Errorf("%s contains an invalid SHA-256 pin", envName)
		}
		if file == name {
			return hash, nil
		}
	}
	return "", fmt.Errorf("script %s is not present in %s", name, envName)
}

func (s *service) executeDockerLaunch(
	automation *models.Automation,
	request TaskLaunchRequest,
	intentID uuid.UUID,
	started time.Time,
	audit []string,
) launchExecution {
	if strings.ToLower(os.Getenv("AUTOMATION_DOCKER_CONTROL_ENABLED")) != "true" {
		return launchExecution{
			Status:           "blocked",
			Message:          "Docker control is disabled; set AUTOMATION_DOCKER_CONTROL_ENABLED=true and mount the Docker socket to enable it",
			ExitCode:         -1,
			DurationMs:       time.Since(started).Milliseconds(),
			RequiresApproval: true,
			AuditEvents:      append(audit, "docker control blocked by policy"),
		}
	}
	containerName := strings.TrimSpace(firstNonEmpty(automation.ServiceName, automation.LaunchTarget))
	if containerName == "" {
		return blockedLaunch("Docker launch requires serviceName or launchTarget", started, append(audit, "docker target missing"))
	}
	if !tokenAllowed(containerName, allowedCSVEnv("AUTOMATION_DOCKER_ALLOWED_CONTAINERS", "")) {
		return blockedLaunch("Docker container is not allowlisted; set AUTOMATION_DOCKER_ALLOWED_CONTAINERS deliberately to enable this target", started, append(audit, "docker target rejected by allowlist"))
	}
	socketPath := firstNonEmpty(os.Getenv("AUTOMATION_DOCKER_SOCKET"), "/var/run/docker.sock")
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	endpoint := "http://docker/containers/" + url.PathEscape(containerName) + "/start"
	executionContext := request.ExecutionContext
	if executionContext == nil {
		executionContext = context.Background()
	}
	ctx, cancel := context.WithTimeout(executionContext, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return failedLaunch(err.Error(), started, append(audit, "docker request creation failed"))
	}
	_, authorizationAudit, err := s.authorizeExternalLaunch(
		automation,
		request,
		intentID,
		"",
	)
	if err != nil {
		return blockedLaunch(
			"unified execution authorization blocked Docker control: "+err.Error(),
			started,
			append(audit, "execution authorization rejected at the final Docker boundary"),
		)
	}
	audit = append(audit, authorizationAudit...)
	if safety.EmergencyStopActive() {
		return blockedLaunch(safety.EmergencyStopReason(), started, append(audit, "emergency stop rechecked before Docker socket access"))
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return failedLaunch("Docker start request was canceled and stopped", started, append(audit, "docker socket request canceled with its caller context"))
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return failedLaunch("Docker start request exceeded its timeout and was stopped", started, append(audit, "docker socket request stopped at its timeout"))
		}
		return failedLaunch(err.Error(), started, append(audit, "docker socket request failed"))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return launchExecution{
			Status:      "completed",
			Message:     fmt.Sprintf("Docker container %s start request accepted", containerName),
			Output:      strings.TrimSpace(string(body)),
			ExitCode:    resp.StatusCode,
			DurationMs:  time.Since(started).Milliseconds(),
			AuditEvents: append(audit, "docker start request executed through Docker API"),
		}
	}
	return launchExecution{
		Status:      "failed",
		Message:     fmt.Sprintf("Docker API returned HTTP %d for container %s", resp.StatusCode, containerName),
		Output:      strings.TrimSpace(string(body)),
		ExitCode:    resp.StatusCode,
		DurationMs:  time.Since(started).Milliseconds(),
		AuditEvents: append(audit, "docker start request failed"),
	}
}

func (s *service) processImageFile(file *multipart.FileHeader) (string, error) {
	if file.Size > config.AppConfig.ImageMaxSize {
		return "", fmt.Errorf("image is too large (%d). Max size is %d Mb", file.Size, config.AppConfig.ImageMaxSize)
	}

	ext := filepath.Ext(file.Filename)
	if !contains(config.AppConfig.ImageExtensions, ext) {
		return "", fmt.Errorf("invalid image extension. Allowed extensions are: %v", config.AppConfig.ImageExtensions)
	}

	src, err := file.Open()
	if err != nil {
		log.Printf("Failed to open the file: %v", err)
		return "", err
	}
	defer src.Close()

	buffer := make([]byte, 512)
	bytesRead, err := src.Read(buffer)
	if err != nil && err != io.EOF {
		return "", err
	}
	if bytesRead == 0 {
		return "", fmt.Errorf("file is empty")
	}

	fileType := http.DetectContentType(buffer[:bytesRead])
	if !strings.HasPrefix(fileType, "image/") {
		return "", fmt.Errorf("file is not an image")
	}
	mimeSuffix := strings.TrimPrefix(fileType, "image/")
	if !contains(config.AppConfig.ImageExtensions, "."+mimeSuffix) {
		return "", fmt.Errorf("mismatch between file extension and MIME type")
	}

	_, err = src.Seek(0, 0)
	if err != nil {
		return "", err
	}

	_, _, err = image.Decode(src)
	if err != nil {
		return "", fmt.Errorf("corrupted image: %w", err)
	}

	_, err = src.Seek(0, 0)
	if err != nil {
		return "", err
	}

	newFileName := uuid.New().String() + ext
	fullPath, err := resolveImagePath(newFileName)
	if err != nil {
		return "", err
	}
	dst, err := os.Create(fullPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	n, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("Failed to copy file: %v", err)
		return "", err
	}
	log.Printf("Processed automation image upload: %d bytes stored", n)
	return newFileName, nil
}

func (s *service) deleteImage(imageName string) error {
	if imageName == "" {
		return nil
	}
	imagePath, err := resolveImagePath(imageName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(imagePath)
}

func resolveImagePath(imageName string) (string, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return "", fmt.Errorf("image name is required")
	}
	if strings.ContainsAny(imageName, `/\`) || strings.Contains(imageName, "..") || imageName != filepath.Base(imageName) {
		return "", fmt.Errorf("image name must be a single safe file name")
	}
	if ext := strings.ToLower(filepath.Ext(imageName)); ext == "" || !contains(config.AppConfig.ImageExtensions, ext) {
		return "", fmt.Errorf("image extension is not allowed")
	}
	if strings.TrimSpace(config.AppConfig.ImageSaveDir) == "" {
		return "", fmt.Errorf("image upload directory is not configured")
	}
	root, err := filepath.Abs(filepath.Clean(config.AppConfig.ImageSaveDir))
	if err != nil {
		return "", err
	}
	imagePath := filepath.Join(root, imageName)
	rel, err := filepath.Rel(root, imagePath)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("image path must stay inside upload directory")
	}
	return imagePath, nil
}

func contains(slice []string, str string) bool {
	str = strings.ToLower(strings.TrimSpace(str))
	for _, v := range slice {
		if strings.ToLower(strings.TrimSpace(v)) == str {
			return true
		}
	}
	return false
}

func (s *service) ensureUniqueURLPath(automation *models.Automation) error {
	baseURLPath := util.GenerateURLPath(automation.Name)
	uniqueURLPath := baseURLPath
	counter := 0

	for {
		existingAutomation, err := s.repo.GetByURLPath(uniqueURLPath)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		if existingAutomation == nil || existingAutomation.ID == automation.ID {
			break
		}

		counter++
		uniqueURLPath = fmt.Sprintf("%s-%d", baseURLPath, counter)
	}

	automation.URLPath = uniqueURLPath
	return nil
}

func (s *service) applyAutomationDefaults(automation *models.Automation) {
	if automation.LaunchType == "" {
		automation.LaunchType = "browser_url"
	}
	if automation.HealthCheckType == "" {
		automation.HealthCheckType = "http"
	}
	if automation.HealthCheckIntervalSeconds == 0 {
		automation.HealthCheckIntervalSeconds = 60
	}
	if automation.ExpectedHTTPStatus == 0 {
		automation.ExpectedHTTPStatus = http.StatusOK
	}
	if automation.Status == "" {
		automation.Status = "unknown"
	}
	if automation.RoutePath == "" {
		automation.RoutePath = automation.URLPath
	}
	if automation.LaunchTarget == "" {
		if automation.PublicURL != "" {
			automation.LaunchTarget = automation.PublicURL
		} else if automation.LocalURL != "" {
			automation.LaunchTarget = automation.LocalURL
		} else {
			automation.LaunchTarget = fmt.Sprintf("/%s", automation.URLPath)
		}
	}
	if automation.HealthCheckURL == "" && automation.Host != "" && automation.Port > 0 {
		automation.HealthCheckURL = fmt.Sprintf("http://%s:%d", automation.Host, automation.Port)
	}
}

func classifyFailure(failures int) string {
	if failures >= 3 {
		return "broken"
	}
	if failures >= 2 {
		return "degraded"
	}
	return "warning"
}

func boolStatus(value bool) string {
	if value {
		return "ok"
	}
	return "missing"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func parseLaunchMethodTarget(value, defaultMethod string) (string, string) {
	trimmed := strings.TrimSpace(value)
	fields := strings.Fields(trimmed)
	if len(fields) >= 2 {
		method := strings.ToUpper(fields[0])
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		if isHTTPMethodToken(method) && target != "" {
			return method, target
		}
	}
	return defaultMethod, trimmed
}

func isHTTPMethodToken(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 'A' || value[i] > 'Z' {
			return false
		}
	}
	return true
}

func redactLaunchTarget(value string) string {
	method, target := parseLaunchMethodTarget(value, "")
	if method != "" && target != "" {
		return method + " " + safety.RedactURL(target)
	}
	return safety.RedactURL(value)
}

func resolveAllowedScriptPath(root, target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("script launch target is required")
	}
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("script allowlist folder is not accessible: %w", err)
	}
	rootAbs, err = filepath.Abs(rootResolved)
	if err != nil {
		return "", err
	}
	target = strings.TrimSpace(target)
	if strings.ContainsAny(target, "\r\n\x00") {
		return "", fmt.Errorf("script launch target contains invalid characters")
	}
	var targetAbs string
	if filepath.IsAbs(target) {
		targetAbs, err = filepath.Abs(filepath.Clean(target))
	} else {
		targetAbs, err = filepath.Abs(filepath.Join(rootAbs, filepath.Clean(target)))
	}
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("script target must stay inside allowlisted folder %s", rootAbs)
	}
	if resolvedTarget, err := filepath.EvalSymlinks(targetAbs); err == nil {
		resolvedAbs, err := filepath.Abs(resolvedTarget)
		if err != nil {
			return "", err
		}
		rel, err = filepath.Rel(rootAbs, resolvedAbs)
		if err != nil {
			return "", err
		}
		if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." || filepath.IsAbs(rel) {
			return "", fmt.Errorf("script target must not resolve outside allowlisted folder %s", rootAbs)
		}
		targetAbs = resolvedAbs
	}
	return targetAbs, nil
}

func blockedLaunch(message string, started time.Time, audit []string) launchExecution {
	return launchExecution{
		Status:           "blocked",
		Message:          message,
		ExitCode:         -1,
		DurationMs:       time.Since(started).Milliseconds(),
		RequiresApproval: true,
		AuditEvents:      audit,
	}
}

func failedLaunch(message string, started time.Time, audit []string) launchExecution {
	return launchExecution{
		Status:      "failed",
		Message:     safety.RedactSecrets(message),
		ExitCode:    -1,
		DurationMs:  time.Since(started).Milliseconds(),
		AuditEvents: audit,
	}
}

func trimOutput(output []byte, limit int64) string {
	if int64(len(output)) > limit {
		output = output[:limit]
	}
	return strings.TrimSpace(safety.RedactSecrets(string(output)))
}

type boundedOutput struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func newBoundedOutput(limit int) *boundedOutput {
	return &boundedOutput{data: make([]byte, 0, limit), limit: limit}
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written := len(p)
	remaining := w.limit - len(w.data)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		w.data = append(w.data, p[:remaining]...)
	}
	if remaining < len(p) {
		w.truncated = true
	}
	return written, nil
}

func (w *boundedOutput) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.data...)
}

func (w *boundedOutput) Truncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncated
}

func intEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "true" || value == "1" || value == "yes"
}

func allowedCSVEnv(name, fallback string) map[string]bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		raw = fallback
	}
	allowed := map[string]bool{}
	for _, token := range strings.Split(raw, ",") {
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "" {
			allowed[token] = true
		}
	}
	return allowed
}

func hostAllowed(host string, allowed map[string]bool) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	return allowed["*"] || allowed[host]
}

func tokenAllowed(value string, allowed map[string]bool) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	return allowed["*"] || allowed[value]
}

type automationLookupIPAddr func(context.Context, string) ([]net.IPAddr, error)

func noRedirectHTTPClient(timeout time.Duration) *http.Client {
	return noRedirectHTTPClientWithResolver(timeout, net.DefaultResolver.LookupIPAddr)
}

func noRedirectHTTPClientWithResolver(timeout time.Duration, lookup automationLookupIPAddr) *http.Client {
	dialTimeout := timeout
	if dialTimeout <= 0 || dialTimeout > 10*time.Second {
		dialTimeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                 nil,
			MaxIdleConns:          16,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: dialTimeout,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, fmt.Errorf("automation: invalid network address: %w", err)
				}
				resolved, err := lookup(ctx, host)
				if err != nil {
					return nil, fmt.Errorf("automation: resolve network target: %w", err)
				}
				if len(resolved) == 0 {
					return nil, fmt.Errorf("automation: network target resolved to no addresses")
				}
				for _, candidate := range resolved {
					if automationIPAddressBlocked(candidate.IP) {
						return nil, fmt.Errorf("automation: network target resolved to blocked address space")
					}
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(resolved[0].IP.String(), port))
			},
		},
	}
}

func (s *service) directHTTPClient() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return noRedirectHTTPClient(10 * time.Second)
}

func automationIPAddressBlocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.String() == "169.254.169.254"
}

func networkTargetBlockedReason(host, allowedHostsEnv, fallbackAllowedHosts, allowLinkLocalEnv string) string {
	if !hostAllowed(host, allowedCSVEnv(allowedHostsEnv, fallbackAllowedHosts)) {
		return fmt.Sprintf("network target host %s is not allowlisted; set %s deliberately to enable this target", host, allowedHostsEnv)
	}
	if unsafeAPILaunchHost(host) && !envEnabled(allowLinkLocalEnv) {
		return "network target uses link-local, metadata, or unspecified address space"
	}
	return ""
}

func safeScriptEnvironment(automation *models.Automation) []string {
	env := []string{
		"HAI_AUTOMATION_ID=" + automation.ID.String(),
		"HAI_AUTOMATION_NAME=" + automation.Name,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	for _, key := range strings.Split(os.Getenv("AUTOMATION_SCRIPT_ENV_ALLOWLIST"), ",") {
		key = strings.TrimSpace(key)
		if !validEnvKey(key) {
			continue
		}
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for index, r := range key {
		letter := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		digit := r >= '0' && r <= '9'
		if letter || index > 0 && (digit || r == '_') {
			continue
		}
		return false
	}
	return true
}

func unsafeAPILaunchHost(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsUnspecified() || ip.IsLinkLocalUnicast()
}
