package authentication

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/users"
	"automation-hub-idp/internal/app/utils"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Only persistence and account-created delivery are synthetic. Authentication,
// bcrypt, cookies, user handlers, middleware and Redis authority are production code.
type redisHTTPUserRepository struct {
	irepository.UserRepository
	mu    sync.Mutex
	users map[uuid.UUID]models.User
}

func (r *redisHTTPUserRepository) FindByID(id uuid.UUID) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.users[id]
	if !ok {
		return nil, irepository.ErrUserNotFound
	}
	return &user, nil
}

func (r *redisHTTPUserRepository) FindByEmail(email string) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if user.Email == email && user.IsActive {
			return &user, nil
		}
	}
	return nil, irepository.ErrUserNotFound
}

func (r *redisHTTPUserRepository) Create(user *models.User) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.users {
		if existing.Email == user.Email && existing.IsActive {
			return nil, irepository.ErrDuplicateUser
		}
	}
	created := *user
	created.ID = uuid.New()
	created.IsActive = true // Matches the repository's database default.
	r.users[created.ID] = created
	return &created, nil
}

func (r *redisHTTPUserRepository) CompleteSuccessfulLogin(expected models.User, now time.Time) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.users[expected.ID]
	if !ok {
		return nil, irepository.ErrUserNotFound
	}
	expiryMatches := current.ResetTokenExpires == nil && expected.ResetTokenExpires == nil ||
		current.ResetTokenExpires != nil && expected.ResetTokenExpires != nil && current.ResetTokenExpires.Equal(*expected.ResetTokenExpires)
	if !current.IsActive || current.Password != expected.Password || current.Email != expected.Email ||
		current.SessionVersion != expected.SessionVersion || current.ResetPasswordToken != expected.ResetPasswordToken || !expiryMatches ||
		(current.IsBlocked && (current.BlockedUntil == nil || now.Before(*current.BlockedUntil))) {
		return nil, irepository.ErrConcurrentUserUpdate
	}
	current.FailedAttempts = 0
	current.LastAttempt = nil
	current.IsBlocked = false
	current.BlockedUntil = nil
	current.UpdatedAt = now.UTC()
	r.users[current.ID] = current
	return &current, nil
}

func (r *redisHTTPUserRepository) Update(user *models.User) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[user.ID]; !ok {
		return nil, irepository.ErrUserNotFound
	}
	r.users[user.ID] = *user
	updated := *user
	return &updated, nil
}

type redisHTTPAccountCreatedSender struct{ topics []string }

func (s *redisHTTPAccountCreatedSender) Send(topic string, _ interface{}) error {
	s.topics = append(s.topics, topic)
	return nil
}

type redisHTTPSessionFixture struct {
	router *gin.Engine
	svc    *service
	client *redis.Client
}

func newRedisHTTPSessionFixture(t *testing.T) *redisHTTPSessionFixture {
	t.Helper()
	// These tests stay serial: environment/config setup ends before request goroutines
	// start, and all request goroutines finish before fixture cleanup restores globals.
	authConfig, serverConfig, kafkaConfig := config.AuthenticationConfig, config.ServerConfig, config.KafkaConfig
	postgresConfig, redisConfig := config.PostgresConfig, config.RedisConfig
	mailConfig, previewConfig := config.MailConfig, config.LocalPreviewConfig
	t.Cleanup(func() {
		config.AuthenticationConfig, config.ServerConfig, config.KafkaConfig = authConfig, serverConfig, kafkaConfig
		config.PostgresConfig, config.RedisConfig = postgresConfig, redisConfig
		config.MailConfig, config.LocalPreviewConfig = mailConfig, previewConfig
	})
	t.Setenv("ACCESS_TOKEN_DURATION_MINUTES", "1")
	t.Setenv("REFRESH_TOKEN_DURATION_DAYS", "1")
	t.Setenv("MIN_TIME_BETWEEN_ATTEMPTS_IN_SECONDS", "0")
	t.Setenv("BASE_URL", "/api")
	t.Setenv("IDP_COOKIE_SECURE", "true")
	// Reuse the TestRedis loopback/instance-marker gate and real Redis implementation.
	base, client := redisRotationTestService(t)
	repo := &redisHTTPUserRepository{users: make(map[uuid.UUID]models.User)}
	hasher := utils.DefaultBcryptHasher()
	userService := users.NewUserService(repo, noopLogger{}, hasher)
	svc := NewService(userService, nil, hasher, &redisHTTPAccountCreatedSender{}, base.blockListService, noopLogger{}, testSigningSecret, newTestSessionRepository(userService)).(*service)
	handler := NewHandler(svc)
	router := gin.New()
	api := router.Group(config.ServerConfig.BaseURL + "/v1")
	api.POST("/auth/register", handler.Register)
	api.POST("/auth/login", handler.Login)
	api.POST("/auth/logout", AuthMiddleware(handler), handler.Logout)
	api.GET("/auth/session", handler.CurrentSession)
	api.GET("/auth/is-user-authenticated", handler.IsUserAuthenticated)
	api.GET("/user/", AuthMiddleware(handler), users.NewHandler(userService).GetCurrentUser)
	return &redisHTTPSessionFixture{router: router, svc: svc, client: client}
}

