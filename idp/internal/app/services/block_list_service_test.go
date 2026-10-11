package services

import (
	"automation-hub-idp/internal/app/services/iservice"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func disposableRedisBlockList(t *testing.T) (*tokenBlockListServiceImpl, *redis.Client) {
	t.Helper()
	address := os.Getenv("HAI_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set HAI_TEST_REDIS_ADDR to run Redis Lua integration coverage")
	}
	host, _, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.True(t, net.ParseIP(host).IsLoopback(), "disposable Redis tests require a literal loopback address")
	instance := os.Getenv("HAI_TEST_REDIS_INSTANCE")
	require.NotEmpty(t, instance, "explicit disposable instance marker is required")
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect to disposable Redis test service: %v", err)
	}
	marker, err := client.Get(ctx, "hai-disposable-test-instance").Result()
	require.NoError(t, err)
	require.Equal(t, instance, marker, "refusing writes to an unmarked Redis instance")
	blocklist := &tokenBlockListServiceImpl{client: client, ctx: ctx}
	return blocklist, client
}

func TestRedisRefreshRotationIsAtomicReplayableAndFamilyRevocable(t *testing.T) {
	blocklist, _ := disposableRedisBlockList(t)
	ctx := blocklist.ctx
	if err := blocklist.Ping(ctx); err != nil {
		t.Fatalf("readiness ping against disposable Redis: %v", err)
	}

	refreshUUID := uuid.NewString()
	familyUUID := uuid.NewString()
	require.NoError(t, blocklist.RegisterRefreshSession(refreshUUID, familyUUID, 5*time.Minute))
	const callers = 24
	results := make([]string, callers)
	created := make([]bool, callers)
	children := make([]string, callers)
	errorsByCall := make([]error, callers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := range results {
		children[i] = uuid.NewString()
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], created[index], errorsByCall[index] = blocklist.RotateRefreshToken(
				refreshUUID,
				familyUUID,
				children[index],
				5*time.Minute,
				10*time.Second,
				fmt.Sprintf("encrypted-pair-%d", index),
			)
		}(i)
	}
	close(start)
	wait.Wait()

	winnerCount := 0
	for i := range results {
		if errorsByCall[i] != nil {
			t.Fatalf("rotation caller %d: %v", i, errorsByCall[i])
		}
		if created[i] {
			winnerCount++
			active, err := blocklist.IsRefreshSessionActive(children[i], familyUUID)
			require.NoError(t, err)
			require.True(t, active, "only the winner's replacement gains session authority")
		} else {
			active, err := blocklist.IsRefreshSessionActive(children[i], familyUUID)
			require.NoError(t, err)
			require.False(t, active, "a losing replacement must not gain session authority")
		}
	}
	if winnerCount != 1 {
		t.Fatalf("expected exactly one atomic rotation winner, got %d", winnerCount)
	}
	for i := 1; i < len(results); i++ {
		if results[i] != results[0] {
			t.Fatalf("concurrent callers received different replacement pairs: %q != %q", results[i], results[0])
		}
	}
	blocked, err := blocklist.IsInBlockList(refreshUUID)
	if err != nil || !blocked {
		t.Fatalf("rotated refresh token was not consumed: blocked=%t err=%v", blocked, err)
	}

	if err := blocklist.RevokeRefreshFamily(familyUUID, time.Minute); err != nil {
		t.Fatalf("revoke refresh family: %v", err)
	}
	value, deniedCreated, err := blocklist.RotateRefreshToken(refreshUUID, familyUUID, uuid.NewString(), time.Minute, time.Second, "must-not-escape")
	if err != nil {
		t.Fatalf("rotation after family revocation: %v", err)
	}
	if value != "" || deniedCreated {
		t.Fatal("family revocation must deny even a cached refresh replay")
	}
	revoked, err := blocklist.IsRefreshFamilyRevoked(familyUUID)
	if err != nil || !revoked {
		t.Fatalf("refresh family revocation not visible: revoked=%t err=%v", revoked, err)
	}

	var _ iservice.TokenBlockListService = blocklist
}

