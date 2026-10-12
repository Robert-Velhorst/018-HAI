package models

import (
	"time"

	"github.com/google/uuid"
)

// Durable job statuses. A job is only ever in one of these.
const (
	DurableJobPending     = "pending"      // waiting for RunAt, claimable
	DurableJobRunning     = "running"      // leased by a worker
	DurableJobSucceeded   = "succeeded"    // terminal, handler returned nil
	DurableJobDead        = "dead"         // terminal, exhausted MaxAttempts
	DurableJobNeedsReview = "needs_review" // nonclaimable; blocks automatic replacement
	DurableJobSettling    = "settling"     // terminal preparation awaiting confirmed release
)

const (
	DurableJobReplayAtLeastOnce = "at_least_once"
	DurableJobReviewUnknown     = "review_unknown"
)

// DurableJob is a unit of background work that survives process restarts.
//
// This is the persistence behind the durable worker model: scheduling (RunAt),
// bounded retry with backoff (Attempts/MaxAttempts), and crash recovery via a
// lease (LockedBy/LockedAt). A worker that dies mid-job does not lose the job —
// its lease expires and recovery either reclaims it or requires reconciliation,
// according to the persisted ReplayPolicy.
//
// The default is at-least-once delivery and requires idempotent handlers.
// Review-sensitive work holds unknown outcomes instead of blindly repeating
// them; this is not an exactly-once guarantee or operator recovery protocol.
type DurableJob struct {
	ID    uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	Queue string    `gorm:"type:text;not null;default:'default';index" json:"queue"`
	Kind  string    `gorm:"type:text;not null;index" json:"kind"`
	// Payload is canonical JSON handed to the registered handler.
	Payload string `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`

	Status string `gorm:"type:text;not null;default:'pending';index" json:"status"`
	// ReviewUnknown prevents automatic replay after an unconfirmed invocation.
	ReplayPolicy string `gorm:"type:text;not null;default:'at_least_once'" json:"replayPolicy"`
	Attempts     int    `gorm:"not null;default:0" json:"attempts"`
	MaxAttempts  int    `gorm:"not null;default:5" json:"maxAttempts"`

	// RunAt is when the job becomes claimable. Retries push it into the future.
	RunAt time.Time `gorm:"not null;index" json:"runAt"`

	// Lease. LockedAt older than the runner's lease duration means the worker
	// holding it is presumed dead. ReplayPolicy decides retry versus review.
	LockedBy string     `gorm:"type:text" json:"lockedBy,omitempty"`
	LockedAt *time.Time `json:"lockedAt,omitempty"`
	// LeaseGeneration is a fencing token. Every successful claim increments it,
	// and outcome writes must present the same generation and worker identity.
	// A stale worker therefore cannot overwrite a job reclaimed after expiry.
	LeaseGeneration int64 `gorm:"not null;default:0" json:"leaseGeneration"`

	LastError   string     `gorm:"type:text" json:"lastError,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// TableName keeps the table name explicit and stable for the SQL migrations.
func (DurableJob) TableName() string { return "durable_jobs" }
