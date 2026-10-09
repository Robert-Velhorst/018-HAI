package frameworkregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultSelectionLimit  = 20
	maxSelectionLimit      = 200
	defaultHistoryLimit    = 20
	maxHistoryLimit        = 100
	maxRequestSummaryRunes = 512
	maxApprovalNoteRunes   = 1024
)

var (
	sensitiveAssignmentPattern = regexp.MustCompile(`(?i)\b(password|passwd|secret|api[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|token|authorization)\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;]+)`)
	bearerTokenPattern         = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	jwtPattern                 = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	querySecretPattern         = regexp.MustCompile(`(?i)([?&](?:api[-_]?key|access[-_]?token|refresh[-_]?token|token|password|secret)=)[^&\s]+`)
	whitespacePattern          = regexp.MustCompile(`\s+`)
)

// GormRepository persists Framework Registry state in canonical PostgreSQL.
type GormRepository struct {
	DB *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{DB: db}
}

func (r *GormRepository) ListPreferences(owner string) ([]Preference, error) {
	if strings.TrimSpace(owner) == "" {
		return []Preference{}, nil
	}
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}

	var rows []models.FrameworkPreference
	if err := r.DB.
		Where("owner_identity = ?", owner).
		Order("pinned DESC, framework_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]Preference, 0, len(rows))
	for _, row := range rows {
		preference, err := preferenceFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, preference)
	}
	return result, nil
}

func (r *GormRepository) UpsertPreference(owner string, preference Preference) (*Preference, error) {
	row, err := preferenceToModel(owner, preference)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row.UpdatedAt = now
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}

	err = r.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "owner_identity"},
			{Name: "framework_id"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"state":                  row.State,
			"pinned":                 row.Pinned,
			"maximum_autonomy_level": row.MaximumAutonomyLevel,
			"adaptations_json":       row.AdaptationsJSON,
			"updated_at":             row.UpdatedAt,
		}),
	}).Create(&row).Error
	if err != nil {
		return nil, err
	}

	var stored models.FrameworkPreference
	if err := r.DB.
		Where("owner_identity = ? AND framework_id = ?", row.OwnerIdentity, row.FrameworkID).
		First(&stored).Error; err != nil {
		return nil, err
	}
	result, err := preferenceFromModel(stored)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *GormRepository) CreateSelection(
	owner string,
	decision SelectionDecision,
	requestHash string,
	requestSummary string,
) error {
	row, err := selectionToModel(owner, decision, requestHash, requestSummary)
	if err != nil {
		return err
	}
	return r.DB.Create(&row).Error
}

func (r *GormRepository) GetSelection(
	ctx context.Context,
	owner,
	id string,
) (*SelectionDecision, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	selectionID, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("framework selection id must be a UUID")
	}
	db, cleanup, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var row models.FrameworkSelectionRecord
	if err := db.
		Where("owner_identity = ? AND id = ?", owner, selectionID).
		First(&row).Error; err != nil {
		return nil, err
	}
	decision, err := selectionFromModel(row)
	if err != nil {
		return nil, err
	}
	return &decision, nil
}

func (r *GormRepository) ListSelections(owner string, limit int) ([]SelectionDecision, error) {
	if strings.TrimSpace(owner) == "" {
		return []SelectionDecision{}, nil
	}
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	limit = boundedSelectionLimit(limit)

	var rows []models.FrameworkSelectionRecord
	if err := r.DB.
		Where("owner_identity = ?", owner).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]SelectionDecision, 0, len(rows))
	for _, row := range rows {
		decision, err := selectionFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, decision)
	}
	return result, nil
}

func (r *GormRepository) ListConstitutions(owner string) ([]Constitution, error) {
	return r.ListConstitutionsContext(context.Background(), owner)
}

func (r *GormRepository) ListConstitutionsContext(ctx context.Context, owner string) ([]Constitution, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Constitution lookup context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(owner) == "" {
		return []Constitution{}, nil
	}
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	db, cleanup, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var rows []models.RobertConstitutionVersion
	if err := db.
		Where("owner_identity = ?", owner).
		Order("version DESC, created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]Constitution, 0, len(rows))
	for _, row := range rows {
		constitution, err := constitutionFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, constitution)
	}
	return result, nil
}

func (r *GormRepository) ListConstitutionHistory(owner string, limit int) ([]Constitution, error) {
	if strings.TrimSpace(owner) == "" {
		return []Constitution{}, nil
	}
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	limit = boundedConstitutionHistoryFetchLimit(limit)

	var rows []models.RobertConstitutionVersion
	if err := r.DB.
		Where("owner_identity = ?", owner).
		Order("version DESC, created_at DESC, id ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]Constitution, 0, len(rows))
	for _, row := range rows {
		constitution, err := constitutionFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, constitution)
	}
	return result, nil
}

