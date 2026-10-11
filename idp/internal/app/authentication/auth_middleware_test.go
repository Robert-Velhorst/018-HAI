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
			const sessionVersion int64 = 9
			service := &middlewareAuthService{
				refreshResult: &dto.TokenDetails{AccessToken: "refreshed-access", AtExpires: time.Now().Add(time.Hour).Unix()},
				userID:        userID,
				sessionResult: &dto.AuthSession{Authenticated: true, Subject: userID.String(), SessionVersion: sessionVersion},
			}
			handler := NewHandler(service)
			router := gin.New()
			router.GET("/protected", AuthMiddleware(handler), func(c *gin.Context) {
				value, ok := c.Get("userID")
				if !ok || value != userID {
					c.Status(http.StatusInternalServerError)
					return
				}
				session, ok := c.Get("authSession")
				if !ok || session.(*dto.AuthSession).SessionVersion != sessionVersion {
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
			if service.lastSessionToken != "refreshed-access" {
				t.Fatalf("session token = %q, want refreshed access token", service.lastSessionToken)
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
	capabilities         dto.AuthCapabilities
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
	localPreviewResult   *dto.TokenDetails
	localPreviewErr      error
	localPreviewCalls    int
	resetRequestCalls    int
	requestedResetEmail  string
	resetConfirmToken    string
	resetConfirmPassword string
	resetConfirmErr      error
	googleLoginCalls     int
}

func (s *middlewareAuthService) Capabilities() dto.AuthCapabilities { return s.capabilities }

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
	s.localPreviewCalls++
	if s.localPreviewErr != nil {
		return nil, s.localPreviewErr
	}
	if s.localPreviewResult == nil {
		return nil, errors.New("not implemented")
	}
	return s.localPreviewResult, nil
}
func (s *middlewareAuthService) Logout(token string) error {
	s.logoutToken = token
	return s.logoutErr
}
func (s *middlewareAuthService) RefreshToken(string) (*dto.TokenDetails, error) {
	s.refreshCalls++
	return s.refreshResult, nil
}
func (s *middlewareAuthService) IsUserAuthenticated(string) (bool, error) { return s.valid, nil }
func (s *middlewareAuthService) RequestPasswordReset(email string) (string, time.Time, error) {
	s.resetRequestCalls++
	s.requestedResetEmail = email
	return "", time.Time{}, errors.New("not implemented")
}
func (s *middlewareAuthService) ConfirmPasswordReset(token, password string) error {
	s.resetConfirmToken = token
	s.resetConfirmPassword = password
	if s.resetConfirmErr != nil {
		return s.resetConfirmErr
	}
	return ErrInvalidPasswordReset
}
func (s *middlewareAuthService) ChangePassword(string, string, string) error {
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
