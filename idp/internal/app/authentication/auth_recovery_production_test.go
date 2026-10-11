package authentication

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/users"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type recoveryLoginUserService struct {
	fakeUserService
	updateErr      error
	updateCalls    int
	missingAccount bool
}

func (s *recoveryLoginUserService) CompleteSuccessfulLogin(user models.User) (*models.User, error) {
	s.updateCalls++
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	if s.missingAccount {
		return nil, nil
	}
	return s.fakeUserService.CompleteSuccessfulLogin(user)
}

type recoveryLoginSessionStore struct {
	fakeBlockListService
	registrations int
}

func (s *recoveryLoginSessionStore) RegisterRefreshSession(string, string, time.Duration) error {
	s.registrations++
	return nil
}

func TestPasswordLoginNeverIssuesSessionAfterAccountUpdateFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		message string
	}{
		{"database write failure", errors.New("synthetic write unavailable"), "failed to update login state"},
		{"account reread failure", errors.New("error fetching user by ID"), "failed to update login state"},
		{"concurrent account change", users.ErrConcurrentUserUpdate, "account changed during login; please try again"},
		{"wrapped concurrent account change", fmt.Errorf("synthetic conflict: %w", users.ErrConcurrentUserUpdate), "account changed during login; please try again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupAuthConfig(t)
			userService := &recoveryLoginUserService{
				fakeUserService: fakeUserService{userByEmail: &models.User{
					ID: uuid.New(), Email: "synthetic-owner@example.test", Role: "owner", IsActive: true,
				}},
				updateErr: tc.failure,
			}
			store := &recoveryLoginSessionStore{}
			svc := &service{
				userService:      userService,
				hasher:           integrationPasswordHasher{},
				blockListService: store,
				logger:           noopLogger{},
				jwtSecret:        testSigningSecret,
			}

			pair, err := svc.Login("synthetic-owner@example.test", "synthetic-valid-passphrase")

			require.EqualError(t, err, tc.message)
			require.Nil(t, pair)
			require.Equal(t, 1, userService.updateCalls)
			require.Zero(t, store.registrations, "failed account persistence must not create refresh authority")
		})
	}
}

func TestPasswordLoginIssuesSessionAfterAccountUpdateSucceeds(t *testing.T) {
	setupAuthConfig(t)
	userService := &recoveryLoginUserService{fakeUserService: fakeUserService{userByEmail: &models.User{
		ID: uuid.New(), Email: "synthetic-operator@example.test", Role: "operator", IsActive: true,
	}}}
	store := &recoveryLoginSessionStore{}
	svc := &service{
		userService:      userService,
		hasher:           integrationPasswordHasher{},
		blockListService: store,
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}

	enableTestSessionAuthority(t, svc)
	pair, err := svc.Login("synthetic-operator@example.test", "synthetic-valid-passphrase")

	require.NoError(t, err)
	require.NotNil(t, pair)
	require.NotEmpty(t, pair.AccessToken)
	require.NotEmpty(t, pair.RefreshToken)
	require.Equal(t, 1, userService.updateCalls)
	require.Equal(t, 1, store.registrations)
}

func TestPasswordLoginRejectsMissingCompletedAccount(t *testing.T) {
	setupAuthConfig(t)
	userService := &recoveryLoginUserService{
		fakeUserService: fakeUserService{userByEmail: &models.User{
			ID: uuid.New(), Email: "synthetic-missing@example.test", Role: "owner", IsActive: true,
		}},
		missingAccount: true,
	}
	store := &recoveryLoginSessionStore{}
	svc := &service{
		userService: userService, hasher: integrationPasswordHasher{},
		blockListService: store, logger: noopLogger{}, jwtSecret: testSigningSecret,
	}
	pair, err := svc.Login("synthetic-missing@example.test", "synthetic-valid-passphrase")
	require.EqualError(t, err, "failed to update login state")
	require.Nil(t, pair)
	require.Zero(t, store.registrations)
}
