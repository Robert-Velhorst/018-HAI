package authentication

import (
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/utils"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func redisSessionIdentity(t *testing.T, svc *service, pair *dto.TokenDetails) (string, string, string) {
	t.Helper()
	_, claims, err := svc.parseAndValidateToken(pair.AccessToken)
	require.NoError(t, err)
	refresh, ok := claims["refresh_uuid"].(string)
	require.True(t, ok)
	family, ok := refreshFamilyFromClaims(claims)
	require.True(t, ok)
	access, ok := claims["access_uuid"].(string)
	require.True(t, ok)
	return refresh, family, access
}

func assertRedisSessionDenied(t *testing.T, svc *service, pair *dto.TokenDetails) {
	t.Helper()
	ok, err := svc.IsUserAuthenticated(pair.AccessToken)
	require.Error(t, err)
	require.False(t, ok, "signed access JWT must not survive missing session authority")
	result, err := svc.RefreshToken(pair.RefreshToken)
	require.Error(t, err)
	require.Nil(t, result, "never issue a replacement for denied session authority")
}

func TestRedisSessionStoreLossDoesNotReviveLoggedOutJWTs(t *testing.T) {
	svc, client := redisRotationTestService(t)
	user := svc.userService.(*fakeUserService).userByID
	original, err := svc.issueSession(user)
	require.NoError(t, err)
	child, err := svc.RefreshToken(original.RefreshToken)
	require.NoError(t, err)
	require.NoError(t, svc.Logout(child.AccessToken))
	parentID, family, parentAccess := redisSessionIdentity(t, svc, original)
	childID, childFamily, childAccess := redisSessionIdentity(t, svc, child)
	require.Equal(t, family, childFamily)
	ctx := context.Background()
	// Simulate selective eviction without clearing the DB or disposable-instance marker.
	require.NoError(t, client.Del(ctx, parentID, childID, parentAccess, childAccess,
		"refresh-family:"+family, "refresh-rotation:"+parentID, "refresh-rotation:"+childID).Err())
	assertRedisSessionDenied(t, svc, original)
	assertRedisSessionDenied(t, svc, child)
}

