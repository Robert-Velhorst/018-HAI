package authentication

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/services/iservice"
	"automation-hub-idp/internal/app/users"
	"automation-hub-idp/internal/app/utils"
	"automation-hub-idp/internal/infra"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestPasswordResetDigestAtRestAndLegacyTokenMigration(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	legacyToken := "legacy-uuid-reset-token"
	user := models.User{
		ID:                 uuid.New(),
		Email:              "reset-migration@example.test",
		Password:           "existing-password-hash",
		ResetPasswordToken: legacyToken,
		ResetTokenExpires:  timePointer(time.Now().Add(time.Hour)),
		IsActive:           true,
	}
	require.NoError(t, db.Create(&user).Error)

	require.NoError(t, infra.RunMigrations(db))
	stored := readStoredResetToken(t, db, user.ID)
	require.True(t, stored == utils.PasswordResetTokenDigest(legacyToken), "legacy token should be upgraded in-place to its digest")
	require.True(t, stored != legacyToken, "legacy bearer token must not remain stored")
	require.NoError(t, infra.RunMigrations(db))
	require.True(t, readStoredResetToken(t, db, user.ID) == stored, "re-running migrations must preserve current digests")

	setupAuthConfig(t)
	sender := &recordingResetSender{}
	userService := users.NewUserService(repo, noopLogger{}, integrationPasswordHasher{})
	auth := &service{userService: userService, passwordResetter: sender, logger: noopLogger{}}
	issuedToken, _, err := auth.RequestPasswordReset(user.Email)
	require.NoError(t, err)
	require.True(t, sender.token == issuedToken, "email sender must receive the bearer token")
	resolved, err := userService.GetUserByResetToken(issuedToken)
	require.NoError(t, err)
	require.Equal(t, user.ID, resolved.ID)
	stored = readStoredResetToken(t, db, user.ID)
	require.True(t, stored == utils.PasswordResetTokenDigest(issuedToken), "database must store the bearer token digest")
	require.True(t, stored != issuedToken, "database must not store the bearer token")
}

func TestPasswordResetTokenCanBeConsumedOnlyOnceUnderConcurrency(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	issuedToken := "concurrent-reset-token-for-integration-test"
	user := models.User{
		ID:                 uuid.New(),
		Email:              "reset-concurrency@example.test",
		Password:           "old-password-hash",
		ResetPasswordToken: utils.PasswordResetTokenDigest(issuedToken),
		ResetTokenExpires:  timePointer(time.Now().Add(time.Hour)),
		IsActive:           true,
	}
	require.NoError(t, db.Create(&user).Error)

	hasher := newPasswordHashBarrier(2)
	userService := users.NewUserService(repo, noopLogger{}, hasher)
	auth := &service{userService: userService, logger: noopLogger{}}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- auth.ConfirmPasswordReset(issuedToken, "new-password-for-reset")
		}()
	}

	first, second := <-results, <-results
	successes := 0
	invalid := 0
	for _, err := range []error{first, second} {
		if err == nil {
			successes++
		} else if err.Error() == "invalid token" {
			invalid++
		}
	}
	require.Equal(t, 1, successes, "exactly one concurrent reset must succeed")
	require.Equal(t, 1, invalid, "the losing reset must be rejected as already consumed")

	var persisted models.User
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Equal(t, "integration-password-hash", persisted.Password)
	require.Empty(t, persisted.ResetPasswordToken)
	require.Nil(t, persisted.ResetTokenExpires)
	require.False(t, persisted.FirstAccess)
	require.Equal(t, int64(1), persisted.SessionVersion, "successful reset must invalidate all sessions issued at the prior version")
}

