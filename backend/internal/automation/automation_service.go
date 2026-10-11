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
	"net/http/httptrace"
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
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/util"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultAPILaunchAllowedHosts = "localhost,127.0.0.1,::1,backend,frontend,gateway,generic-auto,idp"
	maxScriptBytes               = 16 << 20
	maxScriptTimeoutSeconds      = 3600
	scriptWaitDelay              = 250 * time.Millisecond
	maxAutomationOutputCapture   = 1 << 20
	omittedAutomationOutput      = "[output omitted: capture limit exceeded before safe redaction]"
)

var (
	ErrLaunchIdempotencyKeyRequired = errors.New("an idempotency key is required for effectful automation launches")
	ErrLaunchIdempotencyKeyInvalid  = errors.New("automation launch idempotency key must be 1 to 256 visible ASCII characters")
	ErrLaunchIdempotencyConflict    = errors.New("automation launch idempotency key was already used for a different action")
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
	AutomationID       uuid.UUID                           `json:"automationId"`
	LaunchEventID      uuid.UUID                           `json:"launchEventId,omitempty"`
	RuntimeTaskID      string                              `json:"runtimeTaskId,omitempty"`
	ExecutionReference string                              `json:"executionReference,omitempty"`
	RuntimeType        string                              `json:"runtimeType,omitempty"`
	LaunchType         string                              `json:"launchType"`
	Target             string                              `json:"target"`
	Status             string                              `json:"status"`
	Message            string                              `json:"message,omitempty"`
	Output             string                              `json:"output,omitempty"`
	RuntimeRouteTrace  *models.AutomationRuntimeRouteTrace `json:"runtimeRouteTrace,omitempty"`
	ExitCode           int                                 `json:"exitCode"`
	DurationMs         int64                               `json:"durationMs"`
	RequiresApproval   bool                                `json:"requiresApproval"`
	AuditEvents        []string                            `json:"auditEvents"`
	LaunchedAt         time.Time                           `json:"launchedAt"`
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
	Status             string
	Message            string
	Output             string
	RuntimeRouteTrace  *models.AutomationRuntimeRouteTrace
	ExitCode           int
	DurationMs         int64
	RequiresApproval   bool
	RuntimeTaskID      string
	ExecutionReference string
	AuditEvents        []string
}

type TaskLaunchRequest struct {
	IdempotencyKey string                  `json:"idempotencyKey,omitempty"`
	OwnerIdentity  string                  `json:"-"`
	ActorIdentity  string                  `json:"-"`
	ActorKind      executionauth.ActorKind `json:"-"`
	TaskID         string                  `json:"-"`
	Task           string                  `json:"task,omitempty"`
	ProjectKey     string                  `json:"projectKey,omitempty"`
	// MandateID is a reference only. The execution-authorization service
	// resolves it by verified owner and evaluates its exact bounded scope.
	MandateID             string                           `json:"mandateId,omitempty"`
	ApprovalSourceID      string                           `json:"-"`
	ApprovalBindingDigest string                           `json:"-"`
	Governance            executionauth.GovernanceEvidence `json:"-"`
	ExecutionContext      context.Context                  `json:"-"`
	ApprovalProof         *ApprovalProof                   `json:"-"`
	launchActionDigest    string
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
	StopRuntimeTaskForOwnerContext(context.Context, uuid.UUID, string) (*agentruntime.StopResult, error)
	Diagnostics(id uuid.UUID) (*DiagnosticResult, error)
	DiagnosticsForOwner(id uuid.UUID, ownerIdentity string) (*DiagnosticResult, error)
}

type service struct {
	repo            Repository
	publisher       events.Publisher
	runtimeRegistry *agentruntime.Registry
	approvalProofs  ApprovalProofService
	executionAuth   ExecutionAuthorizer
	finalEffects    *executionauth.FinalEffectBridge
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
	if err := rejectMaskedAutomationConfiguration(automation); err != nil {
		return nil, err
	}
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

	automationCreated, err := s.repo.Create(automation)
	if err != nil {
		return nil, err
	}
	event := &events.AutomationEvent{
		Type:       events.CreateEvent,
		Automation: automationCreated,
	}
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
	if currentAutomation == nil {
		return nil, gorm.ErrRecordNotFound
	}
	if err := preserveAutomationCredentials(automation, currentAutomation); err != nil {
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

	automationUpdated, err := s.repo.Update(automation)
	if err != nil {
		return nil, err
	}
	automationUpdated.OldUrlPath = oldUrlPath

	event := &events.AutomationEvent{
		Type:       events.UpdateEvent,
		Automation: automationUpdated,
	}

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

	err = s.repo.Delete(id)
	if err != nil {
		return err
	}

	event := &events.AutomationEvent{
		Type:       events.DeleteEvent,
		Automation: automation,
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
		host := strings.Trim(automation.Host, "[]")
		target = automationHostPort(host, automation.Port)
		if reason := networkTargetBlockedReason(host, "AUTOMATION_HEALTH_ALLOWED_HOSTS", defaultAPILaunchAllowedHosts, "AUTOMATION_HEALTH_ALLOW_LINK_LOCAL"); reason != "" {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = reason
			break
		}
		checkContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		addresses, errResolve := resolveAutomationTargetAddresses(checkContext, host, envEnabled("AUTOMATION_HEALTH_ALLOW_LINK_LOCAL"), net.DefaultResolver.LookupIP)
		if errResolve != nil {
			cancel()
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = "health-check destination could not be safely resolved"
			break
		}
		conn, errDial := dialAutomationTarget(checkContext, "tcp", target, host, addresses)
		cancel()
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
			target = "http://" + automationHostPort(strings.Trim(automation.Host, "[]"), automation.Port)
		}
		parsed, errParse := url.Parse(target)
		if errParse != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = "health check target must be an absolute http or https URL"
			break
		}
		if reason := networkTargetBlockedReason(parsed.Hostname(), "AUTOMATION_HEALTH_ALLOWED_HOSTS", defaultAPILaunchAllowedHosts, "AUTOMATION_HEALTH_ALLOW_LINK_LOCAL"); reason != "" {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = reason
			break
		}
		checkContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		addresses, errResolve := resolveAutomationTargetAddresses(checkContext, parsed.Hostname(), envEnabled("AUTOMATION_HEALTH_ALLOW_LINK_LOCAL"), net.DefaultResolver.LookupIP)
		if errResolve != nil {
			cancel()
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = "health-check destination could not be safely resolved"
			break
		}
		client := noRedirectHTTPClientForTarget(10*time.Second, parsed.Hostname(), addresses)
		req, errRequest := http.NewRequestWithContext(checkContext, http.MethodGet, target, nil)
		if errRequest != nil {
			cancel()
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = "health-check request could not be created"
			break
		}
		resp, errGet := client.Do(req)
		cancel()
		if errGet != nil {
			status = classifyFailure(automation.ConsecutiveFailures + 1)
			failureReason = redactAutomationRequestError(errGet)
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

	failureReason = safety.RedactSecrets(failureReason)
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
		Target:              safety.RedactURL(target),
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
	return s.actionApprovalRequiredForConfiguration(id, automation)
}

func (s *service) InspectReviewConfiguration(id uuid.UUID) (*ReviewConfigurationSnapshot, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("automation repository is unavailable")
	}
	stored, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	return s.reviewConfigurationSnapshot(id, stored)
}

func (s *service) RecordApprovalDecision(id uuid.UUID, request TaskApprovalDecisionRequest) error {
	return s.recordApprovalDecisionWithRepository(context.Background(), s.repo, id, request)
}

