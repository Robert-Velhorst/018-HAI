// Package hostruntimereconcile projects completed Windows host-runtime work
// and unresolved review states into HAI's immutable automation audit ledger.
// It never executes host work.
package hostruntimereconcile

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/hostruntime"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	completionEventPrefix            = "host-runtime-completion:"
	cancellationEventPrefix          = "host-runtime-cancellation:"
	reviewEventPrefix                = "host-runtime-review:"
	processSucceededUnverifiedStatus = "process_succeeded_unverified"
)

type HostJobs interface {
	CompletedUnreconciled(limit int) ([]hostruntime.Job, error)
	CancelledUnreconciled(limit int) ([]hostruntime.Job, error)
	ReviewRequiredUnreconciled(limit int) ([]hostruntime.Job, error)
	MarkReconciled(id uuid.UUID) (bool, error)
}

type AutomationLedger interface {
	FindLaunchEventByExecutionReference(reference string) (*models.AutomationLaunchEvent, error)
	FindByID(id uuid.UUID) (*models.Automation, error)
	Update(automation *models.Automation) (*models.Automation, error)
	SaveLaunchEvent(event *models.AutomationLaunchEvent) error
}

type Service struct {
	hostJobs HostJobs
	ledger   AutomationLedger
	now      func() time.Time
}

func NewService(hostJobs HostJobs, ledger AutomationLedger) *Service {
	return &Service{hostJobs: hostJobs, ledger: ledger, now: func() time.Time { return time.Now().UTC() }}
}

// ReconcileCompleted projects terminal results and review-required host states
// into the immutable launch history. Unknown jobs are retained and retried;
// review-required jobs never update automation outcome or trigger execution.
func (s *Service) ReconcileCompleted(limit int) (int, error) {
	if s == nil || s.hostJobs == nil || s.ledger == nil {
		return 0, fmt.Errorf("host runtime reconciliation dependencies are unavailable")
	}
	projected := 0
	var failures []error
	completedJobs, completedErr := s.hostJobs.CompletedUnreconciled(limit)
	if completedErr != nil {
		failures = append(failures, fmt.Errorf("list unreconciled host runtime completions: %w", completedErr))
	} else {
		for _, job := range completedJobs {
			if err := s.reconcile(job); err != nil {
				failures = append(failures, err)
				continue
			}
			projected++
		}
	}
	cancelledJobs, cancelledErr := s.hostJobs.CancelledUnreconciled(limit)
	if cancelledErr != nil {
		failures = append(failures, fmt.Errorf("list unreconciled host runtime cancellations: %w", cancelledErr))
	} else {
		for _, job := range cancelledJobs {
			if err := s.projectCancellation(job); err != nil {
				failures = append(failures, err)
				continue
			}
			projected++
		}
	}
	reviewJobs, reviewErr := s.hostJobs.ReviewRequiredUnreconciled(limit)
	if reviewErr != nil {
		failures = append(failures, fmt.Errorf("list host runtime jobs requiring review: %w", reviewErr))
	} else {
		for _, job := range reviewJobs {
			if err := s.projectReview(job); err != nil {
				failures = append(failures, err)
				continue
			}
			projected++
		}
	}
	return projected, errors.Join(failures...)
}

func (s *Service) projectCancellation(job hostruntime.Job) error {
	if job.Status != hostruntime.StatusCancelled {
		return fmt.Errorf("host job %s is not cancelled", job.ID)
	}
	if job.ExecutionConfirmedAt != nil && job.CancelRequestedAt == nil {
		return fmt.Errorf("host job %s was cancelled after execution confirmation without durable stop-request evidence", job.ID)
	}
	automation, jobOwner, err := s.bindLaunch(job)
	if err != nil {
		return err
	}
	recordedAt := s.now().UTC()
	message := "The approved host job was cancelled before execution confirmation; the bridge was not authorized to resume a DSH process."
	if job.ExecutionConfirmedAt != nil {
		message = "The bridge acknowledged Stop after confirming the Windows Job Object process tree was empty. Task effects were not rolled back; inspect retained output before treating work as undone."
	}
	if job.CompletedAt != nil {
		recordedAt = job.CompletedAt.UTC()
	}
	event := &models.AutomationLaunchEvent{
		ID:                 uuid.New(),
		AutomationID:       automation.ID,
		OwnerIdentity:      jobOwner,
		RuntimeType:        job.RuntimeID,
		LaunchType:         "agent_runtime_host_cancelled",
		RuntimeTaskID:      job.TaskID,
		ExecutionReference: job.ID.String(),
		EventKey:           cancellationEventPrefix + job.ID.String(),
		Target:             "host-runtime://" + job.RuntimeID,
		Status:             hostruntime.StatusCancelled,
		Message:            message,
		AuditEvents: []string{
			"host runtime cancellation reconciled without inferring task success or rollback",
			"host runtime job " + job.ID.String(),
		},
		ExitCode:    -1,
		StartedAt:   job.CreatedAt.UTC(),
		CompletedAt: recordedAt,
	}
	if err := s.ledger.SaveLaunchEvent(event); err != nil {
		return fmt.Errorf("persist host cancellation event for job %s: %w", job.ID, err)
	}
	if _, err := s.hostJobs.MarkReconciled(job.ID); err != nil {
		return fmt.Errorf("mark host cancellation event for job %s reconciled: %w", job.ID, err)
	}
	return nil
}