func (r *GormRepository) CreateConstitution(owner string, constitution Constitution) (*Constitution, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	constitution, err = normalizeNewConstitution(constitution)
	if err != nil {
		return nil, err
	}

	var stored models.RobertConstitutionVersion
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockConstitutionOwner(tx, owner); err != nil {
			return err
		}
		if constitution.Version <= 0 {
			var latest int
			if err := tx.Model(&models.RobertConstitutionVersion{}).
				Where("owner_identity = ?", owner).
				Select("COALESCE(MAX(version), 0)").
				Scan(&latest).Error; err != nil {
				return err
			}
			constitution.Version = latest + 1
		}
		row, err := constitutionToModel(owner, constitution)
		if err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		stored = row
		return nil
	})
	if err != nil {
		return nil, err
	}

	result, err := constitutionFromModel(stored)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *GormRepository) ActivateConstitution(
	owner string,
	id string,
	approvedBy string,
	approvalNote string,
	approvedAt time.Time,
) (*Constitution, error) {
	owner, constitutionID, approvedBy, approvalNote, approvedAt, err := normalizeActivation(
		owner,
		id,
		approvedBy,
		approvalNote,
		approvedAt,
	)
	if err != nil {
		return nil, err
	}

	var stored models.RobertConstitutionVersion
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockConstitutionOwner(tx, owner); err != nil {
			return err
		}

		var target models.RobertConstitutionVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_identity = ? AND id = ?", owner, constitutionID).
			First(&target).Error; err != nil {
			return err
		}
		if target.Status == ConstitutionActive {
			stored = target
			return nil
		}
		if target.Status != ConstitutionDraft {
			return fmt.Errorf("constitution %s is %s and cannot be activated", id, target.Status)
		}
		var active models.RobertConstitutionVersion
		activeResult := tx.
			Where("owner_identity = ? AND status = ?", owner, ConstitutionActive).
			First(&active)
		if activeResult.Error != nil && !errors.Is(activeResult.Error, gorm.ErrRecordNotFound) {
			return activeResult.Error
		}
		if activeResult.Error == nil {
			if target.BaseVersion != active.Version {
				return fmt.Errorf(
					"constitution %s is stale: base version %d does not match active version %d",
					id,
					target.BaseVersion,
					active.Version,
				)
			}
		} else if !validInitialConstitutionBase(target.Version, target.BaseVersion) {
			return fmt.Errorf(
				"constitution %s has invalid initial base version %d for version %d",
				id,
				target.BaseVersion,
				target.Version,
			)
		}

		if err := tx.Model(&models.RobertConstitutionVersion{}).
			Where("owner_identity = ? AND status = ?", owner, ConstitutionActive).
			Updates(map[string]interface{}{"status": ConstitutionSuperseded}).Error; err != nil {
			return err
		}

		result := tx.Model(&models.RobertConstitutionVersion{}).
			Where("owner_identity = ? AND id = ? AND status = ?", owner, constitutionID, ConstitutionDraft).
			Updates(map[string]interface{}{
				"status":        ConstitutionActive,
				"approved_by":   approvedBy,
				"approval_note": approvalNote,
				"approved_at":   approvedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("constitution %s activation lost its state precondition", id)
		}
		return tx.Where("owner_identity = ? AND id = ?", owner, constitutionID).First(&stored).Error
	})
	if err != nil {
		return nil, err
	}

	result, err := constitutionFromModel(stored)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// MemoryRepository mirrors the Postgres repository contract for deterministic
// service tests. Rows are owner-scoped and copied on read.
type MemoryRepository struct {
	mu            sync.RWMutex
	preferences   map[string]models.FrameworkPreference
	selections    map[string][]models.FrameworkSelectionRecord
	selectionIDs  map[uuid.UUID]struct{}
	constitutions map[string][]models.RobertConstitutionVersion
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		preferences:   map[string]models.FrameworkPreference{},
		selections:    map[string][]models.FrameworkSelectionRecord{},
		selectionIDs:  map[uuid.UUID]struct{}{},
		constitutions: map[string][]models.RobertConstitutionVersion{},
	}
}

func (r *MemoryRepository) ListPreferences(owner string) ([]Preference, error) {
	if strings.TrimSpace(owner) == "" {
		return []Preference{}, nil
	}
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Preference, 0)
	for _, row := range r.preferences {
		if row.OwnerIdentity != owner {
			continue
		}
		preference, err := preferenceFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, preference)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Pinned != result[j].Pinned {
			return result[i].Pinned
		}
		return result[i].FrameworkID < result[j].FrameworkID
	})
	return result, nil
}

func (r *MemoryRepository) UpsertPreference(owner string, preference Preference) (*Preference, error) {
	row, err := preferenceToModel(owner, preference)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()

	key := preferenceKey(row.OwnerIdentity, row.FrameworkID)
	if existing, ok := r.preferences[key]; ok {
		row.ID = existing.ID
		row.CreatedAt = existing.CreatedAt
	} else {
		row.ID = uuid.New()
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	r.preferences[key] = row

	result, err := preferenceFromModel(row)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *MemoryRepository) CreateSelection(
	owner string,
	decision SelectionDecision,
	requestHash string,
	requestSummary string,
) error {
	row, err := selectionToModel(owner, decision, requestHash, requestSummary)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	if _, exists := r.selectionIDs[row.ID]; exists {
		return fmt.Errorf("selection %s already exists", row.ID)
	}
	r.selectionIDs[row.ID] = struct{}{}
	r.selections[row.OwnerIdentity] = append(r.selections[row.OwnerIdentity], row)
	return nil
}

func (r *MemoryRepository) GetSelection(
	_ context.Context,
	owner,
	id string,
) (*SelectionDecision, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	selectionID, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("framework selection id must be a UUID")
	}

	r.mu.RLock()
	rows := append([]models.FrameworkSelectionRecord(nil), r.selections[owner]...)
	r.mu.RUnlock()
	for _, row := range rows {
		if row.ID != selectionID {
			continue
		}
		decision, err := selectionFromModel(row)
		if err != nil {
			return nil, err
		}
		return &decision, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *MemoryRepository) ListSelections(owner string, limit int) ([]SelectionDecision, error) {
	if strings.TrimSpace(owner) == "" {
		return []SelectionDecision{}, nil
	}
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	limit = boundedSelectionLimit(limit)

	r.mu.RLock()
	rows := append([]models.FrameworkSelectionRecord(nil), r.selections[owner]...)
	r.mu.RUnlock()
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID.String() > rows[j].ID.String()
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}

	result := make([]SelectionDecision, 0, len(rows))
	for _, row := range rows {
		decision, err := selectionFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, decision)
	}
	return result, nil
}

