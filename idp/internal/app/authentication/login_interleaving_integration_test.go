package authentication

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories"
	"automation-hub-idp/internal/app/users"
	"automation-hub-idp/internal/app/utils"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type loginInterleavingHasher struct {
	integrationPasswordHasher
	afterSnapshot func()
}

func (h loginInterleavingHasher) Compare(string, string) error {
	if h.afterSnapshot != nil {
		h.afterSnapshot()
	}
	return nil
}

func atomicLoginIntegrationFixture(t *testing.T) (*service, *repositories.GormUserRepository, *models.User) {
	t.Helper()
	t.Setenv("MIN_TIME_BETWEEN_ATTEMPTS_IN_SECONDS", "0")
	setupAuthConfig(t)
	db := openPasswordResetTestDB(t)
	repo := repositories.NewGormUserRepository(db, noopLogger{}).(*repositories.GormUserRepository)
	user := &models.User{
		ID: uuid.New(), Email: "synthetic-" + uuid.NewString() + "@example.test",
		Password: "synthetic-verified-hash", Role: "owner", IsActive: true, SessionVersion: 4,
		FailedAttempts: 2, LastAttempt: timePointer(time.Now().UTC().Add(-time.Hour)),
		ResetPasswordToken: utils.PasswordResetTokenDigest("synthetic-pending-reset"),
		ResetTokenExpires:  timePointer(time.Now().UTC().Add(time.Hour)),
	}
	require.NoError(t, db.Create(user).Error)
	hasher := loginInterleavingHasher{}
	svc := &service{
		sessionRepository: repositories.NewGormSessionRepository(db),
		userService:       users.NewUserService(repo, noopLogger{}, hasher), loginFailureRecorder: repo,
		hasher: hasher, blockListService: &recoveryLoginSessionStore{}, logger: noopLogger{}, jwtSecret: testSigningSecret,
	}
	return svc, repo, user
}

// The hook commits the competing write after the real DB lookup and before
// successful-login reset. No scheduler timing or sleep determines the order.
func TestSuccessfulLoginPreservesLockoutCommittedDuringPasswordComparison(t *testing.T) {
	for _, kind := range []string{"indefinite", "threshold active"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, user := atomicLoginIntegrationFixture(t)
			var locked models.User
			svc.hasher = loginInterleavingHasher{afterSnapshot: func() {
				now := time.Now().UTC()
				if kind == "indefinite" {
					require.NoError(t, repo.DB.Model(&models.User{}).Where("id = ?", user.ID).
						Updates(map[string]interface{}{"is_blocked": true, "blocked_until": nil, "failed_attempts": 11, "last_attempt": now}).Error)
				} else {
					_, blocked, err := repo.RecordLoginFailure(user.ID, now, 3, time.Hour)
					require.NoError(t, err)
					require.True(t, blocked)
				}
				require.NoError(t, repo.DB.First(&locked, "id = ?", user.ID).Error)
			}}

			pair, err := svc.Login(user.Email, "synthetic-valid-passphrase")

			require.EqualError(t, err, "account changed during login; please try again")
			require.Nil(t, pair)
			require.Zero(t, svc.blockListService.(*recoveryLoginSessionStore).registrations)
			var stored models.User
			require.NoError(t, repo.DB.First(&stored, "id = ?", user.ID).Error)
			require.True(t, stored.IsBlocked)
			require.Equal(t, locked.FailedAttempts, stored.FailedAttempts)
			require.Equal(t, locked.LastAttempt, stored.LastAttempt)
			require.Equal(t, locked.BlockedUntil, stored.BlockedUntil)
			require.Equal(t, locked.UpdatedAt, stored.UpdatedAt, "denied success must not update the locked account")
			require.Equal(t, user.Password, stored.Password)
			require.Equal(t, user.Role, stored.Role)
			require.Equal(t, user.SessionVersion, stored.SessionVersion)
			require.Equal(t, user.ResetPasswordToken, stored.ResetPasswordToken)
		})
	}
}

