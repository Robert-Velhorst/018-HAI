package source

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type extractionCorrectionFakeRepository struct {
	*fakeSourceRepo
	corrections          map[uuid.UUID]*models.SourceExtractionCorrection
	correctionJobs       map[uuid.UUID]*models.DurableJob
	failAdvanceFrom      string
	failAdvanceNumber    int
	failApply            error
	workerFenceError     error
	workerFenceSkip      bool
	workerFenceNoRelease bool
	workerFenceCalls     int
	workerFenceReleases  int
	beforeWorkerFence    func(string, uuid.UUID)
	correctionLessons    map[correctionLessonKey]*models.ContextMemory
	lessonFailBefore     int
	lessonFailAfter      int
}

type correctionLessonKey struct {
	owner, sourceURI, kind string
}

type correctionMemoryKey struct {
	owner, project, kind, content string
}

type extractionCorrectionMemoryService struct {
	*fakeSourceMemoryService
	records         map[correctionMemoryKey]*models.ContextMemory
	failBeforeWrite int
	failAfterWrite  int
}

type extractionCorrectionLessonIndexerSpy struct {
	*fakeSourceMemoryService
	indexed []*models.ContextMemory
}

func (s *extractionCorrectionLessonIndexerSpy) IndexPersistedSourceExtractionLesson(saved *models.ContextMemory) {
	if saved != nil {
		copy := *saved
		s.indexed = append(s.indexed, &copy)
	}
}

func newExtractionCorrectionMemoryService() *extractionCorrectionMemoryService {
	return &extractionCorrectionMemoryService{
		fakeSourceMemoryService: &fakeSourceMemoryService{},
		records:                 map[correctionMemoryKey]*models.ContextMemory{},
	}
}

func (s *extractionCorrectionMemoryService) CreateForOwner(owner string, request memory.CreateRequest) (*models.ContextMemory, error) {
	key := correctionMemoryKey{owner: owner, project: request.ProjectKey, kind: request.Kind, content: request.Content}
	if existing := s.records[key]; existing != nil {
		copy := *existing
		return &copy, nil
	}
	if s.failBeforeWrite > 0 {
		s.failBeforeWrite--
		return nil, errors.New("injected memory store failure")
	}
	created, err := s.fakeSourceMemoryService.CreateForOwner(owner, request)
	if err != nil || created == nil {
		return created, err
	}
	s.records[key] = created
	if s.failAfterWrite > 0 {
		s.failAfterWrite--
		return nil, errors.New("injected ambiguous memory store response")
	}
	return created, nil
}

type extractionCorrectionProjectionSpy struct {
	calls int
	err   error
}

func (s *extractionCorrectionProjectionSpy) ProjectOperationalRecord(context.Context, lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error) {
	s.calls++
	return lifeontology.OperationalProjectionResult{}, s.err
}

func newExtractionCorrectionFakeRepository(base *fakeSourceRepo) *extractionCorrectionFakeRepository {
	return &extractionCorrectionFakeRepository{
		fakeSourceRepo: base, corrections: map[uuid.UUID]*models.SourceExtractionCorrection{},
		correctionJobs: map[uuid.UUID]*models.DurableJob{}, correctionLessons: map[correctionLessonKey]*models.ContextMemory{},
	}
}

func (r *extractionCorrectionFakeRepository) CreateExtractionCorrectionIntent(
	correction *models.SourceExtractionCorrection,
	job *models.DurableJob,
) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	for _, current := range r.corrections {
		if current.OwnerIdentity != correction.OwnerIdentity || current.ExtractionID != correction.ExtractionID {
			continue
		}
		if current.IdempotencyKeyHash == correction.IdempotencyKeyHash {
			if current.RequestHash != correction.RequestHash {
				return nil, nil, false, ErrExtractionCorrectionIdempotency
			}
			return copyCorrection(current), copyDurableJob(r.correctionJobs[current.DurableJobID]), false, nil
		}
		job := r.correctionJobs[current.DurableJobID]
		if current.Status == models.SourceExtractionCorrectionPending && job != nil &&
			(job.Status == models.DurableJobPending || job.Status == models.DurableJobRunning) {
			if current.RequestHash != correction.RequestHash {
				return nil, nil, false, ErrExtractionCorrectionActive
			}
			return copyCorrection(current), copyDurableJob(job), false, nil
		}
	}
	extraction, err := r.FindMutableExtractionForOwner(correction.ExtractionID, correction.OwnerIdentity)
	if err != nil {
		return nil, nil, false, err
	}
	if extraction.Archived {
		return nil, nil, false, ErrArchivedExtractionPatch
	}
	if !extraction.UpdatedAt.Equal(correction.ExpectedRevision) {
		return nil, nil, false, ErrExtractionPatchConflict
	}
	correction.SourceID = extraction.SourceID
	correction.Status = models.SourceExtractionCorrectionPending
	correction.Phase = correctionPhaseIntentPersisted
	job.Queue = "source"
	job.Kind = JobKindExtractionCorrection
	job.Status = models.DurableJobPending
	r.corrections[correction.ID] = copyCorrection(correction)
	r.correctionJobs[job.ID] = copyDurableJob(job)
	return copyCorrection(correction), copyDurableJob(job), true, nil
}

func (r *extractionCorrectionFakeRepository) FindLatestAppliedExtractionCorrection(
	owner string,
	extractionID uuid.UUID,
	patchJSON string,
	appliedRevision time.Time,
) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	var latest *models.SourceExtractionCorrection
	for _, correction := range r.corrections {
		if correction.OwnerIdentity != owner || correction.ExtractionID != extractionID ||
			(correction.Status != models.SourceExtractionCorrectionFailed && correction.Status != models.SourceExtractionCorrectionCompleted) ||
			correction.AppliedRevision == nil || !correction.AppliedRevision.Equal(appliedRevision) ||
			!sameCorrectionPatch(correction.PatchJSON, patchJSON) {
			continue
		}
		preferCompletion := correction.Status == models.SourceExtractionCorrectionCompleted &&
			latest != nil && latest.Status != models.SourceExtractionCorrectionCompleted
		sameStatus := latest != nil && correction.Status == latest.Status
		newer := sameStatus && (correction.UpdatedAt.After(latest.UpdatedAt) ||
			(correction.UpdatedAt.Equal(latest.UpdatedAt) && (correction.CreatedAt.After(latest.CreatedAt) ||
				(correction.CreatedAt.Equal(latest.CreatedAt) && correction.ID.String() > latest.ID.String()))))
		if latest == nil || preferCompletion || newer {
			latest = correction
		}
	}
	if latest == nil {
		return nil, nil, false, nil
	}
	return copyCorrection(latest), copyDurableJob(r.correctionJobs[latest.DurableJobID]), true, nil
}