func (r *MemoryRepository) ListConstitutions(owner string) ([]Constitution, error) {
	return r.ListConstitutionsContext(context.Background(), owner)
}

func (r *MemoryRepository) ListConstitutionsContext(ctx context.Context, owner string) ([]Constitution, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Constitution lookup context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(owner) == "" {
		return []Constitution{}, nil
	}
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !r.mu.TryRLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
	rows := append([]models.RobertConstitutionVersion(nil), r.constitutions[owner]...)
	r.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Version != rows[j].Version {
			return rows[i].Version > rows[j].Version
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})

	result := make([]Constitution, 0, len(rows))
	for _, row := range rows {
		constitution, err := constitutionFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, constitution)
	}
	return result, nil
}

func (r *MemoryRepository) ListConstitutionHistory(owner string, limit int) ([]Constitution, error) {
	if strings.TrimSpace(owner) == "" {
		return []Constitution{}, nil
	}
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	limit = boundedConstitutionHistoryFetchLimit(limit)

	r.mu.RLock()
	rows := append([]models.RobertConstitutionVersion(nil), r.constitutions[owner]...)
	r.mu.RUnlock()
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Version != rows[j].Version {
			return rows[i].Version > rows[j].Version
		}
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].ID.String() < rows[j].ID.String()
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}

	result := make([]Constitution, 0, len(rows))
	for _, row := range rows {
		constitution, err := constitutionFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, constitution)
	}
	return result, nil
}

func (r *MemoryRepository) CreateConstitution(owner string, constitution Constitution) (*Constitution, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	constitution, err = normalizeNewConstitution(constitution)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()

	rows := r.constitutions[owner]
	if constitution.Version <= 0 {
		for _, row := range rows {
			if row.Version >= constitution.Version {
				constitution.Version = row.Version + 1
			}
		}
		if constitution.Version <= 0 {
			constitution.Version = 1
		}
	}
	for _, row := range rows {
		if row.Version == constitution.Version {
			return nil, fmt.Errorf("constitution version %d already exists for owner", constitution.Version)
		}
	}

	row, err := constitutionToModel(owner, constitution)
	if err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	r.constitutions[owner] = append(rows, row)
	result, err := constitutionFromModel(row)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *MemoryRepository) ActivateConstitution(
	owner string,
	id string,
	approvedBy string,
	approvalNote string,
	approvedAt time.Time,
) (*Constitution, error) {
	owner, constitutionID, approvedBy, approvalNote, approvedAt, err := normalizeActivation(
		owner,
		id,
		approvedBy,
		approvalNote,
		approvedAt,
	)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	rows := r.constitutions[owner]
	targetIndex := -1
	for index := range rows {
		if rows[index].ID == constitutionID {
			targetIndex = index
			break
		}
	}
	if targetIndex < 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if rows[targetIndex].Status == ConstitutionActive {
		result, err := constitutionFromModel(rows[targetIndex])
		if err != nil {
			return nil, err
		}
		return &result, nil
	}
	if rows[targetIndex].Status != ConstitutionDraft {
		return nil, fmt.Errorf("constitution %s is %s and cannot be activated", id, rows[targetIndex].Status)
	}
	activeVersion := 0
	for index := range rows {
		if rows[index].Status == ConstitutionActive {
			activeVersion = rows[index].Version
			break
		}
	}
	if activeVersion > 0 {
		if rows[targetIndex].BaseVersion != activeVersion {
			return nil, fmt.Errorf(
				"constitution %s is stale: base version %d does not match active version %d",
				id,
				rows[targetIndex].BaseVersion,
				activeVersion,
			)
		}
	} else if !validInitialConstitutionBase(
		rows[targetIndex].Version,
		rows[targetIndex].BaseVersion,
	) {
		return nil, fmt.Errorf(
			"constitution %s has invalid initial base version %d for version %d",
			id,
			rows[targetIndex].BaseVersion,
			rows[targetIndex].Version,
		)
	}

	for index := range rows {
		if rows[index].Status == ConstitutionActive {
			rows[index].Status = ConstitutionSuperseded
		}
	}
	rows[targetIndex].Status = ConstitutionActive
	rows[targetIndex].ApprovedBy = approvedBy
	rows[targetIndex].ApprovalNote = approvalNote
	rows[targetIndex].ApprovedAt = &approvedAt
	r.constitutions[owner] = rows

	result, err := constitutionFromModel(rows[targetIndex])
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *MemoryRepository) ensureInitialized() {
	if r.preferences == nil {
		r.preferences = map[string]models.FrameworkPreference{}
	}
	if r.selections == nil {
		r.selections = map[string][]models.FrameworkSelectionRecord{}
	}
	if r.selectionIDs == nil {
		r.selectionIDs = map[uuid.UUID]struct{}{}
	}
	if r.constitutions == nil {
		r.constitutions = map[string][]models.RobertConstitutionVersion{}
	}
}