func (f *redisHTTPSessionFixture) request(method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "https://idp.example.test/api/v1"+path, strings.NewReader(body))
	if method == http.MethodPost && strings.HasSuffix(path, "/auth/logout") {
		request.Header.Set("Origin", "https://idp.example.test")
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func redisHTTPCookies(t *testing.T, response *httptest.ResponseRecorder, count int, persistent bool) map[string]*http.Cookie {
	t.Helper()
	cookies := response.Result().Cookies()
	require.Len(t, cookies, count)
	byName := make(map[string]*http.Cookie)
	for _, cookie := range cookies {
		require.NotContains(t, byName, cookie.Name, "no duplicate Set-Cookie names")
		byName[cookie.Name] = cookie
		require.True(t, cookie.HttpOnly && cookie.Secure)
		require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
		require.Equal(t, "/", cookie.Path)
		require.Zero(t, cookie.MaxAge)
		require.Empty(t, cookie.Domain)
		if persistent {
			require.False(t, cookie.Expires.IsZero())
		} else {
			require.True(t, cookie.Expires.IsZero())
		}
	}
	require.Contains(t, byName, "access_token")
	require.Contains(t, byName, "refresh_token")
	return byName
}

func (f *redisHTTPSessionFixture) registerAndLogin(t *testing.T, persistent bool) (dto.UserResponse, []*http.Cookie) {
	t.Helper()
	credentials := map[string]interface{}{
		"email": "synthetic-" + uuid.NewString() + "@example.test", "password": "synthetic-local-passphrase-2026", "rememberMe": persistent,
	}
	body, err := json.Marshal(credentials)
	require.NoError(t, err)
	registered := f.request(http.MethodPost, "/auth/register", string(body), nil)
	require.Equal(t, http.StatusOK, registered.Code)
	require.Empty(t, registered.Header().Values("Set-Cookie"), "registration does not grant a session")
	var user dto.UserResponse
	require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &user))
	require.NotEqual(t, uuid.Nil, user.ID)
	require.Equal(t, credentials["email"], user.Email)
	sender := f.svc.sender.(*redisHTTPAccountCreatedSender)
	require.Equal(t, config.AuthenticationConfig.AccountCreatedTopic, sender.topics[len(sender.topics)-1])
	stored, err := f.svc.userService.GetUserByID(user.ID)
	require.NoError(t, err)
	require.Equal(t, "operator", stored.Role)
	require.NotEqual(t, credentials["password"], stored.Password)
	require.NoError(t, f.svc.hasher.Compare(stored.Password, credentials["password"].(string)))
	require.NotContains(t, registered.Body.String(), stored.Password)
	duplicate := f.request(http.MethodPost, "/auth/register", string(body), nil)
	require.Equal(t, http.StatusConflict, duplicate.Code)
	badCredentials := map[string]interface{}{"email": user.Email, "password": "incorrect-synthetic-password"}
	badBody, err := json.Marshal(badCredentials)
	require.NoError(t, err)
	rejected := f.request(http.MethodPost, "/auth/login", string(badBody), nil)
	require.Equal(t, http.StatusUnauthorized, rejected.Code)
	require.Empty(t, rejected.Header().Values("Set-Cookie"))
	loggedIn := f.request(http.MethodPost, "/auth/login", string(body), nil)
	require.Equal(t, http.StatusOK, loggedIn.Code)
	cookies := redisHTTPCookies(t, loggedIn, 3, persistent)
	require.Equal(t, rememberMeValue(persistent), cookies[rememberMeCookie].Value)
	return user, loggedIn.Result().Cookies()
}