func (r *extractionCorrectionFakeRepository) CreateExtractionCorrectionRecoveryIntent(
	correction *models.SourceExtractionCorrection,
	job *models.DurableJob,
	priorCorrectionID uuid.UUID,
) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	for _, current := range r.corrections {
		if current.OwnerIdentity != correction.OwnerIdentity || current.ExtractionID != correction.ExtractionID {
			continue
		}
		if current.IdempotencyKeyHash == correction.IdempotencyKeyHash {
			if current.RequestHash != correction.RequestHash {
				return nil, nil, false, ErrExtractionCorrectionIdempotency
			}
			return copyCorrection(current), copyDurableJob(r.correctionJobs[current.DurableJobID]), false, nil
		}
		currentJob := r.correctionJobs[current.DurableJobID]
		if current.Status == models.SourceExtractionCorrectionPending && currentJob != nil &&
			(currentJob.Status == models.DurableJobPending || currentJob.Status == models.DurableJobRunning) {
			if current.RequestHash != correction.RequestHash {
				return nil, nil, false, ErrExtractionCorrectionActive
			}
			return copyCorrection(current), copyDurableJob(currentJob), false, nil
		}
	}
	prior := r.corrections[priorCorrectionID]
	if prior == nil || prior.OwnerIdentity != correction.OwnerIdentity || prior.ExtractionID != correction.ExtractionID ||
		prior.Status != models.SourceExtractionCorrectionFailed || prior.AppliedRevision == nil ||
		!sameCorrectionPatch(prior.PatchJSON, correction.PatchJSON) {
		return nil, nil, false, ErrExtractionPatchConflict
	}
	extraction, err := r.FindMutableExtractionForOwner(correction.ExtractionID, correction.OwnerIdentity)
	if err != nil {
		return nil, nil, false, err
	}
	if extraction.Archived {
		return nil, nil, false, ErrArchivedExtractionPatch
	}
	if !extraction.UpdatedAt.Equal(correction.ExpectedRevision) || !prior.AppliedRevision.Equal(extraction.UpdatedAt) {
		return nil, nil, false, ErrExtractionPatchConflict
	}
	var patch ExtractionPatch
	if err := json.Unmarshal([]byte(correction.PatchJSON), &patch); err != nil || patch.validate() != nil || !patch.matches(extraction) {
		return nil, nil, false, ErrExtractionPatchConflict
	}
	var before extractionCorrectionBeforeState
	if err := json.Unmarshal([]byte(prior.BeforeStateJSON), &before); err != nil || before.ID != extraction.ID ||
		before.SourceID != extraction.SourceID || !before.UpdatedAt.Equal(prior.ExpectedRevision) {
		return nil, nil, false, errors.New("invalid recovery snapshot")
	}
	correction.SourceID = extraction.SourceID
	correction.BeforeStateJSON = prior.BeforeStateJSON
	correction.RequiresRetraction = prior.RequiresRetraction
	correction.AppliedRevision = &extraction.UpdatedAt
	correction.Phase = extractionCorrectionResumePhase(prior)
	correction.Status = models.SourceExtractionCorrectionPending
	correction.ErrorCode = ""
	correction.CompletedAt = nil
	correction.Attempts = 0
	job.Queue, job.Kind, job.Status = "source", JobKindExtractionCorrection, models.DurableJobPending
	job.Attempts = 0
	job.LockedBy, job.LockedAt, job.CompletedAt, job.LastError = "", nil, nil, ""
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = extractionCorrectionMaxAttempts
	}
	r.corrections[correction.ID] = copyCorrection(correction)
	r.correctionJobs[job.ID] = copyDurableJob(job)
	return copyCorrection(correction), copyDurableJob(job), true, nil
}

func sameCorrectionPatch(first, second string) bool {
	var firstPatch, secondPatch ExtractionPatch
	if json.Unmarshal([]byte(first), &firstPatch) != nil || json.Unmarshal([]byte(second), &secondPatch) != nil {
		return false
	}
	firstJSON, firstErr := json.Marshal(firstPatch)
	secondJSON, secondErr := json.Marshal(secondPatch)
	return firstErr == nil && secondErr == nil && string(firstJSON) == string(secondJSON)
}

func (r *extractionCorrectionFakeRepository) FindActiveExtractionCorrection(owner string, extractionID uuid.UUID, requestHash string) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	for _, correction := range r.corrections {
		if correction.OwnerIdentity == owner && correction.ExtractionID == extractionID && correction.RequestHash == requestHash && correction.Status == models.SourceExtractionCorrectionPending {
			job := r.correctionJobs[correction.DurableJobID]
			if job != nil && (job.Status == models.DurableJobPending || job.Status == models.DurableJobRunning) {
				return copyCorrection(correction), copyDurableJob(job), true, nil
			}
		}
	}
	return nil, nil, false, nil
}

func (r *extractionCorrectionFakeRepository) FindExtractionCorrectionByIdempotency(owner string, extractionID uuid.UUID, keyHash string) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	for _, correction := range r.corrections {
		if correction.OwnerIdentity == owner && correction.ExtractionID == extractionID && correction.IdempotencyKeyHash == keyHash {
			return copyCorrection(correction), copyDurableJob(r.correctionJobs[correction.DurableJobID]), true, nil
		}
	}
	return nil, nil, false, nil
}

func (r *extractionCorrectionFakeRepository) FindExtractionCorrectionForOwner(owner string, id uuid.UUID) (*models.SourceExtractionCorrection, *models.DurableJob, error) {
	correction := r.corrections[id]
	if correction == nil || correction.OwnerIdentity != owner {
		return nil, nil, gorm.ErrRecordNotFound
	}
	return copyCorrection(correction), copyDurableJob(r.correctionJobs[correction.DurableJobID]), nil
}

func (r *extractionCorrectionFakeRepository) FindExtractionCorrectionForWorker(id, durableJobID uuid.UUID) (*models.SourceExtractionCorrection, *models.DurableJob, error) {
	correction := r.corrections[id]
	job := r.correctionJobs[durableJobID]
	if correction == nil || correction.DurableJobID != durableJobID || job == nil {
		return nil, nil, gorm.ErrRecordNotFound
	}
	return copyCorrection(correction), copyDurableJob(job), nil
}

func (r *extractionCorrectionFakeRepository) AcquireExtractionCorrectionSessionLock(
	ctx context.Context,
	ownerIdentity string,
	extractionID uuid.UUID,
) (func(), bool, error) {
	r.workerFenceCalls++
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if r.workerFenceError != nil {
		return nil, false, r.workerFenceError
	}
	if r.beforeWorkerFence != nil {
		before := r.beforeWorkerFence
		r.beforeWorkerFence = nil
		before(ownerIdentity, extractionID)
	}
	if r.workerFenceSkip {
		return nil, false, nil
	}
	if r.workerFenceNoRelease {
		return nil, true, nil
	}
	return func() { r.workerFenceReleases++ }, true, nil
}

