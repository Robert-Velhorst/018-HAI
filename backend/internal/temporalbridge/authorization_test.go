package temporalbridge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
)

func TestScheduleFollowUpFailsClosedWithoutFinalEffectAuthorizer(
	t *testing.T,
) {
	harness := newScheduleSecurityHarness(t, nil)

	_, err := harness.service.ScheduleFollowUp(
		context.Background(),
		"robert@example.test",
		FollowUpRequest{RunAt: harness.now.Add(time.Hour), Limit: 5},
	)
	if !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("ScheduleFollowUp error = %v, want authorization required", err)
	}
	if harness.scheduler.calls != 0 {
		t.Fatalf("scheduler calls = %d, want 0", harness.scheduler.calls)
	}
	if got := harness.repo.lastStatus(); got != "failed" {
		t.Fatalf("stored run status = %q, want failed", got)
	}
}

func TestScheduleFollowUpDenialNeverReachesTemporal(t *testing.T) {
	authorizer := &recordingFinalEffectAuthorizer{
		err: errors.New("authorization denied"),
	}
	harness := newScheduleSecurityHarness(t, authorizer)
	stopChecks := 0
	harness.service.WithEmergencyStopEvaluator(
		func() safety.EmergencyStopDecision {
			stopChecks++
			return safety.EmergencyStopDecision{}
		},
	)

	_, err := harness.service.ScheduleFollowUp(
		context.Background(),
		"robert@example.test",
		FollowUpRequest{RunAt: harness.now.Add(time.Hour), Limit: 5},
	)
	if !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("ScheduleFollowUp error = %v, want authorization required", err)
	}
	if authorizer.calls != 1 || harness.scheduler.calls != 0 {
		t.Fatalf(
			"authorizer calls = %d, scheduler calls = %d; want 1 and 0",
			authorizer.calls,
			harness.scheduler.calls,
		)
	}
	if stopChecks != 0 {
		t.Fatalf("stop checks = %d, want 0 after authorization denial", stopChecks)
	}
}

func TestScheduleFollowUpRechecksEmergencyStopAfterAuthorization(
	t *testing.T,
) {
	events := []string{}
	stopped := false
	authorizer := &recordingFinalEffectAuthorizer{
		events: &events,
		afterAuthorize: func() {
			stopped = true
		},
	}
	harness := newScheduleSecurityHarness(t, authorizer)
	harness.scheduler.events = &events
	harness.service.WithEmergencyStopEvaluator(
		func() safety.EmergencyStopDecision {
			events = append(events, "stop")
			return safety.EmergencyStopDecision{
				Active: stopped,
				Source: "test",
				Reason: "operator stop",
			}
		},
	)

	_, err := harness.service.ScheduleFollowUp(
		context.Background(),
		"robert@example.test",
		FollowUpRequest{RunAt: harness.now.Add(time.Hour), Limit: 5},
	)
	if !errors.Is(err, ErrEmergencyStopActive) {
		t.Fatalf("ScheduleFollowUp error = %v, want emergency stop", err)
	}
	if harness.scheduler.calls != 0 {
		t.Fatalf("scheduler calls = %d, want 0", harness.scheduler.calls)
	}
	if got := strings.Join(events, ","); got != "authorize,stop" {
		t.Fatalf("boundary order = %q, want authorize,stop", got)
	}
}

