package llm

import (
	"automation-hub-backend/internal/safety"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	modelMaintenanceAdmissionInProgress = "in_progress"
	modelMaintenanceAdmissionRetryWait  = "retry_wait"
	modelMaintenanceAdmissionSucceeded  = "succeeded"
	modelMaintenanceAdmissionRetry      = "retry"
	modelMaintenanceAdmissionVerified   = "verified"
	modelMaintenanceAdmissionLeaseGrace = 30 * time.Second
)

var errModelMaintenanceAdmissionLost = errors.New("model maintenance admission claim is no longer owned by this attempt")

// ModelMaintenanceAdmissionClaim is an owner-free, durable fence for one
// configured provider/model resource. The token is only used to fence updates;
// it is never returned from an API or stored in maintenance history.
type ModelMaintenanceAdmissionClaim struct {
	ProviderID               string
	ModelID                  string
	ConfigurationFingerprint string
	Token                    string
	State                    string
	RetryAt                  time.Time
}

// ModelMaintenanceAdmissionRepository must persist a claim before local model
// network I/O and finalize it with the same fencing token afterward.
type ModelMaintenanceAdmissionRepository interface {
	GetModelMaintenanceAdmission(ctx context.Context, providerID, modelID, fingerprint string) (ModelMaintenanceAdmissionClaim, bool, error)
	AcquireModelMaintenanceAdmission(ctx context.Context, providerID, modelID, fingerprint string, lease time.Duration) (ModelMaintenanceAdmissionClaim, bool, error)
	FinalizeModelMaintenanceAdmission(ctx context.Context, claim ModelMaintenanceAdmissionClaim, outcome string, retryAfter time.Duration) error
}

func (r *GormModelMaintenanceRepository) GetModelMaintenanceAdmission(
	ctx context.Context,
	providerID, modelID, fingerprint string,
) (ModelMaintenanceAdmissionClaim, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	claim := ModelMaintenanceAdmissionClaim{
		ProviderID: strings.TrimSpace(providerID), ModelID: strings.TrimSpace(modelID),
		ConfigurationFingerprint: strings.TrimSpace(fingerprint),
	}
	if err := validateModelMaintenanceAdmissionIdentity(claim); err != nil {
		return ModelMaintenanceAdmissionClaim{}, false, err
	}
	const query = `
SELECT state,
       CASE WHEN state = 'in_progress' THEN lease_expires_at ELSE retry_after END
  FROM public.llm_model_maintenance_admission_claims
 WHERE provider_id = ? AND model_id = ? AND configuration_fingerprint = ?`
	err := r.DB.WithContext(ctx).Raw(query, claim.ProviderID, claim.ModelID, claim.ConfigurationFingerprint).
		Row().Scan(&claim.State, &claim.RetryAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelMaintenanceAdmissionClaim{}, false, nil
	}
	if err != nil {
		return ModelMaintenanceAdmissionClaim{}, false, fmt.Errorf("read model maintenance admission state: %w", err)
	}
	claim.RetryAt = claim.RetryAt.UTC()
	return claim, true, nil
}

