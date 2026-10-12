package agentcoordination

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeTransport struct {
	calls   int
	receipt DeliveryReceipt
	err     error
}

type cancelingTransport struct {
	cancel context.CancelFunc
	calls  int
}

func (transport *cancelingTransport) Deliver(ctx context.Context, _ Message) (DeliveryReceipt, error) {
	transport.calls++
	transport.cancel()
	return DeliveryReceipt{}, ctx.Err()
}

func (transport *fakeTransport) Deliver(
	_ context.Context,
	message Message,
) (DeliveryReceipt, error) {
	transport.calls++
	if transport.err != nil {
		return DeliveryReceipt{}, transport.err
	}
	receipt := transport.receipt
	if receipt.MessageID == "" {
		receipt = DeliveryReceipt{
			MessageID:     message.ID,
			CorrelationID: message.CorrelationID,
			TransportID:   "transport-1",
			AcceptedAt:    message.CreatedAt.Add(time.Second),
		}
	}
	return receipt, nil
}

type memoryDispatchRecord struct {
	digest    string
	completed bool
	receipt   DeliveryReceipt
}

type memoryDispatchStore struct {
	records map[string]memoryDispatchRecord
}

type cleanupContextDispatchStore struct {
	*memoryDispatchStore
	abandonContextErr error
	abandonCalls      int
	onBegin           func()
}

func (store *cleanupContextDispatchStore) Begin(ctx context.Context, key, digest string, expiresAt time.Time) (DispatchClaim, error) {
	claim, err := store.memoryDispatchStore.Begin(ctx, key, digest, expiresAt)
	if store.onBegin != nil {
		store.onBegin()
	}
	return claim, err
}

func (store *cleanupContextDispatchStore) Abandon(ctx context.Context, key, digest string) error {
	store.abandonCalls++
	store.abandonContextErr = ctx.Err()
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.memoryDispatchStore.Abandon(ctx, key, digest)
}

func newMemoryDispatchStore() *memoryDispatchStore {
	return &memoryDispatchStore{records: map[string]memoryDispatchRecord{}}
}

func (store *memoryDispatchStore) Begin(
	_ context.Context,
	key string,
	digest string,
	_ time.Time,
) (DispatchClaim, error) {
	record, exists := store.records[key]
	if !exists {
		store.records[key] = memoryDispatchRecord{digest: digest}
		return DispatchClaim{Status: DispatchClaimAcquired}, nil
	}
	if record.digest != digest {
		return DispatchClaim{Status: DispatchClaimConflict}, nil
	}
	if record.completed {
		receipt := record.receipt
		return DispatchClaim{Status: DispatchClaimDuplicate, Receipt: &receipt}, nil
	}
	return DispatchClaim{Status: DispatchClaimDuplicate}, nil
}

func (store *memoryDispatchStore) Complete(
	_ context.Context,
	key string,
	digest string,
	receipt DeliveryReceipt,
) error {
	record, exists := store.records[key]
	if !exists || record.digest != digest {
		return fmt.Errorf("missing dispatch claim")
	}
	record.completed = true
	record.receipt = receipt
	store.records[key] = record
	return nil
}

func (store *memoryDispatchStore) Abandon(
	_ context.Context,
	key string,
	digest string,
) error {
	record, exists := store.records[key]
	if exists && record.digest == digest && !record.completed {
		delete(store.records, key)
	}
	return nil
}

