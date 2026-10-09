package authentication

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/services/iservice"
	"automation-hub-idp/internal/app/users"
	"automation-hub-idp/internal/infra"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Both stores are real and guarded by existing disposable-instance fixtures.
// DUMP/RESTORE models old-state restoration; it is not a process-crash/fsync test.
func durableSessionStores(t *testing.T) (*service, *gorm.DB, *redis.Client, *models.User) {
	t.Helper()
	if os.Getenv("HAI_IDP_TEST_DATABASE_URL") == "" || os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS") != "true" || os.Getenv("HAI_TEST_REDIS_ADDR") == "" || os.Getenv("HAI_TEST_REDIS_INSTANCE") == "" {
		t.Skip("requires explicitly opted-in disposable PostgreSQL and marked Redis")
	}
	if err := validateIDPAuthTestDatabaseURL(os.Getenv("HAI_IDP_TEST_DATABASE_URL"), os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")); err != nil {
		t.Fatal(err)
	}
	// Check the Redis marker before opening the isolated database fixture.
	svc, client := redisRotationTestService(t)
	db := openPasswordResetTestDB(t)
	user := &models.User{ID: uuid.New(), Email: "durable-" + uuid.NewString() + "@example.test", Password: "synthetic-hash", Role: "owner", IsActive: true, SessionVersion: 3}
	require.NoError(t, db.Create(user).Error)
	svc.userService = users.NewUserService(repositories.NewGormUserRepository(db, noopLogger{}), noopLogger{}, integrationPasswordHasher{})
	svc.sessionRepository = repositories.NewGormSessionRepository(db)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	svc.databaseReady = sqlDB.PingContext
	return svc, db, client, user
}

func durableIdentity(t *testing.T, svc *service, token string) irepository.SessionIdentity {
	t.Helper()
	_, claims, err := svc.parseAndValidateToken(token)
	require.NoError(t, err)
	id, err := sessionIdentityFromClaims(claims)
	require.NoError(t, err)
	return id
}

