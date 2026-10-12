package hostruntimereconcile

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/hostruntime"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestReconcileZeroExitPreservesAutomationSuccessAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	automationID := uuid.New()
	jobID := uuid.New()
	exitCode := 0
	host := &fakeHostJobs{jobs: []hostruntime.Job{{
		ID: jobID, OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness",
		TaskID: "automation:" + automationID.String() + ":intent:" + uuid.NewString(), Status: hostruntime.StatusCompleted,
		Output: "completed without exposed secrets", ExitCode: &exitCode, CreatedAt: now.Add(-time.Minute), CompletedAt: &now,
	}}}
	lastSuccess := now.Add(-2 * time.Hour)
	automation := &models.Automation{ID: automationID, LastSuccessAt: &lastSuccess}
	ledger := &fakeLedger{launch: &models.AutomationLaunchEvent{
		AutomationID: automationID, OwnerIdentity: host.jobs[0].OwnerIdentity,
		RuntimeType: "deepseek-harness", RuntimeTaskID: host.jobs[0].TaskID,
		ExecutionReference: jobID.String(), Status: "queued",
	}, automation: automation}
	service := NewService(host, ledger)
	service.now = func() time.Time { return now }

	count, err := service.ReconcileCompleted(10)
	if err != nil || count != 1 {
		t.Fatalf("ReconcileCompleted = %d, %v", count, err)
	}
	if len(ledger.events) != 1 || ledger.events[0].Status != processSucceededUnverifiedStatus || ledger.events[0].EventKey != completionEventPrefix+jobID.String() {
		t.Fatalf("terminal ledger event = %#v", ledger.events)
	}
	if ledger.updateCalls != 0 || ledger.automation.LastSuccessAt == nil || !ledger.automation.LastSuccessAt.Equal(lastSuccess) || !host.reconciled[jobID] {
		t.Fatalf("process success changed automation success or was not finalized: updates=%d automation=%#v reconciled=%#v", ledger.updateCalls, ledger.automation, host.reconciled)
	}

	if count, err := service.ReconcileCompleted(10); err != nil || count != 0 || len(ledger.events) != 1 {
		t.Fatalf("reconciliation was not idempotent: count=%d err=%v events=%#v", count, err, ledger.events)
	}
}

func TestReconcileZeroExitExplicitlyLeavesTaskOutcomeUnverified(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	automationID, jobID := uuid.New(), uuid.New()
	exitCode := 0
	job := hostruntime.Job{
		ID: jobID, OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness",
		TaskID: "automation:" + automationID.String() + ":intent:" + uuid.NewString(),
		Status: hostruntime.StatusCompleted, ExitCode: &exitCode, CompletedAt: &now,
	}
	host := &fakeHostJobs{jobs: []hostruntime.Job{job}}
	ledger := &fakeLedger{
		launch: &models.AutomationLaunchEvent{
			AutomationID: automationID, OwnerIdentity: job.OwnerIdentity, RuntimeType: job.RuntimeID,
			RuntimeTaskID: job.TaskID, ExecutionReference: job.ID.String(), Status: "queued",
		},
		automation: &models.Automation{ID: automationID},
	}
	if count, err := NewService(host, ledger).ReconcileCompleted(1); err != nil || count != 1 {
		t.Fatalf("ReconcileCompleted = %d, %v; want one process result", count, err)
	}
	event := ledger.events[0]
	if event.Status != "process_succeeded_unverified" || !strings.Contains(event.Message, "process success") || !strings.Contains(event.Message, "task outcome remains unverified") {
		t.Fatalf("zero-exit event does not distinguish process success from task outcome: %#v", event)
	}
}

