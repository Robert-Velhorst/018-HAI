package authentication

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type rememberMeAuthService struct {
	IService
	loginResult *dto.TokenDetails
}

func (s *rememberMeAuthService) Login(string, string) (*dto.TokenDetails, error) {
	return s.loginResult, nil
}

func TestLoginRememberMeSelectsBrowserCookiePersistenceAndKeepsLegacyDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("IDP_COOKIE_SECURE", "true")

	for _, test := range []struct {
		name       string
		body       string
		persistent bool
	}{
		{name: "unchecked session", body: `{"email":"operator@example.com","password":"correct horse","rememberMe":false}`},
		{name: "checked persistent", body: `{"email":"operator@example.com","password":"correct horse","rememberMe":true}`, persistent: true},
		{name: "legacy client defaults persistent", body: `{"email":"operator@example.com","password":"correct horse"}`, persistent: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			accessExpires := time.Unix(time.Now().Add(time.Hour).Unix(), 0)
			refreshExpires := time.Unix(time.Now().Add(24*time.Hour).Unix(), 0)
			service := &rememberMeAuthService{loginResult: &dto.TokenDetails{
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				AtExpires:    accessExpires.Unix(),
				RtExpires:    refreshExpires.Unix(),
			}}
			router := gin.New()
			router.POST("/login", NewHandler(service).Login)
			request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			byName := map[string]*http.Cookie{}
			for _, cookie := range recorder.Result().Cookies() {
				byName[cookie.Name] = cookie
			}
			require.Len(t, byName, 3)
			require.Equal(t, "access-token", byName["access_token"].Value)
			require.Equal(t, "refresh-token", byName["refresh_token"].Value)
			require.Equal(t, rememberMeValue(test.persistent), byName[rememberMeCookie].Value)

			for _, cookie := range byName {
				require.True(t, cookie.HttpOnly)
				require.True(t, cookie.Secure)
				require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
				require.Equal(t, "/", cookie.Path)
				if test.persistent {
					require.False(t, cookie.Expires.IsZero())
				} else {
					require.True(t, cookie.Expires.IsZero(), "%s should be a browser-session cookie", cookie.Name)
					require.Zero(t, cookie.MaxAge)
				}
			}
			if test.persistent {
				require.True(t, byName["access_token"].Expires.Equal(accessExpires))
				require.True(t, byName["refresh_token"].Expires.Equal(refreshExpires))
				require.True(t, byName[rememberMeCookie].Expires.Equal(refreshExpires))
			}
		})
	}
}

func TestAuthenticationJSONEndpointsRejectOversizedBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Repeat(" ", maxAuthJSONBodyBytes+1)
	tests := []struct {
		name   string
		route  string
		handle gin.HandlerFunc
	}{
		{name: "registration", route: "/register", handle: NewHandler(&middlewareAuthService{}).Register},
		{name: "login", route: "/login", handle: NewHandler(&middlewareAuthService{}).Login},
		{name: "password reset", route: "/reset", handle: NewHandler(&middlewareAuthService{}).ConfirmPasswordReset},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.POST(test.route, test.handle)
			request := httptest.NewRequest(http.MethodPost, test.route, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "password")
		})
	}
}

func TestPublicRegistrationRateLimitIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&middlewareAuthService{})
	router := gin.New()
	router.POST("/register", handler.Register)
	for i := 0; i < publicRegisterIPLimit; i++ {
		request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("{}"))
		request.RemoteAddr = "192.0.2.40:9000"
		request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusInternalServerError, recorder.Code, "the service stub's expected response should prove the request reached application logic")
	}
	request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("{}"))
	request.RemoteAddr = "192.0.2.40:9000"
	request.Header.Set("X-Forwarded-For", "203.0.113.200")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "900", recorder.Header().Get("Retry-After"))
}

func TestAuthenticationHandlersRejectPasswordsAboveBcryptByteLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	password := strings.Repeat("a", utils.MaximumPasswordBytes+1)
	tests := []struct {
		name   string
		path   string
		body   string
		handle func(*Handler) gin.HandlerFunc
	}{
		{
			name:   "registration",
			path:   "/register",
			body:   fmt.Sprintf(`{"email":"operator@example.com","password":%q}`, password),
			handle: func(handler *Handler) gin.HandlerFunc { return handler.Register },
		},
		{
			name:   "password reset",
			path:   "/confirm-password-reset",
			body:   fmt.Sprintf(`{"token":"reset-token","newPassword":%q}`, password),
			handle: func(handler *Handler) gin.HandlerFunc { return handler.ConfirmPasswordReset },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.POST(test.path, test.handle(NewHandler(&service{})))
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), ErrRegistrationPasswordTooLong.Error())
		})
	}
}

