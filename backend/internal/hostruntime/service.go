// Package hostruntime persists work that must be executed by a narrowly
// configured runtime on the local Windows host rather than by a container.
package hostruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	StatusPending     = "pending"
	StatusLeased      = "leased"
	StatusCompleted   = "completed"
	StatusCancelled   = "cancelled"
	StatusExpired     = "expired"
	StatusNeedsReview = "needs_review"

	maxPromptBytes         = 50 * 1024
	maxResultBytes         = 16 * 1024
	approvalValidityWindow = 24 * time.Hour

	defaultLeaseSeconds    = 20 * 60
	minimumLeaseSeconds    = 60
	maximumLeaseSeconds    = 30 * 60
	defaultHarnessTimeout  = 120
	maximumHarnessTimeout  = 15 * 60
	completionGraceSeconds = 60
	AdmissionTimeout       = 5 * time.Second
	startIntentTimeout     = 30 * time.Second

	reviewReasonApprovalExpired  = "approved host task expired before execution"
	reviewReasonLeaseExpired     = "host execution outcome is unknown; operator review required"
	reviewReasonCancelUnverified = "host cancellation requested but process-tree termination could not be verified; operator review required"
	reviewReasonStopRevision     = "emergency stop changed after approval; explicit re-approval is required before execution"
	reviewReasonStartUnknown     = "host process start intent has no trustworthy acknowledgment; operator review required"
	reviewReasonStartProtocol    = "host process start protocol binding or replay was inconsistent; operator review required"
)

var (
	ErrInvalidTask                   = errors.New("host runtime task is invalid")
	ErrStaleLease                    = errors.New("host runtime lease is no longer valid")
	ErrEmergencyStopped              = errors.New("host runtime lease blocked by emergency stop")
	ErrCancellationRequested         = errors.New("host runtime cancellation was requested")
	ErrJobNotFound                   = errors.New("owner-bound host runtime job was not found")
	ErrExecutionIsolationUnavailable = errors.New("host runtime execution remains blocked because no verified operating-system sandbox or atomic server-to-Windows ordering of Resume against emergency stop is available")
	ErrStopRevisionUnavailable       = errors.New("host runtime execution is blocked because the persisted emergency-stop revision is unavailable")
	ErrStartIntentReplay             = errors.New("host runtime start intent was already consumed by a different intent")
	ErrStartOutcomeReview            = errors.New("host runtime start outcome is uncertain and requires operator review")
)

type ApprovedTask struct {
	OwnerIdentity string
	RuntimeID     string
	TaskID        string
	Prompt        string
	WorkspaceKey  string
	Approved      bool
}

type Completion struct {
	ExitCode              int
	Output                string
	Error                 string
	CancellationRequested bool
	TerminationVerified   bool
}

// Dispatcher is the narrow capability agent-runtime adapters need in order to
// request host execution. It deliberately exposes no lease or completion
// operations, which remain private to the loopback Windows bridge.
type Dispatcher interface {
	EnqueueContext(context.Context, ApprovedTask) (*Job, error)
	CancelTask(context.Context, string, string, uuid.UUID) (*Job, bool, error)
}

