package temporalbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type failedScheduleWriteRepository struct{ Repository }

func (r failedScheduleWriteRepository) Update(*models.TemporalWorkflowRun) (*models.TemporalWorkflowRun, error) {
	return nil, errors.New("synthetic status-write failure")
}

func (r failedScheduleWriteRepository) TransitionSchedule(context.Context, models.TemporalWorkflowRun, string, string, string, time.Time) (bool, error) {
	return false, errors.New("synthetic status-write failure")
}

func TestScheduleTruthDenialWithFailedStorageNeverClaimsScheduled(t *testing.T) {
	authorizer := &recordingFinalEffectAuthorizer{err: errors.New("denied")}
	h := newScheduleSecurityHarness(t, authorizer)
	h.service.repo = failedScheduleWriteRepository{h.repo}
	run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now.Add(time.Hour)})
	if err == nil || errors.Is(err, ErrAuthorizationRequired) || run != nil || h.scheduler.calls != 0 {
		t.Fatalf("denied schedule reached success/dispatch: run=%+v err=%v calls=%d", run, err, h.scheduler.calls)
	}
	if got := h.repo.lastStatus(); got != "preparing" {
		t.Fatalf("unconfirmed denial cleanup left status=%q; want preparing, not an accepted schedule", got)
	}
}

func TestScheduleTruthLateConfirmationPreservesAllWorkerOutcomes(t *testing.T) {
	for _, status := range []string{"running", "completed", "failed"} {
		t.Run(status, func(t *testing.T) {
			h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
			h.scheduler.onSchedule = func() {
				record, err := h.repo.FindByID(uuid.MustParse(h.scheduler.input.RunID))
				if err != nil {
					t.Fatal(err)
				}
				at := h.now
				record.Status, record.StartedAt = status, &at
				if status == "completed" {
					record.CompletedAt = &at
				}
				record.Summary = "worker outcome: " + status
				if _, err := h.repo.Update(record); err != nil {
					t.Fatal(err)
				}
			}
			run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now})
			if err != nil || run == nil || run.Status != status || run.Summary != "worker outcome: "+status || h.repo.lastStatus() != status {
				t.Fatalf("late confirmation changed worker outcome: run=%+v err=%v status=%q", run, err, h.repo.lastStatus())
			}
		})
	}
}

type unconfirmedReadTemporalRepository struct {
	Repository
	change func(*models.TemporalWorkflowRun) *models.TemporalWorkflowRun
}

type activityRecordTemporalRepository struct {
	Repository
	row *models.TemporalWorkflowRun
}

func (r activityRecordTemporalRepository) FindByID(uuid.UUID) (*models.TemporalWorkflowRun, error) {
	return r.row, nil
}

func (r activityRecordTemporalRepository) FindByIDContext(context.Context, uuid.UUID) (*models.TemporalWorkflowRun, error) {
	return r.row, nil
}

func TestScheduleTruthActivityValidatesCompletedReplayBeforeReturningSuccess(t *testing.T) {
	id, at := uuid.New(), time.Now().UTC()
	base := models.TemporalWorkflowRun{ID: id, OwnerIdentity: "robert@example.test", TemporalWorkflowID: "test-workflow", WorkflowType: followUpWorkflowType,
		Status: "completed", StartedAt: &at, CompletedAt: &at, ResultJSON: `{"checked":2}`}
	for name, mutate := range map[string]func(*models.TemporalWorkflowRun) *models.TemporalWorkflowRun{
		"missing record": func(*models.TemporalWorkflowRun) *models.TemporalWorkflowRun { return nil },
		"wrong ID":       func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun { row.ID = uuid.New(); return row },
		"missing owner":  func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun { row.OwnerIdentity = ""; return row },
		"missing workflow": func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun {
			row.TemporalWorkflowID = ""
			return row
		},
		"wrong type": func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun {
			row.WorkflowType = "another-type"
			return row
		},
		"missing start":      func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun { row.StartedAt = nil; return row },
		"missing completion": func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun { row.CompletedAt = nil; return row },
		"malformed result":   func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun { row.ResultJSON = "{"; return row },
	} {
		t.Run(name, func(t *testing.T) {
			row := base
			work := &countingTemporalWorkflowService{}
			a := &followUpActivity{repo: activityRecordTemporalRepository{row: mutate(&row)}, workflows: work, now: time.Now}
			_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String()})
			if err == nil || work.calls != 0 {
				t.Fatalf("unconfirmed completion returned success or ran again: %v calls=%d", err, work.calls)
			}
		})
	}
	work := &countingTemporalWorkflowService{}
	a := &followUpActivity{repo: activityRecordTemporalRepository{row: &base}, workflows: work, now: time.Now}
	result, err := a.Run(context.Background(), FollowUpInput{RunID: id.String()})
	if err != nil || result.Checked != 2 || work.calls != 0 {
		t.Fatalf("valid completed replay was changed/repeated: %+v/%v calls=%d", result, err, work.calls)
	}
}

