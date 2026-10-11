package authentication

import (
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/models"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type rotationHookBlockList struct {
	inMemoryBlockListService
	afterRotate func()
	result      string
	err         error
}

func (s *rotationHookBlockList) RotateRefreshToken(id, family, replacementID string, ttl, grace time.Duration, replacement string) (string, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	result, created, err := s.inMemoryBlockListService.RotateRefreshToken(id, family, replacementID, ttl, grace, replacement)
	if s.afterRotate != nil {
		s.afterRotate()
	}
	if s.result != "" {
		result = s.result
	}
	return result, created, err
}

func rotationTestService(t *testing.T, blocklist *rotationHookBlockList) (*service, *models.User) {
	t.Helper()
	setupAuthConfig(t)
	user := &models.User{ID: uuid.New(), Role: "owner", IsActive: true}
	svc := &service{userService: &fakeUserService{userByID: user}, blockListService: blocklist, logger: noopLogger{}, jwtSecret: testSigningSecret}
	enableTestSessionAuthority(t, svc)
	return svc, user
}

func TestRefreshRejectsLogoutOrAccountChangeDuringRotation(t *testing.T) {
	for _, accountChange := range []bool{false, true} {
		t.Run(map[bool]string{false: "logout", true: "password change"}[accountChange], func(t *testing.T) {
			blocklist := &rotationHookBlockList{}
			svc, user := rotationTestService(t, blocklist)
			original, family, _, err := svc.generateRefreshToken(user.ID)
			require.NoError(t, err)
			blocklist.afterRotate = func() {
				if accountChange {
					user.SessionVersion++
					return
				}
				require.NoError(t, blocklist.RevokeRefreshFamily(family, time.Hour))
			}
			pair, err := svc.RefreshToken(original)
			require.ErrorIs(t, err, ErrRevokedSession)
			require.Nil(t, pair, "do not return cookies for a session revoked during rotation")
		})
	}
}

func TestRefreshRejectsCachedConsumedChild(t *testing.T) {
	blocklist := &rotationHookBlockList{}
	svc, user := rotationTestService(t, blocklist)
	parent, _, _, err := svc.generateRefreshToken(user.ID)
	require.NoError(t, err)
	child, err := svc.RefreshToken(parent)
	require.NoError(t, err)
	grandchild, err := svc.RefreshToken(child.RefreshToken)
	require.NoError(t, err)
	pair, err := svc.RefreshToken(parent)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Nil(t, pair)
	ok, err := svc.IsUserAuthenticated(grandchild.AccessToken)
	require.NoError(t, err)
	require.True(t, ok, "a stale concurrent response inside grace must not revoke the live descendant")
}

func TestRefreshRejectsSwappedOrMalformedCachedPair(t *testing.T) {
	for _, kind := range []string{"other family", "other user", "other version", "expiry mismatch", "expired access", "invalid ciphertext"} {
		t.Run(kind, func(t *testing.T) {
			blocklist := &rotationHookBlockList{}
			svc, user := rotationTestService(t, blocklist)
			parent, family, expires, err := svc.generateRefreshToken(user.ID)
			require.NoError(t, err)
			predecessorID := family
			resultUser, version := user.ID, user.SessionVersion
			if kind == "other family" {
				family = uuid.NewString()
			}
			if kind == "other user" {
				resultUser = uuid.New()
			}
			if kind == "other version" {
				version++
			}
			refresh, refreshID, err := svc.generateRefreshTokenInFamilyWithVersion(resultUser, family, expires, version)
			require.NoError(t, err)
			access, accessExp, err := svc.generateAccessTokenForFamilyWithVersion(resultUser, "owner", refreshID, family, expires, version)
			require.NoError(t, err)
			other := &dto.TokenDetails{AccessToken: access, AtExpires: accessExp, RefreshToken: refresh, RefreshUUID: refreshID, RtExpires: expires}
			if kind == "expiry mismatch" {
				other.RtExpires++
			}
			if kind == "expired access" {
				_, claims, err := svc.parseAndValidateToken(access)
				require.NoError(t, err)
				other.AtExpires = time.Now().Add(-time.Second).Unix()
				claims["exp"] = other.AtExpires
				other.AccessToken, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(svc.jwtSecret))
				require.NoError(t, err)
			}
			blocklist.result, err = svc.encryptRefreshRotationPayload(other, predecessorID)
			require.NoError(t, err)
			if kind == "invalid ciphertext" {
				blocklist.result = "v2.invalid"
			}
			pair, err := svc.RefreshToken(parent)
			require.Error(t, err)
			require.Nil(t, pair)
		})
	}
}

func TestRefreshFailsClosedOnRotationStoreError(t *testing.T) {
	blocklist := &rotationHookBlockList{err: errors.New("Redis unavailable")}
	svc, user := rotationTestService(t, blocklist)
	parent, _, _, err := svc.generateRefreshToken(user.ID)
	require.NoError(t, err)
	pair, err := svc.RefreshToken(parent)
	require.Error(t, err)
	require.Nil(t, pair)
}

func TestExpiredAccessWithinJWTLeewayIsNotAuthenticated(t *testing.T) {
	blocklist := &rotationHookBlockList{}
	svc, user := rotationTestService(t, blocklist)
	pair, err := svc.issueSession(user)
	require.NoError(t, err)
	_, claims, err := svc.parseAndValidateToken(pair.AccessToken)
	require.NoError(t, err)
	claims["exp"] = time.Now().Add(-time.Second).Unix()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(svc.jwtSecret))
	require.NoError(t, err)
	ok, err := svc.IsUserAuthenticated(token)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.False(t, ok)
}

func TestLogoutRejectsMalformedRevocationClaimsBeforeWriting(t *testing.T) {
	for _, expiry := range []interface{}{1.5, "tomorrow", -1, float64(1 << 63)} {
		blocklist := &rotationHookBlockList{}
		svc, user := rotationTestService(t, blocklist)
		pair, err := svc.issueSession(user)
		require.NoError(t, err)
		_, claims, err := svc.parseAndValidateToken(pair.AccessToken)
		require.NoError(t, err)
		claims["refresh_exp"] = expiry
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(svc.jwtSecret))
		require.NoError(t, err)
		require.Error(t, svc.Logout(token))
		require.Empty(t, blocklist.blocked)
		require.Empty(t, blocklist.families)
	}
}
