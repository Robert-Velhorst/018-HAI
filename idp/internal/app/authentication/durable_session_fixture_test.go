package authentication

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/users"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Synthetic authority only: Validate never adopts a JWT or Redis record.
type testSessionRepository struct {
	mu       sync.Mutex
	users    users.UserAccountUpdateService
	families map[uuid.UUID]models.SessionFamily
	receipts map[uuid.UUID]models.RefreshRotationReceipt
	err      error
}

func enableTestSessionAuthority(t *testing.T, svc *service) *testSessionRepository {
	t.Helper()
	r := newTestSessionRepository(svc.userService)
	svc.sessionRepository = r
	return r
}

func newTestSessionRepository(accountService users.UserAccountUpdateService) *testSessionRepository {
	return &testSessionRepository{users: accountService, families: make(map[uuid.UUID]models.SessionFamily), receipts: make(map[uuid.UUID]models.RefreshRotationReceipt)}
}

func seedTestAccessAuthority(t *testing.T, svc *service, token string) {
	t.Helper()
	if svc.sessionRepository == nil {
		enableTestSessionAuthority(t, svc)
	}
	_, claims, err := svc.parseAndValidateToken(token)
	require.NoError(t, err)
	id, err := sessionIdentityFromClaims(claims)
	require.NoError(t, err)
	user, err := svc.userService.GetUserByID(id.UserID)
	require.NoError(t, err)
	_, err = svc.sessionRepository.CreateFamily(context.Background(), *user, id)
	require.NoError(t, err)
}

func (r *testSessionRepository) Ready(context.Context) error { return r.err }

func (r *testSessionRepository) account(id irepository.SessionIdentity) (*models.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.users == nil {
		return nil, irepository.ErrSessionUserUnavailable
	}
	u, err := r.users.GetUserByID(id.UserID)
	if err != nil || u == nil || u.ID != id.UserID || !u.IsActive || (u.IsBlocked && (u.BlockedUntil == nil || time.Now().Before(*u.BlockedUntil))) {
		return nil, irepository.ErrSessionUserUnavailable
	}
	if u.SessionVersion != id.SessionVersion {
		return nil, irepository.ErrSessionRevoked
	}
	copy := *u
	return &copy, nil
}

func (r *testSessionRepository) family(id irepository.SessionIdentity) (models.SessionFamily, error) {
	f, ok := r.families[id.FamilyID]
	if !ok || f.UserID != id.UserID || f.SessionVersion != id.SessionVersion || !f.ExpiresAt.Equal(id.ExpiresAt) {
		return f, irepository.ErrSessionRevoked
	}
	return f, nil
}

func sameTestResetExpiry(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func (r *testSessionRepository) CreateFamily(_ context.Context, expected models.User, id irepository.SessionIdentity) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, err := r.account(id)
	if err != nil {
		return nil, err
	}
	if id.FamilyID == uuid.Nil || id.FamilyID != id.RefreshUUID || !id.ExpiresAt.After(time.Now()) || u.Password != expected.Password || u.Email != expected.Email || u.ResetPasswordToken != expected.ResetPasswordToken || !sameTestResetExpiry(u.ResetTokenExpires, expected.ResetTokenExpires) {
		return nil, irepository.ErrSessionRevoked
	}
	if _, ok := r.families[id.FamilyID]; ok {
		return nil, errors.New("synthetic family collision")
	}
	r.families[id.FamilyID] = models.SessionFamily{ID: id.FamilyID, UserID: id.UserID, SessionVersion: id.SessionVersion, CurrentRefreshUUID: id.RefreshUUID, ExpiresAt: id.ExpiresAt}
	return u, nil
}

func (r *testSessionRepository) Validate(_ context.Context, id irepository.SessionIdentity) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, err := r.account(id)
	if err != nil {
		return nil, err
	}
	f, err := r.family(id)
	if err != nil || f.RevokedAt != nil || f.CurrentRefreshUUID != id.RefreshUUID || !f.ExpiresAt.After(time.Now()) {
		return nil, irepository.ErrSessionRevoked
	}
	return u, nil
}

func (r *testSessionRepository) ValidateRotation(ctx context.Context, id irepository.SessionIdentity, predecessor uuid.UUID) (*models.User, error) {
	u, err := r.Validate(ctx, id)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt, exists := r.receipts[predecessor]
	family, err := r.family(id)
	if err != nil || !exists || receipt.FamilyID != id.FamilyID || receipt.ReplacementUUID != id.RefreshUUID || family.CurrentRefreshUUID != id.RefreshUUID || family.RevokedAt != nil || !time.Now().Before(receipt.ReplayUntil) {
		return nil, irepository.ErrSessionRevoked
	}
	return u, nil
}

func (r *testSessionRepository) Rotate(_ context.Context, id irepository.SessionIdentity, child uuid.UUID, pair string, grace time.Duration) (*irepository.SessionRotationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.account(id); err != nil {
		return nil, err
	}
	f, err := r.family(id)
	if err != nil {
		return nil, err
	}
	result := &irepository.SessionRotationResult{}
	now := time.Now()
	if f.RevokedAt != nil || !f.ExpiresAt.After(now) {
		result.Revoked = true
		return result, nil
	}
	if f.CurrentRefreshUUID != id.RefreshUUID {
		receipt, exists := r.receipts[id.RefreshUUID]
		if exists && receipt.FamilyID == id.FamilyID && now.Before(receipt.ReplayUntil) {
			if f.CurrentRefreshUUID == receipt.ReplacementUUID {
				result.Receipt = &receipt
			}
			return result, nil
		}
		f.RevokedAt = &now
		r.families[id.FamilyID] = f
		result.Revoked = true
		return result, nil
	}
	if child == uuid.Nil || child == id.RefreshUUID || pair == "" || grace <= 0 {
		return nil, irepository.ErrSessionRevoked
	}
	deadline := now.Add(grace)
	if f.ExpiresAt.Before(deadline) {
		deadline = f.ExpiresAt
	}
	receipt := models.RefreshRotationReceipt{PredecessorUUID: id.RefreshUUID, FamilyID: id.FamilyID, PredecessorGeneration: f.CurrentGeneration, ReplacementGeneration: f.CurrentGeneration + 1, ReplacementUUID: child, EncryptedPair: pair, ConsumedAt: now, ReplayUntil: deadline}
	r.receipts[id.RefreshUUID] = receipt
	f.CurrentRefreshUUID = child
	f.CurrentGeneration++
	r.families[id.FamilyID] = f
	result.Receipt = &receipt
	return result, nil
}

func (r *testSessionRepository) RevokeFamily(ctx context.Context, id irepository.SessionIdentity) error {
	return r.revoke(ctx, id, uuid.Nil)
}
func (r *testSessionRepository) RevokeFamilyIfCurrent(ctx context.Context, id irepository.SessionIdentity, current uuid.UUID) error {
	return r.revoke(ctx, id, current)
}
func (r *testSessionRepository) revoke(_ context.Context, id irepository.SessionIdentity, current uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	f, err := r.family(id)
	if err != nil {
		return err
	}
	if f.RevokedAt != nil || (current != uuid.Nil && f.CurrentRefreshUUID != current) {
		return nil
	}
	now := time.Now()
	f.RevokedAt = &now
	r.families[id.FamilyID] = f
	return nil
}

var _ irepository.SessionRepository = (*testSessionRepository)(nil)
