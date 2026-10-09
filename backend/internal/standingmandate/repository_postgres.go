package standingmandate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultDecisionLimit = 100
	maxDecisionLimit     = 1000
)

// GormRepository persists standing mandates and immutable authorization
// receipts in the canonical PostgreSQL database.
type GormRepository struct {
	DB *gorm.DB
}

var _ Repository = (*GormRepository)(nil)

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{DB: db}
}

// DefaultRepository opens the configured database, applies versioned
// migrations, and returns the production standing-mandate repository.
func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return NewGormRepository(db), nil
}

func (r *GormRepository) Create(ctx context.Context, mandate StandingMandate) error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("standing mandate database is required")
	}
	row, err := mandateToModel(mandate)
	if err != nil {
		return err
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return nil
	})
}

func (r *GormRepository) Get(
	ctx context.Context,
	ownerIdentity string,
	id uuid.UUID,
) (*StandingMandate, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || id == uuid.Nil {
		return nil, ErrNotFound
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return nil, err
	}
	defer finish()
	var row models.StandingMandate
	err = db.
		Where("owner_identity = ? AND id = ?", ownerIdentity, id).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	mandate, err := mandateFromModel(row)
	if err != nil {
		return nil, err
	}
	return &mandate, nil
}

func (r *GormRepository) Update(
	ctx context.Context,
	mandate StandingMandate,
	expectedRevision uint64,
) error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("standing mandate database is required")
	}
	row, err := mandateToModel(mandate)
	if err != nil {
		return err
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.StandingMandate
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_identity = ? AND id = ?", row.OwnerIdentity, row.ID).
			First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if row.Revision != expectedRevision+1 {
			return ErrRevisionConflict
		}

		result := tx.Model(&models.StandingMandate{}).
			Where(
				"owner_identity = ? AND id = ? AND revision = ?",
				row.OwnerIdentity,
				row.ID,
				expectedRevision,
			).
			Updates(map[string]any{
				"status":            row.Status,
				"revision":          row.Revision,
				"updated_at":        row.UpdatedAt,
				"activated_at":      row.ActivatedAt,
				"revoked_at":        row.RevokedAt,
				"revoked_by":        row.RevokedBy,
				"revocation_reason": row.RevocationReason,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return nil
	})
}

func (r *GormRepository) List(
	ctx context.Context,
	ownerIdentity string,
) ([]StandingMandate, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return []StandingMandate{}, nil
	}
	var rows []models.StandingMandate
	if err := r.DB.WithContext(ctx).
		Where("owner_identity = ?", ownerIdentity).
		Order("created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]StandingMandate, 0, len(rows))
	for _, row := range rows {
		mandate, err := mandateFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, mandate)
	}
	return result, nil
}

func (r *GormRepository) CreateDecision(
	ctx context.Context,
	decision AuthorizationDecision,
) error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("standing mandate database is required")
	}
	row, err := decisionToModel(decision)
	if err != nil {
		return err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return err
	}
	defer finish()
	result := db.
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrDecisionConflict
	}
	return nil
}

func (r *GormRepository) GetDecision(
	ctx context.Context,
	ownerIdentity string,
	id uuid.UUID,
) (*AuthorizationDecision, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || id == uuid.Nil {
		return nil, ErrNotFound
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return nil, err
	}
	defer finish()
	var row models.StandingMandateDecision
	err = db.
		Where("owner_identity = ? AND id = ?", ownerIdentity, id).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	decision, err := decisionFromModel(row)
	if err != nil {
		return nil, err
	}
	return &decision, nil
}

func (r *GormRepository) ListDecisions(
	ctx context.Context,
	ownerIdentity string,
	mandateID *uuid.UUID,
	limit int,
) ([]AuthorizationDecision, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return []AuthorizationDecision{}, nil
	}
	query := r.DB.WithContext(ctx).
		Where("owner_identity = ?", ownerIdentity)
	if mandateID != nil {
		query = query.Where("mandate_id = ?", *mandateID)
	}
	var rows []models.StandingMandateDecision
	if err := query.
		Order("evaluated_at DESC, id DESC").
		Limit(boundedDecisionLimit(limit)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]AuthorizationDecision, 0, len(rows))
	for _, row := range rows {
		decision, err := decisionFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, decision)
	}
	return result, nil
}