func TestReconcileCancellationRecordsStopWithoutInferringTaskOutcome(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	automationID, jobID := uuid.New(), uuid.New()
	taskID := "automation:" + automationID.String() + ":intent:" + uuid.NewString()
	executionConfirmedAt, cancelRequestedAt, completedAt := now.Add(-time.Minute), now.Add(-10*time.Second), now
	exitCode := -1
	job := hostruntime.Job{
		ID: jobID, OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: taskID,
		Status: hostruntime.StatusCancelled, ExecutionConfirmedAt: &executionConfirmedAt,
		CancelRequestedAt: &cancelRequestedAt, CompletedAt: &completedAt,
		Output: "private process output must not be copied into the cancellation event", ExitCode: &exitCode,
	}
	lastSuccess, lastFailure := now.Add(-2*time.Hour), now.Add(-3*time.Hour)
	automation := &models.Automation{ID: automationID, LastSuccessAt: &lastSuccess, LastFailureAt: &lastFailure, LastFailureReason: "existing state"}
	host := &fakeHostJobs{cancelJobs: []hostruntime.Job{job}}
	ledger := &fakeLedger{launch: &models.AutomationLaunchEvent{
		AutomationID: automationID, OwnerIdentity: job.OwnerIdentity, RuntimeType: job.RuntimeID,
		RuntimeTaskID: taskID, ExecutionReference: jobID.String(),
	}, automation: automation}
	service := NewService(host, ledger)
	service.now = func() time.Time { return now }

	if count, err := service.ReconcileCompleted(10); err != nil || count != 1 {
		t.Fatalf("ReconcileCompleted = %d, %v; want one cancellation projection", count, err)
	}
	if len(ledger.events) != 1 {
		t.Fatalf("cancellation ledger events = %#v; want one", ledger.events)
	}
	event := ledger.events[0]
	if event.Status != hostruntime.StatusCancelled || event.LaunchType != "agent_runtime_host_cancelled" || event.EventKey != cancellationEventPrefix+jobID.String() || event.ExitCode != -1 {
		t.Fatalf("cancellation event = %#v", event)
	}
	if !strings.Contains(event.Message, "process tree was empty") || !strings.Contains(event.Message, "not rolled back") || event.Output != "" {
		t.Fatalf("cancellation event overstated or exposed execution output: %#v", event)
	}
	if ledger.updateCalls != 0 || automation.LastSuccessAt != &lastSuccess || automation.LastFailureAt != &lastFailure || automation.LastFailureReason != "existing state" {
		t.Fatalf("cancellation changed automation outcome: calls=%d automation=%#v", ledger.updateCalls, automation)
	}
	if !host.reconciled[jobID] {
		t.Fatal("durable cancellation event was not marked reconciled")
	}
}

func TestCancellationProjectionRejectsConfirmedCancellationWithoutRequestEvidence(t *testing.T) {
	automationID, jobID := uuid.New(), uuid.New()
	executionConfirmedAt := time.Now().UTC()
	job := hostruntime.Job{
		ID: jobID, OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness",
		TaskID: "task:" + uuid.NewString(), Status: hostruntime.StatusCancelled,
		ExecutionConfirmedAt: &executionConfirmedAt,
	}
	host := &fakeHostJobs{cancelJobs: []hostruntime.Job{job}}
	ledger := &fakeLedger{launch: &models.AutomationLaunchEvent{
		AutomationID: automationID, OwnerIdentity: job.OwnerIdentity, RuntimeType: job.RuntimeID,
		RuntimeTaskID: job.TaskID, ExecutionReference: jobID.String(),
	}, automation: &models.Automation{ID: automationID}}
	service := NewService(host, ledger)
	if count, err := service.ReconcileCompleted(1); count != 0 || err == nil || !strings.Contains(err.Error(), "without durable stop-request evidence") {
		t.Fatalf("ReconcileCompleted = %d, %v; want evidence rejection", count, err)
	}
	if ledger.saveLaunchEventCalls != 0 || host.reconciled[jobID] {
		t.Fatalf("invalid cancellation evidence was projected: saves=%d reconciled=%#v", ledger.saveLaunchEventCalls, host.reconciled)
	}
}