type boundedAuthJSONService struct {
	middlewareAuthService
	registerCalls int
	loginCalls    int
}

func (s *boundedAuthJSONService) Register(dto.UserDTO) (*dto.UserResponse, error) {
	s.registerCalls++
	return nil, errors.New("unexpected registration call")
}

func (s *boundedAuthJSONService) Login(string, string) (*dto.TokenDetails, error) {
	s.loginCalls++
	return nil, errors.New("unexpected login call")
}

func TestAuthenticationJSONEndpointsRejectOversizedSuffixesAndMultipleValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	endpoints := []struct {
		name  string
		route string
		valid string
	}{
		{name: "registration", route: "/register", valid: `{"email":"owner@example.com","password":"a-strong-password"}`},
		{name: "login", route: "/login", valid: `{"email":"owner@example.com","password":"a-strong-password"}`},
		{name: "password reset", route: "/reset", valid: `{"token":"reset-token","newPassword":"a-strong-password"}`},
	}
	suffixes := []struct {
		name       string
		makeBody   func(string) string
		wantStatus int
	}{
		{name: "oversized trailing whitespace", makeBody: func(valid string) string { return valid + strings.Repeat(" ", maxAuthJSONBodyBytes) }, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "oversized trailing junk", makeBody: func(valid string) string { return valid + strings.Repeat("x", maxAuthJSONBodyBytes) }, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "duplicate JSON values", makeBody: func(valid string) string { return valid + valid }, wantStatus: http.StatusBadRequest},
	}

	for _, endpoint := range endpoints {
		for _, suffix := range suffixes {
			t.Run(endpoint.name+"/"+suffix.name, func(t *testing.T) {
				service := &boundedAuthJSONService{}
				handler := NewHandler(service)
				var action gin.HandlerFunc
				switch endpoint.name {
				case "registration":
					action = handler.Register
				case "login":
					action = handler.Login
				case "password reset":
					action = handler.ConfirmPasswordReset
				}
				router := gin.New()
				router.POST(endpoint.route, action)
				request := httptest.NewRequest(http.MethodPost, endpoint.route, strings.NewReader(suffix.makeBody(endpoint.valid)))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)

				require.Equal(t, suffix.wantStatus, recorder.Code)
				require.Zero(t, service.registerCalls)
				require.Zero(t, service.loginCalls)
				require.Empty(t, service.resetConfirmToken)
				require.Empty(t, service.resetConfirmPassword)
			})
		}
	}
}

func TestAuthenticationJSONEndpointsAcceptTrailingWhitespaceAfterOneValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	endpoints := []struct {
		name  string
		route string
		valid string
	}{
		{name: "registration", route: "/register", valid: `{"email":"owner@example.com","password":"a-strong-password"}`},
		{name: "login", route: "/login", valid: `{"email":"owner@example.com","password":"a-strong-password"}`},
		{name: "password reset", route: "/reset", valid: `{"token":"reset-token","newPassword":"a-strong-password"}`},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			service := &boundedAuthJSONService{}
			handler := NewHandler(service)
			var action gin.HandlerFunc
			switch endpoint.name {
			case "registration":
				action = handler.Register
			case "login":
				action = handler.Login
			case "password reset":
				action = handler.ConfirmPasswordReset
			}
			router := gin.New()
			router.POST(endpoint.route, action)
			request := httptest.NewRequest(http.MethodPost, endpoint.route, strings.NewReader(endpoint.valid+" \t\r\n"))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			switch endpoint.name {
			case "registration":
				require.Equal(t, 1, service.registerCalls)
			case "login":
				require.Equal(t, 1, service.loginCalls)
			case "password reset":
				require.Equal(t, "reset-token", service.resetConfirmToken)
				require.Equal(t, "a-strong-password", service.resetConfirmPassword)
			}
		})
	}
}

func TestRequestPasswordResetMapsOversizedFormToRequestEntityTooLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &boundedAuthJSONService{}
	router := gin.New()
	router.POST("/request-password-reset", NewHandler(service).RequestPasswordReset)
	body := "email=owner%40example.com&padding=" + strings.Repeat("x", 8*1024)
	request := httptest.NewRequest(http.MethodPost, "/request-password-reset", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Zero(t, service.resetRequestCalls, "an oversized form must not reach the reset service")
}