func boundedConstitutionHistoryLimit(limit int) int {
	if limit <= 0 || limit > maxHistoryLimit {
		return defaultHistoryLimit
	}
	return limit
}

func boundedConstitutionHistoryFetchLimit(limit int) int {
	if limit <= 0 || limit > maxHistoryLimit+1 {
		return defaultHistoryLimit + 1
	}
	return limit
}

func preferenceToModel(owner string, preference Preference) (models.FrameworkPreference, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return models.FrameworkPreference{}, err
	}
	frameworkID, err := requiredIdentifier("framework id", preference.FrameworkID, 160)
	if err != nil {
		return models.FrameworkPreference{}, err
	}
	state := strings.TrimSpace(preference.State)
	if state == "" {
		state = PreferenceDefault
	}
	if state != PreferenceDefault && state != PreferenceEnabled && state != PreferenceDisabled {
		return models.FrameworkPreference{}, fmt.Errorf("invalid framework preference state %q", state)
	}
	if preference.MaximumAutonomyLevel != nil &&
		(*preference.MaximumAutonomyLevel < 0 || *preference.MaximumAutonomyLevel > 10) {
		return models.FrameworkPreference{}, fmt.Errorf("maximum autonomy level must be between 0 and 10")
	}
	adaptations, err := encodeJSONArray(preference.Adaptations)
	if err != nil {
		return models.FrameworkPreference{}, fmt.Errorf("encode preference adaptations: %w", err)
	}

	return models.FrameworkPreference{
		OwnerIdentity:        owner,
		FrameworkID:          frameworkID,
		State:                state,
		Pinned:               preference.Pinned,
		MaximumAutonomyLevel: cloneIntPointer(preference.MaximumAutonomyLevel),
		AdaptationsJSON:      adaptations,
	}, nil
}

func preferenceFromModel(row models.FrameworkPreference) (Preference, error) {
	adaptations, err := decodeJSONArray[string](row.AdaptationsJSON)
	if err != nil {
		return Preference{}, fmt.Errorf("decode preference adaptations: %w", err)
	}
	return Preference{
		FrameworkID:          row.FrameworkID,
		State:                row.State,
		Pinned:               row.Pinned,
		MaximumAutonomyLevel: cloneIntPointer(row.MaximumAutonomyLevel),
		Adaptations:          adaptations,
		UpdatedAt:            row.UpdatedAt,
	}, nil
}