func (r *extractionCorrectionFakeRepository) PersistExtractionCorrectionLesson(
	ctx context.Context,
	correctionID uuid.UUID,
	durableJobID uuid.UUID,
	workerID string,
	generation int64,
	appliedRevision time.Time,
	ownerIdentity string,
	sourceURI string,
	request memory.CreateRequest,
) (*models.ContextMemory, error) {
	if ctx == nil {
		return nil, errors.New("source correction lesson context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.requireLease(durableJobID, workerID, generation); err != nil {
		return nil, err
	}
	correction := r.corrections[correctionID]
	if correction == nil || correction.DurableJobID != durableJobID || correction.OwnerIdentity != ownerIdentity ||
		correction.Status != models.SourceExtractionCorrectionPending || correction.Phase != correctionPhaseGraphProjected ||
		correction.AppliedRevision == nil || !correction.AppliedRevision.Equal(appliedRevision) {
		return nil, ErrExtractionCorrectionNotFound
	}
	canonicalURI := "source-extraction://" + correction.ExtractionID.String()
	if sourceURI != canonicalURI {
		return nil, errors.New("source correction lesson key does not match extraction")
	}
	var before extractionCorrectionBeforeState
	if err := json.Unmarshal([]byte(correction.BeforeStateJSON), &before); err != nil || before.ID != correction.ExtractionID ||
		before.SourceID != correction.SourceID || before.UpdatedAt.After(correction.ExpectedRevision) {
		return nil, errors.New("source correction before-state is invalid")
	}
	extraction, err := r.FindMutableExtractionForOwner(correction.ExtractionID, ownerIdentity)
	if err != nil {
		return nil, err
	}
	if extraction.Archived || !extraction.UpdatedAt.Equal(appliedRevision) {
		return nil, errExtractionCorrectionLeaseLost
	}
	source, err := r.FindSource(correction.SourceID)
	if err != nil {
		return nil, err
	}
	if source == nil || source.OwnerIdentity != ownerIdentity {
		return nil, errors.New("source correction owner changed")
	}
	expected := extractionCorrectionMemoryRequest(source, before.extraction(), extraction)
	if !reflect.DeepEqual(expected, request) {
		return nil, errors.New("source correction lesson no longer matches extraction")
	}
	key := correctionLessonKey{owner: ownerIdentity, sourceURI: canonicalURI, kind: request.Kind}
	if r.lessonFailBefore > 0 {
		r.lessonFailBefore--
		return nil, errors.New("injected correction lesson transaction failure")
	}
	now := time.Now().UTC()
	persisted := &models.ContextMemory{
		ID: uuid.New(), OwnerIdentity: ownerIdentity, ProjectKey: request.ProjectKey, Kind: request.Kind,
		Content: request.Content, Summary: request.Summary, Tags: strings.Join(request.Tags, ","),
		Confidence: request.Confidence, SourceURI: request.SourceURI, SourceLabel: request.SourceLabel,
		SourceExtractionID: uuidPointer(correction.ExtractionID), CreatedAt: now, UpdatedAt: now,
	}
	if existing := r.correctionLessons[key]; existing != nil {
		persisted.ID = existing.ID
		persisted.CreatedAt = existing.CreatedAt
	}
	r.correctionLessons[key] = persisted
	if r.lessonFailAfter > 0 {
		r.lessonFailAfter--
		return nil, errors.New("injected ambiguous correction lesson commit response")
	}
	copy := *persisted
	return &copy, nil
}

func uuidPointer(value uuid.UUID) *uuid.UUID {
	copy := value
	return &copy
}

func (r *extractionCorrectionFakeRepository) RecordExtractionCorrectionAttempt(id, jobID uuid.UUID, worker string, generation int64, attempt int) error {
	if err := r.requireLease(jobID, worker, generation); err != nil {
		return err
	}
	correction := r.corrections[id]
	if correction == nil || correction.DurableJobID != jobID || correction.Status != models.SourceExtractionCorrectionPending {
		return ErrExtractionCorrectionNotFound
	}
	if attempt > correction.Attempts {
		correction.Attempts = attempt
	}
	return nil
}

func (r *extractionCorrectionFakeRepository) AdvanceExtractionCorrection(id, jobID uuid.UUID, worker string, generation int64, from, to string) error {
	if err := r.requireLease(jobID, worker, generation); err != nil {
		return err
	}
	correction := r.corrections[id]
	if correction == nil || correction.DurableJobID != jobID || correction.Status != models.SourceExtractionCorrectionPending {
		return ErrExtractionCorrectionNotFound
	}
	if r.failAdvanceNumber > 0 && r.failAdvanceFrom == from {
		r.failAdvanceNumber--
		return errors.New("injected checkpoint failure")
	}
	if correction.Phase == to {
		return nil
	}
	if correction.Phase != from {
		return errors.New("unexpected correction phase")
	}
	correction.Phase = to
	return nil
}

func (r *extractionCorrectionFakeRepository) ApplyExtractionCorrection(id, jobID uuid.UUID, worker string, generation int64) (*models.SourceExtractionCorrection, *models.SourceExtraction, bool, error) {
	if err := r.requireLease(jobID, worker, generation); err != nil {
		return nil, nil, false, err
	}
	if r.failApply != nil {
		return nil, nil, false, r.failApply
	}
	correction := r.corrections[id]
	if correction == nil || correction.DurableJobID != jobID {
		return nil, nil, false, ErrExtractionCorrectionNotFound
	}
	if correction.Status != models.SourceExtractionCorrectionPending {
		return copyCorrection(correction), nil, correction.Status == models.SourceExtractionCorrectionConflict, nil
	}
	if correction.Phase == correctionPhasePatchApplied || correction.Phase == correctionPhaseWorkflowReconciled ||
		correction.Phase == correctionPhaseIndexUpdated || correction.Phase == correctionPhaseGraphProjected || correction.Phase == correctionPhaseMemoryRecorded {
		current, err := r.FindMutableExtractionForOwner(correction.ExtractionID, correction.OwnerIdentity)
		return copyCorrection(correction), current, false, err
	}
	if correction.Phase != correctionPhaseIntentPersisted && correction.Phase != correctionPhaseWorkflowRetracted {
		return nil, nil, false, errors.New("correction phase is not ready to apply")
	}
	current, err := r.FindMutableExtractionForOwner(correction.ExtractionID, correction.OwnerIdentity)
	if err != nil || current.Archived || !current.UpdatedAt.Equal(correction.ExpectedRevision) {
		if correction.Phase == correctionPhaseWorkflowRetracted {
			correction.Phase = correctionPhaseConflictReconcile
			correction.ErrorCode = "revision_conflict"
		} else {
			correction.Status = models.SourceExtractionCorrectionConflict
			correction.Phase = correctionPhaseConflict
			correction.ErrorCode = "revision_conflict"
		}
		return copyCorrection(correction), nil, true, nil
	}
	var patch ExtractionPatch
	if err := json.Unmarshal([]byte(correction.PatchJSON), &patch); err != nil {
		return nil, nil, false, err
	}
	patch.applyTo(current)
	current.UpdatedAt = correction.ExpectedRevision.Add(time.Microsecond)
	if stored := r.extractions[correction.ExtractionID]; stored != nil {
		*stored = *current
	}
	correction.AppliedRevision = &current.UpdatedAt
	correction.Phase = correctionPhasePatchApplied
	return copyCorrection(correction), current, false, nil
}

func (r *extractionCorrectionFakeRepository) FinishExtractionCorrection(id, jobID uuid.UUID, worker string, generation int64, status, phase, errorCode string) error {
	if err := r.requireLease(jobID, worker, generation); err != nil {
		return err
	}
	correction := r.corrections[id]
	if correction == nil || correction.DurableJobID != jobID {
		return ErrExtractionCorrectionNotFound
	}
	if correction.Status == status && correction.Phase == phase {
		return nil
	}
	if correction.Status != models.SourceExtractionCorrectionPending {
		return ErrExtractionCorrectionNotFound
	}
	correction.Status, correction.Phase, correction.ErrorCode = status, phase, errorCode
	now := time.Now().UTC()
	correction.CompletedAt = &now
	correction.UpdatedAt = now
	return nil
}

func (r *extractionCorrectionFakeRepository) requireLease(jobID uuid.UUID, worker string, generation int64) error {
	job := r.correctionJobs[jobID]
	if job == nil || job.Status != models.DurableJobRunning || job.LockedBy != worker || job.LeaseGeneration != generation {
		return errExtractionCorrectionLeaseLost
	}
	return nil
}

func (r *extractionCorrectionFakeRepository) claim(id uuid.UUID, attempt int) durablejob.Job {
	correction := r.corrections[id]
	job := r.correctionJobs[correction.DurableJobID]
	job.Status, job.LockedBy, job.LeaseGeneration = models.DurableJobRunning, "correction-test-worker", job.LeaseGeneration+1
	job.Attempts = attempt
	lockedAt := time.Now().UTC()
	job.LockedAt = &lockedAt
	return *copyDurableJob(job)
}

func copyCorrection(value *models.SourceExtractionCorrection) *models.SourceExtractionCorrection {
	if value == nil {
		return nil
	}
	copy := *value
	if value.AppliedRevision != nil {
		applied := *value.AppliedRevision
		copy.AppliedRevision = &applied
	}
	if value.CompletedAt != nil {
		completed := *value.CompletedAt
		copy.CompletedAt = &completed
	}
	return &copy
}

func copyDurableJob(value *models.DurableJob) *models.DurableJob {
	if value == nil {
		return nil
	}
	copy := *value
	if value.LockedAt != nil {
		locked := *value.LockedAt
		copy.LockedAt = &locked
	}
	return &copy
}

type extractionCorrectionWorkflowStub struct {
	workflow.Service
	intakeCalls  int
	retractCalls int
	effectKeys   map[string]bool
	intakeErr    error
	retractErr   error
}

func (s *extractionCorrectionWorkflowStub) Intake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	s.intakeCalls++
	if s.intakeErr != nil {
		return nil, s.intakeErr
	}
	if s.effectKeys == nil {
		s.effectKeys = map[string]bool{}
	}
	s.effectKeys[request.ExtractionID] = true
	return nil, nil
}

func (s *extractionCorrectionWorkflowStub) RetractSource(_, _, _ string) error {
	s.retractCalls++
	return s.retractErr
}