func TestLocalPreviewRejectsNonLoopbackPeerDespiteSpoofedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureLocalPreviewTest(t)
	service := &middlewareAuthService{localPreviewResult: &dto.TokenDetails{
		AccessToken:  "owner-access",
		RefreshToken: "owner-refresh",
		AtExpires:    time.Now().Add(time.Hour).Unix(),
		RtExpires:    time.Now().Add(24 * time.Hour).Unix(),
	}}
	router := gin.New()
	router.POST("/local-preview", NewHandler(service).LocalPreview)

	request := httptest.NewRequest(http.MethodPost, "http://localhost:8088/local-preview", nil)
	request.RemoteAddr = "203.0.113.44:51000"
	request.Header.Set("Origin", "http://localhost:8088")
	request.Header.Set("Forwarded", "for=127.0.0.1;host=localhost")
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	request.Header.Set("X-Forwarded-Host", "localhost")
	request.Header.Set("X-Real-IP", "127.0.0.1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Zero(t, service.localPreviewCalls, "must reject before requesting owner credentials")
	require.Empty(t, recorder.Header().Values("Set-Cookie"), "must not issue owner cookies")
}

func TestLocalPreviewUsesSocketPeerAndLoopbackHostNotForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureLocalPreviewTest(t)
	service := &middlewareAuthService{localPreviewResult: &dto.TokenDetails{
		AccessToken:  "owner-access",
		RefreshToken: "owner-refresh",
		AtExpires:    time.Now().Add(time.Hour).Unix(),
		RtExpires:    time.Now().Add(24 * time.Hour).Unix(),
	}}
	router := gin.New()
	router.POST("/local-preview", NewHandler(service).LocalPreview)

	server := httptest.NewServer(router)
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/local-preview", nil)
	require.NoError(t, err)
	request.Header.Set("Origin", server.URL)
	request.Header.Set("Forwarded", "for=203.0.113.44;host=attacker.example")
	request.Header.Set("X-Forwarded-For", "203.0.113.44")
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.Equal(t, 1, service.localPreviewCalls)
	cookies := response.Cookies()
	require.Len(t, cookies, 2)
	require.Equal(t, "access_token", cookies[0].Name)
	require.Equal(t, "refresh_token", cookies[1].Name)
}

func TestLocalPreviewAcceptsGatewayProofButStillRequiresLoopbackBrowserOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := configureLocalPreviewTest(t)
	service := &middlewareAuthService{localPreviewResult: &dto.TokenDetails{
		AccessToken: "owner-access", RefreshToken: "owner-refresh",
		AtExpires: time.Now().Add(time.Hour).Unix(), RtExpires: time.Now().Add(24 * time.Hour).Unix(),
	}}
	router := gin.New()
	router.POST("/local-preview", NewHandler(service).LocalPreview)

	request := httptest.NewRequest(http.MethodPost, "http://localhost:8088/local-preview", nil)
	request.RemoteAddr = "172.19.0.4:43122"
	request.Header.Set("Origin", "http://localhost:8088")
	request.Header.Set(localPreviewGatewayHeader, secret)
	request.Header.Set("X-Forwarded-For", "203.0.113.44")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, 1, service.localPreviewCalls)
	require.Len(t, recorder.Header().Values("Set-Cookie"), 2)
}

func TestLocalPreviewRejectsWrongGatewayProofAndNonLoopbackOrigin(t *testing.T) {
	for _, test := range []struct {
		name   string
		origin string
		token  string
	}{
		{name: "wrong gateway token", origin: "http://localhost:8088", token: strings.Repeat("c", 64)},
		{name: "remote browser origin", origin: "https://attacker.example", token: strings.Repeat("a", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			secret := configureLocalPreviewTest(t)
			service := &middlewareAuthService{localPreviewResult: &dto.TokenDetails{AccessToken: "owner-access"}}
			router := gin.New()
			router.POST("/local-preview", NewHandler(service).LocalPreview)
			request := httptest.NewRequest(http.MethodPost, "http://localhost:8088/local-preview", nil)
			request.RemoteAddr = "172.19.0.4:43122"
			request.Header.Set("Origin", test.origin)
			token := test.token
			if test.name == "remote browser origin" {
				token = secret
			}
			request.Header.Set(localPreviewGatewayHeader, token)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Zero(t, service.localPreviewCalls)
			require.Empty(t, recorder.Header().Values("Set-Cookie"))
		})
	}
}

