package temporalbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

const (
	enabledEnv   = "HAI_TEMPORAL_ENABLED"
	addressEnv   = "HAI_TEMPORAL_ADDRESS"
	namespaceEnv = "HAI_TEMPORAL_NAMESPACE"
	queueEnv     = "HAI_TEMPORAL_TASK_QUEUE"

	followUpWorkflowType   = "hai.governed-follow-up-check.v1"
	maxRunAhead            = 365 * 24 * time.Hour
	maxFollowUpLimit       = 50
	workerStartupIOTimeout = 10 * time.Second
	workerHealthTimeout    = 3 * time.Second
)

var (
	ErrNotConfigured         = errors.New("Temporal durable workflow bridge is not configured")
	ErrUnavailable           = errors.New("Temporal durable workflow service is unavailable")
	ErrAuthorizationRequired = errors.New("Temporal scheduling authorization is required")
	ErrEmergencyStopActive   = errors.New("emergency stop blocks Temporal scheduling")
	ErrScheduleUncertain     = errors.New("Temporal schedule outcome is uncertain; inspect this run in HAI and Temporal before retrying")
)

type Status struct {
	Enabled        bool   `json:"enabled"`
	Configured     bool   `json:"configured"`
	WorkerStarted  bool   `json:"workerStarted"`
	WorkerStarting bool   `json:"workerStarting"`
	Address        string `json:"address,omitempty"`
	Namespace      string `json:"namespace,omitempty"`
	TaskQueue      string `json:"taskQueue,omitempty"`
	ConfigError    string `json:"configError,omitempty"`
	WorkerError    string `json:"workerError,omitempty"`
	Scope          string `json:"scope"`
}

type FollowUpRequest struct {
	RunAt                 time.Time `json:"runAt"`
	Limit                 int       `json:"limit,omitempty"`
	TaskID                string    `json:"taskId,omitempty"`
	ProjectKey            string    `json:"projectKey,omitempty"`
	ApprovalSourceID      string    `json:"approvalSourceId,omitempty"`
	ApprovalBindingDigest string    `json:"approvalBindingDigest,omitempty"`
}