func selectionToModel(
	owner string,
	decision SelectionDecision,
	requestHash string,
	requestSummary string,
) (models.FrameworkSelectionRecord, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	id, err := uuid.Parse(strings.TrimSpace(decision.ID))
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("selection id must be a UUID: %w", err)
	}
	requestHash, err = normalizeRequestHash(requestHash)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	requestSummary = compactRedactedText(requestSummary, maxRequestSummaryRunes)
	if requestSummary == "" {
		requestSummary = "[summary omitted]"
	}
	if decision.MaximumAutonomyLevel < 0 || decision.MaximumAutonomyLevel > 10 {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("maximum autonomy level must be between 0 and 10")
	}
	if decision.ConstitutionVersion < 0 {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("constitution version cannot be negative")
	}
	catalogVersion, err := requiredIdentifier("catalog version", decision.CatalogVersion, 32)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	catalogDigest, err := normalizeSHA256Digest("catalog digest", decision.CatalogDigest)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	selectorVersion, err := requiredIdentifier("selector algorithm version", decision.SelectorAlgorithmVersion, 64)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	taskRiskLevel, effectiveRiskCeiling, err := persistedSelectionRiskContract(
		selectorVersion,
		decision.TaskRiskLevel,
		decision.EffectiveRiskCeiling,
		decision.Selected,
	)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	preferenceDigest, err := normalizeSHA256Digest("effective preference digest", decision.EffectivePreferenceDigest)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	constitutionDigest, err := normalizeSHA256Digest("constitution digest", decision.ConstitutionDigest)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	constitutionSource, err := normalizeConstitutionSource(
		decision.ConstitutionSource,
		decision.ConstitutionVersion,
	)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}
	taskPlanID := strings.TrimSpace(decision.TaskPlanID)
	if len([]rune(taskPlanID)) > 160 {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("task plan id exceeds 160 characters")
	}
	if decision.CreatedAt.IsZero() {
		decision.CreatedAt = time.Now().UTC()
	} else {
		decision.CreatedAt = decision.CreatedAt.UTC()
	}

	selected, err := encodeJSONArray(decision.Selected)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode selected frameworks: %w", err)
	}
	conflicts, err := encodeJSONArray(decision.Conflicts)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode framework conflicts: %w", err)
	}
	requiredAgents, err := encodeJSONArray(decision.RequiredAgents)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode required agents: %w", err)
	}
	approvalReasons, err := encodeJSONArray(decision.ApprovalReasons)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode approval reasons: %w", err)
	}
	evidence, err := encodeJSONArray(decision.EvidenceRequirements)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode evidence requirements: %w", err)
	}
	completion, err := encodeJSONArray(decision.CompletionCriteria)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode completion criteria: %w", err)
	}
	learning, err := encodeJSONArray(decision.LearningPlan)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode learning plan: %w", err)
	}
	contextRequirements, err := encodeJSONArray(decision.ContextRequirements)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode context requirements: %w", err)
	}
	lifeDomains, err := encodeJSONArray(decision.LifeDomains)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode life domains: %w", err)
	}
	needsState, err := encodeJSONArray(decision.NeedsState)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode needs state: %w", err)
	}
	capacity, err := encodeJSONObject(decision.Capacity)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode capacity snapshot: %w", err)
	}
	agentCards, err := encodeJSONArray(decision.AgentCards)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode agent cards: %w", err)
	}
	delegations, err := encodeJSONArray(decision.Delegations)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode delegations: %w", err)
	}
	communication, err := encodeJSONObject(decision.Communication)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode communication contract: %w", err)
	}
	coordination, err := encodeJSONObject(decision.Coordination)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode coordination plan: %w", err)
	}
	actionAutonomy, err := encodeJSONArray(decision.ActionAutonomy)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode action autonomy: %w", err)
	}
	stopConditions, err := encodeJSONArray(decision.StopConditions)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode stop conditions: %w", err)
	}
	outcomeMonitoring, err := encodeJSONArray(decision.OutcomeMonitoring)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode outcome monitoring: %w", err)
	}
	chiefOfStaff, err := encodeJSONObject(decision.ChiefOfStaff)
	if err != nil {
		return models.FrameworkSelectionRecord{}, fmt.Errorf("encode chief-of-staff decision: %w", err)
	}
	operatingContractDigest, err := normalizeSHA256Digest(
		"operating contract digest",
		decision.OperatingContractDigest,
	)
	if err != nil {
		return models.FrameworkSelectionRecord{}, err
	}

	return models.FrameworkSelectionRecord{
		ID:                        id,
		OwnerIdentity:             owner,
		TaskPlanID:                taskPlanID,
		RequestHash:               requestHash,
		RequestSummary:            requestSummary,
		CatalogVersion:            catalogVersion,
		CatalogDigest:             catalogDigest,
		SelectorAlgorithmVersion:  selectorVersion,
		TaskRiskLevel:             taskRiskLevel,
		EffectiveRiskCeiling:      effectiveRiskCeiling,
		EffectivePreferenceDigest: preferenceDigest,
		ConstitutionDigest:        constitutionDigest,
		LifeDomain:                strings.TrimSpace(decision.LifeDomain),
		NeedOrCommitment:          strings.TrimSpace(decision.NeedOrCommitment),
		SelectedJSON:              selected,
		ConflictsJSON:             conflicts,
		RequiredAgentsJSON:        requiredAgents,
		MaximumAutonomyLevel:      decision.MaximumAutonomyLevel,
		AuthoritySummary:          strings.TrimSpace(decision.AuthoritySummary),
		RequiresApproval:          decision.RequiresApproval,
		ApprovalReasonsJSON:       approvalReasons,
		EvidenceRequirementsJSON:  evidence,
		CompletionCriteriaJSON:    completion,
		LearningPlanJSON:          learning,
		ContextRequirementsJSON:   contextRequirements,
		LifeDomainsJSON:           lifeDomains,
		NeedsStateJSON:            needsState,
		CapacityJSON:              capacity,
		AgentCardsJSON:            agentCards,
		DelegationsJSON:           delegations,
		CommunicationJSON:         communication,
		CoordinationJSON:          coordination,
		ActionAutonomyJSON:        actionAutonomy,
		StopConditionsJSON:        stopConditions,
		OutcomeMonitoringJSON:     outcomeMonitoring,
		ChiefOfStaffJSON:          chiefOfStaff,
		OperatingContractDigest:   operatingContractDigest,
		SelectionReason:           strings.TrimSpace(decision.SelectionReason),
		ConstitutionVersion:       decision.ConstitutionVersion,
		ConstitutionSource:        constitutionSource,
		CreatedAt:                 decision.CreatedAt,
	}, nil
}

