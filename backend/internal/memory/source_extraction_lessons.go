package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SourceExtractionLessonService is an optional capability. Keeping it separate
// from Service preserves existing fakes and trusted worker implementations.
type SourceExtractionLessonService interface {
	PersistSourceExtractionLessonForOwner(ownerIdentity, sourceURI string, request CreateRequest) (*models.ContextMemory, error)
}

// PersistedSourceExtractionLessonIndexer is an optional capability for
// semantic indexing of a source lesson that has already been persisted by a
// caller-owned, lease-validated transaction. Implementations must not write
// or upsert the context-memory row again.
type PersistedSourceExtractionLessonIndexer interface {
	IndexPersistedSourceExtractionLesson(memory *models.ContextMemory)
}

// SourceExtractionCorrectionAuditMessage binds a manual correction audit to
// the exact extraction revision and generated lesson content.
func SourceExtractionCorrectionAuditMessage(extractionID uuid.UUID, revision time.Time, lessonContent string) string {
	contentDigest := sha256.Sum256([]byte(lessonContent))
	return fmt.Sprintf("operator corrected extraction id=%s revision=%s lesson_sha256=%s",
		extractionID, revision.UTC().Format(time.RFC3339Nano), hex.EncodeToString(contentDigest[:]))
}

// IndexPersistedSourceExtractionLesson invokes optional best-effort semantic
// indexing for an already-persisted source lesson. Callers should use it only
// after the persistence transaction succeeds; it never writes the memory row.
func IndexPersistedSourceExtractionLesson(service Service, memory *models.ContextMemory) {
	if service == nil || memory == nil {
		return
	}
	indexer, ok := service.(PersistedSourceExtractionLessonIndexer)
	if !ok {
		return
	}
	indexer.IndexPersistedSourceExtractionLesson(memory)
}

func (s *service) IndexPersistedSourceExtractionLesson(memory *models.ContextMemory) {
	if s == nil || memory == nil {
		return
	}
	s.indexMemory(memory)
}

// SourceExtractionLessonRepository stores correction lessons without the
// generic Create path's exact-content or fuzzy-content merge behavior.
type SourceExtractionLessonRepository interface {
	UpsertSourceExtractionLesson(ownerIdentity, sourceURI string, request CreateRequest) (*models.ContextMemory, error)
}

// PersistSourceExtractionLessonForOwner invokes the optional exact-provenance
// capability or fails closed when the configured memory implementation lacks it.
func PersistSourceExtractionLessonForOwner(service Service, ownerIdentity, sourceURI string, request CreateRequest) (*models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if _, err := canonicalSourceExtractionID(sourceURI); err != nil {
		return nil, err
	}
	if err := validateSourceExtractionLessonRequest(request); err != nil {
		return nil, err
	}
	if service == nil {
		return nil, errors.New("memory service is unavailable")
	}
	capability, ok := service.(SourceExtractionLessonService)
	if !ok {
		return nil, errors.New("exact-provenance source-extraction lesson persistence is unavailable")
	}
	return capability.PersistSourceExtractionLessonForOwner(ownerIdentity, sourceURI, request)
}

func (s *service) PersistSourceExtractionLessonForOwner(ownerIdentity, sourceURI string, request CreateRequest) (*models.ContextMemory, error) {
	if s == nil {
		return nil, errors.New("memory service is unavailable")
	}
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if _, err := canonicalSourceExtractionID(sourceURI); err != nil {
		return nil, err
	}
	if err := validateSourceExtractionLessonRequest(request); err != nil {
		return nil, err
	}
	store, ok := s.repo.(SourceExtractionLessonRepository)
	if !ok {
		return nil, errors.New("repository does not support exact-provenance source-extraction lessons")
	}
	saved, err := store.UpsertSourceExtractionLesson(ownerIdentity, sourceURI, request)
	if err != nil {
		return nil, err
	}
	s.indexMemory(saved)
	return saved, nil
}

// UpsertSourceExtractionLesson serializes writes under a memory-specific
// advisory key, separate from the source correction worker's session fence.
func (r *GormRepository) UpsertSourceExtractionLesson(ownerIdentity, sourceURI string, request CreateRequest) (*models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if _, err := canonicalSourceExtractionID(sourceURI); err != nil {
		return nil, err
	}
	if err := validateSourceExtractionLessonRequest(request); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil || r.DB.Dialector == nil {
		return nil, errors.New("source-extraction lesson repository is unavailable")
	}
	if r.DB.Dialector.Name() != "postgres" {
		return nil, errors.New("source-extraction lesson upsert requires PostgreSQL advisory locks")
	}

	var saved *models.ContextMemory
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		saved, err = PersistSourceExtractionLessonTx(tx, ownerIdentity, sourceURI, request)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("persist source-extraction lesson: %w", err)
	}
	return saved, nil
}

