package authentication

import (
	"automation-hub-idp/internal/app/models"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type unavailableRegistrationStore struct{ fakeBlockListService }

func (unavailableRegistrationStore) RegisterRefreshSession(string, string, time.Duration) error {
	return errors.New("synthetic registration store failure")
}

func TestInitialSessionNeverReturnsTokensWhenRegistrationFails(t *testing.T) {
	setupAuthConfig(t)
	user := &models.User{ID: uuid.New(), Role: "owner", IsActive: true}
	svc := &service{userService: &fakeUserService{userByID: user}, blockListService: unavailableRegistrationStore{}, logger: noopLogger{}, jwtSecret: testSigningSecret}
	enableTestSessionAuthority(t, svc)
	pair, err := svc.issueSession(user)
	require.Error(t, err)
	require.Nil(t, pair)
	token, id, expiry, err := svc.generateRefreshToken(user.ID)
	require.Error(t, err)
	require.Empty(t, token)
	require.Empty(t, id)
	require.Zero(t, expiry)
}

func TestAccessAndRefreshFailClosedWhenPositiveSessionAuthorityIsLost(t *testing.T) {
	for _, missing := range []string{"family", "refresh", "both"} {
		t.Run(missing, func(t *testing.T) {
			blocklist := &rotationHookBlockList{}
			svc, user := rotationTestService(t, blocklist)
			pair, err := svc.issueSession(user)
			require.NoError(t, err)
			ok, err := svc.IsUserAuthenticated(pair.AccessToken)
			require.NoError(t, err)
			require.True(t, ok)
			blocklist.mu.Lock()
			if missing != "refresh" {
				delete(blocklist.activeFamilies, pair.RefreshUUID)
			}
			if missing != "family" {
				delete(blocklist.activeTokens, pair.RefreshUUID)
			}
			blocklist.mu.Unlock()
			ok, err = svc.IsUserAuthenticated(pair.AccessToken)
			require.ErrorIs(t, err, ErrRevokedSession)
			require.False(t, ok)
			refreshed, err := svc.RefreshToken(pair.RefreshToken)
			require.ErrorIs(t, err, ErrRevokedSession)
			require.Nil(t, refreshed)
		})
	}
}

func TestCachedDescendantCannotBeSubstitutedForAncestorReplay(t *testing.T) {
	blocklist := &rotationHookBlockList{}
	svc, user := rotationTestService(t, blocklist)
	parent, parentID, _, err := svc.generateRefreshToken(user.ID)
	require.NoError(t, err)
	child, err := svc.RefreshToken(parent)
	require.NoError(t, err)
	grandchild, err := svc.RefreshToken(child.RefreshToken)
	require.NoError(t, err)
	blocklist.mu.Lock()
	blocklist.rotated[parentID] = blocklist.rotated[child.RefreshUUID]
	blocklist.mu.Unlock()
	pair, err := svc.RefreshToken(parent)
	require.Error(t, err)
	require.Nil(t, pair, "valid ciphertext from another predecessor must not authorize replay")
	ok, err := svc.IsUserAuthenticated(grandchild.AccessToken)
	require.NoError(t, err)
	require.True(t, ok, "rejecting a substituted cache must not revoke the live descendant")
}

func TestRotationCiphertextRequiresItsExactPredecessor(t *testing.T) {
	blocklist := &rotationHookBlockList{}
	svc, user := rotationTestService(t, blocklist)
	pair, err := svc.issueSession(user)
	require.NoError(t, err)
	id := uuid.NewString()
	ciphertext, err := svc.encryptRefreshRotationPayload(pair, id)
	require.NoError(t, err)
	decoded, err := svc.decryptRefreshRotationPayload(ciphertext, id)
	require.NoError(t, err)
	require.Equal(t, pair.RefreshUUID, decoded.RefreshUUID)
	for _, predecessor := range []string{"", uuid.NewString()} {
		decoded, err = svc.decryptRefreshRotationPayload(ciphertext, predecessor)
		require.Error(t, err)
		require.Nil(t, decoded)
	}
	_, err = svc.decryptRefreshRotationPayload("v1."+ciphertext[3:], id)
	require.Error(t, err, "unbound legacy payloads are never accepted")
}
