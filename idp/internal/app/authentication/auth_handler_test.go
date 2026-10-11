package authentication

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"automation-hub-idp/internal/app/dto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRequestPasswordResetDoesNotRevealServiceErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&middlewareAuthService{})
	router := gin.New()
	router.POST("/request-password-reset", handler.RequestPasswordReset)

	form := url.Values{"email": {"missing@example.com"}}
	request := httptest.NewRequest(http.MethodPost, "/request-password-reset", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "If recovery is available")
	require.NotContains(t, recorder.Body.String(), "not implemented")
}

func TestRequestPasswordResetForwardsRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{}
	router := gin.New()
	router.POST("/request-password-reset", NewHandler(service).RequestPasswordReset)

	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	form := url.Values{"email": {"operator@example.com"}}
	request := httptest.NewRequest(http.MethodPost, "/request-password-reset", strings.NewReader(form.Encode())).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, requestContext, service.passwordResetContext)
}

func TestIsUserAuthenticatedRefreshesSessionAndReturnsVerifiedTokenInternally(t *testing.T) {
	for _, test := range []struct {
		name        string
		accessToken string
	}{
		{name: "refresh-only session"},
		{name: "expired access cookie", accessToken: "expired-access"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			service := &middlewareAuthService{
				refreshResult: &dto.TokenDetails{
					AccessToken: "refreshed-access",
					AtExpires:   time.Now().Add(time.Hour).Unix(),
				},
			}
			router := gin.New()
			router.GET("/auth-check", NewHandler(service).IsUserAuthenticated)

			request := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
			request.Header.Set(authSubrequestHeader, authSubrequestHeaderExpected)
			if test.accessToken != "" {
				request.AddCookie(&http.Cookie{Name: "access_token", Value: test.accessToken})
			}
			request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "valid-refresh"})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "refreshed-access", recorder.Header().Get(verifiedAccessTokenHeader))
			require.Contains(t, recorder.Header().Get("Set-Cookie"), "access_token=refreshed-access")
			require.Equal(t, 1, service.refreshCalls)
		})
	}
}

func TestIsUserAuthenticatedReturnsExistingVerifiedTokenInternallyWithoutRotatingCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{valid: true}
	router := gin.New()
	router.GET("/auth-check", NewHandler(service).IsUserAuthenticated)

	request := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	request.Header.Set(authSubrequestHeader, authSubrequestHeaderExpected)
	request.AddCookie(&http.Cookie{Name: "access_token", Value: "valid-access"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "valid-access", recorder.Header().Get(verifiedAccessTokenHeader))
	require.Empty(t, recorder.Header().Get("Set-Cookie"))
	require.Zero(t, service.refreshCalls)
}

func TestIsUserAuthenticatedDoesNotExposeVerifiedTokenOnPublicCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{valid: true}
	router := gin.New()
	router.GET("/auth-check", NewHandler(service).IsUserAuthenticated)

	request := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	request.AddCookie(&http.Cookie{Name: "access_token", Value: "valid-access"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Header().Get(verifiedAccessTokenHeader))
}

func TestCurrentSessionIgnoresIdentityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	service := &middlewareAuthService{
		valid:         true,
		userID:        userID,
		identityToken: "valid-access",
		sessionResult: &dto.AuthSession{
			Authenticated: true,
			Subject:       userID.String(),
			Role:          "viewer",
			Permissions:   dto.AuthSessionPermissions{CanRead: true},
		},
	}
	handler := NewHandler(service)
	router := gin.New()
	router.GET("/session", AuthMiddleware(handler), handler.CurrentSession)

	request := httptest.NewRequest(http.MethodGet, "/session", nil)
	request.AddCookie(&http.Cookie{Name: "access_token", Value: "valid-access"})
	request.Header.Set("X-HAI-Role", "owner")
	request.Header.Set("X-HAI-Subject", "spoofed-owner")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var session dto.AuthSession
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &session))
	require.Equal(t, "viewer", session.Role)
	require.Equal(t, userID.String(), session.Subject)
	require.False(t, session.Permissions.CanAdminister)
	require.Equal(t, "valid-access", service.lastSessionToken)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestCurrentSessionReturnsExplicitUnauthenticatedState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&middlewareAuthService{})
	router := gin.New()
	router.GET("/session", handler.CurrentSession)

	request := httptest.NewRequest(http.MethodGet, "/session", nil)
	request.Header.Set("X-HAI-Role", "owner")
	request.Header.Set("X-HAI-Subject", "spoofed-owner")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var session dto.AuthSession
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &session))
	require.False(t, session.Authenticated)
	require.Empty(t, session.Subject)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestCurrentSessionRefreshOnlySessionSetsAccessCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	service := &middlewareAuthService{
		userID: userID,
		refreshResult: &dto.TokenDetails{
			AccessToken: "refreshed-access",
			AtExpires:   time.Now().Add(time.Hour).Unix(),
		},
		sessionResult: &dto.AuthSession{
			Authenticated: true,
			Subject:       userID.String(),
			Role:          "operator",
			Permissions: dto.AuthSessionPermissions{
				CanRead:    true,
				CanOperate: true,
			},
		},
	}
	handler := NewHandler(service)
	router := gin.New()
	router.GET("/session", AuthMiddleware(handler), handler.CurrentSession)

	request := httptest.NewRequest(http.MethodGet, "/session", nil)
	request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "valid-refresh"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Header().Get("Set-Cookie"), "access_token=refreshed-access")
	var session dto.AuthSession
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &session))
	require.Equal(t, "operator", session.Role)
	require.Equal(t, userID.String(), session.Subject)
	require.False(t, session.Permissions.CanApprove)
	require.Equal(t, "refreshed-access", service.lastSessionToken)
}