func newExtractionCorrectionFixture(t *testing.T, withWorkflow bool) (*extractionCorrectionFakeRepository, *models.SourceExtraction, *service, *extractionCorrectionWorkflowStub) {
	t.Helper()
	sourceRecord := &models.ConnectedSource{ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "gmail", Category: "email", Name: "source"}
	base := newFakeSourceRepo(sourceRecord)
	extraction, err := base.SaveExtraction(&models.SourceExtraction{
		ID: uuid.New(), SourceID: sourceRecord.ID, RawItemID: uuid.New(), Text: "private raw text",
		Summary: "before", Tasks: "existing task", Sensitive: true, Uncertain: true,
	})
	if err != nil {
		t.Fatalf("save extraction: %v", err)
	}
	repo := newExtractionCorrectionFakeRepository(base)
	var workflowStub *extractionCorrectionWorkflowStub
	var svc *service
	if withWorkflow {
		workflowStub = &extractionCorrectionWorkflowStub{}
		svc = NewServiceWithWorkflow(repo, newExtractionCorrectionMemoryService(), workflowStub).(*service)
	} else {
		svc = NewService(repo, newExtractionCorrectionMemoryService()).(*service)
	}
	copy := *extraction
	return repo, &copy, svc, workflowStub
}

func submitTestCorrection(t *testing.T, svc *service, extraction *models.SourceExtraction, patch ExtractionPatch, key string) *ExtractionCorrectionView {
	t.Helper()
	view, err := svc.SubmitExtractionCorrection("alice", extraction.ID, extraction.UpdatedAt, patch, key)
	if err != nil {
		t.Fatalf("SubmitExtractionCorrection: %v", err)
	}
	return view
}

func runTestCorrection(svc *service, repo *extractionCorrectionFakeRepository, correctionID uuid.UUID, attempt int) error {
	job := repo.claim(correctionID, attempt)
	err := svc.runExtractionCorrection(context.Background(), job, correctionID)
	stored := repo.correctionJobs[job.ID]
	if stored == nil {
		return err
	}
	stored.LockedBy, stored.LockedAt = "", nil
	stored.Attempts = job.Attempts + 1
	if err == nil {
		stored.Status = models.DurableJobSucceeded
		now := time.Now().UTC()
		stored.CompletedAt = &now
		return nil
	}
	var deferred *durablejob.DeferredError
	if errors.As(err, &deferred) {
		stored.Status = models.DurableJobPending
		return err
	}
	retryable := true
	var classification durablejob.RetryClassification
	if errors.As(err, &classification) {
		retryable = classification.Retryable()
	}
	if retryable && (job.MaxAttempts <= 0 || stored.Attempts < job.MaxAttempts) {
		stored.Status = models.DurableJobPending
	} else {
		stored.Status = models.DurableJobDead
		now := time.Now().UTC()
		stored.CompletedAt = &now
	}
	stored.LastError = err.Error()
	return err
}

func TestSubmitExtractionCorrectionPersistsIntentBeforePatchAndScopesStatus(t *testing.T) {
	repo, extraction, svc, _ := newExtractionCorrectionFixture(t, false)
	newSummary := "after"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-key-0001")
	if !view.IntentPersisted || view.PatchSaved || !view.RecoveryPending || view.Status != models.SourceExtractionCorrectionPending {
		t.Fatalf("initial correction state = %#v", view)
	}
	if repo.extractions[extraction.ID].Summary != "before" {
		t.Fatal("submission changed the extraction before the worker applied the saved intent")
	}
	correctionID, err := uuid.Parse(view.ID)
	if err != nil {
		t.Fatalf("correction ID: %v", err)
	}
	correction := repo.corrections[correctionID]
	job := repo.correctionJobs[correction.DurableJobID]
	if correction == nil || job == nil || job.Queue != "source" || job.Kind != JobKindExtractionCorrection || job.Status != models.DurableJobPending {
		t.Fatalf("outbox intent/job not durably paired: correction=%#v job=%#v", correction, job)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil || len(payload) != 1 || payload["correctionId"] != correctionID.String() {
		t.Fatalf("job payload must contain only the correction ID: %s (%v)", job.Payload, err)
	}
	if _, err := svc.ExtractionCorrectionForOwner("bob", correctionID); !errors.Is(err, ErrExtractionCorrectionNotFound) {
		t.Fatalf("foreign owner status error = %v, want not found", err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal public status: %v", err)
	}
	if strings.Contains(string(encoded), "private raw text") || strings.Contains(string(encoded), "alice") || strings.Contains(string(encoded), "after") {
		t.Fatalf("public status leaked private correction/source data: %s", encoded)
	}
}

func TestSubmitExtractionCorrectionRejectsStaleRevisionAndIdempotencyReuse(t *testing.T) {
	_, extraction, svc, _ := newExtractionCorrectionFixture(t, false)
	newSummary := "after"
	stale := extraction.UpdatedAt.Add(-time.Second)
	if _, err := svc.SubmitExtractionCorrection("alice", extraction.ID, stale, ExtractionPatch{Summary: &newSummary}, "correction-key-stale"); !errors.Is(err, ErrExtractionPatchConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	first := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-key-reused")
	other := "different"
	if _, err := svc.SubmitExtractionCorrection("alice", extraction.ID, extraction.UpdatedAt, ExtractionPatch{Summary: &other}, "correction-key-reused"); !errors.Is(err, ErrExtractionCorrectionIdempotency) {
		t.Fatalf("reused key error = %v", err)
	}
	second := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-key-other")
	if first.ID != second.ID {
		t.Fatalf("same active correction created duplicate intents: %s != %s", first.ID, second.ID)
	}
}

func TestRunExtractionCorrectionCompletesAndResumesEveryPersistedPhase(t *testing.T) {
	phases := []string{
		correctionPhaseIntentPersisted, correctionPhaseWorkflowRetracted, correctionPhasePatchApplied,
		correctionPhaseWorkflowReconciled, correctionPhaseIndexUpdated, correctionPhaseGraphProjected, correctionPhaseMemoryRecorded,
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			repo, extraction, svc, _ := newExtractionCorrectionFixture(t, true)
			newSummary := "after"
			view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-key-"+phase)
			id, _ := uuid.Parse(view.ID)
			correction := repo.corrections[id]
			if phase != correctionPhaseIntentPersisted && phase != correctionPhaseWorkflowRetracted {
				current := repo.extractions[extraction.ID]
				current.Summary = newSummary
				current.UpdatedAt = extraction.UpdatedAt.Add(time.Microsecond)
				correction.AppliedRevision = &current.UpdatedAt
			}
			correction.Phase = phase
			if err := runTestCorrection(svc, repo, id, 0); err != nil {
				t.Fatalf("resume worker: %v", err)
			}
			result, err := svc.ExtractionCorrectionForOwner("alice", id)
			if err != nil || result.Status != models.SourceExtractionCorrectionCompleted || !result.PatchSaved || result.RecoveryPending {
				t.Fatalf("resumed result = %#v, err=%v", result, err)
			}
			if repo.extractions[extraction.ID].Summary != newSummary {
				t.Fatalf("source summary = %q, want %q", repo.extractions[extraction.ID].Summary, newSummary)
			}
		})
	}
}

func TestRunExtractionCorrectionRechecksIntentAfterFenceBeforeAnySideEffects(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	newSummary := "corrected summary"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-delete-race-key")
	correctionID, _ := uuid.Parse(view.ID)
	job := repo.claim(correctionID, 0)
	jobID := job.ID
	repo.beforeWorkerFence = func(owner string, extractionID uuid.UUID) {
		if owner != "alice" || extractionID != extraction.ID {
			t.Fatalf("worker fence identity = %q/%s, want alice/%s", owner, extractionID, extraction.ID)
		}
		delete(repo.corrections, correctionID)
		delete(repo.correctionJobs, jobID)
	}

	if err := svc.runExtractionCorrection(context.Background(), job, correctionID); err != nil {
		t.Fatalf("worker after deletion committed: %v, want clean stop", err)
	}
	if repo.workerFenceCalls != 1 || repo.workerFenceReleases != 1 {
		t.Fatalf("session fence calls/releases = %d/%d, want 1/1", repo.workerFenceCalls, repo.workerFenceReleases)
	}
	if workflowStub.intakeCalls != 0 || workflowStub.retractCalls != 0 || len(repo.index) != 0 {
		t.Fatalf("deleted correction caused effects: workflow intake=%d retract=%d index entries=%d",
			workflowStub.intakeCalls, workflowStub.retractCalls, len(repo.index))
	}
	if got := svc.memoryService.(*extractionCorrectionMemoryService).records; len(got) != 0 {
		t.Fatalf("deleted correction wrote memory: %#v", got)
	}
	current, err := repo.FindMutableExtractionForOwner(extraction.ID, "alice")
	if err != nil || current.Summary != extraction.Summary {
		t.Fatalf("deleted correction changed extraction: current=%#v err=%v", current, err)
	}
}

func TestRunExtractionCorrectionStopsForCancelledStateUnderFence(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	newSummary := "corrected summary"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-cancelled-state-key")
	correctionID, _ := uuid.Parse(view.ID)
	job := repo.claim(correctionID, 0)
	repo.beforeWorkerFence = func(owner string, extractionID uuid.UUID) {
		correction := repo.corrections[correctionID]
		durable := repo.correctionJobs[job.ID]
		correction.Status = models.SourceExtractionCorrectionFailed
		durable.Status = models.DurableJobDead
	}

	if err := svc.runExtractionCorrection(context.Background(), job, correctionID); err != nil {
		t.Fatalf("worker for cancelled correction: %v, want clean stop", err)
	}
	if repo.workerFenceReleases != 1 {
		t.Fatalf("session lock releases = %d, want 1", repo.workerFenceReleases)
	}
	if workflowStub.intakeCalls != 0 || workflowStub.retractCalls != 0 || len(repo.index) != 0 ||
		len(svc.memoryService.(*extractionCorrectionMemoryService).records) != 0 {
		t.Fatal("cancelled correction caused downstream effects")
	}
}

func TestRunExtractionCorrectionRechecksLeaseGenerationUnderFence(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	newSummary := "corrected summary"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-lease-recheck-key")
	correctionID, _ := uuid.Parse(view.ID)
	job := repo.claim(correctionID, 0)
	repo.beforeWorkerFence = func(string, uuid.UUID) {
		durable := repo.correctionJobs[job.ID]
		durable.LockedBy = "replacement-worker"
		durable.LeaseGeneration++
	}

	if err := svc.runExtractionCorrection(context.Background(), job, correctionID); err != nil {
		t.Fatalf("stale worker after lease reassignment: %v, want clean stop", err)
	}
	if repo.workerFenceReleases != 1 {
		t.Fatalf("session lock releases = %d, want 1", repo.workerFenceReleases)
	}
	if workflowStub.intakeCalls != 0 || workflowStub.retractCalls != 0 || len(repo.index) != 0 ||
		len(svc.memoryService.(*extractionCorrectionMemoryService).records) != 0 {
		t.Fatal("worker with a superseded lease caused downstream effects")
	}
	current, findErr := repo.FindMutableExtractionForOwner(extraction.ID, "alice")
	if findErr != nil || current.Summary != extraction.Summary {
		t.Fatalf("superseded worker lease changed extraction: current=%#v err=%v", current, findErr)
	}
}

func TestRunExtractionCorrectionFailsClosedWhenSessionLockCannotBeAcquired(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	newSummary := "corrected summary"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-lock-error-key")
	correctionID, _ := uuid.Parse(view.ID)
	job := repo.claim(correctionID, 0)
	repo.workerFenceError = errors.New("advisory lock unavailable")

	err := svc.runExtractionCorrection(context.Background(), job, correctionID)
	var workerErr *extractionCorrectionWorkerError
	if !errors.As(err, &workerErr) || !workerErr.Retryable() {
		t.Fatalf("lock failure error = %v, want retryable fail-closed worker error", err)
	}
	if workflowStub.intakeCalls != 0 || workflowStub.retractCalls != 0 || len(repo.index) != 0 ||
		len(svc.memoryService.(*extractionCorrectionMemoryService).records) != 0 {
		t.Fatal("worker produced downstream effects without acquiring the session lock")
	}
	current, findErr := repo.FindMutableExtractionForOwner(extraction.ID, "alice")
	if findErr != nil || current.Summary != extraction.Summary {
		t.Fatalf("lock failure changed extraction: current=%#v err=%v", current, findErr)
	}
}

func TestRunExtractionCorrectionDefersWhenSessionLockIsBusy(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	newSummary := "corrected summary"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-lock-busy-key")
	correctionID, _ := uuid.Parse(view.ID)
	job := repo.claim(correctionID, 0)
	repo.workerFenceSkip = true

	err := svc.runExtractionCorrection(context.Background(), job, correctionID)
	if err == nil || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("busy session lock result = %v, want durable-job defer", err)
	}
	if workflowStub.intakeCalls != 0 || workflowStub.retractCalls != 0 || len(repo.index) != 0 ||
		len(svc.memoryService.(*extractionCorrectionMemoryService).records) != 0 {
		t.Fatal("worker produced downstream effects while the session lock was busy")
	}
}

