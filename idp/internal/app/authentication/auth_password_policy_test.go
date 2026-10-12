package authentication

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChangePasswordRejectsWeakPasswordBeforeTokenValidation(t *testing.T) {
	svc := &service{logger: noopLogger{}}

	err := svc.ChangePassword("not-a-token", "current-password", "short")

	require.ErrorIs(t, err, ErrRegistrationPasswordWeak)
}