func TestRedisRefreshReplayAfterGraceRevokesDescendants(t *testing.T) {
	blocklist, client := disposableRedisBlockList(t)
	parent, child, family := uuid.NewString(), uuid.NewString(), uuid.NewString()
	require.NoError(t, blocklist.RegisterRefreshSession(parent, family, time.Minute))
	_, created, err := blocklist.RotateRefreshToken(parent, family, child, time.Minute, 40*time.Millisecond, "child")
	require.NoError(t, err)
	require.True(t, created)
	require.Eventually(t, func() bool {
		return client.Exists(blocklist.ctx, refreshRotationKey(parent)).Val() == 0
	}, time.Second, 10*time.Millisecond)
	value, created, err := blocklist.RotateRefreshToken(parent, family, uuid.NewString(), time.Minute, time.Second, "replay")
	require.NoError(t, err)
	require.Empty(t, value)
	require.False(t, created)
	revoked, err := blocklist.IsRefreshFamilyRevoked(family)
	require.NoError(t, err)
	require.True(t, revoked, "reuse outside grace must revoke the entire family")
	value, created, err = blocklist.RotateRefreshToken(child, family, uuid.NewString(), time.Minute, time.Second, "grandchild")
	require.NoError(t, err)
	require.Empty(t, value)
	require.False(t, created)
}

func TestRedisRevocationNeverShortensExistingLifetime(t *testing.T) {
	blocklist, client := disposableRedisBlockList(t)
	for _, family := range []bool{false, true} {
		id := uuid.NewString()
		key := id
		revoke := blocklist.AddToBlockList
		if family {
			key = refreshFamilyKey(id)
			revoke = blocklist.RevokeRefreshFamily
		}
		require.NoError(t, revoke(id, time.Minute))
		require.NoError(t, revoke(id, time.Millisecond))
		require.Greater(t, client.PTTL(blocklist.ctx, key).Val(), 50*time.Second)
		require.NoError(t, client.Persist(blocklist.ctx, key).Err())
		require.NoError(t, revoke(id, time.Minute))
		require.Equal(t, time.Duration(-1), client.PTTL(blocklist.ctx, key).Val(), "permanent revocations must remain permanent")
	}
}

func TestRedisExplicitTokenRevocationInvalidatesCachedReplay(t *testing.T) {
	blocklist, client := disposableRedisBlockList(t)
	parent, family := uuid.NewString(), uuid.NewString()
	require.NoError(t, blocklist.RegisterRefreshSession(parent, family, time.Minute))
	_, _, err := blocklist.RotateRefreshToken(parent, family, uuid.NewString(), time.Minute, time.Second, "child")
	require.NoError(t, err)
	require.NoError(t, blocklist.AddToBlockList(parent, time.Millisecond))
	require.Greater(t, client.PTTL(blocklist.ctx, parent).Val(), 50*time.Second)
	require.Zero(t, client.Exists(blocklist.ctx, refreshRotationKey(parent)).Val())
	value, created, err := blocklist.RotateRefreshToken(parent, family, uuid.NewString(), time.Minute, time.Second, "must-not-escape")
	require.NoError(t, err)
	require.Empty(t, value)
	require.False(t, created)
}

func TestRedisRotationStoreErrorsDoNotIssueReplacement(t *testing.T) {
	blocklist, client := disposableRedisBlockList(t)
	parent, family := uuid.NewString(), uuid.NewString()
	require.NoError(t, blocklist.RegisterRefreshSession(parent, family, time.Minute))
	require.NoError(t, client.LPush(blocklist.ctx, refreshRotationKey(parent), "wrong type").Err())
	value, created, err := blocklist.RotateRefreshToken(parent, family, uuid.NewString(), time.Minute, time.Second, "child")
	require.Error(t, err)
	require.Empty(t, value)
	require.False(t, created)
	require.Zero(t, client.Exists(blocklist.ctx, parent).Val(), "Lua read error must precede token consumption")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	blocklist.ctx = cancelled
	value, created, err = blocklist.RotateRefreshToken(uuid.NewString(), family, uuid.NewString(), time.Minute, time.Second, "child")
	require.Error(t, err)
	require.Empty(t, value)
	require.False(t, created)
}