func (r unconfirmedReadTemporalRepository) FindForOwner(ctx context.Context, owner, workflowID string) (*models.TemporalWorkflowRun, error) {
	row, err := r.Repository.FindForOwner(ctx, owner, workflowID)
	if err != nil {
		return nil, err
	}
	return r.change(row), nil
}

func TestScheduleTruthDispatchReadMustConfirmIdentity(t *testing.T) {
	for name, change := range map[string]func(*models.TemporalWorkflowRun) *models.TemporalWorkflowRun{
		"nil reply": func(*models.TemporalWorkflowRun) *models.TemporalWorkflowRun { return nil },
		"wrong ID":  func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun { row.ID = uuid.New(); return row },
		"wrong owner": func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun {
			row.OwnerIdentity = "someone-else"
			return row
		},
		"wrong workflow": func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun {
			row.TemporalWorkflowID = "another-workflow"
			return row
		},
		"wrong type": func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun {
			row.WorkflowType = "another-type"
			return row
		},
		"wrong time": func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun {
			row.ScheduledFor = row.ScheduledFor.Add(time.Hour)
			return row
		},
		"unconfirmed state": func(row *models.TemporalWorkflowRun) *models.TemporalWorkflowRun {
			row.Status = "preparing"
			return row
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
			h.service.repo = unconfirmedReadTemporalRepository{Repository: h.repo, change: change}
			run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now})
			if run != nil || err == nil || h.scheduler.calls != 0 {
				t.Fatalf("unconfirmed read reached SDK: run=%+v err=%v calls=%d", run, err, h.scheduler.calls)
			}
		})
	}
}

func TestScheduleTruthPreparationAndDispatchAreNotAcceptedSchedules(t *testing.T) {
	authorizer := &recordingFinalEffectAuthorizer{}
	h := newScheduleSecurityHarness(t, authorizer)
	authorizer.afterAuthorize = func() {
		if got := h.repo.lastStatus(); got != "preparing" {
			t.Errorf("status during approval=%q; want preparing", got)
		}
	}
	h.scheduler.onSchedule = func() {
		if got := h.repo.lastStatus(); got != "dispatching" {
			t.Errorf("status before SDK confirmation=%q; want dispatching", got)
		}
	}
	run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now.Add(time.Hour)})
	if err != nil || run == nil || run.Status != "scheduled" || h.repo.lastStatus() != "scheduled" {
		t.Fatalf("confirmed schedule was not settled: run=%+v err=%v status=%q", run, err, h.repo.lastStatus())
	}
}

func TestScheduleTruthTransportFailureRetainsUncertainty(t *testing.T) {
	h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
	h.scheduler.err = context.DeadlineExceeded
	run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now.Add(time.Hour)})
	if err == nil || run == nil || h.scheduler.calls != 1 || h.repo.lastStatus() != "schedule_uncertain" {
		t.Fatalf("ambiguous SDK outcome was treated as rejection: run=%+v err=%v calls=%d status=%q", run, err, h.scheduler.calls, h.repo.lastStatus())
	}
}