func TestScheduleFollowUpBindsExactEffectAtFinalBoundary(t *testing.T) {
	events := []string{}
	authorizer := &recordingFinalEffectAuthorizer{events: &events}
	harness := newScheduleSecurityHarness(t, authorizer)
	harness.scheduler.events = &events
	harness.service.WithEmergencyStopEvaluator(
		func() safety.EmergencyStopDecision {
			events = append(events, "stop")
			return safety.EmergencyStopDecision{Source: "test"}
		},
	)
	request := FollowUpRequest{
		RunAt:                 harness.now.Add(2 * time.Hour),
		Limit:                 7,
		TaskID:                "task-follow-up-1",
		ProjectKey:            "project-vivare",
		ApprovalSourceID:      "workflow-decision:approval-1",
		ApprovalBindingDigest: strings.Repeat("a", 64),
	}

	run, err := harness.service.ScheduleFollowUp(
		context.Background(),
		"robert@example.test",
		request,
	)
	if err != nil {
		t.Fatalf("ScheduleFollowUp: %v", err)
	}
	if run == nil || run.Status != "scheduled" {
		t.Fatalf("scheduled run = %#v", run)
	}
	if got := strings.Join(events, ","); got != "authorize,stop,schedule" {
		t.Fatalf(
			"final boundary order = %q, want authorize,stop,schedule",
			got,
		)
	}
	if authorizer.calls != 1 || harness.scheduler.calls != 1 {
		t.Fatalf(
			"authorizer calls = %d, scheduler calls = %d; want 1 and 1",
			authorizer.calls,
			harness.scheduler.calls,
		)
	}

	got := authorizer.request
	if got.OwnerIdentity != "robert@example.test" ||
		got.ActorIdentity != got.OwnerIdentity ||
		got.ActorKind != executionauth.ActorHuman ||
		got.Action != scheduleFollowUpAction ||
		got.ResourceType != scheduleFollowUpResourceType ||
		got.ResourceID != run.ID.String() ||
		got.TaskID != request.TaskID ||
		got.ProjectKey != request.ProjectKey ||
		got.ApprovalSourceID != request.ApprovalSourceID ||
		got.ApprovalBindingDigest != request.ApprovalBindingDigest ||
		got.RuntimeID != temporalRuntimeID ||
		got.ToolID != followUpWorkflowType {
		t.Fatalf("authorization request was not exactly bound: %#v", got)
	}
	if len(got.EffectDigest) != 64 ||
		authorizer.target != temporalRuntimeID+":"+got.EffectDigest {
		t.Fatalf(
			"effect digest/target = %q / %q",
			got.EffectDigest,
			authorizer.target,
		)
	}
	if authorizer.consumer != temporalAuthorizationConsumer {
		t.Fatalf("consumer = %q", authorizer.consumer)
	}
	if got.Facts["workflowId"] != run.TemporalWorkflowID ||
		got.Facts["workflowType"] != followUpWorkflowType {
		t.Fatalf("workflow provenance = %#v", got.Facts)
	}
	if harness.scheduler.options.ID != run.TemporalWorkflowID ||
		harness.scheduler.input.RunID != run.ID.String() ||
		!harness.scheduler.input.RunAt.Equal(request.RunAt.UTC()) ||
		harness.scheduler.input.Limit != request.Limit {
		t.Fatalf(
			"scheduler effect = options=%#v input=%#v",
			harness.scheduler.options,
			harness.scheduler.input,
		)
	}
}

type scheduleSecurityHarness struct {
	service   *Service
	repo      *memoryTemporalRepository
	scheduler *recordingScheduler
	now       time.Time
}

type unconfirmedTemporalRepository struct {
	Repository
	change func(*models.TemporalWorkflowRun)
}

func (r unconfirmedTemporalRepository) Create(run *models.TemporalWorkflowRun) (*models.TemporalWorkflowRun, error) {
	run.ID = uuid.New()
	r.change(run)
	return run, nil
}