func (s *service) recordApprovalDecisionWithRepository(ctx context.Context, repo Repository, id uuid.UUID, request TaskApprovalDecisionRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if repo == nil {
		return fmt.Errorf("automation repository is unavailable")
	}
	kind, _, err := approvalSourceKind(request.ApprovalSourceID)
	if err != nil {
		return err
	}
	if kind != "task-review" {
		return fmt.Errorf("only a verified task-review decision can be registered")
	}
	if err := ValidateTaskApprovalDecisionRequest(request, time.Now().UTC()); err != nil {
		return err
	}
	if strings.TrimSpace(request.ApprovalBindingDigest) == "" {
		return fmt.Errorf("task review request binding digest is required")
	}
	if err := ValidateReviewConfigurationSnapshot(request.ReviewConfiguration, id); err != nil {
		return err
	}
	reviewed := *request.ReviewConfiguration
	stored, err := repo.FindByID(id)
	if ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	if err != nil {
		return err
	}
	if stored == nil || stored.ID != id {
		return fmt.Errorf("automation configuration target does not match")
	}
	configuration := *stored
	automation := &configuration
	s.applyAutomationDefaults(automation)
	scope, required := approvalScopeForAutomation(automation)
	if !required {
		return fmt.Errorf("automation action does not have a supported approval scope")
	}
	// Derive the comparison and registered action from the same policy read.
	policySnapshot := approvalPolicySnapshot()
	if reviewed.Scope != scope || reviewed.ConfigurationDigest != automationActionDigestWithPolicy(automation, TaskLaunchRequest{}, policySnapshot) {
		return fmt.Errorf("automation configuration or execution policy changed since task review; create a new review")
	}
	approvedAt := request.ApprovedAt.UTC().Truncate(time.Microsecond)
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
		ActionDigest:  automationActionDigestWithPolicy(automation, launchRequest, policySnapshot),
		Scope:         scope,
		ApprovedAt:    approvedAt,
	}
	if err := validateApprovalDecisionFreshness(record, time.Now().UTC()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := repo.SaveApprovalDecision(record); err != nil {
		return errors.Join(ErrApprovalRegistrationUnconfirmed, err, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrApprovalRegistrationUnconfirmed, err)
	}
	acknowledged, err := repo.FindApprovalDecision(record.SourceID)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return errors.Join(ErrApprovalRegistrationUnconfirmed, err)
	}
	if !sameApprovalDecision(acknowledged, record) {
		return errors.Join(ErrApprovalRegistrationUnconfirmed, ErrApprovalDecisionMissing)
	}
	return nil
}

func (s *service) IssueApprovalProof(id uuid.UUID, request TaskApprovalProofRequest) (*ApprovalProof, error) {
	return s.issueApprovalProofWithRepository(context.Background(), s.repo, id, request, false)
}

func (s *service) issueApprovalProofWithRepository(ctx context.Context, repo Repository, id uuid.UUID, request TaskApprovalProofRequest, owned bool) (*ApprovalProof, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if repo == nil {
		return nil, fmt.Errorf("automation repository is unavailable")
	}
	automation, err := repo.FindByID(id)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, errors.Join(err, contextErr)
	}
	if err != nil {
		return nil, err
	}
	if automation == nil || id == uuid.Nil || automation.ID != id {
		return nil, fmt.Errorf("automation proof configuration target does not match")
	}
	configuration := *automation
	automation = &configuration
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := repo.FindApprovalDecision(launchRequest.ApprovalSourceID)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, errors.Join(err, contextErr)
	}
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
	issueRequest := ApprovalProofIssueRequest{
		OwnerIdentity:    launchRequest.OwnerIdentity,
		AutomationID:     automation.ID,
		ActionDigest:     expectedDigest,
		Scope:            scope,
		ApprovalSourceID: launchRequest.ApprovalSourceID,
		TTL:              proofTTL,
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.approvalProofs == nil {
		return nil, fmt.Errorf("approval proof signer is unavailable")
	}
	var proof *ApprovalProof
	if owned {
		signer, ok := s.approvalProofs.(ContextualApprovalProofSigner)
		if !ok {
			return nil, ErrApprovalIssuanceContextUnavailable
		}
		proof, err = signer.IssueContext(ctx, issueRequest)
	} else {
		proof, err = s.approvalProofs.Issue(issueRequest)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, errors.Join(err, contextErr)
	}
	return proof, err
}

func (s *service) StopRuntimeTask(id uuid.UUID) (*agentruntime.StopResult, error) {
	return s.stopRuntimeTask(id, "")
}

func (s *service) StopRuntimeTaskForOwner(id uuid.UUID, ownerIdentity string) (*agentruntime.StopResult, error) {
	return s.stopRuntimeTask(id, ownerIdentity)
}

func (s *service) stopRuntimeTask(id uuid.UUID, ownerIdentity string) (*agentruntime.StopResult, error) {
	return s.stopRuntimeTaskContext(context.Background(), id, ownerIdentity, false)
}

func (s *service) StopRuntimeTaskForOwnerContext(ctx context.Context, id uuid.UUID, ownerIdentity string) (*agentruntime.StopResult, error) {
	if ctx == nil || strings.TrimSpace(ownerIdentity) == "" {
		return nil, ErrLaunchConfigurationContextUnavailable
	}
	return s.stopRuntimeTaskContext(ctx, id, ownerIdentity, true)
}