func TestScheduleTruthLateConfirmationDoesNotRegressCompletedActivity(t *testing.T) {
	h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
	h.scheduler.onSchedule = func() {
		record, err := h.repo.FindByID(uuid.MustParse(h.scheduler.input.RunID))
		if err != nil {
			t.Fatal(err)
		}
		at := h.now
		record.Status, record.StartedAt, record.CompletedAt = "completed", &at, &at
		record.Summary = "worker completed during dispatch"
		record.ResultJSON = `{"checked":3,"summary":"worker completed during dispatch"}`
		if _, err := h.repo.Update(record); err != nil {
			t.Fatal(err)
		}
	}
	run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now})
	if err != nil || run == nil || run.Status != "completed" || run.Result.Checked != 3 || h.repo.lastStatus() != "completed" {
		t.Fatalf("late SDK confirmation lost worker progress: run=%+v err=%v stored=%q", run, err, h.repo.lastStatus())
	}
}

type countingTemporalWorkflowService struct {
	workflow.Service
	calls int
}

func (s *countingTemporalWorkflowService) RunDueOpenLoopsForOwner(string, workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	s.calls++
	return &workflow.OpenLoopRunSummary{}, nil
}

func (s *countingTemporalWorkflowService) RunDueOpenLoopsForOwnerContext(ctx context.Context, owner string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.RunDueOpenLoopsForOwner(owner, request)
}

func TestScheduleTruthActivityRejectsUnconfirmedAndMismatchedRecords(t *testing.T) {
	for _, status := range []string{"preparing", "failed", "unknown", "completed-without-owner", "missing-record"} {
		t.Run(status, func(t *testing.T) {
			repo := newMemoryTemporalRepository()
			id := uuid.New()
			record := &models.TemporalWorkflowRun{ID: id, OwnerIdentity: "robert@example.test", TemporalWorkflowID: "test-workflow", WorkflowType: followUpWorkflowType, Status: status, ResultJSON: "{}"}
			if status == "completed-without-owner" {
				record.Status, record.OwnerIdentity = "completed", ""
			}
			if status != "missing-record" {
				if _, err := repo.Create(record); err != nil {
					t.Fatal(err)
				}
			}
			work := &countingTemporalWorkflowService{}
			a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
			_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String()})
			if err == nil || work.calls != 0 {
				t.Fatalf("unconfirmed run executed or succeeded: err=%v calls=%d", err, work.calls)
			}
		})
	}
}

type uncertainScheduleWriteRepository struct {
	Repository
	failTo   string
	conflict bool
}

func (r uncertainScheduleWriteRepository) TransitionSchedule(ctx context.Context, run models.TemporalWorkflowRun, from, to, summary string, at time.Time) (bool, error) {
	if to == r.failTo {
		if r.conflict {
			return false, nil
		}
		return false, errors.New("database password=synthetic-transition-secret")
	}
	return r.Repository.TransitionSchedule(ctx, run, from, to, summary, at)
}

func TestScheduleTruthUnconfirmedDispatchStorageNeverCallsScheduler(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
		h.service.repo = uncertainScheduleWriteRepository{Repository: h.repo, failTo: "dispatching", conflict: conflict}
		run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now.Add(time.Hour)})
		if run != nil || err == nil || h.scheduler.calls != 0 || h.repo.lastStatus() != "preparing" {
			t.Fatalf("unconfirmed dispatch write reached SDK: run=%+v err=%v calls=%d status=%q", run, err, h.scheduler.calls, h.repo.lastStatus())
		}
	}
}