func TestCapabilitiesOnlyAdvertisesLocalPreviewToLoopbackBrowser(t *testing.T) {
	for _, test := range []struct {
		name   string
		target string
		origin string
		want   bool
	}{
		{name: "local browser", target: "http://localhost:8088/capabilities", origin: "http://localhost:8088", want: true},
		{name: "remote browser", target: "https://ha.example/capabilities", origin: "https://ha.example"},
	} {
		t.Run(test.name, func(t *testing.T) {
			configureLocalPreviewTest(t)
			service := &middlewareAuthService{capabilities: dto.AuthCapabilities{LocalPreviewEnabled: true}}
			router := gin.New()
			router.GET("/capabilities", NewHandler(service).Capabilities)
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			request.Header.Set("Origin", test.origin)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			var capabilities dto.AuthCapabilities
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &capabilities))
			require.Equal(t, test.want, capabilities.LocalPreviewEnabled)
		})
	}
}

func TestLocalPreviewRejectsNonLoopbackOrMissingOriginFromLoopbackPeer(t *testing.T) {
	for _, origin := range []string{"https://attacker.example", "", "null"} {
		t.Run(origin, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			service := &middlewareAuthService{localPreviewResult: &dto.TokenDetails{AccessToken: "owner-access"}}
			router := gin.New()
			router.POST("/local-preview", NewHandler(service).LocalPreview)

			request := httptest.NewRequest(http.MethodPost, "http://localhost:8088/local-preview", nil)
			request.RemoteAddr = "127.0.0.1:51000"
			if origin != "" {
				request.Header.Set("Origin", origin)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Zero(t, service.localPreviewCalls)
			require.Empty(t, recorder.Header().Values("Set-Cookie"))
		})
	}
}

func TestLocalPreviewRejectsNonLoopbackHostFromLoopbackPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureLocalPreviewTest(t)
	service := &middlewareAuthService{localPreviewResult: &dto.TokenDetails{AccessToken: "owner-access"}}
	router := gin.New()
	router.POST("/local-preview", NewHandler(service).LocalPreview)

	request := httptest.NewRequest(http.MethodPost, "http://attacker.example:8088/local-preview", nil)
	request.RemoteAddr = "127.0.0.1:51000"
	request.Header.Set("Origin", "http://localhost:8088")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Zero(t, service.localPreviewCalls)
	require.Empty(t, recorder.Header().Values("Set-Cookie"))
}

func configureLocalPreviewTest(t *testing.T) string {
	t.Helper()
	setupAuthConfig(t)
	secret := strings.Repeat("a", 64)
	t.Setenv("LOCAL_LOGIN_BYPASS_ENABLED", "true")
	t.Setenv("FIRST_RUN_ADMIN_EMAIL", "owner@example.com")
	t.Setenv("LOCAL_PREVIEW_GATEWAY_SECRET", secret)
	require.NoError(t, config.Setup())
	return secret
}

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
	require.Contains(t, recorder.Body.String(), "does not confirm account existence or email delivery")
	require.NotContains(t, recorder.Body.String(), "have been sent")
	require.NotContains(t, recorder.Body.String(), "not implemented")
}

func TestRequestPasswordResetRateLimitsNormalizedAccountAcrossIPs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{}
	handler := NewHandler(service)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.POST("/request-password-reset", handler.RequestPasswordReset)

	for i := 0; i < passwordResetAccountLimit+1; i++ {
		email := "Person@example.com"
		if i > 0 {
			email = " person@EXAMPLE.com "
		}
		form := url.Values{"email": {email}}
		request := httptest.NewRequest(http.MethodPost, "/request-password-reset", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.RemoteAddr = fmt.Sprintf("192.0.2.%d:51000", i+1)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if i < passwordResetAccountLimit {
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), "If recovery is available")
		} else {
			require.Equal(t, http.StatusTooManyRequests, recorder.Code)
			require.NotEmpty(t, recorder.Header().Get("Retry-After"))
			require.NotContains(t, recorder.Body.String(), "Person@example.com")
		}
	}
	require.Equal(t, passwordResetAccountLimit, service.resetRequestCalls)
}

func TestRequestPasswordResetRateLimitsSocketIPNotForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{}
	handler := NewHandler(service)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.POST("/request-password-reset", handler.RequestPasswordReset)

	for i := 0; i < passwordResetIPLimit+1; i++ {
		form := url.Values{"email": {fmt.Sprintf("person-%d@example.com", i)}}
		request := httptest.NewRequest(http.MethodPost, "/request-password-reset", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.RemoteAddr = "192.0.2.20:51000"
		request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if i < passwordResetIPLimit {
			require.Equal(t, http.StatusOK, recorder.Code)
		} else {
			require.Equal(t, http.StatusTooManyRequests, recorder.Code)
		}
	}
	require.Equal(t, passwordResetIPLimit, service.resetRequestCalls)
}

func TestConfirmPasswordResetReadsBearerFromJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{}
	router := gin.New()
	router.POST("/confirm-password-reset", NewHandler(service).ConfirmPasswordReset)
	request := httptest.NewRequest(http.MethodPost, "/confirm-password-reset", strings.NewReader(`{"token":"reset-secret","newPassword":"a-new-strong-password"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code, "the fake service should reject after the body was parsed")
	require.NotContains(t, request.URL.Path, "reset-secret")
	require.Equal(t, "reset-secret", service.resetConfirmToken)
	require.Equal(t, "a-new-strong-password", service.resetConfirmPassword)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestConfirmPasswordResetRateLimitsBeforeParsingOrCallingService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{}
	handler := NewHandler(service)
	router := gin.New()
	router.POST("/confirm-password-reset", handler.ConfirmPasswordReset)

	for i := 0; i < publicResetConfirmIPLimit; i++ {
		body := fmt.Sprintf(`{"token":"guess-%d","newPassword":"a-new-strong-password"}`, i)
		request := httptest.NewRequest(http.MethodPost, "/confirm-password-reset", strings.NewReader(body))
		request.RemoteAddr = "192.0.2.44:51000"
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, "requests within the limit should reach reset-token validation")
	}

	request := httptest.NewRequest(http.MethodPost, "/confirm-password-reset", strings.NewReader(`{"token":"blocked-guess","newPassword":"a-new-strong-password"}`))
	request.RemoteAddr = "192.0.2.44:51000"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.NotEmpty(t, recorder.Header().Get("Retry-After"))
	require.Equal(t, fmt.Sprintf("guess-%d", publicResetConfirmIPLimit-1), service.resetConfirmToken,
		"over-limit attempts must not reach reset-token lookup or password hashing")
}

func TestConfirmPasswordResetReportsInfrastructureFailuresAsServerErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{resetConfirmErr: ErrPasswordResetUnavailable}
	router := gin.New()
	router.POST("/confirm-password-reset", NewHandler(service).ConfirmPasswordReset)
	request := httptest.NewRequest(http.MethodPost, "/confirm-password-reset", strings.NewReader(`{"token":"reset-secret","newPassword":"a-new-strong-password"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "temporarily unavailable")
	require.NotContains(t, recorder.Body.String(), "database")
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
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
					AccessToken:  "refreshed-access",
					RefreshToken: "rotated-refresh",
					AtExpires:    time.Now().Add(time.Hour).Unix(),
					RtExpires:    time.Now().Add(24 * time.Hour).Unix(),
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
			require.Contains(t, recorder.Header().Get(refreshedAccessCookieHeader), "access_token=refreshed-access")
			require.Contains(t, recorder.Header().Get(refreshedRefreshCookieHeader), "refresh_token=rotated-refresh")
			require.Empty(t, recorder.Header().Values("Set-Cookie"))
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

func TestAuthSubrequestPropagatesBothRotatedCookiesWithoutPublicSetCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expires := time.Unix(time.Now().Add(time.Hour).Unix(), 0)
	service := &middlewareAuthService{
		refreshResult: &dto.TokenDetails{
			AccessToken:  "rotated-access",
			RefreshToken: "rotated-refresh",
			AtExpires:    expires.Unix(),
			RtExpires:    expires.Add(24 * time.Hour).Unix(),
		},
	}
	handler := NewHandler(service)
	router := gin.New()
	router.GET("/auth-check", handler.IsUserAuthenticated)
	request := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	request.Header.Set(authSubrequestHeader, authSubrequestHeaderExpected)
	request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "old-refresh"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "rotated-access", recorder.Header().Get(verifiedAccessTokenHeader))
	require.Contains(t, recorder.Header().Get(refreshedAccessCookieHeader), "access_token=rotated-access")
	require.Contains(t, recorder.Header().Get(refreshedRefreshCookieHeader), "refresh_token=rotated-refresh")
	require.Empty(t, recorder.Header().Values("Set-Cookie"), "internal auth checks expose only the captured cookie headers")
}

