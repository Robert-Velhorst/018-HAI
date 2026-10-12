package agentcoordination

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const dispatchCleanupTimeout = 5 * time.Second

var (
	// ErrDeliveryNotAccepted may be wrapped by a transport only when it can
	// prove the remote endpoint rejected the message before accepting it.
	ErrDeliveryNotAccepted = errors.New("transport confirmed message was not accepted")
	// ErrDeliveryOutcomeUnknown means the transport may have accepted the
	// message; its idempotency claim remains held to prevent an unsafe replay.
	ErrDeliveryOutcomeUnknown = errors.New("agent coordination delivery outcome is unknown")
)

type Clock func() time.Time

type Coordinator struct {
	policy    ValidationPolicy
	transport Transport
	store     DispatchStore
	clock     Clock
}

func NewCoordinator(
	policy ValidationPolicy,
	transport Transport,
	store DispatchStore,
	clock Clock,
) (*Coordinator, error) {
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}
	if transport == nil {
		return nil, fmt.Errorf("agent coordination transport is required")
	}
	if store == nil {
		return nil, fmt.Errorf("agent coordination dispatch store is required")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Coordinator{
		policy:    policy,
		transport: transport,
		store:     store,
		clock:     clock,
	}, nil
}

// Dispatch validates before acquiring an idempotency claim. A successful
// delivery only proves transport acceptance; it does not prove task execution.
func (coordinator *Coordinator) Dispatch(
	ctx context.Context,
	message Message,
) (DeliveryReceipt, error) {
	now := coordinator.clock().UTC()
	if err := ValidateMessage(coordinator.policy, message, now); err != nil {
		return DeliveryReceipt{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return DeliveryReceipt{}, err
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, message.ExpiresAt.UTC().Sub(now))
	defer cancel()
	digest, err := ComputeMessageDigest(message)
	if err != nil {
		return DeliveryReceipt{}, fmt.Errorf("compute dispatch digest: %w", err)
	}
	claim, err := coordinator.store.Begin(
		dispatchCtx,
		message.IdempotencyKey,
		digest,
		message.ExpiresAt.UTC(),
	)
	if err != nil {
		return DeliveryReceipt{}, fmt.Errorf("begin idempotent dispatch: %w", err)
	}
	switch claim.Status {
	case DispatchClaimDuplicate:
		if claim.Receipt == nil {
			return DeliveryReceipt{}, fmt.Errorf("%w: receipt unavailable", ErrDuplicateDispatch)
		}
		receipt := *claim.Receipt
		receipt.Duplicate = true
		return receipt, nil
	case DispatchClaimConflict:
		return DeliveryReceipt{}, ErrIdempotencyConflict
	case DispatchClaimAcquired:
	default:
		return DeliveryReceipt{}, fmt.Errorf("dispatch store returned an invalid claim state")
	}
	if err := dispatchCtx.Err(); err != nil {
		if abandonErr := coordinator.abandon(message.IdempotencyKey, digest, dispatchCtx); abandonErr != nil {
			return DeliveryReceipt{}, fmt.Errorf("dispatch expired before delivery: %v; abandon idempotency claim: %w", err, abandonErr)
		}
		return DeliveryReceipt{}, fmt.Errorf("dispatch expired before delivery: %w", err)
	}
	if !message.ExpiresAt.After(coordinator.clock().UTC()) {
		if abandonErr := coordinator.abandon(message.IdempotencyKey, digest, dispatchCtx); abandonErr != nil {
			return DeliveryReceipt{}, fmt.Errorf("message expired before delivery; abandon idempotency claim: %w", abandonErr)
		}
		return DeliveryReceipt{}, fmt.Errorf("message expired before delivery")
	}

	receipt, err := coordinator.transport.Deliver(dispatchCtx, message)
	if err != nil {
		if !errors.Is(err, ErrDeliveryNotAccepted) {
			return DeliveryReceipt{}, ErrDeliveryOutcomeUnknown
		}
		if abandonErr := coordinator.abandon(message.IdempotencyKey, digest, dispatchCtx); abandonErr != nil {
			return DeliveryReceipt{}, fmt.Errorf("delivery was rejected; abandon idempotency claim: %w", abandonErr)
		}
		return DeliveryReceipt{}, ErrDeliveryNotAccepted
	}
	if receipt.MessageID != message.ID ||
		receipt.CorrelationID != message.CorrelationID ||
		receipt.AcceptedAt.IsZero() {
		return DeliveryReceipt{}, fmt.Errorf("%w: transport returned an invalid receipt", ErrDeliveryOutcomeUnknown)
	}
	if err := coordinator.store.Complete(
		dispatchCtx,
		message.IdempotencyKey,
		digest,
		receipt,
	); err != nil {
		return DeliveryReceipt{}, fmt.Errorf("complete idempotent dispatch: %w", err)
	}
	return receipt, nil
}

// abandon uses a short cleanup context detached from caller cancellation so a
// timed-out delivery can release its claim and be retried. Context values are
// preserved for stores that need request-scoped tracing or ownership metadata.
func (coordinator *Coordinator) abandon(key, digest string, parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), dispatchCleanupTimeout)
	defer cancel()
	return coordinator.store.Abandon(ctx, key, digest)
}