type Job struct {
	ID                        uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	OwnerIdentity             string     `gorm:"type:text;not null;index" json:"ownerIdentity"`
	RuntimeID                 string     `gorm:"type:text;not null;index" json:"runtimeId"`
	TaskID                    string     `gorm:"type:text;not null;uniqueIndex" json:"taskId"`
	Prompt                    string     `gorm:"type:text;not null" json:"prompt"`
	WorkspaceKey              string     `gorm:"type:text;not null" json:"workspaceKey"`
	ApprovalDigest            string     `gorm:"type:text;not null;default:''" json:"-"`
	StopRevision              uint64     `gorm:"type:bigint;not null;default:0" json:"-"`
	Status                    string     `gorm:"type:text;not null;index" json:"status"`
	ApprovalExpiresAt         time.Time  `gorm:"type:timestamptz;not null" json:"approvalExpiresAt"`
	ReviewRequiredAt          *time.Time `gorm:"type:timestamptz" json:"reviewRequiredAt,omitempty"`
	ReviewReason              string     `gorm:"type:text;not null;default:''" json:"reviewReason,omitempty"`
	WorkerID                  string     `gorm:"type:text" json:"workerId,omitempty"`
	LeaseDigest               string     `gorm:"type:text" json:"-"`
	LeaseExpires              *time.Time `json:"leaseExpiresAt,omitempty"`
	ExecutionConfirmedAt      *time.Time `gorm:"type:timestamptz" json:"-"`
	StartIntentID             *uuid.UUID `gorm:"type:uuid" json:"-"`
	StartIntentAt             *time.Time `gorm:"type:timestamptz" json:"-"`
	StartIntentWorkerID       string     `gorm:"type:text;not null;default:''" json:"-"`
	StartIntentLeaseDigest    string     `gorm:"type:text;not null;default:''" json:"-"`
	StartIntentApprovalDigest string     `gorm:"type:text;not null;default:''" json:"-"`
	StartIntentStopRevision   *uint64    `gorm:"type:bigint" json:"-"`
	ActualStartAckAt          *time.Time `gorm:"type:timestamptz" json:"-"`
	CancelRequestedAt         *time.Time `gorm:"type:timestamptz" json:"cancelRequestedAt,omitempty"`
	Output                    string     `gorm:"type:text" json:"output,omitempty"`
	Error                     string     `gorm:"type:text" json:"error,omitempty"`
	ExitCode                  *int       `json:"exitCode,omitempty"`
	CreatedAt                 time.Time  `json:"createdAt"`
	UpdatedAt                 time.Time  `json:"updatedAt"`
	CompletedAt               *time.Time `json:"completedAt,omitempty"`
	ReconciledAt              *time.Time `json:"reconciledAt,omitempty"`
}

func (Job) TableName() string { return "host_runtime_jobs" }

type Lease struct {
	Job            Job    `json:"job"`
	Token          string `json:"leaseToken"`
	WorkerID       string `json:"workerId"`
	ApprovalDigest string `json:"approvalDigest"`
	StopRevision   uint64 `json:"stopRevision"`
}

// StartIntentBinding is the immutable identity presented by the bridge when it
// asks to start a job and again when it reports that Resume returned. The
// lease token is never persisted; only its digest is bound to the intent.
type StartIntentBinding struct {
	IntentID       uuid.UUID
	ApprovalDigest string
	StopRevision   uint64
}

// StartGate is read while holding the emergency-stop commit fence. An
// unavailable stop revision is not equivalent to a clear stop.
type StartGate struct {
	Available       bool
	Active          bool
	CurrentRevision uint64
}

