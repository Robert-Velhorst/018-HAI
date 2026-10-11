package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultOutboxBatchSize   = 32
	defaultOutboxMaxAttempts = 10
	defaultOutboxLease       = 30 * time.Second
	defaultOutboxPoll        = 5 * time.Second
	defaultOutboxRetention   = 30 * 24 * time.Hour
	defaultOutboxPruneLimit  = 1000
	defaultOutboxPruneEvery  = time.Hour
	maximumOutboxRetry       = 15 * time.Minute
)

type OutboxStatus string

const (
	OutboxPending      OutboxStatus = "pending"
	OutboxPublished    OutboxStatus = "published"
	OutboxDeadLettered OutboxStatus = "dead_lettered"
)

// OutboxRecord is one stable at-least-once delivery intent. Payload is the
// exact event envelope committed with the automation mutation.
type OutboxRecord struct {
	ID            uuid.UUID       `gorm:"column:id" json:"id"`
	AggregateID   uuid.UUID       `gorm:"column:aggregate_id" json:"aggregateId"`
	EventType     string          `gorm:"column:event_type" json:"eventType"`
	Payload       json.RawMessage `gorm:"column:payload" json:"-"`
	Status        OutboxStatus    `gorm:"column:status" json:"status"`
	AttemptCount  int             `gorm:"column:attempt_count" json:"attemptCount"`
	MaxAttempts   int             `gorm:"column:max_attempts" json:"maxAttempts"`
	NextAttemptAt time.Time       `gorm:"column:next_attempt_at" json:"nextAttemptAt"`
	LeaseToken    *uuid.UUID      `gorm:"column:lease_token" json:"-"`
	LeaseUntil    *time.Time      `gorm:"column:lease_until" json:"-"`
	LastError     string          `gorm:"column:last_error" json:"lastError,omitempty"`
	CreatedAt     time.Time       `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt     time.Time       `gorm:"column:updated_at" json:"updatedAt"`
	PublishedAt   *time.Time      `gorm:"column:published_at" json:"publishedAt,omitempty"`
}

type OutboxStats struct {
	Pending         int64           `gorm:"column:pending" json:"pending"`
	DeadLettered    int64           `gorm:"column:dead_lettered" json:"deadLettered"`
	Published       int64           `gorm:"column:published" json:"published"`
	OldestPendingAt *time.Time      `gorm:"column:oldest_pending_at" json:"oldestPendingAt,omitempty"`
	RecentFailures  []OutboxFailure `gorm:"-" json:"recentFailures"`
	CheckedAt       time.Time       `gorm:"-" json:"checkedAt"`
}

// OutboxFailure deliberately excludes the event payload. Delivery diagnostics
// must not turn the operations endpoint into a source-data export surface.
type OutboxFailure struct {
	ID           uuid.UUID    `gorm:"column:id" json:"id"`
	AggregateID  uuid.UUID    `gorm:"column:aggregate_id" json:"aggregateId"`
	EventType    string       `gorm:"column:event_type" json:"eventType"`
	Status       OutboxStatus `gorm:"column:status" json:"status"`
	AttemptCount int          `gorm:"column:attempt_count" json:"attemptCount"`
	MaxAttempts  int          `gorm:"column:max_attempts" json:"maxAttempts"`
	LastError    string       `gorm:"column:last_error" json:"lastError"`
	UpdatedAt    time.Time    `gorm:"column:updated_at" json:"updatedAt"`
}

type OutboxStore struct {
	db *gorm.DB
}

func NewOutboxStore(db *gorm.DB) *OutboxStore {
	return &OutboxStore{db: db}
}

// EnqueueTx must receive the same transaction that owns the state mutation.
// This is the consistency boundary: either both rows commit or neither does.
func EnqueueTx(tx *gorm.DB, event *AutomationEvent) error {
	if tx == nil {
		return fmt.Errorf("enqueue automation event: database transaction is required")
	}
	if err := normalizeAutomationEvent(event); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal automation event: %w", err)
	}
	return tx.Exec(`
		INSERT INTO automation_event_outbox (
			id, aggregate_id, event_type, payload, status, attempt_count,
			max_attempts, next_attempt_at, created_at, updated_at
		) VALUES (?, ?, ?, ?::jsonb, 'pending', 0, ?, ?, ?, ?)
	`, event.ID, event.Automation.ID, string(event.Type), string(payload),
		defaultOutboxMaxAttempts, event.OccurredAt, event.OccurredAt, event.OccurredAt).Error
}

func (s *OutboxStore) Claim(ctx context.Context, workerID uuid.UUID, now time.Time, limit int) ([]OutboxRecord, error) {
	if s == nil || s.db == nil || workerID == uuid.Nil {
		return nil, fmt.Errorf("claim event outbox: store and worker id are required")
	}
	if limit <= 0 || limit > 100 {
		limit = defaultOutboxBatchSize
	}
	now = now.UTC()
	leaseUntil := now.Add(defaultOutboxLease)
	var records []OutboxRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A process can die after claiming its final permitted attempt. Once its
		// lease expires, convert that row to an inspectable dead letter instead
		// of trying attempt max+1 and violating the database constraint.
		if err := tx.Exec(`
			UPDATE automation_event_outbox
			SET status = 'dead_lettered', updated_at = ?, next_attempt_at = ?,
				lease_token = NULL, lease_until = NULL,
				last_error = CASE WHEN last_error = ''
					THEN 'delivery retry budget exhausted after worker lease expired'
					ELSE last_error END
			WHERE status = 'pending'
			  AND attempt_count >= max_attempts
			  AND (lease_until IS NULL OR lease_until <= ?)
		`, now, now, now).Error; err != nil {
			return err
		}
		return tx.Raw(`
		WITH candidates AS (
			SELECT id
			FROM automation_event_outbox
			WHERE status = 'pending'
			  AND attempt_count < max_attempts
			  AND next_attempt_at <= ?
			  AND (lease_until IS NULL OR lease_until <= ?)
			ORDER BY created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT ?
		)
		UPDATE automation_event_outbox AS outbox
		SET lease_token = ?, lease_until = ?, attempt_count = attempt_count + 1,
			updated_at = ?
		FROM candidates
		WHERE outbox.id = candidates.id
		RETURNING outbox.*
		`, now, now, limit, workerID, leaseUntil, now).Scan(&records).Error
	})
	if err != nil {
		return nil, fmt.Errorf("claim event outbox: %w", err)
	}
	return records, nil
}

func (s *OutboxStore) MarkPublished(ctx context.Context, id, workerID uuid.UUID, now time.Time) error {
	result := s.db.WithContext(ctx).Exec(`
		UPDATE automation_event_outbox
		SET status = 'published', published_at = ?, updated_at = ?,
			lease_token = NULL, lease_until = NULL, last_error = ''
		WHERE id = ? AND status = 'pending' AND lease_token = ?
	`, now.UTC(), now.UTC(), id, workerID)
	return fencedOutboxResult(result, "mark event published")
}

func (s *OutboxStore) MarkFailed(ctx context.Context, record OutboxRecord, workerID uuid.UUID, failure error, now time.Time) error {
	if failure == nil {
		return errors.New("mark event failed: failure is required")
	}
	status := OutboxPending
	nextAttemptAt := now.UTC().Add(outboxRetryDelay(record.AttemptCount))
	if record.AttemptCount >= record.MaxAttempts {
		status = OutboxDeadLettered
		nextAttemptAt = now.UTC()
	}
	lastError := strings.TrimSpace(failure.Error())
	if len(lastError) > 1000 {
		lastError = lastError[:1000]
	}
	result := s.db.WithContext(ctx).Exec(`
		UPDATE automation_event_outbox
		SET status = ?, next_attempt_at = ?, updated_at = ?, last_error = ?,
			lease_token = NULL, lease_until = NULL
		WHERE id = ? AND status = 'pending' AND lease_token = ?
	`, status, nextAttemptAt, now.UTC(), lastError, record.ID, workerID)
	return fencedOutboxResult(result, "mark event failed")
}

func (s *OutboxStore) Stats(ctx context.Context) (OutboxStats, error) {
	stats := OutboxStats{RecentFailures: make([]OutboxFailure, 0)}
	if s == nil || s.db == nil {
		return stats, errors.New("event outbox store is unavailable")
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending') AS pending,
			COUNT(*) FILTER (WHERE status = 'dead_lettered') AS dead_lettered,
			COUNT(*) FILTER (WHERE status = 'published') AS published,
			MIN(created_at) FILTER (WHERE status = 'pending') AS oldest_pending_at
		FROM automation_event_outbox
	`).Scan(&stats).Error; err != nil {
		return stats, fmt.Errorf("read event outbox stats: %w", err)
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT id, aggregate_id, event_type, status, attempt_count,
			max_attempts, last_error, updated_at
		FROM automation_event_outbox
		WHERE status = 'dead_lettered' OR last_error <> ''
		ORDER BY updated_at DESC, id
		LIMIT 10
	`).Scan(&stats.RecentFailures).Error; err != nil {
		return stats, fmt.Errorf("read recent event delivery failures: %w", err)
	}
	stats.CheckedAt = time.Now().UTC()
	return stats, nil
}

func (s *OutboxStore) RetryDeadLetter(ctx context.Context, id uuid.UUID, now time.Time) error {
	if s == nil || s.db == nil || id == uuid.Nil {
		return fmt.Errorf("retry event delivery: store and event id are required")
	}
	result := s.db.WithContext(ctx).Exec(`
		UPDATE automation_event_outbox
		SET status = 'pending', attempt_count = 0, next_attempt_at = ?,
			updated_at = ?, lease_token = NULL, lease_until = NULL,
			last_error = '', published_at = NULL
		WHERE id = ? AND status = 'dead_lettered'
	`, now.UTC(), now.UTC(), id)
	if result.Error != nil {
		return fmt.Errorf("retry event delivery: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *OutboxStore) PrunePublished(ctx context.Context, before time.Time, limit int) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("prune event outbox: store is unavailable")
	}
	if limit <= 0 || limit > 5000 {
		limit = defaultOutboxPruneLimit
	}
	result := s.db.WithContext(ctx).Exec(`
		WITH doomed AS (
			SELECT id
			FROM automation_event_outbox
			WHERE status = 'published' AND published_at < ?
			ORDER BY published_at, id
			LIMIT ?
		)
		DELETE FROM automation_event_outbox AS outbox
		USING doomed
		WHERE outbox.id = doomed.id
	`, before.UTC(), limit)
	if result.Error != nil {
		return 0, fmt.Errorf("prune event outbox: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func fencedOutboxResult(result *gorm.DB, action string) error {
	if result.Error != nil {
		return fmt.Errorf("%s: %w", action, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%s: delivery lease was lost", action)
	}
	return nil
}

func outboxRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << min(attempt-1, 10)
	if delay > maximumOutboxRetry {
		return maximumOutboxRetry
	}
	return delay
}

type EventPublisher interface {
	Publish(*AutomationEvent) error
}

type OutboxDispatcher struct {
	store     *OutboxStore
	publisher EventPublisher
	workerID  uuid.UUID
	wake      chan struct{}
	lastPrune time.Time
}

func NewOutboxDispatcher(store *OutboxStore, publisher EventPublisher) *OutboxDispatcher {
	return &OutboxDispatcher{
		store: store, publisher: publisher, workerID: uuid.New(), wake: make(chan struct{}, 1),
	}
}

func (d *OutboxDispatcher) Notify() {
	if d == nil {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *OutboxDispatcher) Run(ctx context.Context) {
	if d == nil || d.store == nil || d.publisher == nil {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case <-timer.C:
		}
		processed, err := d.ProcessOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("automation event outbox dispatch failed: %v", err)
		}
		if time.Since(d.lastPrune) >= defaultOutboxPruneEvery {
			if pruned, pruneErr := d.store.PrunePublished(ctx, time.Now().Add(-defaultOutboxRetention), defaultOutboxPruneLimit); pruneErr != nil && !errors.Is(pruneErr, context.Canceled) {
				log.Printf("automation event outbox prune failed: %v", pruneErr)
			} else {
				if pruned > 0 {
					log.Printf("pruned %d published automation event outbox rows", pruned)
				}
				d.lastPrune = time.Now()
			}
		}
		delay := defaultOutboxPoll
		if processed == defaultOutboxBatchSize {
			delay = 10 * time.Millisecond
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

func (d *OutboxDispatcher) ProcessOnce(ctx context.Context) (int, error) {
	records, err := d.store.Claim(ctx, d.workerID, time.Now(), defaultOutboxBatchSize)
	if err != nil {
		return 0, err
	}
	for _, record := range records {
		var event AutomationEvent
		if err := json.Unmarshal(record.Payload, &event); err != nil {
			if markErr := d.store.MarkFailed(ctx, record, d.workerID, fmt.Errorf("decode event: %w", err), time.Now()); markErr != nil {
				return len(records), markErr
			}
			continue
		}
		if err := d.publisher.Publish(&event); err != nil {
			if markErr := d.store.MarkFailed(ctx, record, d.workerID, err, time.Now()); markErr != nil {
				return len(records), markErr
			}
			continue
		}
		if err := d.store.MarkPublished(ctx, record.ID, d.workerID, time.Now()); err != nil {
			return len(records), err
		}
	}
	return len(records), nil
}
