package authentication

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"automation-hub-idp/internal/app/dto"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestAuthMiddlewareRefreshesBeforeResolvingIdentity(t *testing.T) {
	for _, test := range []struct {
		name        string
		accessToken string
	}{
		{name: "expired access cookie", accessToken: "expired-access"},
		{name: "refresh-only session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			userID := uuid.New()
			service := &middlewareAuthService{
				refreshResult: &dto.TokenDetails{AccessToken: "refreshed-access", AtExpires: time.Now().Add(time.Hour).Unix()},
				userID:        userID,
			}
			handler := NewHandler(service)
			router := gin.New()
			router.GET("/protected", AuthMiddleware(handler), func(c *gin.Context) {
				value, ok := c.Get("userID")
				if !ok || value != userID {
					c.Status(http.StatusInternalServerError)
					return
				}
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if test.accessToken != "" {
				request.AddCookie(&http.Cookie{Name: "access_token", Value: test.accessToken})
			}
			request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "valid-refresh"})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNoContent, recorder.Body.String())
			}
			if service.refreshCalls != 1 {
				t.Fatalf("refresh calls = %d, want 1", service.refreshCalls)
			}
			if service.lastIdentityToken != "refreshed-access" {
				t.Fatalf("identity token = %q, want refreshed access token", service.lastIdentityToken)
			}
			if got := recorder.Header().Get("Set-Cookie"); got == "" {
				t.Fatal("expected refreshed access-token cookie")
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":         true,
		"localhost:8088":    true,
		"127.0.0.1":         true,
		"127.0.0.1:8088":    true,
		"[::1]:8088":        true,
		"192.168.1.10:8088": false,
		"example.com":       false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %t, want %t", host, got, want)
		}
	}
}

type middlewareAuthService struct {
	valid                bool
	refreshResult        *dto.TokenDetails
	userID               uuid.UUID
	refreshCalls         int
	lastIdentityToken    string
	identityToken        string
	sessionResult        *dto.AuthSession
	lastSessionToken     string
	logoutToken          string
	logoutErr            error
	googleAuthURL        string
	googleTokens         *dto.TokenDetails
	googleLoginCalls     int
	passwordResetContext context.Context
}

func (s *middlewareAuthService) Capabilities() dto.AuthCapabilities { return dto.AuthCapabilities{} }

func (s *middlewareAuthService) Register(dto.UserDTO) (*dto.UserResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *middlewareAuthService) Login(string, string) (*dto.TokenDetails, error) {
	return nil, errors.New("not implemented")
}
func (s *middlewareAuthService) GoogleAuthURL() (string, error) {
	if s.googleAuthURL == "" {
		return "", errors.New("not implemented")
	}
	return s.googleAuthURL, nil
}
func (s *middlewareAuthService) LoginWithGoogle(context.Context, string, string) (*dto.TokenDetails, error) {
	s.googleLoginCalls++
	if s.googleTokens == nil {
		return nil, errors.New("not implemented")
	}
	return s.googleTokens, nil
}
func (s *middlewareAuthService) LocalPreviewLogin() (*dto.TokenDetails, error) {
	return nil, errors.New("not implemented")
}
func (s *middlewareAuthService) Logout(_ context.Context, token string) error {
	s.logoutToken = token
	return s.logoutErr
}
func (s *middlewareAuthService) RefreshToken(context.Context, string) (*dto.TokenDetails, error) {
	s.refreshCalls++
	return s.refreshResult, nil
}
func (s *middlewareAuthService) IsUserAuthenticated(context.Context, string) (bool, error) {
	return s.valid, nil
}

func (s *middlewareAuthService) RequestPasswordReset(ctx context.Context, _ string) (string, time.Time, error) {
	s.passwordResetContext = ctx
	return "", time.Time{}, errors.New("not implemented")
}
func (s *middlewareAuthService) ConfirmPasswordReset(string, string) error {
	return errors.New("not implemented")
}
func (s *middlewareAuthService) ChangePassword(string, string) error {
	return errors.New("not implemented")
}
func (s *middlewareAuthService) GetIdFromToken(token string) (uuid.UUID, error) {
	s.lastIdentityToken = token
	expected := s.identityToken
	if expected == "" {
		expected = "refreshed-access"
	}
	if token != expected {
		return uuid.Nil, errors.New("expired access token was used")
	}
	return s.userID, nil
}
func (s *middlewareAuthService) GetSessionFromToken(token string) (*dto.AuthSession, error) {
	s.lastSessionToken = token
	if s.sessionResult != nil {
		return s.sessionResult, nil
	}
	return &dto.AuthSession{
		Authenticated: true,
		Subject:       s.userID.String(),
		Role:          "owner",
		Permissions: dto.AuthSessionPermissions{
			CanRead:       true,
			CanOperate:    true,
			CanApprove:    true,
			CanAdminister: true,
		},
	}, nil
}
