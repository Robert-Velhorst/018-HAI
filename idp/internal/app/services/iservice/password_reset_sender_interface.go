package iservice

import (
	"context"
	"time"
)

// PasswordResetSender sends a reset code through an operator-configured,
// private recovery channel. Implementations must not publish the code to a
// shared event stream.
type PasswordResetSender interface {
	Configured() bool
	SendPasswordReset(ctx context.Context, email, resetToken string, expiresAt time.Time) error
}
