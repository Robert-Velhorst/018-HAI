package temporalbridge

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	historypb "go.temporal.io/api/history/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	temporalworkflow "go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workflowBoundaryService struct {
	workflow.Service
	owners []string
	limits []int
}

func (s *workflowBoundaryService) RunDueOpenLoopsForOwner(owner string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	s.owners = append(s.owners, owner)
	s.limits = append(s.limits, request.Limit)
	return &workflow.OpenLoopRunSummary{Checked: 4, Triggered: 2, Resolved: 1, Skipped: 1}, nil
}

func (s *workflowBoundaryService) RunDueOpenLoopsForOwnerContext(ctx context.Context, owner string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.RunDueOpenLoopsForOwner(owner, request)
}

func TestWorkflowBoundaryTransportsInputToActualGovernedActivity(t *testing.T) {
	for _, delay := range []time.Duration{0, 2 * time.Hour} {
		t.Run(delay.String(), func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			at := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
			env.SetStartTime(at)
			repo := newMemoryTemporalRepository()
			id := uuid.New()
			input := FollowUpInput{RunID: id.String(), RunAt: at.Add(delay), Limit: 7}
			_, err := repo.Create(&models.TemporalWorkflowRun{
				ID: id, OwnerIdentity: "robert@example.test", TemporalWorkflowID: "hai-follow-up-" + id.String(),
				WorkflowType: followUpWorkflowType, Status: "scheduled", ScheduledFor: input.RunAt, ResultJSON: "{}",
			})
			require.NoError(t, err)
			work := &workflowBoundaryService{}
			activity := &followUpActivity{repo: repo, workflows: work, now: func() time.Time { return input.RunAt }}
			// Use the actual registered method and SDK payload conversion, not a
			// replacement that succeeds regardless of missing activity arguments.
			env.RegisterActivity(activity.Run)
			env.ExecuteWorkflow(GovernedFollowUpWorkflow, input)
			require.True(t, env.IsWorkflowCompleted())
			require.NoError(t, env.GetWorkflowError())
			var result FollowUpResult
			require.NoError(t, env.GetWorkflowResult(&result))
			require.Equal(t, FollowUpResult{Checked: 4, Triggered: 2, Resolved: 1, Skipped: 1,
				Summary: "HAI checked 4 due open loops and created 2 follow-up proposals"}, result)
			require.Equal(t, []string{"robert@example.test"}, work.owners)
			require.Equal(t, []int{7}, work.limits)
			require.True(t, input.RunAt.Equal(env.Now()), "the activity runs at the scheduled instant in any local timezone")
			row, err := repo.FindByID(id)
			require.NoError(t, err)
			require.Equal(t, "completed", row.Status)
			require.Equal(t, input.RunAt, *row.StartedAt)
			require.Equal(t, input.RunAt, *row.CompletedAt)
		})
	}
}

func TestWorkflowBoundaryCompletedNullResultNeverReportsSuccess(t *testing.T) {
	for _, encoded := range []string{"null", " \n null \t"} {
		t.Run(encoded, func(t *testing.T) {
			id, at := uuid.New(), time.Now().UTC()
			row := &models.TemporalWorkflowRun{ID: id, OwnerIdentity: "robert@example.test", TemporalWorkflowID: "hai-follow-up-" + id.String(),
				WorkflowType: followUpWorkflowType, Status: "completed", StartedAt: &at, CompletedAt: &at, ResultJSON: encoded}
			work := &workflowBoundaryService{}
			activity := &followUpActivity{repo: activityRecordTemporalRepository{row: row}, workflows: work, now: time.Now}
			_, err := activity.Run(context.Background(), FollowUpInput{RunID: id.String()})
			require.Error(t, err, "JSON null does not prove a recorded completion result")
			require.Empty(t, work.owners, "invalid completion must not regenerate proposals")
		})
	}
}

func TestWorkflowBoundaryLegacyMissingInputCannotSelectAnotherOwner(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	at := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(at)
	env.OnGetVersion("governed-follow-up-activity-input", temporalworkflow.DefaultVersion, 1).
		Return(temporalworkflow.DefaultVersion).Once()
	repo := newMemoryTemporalRepository()
	id := uuid.New()
	_, err := repo.Create(&models.TemporalWorkflowRun{
		ID: id, OwnerIdentity: "robert@example.test", TemporalWorkflowID: "hai-follow-up-" + id.String(),
		WorkflowType: followUpWorkflowType, Status: "scheduled", ScheduledFor: at, ResultJSON: "{}",
	})
	require.NoError(t, err)
	work := &workflowBoundaryService{}
	activity := &followUpActivity{repo: repo, workflows: work, now: func() time.Time { return at }}
	env.RegisterActivity(activity.Run)
	env.ExecuteWorkflow(GovernedFollowUpWorkflow, FollowUpInput{RunID: id.String(), RunAt: at, Limit: 7})
	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "invalid governed follow-up run")
	require.Empty(t, work.owners, "never guess an owner or create proposals for a history missing its run input")
	row, err := repo.FindByID(id)
	require.NoError(t, err)
	require.Equal(t, "scheduled", row.Status, "legacy missing payload is not silently repaired/re-authorized")
	env.AssertExpectations(t)
}