func mandateToModel(value StandingMandate) (models.StandingMandate, error) {
	if value.ID == uuid.Nil {
		return models.StandingMandate{}, fmt.Errorf("standing mandate id is required")
	}
	if strings.TrimSpace(value.OwnerIdentity) == "" {
		return models.StandingMandate{}, fmt.Errorf("owner identity is required")
	}
	if strings.TrimSpace(value.Name) == "" ||
		strings.TrimSpace(value.Purpose) == "" ||
		strings.TrimSpace(value.Version) == "" ||
		strings.TrimSpace(value.CreatedBy) == "" {
		return models.StandingMandate{}, fmt.Errorf(
			"mandate name, purpose, version, and creator are required",
		)
	}
	if value.Status != StatusDraft && value.Status != StatusActive && value.Status != StatusRevoked {
		return models.StandingMandate{}, fmt.Errorf("invalid standing mandate status %q", value.Status)
	}
	if value.Revision == 0 {
		return models.StandingMandate{}, fmt.Errorf("standing mandate revision must be positive")
	}
	if value.AutonomyCeiling < 0 || value.AutonomyCeiling > 10 {
		return models.StandingMandate{}, fmt.Errorf("autonomy ceiling must be between 0 and 10")
	}
	scopes, err := marshalJSONArray(value.Scopes)
	if err != nil {
		return models.StandingMandate{}, fmt.Errorf("encode mandate scopes: %w", err)
	}
	if len(value.Scopes) == 0 {
		return models.StandingMandate{}, fmt.Errorf("at least one bounded scope is required")
	}
	scopeIDs := make(map[string]struct{}, len(value.Scopes))
	for index, scope := range value.Scopes {
		if err := validateScope(scope); err != nil {
			return models.StandingMandate{}, fmt.Errorf("scope %d: %w", index, err)
		}
		id := normalize(scope.ID)
		if _, exists := scopeIDs[id]; exists {
			return models.StandingMandate{}, fmt.Errorf("scope IDs must be unique")
		}
		scopeIDs[id] = struct{}{}
	}
	if err := validateApprovalPolicy(value.ApprovalPolicy); err != nil {
		return models.StandingMandate{}, err
	}
	stopIDs := make(map[string]struct{}, len(value.StopConditions))
	for index, condition := range value.StopConditions {
		if err := validateStopCondition(condition); err != nil {
			return models.StandingMandate{}, fmt.Errorf(
				"stop condition %d: %w",
				index,
				err,
			)
		}
		id := normalize(condition.ID)
		if _, exists := stopIDs[id]; exists {
			return models.StandingMandate{}, fmt.Errorf(
				"stop condition IDs must be unique",
			)
		}
		stopIDs[id] = struct{}{}
	}
	if value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() {
		return models.StandingMandate{}, fmt.Errorf(
			"created and updated timestamps are required",
		)
	}
	if value.ExpiresAt != nil && !value.ExpiresAt.After(value.CreatedAt) {
		return models.StandingMandate{}, fmt.Errorf(
			"mandate expiry must follow creation",
		)
	}
	switch value.Status {
	case StatusDraft:
		if value.ActivatedAt != nil || value.RevokedAt != nil {
			return models.StandingMandate{}, fmt.Errorf(
				"draft mandate cannot have activation or revocation timestamps",
			)
		}
	case StatusActive:
		if value.ActivatedAt == nil || value.RevokedAt != nil {
			return models.StandingMandate{}, fmt.Errorf(
				"active mandate requires activation and cannot be revoked",
			)
		}
	case StatusRevoked:
		if value.RevokedAt == nil ||
			strings.TrimSpace(value.RevokedBy) == "" ||
			strings.TrimSpace(value.RevocationReason) == "" {
			return models.StandingMandate{}, fmt.Errorf(
				"revoked mandate requires revocation metadata",
			)
		}
	}
	approvalPolicy, err := marshalJSONObject(value.ApprovalPolicy)
	if err != nil {
		return models.StandingMandate{}, fmt.Errorf("encode mandate approval policy: %w", err)
	}
	stopConditions, err := marshalJSONArray(value.StopConditions)
	if err != nil {
		return models.StandingMandate{}, fmt.Errorf("encode mandate stop conditions: %w", err)
	}
	sourceReferences, err := marshalJSONArray(value.SourceReferences)
	if err != nil {
		return models.StandingMandate{}, fmt.Errorf("encode mandate source references: %w", err)
	}
	return models.StandingMandate{
		ID:                   value.ID,
		OwnerIdentity:        strings.TrimSpace(value.OwnerIdentity),
		Name:                 strings.TrimSpace(value.Name),
		Purpose:              strings.TrimSpace(value.Purpose),
		Status:               string(value.Status),
		Version:              strings.TrimSpace(value.Version),
		Revision:             value.Revision,
		ScopesJSON:           scopes,
		AutonomyCeiling:      value.AutonomyCeiling,
		ApprovalPolicyJSON:   approvalPolicy,
		StopConditionsJSON:   stopConditions,
		SourceReferencesJSON: sourceReferences,
		CreatedBy:            strings.TrimSpace(value.CreatedBy),
		CreatedAt:            value.CreatedAt.UTC(),
		UpdatedAt:            value.UpdatedAt.UTC(),
		ActivatedAt:          cloneTime(value.ActivatedAt),
		ExpiresAt:            cloneTime(value.ExpiresAt),
		RevokedAt:            cloneTime(value.RevokedAt),
		RevokedBy:            strings.TrimSpace(value.RevokedBy),
		RevocationReason:     strings.TrimSpace(value.RevocationReason),
	}, nil
}

