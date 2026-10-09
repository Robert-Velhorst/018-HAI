package authentication

import (
	"context"
	"errors"
	"testing"

	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type downgradedFinalRotationRepository struct {
	irepository.SessionRepository
}

func (r downgradedFinalRotationRepository) ValidateRotation(ctx context.Context, id irepository.SessionIdentity, predecessor uuid.UUID) (*models.User, error) {
	user, err := r.SessionRepository.ValidateRotation(ctx, id, predecessor)
	if err != nil || user == nil {
		return user, err
	}
	current := *user
	current.Role = "viewer"
	return &current, nil
}

func TestDurableSessionFinalRotationRejectsStalePrivilegedRole(t *testing.T) {
	svc, user := rotationTestService(t, &rotationHookBlockList{})
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	svc.sessionRepository = downgradedFinalRotationRepository{SessionRepository: svc.sessionRepository}
	result, err := svc.RefreshToken(parent.RefreshToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Nil(t, result, "final durable validation must not return stale owner authority")
}

func TestDurableSessionMissingDependencyNeverFallsBackToRedis(t *testing.T) {
	svc, user := rotationTestService(t, &rotationHookBlockList{})
	pair, err := svc.issueSession(user)
	require.NoError(t, err)
	svc.sessionRepository = nil
	ok, err := svc.IsUserAuthenticated(pair.AccessToken)
	require.False(t, ok)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
	result, err := svc.RefreshToken(pair.RefreshToken)
	require.Nil(t, result)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
	result, err = svc.issueSession(user)
	require.Nil(t, result)
	require.Error(t, err)
	require.ErrorIs(t, svc.Logout(pair.AccessToken), irepository.ErrSessionAuthorityUnavailable)
}

func TestDurableSessionUnknownFamilyNeverAdoptsSignedJWT(t *testing.T) {
	svc, user := rotationTestService(t, &rotationHookBlockList{})
	pair, err := svc.issueSession(user)
	require.NoError(t, err)
	repo := enableTestSessionAuthority(t, svc) // Deliberately empty, independently of Redis.
	ok, err := svc.IsUserAuthenticated(pair.AccessToken)
	require.False(t, ok)
	require.ErrorIs(t, err, ErrRevokedSession)
	result, err := svc.RefreshToken(pair.RefreshToken)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Empty(t, repo.families)
	require.Empty(t, repo.receipts)
}

func TestDurableSessionUnavailableDependencyDeniesCachedWinner(t *testing.T) {
	svc, user := rotationTestService(t, &rotationHookBlockList{})
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	child, err := svc.RefreshToken(parent.RefreshToken)
	require.NoError(t, err)
	repo := svc.sessionRepository.(*testSessionRepository)
	repo.err = errors.New("synthetic PostgreSQL unavailable")
	ok, err := svc.IsUserAuthenticated(child.AccessToken)
	require.False(t, ok)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
	result, err := svc.RefreshToken(parent.RefreshToken)
	require.Nil(t, result)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
	require.ErrorIs(t, repo.Ready(context.Background()), repo.err)
}