func TestRedisBlockListOperationsFailClosedWithoutClient(t *testing.T) {
	for _, blocklist := range []*tokenBlockListServiceImpl{nil, {}} {
		require.Error(t, blocklist.RegisterRefreshSession("id", "family", time.Minute))
		active, err := blocklist.IsRefreshSessionActive("id", "family")
		require.Error(t, err)
		require.False(t, active)
		require.Error(t, blocklist.AddToBlockList("id", time.Minute))
		require.Error(t, blocklist.RevokeRefreshFamily("family", time.Minute))
		blocked, err := blocklist.IsInBlockList("id")
		require.Error(t, err)
		require.False(t, blocked)
		revoked, err := blocklist.IsRefreshFamilyRevoked("family")
		require.Error(t, err)
		require.False(t, revoked)
		value, created, err := blocklist.RotateRefreshToken("id", "family", "child", time.Minute, time.Second, "replacement")
		require.Error(t, err)
		require.Empty(t, value)
		require.False(t, created)
	}
}

func TestRedisBlockListPingFailsClosedWithoutClient(t *testing.T) {
	blocklist := &tokenBlockListServiceImpl{}
	if err := blocklist.Ping(context.Background()); err == nil {
		t.Fatal("Ping() must fail when the Redis client is missing")
	}
}

func TestRedisSessionAuthorityLossDeniesAuthorizationAndRotation(t *testing.T) {
	for _, lost := range []string{"family", "refresh", "both"} {
		t.Run(lost, func(t *testing.T) {
			blocklist, client := disposableRedisBlockList(t)
			refresh, family, child := uuid.NewString(), uuid.NewString(), uuid.NewString()
			require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
			keys := []string{activeRefreshFamilyKey(family)}
			if lost == "refresh" {
				keys = []string{activeRefreshTokenKey(refresh)}
			} else if lost == "both" {
				keys = append(keys, activeRefreshTokenKey(refresh))
			}
			require.NoError(t, client.Del(blocklist.ctx, keys...).Err())
			active, err := blocklist.IsRefreshSessionActive(refresh, family)
			require.NoError(t, err)
			require.False(t, active)
			value, created, err := blocklist.RotateRefreshToken(refresh, family, child, time.Minute, time.Second, "must-not-escape")
			require.NoError(t, err)
			require.Empty(t, value)
			require.False(t, created)
			require.Zero(t, client.Exists(blocklist.ctx, activeRefreshTokenKey(child)).Val())
		})
	}
}

func TestRedisLosingNegativeRecordsDoesNotRestoreConsumedAuthority(t *testing.T) {
	for _, kind := range []string{"token revoked", "family revoked", "rotated"} {
		t.Run(kind, func(t *testing.T) {
			blocklist, client := disposableRedisBlockList(t)
			refresh, family, child := uuid.NewString(), uuid.NewString(), uuid.NewString()
			require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
			switch kind {
			case "token revoked":
				require.NoError(t, blocklist.AddToBlockList(refresh, time.Minute))
			case "family revoked":
				require.NoError(t, blocklist.RevokeRefreshFamily(family, time.Minute))
			case "rotated":
				value, created, err := blocklist.RotateRefreshToken(refresh, family, child, time.Minute, time.Second, "child")
				require.NoError(t, err)
				require.True(t, created)
				require.Equal(t, "child", value)
			}
			// Delete only this synthetic session's negative/cache keys, never the DB or marker.
			require.NoError(t, client.Del(blocklist.ctx, refresh, refreshFamilyKey(family), refreshRotationKey(refresh)).Err())
			active, err := blocklist.IsRefreshSessionActive(refresh, family)
			require.NoError(t, err)
			require.False(t, active)
			value, created, err := blocklist.RotateRefreshToken(refresh, family, uuid.NewString(), time.Minute, time.Second, "must-not-escape")
			require.NoError(t, err)
			require.Empty(t, value)
			require.False(t, created)
		})
	}
}