func TestLogoutRevokesRefreshedAccessTokenAndClearsBothCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	service := &middlewareAuthService{
		userID: userID,
		refreshResult: &dto.TokenDetails{
			AccessToken: "refreshed-access",
			AtExpires:   time.Now().Add(time.Hour).Unix(),
		},
	}
	handler := NewHandler(service)
	router := gin.New()
	router.GET("/logout", AuthMiddleware(handler), handler.Logout)

	request := httptest.NewRequest(http.MethodGet, "/logout", nil)
	request.AddCookie(&http.Cookie{Name: "access_token", Value: "expired-access"})
	request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "valid-refresh"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "refreshed-access", service.logoutToken)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 3, "one refreshed access cookie plus two deletion cookies")
	deleted := map[string]bool{}
	for _, cookie := range cookies {
		if cookie.MaxAge < 0 {
			deleted[cookie.Name] = true
		}
	}
	require.True(t, deleted["access_token"])
	require.True(t, deleted["refresh_token"])
}

func TestLogoutClearsBrowserSessionWhenRevocationFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{
		valid:     true,
		userID:    uuid.New(),
		logoutErr: errors.New("block list unavailable"),
	}
	handler := NewHandler(service)
	router := gin.New()
	router.GET("/logout", AuthMiddleware(handler), handler.Logout)

	request := httptest.NewRequest(http.MethodGet, "/logout", nil)
	request.AddCookie(&http.Cookie{Name: "access_token", Value: "refreshed-access"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	deleted := map[string]bool{}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.MaxAge < 0 {
			deleted[cookie.Name] = true
		}
	}
	require.True(t, deleted["access_token"])
	require.True(t, deleted["refresh_token"])
}

func TestGoogleLoginBindsSignedStateToBrowserCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{
		googleAuthURL: "https://accounts.google.test/auth?client_id=hai&state=signed-state",
	}
	router := gin.New()
	router.GET("/google/login", NewHandler(service).GoogleLogin)

	request := httptest.NewRequest(http.MethodGet, "/google/login", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, service.googleAuthURL, recorder.Header().Get("Location"))
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, googleOAuthStateCookie, cookies[0].Name)
	require.Equal(t, "signed-state", cookies[0].Value)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
}

func TestGoogleCallbackRejectsStateNotBoundToBrowser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, cookieState := range []string{"", "different-state"} {
		service := &middlewareAuthService{
			googleTokens: &dto.TokenDetails{
				AccessToken:  "access",
				RefreshToken: "refresh",
				AtExpires:    time.Now().Add(time.Hour).Unix(),
				RtExpires:    time.Now().Add(24 * time.Hour).Unix(),
			},
		}
		router := gin.New()
		router.GET("/google/callback", NewHandler(service).GoogleCallback)
		request := httptest.NewRequest(http.MethodGet, "/google/callback?code=code&state=signed-state", nil)
		if cookieState != "" {
			request.AddCookie(&http.Cookie{Name: googleOAuthStateCookie, Value: cookieState})
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusFound, recorder.Code)
		require.Equal(t, "/login?error=google_failed", recorder.Header().Get("Location"))
		require.Zero(t, service.googleLoginCalls)
	}
}

func TestGoogleCallbackAcceptsMatchingBrowserStateOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{
		googleTokens: &dto.TokenDetails{
			AccessToken:  "access",
			RefreshToken: "refresh",
			AtExpires:    time.Now().Add(time.Hour).Unix(),
			RtExpires:    time.Now().Add(24 * time.Hour).Unix(),
		},
	}
	router := gin.New()
	router.GET("/google/callback", NewHandler(service).GoogleCallback)
	request := httptest.NewRequest(http.MethodGet, "/google/callback?code=code&state=signed-state", nil)
	request.AddCookie(&http.Cookie{Name: googleOAuthStateCookie, Value: "signed-state"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, "/", recorder.Header().Get("Location"))
	require.Equal(t, 1, service.googleLoginCalls)
	deletedState := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == googleOAuthStateCookie && cookie.MaxAge < 0 {
			deletedState = true
		}
	}
	require.True(t, deletedState)
}