func TestScheduleFollowUpRejectsUnconfirmedStorageProvenanceBeforeAuthorization(t *testing.T) {
	for name, change := range map[string]func(*models.TemporalWorkflowRun){
		"missing ID":          func(run *models.TemporalWorkflowRun) { run.ID = uuid.Nil },
		"other owner":         func(run *models.TemporalWorkflowRun) { run.OwnerIdentity = "other@example.test" },
		"other workflow":      func(run *models.TemporalWorkflowRun) { run.TemporalWorkflowID = "other-workflow" },
		"other workflow type": func(run *models.TemporalWorkflowRun) { run.WorkflowType = "other-workflow-type" },
		"already completed":   func(run *models.TemporalWorkflowRun) { run.Status = "completed" },
		"different schedule":  func(run *models.TemporalWorkflowRun) { run.ScheduledFor = run.ScheduledFor.Add(time.Hour) },
		"already started":     func(run *models.TemporalWorkflowRun) { at := run.ScheduledFor; run.StartedAt = &at },
		"has completion time": func(run *models.TemporalWorkflowRun) { at := run.ScheduledFor; run.CompletedAt = &at },
	} {
		t.Run(name, func(t *testing.T) {
			authorizer := &recordingFinalEffectAuthorizer{}
			harness := newScheduleSecurityHarness(t, authorizer)
			harness.service.repo = unconfirmedTemporalRepository{Repository: harness.repo, change: change}
			result, err := harness.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: harness.now.Add(time.Hour), Limit: 5})
			if err == nil || result != nil {
				t.Errorf("unconfirmed storage provenance was accepted: result=%+v err=%v", result, err)
			}
			if authorizer.calls != 0 || harness.scheduler.calls != 0 {
				t.Errorf("unconfirmed storage reached authorization/scheduling: %d/%d", authorizer.calls, harness.scheduler.calls)
			}
		})
	}
}

func TestScheduleFollowUpWithoutRepositoryDoesNotConsumeAuthorization(t *testing.T) {
	authorizer := &recordingFinalEffectAuthorizer{}
	harness := newScheduleSecurityHarness(t, authorizer)
	harness.service.repo = nil
	result, err := harness.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: harness.now.Add(time.Hour), Limit: 5})
	if !errors.Is(err, ErrUnavailable) || result != nil || authorizer.calls != 0 || harness.scheduler.calls != 0 {
		t.Fatalf("missing storage did not fail closed: result=%+v err=%v authorize=%d schedule=%d", result, err, authorizer.calls, harness.scheduler.calls)
	}
}

func newScheduleSecurityHarness(
	t *testing.T,
	authorizer FinalEffectAuthorizer,
) scheduleSecurityHarness {
	t.Helper()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	repo := newMemoryTemporalRepository()
	service := NewService(
		repo,
		nil,
		true,
		"localhost:7233",
		"default",
		"hai-governed-follow-up",
		authorizer,
	)
	service.now = func() time.Time { return now }
	scheduler := &recordingScheduler{}
	service.scheduler = scheduler
	service.WithEmergencyStopEvaluator(
		func() safety.EmergencyStopDecision {
			return safety.EmergencyStopDecision{Source: "test"}
		},
	)
	return scheduleSecurityHarness{
		service:   service,
		repo:      repo,
		scheduler: scheduler,
		now:       now,
	}
}

type recordingFinalEffectAuthorizer struct {
	calls          int
	request        executionauth.Request
	consumer       string
	target         string
	err            error
	events         *[]string
	afterAuthorize func()
}

func (a *recordingFinalEffectAuthorizer) AuthorizeAndConsume(
	_ context.Context,
	request executionauth.Request,
	consumer string,
	target string,
) (executionauth.Receipt, error) {
	a.calls++
	a.request = request
	a.consumer = consumer
	a.target = target
	if a.events != nil {
		*a.events = append(*a.events, "authorize")
	}
	if a.afterAuthorize != nil {
		a.afterAuthorize()
	}
	if a.err != nil {
		return executionauth.Receipt{}, a.err
	}
	return executionauth.Receipt{
		OwnerIdentity: request.OwnerIdentity,
		TaskID:        request.TaskID,
		Action:        request.Action,
		ResourceType:  request.ResourceType,
		ResourceID:    request.ResourceID,
		ProjectKey:    request.ProjectKey,
		EffectDigest:  request.EffectDigest,
		Outcome:       executionauth.OutcomeAuthorized,
	}, nil
}

type recordingScheduler struct {
	calls      int
	options    client.StartWorkflowOptions
	input      FollowUpInput
	err        error
	events     *[]string
	onSchedule func()
}

func (s *recordingScheduler) Schedule(
	_ context.Context,
	options client.StartWorkflowOptions,
	input FollowUpInput,
) error {
	s.calls++
	s.options = options
	s.input = input
	if s.events != nil {
		*s.events = append(*s.events, "schedule")
	}
	if s.onSchedule != nil {
		s.onSchedule()
	}
	return s.err
}

