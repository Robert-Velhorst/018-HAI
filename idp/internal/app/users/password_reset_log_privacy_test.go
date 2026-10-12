package users

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestPasswordResetUserServiceRedactsRepositoryAndHasherFailures(t *testing.T) {
	for _, stage := range []string{"store", "lookup", "hash", "consume", "clear"} {
		t.Run(stage, func(t *testing.T) {
			const secret = "synthetic-private-repository-password-reset-token"
			failure := errors.New(secret)
			repo := new(MockUserRepository)
			hasher := new(MockPasswordHasher)
			logger := new(MockLogger)
			var output string
			logger.On("Error", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				output += fmt.Sprintf(args.String(0), args.Get(1).([]interface{})...)
			}).Return()
			id := uuid.New()
			svc := NewUserService(repo, logger, hasher)
			var err error
			switch stage {
			case "store":
				digest := utils.PasswordResetTokenDigest("reset-token")
				repo.On("SetPasswordResetToken", id, int64(1), digest, mock.Anything, mock.Anything).Return(failure)
				err = svc.StorePasswordResetToken(id, 1, digest, time.Now().Add(time.Hour))
			case "lookup":
				repo.On("FindByResetToken", "reset-token").Return((*models.User)(nil), failure)
				_, err = svc.GetUserByResetToken("reset-token")
			case "hash", "consume":
				if stage == "hash" {
					hasher.On("Hash", "new-strong-password").Return("", failure)
				} else {
					hasher.On("Hash", "new-strong-password").Return("hash", nil)
					repo.On("ConsumePasswordResetToken", id, "reset-token", "hash", mock.Anything).Return(failure)
				}
				err = svc.ConsumePasswordResetToken(id, "reset-token", "new-strong-password")
			case "clear":
				repo.On("ClearPasswordResetToken", id, "reset-token").Return(failure)
				err = svc.ClearPasswordResetToken(id, "reset-token")
			}
			if err == nil || output == "" {
				t.Fatal("failure was ignored or not diagnosed")
			}
			if strings.Contains(output, secret) || strings.Contains(output, id.String()) || strings.Contains(err.Error(), secret) {
				t.Fatal("private backend details leaked")
			}
			repo.AssertExpectations(t)
			hasher.AssertExpectations(t)
		})
	}
}