func TestRedisRefreshRegistrationCollisionDoesNotExtendAuthority(t *testing.T) {
	blocklist, client := disposableRedisBlockList(t)
	refresh, family := uuid.NewString(), uuid.NewString()
	require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
	beforeFamily := client.PTTL(blocklist.ctx, activeRefreshFamilyKey(family)).Val()
	beforeRefresh := client.PTTL(blocklist.ctx, activeRefreshTokenKey(refresh)).Val()
	require.Positive(t, beforeFamily)
	require.Positive(t, beforeRefresh)
	require.Error(t, blocklist.RegisterRefreshSession(refresh, family, time.Hour))
	require.LessOrEqual(t, client.PTTL(blocklist.ctx, activeRefreshFamilyKey(family)).Val(), beforeFamily)
	require.LessOrEqual(t, client.PTTL(blocklist.ctx, activeRefreshTokenKey(refresh)).Val(), beforeRefresh)
	otherRefresh, otherFamily := uuid.NewString(), uuid.NewString()
	require.Error(t, blocklist.RegisterRefreshSession(otherRefresh, family, time.Hour))
	require.Zero(t, client.Exists(blocklist.ctx, activeRefreshTokenKey(otherRefresh)).Val())
	require.Error(t, blocklist.RegisterRefreshSession(refresh, otherFamily, time.Hour))
	require.Zero(t, client.Exists(blocklist.ctx, activeRefreshFamilyKey(otherFamily)).Val())
}

func TestRedisRotationCannotExtendFamilyLifetime(t *testing.T) {
	blocklist, client := disposableRedisBlockList(t)
	refresh, family, child := uuid.NewString(), uuid.NewString(), uuid.NewString()
	require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
	before := client.PTTL(blocklist.ctx, activeRefreshFamilyKey(family)).Val()
	value, created, err := blocklist.RotateRefreshToken(refresh, family, child, time.Hour, time.Hour, "child")
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "child", value)
	for _, key := range []string{activeRefreshFamilyKey(family), activeRefreshTokenKey(child), refreshRotationKey(refresh)} {
		ttl := client.PTTL(blocklist.ctx, key).Val()
		require.Positive(t, ttl)
		require.LessOrEqual(t, ttl, before, "rotation must not outlive original family authority")
	}
}

func TestRedisRotationCannotExtendShorterParentAuthority(t *testing.T) {
	blocklist, client := disposableRedisBlockList(t)
	refresh, family, child := uuid.NewString(), uuid.NewString(), uuid.NewString()
	require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
	require.NoError(t, client.PExpire(blocklist.ctx, activeRefreshTokenKey(refresh), 10*time.Second).Err())
	before := client.PTTL(blocklist.ctx, activeRefreshTokenKey(refresh)).Val()
	require.Positive(t, before)
	value, created, err := blocklist.RotateRefreshToken(refresh, family, child, time.Hour, time.Minute, "child")
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "child", value)
	for _, key := range []string{activeRefreshTokenKey(child), refreshRotationKey(refresh)} {
		ttl := client.PTTL(blocklist.ctx, key).Val()
		require.Positive(t, ttl)
		require.LessOrEqual(t, ttl, before, "replacement authority and grace cannot extend consumed parent's lifetime")
	}
}

func TestRedisReplacementCollisionDoesNotConsumeParent(t *testing.T) {
	for _, collision := range []string{"active", "revoked"} {
		t.Run(collision, func(t *testing.T) {
			blocklist, client := disposableRedisBlockList(t)
			refresh, family, child := uuid.NewString(), uuid.NewString(), uuid.NewString()
			require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
			if collision == "active" {
				require.NoError(t, client.Set(blocklist.ctx, activeRefreshTokenKey(child), uuid.NewString(), time.Minute).Err())
			} else {
				require.NoError(t, blocklist.AddToBlockList(child, time.Minute))
			}
			value, created, err := blocklist.RotateRefreshToken(refresh, family, child, time.Minute, time.Second, "must-not-escape")
			require.NoError(t, err)
			require.Empty(t, value)
			require.False(t, created)
			active, err := blocklist.IsRefreshSessionActive(refresh, family)
			require.NoError(t, err)
			require.True(t, active, "replacement collision must not consume valid original authority")
			require.Zero(t, client.Exists(blocklist.ctx, refresh, refreshRotationKey(refresh)).Val())
		})
	}
}