// PersistSourceExtractionLessonTx upserts one exact-provenance lesson using
// the caller's transaction. Worker callers must already hold the source
// correction fence and validate their durable lease/revision. Direct owner-
// scoped upserts are authorized only by an exact, content-bound audit record.
// Its advisory lock uses a separate memory namespace.
func PersistSourceExtractionLessonTx(tx *gorm.DB, ownerIdentity, sourceURI string, request CreateRequest) (*models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	extractionID, err := canonicalSourceExtractionID(sourceURI)
	if err != nil {
		return nil, err
	}
	if tx == nil || tx.Dialector == nil || tx.Dialector.Name() != "postgres" {
		return nil, errors.New("source-extraction lesson transaction requires PostgreSQL")
	}
	if err := validateSourceExtractionLessonRequest(request); err != nil {
		return nil, err
	}
	if err := validateSourceExtractionLessonEvidence(tx, ownerIdentity, extractionID, request, sourceURI); err != nil {
		return nil, err
	}
	if err := lockSourceExtractionMemoryKey(tx, ownerIdentity, sourceURI); err != nil {
		return nil, err
	}
	lesson := sourceExtractionLesson(ownerIdentity, extractionID, request)

	var existing []models.ContextMemory
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("owner_identity = ? AND source_extraction_id = ? AND kind = ?", ownerIdentity, extractionID, lesson.Kind).
		Order("id").Limit(2).Find(&existing).Error; err != nil {
		return nil, fmt.Errorf("find exact-provenance correction lesson: %w", err)
	}
	if len(existing) > 1 {
		return nil, errors.New("multiple correction lessons already share this owner, extraction ID, and kind; refusing an ambiguous update")
	}
	if len(existing) == 1 {
		prior := existing[0]
		if prior.ProjectKey != lesson.ProjectKey || prior.Content != lesson.Content || prior.Summary != lesson.Summary ||
			prior.Tags != lesson.Tags || prior.SourceURI != lesson.SourceURI || prior.SourceLabel != lesson.SourceLabel ||
			prior.Confidence != lesson.Confidence {
			return nil, errors.New("a different correction lesson already exists for this owner and source revision; review the conflict before replacing it")
		}
		return &prior, nil
	}
	if err := tx.Create(&lesson).Error; err != nil {
		return nil, fmt.Errorf("create exact-provenance correction lesson: %w", err)
	}
	return &lesson, nil
}

// DeleteSourceExtractionLessons runs exact owner/provenance memory and vector
// cleanup under the memory-specific PostgreSQL advisory lock.
func DeleteSourceExtractionLessons(db *gorm.DB, ownerIdentity, sourceURI string) (int64, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return 0, err
	}
	if _, err := canonicalSourceExtractionID(sourceURI); err != nil {
		return 0, err
	}
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return 0, errors.New("source-extraction lesson cleanup requires PostgreSQL")
	}
	var deleted int64
	err = db.Transaction(func(tx *gorm.DB) error {
		var err error
		deleted, err = DeleteSourceExtractionLessonsTx(tx, ownerIdentity, sourceURI)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete source-extraction correction lessons: %w", err)
	}
	return deleted, nil
}