func TestAccessTokenRefreshPreservesRememberMeCookiePersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("IDP_COOKIE_SECURE", "true")
	accessExpires := time.Unix(time.Now().Add(time.Hour).Unix(), 0)
	refreshExpires := time.Unix(time.Now().Add(24*time.Hour).Unix(), 0)
	for _, test := range []struct {
		name       string
		preference string
		present    bool
		persistent bool
	}{
		{name: "session preference", preference: rememberMeSessionValue, present: true},
		{name: "persistent preference", preference: rememberMePersistentValue, present: true, persistent: true},
		{name: "legacy session defaults persistent", persistent: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &middlewareAuthService{refreshResult: &dto.TokenDetails{
				AccessToken: "refreshed-access", RefreshToken: "rotated-refresh",
				AtExpires: accessExpires.Unix(), RtExpires: refreshExpires.Unix(),
			}}
			handler := NewHandler(service)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/session", nil)
			context.Request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "refresh-token"})
			if test.present {
				context.Request.AddCookie(&http.Cookie{Name: rememberMeCookie, Value: test.preference})
			}

			token, ok := handler.resolveAuthenticatedAccessToken(context)
			require.True(t, ok)
			require.Equal(t, "refreshed-access", token)
			cookies := recorder.Result().Cookies()
			require.Len(t, cookies, 2)
			byName := map[string]*http.Cookie{}
			for _, cookie := range cookies {
				byName[cookie.Name] = cookie
			}
			require.Equal(t, "refreshed-access", byName["access_token"].Value)
			require.Equal(t, "rotated-refresh", byName["refresh_token"].Value)
			for name, cookie := range byName {
				if test.persistent {
					wantExpires := accessExpires
					if name == "refresh_token" {
						wantExpires = refreshExpires
					}
					require.True(t, cookie.Expires.Equal(wantExpires))
				} else {
					require.True(t, cookie.Expires.IsZero())
					require.Zero(t, cookie.MaxAge)
				}
				require.True(t, cookie.HttpOnly)
				require.True(t, cookie.Secure)
				require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
			}
		})
	}
}

func TestLogoutRevokesRefreshedAccessTokenAndClearsAuthCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("IDP_COOKIE_SECURE", "false")
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
	router.POST("/logout", AuthMiddleware(handler), handler.Logout)

	request := httptest.NewRequest(http.MethodPost, "/logout", nil)
	request.Header.Set("Origin", "http://example.com")
	request.AddCookie(&http.Cookie{Name: "access_token", Value: "expired-access"})
	request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "valid-refresh"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "refreshed-access", service.logoutToken)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 4, "one refreshed access cookie plus access, refresh, and remember-me deletion cookies")
	deleted := map[string]bool{}
	for _, cookie := range cookies {
		if cookie.MaxAge < 0 {
			deleted[cookie.Name] = true
		}
	}
	require.True(t, deleted["access_token"])
	require.True(t, deleted["refresh_token"])
	require.True(t, deleted[rememberMeCookie])
}

func TestLogoutClearsBrowserSessionWhenRevocationFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("IDP_COOKIE_SECURE", "false")
	service := &middlewareAuthService{
		valid:     true,
		userID:    uuid.New(),
		logoutErr: errors.New("block list unavailable"),
	}
	handler := NewHandler(service)
	router := gin.New()
	router.POST("/logout", AuthMiddleware(handler), handler.Logout)

	request := httptest.NewRequest(http.MethodPost, "/logout", nil)
	request.Header.Set("Origin", "http://example.com")
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
	require.True(t, deleted[rememberMeCookie])
}

func TestLogoutRequiresSingleExactAllowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("IDP_COOKIE_SECURE", "false")
	tests := []struct {
		name       string
		origins    []string
		wantStatus int
	}{
		{name: "missing origin", wantStatus: http.StatusForbidden},
		{name: "same-site sibling origin", origins: []string{"http://sibling.example.com"}, wantStatus: http.StatusForbidden},
		{name: "wrong scheme", origins: []string{"https://app.example.com"}, wantStatus: http.StatusForbidden},
		{name: "null origin", origins: []string{"null"}, wantStatus: http.StatusForbidden},
		{name: "path is not an origin", origins: []string{"http://app.example.com/path"}, wantStatus: http.StatusForbidden},
		{name: "duplicate origins", origins: []string{"http://app.example.com", "http://app.example.com"}, wantStatus: http.StatusForbidden},
		{name: "exact request origin", origins: []string{"http://app.example.com"}, wantStatus: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &middlewareAuthService{}
			handler := NewHandler(service)
			router := gin.New()
			router.POST("/logout", func(c *gin.Context) {
				c.Set(authenticatedTokenContextKey, "valid-access")
				handler.Logout(c)
			})
			request := httptest.NewRequest(http.MethodPost, "http://app.example.com/logout", nil)
			for _, origin := range test.origins {
				request.Header.Add("Origin", origin)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, test.wantStatus, recorder.Code)
			if test.wantStatus == http.StatusOK {
				require.Equal(t, "valid-access", service.logoutToken)
			} else {
				require.Empty(t, service.logoutToken, "invalid origin must be rejected before session revocation")
			}
		})
	}
}