func (s *service) stopRuntimeTaskContext(ctx context.Context, id uuid.UUID, ownerIdentity string, owned bool) (*agentruntime.StopResult, error) {
	automation, err := s.launchConfiguration(ctx, id, owned)
	if err != nil {
		return nil, err
	}
	configuration := *automation
	automation = &configuration
	s.applyAutomationDefaults(automation)
	started := time.Now().UTC()
	repo, admissionCtx, cancelAdmission, err := s.launchAdmissionRepository(ctx, owned)
	if err != nil {
		return nil, err
	}
	defer cancelAdmission()
	if err := admissionCtx.Err(); err != nil {
		return nil, err
	}
	runtimeID := strings.ToLower(strings.TrimSpace(automation.RuntimeType))
	taskID := ""
	persist := func(result *agentruntime.StopResult, intentIDs ...uuid.UUID) (uuid.UUID, error) {
		outcomeRepo, outcomeCtx, cancel, err := s.launchOutcomeRepository(ctx, owned)
		if err != nil {
			return uuid.Nil, err
		}
		defer cancel()
		if err := outcomeCtx.Err(); err != nil {
			return uuid.Nil, err
		}
		id, err := s.persistRuntimeStopEventUsing(outcomeRepo, automation, result, started, ownerIdentity, intentIDs...)
		return id, errors.Join(err, outcomeCtx.Err())
	}
	replay := func(intent *models.AutomationLaunchEvent) (*agentruntime.StopResult, error) {
		result, err := s.replayRuntimeStopIntentUsing(repo, automation, strings.TrimSpace(ownerIdentity), runtimeID, taskID, intent)
		return result, errors.Join(err, admissionCtx.Err())
	}
	executionReference := ""
	if automation.LaunchType != "agent_runtime" || !isSupportedAgentRuntime(runtimeID) {
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
		_, _ = persist(result)
		return result, nil
	}
	owner := strings.TrimSpace(ownerIdentity)
	if owner == "" {
		result := &agentruntime.StopResult{RuntimeID: runtimeID, Status: "blocked", Message: "authenticated owner identity is required to resolve a runtime task", AuditEvents: []string{"ownerless stop rejected before launch lookup"}}
		_, _ = persist(result)
		return result, nil
	}
	launch, lookupErr := repo.FindOwnerActiveRuntimeLaunch(automation.ID, runtimeID, owner)
	if err := admissionCtx.Err(); err != nil {
		return nil, errors.Join(err, lookupErr)
	}
	outcomeLookupFailed := lookupErr != nil
	pendingBinding := lookupErr != nil || launch == nil
	if lookupErr != nil {
		// A pending intent is independently usable only when it supplies the
		// exact owner/task binding and has no matching task outcome.
		launch, lookupErr = repo.FindPendingRuntimeLaunchIntent(automation.ID, owner)
	} else if launch == nil {
		launch, lookupErr = repo.FindPendingRuntimeLaunchIntent(automation.ID, owner)
	}
	if err := admissionCtx.Err(); err != nil {
		return nil, errors.Join(err, lookupErr)
	}
	if lookupErr != nil {
		result := &agentruntime.StopResult{RuntimeID: runtimeID, Status: "indeterminate", Message: "owner-bound pending launch lookup failed; no task identity was substituted and no cancellation was attempted", AuditEvents: []string{"pending launch intent lookup failed; this is not evidence that no task is running"}}
		_, _ = persist(result)
		return result, nil
	}
	if launch == nil {
		if outcomeLookupFailed {
			result := &agentruntime.StopResult{RuntimeID: runtimeID, Status: "indeterminate", Message: "owner-bound runtime launch lookup failed; no task identity was substituted and absence of a task was not inferred", AuditEvents: []string{"runtime outcome lookup failed; pending-intent lookup found no exact binding, so cancellation was not attempted"}}
			_, _ = persist(result)
			return result, nil
		}
		result := &agentruntime.StopResult{RuntimeID: runtimeID, Status: "blocked", Message: "no owner-bound active launch or pending intent was found; no runtime task identity was substituted", AuditEvents: []string{"owner-bound launch and intent lookups completed without a cancellable task"}}
		_, _ = persist(result)
		return result, nil
	}
	if launch.AutomationID != automation.ID || strings.TrimSpace(launch.OwnerIdentity) != owner || strings.ToLower(strings.TrimSpace(launch.RuntimeType)) != runtimeID || strings.TrimSpace(launch.RuntimeTaskID) == "" {
		result := &agentruntime.StopResult{RuntimeID: runtimeID, Status: "indeterminate", Message: "stored runtime launch identity did not match the authenticated owner; no cancellation was attempted", AuditEvents: []string{"runtime task owner, automation, runtime, or task binding mismatch"}}
		_, _ = persist(result)
		return result, nil
	}
	expectedKind := "agent_runtime"
	if pendingBinding {
		expectedKind = "agent_runtime_intent"
	}
	if launch.ID == uuid.Nil || launch.LaunchType != expectedKind || (pendingBinding && launch.Status != "pending") {
		result := &agentruntime.StopResult{RuntimeID: runtimeID, Status: "indeterminate", Message: "stored runtime record kind or identity did not match the lookup; no cancellation was attempted", AuditEvents: []string{"runtime stop rejected an invalid record identity, kind, or pending state"}}
		_, _ = persist(result)
		return result, nil
	}
	if launch.LaunchType == "agent_runtime" && !activeRuntimeLaunchStatus(launch.Status, runtimeID) {
		result := &agentruntime.StopResult{RuntimeID: runtimeID, Status: "blocked", Message: "the owner-bound runtime launch is not in a cancellable state", AuditEvents: []string{"terminal runtime launch was not cancelled"}}
		_, _ = persist(result)
		return result, nil
	}
	taskID = strings.TrimSpace(launch.RuntimeTaskID)
	executionReference = strings.TrimSpace(launch.ExecutionReference)
	stopRequestKey := runtimeStopRequestEventKey(automation.ID, owner, taskID)
	existingStopIntent, lookupErr := repo.FindLaunchIntentByEventKey(stopRequestKey)
	if err := admissionCtx.Err(); err != nil {
		return nil, errors.Join(err, lookupErr)
	}
	if lookupErr != nil {
		return nil, fmt.Errorf("resolve runtime stop idempotency key: %w", lookupErr)
	}
	if existingStopIntent != nil {
		return replay(existingStopIntent)
	}
	// Older releases did not assign a stable request key to stop intents. Never
	// dispatch again while one of those historical attempts is unresolved.
	unresolvedStopIntent, lookupErr := repo.FindUnresolvedRuntimeStopIntent(automation.ID, owner, taskID)
	if err := admissionCtx.Err(); err != nil {
		return nil, errors.Join(err, lookupErr)
	}
	if lookupErr != nil {
		return nil, fmt.Errorf("resolve unresolved runtime stop intent: %w", lookupErr)
	}
	if unresolvedStopIntent != nil {
		return replay(unresolvedStopIntent)
	}
	if s.runtimeRegistry == nil {
		result := &agentruntime.StopResult{
			RuntimeID:   runtimeID,
			TaskID:      taskID,
			Status:      "blocked",
			Message:     "agent runtime registry is not configured",
			AuditEvents: []string{"agent runtime registry unavailable"},
		}
		_, _ = persist(result)
		return result, nil
	}
	if runtimeID == "openclaw" && !s.runtimeRegistry.OpenClawGatewayDelegationReady() {
		// Older CLI intents could be stamped with an ocgw reference even though
		// no Gateway run existed. CLI cancellation is bound to the persisted task
		// ID and local registry context; never route that legacy value to Gateway.
		executionReference = ""
	}
	intent := &models.AutomationLaunchEvent{
		ID:                 uuid.New(),
		AutomationID:       automation.ID,
		OwnerIdentity:      strings.TrimSpace(ownerIdentity),
		RuntimeType:        runtimeID,
		LaunchType:         "agent_runtime_stop_intent",
		EventKey:           stopRequestKey,
		RuntimeTaskID:      taskID,
		ExecutionReference: executionReference,
		Target:             redactLaunchTarget(automation.LaunchTarget),
		Status:             "pending",
		Message:            "immutable runtime stop intent recorded",
		AuditEvents: []string{
			"owner-bound runtime stop intent persisted before cancellation",
		},
		StartedAt:   started,
		CompletedAt: started,
	}
	errIntent := repo.SaveLaunchIntent(intent)
	if contextErr := admissionCtx.Err(); contextErr != nil {
		return &agentruntime.StopResult{RuntimeID: runtimeID, TaskID: taskID, ExecutionReference: executionReference, Status: "indeterminate", EvidenceURI: "automation-launch://" + intent.ID.String(), Message: "stop intent storage acknowledgement is uncertain; no cancellation was dispatched; inspect this candidate before retrying"}, errors.Join(contextErr, errIntent)
	}
	if errIntent != nil {
		existing, lookupErr := repo.FindLaunchIntentByEventKey(stopRequestKey)
		if err := admissionCtx.Err(); err != nil {
			return nil, errors.Join(err, lookupErr, errIntent)
		}
		if lookupErr == nil && existing != nil {
			return replay(existing)
		}
		if lookupErr != nil {
			return nil, fmt.Errorf("persist runtime stop intent: %w; idempotency lookup failed: %v", errIntent, lookupErr)
		}
		return nil, fmt.Errorf("persist runtime stop intent: %w", errIntent)
	}
	if owned {
		cancelAdmission()
	}
	result := s.runtimeRegistry.StopTaskWithReference(ctx, runtimeID, taskID, ownerIdentity, executionReference)
	result.Message = safety.RedactSecrets(result.Message)
	result.AuditEvents = redactAuditEvents(result.AuditEvents)
	if outcomeLookupFailed {
		result.AuditEvents = append(result.AuditEvents, "active outcome lookup failed; cancellation used only the separately verified pending owner/task intent")
	}
	if _, errEvent := persist(&result, intent.ID); errEvent != nil {
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

func (s *service) replayRuntimeStopIntent(automation *models.Automation, owner, runtimeID, taskID string, intent *models.AutomationLaunchEvent) (*agentruntime.StopResult, error) {
	return s.replayRuntimeStopIntentUsing(s.repo, automation, owner, runtimeID, taskID, intent)
}

func (s *service) replayRuntimeStopIntentUsing(repo Repository, automation *models.Automation, owner, runtimeID, taskID string, intent *models.AutomationLaunchEvent) (*agentruntime.StopResult, error) {
	if automation == nil || intent == nil || intent.ID == uuid.Nil ||
		intent.AutomationID != automation.ID ||
		strings.TrimSpace(intent.OwnerIdentity) != strings.TrimSpace(owner) ||
		strings.ToLower(strings.TrimSpace(intent.RuntimeType)) != strings.ToLower(strings.TrimSpace(runtimeID)) ||
		strings.TrimSpace(intent.LaunchType) != "agent_runtime_stop_intent" ||
		strings.TrimSpace(intent.RuntimeTaskID) != strings.TrimSpace(taskID) {
		return nil, fmt.Errorf("persisted runtime stop intent identity mismatch")
	}
	outcome, err := repo.FindLaunchOutcomeByIntentID(intent.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve persisted runtime stop outcome: %w", err)
	}
	if outcome == nil {
		return &agentruntime.StopResult{
			RuntimeID: runtimeID, TaskID: taskID, ExecutionReference: strings.TrimSpace(intent.ExecutionReference),
			Status: "indeterminate", Message: "a prior stop intent has no durable outcome; no cancellation was repeated and the attempt requires reconciliation",
			EvidenceURI: "automation-launch://" + intent.ID.String(),
			AuditEvents: []string{"matching persisted runtime stop intent found without a durable outcome", "retry stopped before dispatch to prevent duplicate cancellation"},
		}, nil
	}
	if outcome.ID == uuid.Nil || outcome.AutomationID != automation.ID ||
		strings.TrimSpace(outcome.OwnerIdentity) != strings.TrimSpace(owner) ||
		strings.ToLower(strings.TrimSpace(outcome.RuntimeType)) != strings.ToLower(strings.TrimSpace(runtimeID)) ||
		strings.TrimSpace(outcome.LaunchType) != "agent_runtime_stop" ||
		strings.TrimSpace(outcome.RuntimeTaskID) != strings.TrimSpace(taskID) ||
		strings.TrimSpace(outcome.ExecutionReference) != strings.TrimSpace(intent.ExecutionReference) ||
		strings.TrimSpace(outcome.EventKey) != runtimeStopOutcomeEventKey(intent.ID) {
		return nil, fmt.Errorf("persisted runtime stop outcome identity mismatch")
	}
	return &agentruntime.StopResult{
		RuntimeID: outcome.RuntimeType, TaskID: outcome.RuntimeTaskID, ExecutionReference: outcome.ExecutionReference,
		Status: outcome.Status, Message: safety.RedactSecrets(outcome.Message),
		EvidenceURI: "automation-launch://" + outcome.ID.String(), AuditEvents: redactAuditEvents(outcome.AuditEvents),
	}, nil
}

func activeRuntimeLaunchStatus(status, runtimeID string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "running":
		return true
	case "indeterminate", "needs_review":
		return strings.EqualFold(strings.TrimSpace(runtimeID), "openclaw")
	default:
		return false
	}
}

func activeRuntimeLaunchEventStatus(event *models.AutomationLaunchEvent, runtimeID string) bool {
	if event == nil || !activeRuntimeLaunchStatus(event.Status, runtimeID) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(runtimeID), "openclaw") {
		switch strings.ToLower(strings.TrimSpace(event.Status)) {
		case "indeterminate", "needs_review":
			return strings.TrimSpace(event.ExecutionReference) != ""
		}
	}
	return true
}

