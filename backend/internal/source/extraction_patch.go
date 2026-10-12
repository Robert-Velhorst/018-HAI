package source

import (
	"errors"
	"strings"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

var (
	ErrInvalidExtractionPatch                 = errors.New("extraction patch is empty or invalid")
	ErrExtractionPatchConflict                = errors.New("source extraction changed before patch could be applied")
	ErrExtractionPatchUnavailable             = errors.New("owner-scoped extraction patch is unavailable")
	ErrArchivedExtractionPatch                = errors.New("archived source extractions must be restored before editing")
	ErrExtractionCorrectionNotFound           = errors.New("source extraction correction not found")
	ErrExtractionCorrectionIdempotency        = errors.New("correction idempotency key was reused with different content")
	ErrExtractionCorrectionActive             = errors.New("a different source correction is already pending for this extraction")
	ErrExtractionCorrectionWorkerNotReady     = errors.New("durable source correction worker is unavailable")
	ErrExtractionCorrectionPersistenceUnknown = errors.New("source correction persistence outcome is unknown")
	ErrExtractionCorrectionNotSaved           = errors.New("source correction intent was not saved")
	ErrSourceExtractionNotFound               = errors.New("source extraction not found")
)

const (
	JobKindExtractionCorrection      = "source.extraction_correction"
	extractionCorrectionMaxAttempts  = 5
	extractionCorrectionMaxKeyLength = 128
)

// ExtractionPatch keeps omitted fields distinct from explicit false values.
// Only operator-editable content and review flags are exposed; source identity,
// provenance, archive state, and raw-item linkage cannot be patched.
type ExtractionPatch struct {
	Text       *string `json:"text"`
	Summary    *string `json:"summary"`
	ProjectKey *string `json:"projectKey"`
	Entities   *string `json:"entities"`
	Dates      *string `json:"dates"`
	Tasks      *string `json:"tasks"`
	Decisions  *string `json:"decisions"`
	FollowUps  *string `json:"followUps"`
	Sensitive  *bool   `json:"sensitive"`
	Uncertain  *bool   `json:"uncertain"`
}

// ExtractionCorrectionView is the complete public state of a correction
// intent. It deliberately omits the correction payload, before image, owner,
// durable job payload, and low-level failure details.
type ExtractionCorrectionView struct {
	ID               string     `json:"id,omitempty"`
	ExtractionID     string     `json:"extractionId"`
	Status           string     `json:"status"`
	Phase            string     `json:"phase"`
	IntentPersisted  bool       `json:"intentPersisted"`
	PatchSaved       bool       `json:"patchSaved"`
	RecoveryPending  bool       `json:"recoveryPending"`
	NeedsReview      bool       `json:"needsReview"`
	AlreadyApplied   bool       `json:"alreadyApplied,omitempty"`
	ExpectedRevision time.Time  `json:"expectedRevision"`
	AppliedRevision  *time.Time `json:"appliedRevision,omitempty"`
	Attempts         int        `json:"attempts"`
	MaxAttempts      int        `json:"maxAttempts"`
	ErrorCode        string     `json:"errorCode,omitempty"`
	Message          string     `json:"message"`
	CreatedAt        time.Time  `json:"createdAt,omitempty"`
	UpdatedAt        time.Time  `json:"updatedAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

type ExtractionCorrectionService interface {
	SubmitExtractionCorrection(ownerIdentity string, id uuid.UUID, expectedRevision time.Time, patch ExtractionPatch, idempotencyKey string) (*ExtractionCorrectionView, error)
	ExtractionCorrectionForOwner(ownerIdentity string, id uuid.UUID) (*ExtractionCorrectionView, error)
}

// ExtractionCorrectionPersistenceUnknownError is returned only when a database
// commit result could not be confirmed by a follow-up owner/idempotency lookup.
// The ID lets the caller query the status endpoint before considering a retry.
type ExtractionCorrectionPersistenceUnknownError struct {
	CorrectionID uuid.UUID
}

func (e *ExtractionCorrectionPersistenceUnknownError) Error() string {
	return ErrExtractionCorrectionPersistenceUnknown.Error()
}

func (e *ExtractionCorrectionPersistenceUnknownError) Unwrap() error {
	return ErrExtractionCorrectionPersistenceUnknown
}

type extractionCorrectionRepository interface {
	CreateExtractionCorrectionIntent(correction *models.SourceExtractionCorrection, job *models.DurableJob) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error)
	FindLatestAppliedExtractionCorrection(ownerIdentity string, extractionID uuid.UUID, patchJSON string, appliedRevision time.Time) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error)
	CreateExtractionCorrectionRecoveryIntent(correction *models.SourceExtractionCorrection, job *models.DurableJob, priorCorrectionID uuid.UUID) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error)
	FindActiveExtractionCorrection(ownerIdentity string, extractionID uuid.UUID, requestHash string) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error)
	FindExtractionCorrectionByIdempotency(ownerIdentity string, extractionID uuid.UUID, keyHash string) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error)
	FindExtractionCorrectionForOwner(ownerIdentity string, id uuid.UUID) (*models.SourceExtractionCorrection, *models.DurableJob, error)
	FindExtractionCorrectionForWorker(id, durableJobID uuid.UUID) (*models.SourceExtractionCorrection, *models.DurableJob, error)
	RecordExtractionCorrectionAttempt(id, durableJobID uuid.UUID, workerID string, generation int64, attempt int) error
	AdvanceExtractionCorrection(id, durableJobID uuid.UUID, workerID string, generation int64, from, to string) error
	ApplyExtractionCorrection(id, durableJobID uuid.UUID, workerID string, generation int64) (*models.SourceExtractionCorrection, *models.SourceExtraction, bool, error)
	FinishExtractionCorrection(id, durableJobID uuid.UUID, workerID string, generation int64, status, phase, errorCode string) error
}

type extractionCorrectionBeforeState struct {
	ID          uuid.UUID `json:"id"`
	SourceID    uuid.UUID `json:"sourceId"`
	RawItemID   uuid.UUID `json:"rawItemId"`
	Text        string    `json:"text"`
	ProjectKey  string    `json:"projectKey"`
	ContentType string    `json:"contentType"`
	Summary     string    `json:"summary"`
	Entities    string    `json:"entities"`
	Dates       string    `json:"dates"`
	Tasks       string    `json:"tasks"`
	Decisions   string    `json:"decisions"`
	FollowUps   string    `json:"followUps"`
	SourceURI   string    `json:"sourceUri"`
	SourceLabel string    `json:"sourceLabel"`
	Sensitive   bool      `json:"sensitive"`
	Uncertain   bool      `json:"uncertain"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func extractionCorrectionBefore(extraction *models.SourceExtraction) extractionCorrectionBeforeState {
	if extraction == nil {
		return extractionCorrectionBeforeState{}
	}
	return extractionCorrectionBeforeState{
		ID: extraction.ID, SourceID: extraction.SourceID, RawItemID: extraction.RawItemID,
		Text: extraction.Text, ProjectKey: extraction.ProjectKey, ContentType: extraction.ContentType,
		Summary: extraction.Summary, Entities: extraction.Entities, Dates: extraction.Dates,
		Tasks: extraction.Tasks, Decisions: extraction.Decisions, FollowUps: extraction.FollowUps,
		SourceURI: extraction.SourceURI, SourceLabel: extraction.SourceLabel,
		Sensitive: extraction.Sensitive, Uncertain: extraction.Uncertain,
		CreatedAt: extraction.CreatedAt, UpdatedAt: extraction.UpdatedAt,
	}
}

func (before extractionCorrectionBeforeState) extraction() *models.SourceExtraction {
	return &models.SourceExtraction{
		ID: before.ID, SourceID: before.SourceID, RawItemID: before.RawItemID,
		Text: before.Text, ProjectKey: before.ProjectKey, ContentType: before.ContentType,
		Summary: before.Summary, Entities: before.Entities, Dates: before.Dates,
		Tasks: before.Tasks, Decisions: before.Decisions, FollowUps: before.FollowUps,
		SourceURI: before.SourceURI, SourceLabel: before.SourceLabel,
		Sensitive: before.Sensitive, Uncertain: before.Uncertain,
		CreatedAt: before.CreatedAt, UpdatedAt: before.UpdatedAt,
	}
}

func (patch ExtractionPatch) applyTo(extraction *models.SourceExtraction) {
	if patch.Text != nil {
		extraction.Text = *patch.Text
	}
	if patch.Summary != nil {
		extraction.Summary = *patch.Summary
	}
	if patch.ProjectKey != nil {
		extraction.ProjectKey = *patch.ProjectKey
	}
	if patch.Entities != nil {
		extraction.Entities = *patch.Entities
	}
	if patch.Dates != nil {
		extraction.Dates = *patch.Dates
	}
	if patch.Tasks != nil {
		extraction.Tasks = *patch.Tasks
	}
	if patch.Decisions != nil {
		extraction.Decisions = *patch.Decisions
	}
	if patch.FollowUps != nil {
		extraction.FollowUps = *patch.FollowUps
	}
	if patch.Sensitive != nil {
		extraction.Sensitive = *patch.Sensitive
	}
	if patch.Uncertain != nil {
		extraction.Uncertain = *patch.Uncertain
	}
}

func (patch ExtractionPatch) matches(extraction *models.SourceExtraction) bool {
	if extraction == nil {
		return false
	}
	if patch.Text != nil && extraction.Text != *patch.Text {
		return false
	}
	if patch.Summary != nil && extraction.Summary != *patch.Summary {
		return false
	}
	if patch.ProjectKey != nil && extraction.ProjectKey != *patch.ProjectKey {
		return false
	}
	if patch.Entities != nil && extraction.Entities != *patch.Entities {
		return false
	}
	if patch.Dates != nil && extraction.Dates != *patch.Dates {
		return false
	}
	if patch.Tasks != nil && extraction.Tasks != *patch.Tasks {
		return false
	}
	if patch.Decisions != nil && extraction.Decisions != *patch.Decisions {
		return false
	}
	if patch.FollowUps != nil && extraction.FollowUps != *patch.FollowUps {
		return false
	}
	if patch.Sensitive != nil && extraction.Sensitive != *patch.Sensitive {
		return false
	}
	if patch.Uncertain != nil && extraction.Uncertain != *patch.Uncertain {
		return false
	}
	return true
}

func (patch ExtractionPatch) removesAction(before *models.SourceExtraction) bool {
	if before == nil {
		return false
	}
	candidate := *before
	patch.applyTo(&candidate)
	return firstNonEmpty(before.Tasks, before.FollowUps) != "" && firstNonEmpty(candidate.Tasks, candidate.FollowUps) == ""
}

func (patch ExtractionPatch) validate() error {
	if patch.Text == nil && patch.Summary == nil && patch.ProjectKey == nil &&
		patch.Entities == nil && patch.Dates == nil && patch.Tasks == nil &&
		patch.Decisions == nil && patch.FollowUps == nil &&
		patch.Sensitive == nil && patch.Uncertain == nil {
		return ErrInvalidExtractionPatch
	}
	for _, value := range []*string{patch.Text, patch.Summary, patch.ProjectKey} {
		if value != nil && strings.TrimSpace(*value) == "" {
			return ErrInvalidExtractionPatch
		}
	}
	return nil
}

func validateCorrectionIdempotencyKey(key string) bool {
	if len(key) < 16 || len(key) > extractionCorrectionMaxKeyLength {
		return false
	}
	for _, value := range key {
		if value < 0x21 || value > 0x7e {
			return false
		}
	}
	return true
}