func TestSuccessfulLoginBeforeThresholdFailureDoesNotClearLaterLockout(t *testing.T) {
	svc, repo, user := atomicLoginIntegrationFixture(t)
	pair, err := svc.Login(user.Email, "synthetic-valid-passphrase")
	require.NoError(t, err)
	var reset models.User
	require.NoError(t, repo.DB.First(&reset, "id = ?", user.ID).Error)
	require.Zero(t, reset.FailedAttempts)
	require.Nil(t, reset.LastAttempt)

	_, blocked, err := repo.RecordLoginFailure(user.ID, time.Now().UTC(), 1, time.Hour)
	require.NoError(t, err)
	require.True(t, blocked)
	var stored models.User
	require.NoError(t, repo.DB.First(&stored, "id = ?", user.ID).Error)
	require.True(t, stored.IsBlocked)
	require.Equal(t, 1, stored.FailedAttempts)
	active, err := svc.IsUserAuthenticated(pair.AccessToken)
	require.Error(t, err)
	require.False(t, active, "a session issued before lockout must honor the current block")
}

func TestSuccessfulLoginPreservesConcurrentAccountAndResetChanges(t *testing.T) {
	for _, kind := range []string{"role demotion", "password", "session version", "reset digest", "reset expiry", "reset expiry cleared", "email", "deactivation"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, user := atomicLoginIntegrationFixture(t)
			var committed models.User
			svc.hasher = loginInterleavingHasher{afterSnapshot: func() {
				changes := map[string]interface{}{}
				switch kind {
				case "role demotion":
					changes["role"] = "viewer"
				case "password":
					changes["password"] = "synthetic-replacement-hash"
				case "session version":
					changes["session_version"] = user.SessionVersion + 1
				case "reset digest":
					changes["reset_password_token"] = utils.PasswordResetTokenDigest("synthetic-new-reset")
				case "reset expiry":
					changes["reset_token_expires"] = user.ResetTokenExpires.Add(time.Hour)
				case "reset expiry cleared":
					changes["reset_token_expires"] = nil
				case "email":
					changes["email"] = "synthetic-new-email@example.test"
				case "deactivation":
					changes["is_active"] = false
				}
				require.NoError(t, repo.DB.Model(&models.User{}).Where("id = ?", user.ID).Updates(changes).Error)
				require.NoError(t, repo.DB.First(&committed, "id = ?", user.ID).Error)
			}}

			pair, err := svc.Login(user.Email, "synthetic-valid-passphrase")
			var stored models.User
			require.NoError(t, repo.DB.First(&stored, "id = ?", user.ID).Error)
			if kind == "role demotion" {
				require.NoError(t, err)
				require.Equal(t, "viewer", stored.Role)
				_, claims, err := svc.parseAndValidateToken(pair.AccessToken)
				require.NoError(t, err)
				require.Equal(t, "viewer", claims["role"], "issue current role, never stale owner authority")
				require.Equal(t, user.Password, stored.Password)
				require.Equal(t, user.ResetPasswordToken, stored.ResetPasswordToken)
				require.WithinDuration(t, *user.ResetTokenExpires, *stored.ResetTokenExpires, time.Microsecond)
			} else {
				require.EqualError(t, err, "account changed during login; please try again")
				require.Nil(t, pair)
				require.Zero(t, svc.blockListService.(*recoveryLoginSessionStore).registrations)
				require.Equal(t, user.FailedAttempts, stored.FailedAttempts, "guard failure must leave attempt state unchanged")
				require.Equal(t, committed, stored, "a denied login must not overwrite any competing account change")
			}
		})
	}
}

type loginOrderContextKey struct{}

func waitLoginOrderSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("login-order barrier timed out")
	}
}