func TestRedisMalformedSessionAuthorityFailsClosed(t *testing.T) {
	for _, kind := range []string{"wrong family value", "wrong refresh value", "family list", "refresh list", "permanent family", "permanent refresh"} {
		t.Run(kind, func(t *testing.T) {
			blocklist, client := disposableRedisBlockList(t)
			refresh, family := uuid.NewString(), uuid.NewString()
			require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
			key := activeRefreshFamilyKey(family)
			if kind == "wrong refresh value" || kind == "refresh list" || kind == "permanent refresh" {
				key = activeRefreshTokenKey(refresh)
			}
			switch kind {
			case "family list", "refresh list":
				require.NoError(t, client.Del(blocklist.ctx, key).Err())
				require.NoError(t, client.LPush(blocklist.ctx, key, "wrong type").Err())
			case "permanent family", "permanent refresh":
				require.NoError(t, client.Persist(blocklist.ctx, key).Err())
			default:
				require.NoError(t, client.Set(blocklist.ctx, key, "wrong authority", time.Minute).Err())
			}
			active, _ := blocklist.IsRefreshSessionActive(refresh, family)
			require.False(t, active, "malformed authority must never authenticate")
			value, created, _ := blocklist.RotateRefreshToken(refresh, family, uuid.NewString(), time.Minute, time.Second, "must-not-escape")
			require.Empty(t, value)
			require.False(t, created)
		})
	}
}