func selectionFromModel(row models.FrameworkSelectionRecord) (SelectionDecision, error) {
	selected, err := decodeJSONArray[SelectedFramework](row.SelectedJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode selected frameworks: %w", err)
	}
	conflicts, err := decodeJSONArray[FrameworkConflict](row.ConflictsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode framework conflicts: %w", err)
	}
	requiredAgents, err := decodeJSONArray[string](row.RequiredAgentsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode required agents: %w", err)
	}
	approvalReasons, err := decodeJSONArray[string](row.ApprovalReasonsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode approval reasons: %w", err)
	}
	evidence, err := decodeJSONArray[string](row.EvidenceRequirementsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode evidence requirements: %w", err)
	}
	completion, err := decodeJSONArray[string](row.CompletionCriteriaJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode completion criteria: %w", err)
	}
	learning, err := decodeJSONArray[string](row.LearningPlanJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode learning plan: %w", err)
	}
	contextRequirements, err := decodeJSONArray[string](row.ContextRequirementsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode context requirements: %w", err)
	}
	lifeDomains, err := decodeJSONArray[LifeDomainAssignment](row.LifeDomainsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode life domains: %w", err)
	}
	needsState, err := decodeJSONArray[NeedStateAssessment](row.NeedsStateJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode needs state: %w", err)
	}
	capacity, err := decodeJSONObject[CapacitySnapshot](row.CapacityJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode capacity snapshot: %w", err)
	}
	agentCards, err := decodeJSONArray[AgentCard](row.AgentCardsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode agent cards: %w", err)
	}
	delegations, err := decodeJSONArray[DelegationContract](row.DelegationsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode delegations: %w", err)
	}
	communication, err := decodeJSONObject[CommunicationContract](row.CommunicationJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode communication contract: %w", err)
	}
	coordination, err := decodeJSONObject[CoordinationPlan](row.CoordinationJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode coordination plan: %w", err)
	}
	actionAutonomy, err := decodeJSONArray[ActionAutonomyDecision](row.ActionAutonomyJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode action autonomy: %w", err)
	}
	stopConditions, err := decodeJSONArray[string](row.StopConditionsJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode stop conditions: %w", err)
	}
	outcomeMonitoring, err := decodeJSONArray[string](row.OutcomeMonitoringJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode outcome monitoring: %w", err)
	}
	chiefOfStaff, err := decodeJSONObject[ChiefOfStaffDecision](row.ChiefOfStaffJSON)
	if err != nil {
		return SelectionDecision{}, fmt.Errorf("decode chief-of-staff decision: %w", err)
	}
	operatingContractDigest := strings.TrimSpace(row.OperatingContractDigest)
	if operatingContractDigest == strings.Repeat("0", sha256.Size*2) {
		// Rows written before selector-v4 keep their immutable audit history but
		// do not gain fabricated operating evidence from migration defaults.
		operatingContractDigest = ""
	}

	return SelectionDecision{
		ID:                        row.ID.String(),
		TaskPlanID:                row.TaskPlanID,
		CreatedAt:                 row.CreatedAt,
		CatalogVersion:            row.CatalogVersion,
		CatalogDigest:             row.CatalogDigest,
		SelectorAlgorithmVersion:  row.SelectorAlgorithmVersion,
		TaskRiskLevel:             dereferenceString(row.TaskRiskLevel),
		EffectiveRiskCeiling:      dereferenceString(row.EffectiveRiskCeiling),
		EffectivePreferenceDigest: row.EffectivePreferenceDigest,
		ConstitutionDigest:        row.ConstitutionDigest,
		LifeDomain:                row.LifeDomain,
		NeedOrCommitment:          row.NeedOrCommitment,
		Selected:                  selected,
		Conflicts:                 conflicts,
		RequiredAgents:            requiredAgents,
		MaximumAutonomyLevel:      row.MaximumAutonomyLevel,
		AuthoritySummary:          row.AuthoritySummary,
		RequiresApproval:          row.RequiresApproval,
		ApprovalReasons:           approvalReasons,
		EvidenceRequirements:      evidence,
		CompletionCriteria:        completion,
		LearningPlan:              learning,
		ContextRequirements:       contextRequirements,
		LifeDomains:               lifeDomains,
		NeedsState:                needsState,
		Capacity:                  capacity,
		AgentCards:                agentCards,
		Delegations:               delegations,
		Communication:             communication,
		Coordination:              coordination,
		ActionAutonomy:            actionAutonomy,
		StopConditions:            stopConditions,
		OutcomeMonitoring:         outcomeMonitoring,
		ChiefOfStaff:              chiefOfStaff,
		OperatingContractDigest:   operatingContractDigest,
		SelectionReason:           row.SelectionReason,
		ConstitutionVersion:       row.ConstitutionVersion,
		ConstitutionSource:        row.ConstitutionSource,
	}, nil
}

func persistedSelectionRiskContract(
	selectorVersion string,
	taskRiskLevel string,
	effectiveRiskCeiling string,
	selected []SelectedFramework,
) (*string, *string, error) {
	taskRiskLevel = normalizedRiskLevel(taskRiskLevel)
	effectiveRiskCeiling = normalizedRiskLevel(effectiveRiskCeiling)
	if selectorVersion != "selector-v5" {
		if taskRiskLevel == "" && effectiveRiskCeiling == "" {
			return nil, nil, nil
		}
	}
	taskRank, taskOK := selectionRiskRank(taskRiskLevel)
	ceilingRank, ceilingOK := selectionRiskRank(effectiveRiskCeiling)
	if !taskOK || !ceilingOK {
		return nil, nil, fmt.Errorf(
			"selector %q requires valid task risk and effective risk ceiling",
			selectorVersion,
		)
	}
	if taskRank > ceilingRank {
		return nil, nil, fmt.Errorf("task risk %q exceeds effective risk ceiling %q", taskRiskLevel, effectiveRiskCeiling)
	}
	minimumSelectedRank := 4
	for _, framework := range selected {
		frameworkRank, ok := selectionRiskRank(framework.RiskCeiling)
		if !ok {
			return nil, nil, fmt.Errorf(
				"selected framework %q has invalid risk ceiling %q",
				framework.ID,
				framework.RiskCeiling,
			)
		}
		if frameworkRank < taskRank {
			return nil, nil, fmt.Errorf(
				"selected framework %q risk ceiling %q is below task risk %q",
				framework.ID,
				framework.RiskCeiling,
				taskRiskLevel,
			)
		}
		if frameworkRank < minimumSelectedRank {
			minimumSelectedRank = frameworkRank
		}
	}
	if len(selected) == 0 || minimumSelectedRank != ceilingRank {
		return nil, nil, fmt.Errorf("effective risk ceiling does not match selected framework ceilings")
	}
	return stringPointer(taskRiskLevel), stringPointer(effectiveRiskCeiling), nil
}

