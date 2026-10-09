package authentication

import (
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/services"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func redisRotationTestService(t *testing.T) (*service, *redis.Client) {
	t.Helper()
	address := os.Getenv("HAI_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set HAI_TEST_REDIS_ADDR and HAI_TEST_REDIS_INSTANCE for disposable Redis tests")
	}
	host, _, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.True(t, net.ParseIP(host).IsLoopback(), "disposable Redis requires a literal loopback address")
	instance := os.Getenv("HAI_TEST_REDIS_INSTANCE")
	require.NotEmpty(t, instance)
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	marker, err := client.Get(ctx, "hai-disposable-test-instance").Result()
	require.NoError(t, err)
	require.Equal(t, instance, marker, "refusing writes to an unmarked Redis instance")
	t.Setenv("REDIS_ADDR", address)
	setupAuthConfig(t)
	blocklist := services.NewRedisTokenBlockListService()
	t.Cleanup(func() { _ = blocklist.(interface{ Close() error }).Close() })
	user := &models.User{ID: uuid.New(), Role: "owner", IsActive: true}
	svc := &service{userService: &fakeUserService{userByID: user}, blockListService: blocklist, logger: noopLogger{}, jwtSecret: testSigningSecret}
	enableTestSessionAuthority(t, svc)
	return svc, client
}

func TestRedisSessionRefreshConcurrentCookiesAndLogout(t *testing.T) {
	svc, _ := redisRotationTestService(t)
	gin.SetMode(gin.TestMode)
	t.Setenv("IDP_COOKIE_SECURE", "true")
	user := svc.userService.(*fakeUserService).userByID
	original, err := svc.issueSession(user)
	require.NoError(t, err)
	_, expiredClaims, err := svc.parseAndValidateToken(original.AccessToken)
	require.NoError(t, err)
	expiredClaims["exp"] = time.Now().Add(-time.Second).Unix()
	expiredAccess, err := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims).SignedString([]byte(svc.jwtSecret))
	require.NoError(t, err)
	handler := NewHandler(svc)
	router := gin.New()
	router.GET("/session", handler.CurrentSession)
	router.GET("/auth-check", handler.IsUserAuthenticated)
	router.POST("/logout", AuthMiddleware(handler), handler.Logout)
	const callers = 24
	responses := make([]*httptest.ResponseRecorder, callers)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for i := range responses {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			request := httptest.NewRequest(http.MethodGet, "/session", nil)
			request.AddCookie(&http.Cookie{Name: "access_token", Value: expiredAccess})
			request.AddCookie(&http.Cookie{Name: "refresh_token", Value: original.RefreshToken})
			request.AddCookie(&http.Cookie{Name: rememberMeCookie, Value: rememberMeSessionValue})
			responses[index] = httptest.NewRecorder()
			router.ServeHTTP(responses[index], request)
		}(i)
	}
	close(start)
	wait.Wait()
	var winner map[string]*http.Cookie
	for _, response := range responses {
		require.Equal(t, http.StatusOK, response.Code)
		require.Contains(t, response.Body.String(), `"authenticated":true`)
		cookies := response.Result().Cookies()
		require.Len(t, cookies, 2)
		byName := make(map[string]*http.Cookie)
		for _, cookie := range cookies {
			byName[cookie.Name] = cookie
			require.True(t, cookie.HttpOnly && cookie.Secure)
			require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
			require.True(t, cookie.Expires.IsZero(), "refresh must preserve session-only preference")
		}
		require.NotEqual(t, original.RefreshToken, byName["refresh_token"].Value)
		if winner == nil {
			winner = byName
		} else {
			require.Equal(t, winner["access_token"].Value, byName["access_token"].Value)
			require.Equal(t, winner["refresh_token"].Value, byName["refresh_token"].Value)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	request.Header.Set(authSubrequestHeader, authSubrequestHeaderExpected)
	request.AddCookie(&http.Cookie{Name: "refresh_token", Value: original.RefreshToken})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, winner["access_token"].Value, response.Header().Get(verifiedAccessTokenHeader))
	require.Contains(t, response.Header().Get(refreshedRefreshCookieHeader), winner["refresh_token"].Value)
	require.Empty(t, response.Header().Values("Set-Cookie"))

	request = httptest.NewRequest(http.MethodPost, "/logout", nil)
	request.Header.Set("Origin", "https://example.com")
	request.AddCookie(winner["access_token"])
	request.AddCookie(winner["refresh_token"])
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, response.Result().Cookies(), 3)
	for _, cookie := range response.Result().Cookies() {
		require.Equal(t, -1, cookie.MaxAge)
	}
	ok, err := svc.IsUserAuthenticated(winner["access_token"].Value)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.False(t, ok)
	for _, refresh := range []string{original.RefreshToken, winner["refresh_token"].Value} {
		pair, err := svc.RefreshToken(refresh)
		require.ErrorIs(t, err, ErrRevokedSession)
		require.Nil(t, pair)
	}
}

func TestRedisSessionReplayRevokesChildAndAccess(t *testing.T) {
	svc, client := redisRotationTestService(t)
	user := svc.userService.(*fakeUserService).userByID
	parent, parentID, _, err := svc.generateRefreshToken(user.ID)
	require.NoError(t, err)
	child, err := svc.RefreshToken(parent)
	require.NoError(t, err)
	require.NoError(t, client.PExpire(context.Background(), "refresh-rotation:"+parentID, 40*time.Millisecond).Err())
	require.Eventually(t, func() bool {
		return client.Exists(context.Background(), "refresh-rotation:"+parentID).Val() == 0
	}, time.Second, 10*time.Millisecond)
	pair, err := svc.RefreshToken(parent)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Nil(t, pair)
	ok, err := svc.IsUserAuthenticated(child.AccessToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.False(t, ok)
	pair, err = svc.RefreshToken(child.RefreshToken)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Nil(t, pair)
}

func TestRedisSessionLogoutRacesRotation(t *testing.T) {
	svc, _ := redisRotationTestService(t)
	user := svc.userService.(*fakeUserService).userByID
	for attempt := 0; attempt < 20; attempt++ {
		original, err := svc.issueSession(user)
		require.NoError(t, err)
		var rotated *dto.TokenDetails
		var refreshErr, logoutErr error
		var wait sync.WaitGroup
		start := make(chan struct{})
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			rotated, refreshErr = svc.RefreshToken(original.RefreshToken)
		}()
		go func() {
			defer wait.Done()
			<-start
			logoutErr = svc.Logout(original.AccessToken)
		}()
		close(start)
		wait.Wait()
		require.NoError(t, logoutErr)
		if refreshErr != nil {
			require.ErrorIs(t, refreshErr, ErrRevokedSession)
			require.Nil(t, rotated)
		}
		pairs := []*dto.TokenDetails{original}
		if rotated != nil {
			pairs = append(pairs, rotated)
		}
		for _, pair := range pairs {
			ok, err := svc.IsUserAuthenticated(pair.AccessToken)
			require.ErrorIs(t, err, ErrRevokedSession)
			require.False(t, ok)
			result, err := svc.RefreshToken(pair.RefreshToken)
			require.ErrorIs(t, err, ErrRevokedSession)
			require.Nil(t, result, "no result of a refresh/logout race may revive the session")
		}
	}
}