func TestRedisRefreshRegistrationRejectsExistingRevocationsAndWrongTypes(t *testing.T) {
	for _, kind := range []string{"refresh revoked", "family revoked", "family list", "refresh list"} {
		t.Run(kind, func(t *testing.T) {
			blocklist, client := disposableRedisBlockList(t)
			refresh, family := uuid.NewString(), uuid.NewString()
			switch kind {
			case "refresh revoked":
				require.NoError(t, blocklist.AddToBlockList(refresh, time.Minute))
			case "family revoked":
				require.NoError(t, blocklist.RevokeRefreshFamily(family, time.Minute))
			case "family list":
				require.NoError(t, client.LPush(blocklist.ctx, activeRefreshFamilyKey(family), "wrong type").Err())
			case "refresh list":
				require.NoError(t, client.LPush(blocklist.ctx, activeRefreshTokenKey(refresh), "wrong type").Err())
			}
			require.Error(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
			active, _ := blocklist.IsRefreshSessionActive(refresh, family)
			require.False(t, active)
		})
	}
}

func TestRedisSessionAuthorityValidationAndOutage(t *testing.T) {
	blocklist, client := disposableRedisBlockList(t)
	refresh, family := uuid.NewString(), uuid.NewString()
	for _, invalid := range []struct {
		refresh, family string
		ttl             time.Duration
	}{
		{"", family, time.Minute}, {refresh, "", time.Minute}, {" ", family, time.Minute},
		{refresh, family, 0}, {refresh, family, -time.Second},
	} {
		require.Error(t, blocklist.RegisterRefreshSession(invalid.refresh, invalid.family, invalid.ttl))
	}
	for _, invalid := range [][2]string{{"", family}, {refresh, ""}, {" ", family}} {
		active, err := blocklist.IsRefreshSessionActive(invalid[0], invalid[1])
		require.Error(t, err)
		require.False(t, active)
	}
	require.Zero(t, client.Exists(blocklist.ctx, activeRefreshFamilyKey(family), activeRefreshTokenKey(refresh)).Val())
	require.NoError(t, client.Close())
	require.Error(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
	active, err := blocklist.IsRefreshSessionActive(refresh, family)
	require.Error(t, err)
	require.False(t, active)
	value, created, err := blocklist.RotateRefreshToken(refresh, family, uuid.NewString(), time.Minute, time.Second, "must-not-escape")
	require.Error(t, err)
	require.Empty(t, value)
	require.False(t, created)
}

func TestRedisSessionScriptPartialWriteFailureFailsClosed(t *testing.T) {
	for _, operation := range []string{"register", "rotate", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			blocklist, client := disposableRedisBlockList(t)
			refresh, family, child := uuid.NewString(), uuid.NewString(), uuid.NewString()
			if operation != "register" {
				require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
			}
			username, password := "hai-test-"+uuid.NewString(), uuid.NewString()
			// Permission faults exercise actual Lua partial writes without production injection hooks.
			// Only this guarded disposable instance and these synthetic session keys are accessible.
			args := []interface{}{"ACL", "SETUSER", username, "reset", "on", ">" + password,
				"+eval", "+evalsha", "+script|load", "+exists", "+get", "+pttl", "+del"}
			for _, key := range []string{refresh, refreshFamilyKey(family), refreshRotationKey(refresh),
				activeRefreshFamilyKey(family), activeRefreshTokenKey(refresh), activeRefreshTokenKey(child), child} {
				args = append(args, "~"+key)
			}
			require.NoError(t, client.Do(blocklist.ctx, args...).Err())
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, client.Do(ctx, "ACL", "DELUSER", username).Err())
			})
			limitedClient := redis.NewClient(&redis.Options{Addr: client.Options().Addr, Username: username, Password: password})
			t.Cleanup(func() { _ = limitedClient.Close() })
			limited := &tokenBlockListServiceImpl{client: limitedClient, ctx: blocklist.ctx}
			switch operation {
			case "register":
				require.Error(t, limited.RegisterRefreshSession(refresh, family, time.Minute))
			case "rotate":
				value, created, err := limited.RotateRefreshToken(refresh, family, child, time.Minute, time.Second, "must-not-escape")
				require.Error(t, err)
				require.Empty(t, value)
				require.False(t, created)
				require.Zero(t, client.Exists(blocklist.ctx, activeRefreshTokenKey(refresh)).Val(), "consume-before-write must remove authority even if following SET fails")
			case "revoke":
				require.Error(t, limited.RevokeRefreshFamily(family, time.Minute))
				require.Zero(t, client.Exists(blocklist.ctx, activeRefreshFamilyKey(family)).Val(), "family authority must be removed before fallible negative writes")
			}
			active, err := blocklist.IsRefreshSessionActive(refresh, family)
			require.NoError(t, err)
			require.False(t, active)
			value, created, err := blocklist.RotateRefreshToken(refresh, family, uuid.NewString(), time.Minute, time.Second, "must-not-escape")
			require.NoError(t, err)
			require.Empty(t, value)
			require.False(t, created)
		})
	}
}