func assertRedisHTTPUser(t *testing.T, response *httptest.ResponseRecorder, user dto.UserResponse) {
	t.Helper()
	require.Equal(t, http.StatusOK, response.Code)
	var actual dto.UserResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &actual))
	require.Equal(t, user, actual, "protected user handler must resolve the cookie's identity")
}

func (f *redisHTTPSessionFixture) assertDenied(t *testing.T, cookies []*http.Cookie) {
	t.Helper()
	for _, path := range []string{"/user/", "/auth/is-user-authenticated", "/auth/session"} {
		response := f.request(http.MethodGet, path, "", cookies)
		if path == "/auth/session" {
			require.Equal(t, http.StatusOK, response.Code)
			var session dto.AuthSession
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &session))
			require.False(t, session.Authenticated)
			require.Empty(t, session.Subject)
			require.Empty(t, session.Role)
			require.Equal(t, dto.AuthSessionPermissions{}, session.Permissions)
		} else {
			require.Equal(t, http.StatusUnauthorized, response.Code, path)
		}
		require.Empty(t, response.Header().Values("Set-Cookie"), "denial must not mint cookies")
		require.Empty(t, response.Header().Get(verifiedAccessTokenHeader))
		require.Empty(t, response.Header().Get(refreshedAccessCookieHeader))
		require.Empty(t, response.Header().Get(refreshedRefreshCookieHeader))
	}
}