// DeleteSourceExtractionLessonsTx removes only rows for the exact owner and
// parsed SourceExtractionID, along with their semantic vectors in the same caller
// transaction. The caller must hold the same correction worker fence so a
// stale worker cannot recreate a lesson after this transaction commits.
func DeleteSourceExtractionLessonsTx(tx *gorm.DB, ownerIdentity, sourceURI string) (int64, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return 0, err
	}
	extractionID, err := canonicalSourceExtractionID(sourceURI)
	if err != nil {
		return 0, err
	}
	if tx == nil || tx.Dialector == nil || tx.Dialector.Name() != "postgres" {
		return 0, errors.New("source-extraction lesson cleanup transaction requires PostgreSQL")
	}
	var deleted int64
	err = tx.Transaction(func(tx *gorm.DB) error {
		if err := lockSourceExtractionMemoryKey(tx, ownerIdentity, sourceURI); err != nil {
			return err
		}
		var memories []models.ContextMemory
		if err := tx.Model(&models.ContextMemory{}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_identity = ? AND source_extraction_id = ?", ownerIdentity, extractionID).
			Order("id").Select("id").Find(&memories).Error; err != nil {
			return fmt.Errorf("select exact-provenance correction lessons: %w", err)
		}
		if len(memories) == 0 {
			return nil
		}

		ids := make([]uuid.UUID, 0, len(memories))
		for _, memory := range memories {
			ids = append(ids, memory.ID)
		}
		var embeddingsExist bool
		if err := tx.Raw("SELECT to_regclass('semantic_memory_embeddings') IS NOT NULL").Scan(&embeddingsExist).Error; err != nil {
			return fmt.Errorf("check semantic memory embedding table: %w", err)
		}
		if embeddingsExist {
			if err := tx.Exec("DELETE FROM semantic_memory_embeddings WHERE memory_id IN ?", ids).Error; err != nil {
				return fmt.Errorf("delete semantic memory embeddings: %w", err)
			}
		}

		result := tx.Where("id IN ? AND owner_identity = ? AND source_extraction_id = ?", ids, ownerIdentity, extractionID).
			Delete(&models.ContextMemory{})
		if result.Error != nil {
			return fmt.Errorf("delete exact-provenance correction lessons: %w", result.Error)
		}
		deleted = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func validateSourceExtractionLessonRequest(request CreateRequest) error {
	kind := strings.ToLower(strings.TrimSpace(request.Kind))
	if kind != "lesson" && kind != "correction_lesson" {
		return errors.New("source-extraction lesson kind must be lesson or correction_lesson")
	}
	if strings.TrimSpace(request.Content) == "" {
		return errors.New("correction lesson content is required")
	}
	if strings.TrimSpace(request.SourceURI) == "" {
		return errors.New("correction lesson source evidence URI is required")
	}
	if strings.TrimSpace(request.SourceLabel) == "" {
		return errors.New("correction lesson source evidence label is required")
	}
	return validateMemoryCreateRequest("source-extraction-owner", request)
}

func validateSourceExtractionLessonEvidence(tx *gorm.DB, ownerIdentity string, extractionID uuid.UUID, request CreateRequest, sourceURI string) error {
	// Read only to discover the stable source ID; lock source evidence in the
	// same source -> extraction -> raw-item order used by source deletion.
	var observed models.SourceExtraction
	if err := tx.Where("id = ? AND archived = ?", extractionID, false).First(&observed).Error; err != nil {
		return fmt.Errorf("current source extraction evidence is unavailable: %w", err)
	}
	if observed.SourceID == uuid.Nil || observed.UpdatedAt.IsZero() || observed.UpdatedAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return errors.New("current source extraction revision is incomplete or stale")
	}

	var source models.ConnectedSource
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND owner_identity = ?", observed.SourceID, ownerIdentity).
		First(&source).Error; err != nil {
		return fmt.Errorf("owner-bound source provenance is unavailable: %w", err)
	}
	if source.OwnerIdentity != ownerIdentity || !source.Enabled || source.RevokedAt != nil || source.Status != "active" {
		return errors.New("source is disabled, revoked, or not active for this owner")
	}

	var extraction models.SourceExtraction
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND source_id = ? AND archived = ?", extractionID, source.ID, false).
		First(&extraction).Error; err != nil {
		return fmt.Errorf("current source extraction evidence is unavailable: %w", err)
	}
	if extraction.ID != extractionID || extraction.SourceID != observed.SourceID || extraction.RawItemID == uuid.Nil ||
		!extraction.UpdatedAt.Equal(observed.UpdatedAt) || extraction.UpdatedAt.IsZero() ||
		extraction.UpdatedAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return errors.New("current source extraction revision changed or is incomplete")
	}
	if extraction.Uncertain && !extraction.Sensitive {
		return errors.New("uncertain source extraction cannot be promoted to a persistent lesson")
	}
	var raw models.SourceRawItem
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND source_id = ?", extraction.RawItemID, source.ID).
		First(&raw).Error; err != nil {
		return fmt.Errorf("durable raw source evidence is unavailable: %w", err)
	}
	if raw.FetchedAt.IsZero() || raw.FetchedAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return errors.New("raw source evidence has no valid fetch timestamp")
	}
	if raw.ProjectKey != "" && extraction.ProjectKey != "" && raw.ProjectKey != extraction.ProjectKey {
		return errors.New("raw source project conflicts with the current extraction")
	}
	if raw.SourceURI != "" && extraction.SourceURI != "" && raw.SourceURI != extraction.SourceURI {
		return errors.New("raw source URI conflicts with the current extraction")
	}
	if raw.ContentHash != "" && extraction.ContentHash != "" && raw.ContentHash != extraction.ContentHash {
		return errors.New("raw source content conflicts with the current extraction")
	}

	canonicalURI := "source-extraction://" + extractionID.String()
	allowedEvidenceURI := canonicalURI
	if !extraction.Sensitive {
		allowedEvidenceURI = safety.RedactURL(firstNonEmpty(strings.TrimSpace(extraction.SourceURI), canonicalURI))
	}
	if request.ProjectKey != extraction.ProjectKey ||
		strings.TrimSpace(request.SourceURI) != allowedEvidenceURI {
		return errors.New("correction lesson project or evidence URI does not match the current source extraction")
	}
	if extraction.Sensitive {
		const safeSensitiveLesson = "Robert corrected a sensitive connected-source extraction. Future behavior: keep similar records review-gated, avoid storing raw sensitive content as memory, and ask for confirmation before workflow, task, or memory use."
		if strings.TrimSpace(request.SourceURI) != canonicalURI || request.SourceLabel != "Sensitive connected-source correction" ||
			strings.TrimSpace(request.Content) != safeSensitiveLesson {
			return errors.New("sensitive extraction may only create the fixed non-content review lesson")
		}
	} else if strings.TrimSpace(request.SourceLabel) != firstNonEmpty(strings.TrimSpace(extraction.SourceLabel), "Corrected connected-source extraction") {
		return errors.New("correction lesson evidence label does not match the current source extraction")
	}

	// A durable correction intent proves that a correction ran, but does not
	// bind arbitrary lesson text to that correction. Require the same
	// content-bound audit for both synchronous and durable correction paths.
	var audit models.SourceAuditLog
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("source_id = ? AND action = ? AND message = ? AND created_at >= ?",
			source.ID, "extraction.corrected", SourceExtractionCorrectionAuditMessage(extraction.ID, extraction.UpdatedAt, request.Content), extraction.UpdatedAt).
		Order("created_at desc").First(&audit).Error; err != nil {
		return fmt.Errorf("explicit operator verification for this source revision is unavailable: %w", err)
	}
	return nil
}