func TestConcurrentFailedLoginsAtomicallyIncrementAndLockAccount(t *testing.T) {
	t.Setenv("MIN_TIME_BETWEEN_ATTEMPTS_IN_SECONDS", "0")
	setupAuthConfig(t)
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	const attemptCount = 16
	user := models.User{
		ID: uuid.New(), Email: "login-concurrency@example.test", Password: "integration-password-hash",
		Role: "operator", IsActive: true,
	}
	require.NoError(t, db.Create(&user).Error)

	hasher := newFailedLoginCompareBarrier(attemptCount)
	sender := &integrationAccountBlockedSender{}
	userService := users.NewUserService(repo, noopLogger{}, hasher)
	recorder, ok := repo.(LoginFailureRecorder)
	require.True(t, ok, "GORM repository must provide atomic login-failure recording")
	auth := &service{
		userService:          userService,
		loginFailureRecorder: recorder,
		hasher:               hasher,
		sender:               sender,
		logger:               noopLogger{},
	}

	results := make(chan error, attemptCount)
	var wait sync.WaitGroup
	for range attemptCount {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := auth.Login(user.Email, "incorrect-password")
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	for err := range results {
		require.EqualError(t, err, "invalid credentials")
	}

	var stored models.User
	require.NoError(t, db.First(&stored, "id = ?", user.ID).Error)
	require.Equal(t, config.AuthenticationConfig.MaxLoginAttemptsBeforeBlock, stored.FailedAttempts, "concurrent requests after the threshold lock must not extend the failed-attempt counter")
	require.True(t, stored.IsBlocked, "the account must be locked when the atomic counter reaches the threshold")
	require.NotNil(t, stored.BlockedUntil)
	require.True(t, stored.BlockedUntil.After(time.Now()), "the lockout must remain active")
	require.NotNil(t, stored.LastAttempt)
	require.Equal(t, 1, sender.Count(), "concurrent requests should emit one threshold-crossing notification")
}

func TestFailedLoginAfterExpiredLockoutStartsNewCounterWindow(t *testing.T) {
	t.Setenv("MIN_TIME_BETWEEN_ATTEMPTS_IN_SECONDS", "0")
	setupAuthConfig(t)
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	oldAttempt := time.Now().UTC().Add(-2 * time.Hour)
	expiredBlock := oldAttempt.Add(-time.Minute)
	user := models.User{
		ID: uuid.New(), Email: "expired-login-lock@example.test", Password: "integration-password-hash",
		Role: "operator", IsActive: true, FailedAttempts: config.AuthenticationConfig.MaxLoginAttemptsBeforeBlock,
		LastAttempt: &oldAttempt, IsBlocked: true, BlockedUntil: &expiredBlock,
	}
	require.NoError(t, db.Create(&user).Error)

	hasher := newFailedLoginCompareBarrier(1)
	userService := users.NewUserService(repo, noopLogger{}, hasher)
	recorder, ok := repo.(LoginFailureRecorder)
	require.True(t, ok, "GORM repository must provide atomic login-failure recording")
	auth := &service{userService: userService, loginFailureRecorder: recorder, hasher: hasher, logger: noopLogger{}}
	_, err := auth.Login(user.Email, "incorrect-password")
	require.EqualError(t, err, "invalid credentials")

	var stored models.User
	require.NoError(t, db.First(&stored, "id = ?", user.ID).Error)
	require.Equal(t, 1, stored.FailedAttempts, "the first failure after an expired lockout starts a fresh counter window")
	require.False(t, stored.IsBlocked)
	require.Nil(t, stored.BlockedUntil)
	require.NotNil(t, stored.LastAttempt)
}

func TestStaleUserUpdateCannotRestoreConsumedResetTokenOrSessionVersion(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	issuedToken := "stale-update-reset-token"
	now := time.Now().UTC()
	user := models.User{
		ID: uuid.New(), Email: "stale-reset@example.test", Password: "old-password-hash", IsActive: true,
		ResetPasswordToken: utils.PasswordResetTokenDigest(issuedToken), ResetTokenExpires: timePointer(now.Add(time.Hour)),
	}
	require.NoError(t, db.Create(&user).Error)

	stale, err := repo.FindByID(user.ID)
	require.NoError(t, err)
	userService := users.NewUserService(repo, noopLogger{}, integrationPasswordHasher{})
	auth := &service{userService: userService, logger: noopLogger{}}
	require.NoError(t, auth.ConfirmPasswordReset(issuedToken, "new-password-for-reset"))

	stale.FailedAttempts++
	_, err = repo.Update(stale)
	require.ErrorIs(t, err, irepository.ErrConcurrentUserUpdate, "a stale full-user update must fail after reset changes the concurrency guard")

	_, err = userService.UpdateUser(*stale)
	require.ErrorIs(t, err, users.ErrConcurrentUserUpdate, "a stale service update must not refresh its concurrency token and overwrite newer account state")

	var persisted models.User
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Equal(t, int64(1), persisted.SessionVersion)
	require.Empty(t, persisted.ResetPasswordToken)
	require.Nil(t, persisted.ResetTokenExpires)
	require.Equal(t, "integration-password-hash", persisted.Password)
}

func TestAuthenticatedPasswordChangeRevokesSessionsAndPendingResetTokens(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	user := models.User{
		ID: uuid.New(), Email: "password-change@example.test", Password: "old-password-hash", IsActive: true,
		SessionVersion: 3, ResetPasswordToken: utils.PasswordResetTokenDigest("pending-reset-token"),
		ResetTokenExpires: timePointer(time.Now().Add(time.Hour)),
	}
	require.NoError(t, db.Create(&user).Error)
	setupAuthConfig(t)
	userService := users.NewUserService(repo, noopLogger{}, integrationPasswordHasher{})
	auth := &service{userService: userService, sessionRepository: repositories.NewGormSessionRepository(db), blockListService: fakeBlockListService{}, logger: noopLogger{}, jwtSecret: testSigningSecret}
	accessToken, _, err := auth.generateAccessTokenWithVersion(user.ID, "operator", uuid.NewString(), time.Now().Add(time.Hour).Unix(), user.SessionVersion)
	require.NoError(t, err)

	seedTestAccessAuthority(t, auth, accessToken)
	require.NoError(t, auth.ChangePassword(accessToken, "old-password", "new-password-for-account"))
	active, err := auth.IsUserAuthenticated(accessToken)
	require.False(t, active)
	require.ErrorIs(t, err, ErrRevokedSession)

	var persisted models.User
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Equal(t, int64(4), persisted.SessionVersion)
	require.Empty(t, persisted.ResetPasswordToken)
	require.Nil(t, persisted.ResetTokenExpires)
	require.Equal(t, "integration-password-hash", persisted.Password)
}

func TestDeletingAndReactivatingUserDoesNotRestoreOldSessionOrResetToken(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	resetToken := "reset-token-before-account-deletion"
	user := models.User{
		ID: uuid.New(), Email: "delete-reactivate@example.test", Password: "old-password-hash", Role: "operator", IsActive: true,
		SessionVersion: 4, ResetPasswordToken: utils.PasswordResetTokenDigest(resetToken),
		ResetTokenExpires: timePointer(time.Now().Add(time.Hour)),
	}
	require.NoError(t, db.Create(&user).Error)
	setupAuthConfig(t)
	userService := users.NewUserService(repo, noopLogger{}, integrationPasswordHasher{})
	blocklist := &inMemoryBlockListService{}
	auth := &service{
		userService: userService, sessionRepository: repositories.NewGormSessionRepository(db),
		blockListService: blocklist, logger: noopLogger{}, jwtSecret: testSigningSecret,
	}
	pair, err := auth.issueSession(&user)
	require.NoError(t, err)
	active, err := auth.IsUserAuthenticated(pair.AccessToken)
	require.NoError(t, err)
	require.True(t, active)

	require.NoError(t, userService.DeleteUser(user.ID))
	// Simulate an administrator restoring account access. The delete operation
	// must already have invalidated the old session and reset credential.
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", user.ID).Update("is_active", true).Error)

	active, err = auth.IsUserAuthenticated(pair.AccessToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.False(t, active)
	require.ErrorIs(t, auth.ConfirmPasswordReset(resetToken, "replacement-password"), ErrInvalidPasswordReset)

	var persisted models.User
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Equal(t, int64(5), persisted.SessionVersion)
	require.Empty(t, persisted.ResetPasswordToken)
	require.Nil(t, persisted.ResetTokenExpires)
}

func TestPasswordChangeWithStaleValidatedSessionCannotOverwriteReset(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	resetToken := "completed-reset-before-password-change"
	user := models.User{
		ID: uuid.New(), Email: "stale-session-change@example.test", Password: "old-password-hash", IsActive: true,
		SessionVersion: 5, ResetPasswordToken: utils.PasswordResetTokenDigest(resetToken),
		ResetTokenExpires: timePointer(time.Now().Add(time.Hour)),
	}
	require.NoError(t, db.Create(&user).Error)

	userService := users.NewUserService(repo, noopLogger{}, integrationPasswordHasher{})
	auth := &service{userService: userService, blockListService: fakeBlockListService{}, logger: noopLogger{}}
	require.NoError(t, auth.ConfirmPasswordReset(resetToken, "new-password-from-reset"))

	err := userService.UpdatePasswordWithCurrentPassword(user.ID, 5, "old-password", "late-password-change")
	require.ErrorIs(t, err, users.ErrConcurrentUserUpdate, "the DB write must compare against the version validated by middleware")

	var persisted models.User
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Equal(t, int64(6), persisted.SessionVersion)
	require.Empty(t, persisted.ResetPasswordToken)
	require.Nil(t, persisted.ResetTokenExpires)
	require.Equal(t, "integration-password-hash", persisted.Password)
}

func TestAtomicAccountUpdateChangesEmailAndPasswordWithSessionGuard(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	user := models.User{
		ID: uuid.New(), Email: "account-update@example.test", Password: "old-password-hash", IsActive: true,
		SessionVersion: 8, ResetPasswordToken: utils.PasswordResetTokenDigest("pending-account-reset"),
		ResetTokenExpires: timePointer(time.Now().Add(time.Hour)),
	}
	require.NoError(t, db.Create(&user).Error)
	newEmail := "account-updated@example.test"
	now := time.Now().UTC()

	require.NoError(t, repo.UpdateAccount(user.ID, 8, &newEmail, "new-password-hash", now))

	var persisted models.User
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Equal(t, newEmail, persisted.Email)
	require.Equal(t, "new-password-hash", persisted.Password)
	require.Equal(t, int64(9), persisted.SessionVersion)
	require.Empty(t, persisted.ResetPasswordToken)
	require.Nil(t, persisted.ResetTokenExpires)

	staleEmail := "stale@example.test"
	err := repo.UpdateAccount(user.ID, 8, &staleEmail, "stale-password-hash", now.Add(time.Minute))
	require.ErrorIs(t, err, irepository.ErrConcurrentUserUpdate)
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Equal(t, newEmail, persisted.Email)
	require.Equal(t, "new-password-hash", persisted.Password)
	require.Equal(t, int64(9), persisted.SessionVersion)

	duplicate := models.User{
		ID: uuid.New(), Email: "duplicate-account@example.test", Password: "unchanged-password-hash", IsActive: true,
		SessionVersion: 3,
	}
	require.NoError(t, db.Create(&duplicate).Error)
	err = repo.UpdateAccount(duplicate.ID, 3, &newEmail, "must-not-commit-hash", now)
	require.ErrorIs(t, err, irepository.ErrDuplicateUser)
	require.NoError(t, db.First(&persisted, "id = ?", duplicate.ID).Error)
	require.Equal(t, duplicate.Email, persisted.Email)
	require.Equal(t, duplicate.Password, persisted.Password)
	require.Equal(t, int64(3), persisted.SessionVersion)
}

func TestEmailOnlyAccountUpdateInvalidatesPendingPasswordReset(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	resetToken := "pending-email-change-reset"
	user := models.User{
		ID: uuid.New(), Email: "email-change@example.test", Password: "old-password-hash", IsActive: true,
		SessionVersion: 4, ResetPasswordToken: utils.PasswordResetTokenDigest(resetToken),
		ResetTokenExpires: timePointer(time.Now().Add(time.Hour)),
	}
	require.NoError(t, db.Create(&user).Error)
	newEmail := "email-changed@example.test"
	now := time.Now().UTC()

	require.NoError(t, repo.UpdateAccount(user.ID, user.SessionVersion, &newEmail, "", now))
	var persisted models.User
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Equal(t, newEmail, persisted.Email)
	require.Empty(t, persisted.ResetPasswordToken)
	require.Nil(t, persisted.ResetTokenExpires)
	require.Equal(t, int64(5), persisted.SessionVersion)
	require.ErrorIs(t, repo.ConsumePasswordResetToken(user.ID, resetToken, "replacement-hash", now), irepository.ErrResetTokenNotConsumable)
}

func TestSetPasswordResetTokenRejectsStaleAccountVersion(t *testing.T) {
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{})
	user := models.User{
		ID: uuid.New(), Email: "stale-reset@example.test", Password: "old-password-hash", IsActive: true,
		SessionVersion: 9,
	}
	require.NoError(t, db.Create(&user).Error)
	now := time.Now().UTC()
	digest := utils.PasswordResetTokenDigest("stale-session-reset-token")

	err := repo.SetPasswordResetToken(user.ID, 8, digest, now.Add(time.Hour), now)

	require.ErrorIs(t, err, irepository.ErrConcurrentUserUpdate)
	var persisted models.User
	require.NoError(t, db.First(&persisted, "id = ?", user.ID).Error)
	require.Empty(t, persisted.ResetPasswordToken)
	require.Nil(t, persisted.ResetTokenExpires)
}

func openPasswordResetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_IDP_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set HAI_IDP_TEST_DATABASE_URL to run PostgreSQL password-reset integration tests")
	}
	if os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS") != "true" {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required for disposable IDP database tests")
	}
	if err := validateIDPAuthTestDatabaseURL(dsn, os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("HAI_IDP_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal("failed to connect to password-reset integration database")
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal("failed to initialize password-reset integration database")
	}
	adminSQL.SetMaxOpenConns(1)
	if err := admin.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		_ = adminSQL.Close()
		t.Fatal("failed to prepare PostgreSQL UUID support")
	}
	schema := "hai_password_reset_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		_ = adminSQL.Close()
		t.Fatal("failed to create isolated integration schema")
	}

	query := parsed.Query()
	options := strings.TrimSpace(query.Get("options") + " -c search_path=" + schema + ",public")
	query.Set("options", options)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		_ = adminSQL.Close()
		t.Fatal("failed to connect to isolated integration schema")
	}
	testSQL, err := db.DB()
	if err != nil {
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		_ = adminSQL.Close()
		t.Fatal("failed to initialize isolated integration schema")
	}
	testSQL.SetMaxOpenConns(8)
	if err := infra.RunMigrations(db); err != nil {
		_ = testSQL.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		_ = adminSQL.Close()
		t.Fatal("failed to prepare isolated integration schema")
	}
	t.Cleanup(func() {
		_ = testSQL.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
		_ = adminSQL.Close()
	})
	return db
}