func (r *GormModelMaintenanceRepository) AcquireModelMaintenanceAdmission(
	ctx context.Context,
	providerID, modelID, fingerprint string,
	lease time.Duration,
) (ModelMaintenanceAdmissionClaim, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	claim := ModelMaintenanceAdmissionClaim{
		ProviderID: strings.TrimSpace(providerID), ModelID: strings.TrimSpace(modelID),
		ConfigurationFingerprint: strings.TrimSpace(fingerprint), Token: uuid.NewString(),
	}
	if err := validateModelMaintenanceAdmission(claim, lease); err != nil {
		return ModelMaintenanceAdmissionClaim{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return ModelMaintenanceAdmissionClaim{}, false, err
	}
	seconds := lease.Seconds()
	const acquireSQL = `
INSERT INTO public.llm_model_maintenance_admission_claims AS existing (
    provider_id, model_id, configuration_fingerprint, claim_token, state,
    claimed_at, lease_expires_at, retry_after, finalized_at, last_outcome
)
VALUES (?, ?, ?, ?::uuid, 'in_progress', clock_timestamp(),
        clock_timestamp() + (? * interval '1 second'),
        clock_timestamp() + (? * interval '1 second'), NULL, NULL)
ON CONFLICT (provider_id, model_id, configuration_fingerprint) DO UPDATE
   SET claim_token = EXCLUDED.claim_token,
       state = 'in_progress',
       claimed_at = clock_timestamp(),
       lease_expires_at = EXCLUDED.lease_expires_at,
       retry_after = EXCLUDED.retry_after,
       finalized_at = NULL,
       last_outcome = NULL
 WHERE (existing.state = 'in_progress' AND existing.lease_expires_at <= clock_timestamp())
    OR (existing.state IN ('retry_wait', 'succeeded') AND existing.retry_after <= clock_timestamp())
RETURNING claim_token::text, state, lease_expires_at`
	err := r.DB.WithContext(ctx).Raw(acquireSQL,
		claim.ProviderID, claim.ModelID, claim.ConfigurationFingerprint, claim.Token, seconds, seconds,
	).Row().Scan(&claim.Token, &claim.State, &claim.RetryAt)
	if err == nil {
		claim.RetryAt = claim.RetryAt.UTC()
		return claim, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ModelMaintenanceAdmissionClaim{}, false, fmt.Errorf("persist model maintenance admission claim: %w", err)
	}

	const blockedSQL = `
SELECT state,
       CASE WHEN state = 'in_progress' THEN lease_expires_at ELSE retry_after END
  FROM public.llm_model_maintenance_admission_claims
 WHERE provider_id = ? AND model_id = ? AND configuration_fingerprint = ?`
	if err := r.DB.WithContext(ctx).Raw(blockedSQL, claim.ProviderID, claim.ModelID, claim.ConfigurationFingerprint).
		Row().Scan(&claim.State, &claim.RetryAt); err != nil {
		return ModelMaintenanceAdmissionClaim{}, false, fmt.Errorf("read model maintenance admission state: %w", err)
	}
	claim.Token = ""
	claim.RetryAt = claim.RetryAt.UTC()
	return claim, false, nil
}

func (r *GormModelMaintenanceRepository) FinalizeModelMaintenanceAdmission(
	ctx context.Context,
	claim ModelMaintenanceAdmissionClaim,
	outcome string,
	retryAfter time.Duration,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	claim.ProviderID = strings.TrimSpace(claim.ProviderID)
	claim.ModelID = strings.TrimSpace(claim.ModelID)
	claim.ConfigurationFingerprint = strings.TrimSpace(claim.ConfigurationFingerprint)
	claim.Token = strings.TrimSpace(claim.Token)
	if err := validateModelMaintenanceAdmission(claim, time.Second); err != nil {
		return err
	}
	state := modelMaintenanceAdmissionRetryWait
	switch strings.TrimSpace(outcome) {
	case modelMaintenanceAdmissionVerified:
		state = modelMaintenanceAdmissionSucceeded
	case modelMaintenanceAdmissionRetry:
	default:
		return fmt.Errorf("unsupported model maintenance admission outcome")
	}
	if retryAfter <= 0 || retryAfter > modelMaintenanceInterval() {
		return fmt.Errorf("model maintenance admission retry interval is outside the allowed range")
	}
	const finalizeSQL = `
UPDATE public.llm_model_maintenance_admission_claims
   SET state = ?,
       lease_expires_at = clock_timestamp(),
       retry_after = clock_timestamp() + (? * interval '1 second'),
       finalized_at = clock_timestamp(),
       last_outcome = ?,
       updated_at = clock_timestamp()
 WHERE provider_id = ? AND model_id = ? AND configuration_fingerprint = ?
   AND claim_token = ?::uuid AND state = 'in_progress'`
	result := r.DB.WithContext(ctx).Exec(finalizeSQL,
		state, retryAfter.Seconds(), strings.TrimSpace(outcome),
		claim.ProviderID, claim.ModelID, claim.ConfigurationFingerprint, claim.Token,
	)
	if result.Error != nil {
		return fmt.Errorf("finalize model maintenance admission claim: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return errModelMaintenanceAdmissionLost
	}
	return nil
}

func validateModelMaintenanceAdmission(claim ModelMaintenanceAdmissionClaim, lease time.Duration) error {
	if err := validateModelMaintenanceAdmissionIdentity(claim); err != nil {
		return err
	}
	if _, err := uuid.Parse(claim.Token); err != nil {
		return fmt.Errorf("model maintenance claim token must be a UUID")
	}
	maxLease := 3*modelMaintenanceTimeout() + failedRefreshDigestInspectionTimeout + modelMaintenanceAdmissionLeaseGrace
	if lease < time.Second || lease > maxLease {
		return fmt.Errorf("model maintenance claim lease is outside the bounded operation window")
	}
	return nil
}

func validateModelMaintenanceAdmissionIdentity(claim ModelMaintenanceAdmissionClaim) error {
	if claim.ProviderID == "" || len(claim.ProviderID) > 120 || claim.ModelID == "" || len(claim.ModelID) > 255 {
		return fmt.Errorf("provider and model identifiers are required and must fit the maintenance schema")
	}
	fingerprint, err := hex.DecodeString(claim.ConfigurationFingerprint)
	if err != nil || len(fingerprint) != 32 || strings.ToLower(claim.ConfigurationFingerprint) != claim.ConfigurationFingerprint {
		return fmt.Errorf("model maintenance configuration fingerprint must be a lowercase SHA-256 digest")
	}
	return nil
}

func modelMaintenanceAdmissionLeaseDuration() time.Duration {
	return 3*modelMaintenanceTimeout() + failedRefreshDigestInspectionTimeout + modelMaintenanceAdmissionLeaseGrace
}

func modelMaintenanceAdmissionBlockedResult(provider Provider, model Model, fingerprint string, claim ModelMaintenanceAdmissionClaim) ModelMaintenanceResult {
	status := "failed"
	reason := "durable model maintenance admission is holding this model until its bounded retry time"
	if claim.State == modelMaintenanceAdmissionInProgress {
		status = "in_progress"
		reason = "another backend process owns the durable model maintenance lease"
	}
	retryAt := claim.RetryAt.UTC()
	if retryAt.IsZero() {
		retryAt = time.Now().UTC().Add(modelMaintenanceFailureRetryInterval())
	}
	return ModelMaintenanceResult{
		ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name,
		Status: status, Reason: safety.RedactSecrets(reason), ConfigurationFingerprint: fingerprint,
		BlocksExecution: true, CheckedAt: time.Now().UTC(), NextCheckDueAt: &retryAt,
	}
}