// StartIntent is a server-stamped receipt. ActualStartAckAt records the
// authenticated bridge's report, not independent operating-system proof.
type StartIntent struct {
	JobID          uuid.UUID `json:"jobId"`
	IntentID       uuid.UUID `json:"intentId"`
	WorkerID       string    `json:"workerId"`
	ApprovalDigest string    `json:"approvalDigest"`
	StopRevision   uint64    `json:"stopRevision"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Repository interface {
	Create(context.Context, Job) (*Job, error)
	Lease(workerID, runtimeID string, now, expires time.Time, digest string, stopRevision uint64) (*Job, error)
	ConfirmLeaseContext(context.Context, string, uuid.UUID, string, time.Time, uint64) error
	BeginStartIntent(context.Context, string, uuid.UUID, string, StartIntentBinding, StartGate, time.Time) (*StartIntent, error)
	AcknowledgeActualStart(context.Context, string, uuid.UUID, string, StartIntentBinding, StartGate, time.Time) error
	ReportStartOutcomeUnknown(context.Context, string, uuid.UUID, string, StartIntentBinding, time.Time) (bool, error)
	CancelTask(context.Context, string, string, uuid.UUID, time.Time) (*Job, bool, error)
	Complete(workerID string, id uuid.UUID, digest string, completion Completion, now time.Time) (*Job, error)
	ListCompletedUnreconciled(limit int) ([]Job, error)
	ListCancelledUnreconciled(limit int) ([]Job, error)
	ListReviewRequiredUnreconciled(limit int) ([]Job, error)
	MarkReconciled(id uuid.UUID, at time.Time) (bool, error)
}

type Service struct {
	repository                  Repository
	now                         func() time.Time
	leaseFor                    time.Duration
	executionIsolationAvailable bool
}

type Option func(*Service)

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

// WithLeaseDuration is primarily useful for tests and explicitly bounded
// deployments. Production construction uses configuredLeaseDuration so a
// Windows worker cannot outlive its own lease.
func WithLeaseDuration(duration time.Duration) Option {
	return func(service *Service) {
		if duration >= time.Duration(minimumLeaseSeconds)*time.Second &&
			duration <= time.Duration(maximumLeaseSeconds)*time.Second {
			service.leaseFor = duration
		}
	}
}

func NewService(repository Repository, options ...Option) *Service {
	service := &Service{repository: repository, now: func() time.Time { return time.Now().UTC() }, leaseFor: configuredLeaseDuration()}
	for _, option := range options {
		option(service)
	}
	return service
}

// configuredLeaseDuration reserves enough time for the configured Windows DSH
// process plus submission of its terminal result. A too-short operator value
// is raised rather than allowing an approved task to be leased a second time
// while its first execution is still running.
func configuredLeaseDuration() time.Duration {
	harnessTimeout := boundedEnvSeconds("DEEPSEEK_HARNESS_TIMEOUT_SECONDS", defaultHarnessTimeout, 1, maximumHarnessTimeout)
	configured := boundedEnvSeconds("HAI_HOST_RUNTIME_LEASE_SECONDS", defaultLeaseSeconds, minimumLeaseSeconds, maximumLeaseSeconds)
	minimum := harnessTimeout + completionGraceSeconds
	if configured < minimum {
		configured = minimum
	}
	return time.Duration(configured) * time.Second
}

func boundedEnvSeconds(name string, fallback, minimum, maximum int) int {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || seconds < minimum || seconds > maximum {
		return fallback
	}
	return seconds
}

func (s *Service) Enqueue(task ApprovedTask) (*Job, error) {
	ctx, cancel := context.WithTimeout(context.Background(), AdmissionTimeout)
	defer cancel()
	releaseFence, err := safety.AcquireExecutionCommitFenceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseFence()
	return s.EnqueueContext(ctx, task)
}

// EnqueueContext writes an approved host job using a bounded caller context.
// Agent-runtime callers hold the execution admission fence around this
// operation so a stop cannot be committed before the durable admission.
func (s *Service) EnqueueContext(ctx context.Context, task ApprovedTask) (*Job, error) {
	if s == nil || s.repository == nil || !task.Approved || !validTask(task) {
		return nil, ErrInvalidTask
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, AdmissionTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.executionIsolationAvailable {
		return nil, ErrExecutionIsolationUnavailable
	}
	if safety.EvaluateEmergencyStopForExecution().Active {
		return nil, ErrEmergencyStopped
	}
	stopRevision, err := safety.CurrentEmergencyStopRevision()
	if err != nil {
		return nil, ErrStopRevisionUnavailable
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	job := Job{
		ID:                uuid.New(),
		OwnerIdentity:     strings.TrimSpace(task.OwnerIdentity),
		RuntimeID:         strings.ToLower(strings.TrimSpace(task.RuntimeID)),
		TaskID:            strings.TrimSpace(task.TaskID),
		Prompt:            strings.TrimSpace(task.Prompt),
		WorkspaceKey:      strings.TrimSpace(task.WorkspaceKey),
		StopRevision:      stopRevision,
		Status:            StatusPending,
		ApprovalExpiresAt: now.Add(approvalValidityWindow),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	job.ApprovalDigest = approvalDigest(job)
	created, err := s.repository.Create(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("enqueue host runtime task: %w", err)
	}
	return created, nil
}

func (s *Service) Lease(workerID, runtimeID string) (*Lease, error) {
	if s == nil || s.repository == nil || !validIdentifier(workerID) || !validIdentifier(runtimeID) {
		return nil, ErrInvalidTask
	}
	if !s.executionIsolationAvailable {
		return nil, nil
	}
	// A durable job can wait in the queue while the operator activates the
	// emergency stop. Re-check at the lease boundary so already-queued work is
	// not converted into a new Windows process after the stop takes effect.
	if safety.EvaluateEmergencyStopForExecution().Active {
		return nil, ErrEmergencyStopped
	}
	stopRevision, err := safety.CurrentEmergencyStopRevision()
	if err != nil {
		return nil, ErrStopRevisionUnavailable
	}
	token, digest, err := randomLeaseToken()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	job, err := s.repository.Lease(strings.TrimSpace(workerID), strings.ToLower(strings.TrimSpace(runtimeID)), now, now.Add(s.leaseFor), digest, stopRevision)
	if err != nil {
		return nil, fmt.Errorf("lease host runtime task: %w", err)
	}
	if job == nil {
		return nil, nil
	}
	return &Lease{Job: *job, Token: token, WorkerID: strings.TrimSpace(workerID), ApprovalDigest: job.ApprovalDigest, StopRevision: job.StopRevision}, nil
}

func (s *Service) Complete(workerID string, id uuid.UUID, token string, completion Completion) (*Job, error) {
	if s == nil || s.repository == nil || id == uuid.Nil || !validIdentifier(workerID) || strings.TrimSpace(token) == "" {
		return nil, ErrInvalidTask
	}
	job, err := s.repository.Complete(strings.TrimSpace(workerID), id, digestToken(token), sanitizeCompletion(completion), s.now().UTC())
	if errors.Is(err, ErrStaleLease) {
		return nil, ErrStaleLease
	}
	if errors.Is(err, ErrCancellationRequested) {
		return nil, ErrCancellationRequested
	}
	if err != nil {
		return nil, fmt.Errorf("complete host runtime task: %w", err)
	}
	return job, nil
}

// ConfirmLease is the bounded final server-side gate before the bridge may
// consider starting an external process. It does not make the later remote
// Windows Resume call atomic with the persisted emergency stop; production
// execution remains disabled until that distributed start protocol exists.
func (s *Service) ConfirmLease(workerID string, id uuid.UUID, token string) error {
	if s == nil || s.repository == nil || id == uuid.Nil || !validIdentifier(workerID) || strings.TrimSpace(token) == "" {
		return ErrInvalidTask
	}
	if !s.executionIsolationAvailable {
		return ErrExecutionIsolationUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), AdmissionTimeout)
	defer cancel()
	releaseFence, err := safety.AcquireExecutionCommitFenceContext(ctx)
	if err != nil {
		return err
	}
	defer releaseFence()
	if safety.EvaluateEmergencyStopForExecution().Active {
		return ErrEmergencyStopped
	}
	stopRevision, err := safety.CurrentEmergencyStopRevision()
	if err != nil {
		return ErrStopRevisionUnavailable
	}
	if err := s.repository.ConfirmLeaseContext(ctx, strings.TrimSpace(workerID), id, digestToken(token), s.now().UTC(), stopRevision); err != nil {
		if errors.Is(err, ErrStaleLease) {
			return ErrStaleLease
		}
		if errors.Is(err, ErrCancellationRequested) {
			return ErrCancellationRequested
		}
		return fmt.Errorf("confirm host runtime lease: %w", err)
	}
	return nil
}

// BeginStartIntent durably consumes a one-shot start identity for the exact
// leased approval. It is a database fence, not an atomic lock over the later
// Windows Resume call; a stop after this commit remains an ambiguous outcome.
func (s *Service) BeginStartIntent(ctx context.Context, workerID string, jobID uuid.UUID, token string, binding StartIntentBinding) (*StartIntent, error) {
	if s == nil || s.repository == nil || jobID == uuid.Nil || binding.IntentID == uuid.Nil ||
		!validIdentifier(workerID) || strings.TrimSpace(token) == "" || !validDigest(binding.ApprovalDigest) || binding.StopRevision > math.MaxInt64 {
		return nil, ErrInvalidTask
	}
	if !s.executionIsolationAvailable {
		return nil, ErrExecutionIsolationUnavailable
	}
	ctx, cancel := boundedProtocolContext(ctx)
	defer cancel()
	releaseFence, err := safety.AcquireExecutionCommitFenceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseFence()
	gate := currentStartGate()
	intent, err := s.repository.BeginStartIntent(ctx, strings.TrimSpace(workerID), jobID, digestToken(token), binding, gate, s.now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return nil, err
	}
	return intent, nil
}

// AcknowledgeActualStart persists the bridge's report that the exact process
// start intent reached Resume. The acknowledgment is an authenticated worker
// claim, not independent proof from Windows.
func (s *Service) AcknowledgeActualStart(ctx context.Context, workerID string, jobID uuid.UUID, token string, binding StartIntentBinding) error {
	if s == nil || s.repository == nil || jobID == uuid.Nil || binding.IntentID == uuid.Nil ||
		!validIdentifier(workerID) || strings.TrimSpace(token) == "" || !validDigest(binding.ApprovalDigest) || binding.StopRevision > math.MaxInt64 {
		return ErrInvalidTask
	}
	if !s.executionIsolationAvailable {
		return ErrExecutionIsolationUnavailable
	}
	ctx, cancel := boundedProtocolContext(ctx)
	defer cancel()
	releaseFence, err := safety.AcquireExecutionCommitFenceContext(ctx)
	if err != nil {
		return err
	}
	defer releaseFence()
	gate := currentStartGate()
	return s.repository.AcknowledgeActualStart(ctx, strings.TrimSpace(workerID), jobID, digestToken(token), binding, gate, s.now().UTC().Truncate(time.Microsecond))
}

// ReportStartOutcomeUnknown makes an unresolved process-start report
// non-retryable. The bool reports that the same acknowledgment was already
// persisted, which is safe for an idempotent network replay.
func (s *Service) ReportStartOutcomeUnknown(ctx context.Context, workerID string, jobID uuid.UUID, token string, binding StartIntentBinding) (bool, error) {
	if s == nil || s.repository == nil || jobID == uuid.Nil || binding.IntentID == uuid.Nil ||
		!validIdentifier(workerID) || strings.TrimSpace(token) == "" || !validDigest(binding.ApprovalDigest) || binding.StopRevision > math.MaxInt64 {
		return false, ErrInvalidTask
	}
	ctx, cancel := boundedProtocolContext(ctx)
	defer cancel()
	alreadyAcknowledged, err := s.repository.ReportStartOutcomeUnknown(ctx, strings.TrimSpace(workerID), jobID, digestToken(token), binding, s.now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return false, err
	}
	if !alreadyAcknowledged {
		return false, ErrStartOutcomeReview
	}
	return true, nil
}

func boundedProtocolContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, AdmissionTimeout)
}

func currentStartGate() StartGate {
	revision, err := safety.CurrentEmergencyStopRevision()
	if err != nil {
		return StartGate{}
	}
	return StartGate{Available: true, Active: safety.EvaluateEmergencyStopForExecution().Active, CurrentRevision: revision}
}

// ExecutionAvailability lets runtime adapters report the same hard gate used
// by enqueue, lease, and confirmation boundaries.
func (s *Service) ExecutionAvailability() (bool, string) {
	if s == nil || !s.executionIsolationAvailable {
		return false, ErrExecutionIsolationUnavailable.Error()
	}
	return true, ""
}

// CancelTask revokes queued or unconfirmed work immediately. For a confirmed
// execution it persists a stop request, keeps the lease valid for bridge
// heartbeats, and reports terminal cancellation only after verified exit.
func (s *Service) CancelTask(ctx context.Context, ownerIdentity, taskID string, jobID uuid.UUID) (*Job, bool, error) {
	if s == nil || s.repository == nil || !validIdentifier(ownerIdentity) || !validIdentifier(taskID) {
		return nil, false, ErrInvalidTask
	}
	if ctx == nil {
		return nil, false, ErrInvalidTask
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, AdmissionTimeout)
	defer cancel()
	job, revoked, err := s.repository.CancelTask(requestCtx, strings.TrimSpace(ownerIdentity), strings.TrimSpace(taskID), jobID, s.now().UTC())
	if requestCtx.Err() != nil {
		return job, false, errors.Join(err, requestCtx.Err())
	}
	return job, revoked, err
}

// CompletedUnreconciled exposes terminal host work to the audit reconciler.
// It cannot lease, complete, or execute work.
func (s *Service) CompletedUnreconciled(limit int) ([]Job, error) {
	if s == nil || s.repository == nil {
		return nil, ErrInvalidTask
	}
	return s.repository.ListCompletedUnreconciled(limit)
}

// CancelledUnreconciled exposes cancellation outcomes only after the repository
// has recorded either pre-execution revocation or verified process-tree exit.
func (s *Service) CancelledUnreconciled(limit int) ([]Job, error) {
	if s == nil || s.repository == nil {
		return nil, ErrInvalidTask
	}
	return s.repository.ListCancelledUnreconciled(limit)
}

// ReviewRequiredUnreconciled exposes expired approvals and uncertain host
// execution outcomes to the audit reconciler. It cannot lease, complete, or
// execute work.
func (s *Service) ReviewRequiredUnreconciled(limit int) ([]Job, error) {
	if s == nil || s.repository == nil {
		return nil, ErrInvalidTask
	}
	return s.repository.ListReviewRequiredUnreconciled(limit)
}

// MarkReconciled records that a host completion or review notice was projected
// into HAI's immutable automation audit ledger. It is safe for scheduler retries.
func (s *Service) MarkReconciled(id uuid.UUID) (bool, error) {
	if s == nil || s.repository == nil || id == uuid.Nil {
		return false, ErrInvalidTask
	}
	return s.repository.MarkReconciled(id, s.now().UTC())
}

func validTask(task ApprovedTask) bool {
	return validIdentifier(task.OwnerIdentity) && validIdentifier(task.RuntimeID) && validIdentifier(task.TaskID) &&
		validIdentifier(task.WorkspaceKey) && strings.TrimSpace(task.Prompt) != "" && len(task.Prompt) <= maxPromptBytes
}

func validIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 255 && !strings.ContainsAny(value, "\r\n\x00")
}

func randomLeaseToken() (string, string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate host runtime lease token: %w", err)
	}
	token := hex.EncodeToString(bytes)
	return token, digestToken(token), nil
}

func digestToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func approvalDigest(job Job) string {
	immutable := struct {
		ID                string `json:"id"`
		OwnerIdentity     string `json:"ownerIdentity"`
		RuntimeID         string `json:"runtimeId"`
		TaskID            string `json:"taskId"`
		Prompt            string `json:"prompt"`
		WorkspaceKey      string `json:"workspaceKey"`
		ApprovalExpiresAt string `json:"approvalExpiresAt"`
		CreatedAt         string `json:"createdAt"`
	}{
		ID: job.ID.String(), OwnerIdentity: job.OwnerIdentity, RuntimeID: job.RuntimeID, TaskID: job.TaskID,
		Prompt: job.Prompt, WorkspaceKey: job.WorkspaceKey,
		ApprovalExpiresAt: job.ApprovalExpiresAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
		CreatedAt:         job.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
	}
	encoded, _ := json.Marshal(immutable)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func startIntentFresh(job Job, now time.Time) bool {
	return job.StartIntentAt != nil &&
		!job.StartIntentAt.After(now) &&
		job.StartIntentAt.After(now.Add(-startIntentTimeout))
}

func sanitizeCompletion(completion Completion) Completion {
	completion.Output = boundedRedacted(completion.Output)
	completion.Error = boundedRedacted(completion.Error)
	return completion
}

func boundedRedacted(value string) string {
	value = strings.TrimSpace(safety.RedactSecrets(value))
	if len(value) > maxResultBytes {
		return strings.TrimSpace(value[:maxResultBytes])
	}
	return value
}
