package executionauth

import "errors"

var (
	ErrNotFound                          = errors.New("execution authorization record not found")
	ErrIdempotencyConflict               = errors.New("execution authorization idempotency conflict")
	ErrAlreadyConsumed                   = errors.New("execution authorization was already consumed")
	ErrAlreadyExercised                  = errors.New("execution authorization final effect was already exercised")
	ErrFinalEffectExpired                = errors.New("execution authorization final effect proof expired")
	ErrNotAuthorized                     = errors.New("execution authorization did not permit execution")
	ErrFinalEffectMismatch               = errors.New("execution authorization final effect binding does not match")
	ErrPolicyUnavailable                 = errors.New("execution authorization policy is unavailable")
	ErrAuthorizationChanged              = errors.New("execution authorization evidence changed before consumption")
	ErrSourceEvidenceUnverified          = errors.New("source evidence could not be independently verified")
	ErrPostgresTransactionRequired       = errors.New("execution authorization requires a PostgreSQL repository and active transaction from the same database")
	ErrApprovalAlreadyClaimed            = errors.New("execution approval decision was already claimed by another receipt")
	ErrApprovalClaimIsolationUnsupported = errors.New("execution approval claims require read-committed PostgreSQL isolation")
)