func durableSessionKeys(t *testing.T, svc *service, pairs ...*dto.TokenDetails) []string {
	t.Helper()
	seen := make(map[string]bool)
	var keys []string
	for _, pair := range pairs {
		refresh, family, access := redisSessionIdentity(t, svc, pair)
		for _, key := range []string{refresh, access, "refresh-family:" + family, "refresh-family-active:" + family, "refresh-active:" + refresh, "refresh-rotation:" + refresh} {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys
}

type durableRedisSnapshotValue struct {
	dump    string
	expires time.Time
}
type durableRedisSnapshot map[string]durableRedisSnapshotValue

func captureDurableRedisSnapshot(t *testing.T, client *redis.Client, keys []string) durableRedisSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot := make(durableRedisSnapshot)
	for _, key := range keys {
		value, err := client.Dump(ctx, key).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		require.NoError(t, err)
		ttl, err := client.PTTL(ctx, key).Result()
		require.NoError(t, err)
		require.Positive(t, ttl)
		snapshot[key] = durableRedisSnapshotValue{dump: value, expires: time.Now().Add(ttl)}
	}
	return snapshot
}

func restoreDurableRedisSnapshot(t *testing.T, client *redis.Client, snapshot durableRedisSnapshot, ownedKeys []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Only synthetic session keys are touched; never flush the DB or marker.
	require.NoError(t, client.Del(ctx, ownedKeys...).Err())
	for key, value := range snapshot {
		ttl := time.Until(value.expires)
		if ttl > 0 {
			require.NoError(t, client.RestoreReplace(ctx, key, ttl, value.dump).Err())
		}
	}
}

func readDurableFamily(t *testing.T, db *gorm.DB, id uuid.UUID) models.SessionFamily {
	t.Helper()
	var family models.SessionFamily
	require.NoError(t, db.First(&family, "id = ?", id).Error)
	return family
}

func expireDurableReceipt(t *testing.T, db *gorm.DB, parent uuid.UUID) {
	t.Helper()
	result := db.Exec("UPDATE refresh_rotation_receipts SET consumed_at = clock_timestamp() - interval '10 seconds', replay_until = clock_timestamp() - interval '1 second' WHERE predecessor_uuid = ?", parent)
	require.NoError(t, result.Error)
	require.EqualValues(t, 1, result.RowsAffected)
}

func TestDurableSessionOldSnapshotCannotUndoLogout(t *testing.T) {
	svc, db, client, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	otherDevice, err := svc.issueSession(user)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	snapshot := captureDurableRedisSnapshot(t, client, durableSessionKeys(t, svc, parent))
	child, err := svc.RefreshToken(parent.RefreshToken)
	require.NoError(t, err)
	require.NoError(t, svc.Logout(parent.AccessToken), "pre-rotation access must revoke the whole device family")
	restoreDurableRedisSnapshot(t, client, snapshot, durableSessionKeys(t, svc, parent, child))
	assertRedisSessionDenied(t, svc, parent)
	assertRedisSessionDenied(t, svc, child)
	require.NotNil(t, readDurableFamily(t, db, id.FamilyID).RevokedAt)
	ok, err := svc.IsUserAuthenticated(otherDevice.AccessToken)
	require.NoError(t, err)
	require.True(t, ok)
	var stored models.User
	require.NoError(t, db.First(&stored, "id = ?", user.ID).Error)
	require.Equal(t, user.SessionVersion, stored.SessionVersion, "device logout is not global user logout")
}

func TestDurableSessionOldSnapshotCannotBranchConsumedRefresh(t *testing.T) {
	svc, db, client, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	snapshot := captureDurableRedisSnapshot(t, client, durableSessionKeys(t, svc, parent))
	child, err := svc.RefreshToken(parent.RefreshToken)
	require.NoError(t, err)
	restoreDurableRedisSnapshot(t, client, snapshot, durableSessionKeys(t, svc, parent, child))
	ok, err := svc.IsUserAuthenticated(parent.AccessToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.False(t, ok)
	recovered, err := svc.RefreshToken(parent.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, child.RefreshToken, recovered.RefreshToken)
	require.Equal(t, child.AccessToken, recovered.AccessToken)
	family := readDurableFamily(t, db, id.FamilyID)
	require.EqualValues(t, 1, family.CurrentGeneration)
	require.Equal(t, uuid.MustParse(child.RefreshUUID), family.CurrentRefreshUUID)
	var count int64
	require.NoError(t, db.Model(&models.RefreshRotationReceipt{}).Where("family_id = ?", id.FamilyID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestDurableSessionRestoredGraceCannotUndoReplayRevocation(t *testing.T) {
	svc, db, client, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	child, err := svc.RefreshToken(parent.RefreshToken)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	keys := durableSessionKeys(t, svc, parent, child)
	snapshot := captureDurableRedisSnapshot(t, client, keys)
	expireDurableReceipt(t, db, id.RefreshUUID)
	// Redis still has its unexpired encrypted response; SQL decides eligibility.
	restoreDurableRedisSnapshot(t, client, snapshot, keys)
	pair, err := svc.RefreshToken(parent.RefreshToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Nil(t, pair)
	require.NotNil(t, readDurableFamily(t, db, id.FamilyID).RevokedAt, "revocation must survive the returned error")
	restoreDurableRedisSnapshot(t, client, snapshot, keys)
	assertRedisSessionDenied(t, svc, child)
}

type durableRedisFaultStore struct {
	iservice.TokenBlockListService
	registerFailure error
	rotateFailure   error
	afterRegister   func(string, string)
	afterRotate     func()
	rotateCalls     int
}

func (s *durableRedisFaultStore) RegisterRefreshSession(id, family string, ttl time.Duration) error {
	if err := s.TokenBlockListService.RegisterRefreshSession(id, family, ttl); err != nil {
		return err
	}
	if s.afterRegister != nil {
		s.afterRegister(id, family)
	}
	return s.registerFailure
}

func (s *durableRedisFaultStore) RotateRefreshToken(parent, family, child string, ttl, grace time.Duration, pair string) (string, bool, error) {
	s.rotateCalls++
	if s.rotateFailure != nil {
		return "", false, s.rotateFailure
	}
	value, created, err := s.TokenBlockListService.RotateRefreshToken(parent, family, child, ttl, grace, pair)
	if s.afterRotate != nil {
		s.afterRotate()
	}
	return value, created, err
}

func TestDurableSessionLoginRegistrationFaultCannotBeRestored(t *testing.T) {
	svc, db, client, user := durableSessionStores(t)
	base := svc.blockListService
	fault := &durableRedisFaultStore{TokenBlockListService: base, registerFailure: errors.New("synthetic lost registration acknowledgment")}
	var id uuid.UUID
	var snapshot durableRedisSnapshot
	var keys []string
	fault.afterRegister = func(refresh, family string) {
		id = uuid.MustParse(family)
		keys = []string{refresh, "refresh-family:" + family, "refresh-family-active:" + family, "refresh-active:" + refresh, "refresh-rotation:" + refresh}
		snapshot = captureDurableRedisSnapshot(t, client, keys)
	}
	svc.blockListService = fault
	pair, err := svc.issueSession(user)
	require.Error(t, err)
	require.Nil(t, pair)
	require.NotEqual(t, uuid.Nil, id)
	family := readDurableFamily(t, db, id)
	require.NotNil(t, family.RevokedAt)
	restoreDurableRedisSnapshot(t, client, snapshot, keys)
	svc.blockListService = base
	// Synthetic bearer reconstruction exercises authority, not real issuance.
	token, err := svc.generateRefreshTokenWithIdentity(user.ID, id.String(), id.String(), family.ExpiresAt.Unix(), user.SessionVersion)
	require.NoError(t, err)
	pair, err = svc.RefreshToken(token)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Nil(t, pair)
}

func TestDurableSessionCommittedRefreshRecoversOnlyCanonicalPair(t *testing.T) {
	svc, db, _, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	base := svc.blockListService
	fault := &durableRedisFaultStore{TokenBlockListService: base, rotateFailure: errors.New("synthetic crash boundary before Redis write")}
	svc.blockListService = fault
	pair, err := svc.RefreshToken(parent.RefreshToken)
	require.Error(t, err)
	require.Nil(t, pair)
	var receipt models.RefreshRotationReceipt
	require.NoError(t, db.First(&receipt, "predecessor_uuid = ?", id.RefreshUUID).Error)
	expected, err := svc.decryptRefreshRotationPayload(receipt.EncryptedPair, id.RefreshUUID.String())
	require.NoError(t, err)
	ok, err := svc.IsUserAuthenticated(parent.AccessToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.False(t, ok)
	svc.blockListService = base
	pair, err = svc.RefreshToken(parent.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, expected.RefreshToken, pair.RefreshToken)
	require.Equal(t, expected.AccessToken, pair.AccessToken)
	require.EqualValues(t, 1, readDurableFamily(t, db, id.FamilyID).CurrentGeneration)
}

func TestDurableSessionPostgresWriteFaultDoesNotConsumeOrCallRedis(t *testing.T) {
	svc, db, _, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	// These objects live only in the fixture's isolated PostgreSQL schema.
	require.NoError(t, db.Exec("CREATE FUNCTION reject_test_rotation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic durable write failure'; END; $$").Error)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_test_rotation BEFORE INSERT ON refresh_rotation_receipts FOR EACH ROW EXECUTE FUNCTION reject_test_rotation()").Error)
	fault := &durableRedisFaultStore{TokenBlockListService: svc.blockListService}
	svc.blockListService = fault
	pair, err := svc.RefreshToken(parent.RefreshToken)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
	require.Nil(t, pair)
	require.Zero(t, fault.rotateCalls)
	family := readDurableFamily(t, db, id.FamilyID)
	require.EqualValues(t, 0, family.CurrentGeneration)
	require.Equal(t, id.RefreshUUID, family.CurrentRefreshUUID)
	var count int64
	require.NoError(t, db.Model(&models.RefreshRotationReceipt{}).Where("family_id = ?", id.FamilyID).Count(&count).Error)
	require.Zero(t, count)
	ok, err := svc.IsUserAuthenticated(parent.AccessToken)
	require.NoError(t, err)
	require.True(t, ok, "rolled-back SQL write does not consume the parent")
}

func TestDurableSessionConcurrentStoresReturnOneCommittedPair(t *testing.T) {
	svc, db, _, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	const callers = 8
	pairs := make([]*dto.TokenDetails, callers)
	failures := make([]error, callers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := range pairs {
		wait.Add(1)
		go func(i int) { defer wait.Done(); <-start; pairs[i], failures[i] = svc.RefreshToken(parent.RefreshToken) }(i)
	}
	close(start)
	wait.Wait()
	for i := range pairs {
		require.NoError(t, failures[i])
		require.NotNil(t, pairs[i])
		require.Equal(t, pairs[0].RefreshToken, pairs[i].RefreshToken)
		require.Equal(t, pairs[0].AccessToken, pairs[i].AccessToken)
	}
	require.EqualValues(t, 1, readDurableFamily(t, db, id.FamilyID).CurrentGeneration)
}

func TestDurableSessionLogoutAndResetAfterRedisRotationDenyResult(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(map[bool]string{false: "logout", true: "reset"}[reset], func(t *testing.T) {
			svc, db, _, user := durableSessionStores(t)
			parent, err := svc.issueSession(user)
			require.NoError(t, err)
			id := durableIdentity(t, svc, parent.AccessToken)
			fault := &durableRedisFaultStore{TokenBlockListService: svc.blockListService}
			fault.afterRotate = func() {
				if reset {
					require.NoError(t, repositories.NewGormUserRepository(db, noopLogger{}).UpdatePassword(user.ID, user.SessionVersion, "synthetic-new-hash", time.Now().UTC()))
				} else {
					require.NoError(t, svc.Logout(parent.AccessToken))
				}
			}
			svc.blockListService = fault
			pair, err := svc.RefreshToken(parent.RefreshToken)
			require.ErrorIs(t, err, ErrRevokedSession)
			require.Nil(t, pair)
			var receipt models.RefreshRotationReceipt
			require.NoError(t, db.First(&receipt, "predecessor_uuid = ?", id.RefreshUUID).Error)
			child, err := svc.decryptRefreshRotationPayload(receipt.EncryptedPair, id.RefreshUUID.String())
			require.NoError(t, err)
			ok, err := svc.IsUserAuthenticated(child.AccessToken)
			require.ErrorIs(t, err, ErrRevokedSession)
			require.False(t, ok)
		})
	}
}

func TestDurableSessionLegacyRedisFamilyCannotBackfillPostgres(t *testing.T) {
	svc, db, _, user := durableSessionStores(t)
	familyID := uuid.NewString()
	expires := time.Now().Add(time.Hour).Unix()
	require.NoError(t, svc.blockListService.RegisterRefreshSession(familyID, familyID, time.Until(time.Unix(expires, 0))))
	refresh, err := svc.generateRefreshTokenWithIdentity(user.ID, familyID, familyID, expires, user.SessionVersion)
	require.NoError(t, err)
	access, _, err := svc.generateAccessTokenForFamilyWithVersion(user.ID, user.Role, familyID, familyID, expires, user.SessionVersion)
	require.NoError(t, err)
	ok, err := svc.IsUserAuthenticated(access)
	require.False(t, ok)
	require.ErrorIs(t, err, ErrRevokedSession)
	pair, err := svc.RefreshToken(refresh)
	require.Nil(t, pair)
	require.ErrorIs(t, err, ErrRevokedSession)
	var count int64
	require.NoError(t, db.Model(&models.SessionFamily{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestDurableSessionMigrationPreservesRevocationsAndReceipts(t *testing.T) {
	svc, db, client, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	child, err := svc.RefreshToken(parent.RefreshToken)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	keys := durableSessionKeys(t, svc, parent, child)
	snapshot := captureDurableRedisSnapshot(t, client, keys)
	require.NoError(t, svc.Logout(child.AccessToken))
	before := readDurableFamily(t, db, id.FamilyID)
	var receipt models.RefreshRotationReceipt
	require.NoError(t, db.First(&receipt, "predecessor_uuid = ?", id.RefreshUUID).Error)
	require.NoError(t, infra.RunMigrations(db))
	require.NoError(t, infra.RunMigrations(db))
	after := readDurableFamily(t, db, id.FamilyID)
	require.Equal(t, before, after)
	var persisted models.RefreshRotationReceipt
	require.NoError(t, db.First(&persisted, "predecessor_uuid = ?", id.RefreshUUID).Error)
	require.Equal(t, receipt, persisted)
	restoreDurableRedisSnapshot(t, client, snapshot, keys)
	assertRedisSessionDenied(t, svc, child)
}

func TestDurableSessionSchemaFaultNeverRelaxesAuthority(t *testing.T) {
	svc, db, _, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	// Deliberately incompatible schema, only inside this isolated fixture.
	require.NoError(t, db.Exec("ALTER TABLE session_families RENAME COLUMN revoked_at TO broken_revoked_at").Error)
	require.Error(t, svc.Readiness(context.Background()))
	ok, err := svc.IsUserAuthenticated(parent.AccessToken)
	require.False(t, ok)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
	pair, err := svc.RefreshToken(parent.RefreshToken)
	require.Nil(t, pair)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
	require.Error(t, infra.RunMigrations(db), "incompatible tables are not silently adopted")
	var generation int64
	require.NoError(t, db.Table("session_families").Select("current_generation").Where("id = ?", id.FamilyID).Scan(&generation).Error)
	require.Zero(t, generation)
}

func TestDurableSessionClosedPostgresPoolDeniesBothTokenTypes(t *testing.T) {
	svc, db, client, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	keys := durableSessionKeys(t, svc, parent)
	snapshot := captureDurableRedisSnapshot(t, client, keys)
	pool, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, pool.Close())
	ok, err := svc.IsUserAuthenticated(parent.AccessToken)
	require.False(t, ok)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
	pair, err := svc.RefreshToken(parent.RefreshToken)
	require.Nil(t, pair)
	require.Error(t, err)
	pair, err = svc.issueSession(user)
	require.Nil(t, pair)
	require.Error(t, err)
	require.ErrorIs(t, svc.Logout(parent.AccessToken), irepository.ErrSessionAuthorityUnavailable, "no successful durable logout acknowledgment when SQL is unavailable")
	restoreDurableRedisSnapshot(t, client, snapshot, keys)
	ok, err = svc.IsUserAuthenticated(parent.AccessToken)
	require.False(t, ok)
	require.ErrorIs(t, err, irepository.ErrSessionAuthorityUnavailable)
}

type durableLockProbeContextKey struct{}

func TestDurableSessionUserLockPrecedesFamilyAndSeesReset(t *testing.T) {
	svc, db, _, user := durableSessionStores(t)
	parent, err := svc.issueSession(user)
	require.NoError(t, err)
	id := durableIdentity(t, svc, parent.AccessToken)
	holding := db.Begin()
	require.NoError(t, holding.Error)
	defer holding.Rollback()
	require.NoError(t, holding.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", user.ID).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, durableLockProbeContextKey{}, true)
	pidReady := make(chan int, 1)
	var once sync.Once
	callback := "durable_user_lock_" + uuid.NewString()
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(durableLockProbeContextKey{}) != true || tx.Statement.Table != "users" {
			return
		}
		once.Do(func() {
			var pid int
			if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				tx.AddError(err)
			}
			pidReady <- pid
		})
	}))
	defer db.Callback().Query().Remove(callback)
	finished := make(chan error, 1)
	go func() {
		_, err := svc.sessionRepository.Rotate(ctx, id, uuid.New(), "synthetic-lock-probe", refreshRotationReplayGrace)
		finished <- err
	}()
	var pid int
	select {
	case pid = <-pidReady:
	case <-ctx.Done():
		t.Fatal("rotation did not reach its user lock")
	}
	require.Positive(t, pid)
	require.Eventually(t, func() bool {
		var blocked bool
		return db.Raw("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock')", pid).Scan(&blocked).Error == nil && blocked
	}, 2*time.Second, 10*time.Millisecond)
	// While rotation demonstrably waits on users, the family is not locked.
	require.NoError(t, db.Exec("SELECT id FROM session_families WHERE id = ? FOR UPDATE NOWAIT", id.FamilyID).Error)
	require.NoError(t, holding.Model(&models.User{}).Where("id = ?", user.ID).Update("session_version", gorm.Expr("session_version + 1")).Error)
	require.NoError(t, holding.Commit().Error)
	select {
	case err := <-finished:
		require.ErrorIs(t, err, irepository.ErrSessionRevoked)
	case <-ctx.Done():
		t.Fatal("rotation did not finish after the reset committed")
	}
	require.EqualValues(t, 0, readDurableFamily(t, db, id.FamilyID).CurrentGeneration)
}