func mandateFromModel(row models.StandingMandate) (StandingMandate, error) {
	var scopes []Scope
	if err := unmarshalJSONArray("mandate scopes", row.ScopesJSON, &scopes); err != nil {
		return StandingMandate{}, err
	}
	if len(scopes) == 0 {
		return StandingMandate{}, fmt.Errorf("mandate scopes must not be empty")
	}
	var approvalPolicy ApprovalPolicy
	if err := unmarshalJSONObject(
		"mandate approval policy",
		row.ApprovalPolicyJSON,
		&approvalPolicy,
	); err != nil {
		return StandingMandate{}, err
	}
	var stopConditions []StopCondition
	if err := unmarshalJSONArray(
		"mandate stop conditions",
		row.StopConditionsJSON,
		&stopConditions,
	); err != nil {
		return StandingMandate{}, err
	}
	var sourceReferences []string
	if err := unmarshalJSONArray(
		"mandate source references",
		row.SourceReferencesJSON,
		&sourceReferences,
	); err != nil {
		return StandingMandate{}, err
	}
	status := Status(row.Status)
	if status != StatusDraft && status != StatusActive && status != StatusRevoked {
		return StandingMandate{}, fmt.Errorf("invalid persisted mandate status %q", row.Status)
	}
	return StandingMandate{
		ID:               row.ID,
		OwnerIdentity:    row.OwnerIdentity,
		Name:             row.Name,
		Purpose:          row.Purpose,
		Status:           status,
		Version:          row.Version,
		Revision:         row.Revision,
		Scopes:           scopes,
		AutonomyCeiling:  row.AutonomyCeiling,
		ApprovalPolicy:   approvalPolicy,
		StopConditions:   stopConditions,
		SourceReferences: sourceReferences,
		CreatedBy:        row.CreatedBy,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
		ActivatedAt:      cloneTime(row.ActivatedAt),
		ExpiresAt:        cloneTime(row.ExpiresAt),
		RevokedAt:        cloneTime(row.RevokedAt),
		RevokedBy:        row.RevokedBy,
		RevocationReason: row.RevocationReason,
	}, nil
}

func decisionToModel(value AuthorizationDecision) (models.StandingMandateDecision, error) {
	if value.ID == uuid.Nil || value.MandateID == uuid.Nil {
		return models.StandingMandateDecision{}, fmt.Errorf("decision and mandate IDs are required")
	}
	if strings.TrimSpace(value.OwnerIdentity) == "" {
		return models.StandingMandateDecision{}, fmt.Errorf("decision owner identity is required")
	}
	if strings.TrimSpace(value.ActorIdentity) == "" ||
		strings.TrimSpace(value.Action) == "" ||
		strings.TrimSpace(value.Reason) == "" ||
		value.EvaluatedAt.IsZero() {
		return models.StandingMandateDecision{}, fmt.Errorf(
			"decision actor, action, reason, and timestamp are required",
		)
	}
	if value.Outcome != DecisionAuthorized &&
		value.Outcome != DecisionRequiresApproval &&
		value.Outcome != DecisionDenied {
		return models.StandingMandateDecision{}, fmt.Errorf("invalid decision outcome %q", value.Outcome)
	}
	if value.Evidence.MandateRevision == 0 {
		return models.StandingMandateDecision{}, fmt.Errorf("decision mandate revision is required")
	}
	if value.EffectiveAutonomy < 0 || value.EffectiveAutonomy > 10 {
		return models.StandingMandateDecision{}, fmt.Errorf(
			"effective autonomy must be between 0 and 10",
		)
	}
	if value.ApprovalSatisfied && !value.ApprovalRequired {
		return models.StandingMandateDecision{}, fmt.Errorf(
			"satisfied approval must also be required",
		)
	}
	for label, digest := range map[string]string{
		"request digest":  value.Evidence.RequestDigest,
		"mandate digest":  value.Evidence.MandateDigest,
		"decision digest": value.Evidence.DecisionDigest,
	} {
		if !validSHA256Digest(digest) {
			return models.StandingMandateDecision{}, fmt.Errorf("%s must be a SHA-256 hex digest", label)
		}
	}
	evidence, err := marshalJSONObject(value.Evidence)
	if err != nil {
		return models.StandingMandateDecision{}, fmt.Errorf("encode decision evidence: %w", err)
	}
	return models.StandingMandateDecision{
		ID:                value.ID,
		MandateID:         value.MandateID,
		OwnerIdentity:     strings.TrimSpace(value.OwnerIdentity),
		ActorIdentity:     strings.TrimSpace(value.ActorIdentity),
		Action:            strings.TrimSpace(value.Action),
		Outcome:           string(value.Outcome),
		Reason:            strings.TrimSpace(value.Reason),
		EffectiveAutonomy: value.EffectiveAutonomy,
		ApprovalRequired:  value.ApprovalRequired,
		ApprovalSatisfied: value.ApprovalSatisfied,
		MandateRevision:   value.Evidence.MandateRevision,
		RequestDigest:     value.Evidence.RequestDigest,
		MandateDigest:     value.Evidence.MandateDigest,
		DecisionDigest:    value.Evidence.DecisionDigest,
		EvidenceJSON:      evidence,
		EvaluatedAt:       value.EvaluatedAt.UTC(),
	}, nil
}