func TestRedisSessionStoreLossDoesNotReviveRotatedJWTs(t *testing.T) {
	svc, client := redisRotationTestService(t)
	user := svc.userService.(*fakeUserService).userByID
	original, err := svc.issueSession(user)
	require.NoError(t, err)
	child, err := svc.RefreshToken(original.RefreshToken)
	require.NoError(t, err)
	parentID, _, parentAccess := redisSessionIdentity(t, svc, original)
	require.NoError(t, client.Del(context.Background(), parentID, parentAccess, "refresh-rotation:"+parentID).Err())
	ok, err := svc.IsUserAuthenticated(original.AccessToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.False(t, ok)
	ok, err = svc.IsUserAuthenticated(child.AccessToken)
	require.NoError(t, err)
	require.True(t, ok, "selective key loss must not itself grant parent authority")
	result, err := svc.RefreshToken(original.RefreshToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Nil(t, result)
	assertRedisSessionDenied(t, svc, child)
}

func TestRedisSessionStoreLossOfPositiveAuthorityDeniesCurrentAndCachedJWTs(t *testing.T) {
	for _, missing := range []string{"family", "current refresh", "both"} {
		t.Run(missing, func(t *testing.T) {
			svc, client := redisRotationTestService(t)
			user := svc.userService.(*fakeUserService).userByID
			parent, err := svc.issueSession(user)
			require.NoError(t, err)
			child, err := svc.RefreshToken(parent.RefreshToken)
			require.NoError(t, err)
			refreshID, family, _ := redisSessionIdentity(t, svc, child)
			keys := []string{"refresh-family-active:" + family}
			if missing == "current refresh" {
				keys = []string{"refresh-active:" + refreshID}
			} else if missing == "both" {
				keys = append(keys, "refresh-active:"+refreshID)
			}
			require.NoError(t, client.Del(context.Background(), keys...).Err())
			// Even the still-present encrypted grace response must not return unauthorized JWTs.
			assertRedisSessionDenied(t, svc, parent)
			assertRedisSessionDenied(t, svc, child)
		})
	}
}

func TestRedisSessionStoreLossFreshPasswordLoginRestoresOnlyNewFamily(t *testing.T) {
	svc, client := redisRotationTestService(t)
	userService := svc.userService.(*fakeUserService)
	user := userService.userByID
	user.Email = "synthetic-" + uuid.NewString() + "@example.com"
	password := "synthetic-local-passphrase-2026"
	svc.hasher = utils.DefaultBcryptHasher()
	var err error
	user.Password, err = svc.hasher.Hash(password)
	require.NoError(t, err)
	userService.userByEmail = user
	original, err := svc.Login(user.Email, password)
	require.NoError(t, err)
	refreshID, family, _ := redisSessionIdentity(t, svc, original)
	require.NoError(t, client.Del(context.Background(), "refresh-family-active:"+family, "refresh-active:"+refreshID).Err())
	assertRedisSessionDenied(t, svc, original)
	fresh, err := svc.Login(user.Email, password)
	require.NoError(t, err)
	freshID, freshFamily, _ := redisSessionIdentity(t, svc, fresh)
	require.NotEqual(t, refreshID, freshID)
	require.NotEqual(t, family, freshFamily)
	ok, err := svc.IsUserAuthenticated(fresh.AccessToken)
	require.NoError(t, err)
	require.True(t, ok)
	rotated, err := svc.RefreshToken(fresh.RefreshToken)
	require.NoError(t, err)
	require.NotNil(t, rotated)
	assertRedisSessionDenied(t, svc, original)
	ok, err = svc.IsUserAuthenticated(rotated.AccessToken)
	require.NoError(t, err)
	require.True(t, ok, "old-session rejection must not revoke unrelated new login")
}

func TestRedisSessionStoreLossMalformedAuthorityDoesNotIssueJWTs(t *testing.T) {
	for _, kind := range []string{"family wrong value", "refresh wrong value", "family wrong type", "refresh wrong type", "family no expiry", "refresh no expiry", "cache wrong type"} {
		t.Run(kind, func(t *testing.T) {
			svc, client := redisRotationTestService(t)
			user := svc.userService.(*fakeUserService).userByID
			pair, err := svc.issueSession(user)
			require.NoError(t, err)
			refresh, family, _ := redisSessionIdentity(t, svc, pair)
			key := "refresh-family-active:" + family
			if kind == "refresh wrong value" || kind == "refresh wrong type" || kind == "refresh no expiry" {
				key = "refresh-active:" + refresh
			}
			ctx := context.Background()
			switch kind {
			case "family wrong type", "refresh wrong type":
				require.NoError(t, client.Del(ctx, key).Err())
				require.NoError(t, client.LPush(ctx, key, "wrong type").Err())
			case "family no expiry", "refresh no expiry":
				require.NoError(t, client.Persist(ctx, key).Err())
			case "cache wrong type":
				require.NoError(t, client.LPush(ctx, "refresh-rotation:"+refresh, "wrong type").Err())
			default:
				require.NoError(t, client.Set(ctx, key, "incorrect authority", time.Minute).Err())
			}
			if kind != "cache wrong type" {
				ok, err := svc.IsUserAuthenticated(pair.AccessToken)
				require.Error(t, err)
				require.False(t, ok)
			}
			result, err := svc.RefreshToken(pair.RefreshToken)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}

func TestRedisSessionStoreLossClosedClientDeniesJWTsAndNewLogin(t *testing.T) {
	svc, _ := redisRotationTestService(t)
	user := svc.userService.(*fakeUserService).userByID
	pair, err := svc.issueSession(user)
	require.NoError(t, err)
	require.NoError(t, svc.blockListService.(interface{ Close() error }).Close())
	assertRedisSessionDenied(t, svc, pair)
	result, err := svc.issueSession(user)
	require.Error(t, err)
	require.Nil(t, result, "initial session minting must fail when allowlist registration is unavailable")
}