func TestLogoutOriginIsCheckedBeforeSessionRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("IDP_COOKIE_SECURE", "false")
	service := &middlewareAuthService{
		valid: true,
		userID: uuid.New(),
		refreshResult: &dto.TokenDetails{
			AccessToken: "refreshed-access",
			AtExpires:   time.Now().Add(time.Hour).Unix(),
		},
	}
	handler := NewHandler(service)
	router := gin.New()
	router.POST("/logout", handler.RequireLogoutOrigin, AuthMiddleware(handler), handler.Logout)
	request := httptest.NewRequest(http.MethodPost, "http://example.com/logout", nil)
	request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "valid-refresh"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Zero(t, service.refreshCalls, "a rejected Origin must not trigger session refresh")
	require.Empty(t, service.logoutToken, "a rejected Origin must not revoke a session")
}

func TestGoogleLoginBindsSignedStateToBrowserCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{
		googleAuthURL: "https://accounts.google.test/auth?client_id=hai&state=signed-state",
	}
	router := gin.New()
	router.GET("/google/login", NewHandler(service).GoogleLogin)

	request := httptest.NewRequest(http.MethodGet, "/google/login?returnUrl=/connected-sources", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, service.googleAuthURL, recorder.Header().Get("Location"))
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 2)
	byName := map[string]*http.Cookie{}
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	require.Equal(t, "signed-state", byName[googleOAuthStateCookie].Value)
	require.Equal(t, "/connected-sources", byName[googleOAuthReturnURLCookie].Value)
	for _, cookie := range byName {
		require.True(t, cookie.HttpOnly)
		require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	}
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

func TestGoogleCallbackDenialRequiresMatchingBrowserStateBeforeClearingCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		queryState string
		cookie     string
		wantError  string
		wantClear  bool
	}{
		{name: "forged denial does not clear pending login", queryState: "attacker-state", cookie: "signed-state", wantError: "google_failed"},
		{name: "missing state does not clear pending login", cookie: "signed-state", wantError: "google_failed"},
		{name: "matching denial clears pending login", queryState: "signed-state", cookie: "signed-state", wantError: "google_denied", wantClear: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &middlewareAuthService{}
			router := gin.New()
			router.GET("/google/callback", NewHandler(service).GoogleCallback)
			request := httptest.NewRequest(http.MethodGet, "/google/callback?error=access_denied&state="+test.queryState, nil)
			request.AddCookie(&http.Cookie{Name: googleOAuthStateCookie, Value: test.cookie})
			request.AddCookie(&http.Cookie{Name: googleOAuthReturnURLCookie, Value: "/workflow-engine"})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusFound, recorder.Code)
			require.Equal(t, "/login?error="+test.wantError, recorder.Header().Get("Location"))
			require.Zero(t, service.googleLoginCalls)
			cleared := map[string]bool{}
			for _, cookie := range recorder.Result().Cookies() {
				if cookie.MaxAge < 0 {
					cleared[cookie.Name] = true
				}
			}
			require.Equal(t, test.wantClear, cleared[googleOAuthStateCookie])
			require.Equal(t, test.wantClear, cleared[googleOAuthReturnURLCookie])
		})
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
	request.AddCookie(&http.Cookie{Name: googleOAuthReturnURLCookie, Value: "/workflow-engine?view=advanced"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, "/workflow-engine?view=advanced", recorder.Header().Get("Location"))
	require.Equal(t, 1, service.googleLoginCalls)
	deletedState := false
	deletedReturnURL := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == googleOAuthStateCookie && cookie.MaxAge < 0 {
			deletedState = true
		}
		if cookie.Name == googleOAuthReturnURLCookie && cookie.MaxAge < 0 {
			deletedReturnURL = true
		}
	}
	require.True(t, deletedState)
	require.True(t, deletedReturnURL)
}