func TestScheduleTruthFailedSettlementPreservesRunIdentityAndNeverReschedules(t *testing.T) {
	for _, tc := range []struct {
		to             string
		schedulerError error
	}{
		{"scheduled", nil}, {"schedule_uncertain", context.DeadlineExceeded},
	} {
		t.Run(tc.to, func(t *testing.T) {
			h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
			h.service.repo = uncertainScheduleWriteRepository{Repository: h.repo, failTo: tc.to}
			h.scheduler.err = tc.schedulerError
			run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now.Add(time.Hour)})
			if !errors.Is(err, ErrScheduleUncertain) || run == nil || run.ID.String() != h.scheduler.input.RunID ||
				run.TemporalWorkflowID != h.scheduler.options.ID || run.Status != "dispatching" || h.repo.lastStatus() != "dispatching" || h.scheduler.calls != 1 {
				t.Fatalf("lost or repeated attempted effect: run=%+v err=%v calls=%d status=%q", run, err, h.scheduler.calls, h.repo.lastStatus())
			}
		})
	}
}

func TestScheduleTruthCanceledTransportStillPersistsUncertainty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
	h.scheduler.onSchedule = cancel
	h.scheduler.err = context.Canceled
	run, err := h.service.ScheduleFollowUp(ctx, "robert@example.test", FollowUpRequest{RunAt: h.now.Add(time.Hour)})
	if !errors.Is(err, ErrScheduleUncertain) || run == nil || run.Status != "schedule_uncertain" || h.repo.lastStatus() != "schedule_uncertain" {
		t.Fatalf("client cancellation erased dispatch uncertainty: run=%+v err=%v status=%q", run, err, h.repo.lastStatus())
	}
}

func TestScheduleTruthCanonicalTimeSurvivesDatabasePrecision(t *testing.T) {
	authorizer := &recordingFinalEffectAuthorizer{}
	h := newScheduleSecurityHarness(t, authorizer)
	request := FollowUpRequest{RunAt: h.now.Add(time.Hour + 123456789*time.Nanosecond)}
	run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", request)
	canonical := request.RunAt.UTC().Truncate(time.Microsecond)
	if err != nil || run == nil || !run.ScheduledFor.Equal(canonical) || !h.scheduler.input.RunAt.Equal(canonical) {
		t.Fatalf("schedule precision differs at boundaries: run=%+v err=%v input=%+v", run, err, h.scheduler.input)
	}
	// Check the installed PostgreSQL driver's actual wire codec, not a second
	// handwritten implementation of the timestamp precision rule.
	codec, types := pgtype.TimestamptzCodec{}, pgtype.NewMap()
	value := pgtype.Timestamptz{Time: request.RunAt, Valid: true}
	encoded, err := codec.PlanEncode(types, pgtype.TimestamptzOID, pgtype.BinaryFormatCode, value).Encode(value, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded pgtype.Timestamptz
	if err := codec.PlanScan(types, pgtype.TimestamptzOID, pgtype.BinaryFormatCode, &decoded).Scan(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Valid || !decoded.Time.Equal(run.ScheduledFor) {
		t.Fatalf("wire codec changed persisted scheduling time: %v / %v", decoded.Time, run.ScheduledFor)
	}
	request.RunAt = canonical
	expected, _, err := buildScheduleAuthorizationRequest("robert@example.test", run.ID.String(), run.TemporalWorkflowID, request)
	if err != nil || expected.EffectDigest != authorizer.request.EffectDigest {
		t.Fatalf("approval did not bind canonical dispatch time: %v", err)
	}
}

func TestScheduleTruthCanceledRequestDoesNotCreateOrConsume(t *testing.T) {
	authorizer := &recordingFinalEffectAuthorizer{}
	h := newScheduleSecurityHarness(t, authorizer)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run, err := h.service.ScheduleFollowUp(ctx, "robert@example.test", FollowUpRequest{RunAt: h.now.Add(time.Hour)})
	if !errors.Is(err, context.Canceled) || run != nil || h.repo.lastStatus() != "" || authorizer.calls != 0 || h.scheduler.calls != 0 {
		t.Fatalf("already canceled request produced work: run=%+v err=%v authorization=%d schedule=%d", run, err, authorizer.calls, h.scheduler.calls)
	}
}