func TestAtomicSuccessfulLoginAndFailureSerializeOnSameRow(t *testing.T) {
	for _, order := range []string{"lockout first", "success first"} {
		t.Run(order, func(t *testing.T) {
			_, repo, user := atomicLoginIntegrationFixture(t)
			expected, err := repo.FindByID(user.ID)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			leader := &repositories.GormUserRepository{DB: repo.DB.WithContext(context.WithValue(ctx, loginOrderContextKey{}, "leader"))}
			follower := &repositories.GormUserRepository{DB: repo.DB.WithContext(context.WithValue(ctx, loginOrderContextKey{}, "follower"))}
			leaderLocked, followerEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce, leaderOnce, followerOnce sync.Once
			var workers sync.WaitGroup
			unlock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(func() {
				unlock()
				cancel()
				workers.Wait()
			})
			// The leader pauses only after SELECT FOR UPDATE and before UPDATE,
			// while the follower enters its real query on another connection.
			require.NoError(t, repo.DB.Callback().Update().Before("gorm:update").Register("test:login-leader-lock", func(db *gorm.DB) {
				if db.Statement.Context.Value(loginOrderContextKey{}) == "leader" {
					leaderOnce.Do(func() { close(leaderLocked) })
					select {
					case <-release:
					case <-ctx.Done():
						db.AddError(ctx.Err())
					}
				}
			}))
			require.NoError(t, repo.DB.Callback().Query().Before("gorm:query").Register("test:login-follower-query", func(db *gorm.DB) {
				if db.Statement.Context.Value(loginOrderContextKey{}) == "follower" {
					followerOnce.Do(func() { close(followerEntered) })
				}
			}))
			leaderResult, followerResult := make(chan error, 1), make(chan error, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				if order == "lockout first" {
					_, _, err := leader.RecordLoginFailure(user.ID, time.Now().UTC(), 3, time.Hour)
					leaderResult <- err
				} else {
					_, err := leader.CompleteSuccessfulLogin(*expected, time.Now().UTC())
					leaderResult <- err
				}
			}()
			waitLoginOrderSignal(t, leaderLocked)
			workers.Add(1)
			go func() {
				defer workers.Done()
				if order == "lockout first" {
					_, err := follower.CompleteSuccessfulLogin(*expected, time.Now().UTC())
					followerResult <- err
				} else {
					_, _, err := follower.RecordLoginFailure(user.ID, time.Now().UTC(), 1, time.Hour)
					followerResult <- err
				}
			}()
			waitLoginOrderSignal(t, followerEntered)
			unlock()
			require.NoError(t, <-leaderResult)
			if order == "lockout first" {
				require.ErrorIs(t, <-followerResult, users.ErrConcurrentUserUpdate)
			} else {
				require.NoError(t, <-followerResult)
			}
			var stored models.User
			require.NoError(t, repo.DB.First(&stored, "id = ?", user.ID).Error)
			require.True(t, stored.IsBlocked)
			require.NotNil(t, stored.BlockedUntil)
			require.True(t, stored.BlockedUntil.After(time.Now()))
			if order == "lockout first" {
				require.Equal(t, 3, stored.FailedAttempts)
			} else {
				require.Equal(t, 1, stored.FailedAttempts)
			}
			require.Equal(t, expected.Password, stored.Password)
			require.Equal(t, expected.Role, stored.Role)
			require.Equal(t, expected.SessionVersion, stored.SessionVersion)
			require.Equal(t, expected.ResetPasswordToken, stored.ResetPasswordToken)
		})
	}
}

func TestAtomicSuccessfulLoginClearsExpiredBlockOnly(t *testing.T) {
	svc, repo, user := atomicLoginIntegrationFixture(t)
	require.NoError(t, repo.DB.Model(&models.User{}).Where("id = ?", user.ID).
		Updates(map[string]interface{}{"is_blocked": true, "blocked_until": time.Now().UTC().Add(-time.Hour)}).Error)
	pair, err := svc.Login(user.Email, "synthetic-valid-passphrase")
	require.NoError(t, err)
	require.NotNil(t, pair)
	var stored models.User
	require.NoError(t, repo.DB.First(&stored, "id = ?", user.ID).Error)
	require.False(t, stored.IsBlocked)
	require.Nil(t, stored.BlockedUntil)
	require.Zero(t, stored.FailedAttempts)
	require.Nil(t, stored.LastAttempt)
	require.Equal(t, user.Password, stored.Password)
	require.Equal(t, user.Role, stored.Role)
	require.Equal(t, user.SessionVersion, stored.SessionVersion)
	require.Equal(t, user.ResetPasswordToken, stored.ResetPasswordToken)
}
