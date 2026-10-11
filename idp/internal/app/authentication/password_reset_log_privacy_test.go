package authentication

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type resetPrivacyUserService struct {
	*fakeUserService
	clearErr error
}

func (s *resetPrivacyUserService) ClearPasswordResetToken(id uuid.UUID, token string) error {
	if s.clearErr != nil {
		return s.clearErr
	}
	return s.fakeUserService.ClearPasswordResetToken(id, token)
}

func TestPasswordResetBackendFailuresDoNotLeakPrivateDetails(t *testing.T) {
	for _, stage := range []string{"store", "lookup", "consume", "clear"} {
		t.Run(stage, func(t *testing.T) {
			setupAuthConfig(t)
			const secret = "synthetic-private-db-password-reset-token"
			failure := errors.New(secret)
			expires := time.Now().Add(time.Hour)
			user := &models.User{ID: uuid.New(), Email: "operator@example.com", ResetPasswordToken: utils.PasswordResetTokenDigest("reset-token"), ResetTokenExpires: &expires}
			users := &resetPrivacyUserService{fakeUserService: &fakeUserService{userByEmail: user, userByResetToken: user}}
			sender := fakePasswordResetSender{configured: true}
			switch stage {
			case "store":
				users.storeResetError = failure
			case "lookup":
				users.resetLookupErr = failure
			case "consume":
				users.consumeResetErr = failure
			case "clear":
				users.clearErr = failure
				sender.err = errors.New("delivery unavailable")
			}
			logger := &captureLogger{}
			svc := &service{userService: users, passwordResetter: sender, logger: logger}
			var err error
			if stage == "store" || stage == "clear" {
				_, _, err = svc.RequestPasswordReset(user.Email)
			} else {
				err = svc.ConfirmPasswordReset("reset-token", "new-strong-password")
			}
			if err == nil {
				t.Fatal("backend failure was ignored")
			}
			output := strings.Join(logger.messages, "\n")
			if output == "" {
				t.Fatal("operational failure was not logged")
			}
			if strings.Contains(output, secret) || strings.Contains(err.Error(), secret) {
				t.Fatal("private backend error leaked")
			}
		})
	}
}