func TestCoordinatorDispatchIsIdempotent(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	transport := &fakeTransport{}
	store := newMemoryDispatchStore()
	coordinator, err := NewCoordinator(
		DefaultValidationPolicy(),
		transport,
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	message := validMessage(t, now)
	first, err := coordinator.Dispatch(context.Background(), message)
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	second, err := coordinator.Dispatch(context.Background(), message)
	if err != nil {
		t.Fatalf("duplicate dispatch: %v", err)
	}
	if transport.calls != 1 || first.Duplicate || !second.Duplicate {
		t.Fatalf(
			"idempotent dispatch failed: calls=%d first=%#v second=%#v",
			transport.calls,
			first,
			second,
		)
	}
}

func TestCoordinatorReleasesClaimAfterTransportFailure(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	transport := &fakeTransport{err: fmt.Errorf("offline: %w", ErrDeliveryNotAccepted)}
	store := newMemoryDispatchStore()
	coordinator, err := NewCoordinator(
		DefaultValidationPolicy(),
		transport,
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	message := validMessage(t, now)
	if _, err := coordinator.Dispatch(context.Background(), message); err == nil {
		t.Fatal("transport failure was not returned")
	}
	transport.err = nil
	if _, err := coordinator.Dispatch(context.Background(), message); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if transport.calls != 2 {
		t.Fatalf("transport calls = %d, want 2", transport.calls)
	}
}

func TestCoordinatorRetainsClaimAfterCanceledDelivery(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cleanupContextDispatchStore{memoryDispatchStore: newMemoryDispatchStore()}
	transport := &cancelingTransport{cancel: cancel}
	coordinator, err := NewCoordinator(
		DefaultValidationPolicy(),
		transport,
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	message := validMessage(t, now)
	if _, err := coordinator.Dispatch(ctx, message); !errors.Is(err, ErrDeliveryOutcomeUnknown) {
		t.Fatal("canceled delivery was not returned as an error")
	}
	if store.abandonCalls != 0 {
		t.Fatalf("ambiguous delivery released its claim %d times", store.abandonCalls)
	}
	if _, exists := store.records[message.IdempotencyKey]; !exists {
		t.Fatal("ambiguous delivery released its idempotency claim")
	}
	if _, err := coordinator.Dispatch(context.Background(), message); !errors.Is(err, ErrDuplicateDispatch) {
		t.Fatalf("retry after ambiguous delivery = %v, want duplicate claim refusal", err)
	}
	if transport.calls != 1 {
		t.Fatalf("ambiguous delivery was attempted %d times, want exactly once", transport.calls)
	}
}

func TestCoordinatorKeepsClaimWhenTransportReceiptIsInvalid(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	message := validMessage(t, now)
	store := &cleanupContextDispatchStore{memoryDispatchStore: newMemoryDispatchStore()}
	transport := &fakeTransport{receipt: DeliveryReceipt{
		MessageID: "unexpected-message", CorrelationID: message.CorrelationID,
		TransportID: "transport-1", AcceptedAt: now.Add(time.Second),
	}}
	coordinator, err := NewCoordinator(DefaultValidationPolicy(), transport, store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	if _, err := coordinator.Dispatch(context.Background(), message); !errors.Is(err, ErrDeliveryOutcomeUnknown) {
		t.Fatalf("invalid receipt error = %v, want unknown delivery outcome", err)
	}
	if store.abandonCalls != 0 {
		t.Fatalf("invalid receipt released its claim %d times", store.abandonCalls)
	}
	if _, err := coordinator.Dispatch(context.Background(), message); !errors.Is(err, ErrDuplicateDispatch) {
		t.Fatalf("retry after invalid receipt = %v, want duplicate claim refusal", err)
	}
	if transport.calls != 1 {
		t.Fatalf("invalid receipt led to %d transport calls, want 1", transport.calls)
	}
}

func TestCoordinatorDoesNotDeliverAfterClaimAcquisitionCancellation(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	memoryStore := newMemoryDispatchStore()
	store := &cleanupContextDispatchStore{memoryDispatchStore: memoryStore, onBegin: cancel}
	coordinator, err := NewCoordinator(
		DefaultValidationPolicy(),
		&fakeTransport{},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	message := validMessage(t, now)
	transport := coordinator.transport.(*fakeTransport)
	if _, err := coordinator.Dispatch(ctx, message); err == nil {
		t.Fatal("canceled claim acquisition was not returned as an error")
	}
	if transport.calls != 0 {
		t.Fatalf("transport called %d times after cancellation, want 0", transport.calls)
	}
	if store.abandonContextErr != nil {
		t.Fatalf("claim cleanup inherited caller cancellation: %v", store.abandonContextErr)
	}
	if _, exists := memoryStore.records[message.IdempotencyKey]; exists {
		t.Fatal("canceled claim remained acquired")
	}
}

func TestCoordinatorDoesNotDeliverAfterMessageExpiresDuringClaimAcquisition(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	memoryStore := newMemoryDispatchStore()
	store := &cleanupContextDispatchStore{memoryDispatchStore: memoryStore}
	coordinator, err := NewCoordinator(
		DefaultValidationPolicy(),
		&fakeTransport{},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	message := validMessage(t, now)
	store.onBegin = func() { now = message.ExpiresAt.Add(time.Nanosecond) }
	transport := coordinator.transport.(*fakeTransport)
	if _, err := coordinator.Dispatch(context.Background(), message); err == nil {
		t.Fatal("expired message was not rejected after claim acquisition")
	}
	if transport.calls != 0 {
		t.Fatalf("transport called %d times after message expiry, want 0", transport.calls)
	}
	if _, exists := memoryStore.records[message.IdempotencyKey]; exists {
		t.Fatal("expired message left its idempotency claim acquired")
	}
}

func TestCoordinatorRejectsIdempotencyKeyReuseWithDifferentContent(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	transport := &fakeTransport{}
	store := newMemoryDispatchStore()
	coordinator, err := NewCoordinator(
		DefaultValidationPolicy(),
		transport,
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	message := validMessage(t, now)
	if _, err := coordinator.Dispatch(context.Background(), message); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	message.ID = "23da87aa-8ea7-4de4-8d61-869fb9b808ed"
	message.Payload.Subject = "different content"
	message = withDigest(t, message)
	if _, err := coordinator.Dispatch(context.Background(), message); err != ErrIdempotencyConflict {
		t.Fatalf("key reuse error = %v, want %v", err, ErrIdempotencyConflict)
	}
	if transport.calls != 1 {
		t.Fatalf("conflicting content reached transport")
	}
}