func TestRunExtractionCorrectionFailsClosedWhenSessionLockCannotBeReleased(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	newSummary := "corrected summary"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-lock-release-key")
	correctionID, _ := uuid.Parse(view.ID)
	job := repo.claim(correctionID, 0)
	repo.workerFenceNoRelease = true

	err := svc.runExtractionCorrection(context.Background(), job, correctionID)
	var workerErr *extractionCorrectionWorkerError
	if !errors.As(err, &workerErr) || !workerErr.Retryable() {
		t.Fatalf("missing release function error = %v, want retryable fail-closed worker error", err)
	}
	if workflowStub.intakeCalls != 0 || workflowStub.retractCalls != 0 || len(repo.index) != 0 ||
		len(svc.memoryService.(*extractionCorrectionMemoryService).records) != 0 {
		t.Fatal("worker produced downstream effects without a releasable session lock")
	}
	current, findErr := repo.FindMutableExtractionForOwner(extraction.ID, "alice")
	if findErr != nil || current.Summary != extraction.Summary {
		t.Fatalf("missing release function changed extraction: current=%#v err=%v", current, findErr)
	}
}

func TestRunExtractionCorrectionReleasesFenceWhenContextIsCancelled(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	newSummary := "corrected summary"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-cancel-lock-key")
	correctionID, _ := uuid.Parse(view.ID)
	job := repo.claim(correctionID, 0)
	ctx, cancel := context.WithCancel(context.Background())
	repo.beforeWorkerFence = func(string, uuid.UUID) { cancel() }

	err := svc.runExtractionCorrection(ctx, job, correctionID)
	cancel()
	if err == nil || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("cancelled worker result = %v, want durable-job defer", err)
	}
	if repo.workerFenceReleases != 1 {
		t.Fatalf("session lock releases = %d, want 1 after cancellation", repo.workerFenceReleases)
	}
	if workflowStub.intakeCalls != 0 || workflowStub.retractCalls != 0 || len(repo.index) != 0 ||
		len(svc.memoryService.(*extractionCorrectionMemoryService).records) != 0 {
		t.Fatal("cancelled worker produced downstream effects")
	}
}

func TestExtractionCorrectionAtLeastOnceRecoveryAfterEffectBeforeCheckpoint(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	newSummary := "after"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-key-crash-recovery")
	id, _ := uuid.Parse(view.ID)
	repo.failAdvanceFrom = correctionPhasePatchApplied
	repo.failAdvanceNumber = 1
	if err := runTestCorrection(svc, repo, id, 0); err == nil {
		t.Fatal("first delivery should fail at the injected post-effect checkpoint")
	}
	checkpoint, err := svc.ExtractionCorrectionForOwner("alice", id)
	if err != nil || checkpoint.Status != models.SourceExtractionCorrectionPending || !checkpoint.PatchSaved || !checkpoint.RecoveryPending {
		t.Fatalf("post-crash status = %#v, err=%v", checkpoint, err)
	}
	if repo.extractions[extraction.ID].Summary != newSummary {
		t.Fatal("the patch side effect was not atomically recorded before later projections")
	}
	if err := runTestCorrection(svc, repo, id, 1); err != nil {
		t.Fatalf("recovered delivery: %v", err)
	}
	finished, err := svc.ExtractionCorrectionForOwner("alice", id)
	if err != nil || finished.Status != models.SourceExtractionCorrectionCompleted || !finished.PatchSaved || finished.RecoveryPending {
		t.Fatalf("recovered correction status = %#v, err=%v", finished, err)
	}
	if workflowStub.intakeCalls != 2 || len(workflowStub.effectKeys) != 1 {
		t.Fatalf("workflow replay calls=%d unique effects=%d, want at-least-once replay with one idempotent effect", workflowStub.intakeCalls, len(workflowStub.effectKeys))
	}
	if len(repo.index) == 0 {
		t.Fatal("recovery did not rebuild the source index")
	}
}