func TestReconcileCompletedRejectsUnboundOwnerWithoutWritingCompletion(t *testing.T) {
	tests := []struct {
		name        string
		launchOwner string
		jobOwner    string
	}{
		{name: "mismatched owners", launchOwner: "robert@example.test", jobOwner: "other@example.test"},
		{name: "blank launch owner", launchOwner: " ", jobOwner: "robert@example.test"},
		{name: "blank job owner", launchOwner: "robert@example.test", jobOwner: "\t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			automationID := uuid.New()
			jobID := uuid.New()
			taskID := "automation:" + automationID.String() + ":intent:" + uuid.NewString()
			exitCode := 0
			host := &fakeHostJobs{jobs: []hostruntime.Job{{
				ID: jobID, OwnerIdentity: tt.jobOwner, RuntimeID: "deepseek-harness", TaskID: taskID,
				Status: hostruntime.StatusCompleted, ExitCode: &exitCode,
			}}}
			automation := &models.Automation{ID: automationID, LastFailureReason: "unchanged"}
			ledger := &fakeLedger{
				launch: &models.AutomationLaunchEvent{
					AutomationID: automationID, OwnerIdentity: tt.launchOwner,
					RuntimeType: "deepseek-harness", RuntimeTaskID: taskID, ExecutionReference: jobID.String(),
				},
				automation: automation,
			}

			service := NewService(host, ledger)
			count, err := service.ReconcileCompleted(1)
			if count != 0 || err == nil || !strings.Contains(err.Error(), "owner does not match") {
				t.Fatalf("ReconcileCompleted = %d, %v; want owner binding failure", count, err)
			}
			if len(ledger.events) != 0 || ledger.saveLaunchEventCalls != 0 {
				t.Fatalf("owner binding failure wrote completion event: events=%#v save calls=%d", ledger.events, ledger.saveLaunchEventCalls)
			}
			if ledger.updateCalls != 0 || automation.LastFailureReason != "unchanged" {
				t.Fatalf("owner binding failure updated automation: calls=%d automation=%#v", ledger.updateCalls, automation)
			}
			if host.reconciled[jobID] {
				t.Fatal("owner binding failure marked the host job reconciled")
			}
		})
	}
}

func TestReconcileCompletedKeepsUnlinkedHostJobVisible(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	jobID := uuid.New()
	exitCode := 0
	host := &fakeHostJobs{jobs: []hostruntime.Job{{ID: jobID, RuntimeID: "deepseek-harness", Status: hostruntime.StatusCompleted, ExitCode: &exitCode, CompletedAt: &now}}}
	ledger := &fakeLedger{findErr: errTest("queued launch missing")}
	service := NewService(host, ledger)
	if _, err := service.ReconcileCompleted(1); err == nil || !strings.Contains(err.Error(), "queued launch missing") {
		t.Fatalf("unlinked job error = %v", err)
	}
	if host.reconciled[jobID] {
		t.Fatal("unlinked host job was silently marked reconciled")
	}
}

func TestReconcileCompletedProjectsFailureWithoutLeakingSecrets(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	automationID := uuid.New()
	jobID := uuid.New()
	exitCode := 23
	taskID := "automation:" + automationID.String() + ":intent:" + uuid.NewString()
	host := &fakeHostJobs{jobs: []hostruntime.Job{{
		ID: jobID, OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: taskID,
		Status: hostruntime.StatusCompleted, ExitCode: &exitCode, Error: "Authorization: Bearer secret-value", CompletedAt: &now,
	}}}
	ledger := &fakeLedger{launch: &models.AutomationLaunchEvent{
		AutomationID: automationID, OwnerIdentity: host.jobs[0].OwnerIdentity,
		RuntimeType: "deepseek-harness", RuntimeTaskID: taskID, ExecutionReference: jobID.String(),
	}, automation: &models.Automation{ID: automationID}}
	service := NewService(host, ledger)
	if count, err := service.ReconcileCompleted(1); err != nil || count != 1 {
		t.Fatalf("ReconcileCompleted = %d, %v", count, err)
	}
	if ledger.automation.LastFailureAt == nil || strings.Contains(ledger.automation.LastFailureReason, "secret-value") {
		t.Fatalf("failure projection was not safely recorded: %#v", ledger.automation)
	}
	if len(ledger.events) != 1 || ledger.events[0].Status != "failed" || strings.Contains(ledger.events[0].Message, "secret-value") {
		t.Fatalf("failed event leaked or had wrong status: %#v", ledger.events)
	}
}