func TestWorkflowBoundaryCancellationWhileWaitingDoesNotRunActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	at := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(at)
	work := &workflowBoundaryService{}
	activity := &followUpActivity{repo: newMemoryTemporalRepository(), workflows: work, now: time.Now}
	env.RegisterActivity(activity.Run)
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Minute)
	env.ExecuteWorkflow(GovernedFollowUpWorkflow, FollowUpInput{RunID: uuid.NewString(), RunAt: at.Add(time.Hour), Limit: 7})
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Empty(t, work.owners)
	require.True(t, at.Add(time.Minute).Equal(env.Now()))
}

func TestWorkflowBoundaryReplaysLegacyFailureWithoutVersionMarker(t *testing.T) {
	at := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	input, err := converter.GetDefaultDataConverter().ToPayloads(FollowUpInput{
		RunID: "00000000-0000-0000-0000-000000000001", RunAt: at, Limit: 7,
	})
	require.NoError(t, err)
	failure := &failurepb.Failure{Message: "invalid governed follow-up run",
		FailureInfo: &failurepb.Failure_ApplicationFailureInfo{ApplicationFailureInfo: &failurepb.ApplicationFailureInfo{Type: "errorString"}}}
	// Constructed legacy history, not an exported/private server history. Replay
	// consumes its missing activity payload; it does not execute any activity.
	history := &historypb.History{Events: []*historypb.HistoryEvent{
		{EventId: 1, EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: &historypb.WorkflowExecutionStartedEventAttributes{
				WorkflowType: &commonpb.WorkflowType{Name: "GovernedFollowUpWorkflow"}, TaskQueue: &taskqueuepb.TaskQueue{Name: "hai-test"}, Input: input,
			}}},
		{EventId: 2, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{}}},
		{EventId: 3, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 2}}},
		{EventId: 4, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 2, StartedEventId: 3}}},
		{EventId: 5, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{
				ActivityId: "5", ActivityType: &commonpb.ActivityType{Name: "Run"}, TaskQueue: &taskqueuepb.TaskQueue{Name: "hai-test"}, WorkflowTaskCompletedEventId: 4,
			}}},
		{EventId: 6, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_ActivityTaskStartedEventAttributes{ActivityTaskStartedEventAttributes: &historypb.ActivityTaskStartedEventAttributes{ScheduledEventId: 5}}},
		{EventId: 7, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_FAILED,
			Attributes: &historypb.HistoryEvent_ActivityTaskFailedEventAttributes{ActivityTaskFailedEventAttributes: &historypb.ActivityTaskFailedEventAttributes{
				ScheduledEventId: 5, StartedEventId: 6, Failure: failure, RetryState: enumspb.RETRY_STATE_MAXIMUM_ATTEMPTS_REACHED,
			}}},
		{EventId: 8, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{}}},
		{EventId: 9, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 8}}},
		{EventId: 10, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 8, StartedEventId: 9}}},
		{EventId: 11, EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_FAILED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionFailedEventAttributes{WorkflowExecutionFailedEventAttributes: &historypb.WorkflowExecutionFailedEventAttributes{WorkflowTaskCompletedEventId: 10, Failure: failure}}},
	}}
	for _, event := range history.Events {
		event.EventTime = timestamppb.New(at)
	}
	replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{})
	require.NoError(t, err)
	replayer.RegisterWorkflow(GovernedFollowUpWorkflow)
	require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
}

func TestWorkflowBoundaryScheduleAdapterAndActivityCompleteSameRun(t *testing.T) {
	authorizer := &recordingFinalEffectAuthorizer{}
	h := newScheduleSecurityHarness(t, authorizer)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(h.now)
	work := &workflowBoundaryService{}
	activity := &followUpActivity{repo: h.repo, workflows: work, now: func() time.Time { return h.now }}
	env.RegisterActivity(activity.Run)
	sdk := &mocks.Client{}
	var dispatched FollowUpInput
	sdk.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			require.Equal(t, 1, authorizer.calls, "authorization precedes SDK dispatch")
			require.Equal(t, "dispatching", h.repo.lastStatus())
			dispatched = args.Get(3).(FollowUpInput)
			options := args.Get(1).(client.StartWorkflowOptions)
			row, err := h.repo.FindByID(uuid.MustParse(dispatched.RunID))
			require.NoError(t, err)
			require.Equal(t, row.TemporalWorkflowID, options.ID)
			require.Equal(t, h.service.config.queue, options.TaskQueue)
			env.ExecuteWorkflow(args.Get(2), dispatched)
			require.NoError(t, env.GetWorkflowError())
		}).Return(nil, nil).Once()
	h.service.scheduler = temporalClientScheduler{client: sdk}
	run, err := h.service.ScheduleFollowUp(context.Background(), "robert@example.test", FollowUpRequest{RunAt: h.now, Limit: 7})
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, dispatched.RunID, run.ID.String())
	require.Equal(t, "completed", run.Status, "SDK acceptance must not overwrite completed activity progress")
	require.Equal(t, 4, run.Result.Checked)
	require.Equal(t, 2, run.Result.Triggered)
	require.Equal(t, []string{"robert@example.test"}, work.owners)
	require.Equal(t, []int{7}, work.limits)
	sdk.AssertExpectations(t)
}