func TestExtractionCorrectionRevisionConflictIsTerminalAndDoesNotOverwrite(t *testing.T) {
	repo, extraction, svc, _ := newExtractionCorrectionFixture(t, false)
	newSummary := "requested"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-key-conflict")
	id, _ := uuid.Parse(view.ID)
	repo.extractions[extraction.ID].Summary = "newer external value"
	repo.extractions[extraction.ID].UpdatedAt = extraction.UpdatedAt.Add(time.Second)
	if err := runTestCorrection(svc, repo, id, 0); err != nil {
		t.Fatalf("conflict should be a terminal outcome, not a retry: %v", err)
	}
	result, err := svc.ExtractionCorrectionForOwner("alice", id)
	if err != nil || result.Status != models.SourceExtractionCorrectionConflict || result.PatchSaved || result.RecoveryPending || !result.NeedsReview {
		t.Fatalf("conflict status = %#v, err=%v", result, err)
	}
	if got := repo.extractions[extraction.ID].Summary; got != "newer external value" {
		t.Fatalf("stale correction overwrote newer source value: %q", got)
	}
}

func TestExtractionCorrectionStopsWhenAppliedRevisionIsSuperseded(t *testing.T) {
	phases := []string{
		correctionPhasePatchApplied,
		correctionPhaseWorkflowReconciled,
		correctionPhaseIndexUpdated,
		correctionPhaseGraphProjected,
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
			projector := &extractionCorrectionProjectionSpy{}
			svc.lifeOntologyProjector = projector
			correctedSummary := "Robert corrected summary"
			view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &correctedSummary}, "correction-revision-gate-"+phase)
			id, err := uuid.Parse(view.ID)
			if err != nil {
				t.Fatalf("parse correction ID: %v", err)
			}
			appliedRevision := extraction.UpdatedAt.Add(time.Microsecond)
			repo.extractions[extraction.ID].Summary = correctedSummary
			repo.extractions[extraction.ID].UpdatedAt = appliedRevision
			repo.corrections[id].AppliedRevision = &appliedRevision
			repo.corrections[id].Phase = phase

			laterSummary := "newer sync content"
			repo.extractions[extraction.ID].Summary = laterSummary
			repo.extractions[extraction.ID].UpdatedAt = appliedRevision.Add(time.Microsecond)
			if err := runTestCorrection(svc, repo, id, 0); err != nil {
				t.Fatalf("worker should record a reviewable conflict: %v", err)
			}
			result, err := svc.ExtractionCorrectionForOwner("alice", id)
			if err != nil || result.Status != models.SourceExtractionCorrectionConflict || !result.NeedsReview || result.RecoveryPending ||
				result.ErrorCode != "applied_revision_superseded" || !result.PatchSaved {
				t.Fatalf("superseded correction state = %#v, err=%v", result, err)
			}
			if got := repo.extractions[extraction.ID].Summary; got != laterSummary {
				t.Fatalf("worker changed later source state to %q, want %q", got, laterSummary)
			}
			memoryService := svc.memoryService.(*extractionCorrectionMemoryService)
			switch phase {
			case correctionPhasePatchApplied:
				if workflowStub.intakeCalls != 0 {
					t.Fatalf("workflow intake calls = %d, want none for superseded correction", workflowStub.intakeCalls)
				}
			case correctionPhaseWorkflowReconciled:
				if len(repo.index) != 0 {
					t.Fatalf("index entries = %#v, want none for superseded correction", repo.index)
				}
			case correctionPhaseIndexUpdated:
				if projector.calls != 0 {
					t.Fatalf("life-graph projection calls = %d, want none for superseded correction", projector.calls)
				}
			case correctionPhaseGraphProjected:
				if len(memoryService.ownerCreated) != 0 || len(repo.correctionLessons) != 0 {
					t.Fatalf("memory lessons generic/exact = %d/%d, want none for superseded correction", len(memoryService.ownerCreated), len(repo.correctionLessons))
				}
			}
		})
	}
}

func TestExtractionCorrectionMemoryFailureRetriesWithoutFalseCheckpointOrDuplicate(t *testing.T) {
	for _, ambiguousSuccess := range []bool{false, true} {
		name := "store_failure"
		if ambiguousSuccess {
			name = "ambiguous_success"
		}
		t.Run(name, func(t *testing.T) {
			repo, extraction, svc, _ := newExtractionCorrectionFixture(t, true)
			if ambiguousSuccess {
				repo.lessonFailAfter = 1
			} else {
				repo.lessonFailBefore = 1
			}
			newSummary := "corrected summary"
			view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-memory-retry-"+name)
			id, err := uuid.Parse(view.ID)
			if err != nil {
				t.Fatalf("parse correction ID: %v", err)
			}
			if err := runTestCorrection(svc, repo, id, 0); err == nil {
				t.Fatal("first memory write attempt should fail and remain retryable")
			} else {
				var classified durablejob.RetryClassification
				if !errors.As(err, &classified) || !classified.Retryable() {
					t.Fatalf("memory failure classification = %T %v, want retryable", err, err)
				}
			}
			pending, err := svc.ExtractionCorrectionForOwner("alice", id)
			if err != nil || pending.Status != models.SourceExtractionCorrectionPending || pending.Phase != correctionPhaseGraphProjected || !pending.RecoveryPending {
				t.Fatalf("memory failure checkpoint = %#v, err=%v; want graph_projected pending", pending, err)
			}
			wantBeforeRetry := 0
			if ambiguousSuccess {
				wantBeforeRetry = 1
			}
			if len(repo.correctionLessons) != wantBeforeRetry {
				t.Fatalf("persisted lessons before retry = %d, want %d", len(repo.correctionLessons), wantBeforeRetry)
			}
			if err := runTestCorrection(svc, repo, id, 1); err != nil {
				t.Fatalf("retry after memory store recovery: %v", err)
			}
			completed, err := svc.ExtractionCorrectionForOwner("alice", id)
			if err != nil || completed.Status != models.SourceExtractionCorrectionCompleted || completed.Phase != correctionPhaseCompleted || completed.RecoveryPending {
				t.Fatalf("completed correction = %#v, err=%v", completed, err)
			}
			if len(repo.correctionLessons) != 1 || len(svc.memoryService.(*extractionCorrectionMemoryService).records) != 0 {
				t.Fatalf("repository lessons = %d generic memory writes = %d, want one exact lesson and no generic write",
					len(repo.correctionLessons), len(svc.memoryService.(*extractionCorrectionMemoryService).records))
			}
			for key, lesson := range repo.correctionLessons {
				if key.owner != "alice" || key.sourceURI != "source-extraction://"+extraction.ID.String() ||
					lesson.SourceExtractionID == nil || *lesson.SourceExtractionID != extraction.ID ||
					lesson.SourceURI != "source-extraction://"+extraction.ID.String() {
					t.Fatalf("sensitive correction lesson provenance = key %#v lesson %#v", key, lesson)
				}
			}
		})
	}
}

