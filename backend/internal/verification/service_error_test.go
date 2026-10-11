package verification

import (
	"errors"
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type runLookupErrorRepository struct {
	*fakeVerificationRepository
	lookupErr error
}

func (r *runLookupErrorRepository) FindRunForOwner(string, uuid.UUID) (*models.VerificationRun, error) {
	return nil, r.lookupErr
}

func TestRunDetailsForOwnerDistinguishesNotFoundFromStorageErrors(t *testing.T) {
	storageErr := errors.New("database unavailable")
	tests := []struct {
		name           string
		lookupErr      error
		wantNotFound   bool
		wantStorageErr bool
	}{
		{name: "not found", lookupErr: gorm.ErrRecordNotFound, wantNotFound: true},
		{name: "storage failure", lookupErr: storageErr, wantStorageErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &runLookupErrorRepository{
				fakeVerificationRepository: &fakeVerificationRepository{},
				lookupErr:                  test.lookupErr,
			}
			service := NewService(repository, nil, nil)
			result, err := service.RunDetailsForOwner("alice", uuid.New())
			if err == nil || result != nil {
				t.Fatalf("RunDetailsForOwner() = (%#v, %v), want an error and no result", result, err)
			}
			if got := errors.Is(err, ErrVerificationRunNotFound); got != test.wantNotFound {
				t.Fatalf("errors.Is(err, ErrVerificationRunNotFound) = %t, want %t: %v", got, test.wantNotFound, err)
			}
			if got := errors.Is(err, storageErr); got != test.wantStorageErr {
				t.Fatalf("errors.Is(err, storageErr) = %t, want %t: %v", got, test.wantStorageErr, err)
			}
		})
	}
}