func (s *service) persistRuntimeStopEvent(automation *models.Automation, result *agentruntime.StopResult, started time.Time, ownerIdentity string, stopIntentIDs ...uuid.UUID) (uuid.UUID, error) {
	return s.persistRuntimeStopEventUsing(s.repo, automation, result, started, ownerIdentity, stopIntentIDs...)
}

func (s *service) persistRuntimeStopEventUsing(repo Repository, automation *models.Automation, result *agentruntime.StopResult, started time.Time, ownerIdentity string, stopIntentIDs ...uuid.UUID) (uuid.UUID, error) {
	if automation == nil || result == nil {
		return uuid.Nil, fmt.Errorf("runtime stop audit requires automation and result")
	}
	audit := append([]string{"runtime stop requested"}, result.AuditEvents...)
	exitCode := 0
	if result.Status == "blocked" || result.Status == "failed" {
		exitCode = -1
		if errUpdate := repo.UpdateRuntimeStopFailure(automation.ID, started, safety.RedactSecrets(result.Message)); errUpdate != nil {
			log.Printf("Runtime stop failure summary update was not confirmed for automation %s", automation.ID)
			audit = append(audit, "runtime stop failure summary update was not confirmed; use the persisted stop event")
		}
	}
	event := &models.AutomationLaunchEvent{
		ID:                 uuid.New(),
		AutomationID:       automation.ID,
		OwnerIdentity:      strings.TrimSpace(ownerIdentity),
		RuntimeType:        automation.RuntimeType,
		LaunchType:         "agent_runtime_stop",
		RuntimeTaskID:      result.TaskID,
		ExecutionReference: result.ExecutionReference,
		Target:             redactLaunchTarget(automation.LaunchTarget),
		Status:             result.Status,
		Message:            safety.RedactSecrets(result.Message),
		AuditEvents:        redactAuditEvents(audit),
		ExitCode:           exitCode,
		DurationMs:         time.Since(started).Milliseconds(),
		StartedAt:          started,
		CompletedAt:        time.Now().UTC(),
	}
	if len(stopIntentIDs) > 0 && stopIntentIDs[0] != uuid.Nil {
		event.EventKey = runtimeStopOutcomeEventKey(stopIntentIDs[0])
	}
	if errEvent := repo.SaveLaunchEvent(event); errEvent != nil {
		log.Printf("Failed to persist runtime stop event for automation %s: %v", automation.ID, errEvent)
		return uuid.Nil, errEvent
	}
	result.EvidenceURI = "automation-launch://" + event.ID.String()
	return event.ID, nil
}

func (s *service) launch(id uuid.UUID, request TaskLaunchRequest) (*LaunchResult, error) {
	owned := request.ExecutionContext != nil
	emergencyStopCtx, cancelEmergencyStop := safety.WithEmergencyStop(request.ExecutionContext)
	defer cancelEmergencyStop()
	request.ExecutionContext = emergencyStopCtx

	automation, err := s.launchConfiguration(request.ExecutionContext, id, owned)
	if err != nil {
		return nil, err
	}
	if automation == nil {
		return nil, fmt.Errorf("automation configuration is unavailable")
	}
	configuration := *automation
	automation = &configuration
	s.applyAutomationDefaults(automation)
	request = captureLaunchActionBinding(automation, request)
	admissionRepo, admissionCtx, cancelAdmission, err := s.launchAdmissionRepository(request.ExecutionContext, owned)
	if err != nil {
		return nil, err
	}
	defer cancelAdmission()
	idempotencyEventKey := ""
	if automationLaunchRequiresIdempotency(automation) {
		request.IdempotencyKey, err = normalizeLaunchIdempotencyKey(request.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		idempotencyEventKey = automationLaunchRequestEventKey(automation.ID, request.OwnerIdentity, request.IdempotencyKey)
		if owned {
			if err := request.ExecutionContext.Err(); err != nil {
				return nil, err
			}
		}
		existing, lookupErr := admissionRepo.FindLaunchIntentByEventKey(idempotencyEventKey)
		if owned {
			if contextErr := admissionCtx.Err(); contextErr != nil {
				return nil, errors.Join(lookupErr, contextErr)
			}
		}
		if lookupErr != nil {
			return nil, fmt.Errorf("resolve automation launch idempotency key: %w", lookupErr)
		}
		if existing != nil {
			return s.replayLaunchIntentWithRepository(admissionCtx, admissionRepo, automation, request, existing)
		}
	}
	launchedAt := time.Now().UTC()
	intentID := uuid.New()
	launchType := strings.ToLower(strings.TrimSpace(automation.LaunchType))
	runtimeTaskID := ""
	if launchType == "agent_runtime" {
		runtimeTaskID = automationLaunchRuntimeTaskID(automation, intentID)
	}
	executionReference := openClawGatewayExecutionReference(s.runtimeRegistry, automation, intentID)
	intent := &models.AutomationLaunchEvent{
		ID:                 intentID,
		AutomationID:       automation.ID,
		OwnerIdentity:      strings.TrimSpace(request.OwnerIdentity),
		RuntimeType:        automation.RuntimeType,
		LaunchType:         strings.ToLower(strings.TrimSpace(automation.LaunchType)) + "_intent",
		EventKey:           idempotencyEventKey,
		RuntimeTaskID:      runtimeTaskID,
		ExecutionReference: executionReference,
		Target:             redactLaunchTarget(automation.LaunchTarget),
		Status:             "pending",
		Message:            "immutable pre-execution intent recorded",
		AuditEvents: redactAuditEvents([]string{
			"controlled launch intent persisted before approval consumption or external access",
			"exact action digest " + request.launchActionDigest,
		}),
		StartedAt:   launchedAt,
		CompletedAt: launchedAt,
	}
	if owned {
		if err := admissionCtx.Err(); err != nil {
			return nil, err
		}
	}
	if errIntent := admissionRepo.SaveLaunchIntent(intent); errIntent != nil {
		if owned {
			return unconfirmedLaunchIntent(automation, intent), errors.Join(ErrLaunchIntentStorageUnconfirmed, errIntent, admissionCtx.Err())
		}
		if idempotencyEventKey != "" {
			existing, lookupErr := s.repo.FindLaunchIntentByEventKey(idempotencyEventKey)
			if lookupErr != nil {
				return nil, fmt.Errorf("persist pre-execution launch intent: %w; idempotency lookup failed: %v", errIntent, lookupErr)
			}
			if existing != nil {
				return s.replayLaunchIntent(automation, request, existing)
			}
		}
		return nil, fmt.Errorf("persist pre-execution launch intent: %w", errIntent)
	}
	if owned {
		if err := admissionCtx.Err(); err != nil {
			return unconfirmedLaunchIntent(automation, intent), errors.Join(ErrLaunchIntentStorageUnconfirmed, err)
		}
	}
	cancelAdmission()
	execution := s.executeLaunch(automation, request, launchedAt, intent.ID)
	execution.AuditEvents = append(
		[]string{"immutable pre-execution intent " + intent.ID.String() + " persisted"},
		execution.AuditEvents...,
	)
	event := &models.AutomationLaunchEvent{
		ID:                 uuid.New(),
		AutomationID:       automation.ID,
		OwnerIdentity:      strings.TrimSpace(request.OwnerIdentity),
		RuntimeType:        automation.RuntimeType,
		LaunchType:         automation.LaunchType,
		EventKey:           automationLaunchOutcomeEventKey(intent.ID),
		RuntimeTaskID:      execution.RuntimeTaskID,
		ExecutionReference: execution.ExecutionReference,
		Target:             redactLaunchTarget(automation.LaunchTarget),
		Status:             execution.Status,
		Message:            safety.RedactSecrets(execution.Message),
		Output:             safety.RedactSecrets(execution.Output),
		AuditEvents:        redactAuditEvents(execution.AuditEvents),
		RuntimeRouteTrace: redactRuntimeRouteTrace(
			execution.RuntimeRouteTrace,
		),
		ExitCode:         execution.ExitCode,
		DurationMs:       execution.DurationMs,
		RequiresApproval: execution.RequiresApproval,
		StartedAt:        launchedAt,
		CompletedAt:      time.Now().UTC(),
	}
	outcomeRepo, outcomeCtx, cancelOutcome, errEvent := s.launchOutcomeRepository(request.ExecutionContext, owned)
	defer cancelOutcome()
	if errEvent == nil && owned {
		errEvent = outcomeCtx.Err()
	}
	if errEvent == nil {
		errEvent = outcomeRepo.SaveLaunchEvent(event)
		if owned && outcomeCtx.Err() != nil {
			errEvent = errors.Join(errEvent, outcomeCtx.Err())
		}
	}
	if errEvent != nil {
		log.Printf("Launch outcome storage requires reconciliation for automation %s", automation.ID)
		partial := &LaunchResult{
			AutomationID:       automation.ID,
			LaunchEventID:      intent.ID,
			RuntimeTaskID:      execution.RuntimeTaskID,
			ExecutionReference: execution.ExecutionReference,
			RuntimeType:        automation.RuntimeType,
			LaunchType:         automation.LaunchType,
			Target:             redactLaunchTarget(automation.LaunchTarget),
			Status:             "indeterminate",
			Message:            "execution outcome audit storage was not confirmed; reconcile the immutable pre-execution intent before another attempt",
			Output:             safety.RedactSecrets(execution.Output),
			RuntimeRouteTrace:  redactRuntimeRouteTrace(execution.RuntimeRouteTrace),
			ExitCode:           -1,
			DurationMs:         execution.DurationMs,
			RequiresApproval:   execution.RequiresApproval,
			AuditEvents: redactAuditEvents(append(
				execution.AuditEvents,
				"execution outcome audit storage acknowledgement unconfirmed",
				"completion was not claimed",
			)),
			LaunchedAt: launchedAt,
		}
		if owned {
			return partial, errors.Join(ErrLaunchOutcomeStorageUnconfirmed, errEvent)
		}
		return partial, nil
	}
	result := &LaunchResult{
		AutomationID:       automation.ID,
		LaunchEventID:      event.ID,
		RuntimeTaskID:      execution.RuntimeTaskID,
		ExecutionReference: execution.ExecutionReference,
		RuntimeType:        automation.RuntimeType,
		LaunchType:         automation.LaunchType,
		Target:             redactLaunchTarget(automation.LaunchTarget),
		Status:             execution.Status,
		Message:            safety.RedactSecrets(execution.Message),
		Output:             safety.RedactSecrets(execution.Output),
		RuntimeRouteTrace:  redactRuntimeRouteTrace(execution.RuntimeRouteTrace),
		ExitCode:           execution.ExitCode,
		DurationMs:         execution.DurationMs,
		RequiresApproval:   execution.RequiresApproval,
		AuditEvents:        redactAuditEvents(execution.AuditEvents),
		LaunchedAt:         launchedAt,
	}
	if owned && outcomeCtx.Err() != nil {
		return result, outcomeCtx.Err()
	}
	if errUpdate := outcomeRepo.UpdateLaunchState(automation.ID, launchedAt, launchFailureReason(execution.Status, execution.Message)); errUpdate != nil {
		updateErr := fmt.Errorf("update automation launch summary after persisted outcome: %w", errUpdate)
		if owned && outcomeCtx.Err() != nil {
			return result, errors.Join(updateErr, outcomeCtx.Err())
		}
		return result, updateErr
	}
	if owned && outcomeCtx.Err() != nil {
		return result, outcomeCtx.Err()
	}
	return result, nil
}

func automationLaunchRequiresIdempotency(automation *models.Automation) bool {
	if automation == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(automation.LaunchType)) {
	case "script", "docker_service", "agent_runtime":
		return true
	case "api":
		// HTTP verbs do not reliably describe endpoint semantics; a configured
		// GET or HEAD target can still mutate remote state.
		return true
	default:
		return false
	}
}

func normalizeLaunchIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrLaunchIdempotencyKeyRequired
	}
	if len(value) > 256 {
		return "", ErrLaunchIdempotencyKeyInvalid
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return "", ErrLaunchIdempotencyKeyInvalid
		}
	}
	return value, nil
}

func automationLaunchRequestEventKey(automationID uuid.UUID, owner, idempotencyKey string) string {
	identity := automationID.String() + "\x00" + strings.TrimSpace(owner) + "\x00" + idempotencyKey
	digest := sha256.Sum256([]byte(identity))
	return "automation-launch-request:v1:" + hex.EncodeToString(digest[:])
}

func automationLaunchOutcomeEventKey(intentID uuid.UUID) string {
	if intentID == uuid.Nil {
		return ""
	}
	return "automation-launch-outcome:v1:" + intentID.String()
}

func (s *service) replayLaunchIntent(automation *models.Automation, request TaskLaunchRequest, intent *models.AutomationLaunchEvent) (*LaunchResult, error) {
	return s.replayLaunchIntentWithRepository(context.Background(), s.repo, automation, request, intent)
}

func (s *service) replayLaunchIntentWithRepository(ctx context.Context, repo Repository, automation *models.Automation, request TaskLaunchRequest, intent *models.AutomationLaunchEvent) (*LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	launchType := strings.ToLower(strings.TrimSpace(automation.LaunchType))
	if intent == nil || intent.ID == uuid.Nil || intent.AutomationID != automation.ID ||
		strings.TrimSpace(intent.OwnerIdentity) != strings.TrimSpace(request.OwnerIdentity) ||
		strings.ToLower(strings.TrimSpace(intent.LaunchType)) != launchType+"_intent" ||
		launchIntentActionDigest(intent) != automationActionDigest(automation, request) {
		return nil, ErrLaunchIdempotencyConflict
	}

	outcome, err := repo.FindLaunchOutcomeByIntentID(intent.ID)
	contextErr := ctx.Err()
	if err != nil {
		readErr := fmt.Errorf("resolve persisted automation launch outcome: %w", err)
		if contextErr != nil {
			return nil, errors.Join(readErr, contextErr)
		}
		return nil, readErr
	}
	if outcome == nil {
		if contextErr != nil {
			return nil, contextErr
		}
		return &LaunchResult{
			AutomationID:       automation.ID,
			LaunchEventID:      intent.ID,
			RuntimeTaskID:      intent.RuntimeTaskID,
			ExecutionReference: intent.ExecutionReference,
			RuntimeType:        automation.RuntimeType,
			LaunchType:         automation.LaunchType,
			Target:             redactLaunchTarget(automation.LaunchTarget),
			Status:             "indeterminate",
			Message:            "a prior launch intent has no durable outcome; no external action was replayed, and the existing attempt requires reconciliation",
			ExitCode:           -1,
			RequiresApproval:   false,
			AuditEvents:        []string{"matching persisted launch intent found without a durable outcome", "retry stopped before dispatch to prevent duplicate external effects"},
			LaunchedAt:         intent.StartedAt,
		}, nil
	}
	if outcome.AutomationID != automation.ID ||
		strings.TrimSpace(outcome.OwnerIdentity) != strings.TrimSpace(request.OwnerIdentity) ||
		!launchOutcomeMatchesIntent(automation, intent, outcome, launchType) {
		if contextErr != nil {
			return nil, errors.Join(ErrLaunchIdempotencyConflict, contextErr)
		}
		return nil, ErrLaunchIdempotencyConflict
	}
	result := &LaunchResult{
		AutomationID:       outcome.AutomationID,
		LaunchEventID:      outcome.ID,
		RuntimeTaskID:      outcome.RuntimeTaskID,
		ExecutionReference: outcome.ExecutionReference,
		RuntimeType:        outcome.RuntimeType,
		LaunchType:         automation.LaunchType,
		Target:             redactLaunchTarget(automation.LaunchTarget),
		Status:             outcome.Status,
		Message:            safety.RedactSecrets(outcome.Message),
		Output:             safety.RedactSecrets(outcome.Output),
		RuntimeRouteTrace:  redactRuntimeRouteTrace(outcome.RuntimeRouteTrace),
		ExitCode:           outcome.ExitCode,
		DurationMs:         outcome.DurationMs,
		RequiresApproval:   outcome.RequiresApproval,
		AuditEvents:        redactAuditEvents(outcome.AuditEvents),
		LaunchedAt:         outcome.StartedAt,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if updateAutomationLaunchProjection(automation, outcome) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := repo.UpdateLaunchState(automation.ID, outcome.StartedAt, launchFailureReason(outcome.Status, outcome.Message)); err != nil {
			return result, fmt.Errorf("repair automation launch summary from persisted outcome: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func updateAutomationLaunchProjection(automation *models.Automation, outcome *models.AutomationLaunchEvent) bool {
	if automation == nil || outcome == nil || outcome.StartedAt.IsZero() {
		return false
	}
	if automation.LastLaunchAt != nil && outcome.StartedAt.Before(*automation.LastLaunchAt) {
		return false
	}
	changed := automation.LastLaunchAt == nil || !automation.LastLaunchAt.Equal(outcome.StartedAt)
	startedAt := outcome.StartedAt
	automation.LastLaunchAt = &startedAt
	if outcome.Status == "failed" || outcome.Status == "blocked" {
		failureReason := safety.RedactSecrets(outcome.Message)
		if automation.LastFailureReason != failureReason {
			changed = true
			automation.LastFailureReason = failureReason
		}
	}
	return changed
}

func launchFailureReason(status, message string) *string {
	if status != "failed" && status != "blocked" {
		return nil
	}
	reason := safety.RedactSecrets(message)
	return &reason
}

func launchOutcomeMatchesIntent(automation *models.Automation, intent, outcome *models.AutomationLaunchEvent, expectedLaunchType string) bool {
	if automation == nil || intent == nil || outcome == nil {
		return false
	}
	outcomeType := strings.ToLower(strings.TrimSpace(outcome.LaunchType))
	if outcomeType == strings.ToLower(strings.TrimSpace(expectedLaunchType)) {
		if outcome.EventKey != automationLaunchOutcomeEventKey(intent.ID) || outcome.Target != intent.Target {
			return false
		}
		if outcomeType == "agent_runtime" {
			return strings.TrimSpace(intent.RuntimeTaskID) != "" &&
				strings.TrimSpace(outcome.RuntimeTaskID) == strings.TrimSpace(intent.RuntimeTaskID) &&
				strings.EqualFold(strings.TrimSpace(outcome.RuntimeType), strings.TrimSpace(automation.RuntimeType))
		}
		return true
	}
	if strings.ToLower(strings.TrimSpace(expectedLaunchType)) != "agent_runtime" {
		return false
	}
	switch outcomeType {
	case "agent_runtime_openclaw_terminal", "agent_runtime_host_completion":
		return strings.TrimSpace(intent.RuntimeTaskID) != "" &&
			strings.TrimSpace(outcome.RuntimeTaskID) == strings.TrimSpace(intent.RuntimeTaskID) &&
			strings.EqualFold(strings.TrimSpace(outcome.RuntimeType), strings.TrimSpace(automation.RuntimeType))
	default:
		return false
	}
}

func launchIntentActionDigest(intent *models.AutomationLaunchEvent) string {
	if intent == nil {
		return ""
	}
	const prefix = "exact action digest "
	for _, event := range intent.AuditEvents {
		if strings.HasPrefix(event, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(event, prefix))
		}
	}
	return ""
}

func automationRuntimeTaskID(automation *models.Automation) string {
	if automation == nil ||
		strings.ToLower(strings.TrimSpace(automation.LaunchType)) != "agent_runtime" {
		return ""
	}
	return automation.ID.String()
}

// automationLaunchRuntimeTaskID gives every immutable launch intent its own
// runtime identity. Host-runtime jobs use a unique task ID so a completed run
// cannot prevent the next deliberately approved launch of the same automation.
func automationLaunchRuntimeTaskID(automation *models.Automation, intentID uuid.UUID) string {
	if automation == nil || automation.ID == uuid.Nil || intentID == uuid.Nil {
		return ""
	}
	return "automation:" + automation.ID.String() + ":intent:" + intentID.String()
}

func openClawGatewayExecutionReference(registry *agentruntime.Registry, automation *models.Automation, intentID uuid.UUID) string {
	if registry == nil || automation == nil || automation.ID == uuid.Nil || intentID == uuid.Nil ||
		strings.ToLower(strings.TrimSpace(automation.LaunchType)) != "agent_runtime" ||
		!strings.EqualFold(strings.TrimSpace(automation.RuntimeType), "openclaw") ||
		!registry.OpenClawGatewayDelegationReady() {
		return ""
	}
	return "ocgw:v2:" + intentID.String()
}

func runtimeStopOutcomeEventKey(intentID uuid.UUID) string {
	if intentID == uuid.Nil {
		return ""
	}
	return "runtime-stop-outcome:" + intentID.String()
}

func runtimeStopRequestEventKey(automationID uuid.UUID, owner, taskID string) string {
	if automationID == uuid.Nil || strings.TrimSpace(owner) == "" || strings.TrimSpace(taskID) == "" {
		return ""
	}
	identity := automationID.String() + "\x00" + strings.TrimSpace(owner) + "\x00" + strings.TrimSpace(taskID)
	digest := sha256.Sum256([]byte(identity))
	return "runtime-stop-request:v1:" + hex.EncodeToString(digest[:])
}

func (s *service) Diagnostics(id uuid.UUID) (*DiagnosticResult, error) {
	return s.diagnostics(id, "")
}

func (s *service) DiagnosticsForOwner(id uuid.UUID, ownerIdentity string) (*DiagnosticResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("authenticated owner identity is required for launch diagnostics")
	}
	return s.diagnostics(id, ownerIdentity)
}

func (s *service) diagnostics(id uuid.UUID, ownerIdentity string) (*DiagnosticResult, error) {
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if automation == nil {
		return nil, gorm.ErrRecordNotFound
	}
	// Defaults belong to this diagnostic copy, never the repository record.
	copyAutomation := *automation
	automation = &copyAutomation
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
	recentLaunches := []models.AutomationLaunchEvent{}
	if ownerIdentity != "" {
		recentLaunches, err = s.repo.FindOwnerLaunchEvents(automation.ID, ownerIdentity, 10)
		if err != nil {
			return nil, fmt.Errorf("load owner-scoped launch history: %w", err)
		}
	}

	return publicDiagnostics(&DiagnosticResult{
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
	}), nil
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
	if decision := safety.EvaluateEmergencyStopForExecution(); decision.Active {
		return blockedLaunch(decision.Reason, started, append(audit, "emergency stop blocked runtime launch"))
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
		verifiedAudit, err := s.verifyAndConsumeApproval(automation, request)
		if err != nil {
			return blockedLaunch(
				"action-bound owner approval is required at the launcher boundary for API requests: "+err.Error(),
				started,
				append(audit, "action-bound approval proof rejected before network access"),
			)
		}
		audit = append(audit, verifiedAudit...)
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
			EffectDigest:          request.launchActionDigest,
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
		RuntimeModel:     automation.RuntimeModel,
		ID:               automationLaunchRuntimeTaskID(automation, intentID),
		Prompt:           strings.TrimSpace(request.Task),
		ProjectKey:       strings.TrimSpace(request.ProjectKey),
		OwnerIdentity:    owner,
		ApprovalSourceID: strings.TrimSpace(request.ApprovalSourceID),
		// The action-bound proof remains an independent defense-in-depth gate.
		HumanApproved: true,
	}
	runtimeTask.ExecutionReference = openClawGatewayExecutionReference(s.runtimeRegistry, automation, intentID)
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
	finalRequest.RuntimeModel = runtimeTask.RuntimeModel
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
			// Preserve the established execution-auth policy identity, but do not
			// treat this classification as evidence that the endpoint is read-only.
			// executeLaunch requires and consumes the exact owner proof before this
			// network-authorization boundary is reachable.
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
	if !isSupportedAgentRuntime(runtimeID) {
		return blockedLaunch("agent_runtime launch type requires runtimeType deepseek-harness, hermes, odysseus, or openclaw", started, append(audit, "agent runtime type rejected"))
	}
	if s.runtimeRegistry == nil {
		return blockedLaunch("agent runtime registry is not configured", started, append(audit, "agent runtime registry unavailable"))
	}
	executionContext := request.ExecutionContext
	if executionContext == nil {
		executionContext = context.Background()
	}
	if err := validateLaunchActionBinding(automation, request); err != nil {
		return blockedLaunch(err.Error(), started, append(audit, "launch action binding rejected before agent runtime dispatch"))
	}
	result := s.runtimeRegistry.Execute(executionContext, runtimeID, runtimeTask)
	executionReference := strings.TrimSpace(result.ExecutionReference)
	if runtimeID == "openclaw" {
		executionReference = ""
		if s.runtimeRegistry.OpenClawGatewayDelegationReady() {
			executionReference = strings.TrimSpace(runtimeTask.ExecutionReference)
			if executionReference == "" {
				return blockedLaunch("OpenClaw Gateway execution has no persisted intent-bound reference", started, append(audit, "Gateway task rejected before accepting an unbound execution identity"))
			}
			if adapterReference := strings.TrimSpace(result.ExecutionReference); adapterReference != "" && adapterReference != executionReference {
				return launchExecution{
					Status:             "indeterminate",
					Message:            "OpenClaw Gateway returned a reference that does not match the immutable task intent; the run requires review",
					RuntimeTaskID:      runtimeTask.ID,
					ExecutionReference: executionReference,
					ExitCode:           -1,
					DurationMs:         result.DurationMs,
					RequiresApproval:   true,
					AuditEvents:        append(audit, "Gateway execution reference mismatch; no outcome was correlated to the foreign reference"),
				}
			}
		}
	}
	return launchExecution{
		Status:             result.Status,
		Message:            result.Message,
		Output:             result.Output,
		RuntimeRouteTrace:  automationRuntimeRouteTrace(result.RouteTrace),
		ExitCode:           result.ExitCode,
		DurationMs:         result.DurationMs,
		RequiresApproval:   result.Status == "blocked",
		RuntimeTaskID:      runtimeTask.ID,
		ExecutionReference: executionReference,
		AuditEvents:        append(audit, result.AuditEvents...),
	}
}

func isSupportedAgentRuntime(runtimeID string) bool {
	switch strings.ToLower(strings.TrimSpace(runtimeID)) {
	case "deepseek-harness", "hermes", "odysseus", "openclaw":
		return true
	default:
		return false
	}
}

func (s *service) verifyAndConsumeApproval(automation *models.Automation, request TaskLaunchRequest) ([]string, error) {
	executionContext := request.ExecutionContext
	if executionContext == nil {
		executionContext = context.Background()
	}
	executionContext, cancel := context.WithTimeout(executionContext, approvalProofConsumptionTimeout)
	defer cancel()
	if err := executionContext.Err(); err != nil {
		return nil, err
	}
	if s.approvalProofs == nil {
		return nil, fmt.Errorf("approval proof service is unavailable")
	}
	scope, required := approvalScopeForAutomation(automation)
	if !required {
		return nil, fmt.Errorf("automation action has no supported approval scope")
	}
	if err := validateLaunchActionBinding(automation, request); err != nil {
		return nil, err
	}
	err := s.approvalProofs.VerifyAndConsume(executionContext, request.ApprovalProof, ApprovalProofExpectation{
		OwnerIdentity:    strings.TrimSpace(request.OwnerIdentity),
		AutomationID:     automation.ID,
		ActionDigest:     request.launchActionDigest,
		Scope:            scope,
		ApprovalSourceID: strings.TrimSpace(request.ApprovalSourceID),
	})
	if contextErr := executionContext.Err(); contextErr != nil {
		return nil, errors.Join(ErrApprovalProofConsumptionUnconfirmed, err, contextErr)
	}
	if err != nil {
		return nil, err
	}
	if err := validateLaunchActionBinding(automation, request); err != nil {
		return nil, errors.Join(ErrApprovalProofConsumptionUnconfirmed, err)
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
	executionContext := request.ExecutionContext
	if executionContext == nil {
		executionContext = context.Background()
	}
	req, err := http.NewRequestWithContext(executionContext, method, target, nil)
	if err != nil {
		return failedLaunch(redactAutomationRequestError(err), started, append(audit, "api request creation failed"))
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
	addresses, err := resolveAutomationTargetAddresses(executionContext, host, envEnabled("AUTOMATION_API_ALLOW_LINK_LOCAL"), net.DefaultResolver.LookupIP)
	if err != nil {
		return blockedLaunch("API launch destination could not be safely resolved", started, append(audit, "api destination resolution failed closed"))
	}
	client := noRedirectHTTPClientForTarget(10*time.Second, host, addresses)
	var resp *http.Response
	var requestErr error
	var bindingErr error
	decision, admissionErr := withAutomationEffectAdmission(executionContext, func(release func()) {
		if bindingErr = validateLaunchActionBinding(automation, request); bindingErr != nil {
			return
		}
		trace := &httptrace.ClientTrace{
			WroteRequest: func(httptrace.WroteRequestInfo) { release() },
		}
		dispatchRequest := req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		resp, requestErr = client.Do(dispatchRequest)
		// Cover failures before the transport reports WroteRequest.
		release()
	})
	if admissionErr != nil {
		return interruptedLaunch(executionContext, "API request", "", started, append(audit, "API request was not admitted"))
	}
	if decision.Active {
		return blockedLaunch(decision.Reason, started, append(audit, "emergency stop rechecked before API network access"))
	}
	if bindingErr != nil {
		return blockedLaunch(bindingErr.Error(), started, append(audit, "launch action binding rejected before API network access"))
	}
	err = requestErr
	if err != nil {
		if executionContext.Err() != nil {
			return interruptedLaunch(executionContext, "API request", "", started, append(audit, "API request ended without a verified remote outcome"))
		}
		return failedLaunch(redactAutomationRequestError(err), started, append(audit, "api request failed"))
	}
	defer resp.Body.Close()
	output := readAutomationOutput(resp.Body, 4096)
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
		Output:      output,
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
	timeoutSeconds := intEnv("AUTOMATION_SCRIPT_TIMEOUT_SECONDS", 30)
	if timeoutSeconds > maxScriptTimeoutSeconds {
		timeoutSeconds = maxScriptTimeoutSeconds
	}
	parent := request.ExecutionContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	request.ExecutionContext = ctx
	if ctx.Err() != nil {
		return interruptedLaunch(ctx, "script preprocessing", "", started, append(audit, "script process was not started"))
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
	if err := verifyPinnedScript(ctx, scriptPath); err != nil {
		if ctx.Err() != nil {
			return interruptedLaunch(ctx, "script preprocessing", "", started, append(audit, "script process was not started"))
		}
		return blockedLaunch(err.Error(), started, append(audit, "script hash pin rejected"))
	}
	_, authorizationAudit, err := s.authorizeExternalLaunch(
		automation,
		request,
		intentID,
		"",
	)
	if ctx.Err() != nil {
		return interruptedLaunch(ctx, "script authorization", "", started, append(audit, authorizationAudit...))
	}
	if err != nil {
		return blockedLaunch(
			"unified execution authorization blocked local script execution: "+err.Error(),
			started,
			append(audit, "execution authorization rejected at the final process boundary"),
		)
	}
	audit = append(audit, authorizationAudit...)
	// Copy the verified bytes to a private temporary directory after approval.
	// Executing the mutable allowlist path directly leaves a final path-open race
	// after the hash check, even when the source is rechecked after authorization.
	stagedScript, cleanupStagedScript, err := stagePinnedScript(ctx, scriptPath)
	if err != nil {
		if ctx.Err() != nil {
			return interruptedLaunch(ctx, "script preprocessing", "", started, append(audit, "script process was not started"))
		}
		return blockedLaunch(
			err.Error(),
			started,
			append(audit, "script hash pin rejected after execution authorization"),
		)
	}
	defer cleanupStagedScript()
	cmd := exec.CommandContext(ctx, stagedScript)
	prepareScriptProcess(cmd)
	cmd.WaitDelay = scriptWaitDelay
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Env = safeScriptEnvironment(automation)
	outputLimit := intEnv("AUTOMATION_SCRIPT_OUTPUT_LIMIT_BYTES", 4096)
	if outputLimit < 1024 {
		outputLimit = 1024
	}
	if outputLimit > 65536 {
		outputLimit = 65536
	}
	output := newBoundedOutput(maxAutomationOutputCapture)
	cmd.Stdout = output
	cmd.Stderr = output
	var bindingErr error
	decision, admissionErr, startErr := startScriptWithEmergencyStopAdmission(ctx, func() error {
		bindingErr = validateLaunchActionBinding(automation, request)
		if bindingErr != nil {
			return bindingErr
		}
		return cmd.Start()
	})
	if admissionErr != nil {
		return interruptedLaunch(ctx, "script execution", "", started, append(audit, "script process was not admitted"))
	}
	if decision.Active {
		return blockedLaunch(decision.Reason, started, append(audit, "emergency stop rechecked before script process start"))
	}
	if bindingErr != nil {
		return blockedLaunch(bindingErr.Error(), started, append(audit, "launch action binding rejected before script process start"))
	}
	if startErr != nil {
		err = startErr
	} else {
		err = cmd.Wait()
	}
	outputText := trimOutput(output.Bytes(), int64(outputLimit))
	if output.Truncated() {
		outputText = omittedAutomationOutput
		audit = append(audit, fmt.Sprintf("script output omitted after capture exceeded %d bytes", maxAutomationOutputCapture))
	} else if len(output.Bytes()) > outputLimit {
		audit = append(audit, fmt.Sprintf("redacted script output limited to %d bytes", outputLimit))
	}
	if ctx.Err() != nil {
		audit = append(audit, "script process cancellation requested")
		if verifyScriptProcessTreeStopped(cmd) {
			audit = append(audit, "script process group termination verified")
		} else {
			audit = append(audit, "script process group termination not verified")
		}
		return interruptedLaunch(ctx, "script execution", outputText, started, append(audit, "subprocess and external-effect completion not verified"))
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		return launchExecution{
			Status:      "indeterminate",
			Message:     "script output pipes did not close within the drain deadline; subprocess and external-effect completion not verified",
			Output:      outputText,
			ExitCode:    -1,
			DurationMs:  time.Since(started).Milliseconds(),
			AuditEvents: append(audit, "script output drain deadline elapsed", "subprocess and external-effect completion not verified"),
		}
	}
	if err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return launchExecution{
			Status:      "failed",
			Message:     safety.RedactSecrets(err.Error()),
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

func verifyPinnedScript(ctx context.Context, scriptPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	expected, err := configuredScriptHash(filepath.Base(scriptPath))
	if err != nil {
		return err
	}
	file, err := openRegularScript(ctx, scriptPath)
	if err != nil {
		return fmt.Errorf("could not read script for SHA-256 verification: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := copyScriptBytes(ctx, hash, file); err != nil {
		return fmt.Errorf("could not hash script for SHA-256 verification: %w", err)
	}
	actual := hash.Sum(nil)
	want, _ := hex.DecodeString(expected)
	if subtle.ConstantTimeCompare(actual, want) != 1 {
		return fmt.Errorf("script SHA-256 does not match the configured pin for %s", filepath.Base(scriptPath))
	}
	return nil
}

func stagePinnedScript(ctx context.Context, scriptPath string) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	expected, err := configuredScriptHash(filepath.Base(scriptPath))
	if err != nil {
		return "", nil, err
	}
	source, err := openRegularScript(ctx, scriptPath)
	if err != nil {
		return "", nil, fmt.Errorf("could not read script for SHA-256 verification: %w", err)
	}
	defer source.Close()

	directory, err := os.MkdirTemp("", "hai-automation-script-")
	if err != nil {
		return "", nil, fmt.Errorf("could not create private script staging directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	stagedPath := filepath.Join(directory, filepath.Base(scriptPath))
	staged, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("could not stage verified script: %w", err)
	}
	hash := sha256.New()
	_, copyErr := copyScriptBytes(ctx, io.MultiWriter(staged, hash), source)
	closeErr := staged.Close()
	if copyErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("could not stage verified script: %w", copyErr)
	}
	if closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("could not finish staging verified script: %w", closeErr)
	}
	want, _ := hex.DecodeString(expected)
	if subtle.ConstantTimeCompare(hash.Sum(nil), want) != 1 {
		cleanup()
		return "", nil, fmt.Errorf("script SHA-256 does not match the configured pin for %s", filepath.Base(scriptPath))
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.Chmod(stagedPath, 0700); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("could not restrict staged script permissions: %w", err)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return "", nil, err
	}
	return stagedPath, cleanup, nil
}

// Regular-file I/O is checked between chunks; this is not a sandbox or a
// guarantee that cancellation can interrupt a stalled kernel/filesystem call.
func copyScriptBytes(ctx context.Context, dst io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		remaining := int64(maxScriptBytes) - copied
		readSize := len(buffer)
		if remaining < int64(readSize) {
			readSize = int(remaining) + 1
		}
		n, readErr := source.Read(buffer[:readSize])
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		if int64(n) > remaining {
			return copied, fmt.Errorf("script exceeds maximum size of %d bytes", maxScriptBytes)
		}
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			copied += int64(written)
			if writeErr != nil {
				return copied, writeErr
			}
			if written != n {
				return copied, io.ErrShortWrite
			}
		}
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		if readErr == io.EOF {
			return copied, nil
		}
		if readErr != nil {
			return copied, readErr
		}
		if n == 0 {
			return copied, io.ErrNoProgress
		}
	}
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
	req, err := http.NewRequestWithContext(executionContext, http.MethodPost, endpoint, nil)
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
	var resp *http.Response
	var requestErr error
	var bindingErr error
	decision, admissionErr := withAutomationEffectAdmission(executionContext, func(release func()) {
		if bindingErr = validateLaunchActionBinding(automation, request); bindingErr != nil {
			return
		}
		trace := &httptrace.ClientTrace{
			WroteRequest: func(httptrace.WroteRequestInfo) { release() },
		}
		dispatchRequest := req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		resp, requestErr = client.Do(dispatchRequest)
		// Cover failures before the transport reports WroteRequest.
		release()
	})
	if admissionErr != nil {
		return interruptedLaunch(executionContext, "Docker start request", "", started, append(audit, "Docker request was not admitted"))
	}
	if decision.Active {
		return blockedLaunch(decision.Reason, started, append(audit, "emergency stop rechecked before Docker socket access"))
	}
	if bindingErr != nil {
		return blockedLaunch(bindingErr.Error(), started, append(audit, "launch action binding rejected before Docker socket access"))
	}
	err = requestErr
	if err != nil {
		if executionContext.Err() != nil {
			return interruptedLaunch(executionContext, "Docker start request", "", started, append(audit, "Docker API outcome was not verified"))
		}
		return failedLaunch(err.Error(), started, append(audit, "docker socket request failed"))
	}
	defer resp.Body.Close()
	output := readAutomationOutput(resp.Body, 4096)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return launchExecution{
			Status:      "completed",
			Message:     fmt.Sprintf("Docker container %s start request accepted", containerName),
			Output:      output,
			ExitCode:    resp.StatusCode,
			DurationMs:  time.Since(started).Milliseconds(),
			AuditEvents: append(audit, "docker start request executed through Docker API"),
		}
	}
	return launchExecution{
		Status:      "failed",
		Message:     fmt.Sprintf("Docker API returned HTTP %d for container %s", resp.StatusCode, containerName),
		Output:      output,
		ExitCode:    resp.StatusCode,
		DurationMs:  time.Since(started).Milliseconds(),
		AuditEvents: append(audit, "docker start request failed"),
	}
}

func startScriptWithEmergencyStopAdmission(
	ctx context.Context,
	start func() error,
) (safety.EmergencyStopDecision, error, error) {
	if start == nil {
		return safety.EmergencyStopDecision{}, errors.New("script process start is unavailable"), nil
	}
	var startErr error
	decision, admissionErr := withAutomationEffectAdmission(ctx, func(release func()) {
		startErr = start()
		release()
	})
	return decision, admissionErr, startErr
}

func withAutomationEffectAdmission(
	ctx context.Context,
	admit func(release func()),
) (safety.EmergencyStopDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if admit == nil {
		return safety.EmergencyStopDecision{}, errors.New("effect admission is unavailable")
	}
	releaseFence, err := safety.AcquireExecutionCommitFenceContext(ctx)
	if err != nil {
		return safety.EmergencyStopDecision{}, err
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(releaseFence) }
	defer release()

	if err := ctx.Err(); err != nil {
		return safety.EmergencyStopDecision{}, err
	}
	decision := safety.EvaluateEmergencyStopForExecution()
	if decision.Active {
		return decision, nil
	}
	if err := ctx.Err(); err != nil {
		return safety.EmergencyStopDecision{}, err
	}
	admit(release)
	return safety.EmergencyStopDecision{}, nil
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
		automation.HealthCheckURL = "http://" + automationHostPort(strings.Trim(automation.Host, "[]"), automation.Port)
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

func interruptedLaunch(ctx context.Context, operation string, output string, started time.Time, audit []string) launchExecution {
	message := operation + " was interrupted; downstream effects require verification"
	if errors.Is(context.Cause(ctx), safety.ErrEmergencyStopActivated) {
		message = "emergency stop interrupted " + operation + "; downstream effects require verification"
		audit = append(audit, "emergency stop observed during "+operation)
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		message = operation + " exceeded its deadline; downstream effects require verification"
		audit = append(audit, operation+" deadline elapsed")
	} else {
		audit = append(audit, operation+" context cancellation observed")
	}
	return launchExecution{
		Status:      "indeterminate",
		Message:     safety.RedactSecrets(message),
		Output:      safety.RedactSecrets(output),
		ExitCode:    -1,
		DurationMs:  time.Since(started).Milliseconds(),
		AuditEvents: audit,
	}
}

func trimOutput(output []byte, limit int64) string {
	redacted := safety.RedactSecrets(string(output))
	if limit <= 0 {
		return ""
	}
	if int64(len(redacted)) > limit {
		redacted = redacted[:limit]
	}
	return strings.TrimSpace(redacted)
}

// Read a complete bounded response before redacting nested/escaped JSON.
// Never return a raw prefix when transport or capture limits break the JSON.
func readAutomationOutput(reader io.Reader, displayLimit int64) string {
	body, err := io.ReadAll(io.LimitReader(reader, maxAutomationOutputCapture+1))
	if err != nil {
		return "[output omitted: response capture incomplete]"
	}
	if len(body) > maxAutomationOutputCapture {
		return omittedAutomationOutput
	}
	return trimOutput(body, displayLimit)
}

func redactAutomationRequestError(err error) string {
	var requestError *url.Error
	if errors.As(err, &requestError) {
		return fmt.Sprintf("%s %s: %s", requestError.Op, safety.RedactURL(requestError.URL), safety.RedactSecrets(requestError.Err.Error()))
	}
	return safety.RedactSecrets(err.Error())
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

func noRedirectHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
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
