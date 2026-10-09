package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type claimServiceAuthorityRepository struct {
	Repository
	calls int
}

func (r *claimServiceAuthorityRepository) ClaimNext(context.Context, string, string, uuid.UUID, time.Duration) (*ClaimedOperation, error) {
	r.calls++
	return nil, ErrClaimLost
}
func (r *claimServiceAuthorityRepository) ClaimOperation(context.Context, string, string, uuid.UUID, uuid.UUID, time.Duration) (*ClaimedOperation, error) {
	r.calls++
	return nil, ErrClaimLost
}
func (r *claimServiceAuthorityRepository) RenewClaim(context.Context, ExecutionClaim, time.Duration) error {
	r.calls++
	return ErrClaimLost
}
func (r *claimServiceAuthorityRepository) TransitionClaimed(context.Context, ExecutionClaim, models.Operation, models.OperationEvent, bool) (*models.Operation, error) {
	r.calls++
	return nil, ErrClaimLost
}
func (r *claimServiceAuthorityRepository) ReleaseClaim(context.Context, ExecutionClaim) error {
	r.calls++
	return ErrClaimLost
}
func (r *claimServiceAuthorityRepository) RecoverExpiredClaims(context.Context, string, string, int) (RecoveryResult, error) {
	r.calls++
	return RecoveryResult{}, ErrClaimLost
}

func TestClaimServiceChecksAuthorityBeforeDelegating(t *testing.T) {
	f := newClaimedEffectFixture(t, true)
	repo := &claimServiceAuthorityRepository{Repository: f.repo}
	service := NewService(repo)
	calls := map[string]func(context.Context) error{
		"next": func(ctx context.Context) error {
			_, err := service.ClaimNext(ctx, f.op.OwnerUserID, f.op.WorkspaceID, uuid.New(), time.Minute)
			return err
		},
		"explicit": func(ctx context.Context) error {
			_, err := service.ClaimOperation(ctx, f.op.OwnerUserID, f.op.WorkspaceID, f.op.ID, uuid.New(), time.Minute)
			return err
		},
		"renew": func(ctx context.Context) error { return service.RenewClaim(ctx, f.claim, time.Minute) },
		"transition": func(ctx context.Context) error {
			_, err := service.TransitionClaimed(ctx, f.claim, f.op, StatusInterrupted, "test", "", "refusal test")
			return err
		},
		"release": func(ctx context.Context) error { return service.ReleaseClaim(ctx, f.claim) },
		"recover": func(ctx context.Context) error {
			_, err := service.RecoverExpiredClaims(ctx, f.op.OwnerUserID, f.op.WorkspaceID, 10)
			return err
		},
	}
	for _, kind := range []string{"nil", "canceled", "delayed_expiry", "ended_observation", "safe_ended"} {
		for name, call := range calls {
			t.Run(kind+"/"+name, func(t *testing.T) {
				ctx, want := rejectedClaimAuthority(t, kind)
				repo.calls = 0
				if err := call(ctx); !errors.Is(err, want) || repo.calls != 0 {
					t.Fatalf("invalid authority delegated: %v / calls=%d", err, repo.calls)
				}
			})
		}
	}
	for name, call := range calls {
		t.Run("explicit_background/"+name, func(t *testing.T) {
			repo.calls = 0
			if err := call(context.Background()); !errors.Is(err, ErrClaimLost) || repo.calls != 1 {
				t.Fatalf("healthy context was not delegated exactly once: %v / calls=%d", err, repo.calls)
			}
		})
	}
}
