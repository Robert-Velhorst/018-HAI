package users

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCompleteSuccessfulLoginDelegatesSnapshotAndReturnsCurrentAccount(t *testing.T) {
	repo := new(MockUserRepository)
	expected := models.User{ID: uuid.New(), Role: "owner", Password: "synthetic-verified-hash", SessionVersion: 5}
	current := expected
	current.Role = "viewer"
	repo.On("CompleteSuccessfulLogin", expected, mock.MatchedBy(func(now time.Time) bool {
		return !now.IsZero() && now.Location() == time.UTC
	})).Return(&current, nil).Once()
	svc := NewUserService(repo, nil, nil)
	result, err := svc.CompleteSuccessfulLogin(expected)
	require.NoError(t, err)
	require.Equal(t, &current, result)
	repo.AssertExpectations(t)
	repo.AssertNotCalled(t, "Update", mock.Anything)
	repo.AssertNotCalled(t, "FindByID", mock.Anything)
}

func TestCompleteSuccessfulLoginFailsClosedOnRepositoryErrorsOrMissingAccount(t *testing.T) {
	for _, failure := range []error{irepository.ErrConcurrentUserUpdate, errors.New("synthetic write failure"), nil} {
		repo := new(MockUserRepository)
		expected := models.User{ID: uuid.New()}
		repo.On("CompleteSuccessfulLogin", expected, mock.Anything).Return(nil, failure).Once()
		result, err := NewUserService(repo, nil, nil).CompleteSuccessfulLogin(expected)
		require.Nil(t, result)
		require.Error(t, err)
		if failure != nil {
			require.ErrorIs(t, err, failure)
		}
		repo.AssertExpectations(t)
	}
}