func stringPointer(value string) *string {
	return &value
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func normalizeNewConstitution(constitution Constitution) (Constitution, error) {
	status := strings.TrimSpace(constitution.Status)
	if status == "" {
		status = ConstitutionDraft
	}
	if status != ConstitutionDraft {
		return Constitution{}, fmt.Errorf("new constitution versions must be drafts")
	}
	constitution.Status = status
	constitution.ApprovedBy = ""
	constitution.ApprovedAt = nil
	if constitution.CreatedAt.IsZero() {
		constitution.CreatedAt = time.Now().UTC()
	} else {
		constitution.CreatedAt = constitution.CreatedAt.UTC()
	}
	if constitution.Version < 0 {
		return Constitution{}, fmt.Errorf("constitution version cannot be negative")
	}
	if constitution.BaseVersion < 0 {
		return Constitution{}, fmt.Errorf("constitution base version cannot be negative")
	}
	if constitution.ID != "" {
		if _, err := uuid.Parse(strings.TrimSpace(constitution.ID)); err != nil {
			return Constitution{}, fmt.Errorf("constitution id must be a UUID: %w", err)
		}
	}
	if err := validateConstitutionDraft(constitution); err != nil {
		return Constitution{}, err
	}
	return constitution, nil
}

func constitutionToModel(owner string, constitution Constitution) (models.RobertConstitutionVersion, error) {
	id := uuid.New()
	if strings.TrimSpace(constitution.ID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(constitution.ID))
		if err != nil {
			return models.RobertConstitutionVersion{}, fmt.Errorf("constitution id must be a UUID: %w", err)
		}
		id = parsed
	}
	if constitution.Version <= 0 {
		return models.RobertConstitutionVersion{}, fmt.Errorf("constitution version must be positive")
	}
	if constitution.BaseVersion < 0 ||
		constitution.BaseVersion >= constitution.Version ||
		(constitution.BaseVersion == 0 && constitution.Version != 1) {
		return models.RobertConstitutionVersion{}, fmt.Errorf(
			"constitution base version %d is invalid for version %d",
			constitution.BaseVersion,
			constitution.Version,
		)
	}

	values, err := encodeJSONArray(constitution.Values)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode constitution values: %w", err)
	}
	prohibitions, err := encodeJSONArray(constitution.Prohibitions)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode constitution prohibitions: %w", err)
	}
	permissions, err := encodeJSONArray(constitution.StandingPermissions)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode standing permissions: %w", err)
	}
	preferences, err := encodeJSONArray(constitution.Preferences)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode constitution preferences: %w", err)
	}
	relationshipRules, err := encodeJSONArray(constitution.RelationshipRules)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode relationship rules: %w", err)
	}
	financialBoundaries, err := encodeJSONArray(constitution.FinancialBoundaries)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode financial boundaries: %w", err)
	}
	communicationRules, err := encodeJSONArray(constitution.CommunicationRules)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode communication rules: %w", err)
	}
	escalationRules, err := encodeJSONArray(constitution.EscalationRules)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode escalation rules: %w", err)
	}
	protectedRules, err := encodeJSONArray(constitution.ProtectedRules)
	if err != nil {
		return models.RobertConstitutionVersion{}, fmt.Errorf("encode protected rules: %w", err)
	}

	return models.RobertConstitutionVersion{
		ID:                      id,
		OwnerIdentity:           owner,
		Version:                 constitution.Version,
		BaseVersion:             constitution.BaseVersion,
		Status:                  constitution.Status,
		ValuesJSON:              values,
		ProhibitionsJSON:        prohibitions,
		StandingPermissionsJSON: permissions,
		PreferencesJSON:         preferences,
		RelationshipRulesJSON:   relationshipRules,
		FinancialBoundariesJSON: financialBoundaries,
		CommunicationRulesJSON:  communicationRules,
		EscalationRulesJSON:     escalationRules,
		ProtectedRulesJSON:      protectedRules,
		ChangeSummary:           compactRedactedText(constitution.ChangeSummary, maxApprovalNoteRunes),
		CreatedAt:               constitution.CreatedAt,
	}, nil
}