type FollowUpRun struct {
	ID                 uuid.UUID      `json:"id"`
	TemporalWorkflowID string         `json:"temporalWorkflowId"`
	WorkflowType       string         `json:"workflowType"`
	Status             string         `json:"status"`
	ScheduledFor       time.Time      `json:"scheduledFor"`
	StartedAt          *time.Time     `json:"startedAt,omitempty"`
	CompletedAt        *time.Time     `json:"completedAt,omitempty"`
	Summary            string         `json:"summary"`
	Result             FollowUpResult `json:"result"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

// FollowUpInput contains only opaque HAI run and scheduling metadata. HAI's
// own database remains the source of workflow context, ownership, and approval
// decisions.
type FollowUpInput struct {
	RunID string    `json:"runId"`
	RunAt time.Time `json:"runAt"`
	Limit int       `json:"limit"`
}

type FollowUpResult struct {
	Checked   int    `json:"checked"`
	Triggered int    `json:"triggered"`
	Resolved  int    `json:"resolved"`
	Skipped   int    `json:"skipped"`
	Summary   string `json:"summary"`
}

type config struct {
	enabled   bool
	address   string
	namespace string
	queue     string
}

type durableWorkflowScheduler interface {
	Schedule(
		context.Context,
		client.StartWorkflowOptions,
		FollowUpInput,
	) error
}

type temporalClientScheduler struct{ client client.Client }

func (s temporalClientScheduler) Schedule(
	ctx context.Context,
	options client.StartWorkflowOptions,
	input FollowUpInput,
) error {
	_, err := s.client.ExecuteWorkflow(
		ctx,
		options,
		GovernedFollowUpWorkflow,
		input,
	)
	return err
}

type Service struct {
	config                  config
	configErr               string
	repo                    Repository
	workflows               workflow.Service
	now                     func() time.Time
	dial                    func(context.Context, client.Options) (client.Client, error)
	newWorker               func(client.Client, string, worker.Options) worker.Worker
	mu                      sync.RWMutex
	client                  client.Client
	worker                  worker.Worker
	scheduler               durableWorkflowScheduler
	authorize               FinalEffectAuthorizer
	stop                    func() safety.EmergencyStopDecision
	workerErr               string
	workerContext           context.Context
	workerStopped           bool
	workerSupervisorStarted bool
	workerStarting          bool
	workerStartCancel       context.CancelFunc
}

func NewService(
	repo Repository,
	workflows workflow.Service,
	enabled bool,
	address,
	namespace,
	queue string,
	authorizers ...FinalEffectAuthorizer,
) *Service {
	s := &Service{
		config: config{
			enabled:   enabled,
			address:   strings.TrimSpace(address),
			namespace: strings.TrimSpace(namespace),
			queue:     strings.TrimSpace(queue),
		},
		repo:      repo,
		workflows: workflows,
		now:       time.Now,
		dial:      client.DialContext,
		newWorker: worker.New,
		stop:      safety.EvaluateEmergencyStop,
	}
	if len(authorizers) > 0 {
		s.authorize = authorizers[0]
	}
	if s.config.enabled {
		s.configErr = validateConfig(s.config)
	}
	return s
}

func NewServiceFromEnv(
	workflows workflow.Service,
	authorizers ...FinalEffectAuthorizer,
) *Service {
	return NewService(
		DefaultRepository(),
		workflows,
		strings.EqualFold(strings.TrimSpace(os.Getenv(enabledEnv)), "true"),
		strings.TrimSpace(os.Getenv(addressEnv)),
		strings.TrimSpace(os.Getenv(namespaceEnv)),
		strings.TrimSpace(os.Getenv(queueEnv)),
		firstAuthorizer(authorizers),
	)
}

func firstAuthorizer(values []FinalEffectAuthorizer) FinalEffectAuthorizer {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

// WithEmergencyStopEvaluator supports deterministic boundary tests. Production
// uses the process-wide persisted emergency-stop provider.
func (s *Service) WithEmergencyStopEvaluator(
	evaluator func() safety.EmergencyStopDecision,
) *Service {
	if evaluator != nil {
		s.stop = evaluator
	}
	return s
}

func (s *Service) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Status{
		Enabled:        s.config.enabled,
		Configured:     s.config.enabled && s.configErr == "",
		WorkerStarted:  s.worker != nil,
		WorkerStarting: s.workerStarting,
		Address:        safety.RedactSecrets(s.config.address),
		Namespace:      safety.RedactSecrets(s.config.namespace),
		TaskQueue:      safety.RedactSecrets(s.config.queue),
		ConfigError:    safety.RedactSecrets(s.configErr),
		WorkerError:    safety.RedactSecrets(s.workerErr),
		Scope:          "Opt-in local durable follow-up checks only. A run creates HAI follow-up proposals; it cannot send messages, execute tools, or bypass approvals.",
	}
}

// StartWorker launches exactly one worker for this process. A failed launch is
// surfaced through Status; it never prevents HAI's regular API from starting.
func (s *Service) StartWorker() {
	s.StartWorkerContext(context.Background())
}

// StartWorkerContext honors request cancellation without making the published
// worker's activity lifetime depend on that request. Host shutdown owns both.
func (s *Service) StartWorkerContext(ctx context.Context) {
	if !s.config.enabled || s.configErr != "" {
		return
	}
	s.mu.Lock()
	if s.workerStopped {
		s.workerErr = "backend is shutting down"
		s.mu.Unlock()
		return
	}
	if s.worker != nil || s.workerStarting {
		s.mu.Unlock()
		return
	}
	if s.repo == nil || s.workflows == nil {
		s.workerErr = "governed follow-up dependencies are unavailable"
		s.mu.Unlock()
		return
	}
	if _, ok := s.workflows.(workflow.ContextualFollowUpService); !ok {
		s.workerErr = "context-aware governed follow-up adapter is required"
		s.mu.Unlock()
		return
	}
	ownerCtx := s.workerContext
	if ownerCtx == nil {
		ownerCtx = context.Background()
	}
	if ownerCtx.Err() != nil {
		s.workerErr = "backend is shutting down"
		s.mu.Unlock()
		return
	}
	if ctx == nil || ctx.Err() != nil {
		s.workerErr = "Temporal worker startup request was canceled"
		s.mu.Unlock()
		return
	}
	finish, admitted := lifecycle.Enter(lifecycle.WithOwnership(ctx, ownerCtx), "temporal-worker-start")
	if !admitted {
		s.workerErr = "backend is shutting down"
		s.mu.Unlock()
		return
	}
	attemptCtx, cancel := context.WithTimeout(ctx, workerStartupIOTimeout)
	stopOwnerCancellation := context.AfterFunc(ownerCtx, cancel)
	s.workerStarting, s.workerStartCancel, s.workerErr = true, cancel, ""
	s.mu.Unlock()

	var clientValue client.Client
	var workerValue worker.Worker
	published, startAttempted := false, false
	failure := "Temporal worker startup was canceled or exceeded its I/O deadline"
	defer func() {
		cancel()
		stopOwnerCancellation()
		s.mu.Lock()
		if s.workerStopped || ownerCtx.Err() != nil {
			s.workerErr = "backend is shutting down"
		} else if !published {
			s.workerErr = failure
		}
		s.mu.Unlock()
		// Cleanup can wait for SDK work. Keep the attempt owned and the service
		// mutex free until resources are closed; no second attempt may overlap it.
		if !published {
			if workerValue != nil && startAttempted {
				workerValue.Stop()
			}
			if clientValue != nil {
				clientValue.Close()
			}
		}
		s.mu.Lock()
		s.workerStarting, s.workerStartCancel = false, nil
		if s.workerStopped || ownerCtx.Err() != nil {
			s.workerErr = "backend is shutting down"
		} else if !published {
			s.workerErr = failure
		}
		s.mu.Unlock()
		finish()
	}()

	var err error
	if attemptCtx.Err() != nil {
		return
	}
	clientValue, err = s.dial(attemptCtx, client.Options{HostPort: s.config.address, Namespace: s.config.namespace})
	if err != nil || clientValue == nil {
		failure = "could not connect to the configured local Temporal service"
		return
	}
	if attemptCtx.Err() != nil {
		return
	}
	healthCtx, healthCancel := context.WithTimeout(attemptCtx, workerHealthTimeout)
	_, err = clientValue.CheckHealth(healthCtx, &client.CheckHealthRequest{})
	healthCancel()
	if err != nil {
		failure = "configured local Temporal service is not healthy yet"
		return
	}
	if attemptCtx.Err() != nil {
		return
	}
	if s.newWorker == nil {
		failure = "governed follow-up worker factory is unavailable"
		return
	}
	activity := &followUpActivity{repo: s.repo, workflows: s.workflows, now: s.now}
	workerValue = s.newWorker(clientValue, s.config.queue, worker.Options{BackgroundActivityContext: ownerCtx})
	if workerValue == nil {
		failure = "governed follow-up worker factory returned no worker"
		return
	}
	workerValue.RegisterWorkflow(GovernedFollowUpWorkflow)
	workerValue.RegisterActivity(activity.Run)
	if attemptCtx.Err() != nil {
		return
	}
	startAttempted = true
	if err := workerValue.Start(); err != nil {
		failure = "could not start the governed follow-up worker"
		return
	}
	// SDK Worker.Start does not accept a context. A late return must never
	// resurrect a stopped/canceled service; the defer still joins its cleanup.
	s.mu.Lock()
	if !s.workerStopped && ownerCtx.Err() == nil && attemptCtx.Err() == nil {
		s.client, s.worker = clientValue, workerValue
		s.scheduler = temporalClientScheduler{client: clientValue}
		published = true
	}
	s.mu.Unlock()
}

// StartWorkerEventually handles Compose ordering without turning an unavailable
// local scheduler into an application-startup failure. It makes at most thirty
// local connection attempts, then leaves the explicit admin retry route
// available. It never schedules or executes a follow-up by itself.
func (s *Service) StartWorkerEventually(ctx context.Context) {
	if ctx == nil || !s.config.enabled || s.configErr != "" {
		return
	}
	s.mu.Lock()
	if s.workerSupervisorStarted || s.workerStopped {
		s.mu.Unlock()
		return
	}
	s.workerSupervisorStarted = true
	s.workerContext = ctx
	s.mu.Unlock()
	if !lifecycle.Go(ctx, "temporal-worker-supervisor", func() {
		defer s.stopWorker()
		for attempt := 0; attempt < 30; attempt++ {
			if ctx.Err() != nil {
				return
			}
			s.StartWorker()
			if s.Status().WorkerStarted {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
		// Retain ownership after startup, including workers started by an admin retry.
		<-ctx.Done()
	}) {
		s.stopWorker()
	}
}

func (s *Service) stopWorker() {
	s.mu.Lock()
	if s.workerStopped {
		s.mu.Unlock()
		return
	}
	s.workerStopped = true
	workerValue, clientValue, cancelStart := s.worker, s.client, s.workerStartCancel
	s.worker, s.client, s.scheduler = nil, nil, nil
	s.workerErr = "backend is shutting down"
	s.mu.Unlock()
	if cancelStart != nil {
		cancelStart()
	}
	// SDK shutdown may wait for activities. Never hold the service mutex during it.
	if workerValue != nil {
		workerValue.Stop()
	}
	if clientValue != nil {
		clientValue.Close()
	}
}

func (s *Service) ScheduleFollowUp(ctx context.Context, ownerIdentity string, request FollowUpRequest) (*FollowUpRun, error) {
	if ctx == nil {
		return nil, errors.New("Temporal schedule context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, apierror.New(apierror.CodeBadRequest, "owner identity is required")
	}
	if err := validateRequest(s.now().UTC(), request); err != nil {
		return nil, err
	}
	// PostgreSQL timestamps have microsecond precision. Bind, persist and dispatch
	// one canonical value so a database round trip cannot invalidate the fence.
	request.RunAt = request.RunAt.UTC().Truncate(time.Microsecond)
	if !s.config.enabled || s.configErr != "" {
		return nil, ErrNotConfigured
	}
	s.mu.RLock()
	scheduler := s.scheduler
	s.mu.RUnlock()
	if scheduler == nil || s.repo == nil {
		return nil, ErrUnavailable
	}

	now := s.now().UTC()
	workflowID := "hai-follow-up-" + uuid.NewString()
	scheduledFor := request.RunAt.UTC()
	record := &models.TemporalWorkflowRun{
		OwnerIdentity:      ownerIdentity,
		TemporalWorkflowID: workflowID,
		WorkflowType:       followUpWorkflowType,
		Status:             "preparing",
		ScheduledFor:       scheduledFor,
		Summary:            "governed follow-up schedule is being prepared; Temporal has not accepted it",
		ResultJSON:         "{}",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	stored, err := s.repo.Create(record)
	if err != nil {
		return nil, err
	}
	// A repository may mutate record in place. Compare against independently
	// retained intent before authorizing or dispatching any scheduling effect.
	if stored == nil || stored.ID == uuid.Nil || stored.OwnerIdentity != ownerIdentity ||
		stored.TemporalWorkflowID != workflowID || stored.WorkflowType != followUpWorkflowType ||
		stored.Status != "preparing" || !stored.ScheduledFor.Equal(scheduledFor) ||
		stored.StartedAt != nil || stored.CompletedAt != nil {
		return nil, errors.New("durable Temporal run did not confirm the expected scheduling provenance")
	}
	// Retain a value snapshot: repository/authorizer callbacks must not be able to
	// change the identity used by the final state fence.
	expected := *stored
	authorizationRequest, executionTarget, err :=
		buildScheduleAuthorizationRequest(
			ownerIdentity,
			stored.ID.String(),
			workflowID,
			request,
		)
	if err != nil {
		return nil, s.markScheduleFailed(ctx, expected,
			"Temporal scheduling authorization could not be constructed",
			ErrAuthorizationRequired,
		)
	}
	if s.authorize == nil {
		return nil, s.markScheduleFailed(ctx, expected,
			"Temporal scheduling authorization is not configured",
			ErrAuthorizationRequired,
		)
	}
	if _, err = s.authorize.AuthorizeAndConsume(
		ctx,
		authorizationRequest,
		temporalAuthorizationConsumer,
		executionTarget,
	); err != nil {
		return nil, s.markScheduleFailed(ctx, expected,
			"Temporal scheduling authorization was denied",
			ErrAuthorizationRequired,
		)
	}
	stored, changed, err := s.transitionSchedule(ctx, expected, "preparing", "dispatching",
		"authorized schedule is being dispatched; Temporal acceptance is not yet confirmed")
	if err != nil {
		return nil, err
	}
	if !changed || stored.Status != "dispatching" || stored.StartedAt != nil || stored.CompletedAt != nil {
		return nil, errors.New("durable Temporal dispatch state was not confirmed")
	}
	expected = *stored

	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: s.config.queue,
	}
	input := FollowUpInput{
		RunID: stored.ID.String(),
		RunAt: request.RunAt.UTC(),
		Limit: normalizeLimit(request.Limit),
	}
	stop := s.stop()
	if stop.Active {
		return nil, s.markScheduleFailed(ctx, expected,
			"Emergency stop blocked the governed follow-up schedule",
			ErrEmergencyStopActive,
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, s.markScheduleFailed(ctx, expected, "request was canceled before Temporal dispatch", err)
	}
	err = scheduler.Schedule(ctx, options, input)
	// A canceled request cannot cancel recording an already-attempted effect.
	// This cleanup remains bounded and never retries the scheduling effect.
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err != nil {
		current, _, settleErr := s.transitionSchedule(settleCtx, expected, "dispatching", "schedule_uncertain",
			"Temporal acceptance is uncertain; inspect the same workflow ID before any retry")
		if settleErr == nil {
			stored = current
		}
		run := runFromModel(*stored)
		return &run, ErrScheduleUncertain
	}
	current, _, err := s.transitionSchedule(settleCtx, expected, "dispatching", "scheduled",
		"Temporal accepted the governed follow-up check; it will create proposals only")
	if err != nil || !confirmedScheduleState(current) {
		run := runFromModel(*stored)
		return &run, ErrScheduleUncertain
	}
	stored = current
	run := runFromModel(*stored)
	return &run, nil
}

func (s *Service) markScheduleFailed(
	ctx context.Context,
	expected models.TemporalWorkflowRun,
	summary string,
	cause error,
) error {
	current, changed, err := s.transitionSchedule(ctx, expected, expected.Status, "failed", summary)
	if err != nil {
		return err
	}
	if !changed || current.Status != "failed" || current.StartedAt != nil || current.CompletedAt != nil {
		return errors.New("durable Temporal rejection state was not confirmed")
	}
	return cause
}

func (s *Service) transitionSchedule(ctx context.Context, expected models.TemporalWorkflowRun, from, to, summary string) (*models.TemporalWorkflowRun, bool, error) {
	changed, err := s.repo.TransitionSchedule(ctx, expected, from, to, summary, s.now().UTC())
	if err != nil {
		return nil, false, err
	}
	current, err := s.repo.FindForOwner(ctx, expected.OwnerIdentity, expected.TemporalWorkflowID)
	if err != nil {
		return nil, false, err
	}
	if current == nil || current.ID != expected.ID || current.OwnerIdentity != expected.OwnerIdentity ||
		current.TemporalWorkflowID != expected.TemporalWorkflowID || current.WorkflowType != expected.WorkflowType ||
		!current.ScheduledFor.Equal(expected.ScheduledFor) {
		return nil, false, errors.New("durable Temporal transition returned unconfirmed provenance")
	}
	return current, changed, nil
}

func confirmedScheduleState(run *models.TemporalWorkflowRun) bool {
	if run == nil {
		return false
	}
	switch run.Status {
	case "scheduled":
		return run.StartedAt == nil && run.CompletedAt == nil
	case "running", "failed":
		return run.StartedAt != nil && run.CompletedAt == nil
	case "completed":
		return run.StartedAt != nil && run.CompletedAt != nil
	default:
		return false
	}
}

func (s *Service) Runs(ownerIdentity string, limit int) ([]FollowUpRun, error) {
	records, err := s.repo.ListForOwner(strings.TrimSpace(ownerIdentity), limit)
	if err != nil {
		return nil, err
	}
	runs := make([]FollowUpRun, 0, len(records))
	for _, record := range records {
		runs = append(runs, runFromModel(record))
	}
	return runs, nil
}

func GovernedFollowUpWorkflow(ctx temporalworkflow.Context, input FollowUpInput) (FollowUpResult, error) {
	if wait := input.RunAt.Sub(temporalworkflow.Now(ctx)); wait > 0 {
		if err := temporalworkflow.Sleep(ctx, wait); err != nil {
			return FollowUpResult{}, err
		}
	}
	options := temporalworkflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, MaximumAttempts: 3},
	}
	ctx = temporalworkflow.WithActivityOptions(ctx, options)
	var result FollowUpResult
	var run temporalworkflow.Future
	// Preserve the recorded command for histories created before input was
	// forwarded. New executions carry only the opaque, bounded run metadata.
	if temporalworkflow.GetVersion(ctx, "governed-follow-up-activity-input", temporalworkflow.DefaultVersion, 1) == temporalworkflow.DefaultVersion {
		run = temporalworkflow.ExecuteActivity(ctx, "Run")
	} else {
		run = temporalworkflow.ExecuteActivity(ctx, "Run", input)
	}
	if err := run.Get(ctx, &result); err != nil {
		return FollowUpResult{}, err
	}
	return result, nil
}

type followUpActivity struct {
	repo      Repository
	workflows workflow.Service
	now       func() time.Time
}

func (a *followUpActivity) Run(ctx context.Context, input FollowUpInput) (FollowUpResult, error) {
	finish, admitted := lifecycle.Enter(ctx, "temporal-follow-up-activity")
	if !admitted {
		return FollowUpResult{}, context.Canceled
	}
	defer finish()
	if a.repo == nil || a.workflows == nil {
		return FollowUpResult{}, errors.New("governed follow-up activity is not initialized")
	}
	// The durable run ID is the only record key supplied to the activity. The
	// owner identity stays in HAI's own database and never enters Temporal.
	runID, parseErr := uuid.Parse(input.RunID)
	if parseErr != nil {
		return FollowUpResult{}, errors.New("invalid governed follow-up run")
	}
	result, err := a.runByID(ctx, runID, input.Limit)
	if err != nil {
		return FollowUpResult{}, err
	}
	return result, nil
}

func (a *followUpActivity) runByID(ctx context.Context, runID uuid.UUID, limit int) (FollowUpResult, error) {
	record, err := a.repo.FindByIDContext(ctx, runID)
	if ctx.Err() != nil {
		return FollowUpResult{}, ctx.Err()
	}
	if err != nil {
		return FollowUpResult{}, errors.New("governed activity record lookup failed")
	}
	if record == nil || record.ID != runID {
		return FollowUpResult{}, errors.New("invalid governed follow-up run record")
	}
	ownerIdentity := strings.TrimSpace(record.OwnerIdentity)
	if ownerIdentity == "" || record.TemporalWorkflowID == "" || record.WorkflowType != followUpWorkflowType {
		return FollowUpResult{}, errors.New("invalid governed follow-up run record")
	}
	if record.Status == "completed" {
		if !confirmedScheduleState(record) {
			return FollowUpResult{}, errors.New("unconfirmed governed follow-up completion")
		}
		var result *FollowUpResult
		if err := json.Unmarshal([]byte(record.ResultJSON), &result); err != nil || result == nil {
			return FollowUpResult{}, errors.New("invalid governed follow-up completion result")
		}
		return *result, nil
	}
	switch record.Status {
	case "dispatching", "scheduled", "schedule_uncertain", "running":
	case "failed":
		if record.StartedAt == nil {
			return FollowUpResult{}, errors.New("rejected governed schedule cannot execute")
		}
	default:
		return FollowUpResult{}, errors.New("governed schedule has not passed the dispatch boundary")
	}
	if record.StartedAt != nil || record.CompletedAt != nil || record.Status == "running" {
		return FollowUpResult{}, activityReviewRequired()
	}
	workflows, ok := a.workflows.(workflow.ContextualFollowUpService)
	if !ok {
		return FollowUpResult{}, temporal.NewNonRetryableApplicationError(
			"context-aware governed follow-up adapter is required", "GovernedFollowUpUnavailable", nil)
	}
	expected := *record
	now := a.now().UTC().Truncate(time.Microsecond)
	claimed := expected
	claimed.Status, claimed.StartedAt, claimed.UpdatedAt = "running", &now, now
	claimed.Summary = "governed follow-up check is running through HAI controls"
	changed, err := a.repo.TransitionActivity(ctx, expected, claimed)
	if err != nil {
		return FollowUpResult{}, errors.New("governed activity claim could not be confirmed")
	}
	if !changed {
		return FollowUpResult{}, activityReviewRequired()
	}
	if err := ctx.Err(); err != nil {
		if settleErr := a.settleActivity(ctx, claimed, "failed", "activity was canceled before proposal generation", "{}"); settleErr != nil {
			return FollowUpResult{}, settleErr
		}
		return FollowUpResult{}, err
	}
	summary, err := workflows.RunDueOpenLoopsForOwnerContext(ctx, ownerIdentity, workflow.RunDueRequest{Limit: normalizeLimit(limit)})
	if err != nil || summary == nil {
		if settleErr := a.settleActivity(ctx, claimed, "failed", "follow-up proposal outcome requires review before retrying", "{}"); settleErr != nil {
			return FollowUpResult{}, settleErr
		}
		return FollowUpResult{}, activityReviewRequired()
	}
	result := FollowUpResult{Checked: summary.Checked, Triggered: summary.Triggered, Resolved: summary.Resolved, Skipped: summary.Skipped,
		Summary: fmt.Sprintf("HAI checked %d due open loops and created %d follow-up proposals", summary.Checked, summary.Triggered)}
	encoded, _ := json.Marshal(result)
	if err := a.settleActivity(ctx, claimed, "completed", result.Summary, string(encoded)); err != nil {
		return FollowUpResult{}, err
	}
	return result, nil
}

func activityReviewRequired() error {
	return temporal.NewNonRetryableApplicationError(
		"governed activity outcome requires review; inspect this same run before retrying",
		"GovernedActivityNeedsReview", nil)
}

func (a *followUpActivity) settleActivity(ctx context.Context, claimed models.TemporalWorkflowRun, status, summary, encoded string) error {
	// The effect may already have returned after caller cancellation. Record its
	// outcome without repeating it; never release/reclaim an ambiguous activity.
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	next := claimed
	next.Status, next.Summary, next.ResultJSON = status, summary, encoded
	next.UpdatedAt = a.now().UTC().Truncate(time.Microsecond)
	if status == "completed" {
		next.CompletedAt = &next.UpdatedAt
	}
	changed, err := a.repo.TransitionActivity(settleCtx, claimed, next)
	if err != nil || !changed {
		return activityReviewRequired()
	}
	return nil
}

func validateConfig(value config) string {
	if value.address == "" || value.namespace == "" || value.queue == "" {
		return "HAI_TEMPORAL_ADDRESS, HAI_TEMPORAL_NAMESPACE, and HAI_TEMPORAL_TASK_QUEUE are required when HAI_TEMPORAL_ENABLED=true"
	}
	for _, field := range []string{value.address, value.namespace, value.queue} {
		if safety.RedactSecrets(field) != field {
			return "Temporal configuration must not contain credentials"
		}
	}
	host, port, err := net.SplitHostPort(value.address)
	if err != nil || port == "" {
		return "HAI_TEMPORAL_ADDRESS must be a local host:port value"
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || host == "host.docker.internal" || host == "temporal" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return ""
	}
	return "HAI_TEMPORAL_ADDRESS may only target localhost, loopback IPs, host.docker.internal, or the local temporal service"
}

func validateRequest(now time.Time, request FollowUpRequest) error {
	if request.RunAt.IsZero() {
		return apierror.New(apierror.CodeBadRequest, "runAt is required")
	}
	if request.RunAt.Before(now.Add(-time.Minute)) || request.RunAt.After(now.Add(maxRunAhead)) {
		return apierror.New(apierror.CodeBadRequest, "runAt must be within the next 365 days")
	}
	if request.Limit < 0 || request.Limit > maxFollowUpLimit {
		return apierror.New(apierror.CodeBadRequest, fmt.Sprintf("limit must be between 1 and %d", maxFollowUpLimit))
	}
	return nil
}

func normalizeLimit(value int) int {
	if value <= 0 {
		return 10
	}
	if value > maxFollowUpLimit {
		return maxFollowUpLimit
	}
	return value
}

func runFromModel(record models.TemporalWorkflowRun) FollowUpRun {
	var result FollowUpResult
	_ = json.Unmarshal([]byte(record.ResultJSON), &result)
	return FollowUpRun{ID: record.ID, TemporalWorkflowID: record.TemporalWorkflowID, WorkflowType: record.WorkflowType, Status: record.Status, ScheduledFor: record.ScheduledFor, StartedAt: record.StartedAt, CompletedAt: record.CompletedAt, Summary: record.Summary, Result: result, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}