func TestReconcileCompletedDoesNotLetAnUnlinkedJobBlockLaterCompletion(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	automationID := uuid.New()
	missingID, linkedID := uuid.New(), uuid.New()
	exitCode := 0
	linkedTaskID := "automation:" + automationID.String() + ":intent:" + uuid.NewString()
	host := &fakeHostJobs{jobs: []hostruntime.Job{
		{ID: missingID, RuntimeID: "deepseek-harness", Status: hostruntime.StatusCompleted, ExitCode: &exitCode, CompletedAt: &now},
		{ID: linkedID, OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: linkedTaskID, Status: hostruntime.StatusCompleted, ExitCode: &exitCode, CompletedAt: &now},
	}}
	ledger := &multiLaunchLedger{launches: map[string]*models.AutomationLaunchEvent{
		linkedID.String(): {AutomationID: automationID, OwnerIdentity: host.jobs[1].OwnerIdentity, RuntimeType: "deepseek-harness", RuntimeTaskID: linkedTaskID, ExecutionReference: linkedID.String()},
	}, automation: &models.Automation{ID: automationID}}
	service := NewService(host, ledger)
	if count, err := service.ReconcileCompleted(10); count != 1 || err == nil {
		t.Fatalf("ReconcileCompleted = %d, %v; want one success plus retained error", count, err)
	}
	if host.reconciled[missingID] || !host.reconciled[linkedID] || len(ledger.events) != 1 {
		t.Fatalf("batch continuation failed: reconciled=%#v events=%#v", host.reconciled, ledger.events)
	}
}

func TestReviewProjectionPreservesUnknownOutcomeAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{hostruntime.StatusExpired, hostruntime.StatusNeedsReview} {
		t.Run(status, func(t *testing.T) {
			automationID, jobID := uuid.New(), uuid.New()
			taskID := "automation:" + automationID.String() + ":intent:" + uuid.NewString()
			reviewedAt := now.Add(-time.Minute)
			job := hostruntime.Job{
				ID: jobID, OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: taskID,
				Status: status, ReviewRequiredAt: &reviewedAt, ReviewReason: "operator review reason",
				Prompt: "PROMPT_CANARY_DO_NOT_PROJECT", LeaseDigest: "LEASE_DIGEST_CANARY_DO_NOT_PROJECT",
				Output: "OUTPUT_CANARY_DO_NOT_PROJECT", Error: "ERROR_CANARY_DO_NOT_PROJECT", CreatedAt: now.Add(-time.Hour),
			}
			host := &fakeHostJobs{reviewJobs: []hostruntime.Job{job}}
			lastSuccess, lastFailure := now.Add(-2*time.Hour), now.Add(-3*time.Hour)
			automation := &models.Automation{
				ID: automationID, LastSuccessAt: &lastSuccess, LastFailureAt: &lastFailure,
				LastFailureReason: "pre-existing failure remains unchanged",
			}
			originalLaunch := models.AutomationLaunchEvent{
				ID: uuid.New(), AutomationID: automationID, OwnerIdentity: job.OwnerIdentity,
				RuntimeType: job.RuntimeID, LaunchType: "agent_runtime", RuntimeTaskID: taskID,
				ExecutionReference: jobID.String(), Status: "queued", Message: "original launch remains immutable",
			}
			ledger := &fakeLedger{launch: &originalLaunch, automation: automation}
			service := NewService(host, ledger)
			service.now = func() time.Time { return now }

			if count, err := service.ReconcileCompleted(10); err != nil || count != 1 {
				t.Fatalf("ReconcileCompleted = %d, %v; want one review projection", count, err)
			}
			if len(ledger.events) != 1 {
				t.Fatalf("review launch history events = %#v; want one appended event", ledger.events)
			}
			event := ledger.events[0]
			if event.AutomationID != automationID || event.OwnerIdentity != job.OwnerIdentity || event.RuntimeType != job.RuntimeID ||
				event.RuntimeTaskID != job.TaskID || event.ExecutionReference != jobID.String() || event.LaunchType != "agent_runtime_host_review" ||
				event.EventKey != reviewEventPrefix+jobID.String() || event.Status != status {
				t.Fatalf("review event identity/state = %#v", event)
			}
			if event.Output != "" || event.ExitCode != -1 || event.DurationMs != 0 || !event.StartedAt.Equal(now) || !event.CompletedAt.Equal(now) {
				t.Fatalf("review event contains an inferred execution result: %#v", event)
			}
			projectionText := event.Message + " " + strings.Join(event.AuditEvents, " ") + " " + event.Output
			for _, secret := range []string{job.Prompt, job.LeaseDigest, job.Output, job.Error} {
				if strings.Contains(projectionText, secret) {
					t.Fatalf("review event exposed sensitive or execution data %q: %#v", secret, event)
				}
			}
			if !strings.Contains(event.Message, "review") || (status == hostruntime.StatusNeedsReview && !strings.Contains(event.Message, "unknown")) {
				t.Fatalf("review event does not explain its operator action: %#v", event)
			}
			if ledger.updateCalls != 0 || automation.LastSuccessAt != &lastSuccess || automation.LastFailureAt != &lastFailure || automation.LastFailureReason != "pre-existing failure remains unchanged" {
				t.Fatalf("unknown outcome changed automation state: calls=%d automation=%#v", ledger.updateCalls, automation)
			}
			if !reflect.DeepEqual(*ledger.launch, originalLaunch) {
				t.Fatalf("queued launch event was mutated: got=%#v want=%#v", *ledger.launch, originalLaunch)
			}
			if !host.reconciled[jobID] {
				t.Fatal("review event was saved but the source record was not marked projected")
			}
			stored, exists := host.reviewed[jobID]
			if !exists || stored.ID != jobID || stored.Status != status || stored.Prompt != job.Prompt {
				t.Fatalf("source host record was deleted or changed: %#v exists=%v", stored, exists)
			}
			if count, err := service.ReconcileCompleted(10); err != nil || count != 0 || len(ledger.events) != 1 {
				t.Fatalf("review retry was not idempotent: count=%d err=%v events=%#v", count, err, ledger.events)
			}
		})
	}
}