type memoryTemporalRepository struct {
	mu      sync.Mutex
	records map[uuid.UUID]models.TemporalWorkflowRun
	order   []uuid.UUID
}

func newMemoryTemporalRepository() *memoryTemporalRepository {
	return &memoryTemporalRepository{
		records: map[uuid.UUID]models.TemporalWorkflowRun{},
	}
}

func (r *memoryTemporalRepository) Create(
	run *models.TemporalWorkflowRun,
) (*models.TemporalWorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := *run
	if value.ID == uuid.Nil {
		value.ID = uuid.New()
	}
	r.records[value.ID] = value
	r.order = append(r.order, value.ID)
	*run = value
	return run, nil
}

func (r *memoryTemporalRepository) Update(
	run *models.TemporalWorkflowRun,
) (*models.TemporalWorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := *run
	r.records[value.ID] = value
	return run, nil
}

func (r *memoryTemporalRepository) FindByID(
	id uuid.UUID,
) (*models.TemporalWorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.records[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	return &value, nil
}

func (r *memoryTemporalRepository) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.TemporalWorkflowRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.FindByID(id)
}

func (r *memoryTemporalRepository) TransitionActivity(ctx context.Context, expected, next models.TemporalWorkflowRun) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.records[expected.ID]
	if !ok || !validActivityTransition(expected, next) || current.OwnerIdentity != expected.OwnerIdentity ||
		current.TemporalWorkflowID != expected.TemporalWorkflowID || current.WorkflowType != expected.WorkflowType ||
		!current.ScheduledFor.Equal(expected.ScheduledFor) || current.Status != expected.Status ||
		!current.UpdatedAt.Equal(expected.UpdatedAt) || current.CompletedAt != nil ||
		(current.StartedAt == nil) != (expected.StartedAt == nil) ||
		(current.StartedAt != nil && !current.StartedAt.Equal(*expected.StartedAt)) {
		return false, nil
	}
	current.Status, current.StartedAt, current.CompletedAt = next.Status, next.StartedAt, next.CompletedAt
	current.Summary, current.ResultJSON, current.UpdatedAt = next.Summary, next.ResultJSON, next.UpdatedAt
	r.records[expected.ID] = current
	return true, nil
}

func (r *memoryTemporalRepository) FindForOwner(
	ctx context.Context,
	ownerIdentity string,
	workflowID string,
) (*models.TemporalWorkflowRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range r.records {
		if value.OwnerIdentity == ownerIdentity &&
			value.TemporalWorkflowID == workflowID {
			copy := value
			return &copy, nil
		}
	}
	return nil, errors.New("run not found")
}

func (r *memoryTemporalRepository) TransitionSchedule(ctx context.Context, expected models.TemporalWorkflowRun, from, to, summary string, at time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.records[expected.ID]
	if !ok || !validScheduleTransition(from, to) || current.OwnerIdentity != expected.OwnerIdentity ||
		current.TemporalWorkflowID != expected.TemporalWorkflowID || current.WorkflowType != expected.WorkflowType ||
		!current.ScheduledFor.Equal(expected.ScheduledFor) || current.Status != from || current.StartedAt != nil || current.CompletedAt != nil {
		return false, nil
	}
	current.Status, current.Summary, current.UpdatedAt = to, summary, at
	r.records[current.ID] = current
	return true, nil
}

func (r *memoryTemporalRepository) ListForOwner(
	ownerIdentity string,
	limit int,
) ([]models.TemporalWorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make([]models.TemporalWorkflowRun, 0, len(r.records))
	for _, id := range r.order {
		value := r.records[id]
		if value.OwnerIdentity == ownerIdentity {
			values = append(values, value)
		}
		if limit > 0 && len(values) == limit {
			break
		}
	}
	return values, nil
}

func (r *memoryTemporalRepository) lastStatus() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.order) == 0 {
		return ""
	}
	return r.records[r.order[len(r.order)-1]].Status
}