func sourceExtractionLesson(ownerIdentity string, extractionID uuid.UUID, request CreateRequest) models.ContextMemory {
	kind := strings.TrimSpace(request.Kind)
	content := strings.TrimSpace(request.Content)
	projectKey := strings.TrimSpace(request.ProjectKey)
	return models.ContextMemory{
		ID:                 uuid.New(),
		OwnerIdentity:      ownerIdentity,
		ProjectKey:         projectKey,
		Kind:               kind,
		Content:            content,
		Summary:            compactSummary(firstNonEmpty(request.Summary, content)),
		Tags:               joinTags(request.Tags),
		Confidence:         normalizeConfidence(request.Confidence),
		SourceURI:          strings.TrimSpace(request.SourceURI),
		SourceExtractionID: &extractionID,
		SourceLabel:        strings.TrimSpace(request.SourceLabel),
		ContentHash:        hashContent(projectKey, kind, content),
	}
}

func canonicalSourceExtractionID(sourceURI string) (uuid.UUID, error) {
	const prefix = "source-extraction://"
	if !strings.HasPrefix(sourceURI, prefix) {
		return uuid.Nil, errors.New("source URI must be the exact canonical source-extraction://<uuid> value")
	}
	id, err := uuid.Parse(strings.TrimPrefix(sourceURI, prefix))
	if err != nil || sourceURI != prefix+id.String() {
		return uuid.Nil, errors.New("source URI must be the exact canonical source-extraction://<uuid> value")
	}
	return id, nil
}

func lockSourceExtractionMemoryKey(tx *gorm.DB, ownerIdentity, sourceURI string) error {
	id, err := canonicalSourceExtractionID(sourceURI)
	if err != nil {
		return err
	}
	key := sourceExtractionMemoryLockKey(ownerIdentity, id)
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
		return fmt.Errorf("acquire source-extraction memory fence: %w", err)
	}
	return nil
}

func sourceExtractionMemoryLockKey(ownerIdentity string, extractionID uuid.UUID) string {
	digest := sha256.Sum256([]byte(ownerIdentity + "\x00" + extractionID.String() + "\x00source-extraction-memory"))
	return hex.EncodeToString(digest[:])
}