func TestReviewProjectionRequiresExactLaunchBinding(t *testing.T) {
	for _, mismatch := range []string{"owner", "task", "runtime", "execution reference"} {
		t.Run(mismatch, func(t *testing.T) {
			automationID, jobID := uuid.New(), uuid.New()
			taskID, owner, runtimeID := "task:"+uuid.NewString(), "robert@example.test", "deepseek-harness"
			reviewedAt := time.Now().UTC()
			job := hostruntime.Job{
				ID: jobID, OwnerIdentity: owner, RuntimeID: runtimeID, TaskID: taskID,
				Status: hostruntime.StatusNeedsReview, ReviewRequiredAt: &reviewedAt,
			}
			launch := &models.AutomationLaunchEvent{
				AutomationID: automationID, OwnerIdentity: owner, RuntimeType: runtimeID,
				RuntimeTaskID: taskID, ExecutionReference: jobID.String(),
			}
			switch mismatch {
			case "owner":
				launch.OwnerIdentity = "another-owner"
			case "task":
				launch.RuntimeTaskID = "another-task"
			case "runtime":
				launch.RuntimeType = "another-runtime"
			case "execution reference":
				launch.ExecutionReference = uuid.NewString()
			}
			host := &fakeHostJobs{reviewJobs: []hostruntime.Job{job}}
			ledger := &fakeLedger{launch: launch, automation: &models.Automation{ID: automationID}}
			service := NewService(host, ledger)
			if count, err := service.ReconcileCompleted(1); count != 0 || err == nil {
				t.Fatalf("ReconcileCompleted = %d, %v; want binding rejection", count, err)
			}
			if ledger.saveLaunchEventCalls != 0 || ledger.updateCalls != 0 || host.reconciled[jobID] {
				t.Fatalf("mismatched host job caused effects: saved=%d updates=%d reconciled=%#v", ledger.saveLaunchEventCalls, ledger.updateCalls, host.reconciled)
			}
		})
	}
}