func (s *Service) reconcile(job hostruntime.Job) error {
	automation, jobOwner, err := s.bindLaunch(job)
	if err != nil {
		return err
	}
	status := processSucceededUnverifiedStatus
	message := "Windows host process exited with code 0; process success is recorded, but task outcome remains unverified."
	exitCode := 0
	if job.ExitCode != nil {
		exitCode = *job.ExitCode
	}
	if exitCode != 0 || strings.TrimSpace(job.Error) != "" {
		status = "failed"
		message = firstNonEmpty(job.Error, "Windows host runtime failed the approved task")
	}
	finished := s.now().UTC()
	if job.CompletedAt != nil {
		finished = job.CompletedAt.UTC()
	}
	event := &models.AutomationLaunchEvent{
		ID:                 uuid.New(),
		AutomationID:       automation.ID,
		OwnerIdentity:      jobOwner,
		RuntimeType:        job.RuntimeID,
		LaunchType:         "agent_runtime_host_completion",
		RuntimeTaskID:      job.TaskID,
		ExecutionReference: job.ID.String(),
		EventKey:           completionEventPrefix + job.ID.String(),
		Target:             "host-runtime://" + job.RuntimeID,
		Status:             status,
		Message:            safety.RedactSecrets(message),
		Output:             safety.RedactSecrets(job.Output),
		AuditEvents: []string{
			"Windows host-runtime process result reconciled into the immutable automation ledger",
			"host runtime job " + job.ID.String(),
			"process exit status alone does not verify task completion",
		},
		ExitCode:    exitCode,
		DurationMs:  durationSince(job.CreatedAt, finished),
		StartedAt:   job.CreatedAt.UTC(),
		CompletedAt: finished,
	}
	if err := s.ledger.SaveLaunchEvent(event); err != nil {
		return fmt.Errorf("persist host completion event for job %s: %w", job.ID, err)
	}
	if status == "failed" {
		automation.LastFailureAt = &finished
		automation.LastFailureReason = safety.RedactSecrets(message)
		if _, err := s.ledger.Update(automation); err != nil {
			return fmt.Errorf("update automation terminal state for host job %s: %w", job.ID, err)
		}
	}
	if _, err := s.hostJobs.MarkReconciled(job.ID); err != nil {
		return fmt.Errorf("mark host job %s reconciled: %w", job.ID, err)
	}
	return nil
}

func (s *Service) projectReview(job hostruntime.Job) error {
	if (job.Status != hostruntime.StatusExpired && job.Status != hostruntime.StatusNeedsReview) || job.ReviewRequiredAt == nil {
		return fmt.Errorf("host job %s is not in a review-required state", job.ID)
	}
	automation, jobOwner, err := s.bindLaunch(job)
	if err != nil {
		return err
	}
	recordedAt := s.now().UTC()
	message := "Approved host work expired before execution. No success or failure was inferred; review before deciding what happens next."
	if job.Status == hostruntime.StatusNeedsReview {
		message = "The host execution lease expired and the outcome is unknown. No success or failure was inferred; review before any retry."
	}
	event := &models.AutomationLaunchEvent{
		ID:                 uuid.New(),
		AutomationID:       automation.ID,
		OwnerIdentity:      jobOwner,
		RuntimeType:        job.RuntimeID,
		LaunchType:         "agent_runtime_host_review",
		RuntimeTaskID:      job.TaskID,
		ExecutionReference: job.ID.String(),
		EventKey:           reviewEventPrefix + job.ID.String(),
		Target:             "host-runtime://" + job.RuntimeID,
		Status:             job.Status,
		Message:            message,
		AuditEvents: []string{
			"host runtime result requires operator review; execution outcome is not asserted",
			"no host task retry or completion was initiated by reconciliation",
			"host runtime job " + job.ID.String(),
		},
		ExitCode:    -1,
		StartedAt:   recordedAt,
		CompletedAt: recordedAt,
	}
	if err := s.ledger.SaveLaunchEvent(event); err != nil {
		return fmt.Errorf("persist host review event for job %s: %w", job.ID, err)
	}
	if _, err := s.hostJobs.MarkReconciled(job.ID); err != nil {
		return fmt.Errorf("mark host review event for job %s reconciled: %w", job.ID, err)
	}
	return nil
}

func (s *Service) bindLaunch(job hostruntime.Job) (*models.Automation, string, error) {
	launch, err := s.ledger.FindLaunchEventByExecutionReference(job.ID.String())
	if err != nil {
		return nil, "", fmt.Errorf("find queued automation launch for host job %s: %w", job.ID, err)
	}
	if launch == nil || launch.ExecutionReference != job.ID.String() || launch.AutomationID == uuid.Nil || launch.RuntimeTaskID != job.TaskID || launch.RuntimeType != job.RuntimeID {
		return nil, "", fmt.Errorf("host job %s does not match its queued automation launch", job.ID)
	}
	launchOwner := strings.TrimSpace(launch.OwnerIdentity)
	jobOwner := strings.TrimSpace(job.OwnerIdentity)
	if launchOwner == "" || jobOwner == "" || launchOwner != jobOwner {
		return nil, "", fmt.Errorf("host job %s owner does not match its queued automation launch", job.ID)
	}
	automation, err := s.ledger.FindByID(launch.AutomationID)
	if err != nil {
		return nil, "", fmt.Errorf("find automation for host job %s: %w", job.ID, err)
	}
	if automation == nil || automation.ID != launch.AutomationID {
		return nil, "", fmt.Errorf("host job %s automation does not match its queued launch", job.ID)
	}
	return automation, jobOwner, nil
}

func durationSince(start, end time.Time) int64 {
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