func TestRedisHTTPSessionCookieChain(t *testing.T) {
	f := newRedisHTTPSessionFixture(t)
	type browserSession struct {
		user       dto.UserResponse
		cookies    []*http.Cookie
		persistent bool
		access     string
		claims     jwt.MapClaims
	}
	browsers := make([]browserSession, 0, 2)
	var latestExpiry int64
	for _, persistent := range []bool{false, true} {
		user, cookies := f.registerAndLogin(t, persistent)
		assertRedisHTTPUser(t, f.request(http.MethodGet, "/user/", "", cookies), user)
		var access string
		for _, cookie := range cookies {
			if cookie.Name == "access_token" {
				access = cookie.Value
			}
		}
		_, claims, err := f.svc.parseAndValidateToken(access)
		require.NoError(t, err)
		expires, ok := unixClaim(claims, "exp")
		require.True(t, ok)
		require.InDelta(t, 60, time.Until(time.Unix(expires, 0)).Seconds(), 2)
		browsers = append(browsers, browserSession{user, cookies, persistent, access, claims})
		if expires > latestExpiry {
			latestExpiry = expires
		}
	}
	// Let the exact HTTP-issued JWTs expire; never replace claims or the global clock.
	t.Log("waiting for naturally expiring HTTP-issued access cookies")
	require.Eventually(t, func() bool { return time.Now().Unix() >= latestExpiry }, 90*time.Second, 25*time.Millisecond)
	for _, browser := range browsers {
		name := "session-only"
		if browser.persistent {
			name = "persistent"
		}
		t.Run(name, func(t *testing.T) {
			// The parser allows skew, but access authorization enforces exact expiry.
			require.Eventually(t, func() bool {
				return jwt.NewValidator(jwt.WithExpirationRequired()).Validate(browser.claims) != nil
			}, 10*time.Second, 25*time.Millisecond, "the original HTTP-issued JWT must reach observable expiry")
			require.ErrorIs(t, jwt.NewValidator(jwt.WithExpirationRequired()).Validate(browser.claims), jwt.ErrTokenExpired)
			ok, err := f.svc.IsUserAuthenticated(browser.access)
			require.ErrorIs(t, err, ErrRevokedSession)
			require.False(t, ok)
			f.assertDenied(t, []*http.Cookie{{Name: "access_token", Value: browser.access}})
			const callers = 24
			responses := make([]*httptest.ResponseRecorder, callers)
			var wait sync.WaitGroup
			start := make(chan struct{})
			for i := range responses {
				wait.Add(1)
				go func(index int) {
					defer wait.Done()
					<-start
					responses[index] = f.request(http.MethodGet, "/user/", "", browser.cookies)
				}(i)
			}
			close(start)
			wait.Wait()
			var winner map[string]*http.Cookie
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			origin, err := url.Parse("https://idp.example.test/api/v1/user/")
			require.NoError(t, err)
			jar.SetCookies(origin, browser.cookies)
			for i := len(responses) - 1; i >= 0; i-- {
				response := responses[i]
				assertRedisHTTPUser(t, response, browser.user)
				cookies := redisHTTPCookies(t, response, 2, browser.persistent)
				if winner == nil {
					winner = cookies
				} else {
					require.Equal(t, winner["access_token"].Value, cookies["access_token"].Value)
					require.Equal(t, winner["refresh_token"].Value, cookies["refresh_token"].Value)
				}
				jar.SetCookies(origin, response.Result().Cookies())
			}
			require.NotEqual(t, browser.access, winner["access_token"].Value)
			for _, cookie := range browser.cookies {
				if cookie.Name == "refresh_token" {
					require.NotEqual(t, cookie.Value, winner["refresh_token"].Value)
				}
			}
			current := jar.Cookies(origin)
			protected := f.request(http.MethodGet, "/user/", "", current)
			assertRedisHTTPUser(t, protected, browser.user)
			require.Empty(t, protected.Header().Values("Set-Cookie"), "the browser's replacement pair is already valid")
			// Refresh-only resolution also succeeds during the predecessor's replay grace.
			var predecessor *http.Cookie
			for _, cookie := range browser.cookies {
				if cookie.Name == "refresh_token" {
					predecessor = cookie
				}
			}
			assertRedisHTTPUser(t, f.request(http.MethodGet, "/user/", "", []*http.Cookie{predecessor}), browser.user)
			logout := f.request(http.MethodPost, "/auth/logout", "", current)
			require.Equal(t, http.StatusOK, logout.Code)
			cleared := logout.Result().Cookies()
			require.Len(t, cleared, 3)
			for _, cookie := range cleared {
				require.Contains(t, []string{"access_token", "refresh_token", rememberMeCookie}, cookie.Name)
				require.Equal(t, -1, cookie.MaxAge)
				require.Empty(t, cookie.Value)
				require.True(t, cookie.HttpOnly && cookie.Secure)
				require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
				require.Equal(t, "/", cookie.Path)
			}
			jar.SetCookies(origin, cleared)
			require.Empty(t, jar.Cookies(origin))
			f.assertDenied(t, nil)
			for _, pair := range [][]*http.Cookie{browser.cookies, current} {
				f.assertDenied(t, pair)
				for _, cookie := range pair {
					if cookie.Name == "access_token" || cookie.Name == "refresh_token" {
						f.assertDenied(t, []*http.Cookie{cookie})
					}
				}
			}
			// Lose this test's revocations and authority, not the DB or instance marker.
			_, childClaims, err := f.svc.parseAndValidateToken(winner["access_token"].Value)
			require.NoError(t, err, "child JWT is still cryptographically valid after logout")
			_, parentClaims, err := f.svc.parseAndValidateToken(predecessor.Value)
			require.NoError(t, err)
			family, ok := refreshFamilyFromClaims(childClaims)
			require.True(t, ok)
			keys := []string{"refresh-family:" + family, "refresh-family-active:" + family}
			for _, claims := range []jwt.MapClaims{parentClaims, childClaims} {
				refresh, ok := claims["refresh_uuid"].(string)
				require.True(t, ok)
				keys = append(keys, refresh, "refresh-active:"+refresh, "refresh-rotation:"+refresh)
			}
			accessID, ok := childClaims["access_uuid"].(string)
			require.True(t, ok)
			keys = append(keys, accessID)
			require.NoError(t, f.client.Del(context.Background(), keys...).Err())
			f.assertDenied(t, browser.cookies)
			f.assertDenied(t, current)
			t.Logf("24 concurrent protected refreshes agreed; logout and subsequent key loss denied both saved pairs (%s)", name)
		})
	}
}