func TestRedisSessionScriptLaterWriteFailureFailsClosed(t *testing.T) {
	for _, operation := range []string{"register refresh write", "rotate replay cache write"} {
		t.Run(operation, func(t *testing.T) {
			blocklist, client := disposableRedisBlockList(t)
			refresh, family, child := uuid.NewString(), uuid.NewString(), uuid.NewString()
			allKeys := []string{refresh, refreshFamilyKey(family), refreshRotationKey(refresh),
				activeRefreshFamilyKey(family), activeRefreshTokenKey(refresh), activeRefreshTokenKey(child), child}
			writeKeys := []string{activeRefreshFamilyKey(family)}
			if operation == "rotate replay cache write" {
				require.NoError(t, blocklist.RegisterRefreshSession(refresh, family, time.Minute))
				writeKeys = []string{refresh, activeRefreshTokenKey(child)}
			}
			username, password := "hai-test-"+uuid.NewString(), uuid.NewString()
			// EVAL preflight requires every declared key in the primary permissions.
			// A separate selector permits SET only for writes before the injected fault.
			args := []interface{}{"ACL", "SETUSER", username, "reset", "on", ">" + password,
				"+eval", "+evalsha", "+script|load", "+exists", "+get", "+pttl", "+del"}
			for _, key := range allKeys {
				args = append(args, "~"+key)
			}
			selector := []string{"+set"}
			for _, key := range writeKeys {
				selector = append(selector, "~"+key)
			}
			args = append(args, "("+strings.Join(selector, " ")+")")
			require.NoError(t, client.Do(blocklist.ctx, args...).Err())
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, client.Do(ctx, "ACL", "DELUSER", username).Err())
			})
			limitedClient := redis.NewClient(&redis.Options{Addr: client.Options().Addr, Username: username, Password: password})
			t.Cleanup(func() { _ = limitedClient.Close() })
			limited := &tokenBlockListServiceImpl{client: limitedClient, ctx: blocklist.ctx}
			if operation == "register refresh write" {
				err := limited.RegisterRefreshSession(refresh, family, time.Minute)
				require.Error(t, err)
				require.ErrorContains(t, err, "ACL failure in script:")
				require.Contains(t, err.Error(), "@user_script:", "failure must occur inside Lua, not EVAL key preflight")
				require.Equal(t, "1", client.Get(blocklist.ctx, activeRefreshFamilyKey(family)).Val(), "family SET must have succeeded before denied refresh SET")
				require.Positive(t, client.PTTL(blocklist.ctx, activeRefreshFamilyKey(family)).Val())
				require.Zero(t, client.Exists(blocklist.ctx, activeRefreshTokenKey(refresh)).Val())
				active, err := blocklist.IsRefreshSessionActive(refresh, family)
				require.NoError(t, err)
				require.False(t, active, "orphaned family alone must never grant session authority")
				require.Error(t, blocklist.RegisterRefreshSession(refresh, family, time.Hour), "registration must not silently adopt incomplete authority")
			} else {
				value, created, err := limited.RotateRefreshToken(refresh, family, child, time.Minute, time.Second, "must-not-escape")
				require.Error(t, err)
				require.ErrorContains(t, err, "ACL failure in script:")
				require.Contains(t, err.Error(), "@user_script:", "failure must occur inside Lua, not EVAL key preflight")
				require.Empty(t, value, "failed replay cache write must not return replacement payload")
				require.False(t, created)
				require.Zero(t, client.Exists(blocklist.ctx, activeRefreshTokenKey(refresh)).Val())
				require.Equal(t, "1", client.Get(blocklist.ctx, refresh).Val(), "negative SET must have succeeded before denied cache SET")
				require.Equal(t, family, client.Get(blocklist.ctx, activeRefreshTokenKey(child)).Val(), "child authority SET must have succeeded before denied cache SET")
				require.Zero(t, client.Exists(blocklist.ctx, refreshRotationKey(refresh)).Val())
				// Even losing the successful negative write must not restore consumed parent authority.
				require.NoError(t, client.Del(blocklist.ctx, refresh).Err())
			}
			active, err := blocklist.IsRefreshSessionActive(refresh, family)
			require.NoError(t, err)
			require.False(t, active)
			value, created, err := blocklist.RotateRefreshToken(refresh, family, uuid.NewString(), time.Minute, time.Second, "must-not-escape")
			require.NoError(t, err)
			require.Empty(t, value)
			require.False(t, created)
			active, err = blocklist.IsRefreshSessionActive(child, family)
			require.NoError(t, err)
			require.False(t, active, "reuse denial must invalidate any unreachable child created before the partial error")
		})
	}
}

func TestRedisBlockListPingHonorsCallerDeadline(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: 0})
	t.Cleanup(func() { _ = client.Close() })
	blocklist := &tokenBlockListServiceImpl{client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := blocklist.Ping(ctx); err == nil {
		t.Fatal("Ping() must fail when Redis is unavailable")
	}
}