type fakeHostJobs struct {
	jobs       []hostruntime.Job
	cancelJobs []hostruntime.Job
	reviewJobs []hostruntime.Job
	reviewed   map[uuid.UUID]hostruntime.Job
	reconciled map[uuid.UUID]bool
}

func (f *fakeHostJobs) CancelledUnreconciled(limit int) ([]hostruntime.Job, error) {
	if f.reconciled == nil {
		f.reconciled = map[uuid.UUID]bool{}
	}
	jobs := make([]hostruntime.Job, 0)
	for _, job := range f.cancelJobs {
		if !f.reconciled[job.ID] {
			jobs = append(jobs, job)
		}
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}
	return jobs, nil
}

func (f *fakeHostJobs) CompletedUnreconciled(limit int) ([]hostruntime.Job, error) {
	if f.reconciled == nil {
		f.reconciled = map[uuid.UUID]bool{}
	}
	jobs := make([]hostruntime.Job, 0)
	for _, job := range f.jobs {
		if !f.reconciled[job.ID] {
			jobs = append(jobs, job)
		}
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}
	return jobs, nil
}

func (f *fakeHostJobs) ReviewRequiredUnreconciled(limit int) ([]hostruntime.Job, error) {
	if f.reconciled == nil {
		f.reconciled = map[uuid.UUID]bool{}
	}
	if f.reviewed == nil {
		f.reviewed = map[uuid.UUID]hostruntime.Job{}
	}
	jobs := make([]hostruntime.Job, 0)
	for _, job := range f.reviewJobs {
		if !f.reconciled[job.ID] {
			jobs = append(jobs, job)
		}
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}
	return jobs, nil
}

func (f *fakeHostJobs) MarkReconciled(id uuid.UUID) (bool, error) {
	if f.reconciled == nil {
		f.reconciled = map[uuid.UUID]bool{}
	}
	if f.reconciled[id] {
		return false, nil
	}
	f.reconciled[id] = true
	for _, job := range append(append(append([]hostruntime.Job{}, f.jobs...), f.cancelJobs...), f.reviewJobs...) {
		if job.ID == id {
			if f.reviewed == nil {
				f.reviewed = map[uuid.UUID]hostruntime.Job{}
			}
			f.reviewed[id] = job
			break
		}
	}
	return true, nil
}

type fakeLedger struct {
	launch               *models.AutomationLaunchEvent
	automation           *models.Automation
	events               []models.AutomationLaunchEvent
	findErr              error
	updateCalls          int
	saveLaunchEventCalls int
}

func (f *fakeLedger) FindLaunchEventByExecutionReference(string) (*models.AutomationLaunchEvent, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.launch, nil
}

func (f *fakeLedger) FindByID(uuid.UUID) (*models.Automation, error) { return f.automation, nil }
func (f *fakeLedger) Update(automation *models.Automation) (*models.Automation, error) {
	f.updateCalls++
	f.automation = automation
	return automation, nil
}
func (f *fakeLedger) SaveLaunchEvent(event *models.AutomationLaunchEvent) error {
	f.saveLaunchEventCalls++
	for _, existing := range f.events {
		if existing.EventKey == event.EventKey {
			return nil
		}
	}
	f.events = append(f.events, *event)
	return nil
}

type multiLaunchLedger struct {
	launches   map[string]*models.AutomationLaunchEvent
	automation *models.Automation
	events     []models.AutomationLaunchEvent
}

func (f *multiLaunchLedger) FindLaunchEventByExecutionReference(reference string) (*models.AutomationLaunchEvent, error) {
	launch, ok := f.launches[reference]
	if !ok {
		return nil, errTest("queued launch missing")
	}
	return launch, nil
}
func (f *multiLaunchLedger) FindByID(uuid.UUID) (*models.Automation, error) { return f.automation, nil }
func (f *multiLaunchLedger) Update(automation *models.Automation) (*models.Automation, error) {
	f.automation = automation
	return automation, nil
}
func (f *multiLaunchLedger) SaveLaunchEvent(event *models.AutomationLaunchEvent) error {
	f.events = append(f.events, *event)
	return nil
}

type errTest string

func (e errTest) Error() string { return string(e) }