func TestGoogleOAuthCarriesSessionPreferenceThroughCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("IDP_COOKIE_SECURE", "true")
	tokens := &dto.TokenDetails{
		AccessToken:  "google-access",
		RefreshToken: "google-refresh",
		AtExpires:    time.Now().Add(time.Hour).Unix(),
		RtExpires:    time.Now().Add(24 * time.Hour).Unix(),
	}
	service := &middlewareAuthService{
		googleAuthURL: "https://accounts.google.test/auth?client_id=hai&state=signed-state",
		googleTokens:  tokens,
	}
	startRouter := gin.New()
	startRouter.GET("/google/login", NewHandler(service).GoogleLogin)
	startRequest := httptest.NewRequest(http.MethodGet, "/google/login?rememberMe=false", nil)
	startRecorder := httptest.NewRecorder()
	startRouter.ServeHTTP(startRecorder, startRequest)
	require.Equal(t, http.StatusFound, startRecorder.Code)

	startCookies := startRecorder.Result().Cookies()
	var oauthRememberCookie *http.Cookie
	for _, cookie := range startCookies {
		if cookie.Name == googleOAuthRememberMeCookie {
			oauthRememberCookie = cookie
		}
	}
	require.NotNil(t, oauthRememberCookie)
	require.Equal(t, rememberMeSessionValue, oauthRememberCookie.Value)
	require.True(t, oauthRememberCookie.Expires.IsZero())
	require.True(t, oauthRememberCookie.HttpOnly)
	require.True(t, oauthRememberCookie.Secure)
	require.Equal(t, http.SameSiteLaxMode, oauthRememberCookie.SameSite)

	callbackRouter := gin.New()
	callbackRouter.GET("/google/callback", NewHandler(service).GoogleCallback)
	callbackRequest := httptest.NewRequest(http.MethodGet, "/google/callback?code=code&state=signed-state", nil)
	for _, cookie := range startCookies {
		callbackRequest.AddCookie(cookie)
	}
	callbackRecorder := httptest.NewRecorder()
	callbackRouter.ServeHTTP(callbackRecorder, callbackRequest)
	require.Equal(t, http.StatusFound, callbackRecorder.Code)

	byName := map[string]*http.Cookie{}
	for _, cookie := range callbackRecorder.Result().Cookies() {
		byName[cookie.Name] = cookie
	}
	for _, name := range []string{"access_token", "refresh_token", rememberMeCookie} {
		cookie := byName[name]
		require.NotNil(t, cookie)
		require.True(t, cookie.Expires.IsZero(), "%s should remain a browser-session cookie", name)
		require.True(t, cookie.HttpOnly)
		require.True(t, cookie.Secure)
		require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	}
	require.Equal(t, "google-access", byName["access_token"].Value)
	require.Equal(t, "google-refresh", byName["refresh_token"].Value)
	require.Equal(t, rememberMeSessionValue, byName[rememberMeCookie].Value)
}

func TestGoogleLoginRejectsUnsafeReturnURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{googleAuthURL: "https://accounts.google.test/auth?client_id=hai&state=signed-state"}
	router := gin.New()
	router.GET("/google/login", NewHandler(service).GoogleLogin)

	request := httptest.NewRequest(http.MethodGet, "/google/login?returnUrl=//untrusted.example", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == googleOAuthReturnURLCookie {
			require.Equal(t, "/", cookie.Value)
			return
		}
	}
	t.Fatal("expected Google return URL cookie")
}

func TestGoogleCallbackRejectsUnsafeReturnURLCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &middlewareAuthService{googleTokens: &dto.TokenDetails{AccessToken: "access", RefreshToken: "refresh", AtExpires: time.Now().Add(time.Hour).Unix(), RtExpires: time.Now().Add(24 * time.Hour).Unix()}}
	router := gin.New()
	router.GET("/google/callback", NewHandler(service).GoogleCallback)
	request := httptest.NewRequest(http.MethodGet, "/google/callback?code=code&state=signed-state", nil)
	request.AddCookie(&http.Cookie{Name: googleOAuthStateCookie, Value: "signed-state"})
	request.AddCookie(&http.Cookie{Name: googleOAuthReturnURLCookie, Value: "//untrusted.example"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, "/", recorder.Header().Get("Location"))
}

func TestSafeAuthenticationReturnURL(t *testing.T) {
	for _, test := range []struct {
		candidate string
		expected  string
	}{
		{candidate: "/connected-sources?source=gmail", expected: "/connected-sources?source=gmail"},
		{candidate: "", expected: "/"},
		{candidate: "//untrusted.example", expected: "/"},
		{candidate: "/\\untrusted.example", expected: "/"},
		{candidate: "/\r\nLocation: https://untrusted.example", expected: "/"},
		{candidate: "/login?returnUrl=/connected-sources", expected: "/"},
	} {
		require.Equal(t, test.expected, safeAuthenticationReturnURL(test.candidate))
	}
}