func constitutionFromModel(row models.RobertConstitutionVersion) (Constitution, error) {
	values, err := decodeJSONArray[string](row.ValuesJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode constitution values: %w", err)
	}
	prohibitions, err := decodeJSONArray[string](row.ProhibitionsJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode constitution prohibitions: %w", err)
	}
	permissions, err := decodeJSONArray[string](row.StandingPermissionsJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode standing permissions: %w", err)
	}
	preferences, err := decodeJSONArray[string](row.PreferencesJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode constitution preferences: %w", err)
	}
	relationshipRules, err := decodeJSONArray[string](row.RelationshipRulesJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode relationship rules: %w", err)
	}
	financialBoundaries, err := decodeJSONArray[string](row.FinancialBoundariesJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode financial boundaries: %w", err)
	}
	communicationRules, err := decodeJSONArray[string](row.CommunicationRulesJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode communication rules: %w", err)
	}
	escalationRules, err := decodeJSONArray[string](row.EscalationRulesJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode escalation rules: %w", err)
	}
	protectedRules, err := decodeJSONArray[string](row.ProtectedRulesJSON)
	if err != nil {
		return Constitution{}, fmt.Errorf("decode protected rules: %w", err)
	}

	return Constitution{
		ID:                  row.ID.String(),
		Version:             row.Version,
		BaseVersion:         row.BaseVersion,
		Status:              row.Status,
		Values:              values,
		Prohibitions:        prohibitions,
		StandingPermissions: permissions,
		Preferences:         preferences,
		RelationshipRules:   relationshipRules,
		FinancialBoundaries: financialBoundaries,
		CommunicationRules:  communicationRules,
		EscalationRules:     escalationRules,
		ProtectedRules:      protectedRules,
		ChangeSummary:       row.ChangeSummary,
		ApprovedBy:          row.ApprovedBy,
		ApprovedAt:          cloneTimePointer(row.ApprovedAt),
		CreatedAt:           row.CreatedAt,
	}, nil
}

func validInitialConstitutionBase(version, baseVersion int) bool {
	return (version == 1 && baseVersion == 0) ||
		(version > 1 && baseVersion == 1)
}

func normalizeActivation(
	owner string,
	id string,
	approvedBy string,
	approvalNote string,
	approvedAt time.Time,
) (string, uuid.UUID, string, string, time.Time, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return "", uuid.Nil, "", "", time.Time{}, err
	}
	constitutionID, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return "", uuid.Nil, "", "", time.Time{}, fmt.Errorf("constitution id must be a UUID: %w", err)
	}
	approvedBy, err = requiredIdentifier("approver", approvedBy, 255)
	if err != nil {
		return "", uuid.Nil, "", "", time.Time{}, err
	}
	if strings.TrimSpace(approvalNote) == "" {
		return "", uuid.Nil, "", "", time.Time{}, fmt.Errorf("approval note is required")
	}
	approvalNote = compactRedactedText(approvalNote, maxApprovalNoteRunes)
	if approvedAt.IsZero() {
		return "", uuid.Nil, "", "", time.Time{}, fmt.Errorf("approval timestamp is required")
	}
	return owner, constitutionID, approvedBy, approvalNote, approvedAt.UTC(), nil
}

func lockConstitutionOwner(tx *gorm.DB, owner string) error {
	return tx.Exec(
		"SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
		"hai-framework-constitution:"+owner,
	).Error
}

func normalizeOwner(owner string) (string, error) {
	return requiredIdentifier("owner identity", owner, 255)
}

func requiredIdentifier(name, value string, maxRunes int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if len([]rune(value)) > maxRunes {
		return "", fmt.Errorf("%s exceeds %d characters", name, maxRunes)
	}
	return value, nil
}

func normalizeRequestHash(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("request hash must be a 64-character SHA-256 hex digest")
	}
	return value, nil
}

func normalizeSHA256Digest(name, value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("%s must be a 64-character SHA-256 hex digest", name)
	}
	return value, nil
}

func normalizeConstitutionSource(value string, version int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("constitution source is required")
	}
	suffix := fmt.Sprintf(":v%d", version)
	if version <= 0 || !strings.HasSuffix(value, suffix) || strings.TrimSuffix(value, suffix) == "" {
		return "", fmt.Errorf("constitution source must use exact ID:vN format and match version %d", version)
	}
	return value, nil
}

func compactRedactedText(value string, maxRunes int) string {
	value = bearerTokenPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = jwtPattern.ReplaceAllString(value, "[REDACTED_JWT]")
	value = querySecretPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = sensitiveAssignmentPattern.ReplaceAllString(value, `${1}=[REDACTED]`)
	value = strings.TrimSpace(whitespacePattern.ReplaceAllString(value, " "))
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return value
}

func encodeJSONArray[T any](values []T) (string, error) {
	if values == nil {
		values = make([]T, 0)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	if len(encoded) == 0 || encoded[0] != '[' {
		return "", errors.New("JSON value is not an array")
	}
	return string(encoded), nil
}

func decodeJSONArray[T any](value string) ([]T, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return make([]T, 0), nil
	}
	var decoded []T
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		decoded = make([]T, 0)
	}
	return decoded, nil
}

func encodeJSONObject[T any](value T) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(encoded) == 0 || encoded[0] != '{' {
		return "", errors.New("JSON value is not an object")
	}
	return string(encoded), nil
}

func decodeJSONObject[T any](value string) (T, error) {
	var decoded T
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return decoded, nil
	}
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return decoded, err
	}
	return decoded, nil
}

func boundedSelectionLimit(limit int) int {
	if limit <= 0 {
		return defaultSelectionLimit
	}
	if limit > maxSelectionLimit {
		return maxSelectionLimit
	}
	return limit
}

func preferenceKey(owner, frameworkID string) string {
	return owner + "\x00" + frameworkID
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