func TestRedisHTTPSessionAuthorityLoss(t *testing.T) {
	for _, missing := range []string{"family", "current-refresh", "both"} {
		t.Run(missing, func(t *testing.T) {
			f := newRedisHTTPSessionFixture(t)
			user, original := f.registerAndLogin(t, false)
			var refreshOnly []*http.Cookie
			for _, cookie := range original {
				if cookie.Name != "access_token" {
					refreshOnly = append(refreshOnly, cookie)
				}
			}
			rotated := f.request(http.MethodGet, "/user/", "", refreshOnly)
			assertRedisHTTPUser(t, rotated, user)
			current := rotated.Result().Cookies()
			cookies := redisHTTPCookies(t, rotated, 2, false)
			_, claims, err := f.svc.parseAndValidateToken(cookies["access_token"].Value)
			require.NoError(t, err)
			family, ok := refreshFamilyFromClaims(claims)
			require.True(t, ok)
			refresh, ok := claims["refresh_uuid"].(string)
			require.True(t, ok)
			_, parentClaims, err := f.svc.parseAndValidateToken(refreshOnly[0].Value)
			require.NoError(t, err)
			parentRefresh, ok := parentClaims["refresh_uuid"].(string)
			require.True(t, ok)
			require.EqualValues(t, 1, f.client.Exists(context.Background(), "refresh-rotation:"+parentRefresh).Val(), "predecessor replay cache must exist before authority loss")
			keys := []string{"refresh-family-active:" + family}
			if missing == "current-refresh" {
				keys = []string{"refresh-active:" + refresh}
			} else if missing == "both" {
				keys = append(keys, "refresh-active:"+refresh)
			}
			deleted, err := f.client.Del(context.Background(), keys...).Result()
			require.NoError(t, err)
			require.EqualValues(t, len(keys), deleted)
			// Current access is unexpired; cached predecessor refresh must not return its winner.
			f.assertDenied(t, current)
			f.assertDenied(t, refreshOnly)
			f.assertDenied(t, original)
			for _, cookie := range current {
				f.assertDenied(t, []*http.Cookie{cookie})
			}
			freshBody, err := json.Marshal(map[string]interface{}{
				"email": user.Email, "password": "synthetic-local-passphrase-2026", "rememberMe": false,
			})
			require.NoError(t, err)
			fresh := f.request(http.MethodPost, "/auth/login", string(freshBody), nil)
			require.Equal(t, http.StatusOK, fresh.Code)
			redisHTTPCookies(t, fresh, 3, false)
			assertRedisHTTPUser(t, f.request(http.MethodGet, "/user/", "", fresh.Result().Cookies()), user)
			f.assertDenied(t, original)
			f.assertDenied(t, current)
			assertRedisHTTPUser(t, f.request(http.MethodGet, "/user/", "", fresh.Result().Cookies()), user)
		})
	}
}

func TestRedisHTTPSessionStoreUnavailable(t *testing.T) {
	f := newRedisHTTPSessionFixture(t)
	user, cookies := f.registerAndLogin(t, false)
	require.NoError(t, f.svc.blockListService.(interface{ Close() error }).Close())
	f.assertDenied(t, cookies)
	for _, cookie := range cookies {
		if cookie.Name == "access_token" || cookie.Name == "refresh_token" {
			f.assertDenied(t, []*http.Cookie{cookie})
		}
	}
	body, err := json.Marshal(map[string]string{"email": user.Email, "password": "synthetic-local-passphrase-2026"})
	require.NoError(t, err)
	response := f.request(http.MethodPost, "/auth/login", string(body), nil)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Empty(t, response.Header().Values("Set-Cookie"), "store unavailability must prevent new session issuance")
}