func TestExtractionCorrectionLessonPersistenceRejectsStaleLeaseAndRevisionThenUpsertsByExtraction(t *testing.T) {
	repo, extraction, _, _ := newExtractionCorrectionFixture(t, true)
	extraction.SourceURI = "https://evidence.example/mail/938"
	extraction.Sensitive = false
	extraction.Uncertain = false
	repo.extractions[extraction.ID].SourceURI = extraction.SourceURI
	repo.extractions[extraction.ID].Sensitive = false
	repo.extractions[extraction.ID].Uncertain = false
	newSummary := "corrected evidence summary"
	svc := NewService(repo, nil).(*service)
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-lesson-lease-idempotency")
	correctionID, _ := uuid.Parse(view.ID)
	correction := repo.corrections[correctionID]
	current := repo.extractions[extraction.ID]
	current.Summary = newSummary
	current.UpdatedAt = extraction.UpdatedAt.Add(time.Microsecond)
	correction.AppliedRevision = &current.UpdatedAt
	correction.Phase = correctionPhaseGraphProjected
	job := repo.claim(correctionID, 0)
	source, err := repo.FindSource(correction.SourceID)
	if err != nil {
		t.Fatalf("load correction source: %v", err)
	}
	var before extractionCorrectionBeforeState
	if err := json.Unmarshal([]byte(correction.BeforeStateJSON), &before); err != nil {
		t.Fatalf("decode before-state: %v", err)
	}
	request := extractionCorrectionMemoryRequest(source, before.extraction(), current)
	internalKey := "source-extraction://" + extraction.ID.String()

	if _, err := repo.PersistExtractionCorrectionLesson(context.Background(), correctionID, job.ID, job.LockedBy,
		job.LeaseGeneration+1, current.UpdatedAt, "alice", internalKey, request); !errors.Is(err, errExtractionCorrectionLeaseLost) {
		t.Fatalf("stale lease persistence error = %v, want lease lost", err)
	}
	if _, err := repo.PersistExtractionCorrectionLesson(context.Background(), correctionID, job.ID, job.LockedBy,
		job.LeaseGeneration, current.UpdatedAt.Add(time.Second), "alice", internalKey, request); !errors.Is(err, ErrExtractionCorrectionNotFound) {
		t.Fatalf("stale revision persistence error = %v, want correction state rejection", err)
	}
	if len(repo.correctionLessons) != 0 {
		t.Fatalf("stale persistence attempts created %d lessons", len(repo.correctionLessons))
	}

	first, err := repo.PersistExtractionCorrectionLesson(context.Background(), correctionID, job.ID, job.LockedBy,
		job.LeaseGeneration, current.UpdatedAt, "alice", internalKey, request)
	if err != nil {
		t.Fatalf("persist correction lesson: %v", err)
	}
	second, err := repo.PersistExtractionCorrectionLesson(context.Background(), correctionID, job.ID, job.LockedBy,
		job.LeaseGeneration, current.UpdatedAt, "alice", internalKey, request)
	if err != nil {
		t.Fatalf("retry correction lesson: %v", err)
	}
	if len(repo.correctionLessons) != 1 || first.ID != second.ID || first.SourceURI != extraction.SourceURI ||
		second.SourceURI != extraction.SourceURI || first.SourceExtractionID == nil || second.SourceExtractionID == nil ||
		*first.SourceExtractionID != extraction.ID || *second.SourceExtractionID != extraction.ID {
		t.Fatalf("idempotent correction lesson count/provenance = %d, %#v / %#v", len(repo.correctionLessons), first, second)
	}
}

func TestExtractionCorrectionStopsAfterBoundedRetryBudget(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	workflowStub.intakeErr = errors.New("temporary workflow outage")
	newSummary := "after"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &newSummary}, "correction-key-retry-budget")
	id, _ := uuid.Parse(view.ID)
	job := repo.correctionJobs[repo.corrections[id].DurableJobID]
	job.MaxAttempts = 2
	if err := runTestCorrection(svc, repo, id, 0); err == nil {
		t.Fatal("first failed execution should be retryable")
	} else if classified, ok := err.(durablejob.RetryClassification); !ok || !classified.Retryable() {
		t.Fatalf("first error must be retryable, got %T: %v", err, err)
	}
	if err := runTestCorrection(svc, repo, id, 1); err == nil {
		t.Fatal("second failed execution should become terminal")
	} else if classified, ok := err.(durablejob.RetryClassification); !ok || classified.Retryable() {
		t.Fatalf("retry-exhaustion error must be permanent, got %T: %v", err, err)
	}
	result, err := svc.ExtractionCorrectionForOwner("alice", id)
	if err != nil || result.Status != models.SourceExtractionCorrectionFailed || !result.PatchSaved || result.RecoveryPending || result.Attempts != 2 {
		t.Fatalf("terminal retry state = %#v, err=%v", result, err)
	}
}

func TestExtractionCorrectionResubmissionRecoversAfterAppliedPatchFailure(t *testing.T) {
	repo, extraction, svc, workflowStub := newExtractionCorrectionFixture(t, true)
	workflowStub.intakeErr = errors.New("workflow projection unavailable")
	newSummary := "corrected summary"
	patch := ExtractionPatch{Summary: &newSummary}
	first, err := svc.SubmitExtractionCorrection("alice", extraction.ID, extraction.UpdatedAt, patch, "correction-recovery-original-key")
	if err != nil {
		t.Fatalf("submit original correction: %v", err)
	}
	firstID, err := uuid.Parse(first.ID)
	if err != nil {
		t.Fatalf("parse original correction ID: %v", err)
	}
	original := repo.corrections[firstID]
	originalSnapshot := original.BeforeStateJSON
	repo.correctionJobs[original.DurableJobID].MaxAttempts = 1
	if err := runTestCorrection(svc, repo, firstID, 0); err == nil {
		t.Fatal("downstream outage should exhaust the one-attempt test budget")
	} else {
		var classified durablejob.RetryClassification
		if !errors.As(err, &classified) || classified.Retryable() {
			t.Fatalf("exhausted downstream error = %T %v, want terminal", err, err)
		}
	}

	failed := repo.corrections[firstID]
	current := repo.extractions[extraction.ID]
	if failed.Status != models.SourceExtractionCorrectionFailed || failed.Phase != correctionPhasePatchApplied ||
		failed.AppliedRevision == nil || !failed.AppliedRevision.Equal(current.UpdatedAt) || current.Summary != newSummary {
		t.Fatalf("terminal correction lost its applied checkpoint: correction=%#v extraction=%#v", failed, current)
	}
	failedRevision := current.UpdatedAt
	workflowStub.intakeErr = nil

	recovery, err := svc.SubmitExtractionCorrection("alice", extraction.ID, failedRevision, patch, "correction-recovery-new-key")
	if err != nil {
		t.Fatalf("submit recovery with current revision and a new idempotency key: %v", err)
	}
	recoveryID, err := uuid.Parse(recovery.ID)
	if err != nil {
		t.Fatalf("parse recovery correction ID: %v", err)
	}
	if recoveryID == firstID || recovery.Status != models.SourceExtractionCorrectionPending ||
		!recovery.IntentPersisted || !recovery.PatchSaved || !recovery.RecoveryPending ||
		recovery.Phase != correctionPhasePatchApplied {
		t.Fatalf("recovery submission state = %#v; want distinct pending downstream recovery at patch_applied", recovery)
	}
	recoveryRecord := repo.corrections[recoveryID]
	if recoveryRecord.BeforeStateJSON != originalSnapshot || recoveryRecord.AppliedRevision == nil ||
		!recoveryRecord.AppliedRevision.Equal(failedRevision) || !recoveryRecord.ExpectedRevision.Equal(failedRevision) {
		t.Fatalf("recovery did not retain immutable before-state/current applied revision: %#v", recoveryRecord)
	}
	if len(repo.corrections) != 2 {
		t.Fatalf("correction records after recovery submission = %d, want original plus one recovery intent", len(repo.corrections))
	}

	if err := runTestCorrection(svc, repo, recoveryID, 0); err != nil {
		t.Fatalf("run downstream recovery: %v", err)
	}
	completed, err := svc.ExtractionCorrectionForOwner("alice", recoveryID)
	if err != nil || completed.Status != models.SourceExtractionCorrectionCompleted ||
		!completed.PatchSaved || completed.RecoveryPending || completed.Phase != correctionPhaseCompleted {
		t.Fatalf("completed recovery = %#v, err=%v", completed, err)
	}
	if got := repo.extractions[extraction.ID]; got.Summary != newSummary || !got.UpdatedAt.Equal(failedRevision) {
		t.Fatalf("recovery reapplied or changed the source patch: summary=%q revision=%s want %s", got.Summary, got.UpdatedAt, failedRevision)
	}
	if repo.corrections[firstID].Status != models.SourceExtractionCorrectionFailed ||
		workflowStub.intakeCalls != 2 || len(workflowStub.effectKeys) != 1 {
		t.Fatalf("recovery must retain the failed attempt and replay the idempotent projection once: prior=%q calls=%d effects=%d",
			repo.corrections[firstID].Status, workflowStub.intakeCalls, len(workflowStub.effectKeys))
	}

	// Timestamps are not a reliable ordering signal for competing terminal rows:
	// databases can persist both within the same clock tick. Completion must win.
	originalRecord := repo.corrections[firstID]
	completedRecord := repo.corrections[recoveryID]
	originalRecord.UpdatedAt = completedRecord.UpdatedAt
	originalRecord.CreatedAt = completedRecord.CreatedAt
	duplicate, err := svc.SubmitExtractionCorrection("alice", extraction.ID, failedRevision, patch, "correction-recovery-after-completion")
	if err != nil || duplicate.ID != recovery.ID || duplicate.Status != models.SourceExtractionCorrectionCompleted || len(repo.corrections) != 2 {
		t.Fatalf("completed patch resubmission = %#v err=%v; must not create another recovery attempt", duplicate, err)
	}
}