func decisionFromModel(row models.StandingMandateDecision) (AuthorizationDecision, error) {
	var evidence DecisionEvidence
	if err := unmarshalJSONObject("decision evidence", row.EvidenceJSON, &evidence); err != nil {
		return AuthorizationDecision{}, err
	}
	if evidence.MandateRevision != row.MandateRevision ||
		evidence.RequestDigest != row.RequestDigest ||
		evidence.MandateDigest != row.MandateDigest ||
		evidence.DecisionDigest != row.DecisionDigest {
		return AuthorizationDecision{}, fmt.Errorf("decision evidence columns do not match evidence payload")
	}
	outcome := DecisionOutcome(row.Outcome)
	if outcome != DecisionAuthorized &&
		outcome != DecisionRequiresApproval &&
		outcome != DecisionDenied {
		return AuthorizationDecision{}, fmt.Errorf("invalid persisted decision outcome %q", row.Outcome)
	}
	return AuthorizationDecision{
		ID:                row.ID,
		MandateID:         row.MandateID,
		OwnerIdentity:     row.OwnerIdentity,
		ActorIdentity:     row.ActorIdentity,
		Action:            row.Action,
		Outcome:           outcome,
		Reason:            row.Reason,
		EffectiveAutonomy: row.EffectiveAutonomy,
		ApprovalRequired:  row.ApprovalRequired,
		ApprovalSatisfied: row.ApprovalSatisfied,
		EvaluatedAt:       row.EvaluatedAt.UTC(),
		Evidence:          evidence,
	}, nil
}

func marshalJSONArray[T any](value []T) (string, error) {
	if value == nil {
		value = make([]T, 0)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(encoded) == 0 || encoded[0] != '[' {
		return "", fmt.Errorf("JSON value is not an array")
	}
	return string(encoded), nil
}

func marshalJSONObject[T any](value T) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(encoded) == 0 || encoded[0] != '{' {
		return "", fmt.Errorf("JSON value is not an object")
	}
	return string(encoded), nil
}

func unmarshalJSONArray(label, value string, target any) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" || value[0] != '[' {
		return fmt.Errorf("%s must be a JSON array", label)
	}
	if err := json.Unmarshal([]byte(value), target); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	return nil
}

func unmarshalJSONObject(label, value string, target any) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" || value[0] != '{' {
		return fmt.Errorf("%s must be a JSON object", label)
	}
	if err := json.Unmarshal([]byte(value), target); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func boundedDecisionLimit(limit int) int {
	if limit <= 0 {
		return defaultDecisionLimit
	}
	if limit > maxDecisionLimit {
		return maxDecisionLimit
	}
	return limit
}

func cloneDecision(value AuthorizationDecision) AuthorizationDecision {
	cloned := value
	cloned.Evidence.MatchedScopeIDs = append(
		[]string(nil),
		value.Evidence.MatchedScopeIDs...,
	)
	cloned.Evidence.TriggeredStops = append(
		[]TriggeredStop(nil),
		value.Evidence.TriggeredStops...,
	)
	cloned.Evidence.SourceReferences = append(
		[]string(nil),
		value.Evidence.SourceReferences...,
	)
	cloned.Evidence.Trace = append([]DecisionTrace(nil), value.Evidence.Trace...)
	return cloned
}

func monotonicTime(now time.Time, previous time.Time) time.Time {
	now = now.UTC()
	previous = previous.UTC()
	if !now.After(previous) {
		return previous.Add(time.Microsecond)
	}
	return now
}
