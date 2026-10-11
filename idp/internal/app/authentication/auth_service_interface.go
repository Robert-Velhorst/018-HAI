package authentication

import (
	"automation-hub-idp/internal/app/dto"
	"context"
	"github.com/google/uuid"
	"time"
)

type IService interface {
	Capabilities() dto.AuthCapabilities
	Register(userDTO dto.UserDTO) (*dto.UserResponse, error)
	Login(email, password string) (*dto.TokenDetails, error)
	// GoogleAuthURL returns the Google consent URL for "Sign in with Google".
	GoogleAuthURL() (string, error)
	// LoginWithGoogle completes the Google flow and returns a HAI session,
	// creating the user on first sign-in.
	LoginWithGoogle(ctx context.Context, code, state string) (*dto.TokenDetails, error)
	// LocalPreviewLogin is an explicitly enabled, local-only owner session for
	// a single-user installation. It is not a general authentication bypass.
	LocalPreviewLogin() (*dto.TokenDetails, error)
	Logout(ctx context.Context, accessToken string) error
	RefreshToken(ctx context.Context, refreshToken string) (*dto.TokenDetails, error)
	IsUserAuthenticated(ctx context.Context, accessToken string) (bool, error)
	RequestPasswordReset(ctx context.Context, email string) (string, time.Time, error)
	ConfirmPasswordReset(token, newPassword string) error
	ChangePassword(accessToken string, newPassword string) error
	GetIdFromToken(accessToken string) (uuid.UUID, error)
	GetSessionFromToken(accessToken string) (*dto.AuthSession, error)
}