func TestExtractionCorrectionHandlerRespectsBackgroundSafetyGate(t *testing.T) {
	repo, extraction, svc, _ := newExtractionCorrectionFixture(t, false)
	value := "after"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Summary: &value}, "correction-key-safe-gate")
	id, _ := uuid.Parse(view.ID)
	durable := repo.claim(id, 0)
	var payload extractionCorrectionJobPayload
	if err := json.Unmarshal([]byte(durable.Payload), &payload); err != nil {
		t.Fatalf("decode queued correction payload: %v", err)
	}
	durable.Status = models.DurableJobPending
	durable.LockedBy = ""
	durable.LockedAt = nil
	repo.correctionJobs[repo.corrections[id].DurableJobID].Status = models.DurableJobPending
	repo.correctionJobs[repo.corrections[id].DurableJobID].LockedBy = ""
	repo.correctionJobs[repo.corrections[id].DurableJobID].LockedAt = nil
	if err := extractionCorrectionJobHandler(svc, func() bool { return false })(context.Background(), durable); err == nil {
		t.Fatal("stopped background processing should defer the correction worker")
	}
	if repo.extractions[extraction.ID].Summary != "before" {
		t.Fatal("safety gate allowed a source correction side effect")
	}
	if status, err := svc.ExtractionCorrectionForOwner("alice", id); err != nil || status.Status != models.SourceExtractionCorrectionPending {
		t.Fatalf("gated correction should remain recoverable: status=%#v err=%v", status, err)
	}
}

func TestExtractionCorrectionTextOnlyChangeSnapshotsAndTeaches(t *testing.T) {
	repo, extraction, svc, _ := newExtractionCorrectionFixture(t, false)
	memorySpy := &extractionCorrectionLessonIndexerSpy{fakeSourceMemoryService: &fakeSourceMemoryService{}}
	svc.memoryService = memorySpy
	originalText := "Original connected-source prose with a corrected interpretation."
	correctedText := "Robert's corrected connected-source prose."
	repo.extractions[extraction.ID].Text = originalText
	repo.extractions[extraction.ID].Sensitive = false
	repo.extractions[extraction.ID].Uncertain = false
	repo.extractions[extraction.ID].SourceURI = "https://evidence.example/messages/42"
	extraction.Text = originalText
	extraction.Sensitive = false
	extraction.Uncertain = false
	extraction.SourceURI = "https://evidence.example/messages/42"
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Text: &correctedText}, "correction-text-only-snapshot")
	id, err := uuid.Parse(view.ID)
	if err != nil {
		t.Fatalf("parse correction ID: %v", err)
	}
	var snapshot extractionCorrectionBeforeState
	if err := json.Unmarshal([]byte(repo.corrections[id].BeforeStateJSON), &snapshot); err != nil {
		t.Fatalf("decode immutable before snapshot: %v", err)
	}
	if snapshot.Text != originalText || snapshot.extraction().Text != originalText {
		t.Fatalf("before snapshot text = %q / %q, want %q", snapshot.Text, snapshot.extraction().Text, originalText)
	}
	if err := runTestCorrection(svc, repo, id, 0); err != nil {
		t.Fatalf("run text-only correction: %v", err)
	}
	if len(repo.correctionLessons) != 1 || len(memorySpy.ownerCreated) != 0 || len(memorySpy.indexed) != 1 {
		t.Fatalf("exact correction lessons = %d generic memory writes = %d, want one exact lesson and no generic write",
			len(repo.correctionLessons), len(memorySpy.ownerCreated))
	}
	var lesson *models.ContextMemory
	for _, saved := range repo.correctionLessons {
		lesson = saved
	}
	if !strings.Contains(lesson.Content, "Changed fields: text.") ||
		!strings.Contains(lesson.Content, "Previous text: "+originalText) ||
		!strings.Contains(lesson.Content, "Revised text: "+correctedText) {
		t.Fatalf("text-only correction lesson is incomplete: %#v", lesson)
	}
	if lesson.SourceURI != "https://evidence.example/messages/42" || lesson.SourceExtractionID == nil || *lesson.SourceExtractionID != extraction.ID {
		t.Fatalf("safe external evidence provenance was not preserved: %#v", lesson)
	}
	if memorySpy.indexed[0].ID != lesson.ID || memorySpy.indexed[0].SourceURI != lesson.SourceURI ||
		memorySpy.indexed[0].SourceExtractionID == nil || *memorySpy.indexed[0].SourceExtractionID != extraction.ID {
		t.Fatalf("post-commit indexer did not receive persisted evidence record: %#v", memorySpy.indexed[0])
	}
}

func TestExtractionCorrectionSensitiveToNonsensitiveTransitionNeverCopiesSourceIntoMemory(t *testing.T) {
	repo, extraction, svc, _ := newExtractionCorrectionFixture(t, false)
	memorySpy := &fakeSourceMemoryService{}
	svc.memoryService = memorySpy
	privateValues := []string{
		"private-sensitive-source-text",
		"private-sensitive-source-summary",
		"private-sensitive-source-task",
		"private-sensitive-source-follow-up",
	}
	stored := repo.extractions[extraction.ID]
	stored.Text, extraction.Text = privateValues[0], privateValues[0]
	stored.Summary, extraction.Summary = privateValues[1], privateValues[1]
	stored.Tasks, extraction.Tasks = privateValues[2], privateValues[2]
	stored.FollowUps, extraction.FollowUps = privateValues[3], privateValues[3]
	stored.Sensitive, extraction.Sensitive = true, true
	markNonsensitive := false
	view := submitTestCorrection(t, svc, extraction, ExtractionPatch{Sensitive: &markNonsensitive}, "correction-sensitive-toggle")
	id, err := uuid.Parse(view.ID)
	if err != nil {
		t.Fatalf("parse correction ID: %v", err)
	}
	if err := runTestCorrection(svc, repo, id, 0); err != nil {
		t.Fatalf("run sensitivity correction: %v", err)
	}
	if len(repo.correctionLessons) != 1 || len(memorySpy.ownerCreated) != 0 {
		t.Fatalf("exact correction lessons = %d generic memory writes = %d, want one redacted lesson and no generic write",
			len(repo.correctionLessons), len(memorySpy.ownerCreated))
	}
	var lesson *models.ContextMemory
	for _, saved := range repo.correctionLessons {
		lesson = saved
	}
	lessonText := strings.Join([]string{lesson.Content, lesson.Summary, lesson.SourceLabel, lesson.SourceURI, lesson.Tags}, " ")
	for _, privateValue := range privateValues {
		if strings.Contains(lessonText, privateValue) {
			t.Fatalf("sensitive source value %q leaked into lesson: %#v", privateValue, lesson)
		}
	}
	if !strings.Contains(lesson.Content, "corrected a sensitive connected-source extraction") ||
		!strings.Contains(lesson.Summary, "Sensitive source correction requires review") {
		t.Fatalf("sensitivity transition did not produce the safe generic lesson: %#v", lesson)
	}
	if lesson.SourceURI != "source-extraction://"+extraction.ID.String() || lesson.SourceExtractionID == nil || *lesson.SourceExtractionID != extraction.ID {
		t.Fatalf("sensitive correction must use canonical redacted provenance and extraction identity: %#v", lesson)
	}
}