func readStoredResetToken(t *testing.T, db *gorm.DB, userID uuid.UUID) string {
	t.Helper()
	var stored string
	require.NoError(t, db.Model(&models.User{}).
		Select("reset_password_token").
		Where("id = ?", userID).
		Scan(&stored).Error)
	return stored
}

func timePointer(value time.Time) *time.Time { return &value }

type recordingResetSender struct{ token string }

func (*recordingResetSender) Configured() bool { return true }
func (s *recordingResetSender) SendPasswordReset(_ string, token string, _ time.Time) error {
	s.token = token
	return nil
}

var _ iservice.PasswordResetSender = (*recordingResetSender)(nil)

type integrationPasswordHasher struct{}

func (integrationPasswordHasher) Hash(string) (string, error) {
	return "integration-password-hash", nil
}
func (integrationPasswordHasher) Compare(string, string) error { return nil }

type failedLoginCompareBarrier struct {
	mu      sync.Mutex
	arrived int
	count   int
	ready   chan struct{}
}

func newFailedLoginCompareBarrier(count int) *failedLoginCompareBarrier {
	return &failedLoginCompareBarrier{count: count, ready: make(chan struct{})}
}

func (h *failedLoginCompareBarrier) Hash(string) (string, error) { return "unused", nil }

func (h *failedLoginCompareBarrier) Compare(string, string) error {
	h.mu.Lock()
	h.arrived++
	if h.arrived == h.count {
		close(h.ready)
	}
	ready := h.ready
	h.mu.Unlock()

	select {
	case <-ready:
		return errors.New("password mismatch")
	case <-time.After(10 * time.Second):
		return errors.New("concurrent login test barrier timed out")
	}
}

type integrationAccountBlockedSender struct {
	mu    sync.Mutex
	count int
}

func (s *integrationAccountBlockedSender) Send(string, interface{}) error {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
	return nil
}

func (s *integrationAccountBlockedSender) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

type passwordHashBarrier struct {
	mu      sync.Mutex
	arrived int
	ready   chan struct{}
	count   int
}

func newPasswordHashBarrier(count int) *passwordHashBarrier {
	return &passwordHashBarrier{ready: make(chan struct{}), count: count}
}

func (h *passwordHashBarrier) Hash(string) (string, error) {
	h.mu.Lock()
	h.arrived++
	if h.arrived == h.count {
		close(h.ready)
	}
	ready := h.ready
	h.mu.Unlock()
	<-ready
	return "integration-password-hash", nil
}

func (h *passwordHashBarrier) Compare(string, string) error { return nil }

var _ utils.PasswordHasher = integrationPasswordHasher{}
var _ utils.PasswordHasher = (*passwordHashBarrier)(nil)
