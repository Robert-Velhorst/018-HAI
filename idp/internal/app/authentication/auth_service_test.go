package authentication

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/services"
	"automation-hub-idp/internal/app/services/iservice"
	"automation-hub-idp/internal/app/users"
	"automation-hub-idp/internal/app/utils"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const testSigningSecret = "0123456789abcdef0123456789abcdef"

func TestRegisterRejectsInvalidInputBeforeHashing(t *testing.T) {
	svc := &service{}

	_, err := svc.Register(dto.UserDTO{Email: "not-an-email", Password: "local-passphrase-2026"})
	require.ErrorIs(t, err, ErrRegistrationEmailInvalid)

	_, err = svc.Register(dto.UserDTO{Email: "operator@example.com", Password: "short-pass"})
	require.ErrorIs(t, err, ErrRegistrationPasswordWeak)

	_, err = svc.Register(dto.UserDTO{Email: "operator@example.com", Password: strings.Repeat("a", utils.MaximumPasswordBytes+1)})
	require.ErrorIs(t, err, ErrRegistrationPasswordTooLong)
}

func TestPasswordMaximumByteLengthIsEnforcedAcrossAuthFlows(t *testing.T) {
	password := strings.Repeat("a", utils.MaximumPasswordBytes+1)
	require.ErrorIs(t, utils.ValidatePassword(password), utils.ErrPasswordTooLong)

	t.Run("password reset", func(t *testing.T) {
		userService := &fakeUserService{}
		svc := &service{userService: userService}
		err := svc.ConfirmPasswordReset("reset-token", password)
		require.ErrorIs(t, err, ErrRegistrationPasswordTooLong)
		require.Empty(t, userService.lookupResetToken)
	})

	t.Run("authenticated password change", func(t *testing.T) {
		svc := &service{}
		err := svc.ChangePassword("", "current-password", password)
		require.ErrorIs(t, err, ErrRegistrationPasswordTooLong)
	})
}

func TestPasswordMinimumCountsUnicodeCharactersAcrossEntryPoints(t *testing.T) {
	weakPassword := strings.Repeat("é", minimumPasswordLength/2)
	require.Len(t, []rune(weakPassword), minimumPasswordLength/2)
	require.Len(t, []byte(weakPassword), minimumPasswordLength)
	require.False(t, passwordMeetsMinimumLength(weakPassword))
	require.True(t, passwordMeetsMinimumLength(strings.Repeat("é", minimumPasswordLength)))

	t.Run("registration", func(t *testing.T) {
		svc := &service{userService: &fakeUserService{}}
		_, err := svc.Register(dto.UserDTO{Email: "operator@example.com", Password: weakPassword})
		require.ErrorIs(t, err, ErrRegistrationPasswordWeak)
	})

	t.Run("password reset", func(t *testing.T) {
		userService := &fakeUserService{}
		svc := &service{userService: userService}
		err := svc.ConfirmPasswordReset("reset-token", weakPassword)
		require.ErrorIs(t, err, ErrRegistrationPasswordWeak)
		require.Empty(t, userService.lookupResetToken)
	})

	t.Run("password change", func(t *testing.T) {
		svc := &service{}
		err := svc.ChangePassword("", "current-password", weakPassword)
		require.ErrorIs(t, err, ErrRegistrationPasswordWeak)
	})
}

func TestLoginWithGoogleRecoversConcurrentFirstSignIn(t *testing.T) {
	setupAuthConfig(t)
	concurrentUser := &models.User{
		ID: uuid.New(), Email: "person@example.com", Role: "operator", IsActive: true, SessionVersion: 2,
	}
	userService := &fakeUserService{
		createUserErr:        users.ErrUserAlreadyExists,
		concurrentGoogleUser: concurrentUser,
	}
	svc := &service{
		userService:      userService,
		hasher:           utils.DefaultBcryptHasher(),
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
		googleOAuthFactory: func(string) googleOAuthProvider {
			return mockGoogleOAuthProvider{email: concurrentUser.Email}
		},
	}

	enableTestSessionAuthority(t, svc)
	tokens, err := svc.LoginWithGoogle(context.Background(), "mock-code", "mock-state")
	require.NoError(t, err)
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)
	require.Equal(t, 2, userService.getUserByEmailCalls)
	require.Equal(t, 1, userService.createUserCalls)
	_, claims, err := svc.parseAndValidateToken(tokens.AccessToken)
	require.NoError(t, err)
	require.Equal(t, concurrentUser.ID.String(), claims["user_id"])
	require.EqualValues(t, concurrentUser.SessionVersion, claims["session_version"])
}

func TestLoginWithGoogleLogsSafeProviderFailureWithoutIdentityOrProviderText(t *testing.T) {
	logger := &captureLogger{}
	providerDetail := "provider rejected private-person@example.com client_secret=synthetic-secret"
	svc := &service{
		logger: logger,
		googleOAuthFactory: func(string) googleOAuthProvider {
			return mockGoogleOAuthProvider{exchangeErr: errors.New(providerDetail)}
		},
	}

	_, err := svc.LoginWithGoogle(context.Background(), "authorization-code", "signed-state")
	require.Error(t, err)
	logOutput := strings.Join(logger.messages, "\n")
	require.Contains(t, logOutput, "Google sign-in failed during authorization-code exchange or identity verification")
	require.NotContains(t, logOutput, "private-person@example.com")
	require.NotContains(t, logOutput, "synthetic-secret")
	require.NotContains(t, logOutput, providerDetail)
}

func TestSuccessfulLoginDoesNotThrottleNextValidSession(t *testing.T) {
	setupAuthConfig(t)
	hasher := utils.DefaultBcryptHasher()
	password := "valid-local-password"
	hashedPassword, err := hasher.Hash(password)
	require.NoError(t, err)
	userService := &fakeUserService{userByEmail: &models.User{
		ID:       uuid.New(),
		Email:    "operator@example.com",
		Password: hashedPassword,
		Role:     "owner",
		IsActive: true,
	}}
	svc := &service{
		userService:      userService,
		hasher:           hasher,
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}

	enableTestSessionAuthority(t, svc)
	_, err = svc.Login("operator@example.com", password)
	require.NoError(t, err)
	require.Nil(t, userService.updatedUser.LastAttempt)

	_, err = svc.Login("operator@example.com", password)
	require.NoError(t, err)
}

func TestRefreshTokenUsesRefreshTokenExpiration(t *testing.T) {
	setupAuthConfig(t)

	userID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}

	enableTestSessionAuthority(t, svc)
	refreshToken, refreshUUID, refreshExp, err := svc.generateRefreshToken(userID)
	require.NoError(t, err)

	tokenDetails, err := svc.RefreshToken(refreshToken)
	require.NoError(t, err)
	require.NotEmpty(t, tokenDetails.AccessToken)
	require.NotEqual(t, refreshToken, tokenDetails.RefreshToken)
	require.NotEqual(t, refreshUUID, tokenDetails.RefreshUUID)
	require.Equal(t, refreshExp, tokenDetails.RtExpires)
	_, claims, err := svc.parseAndValidateToken(tokenDetails.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "owner", claims["role"])
	require.Equal(t, tokenDetails.RefreshUUID, claims["refresh_uuid"])
	require.Equal(t, refreshUUID, claims["family_uuid"])
	_, rotatedClaims, err := svc.parseAndValidateToken(tokenDetails.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, refreshUUID, rotatedClaims["family_uuid"])
}

func TestConcurrentRefreshReusesOneRotatedTokenPair(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	blocklist := &inMemoryBlockListService{}
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "operator", IsActive: true}},
		blockListService: blocklist,
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}
	enableTestSessionAuthority(t, svc)
	originalRefresh, _, _, err := svc.generateRefreshToken(userID)
	require.NoError(t, err)

	results := make([]*dto.TokenDetails, 2)
	errorsByCall := make([]error, 2)
	var wait sync.WaitGroup
	for i := range results {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index], errorsByCall[index] = svc.RefreshToken(originalRefresh)
		}(i)
	}
	wait.Wait()

	for _, err := range errorsByCall {
		require.NoError(t, err)
	}
	require.Equal(t, results[0].RefreshToken, results[1].RefreshToken)
	require.Equal(t, results[0].RefreshUUID, results[1].RefreshUUID)
	require.Equal(t, results[0].AccessToken, results[1].AccessToken)

	_, rotatedClaims, err := svc.parseAndValidateToken(results[0].RefreshToken)
	require.NoError(t, err)
	familyUUID, ok := refreshFamilyFromClaims(rotatedClaims)
	require.True(t, ok)
	require.NoError(t, blocklist.RevokeRefreshFamily(familyUUID, time.Hour))
	_, err = svc.RefreshToken(results[0].RefreshToken)
	require.ErrorIs(t, err, ErrRevokedSession, "logout/family revocation must prevent child refresh")
}

func TestGenerateAccessTokenDoesNotOutliveRefreshToken(t *testing.T) {
	setupAuthConfig(t)
	previousAccessDuration := config.AuthenticationConfig.AccessTokenDurationMinutes
	config.AuthenticationConfig.AccessTokenDurationMinutes = 60
	t.Cleanup(func() {
		config.AuthenticationConfig.AccessTokenDurationMinutes = previousAccessDuration
	})

	svc := &service{logger: noopLogger{}, jwtSecret: testSigningSecret}
	userID := uuid.New()
	refreshExp := time.Now().Add(2 * time.Second).Unix()

	token, accessExp, err := svc.generateAccessTokenWithVersion(userID, "owner", uuid.NewString(), refreshExp, 0)
	require.NoError(t, err)
	require.Equal(t, refreshExp, accessExp, "access-token expiry must be capped by the refresh session")
	_, claims, err := svc.parseAndValidateToken(token)
	require.NoError(t, err)
	require.EqualValues(t, refreshExp, claims["exp"])

	_, _, err = svc.generateAccessTokenWithVersion(userID, "owner", uuid.NewString(), time.Now().Add(-time.Minute).Unix(), 0)
	require.Error(t, err, "an already expired refresh session must not mint a new access token")
}

func TestAccessTokenRejectsExpiredOrMalformedRefreshExpiration(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}

	tests := []struct {
		name       string
		refreshExp interface{}
	}{
		{name: "expired", refreshExp: time.Now().Add(-time.Second).Unix()},
		{name: "fractional seconds", refreshExp: float64(time.Now().Add(time.Hour).Unix()) + 0.5},
		{name: "negative", refreshExp: int64(-1)},
		{name: "overflow", refreshExp: float64(1 << 63)},
		{name: "wrong type", refreshExp: "tomorrow"},
		{name: "missing", refreshExp: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			claims := jwt.MapClaims{
				"iss":             tokenIssuer,
				"aud":             tokenAudience,
				"iat":             now.Unix(),
				"user_id":         userID.String(),
				"role":            "owner",
				"token_type":      tokenTypeAccess,
				"access_uuid":     uuid.NewString(),
				"refresh_uuid":    uuid.NewString(),
				"exp":             now.Add(time.Minute).Unix(),
				"session_version": int64(0),
			}
			if test.refreshExp != nil {
				claims["refresh_exp"] = test.refreshExp
			}
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			tokenString, err := token.SignedString([]byte(testSigningSecret))
			require.NoError(t, err)

			authenticated, err := svc.IsUserAuthenticated(tokenString)
			require.False(t, authenticated)
			require.ErrorIs(t, err, ErrRevokedSession)
		})
	}
}

func TestRefreshTokenRejectsExpiredJWT(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": tokenIssuer, "aud": tokenAudience, "iat": now.Add(-2 * time.Minute).Unix(),
		"user_id": userID.String(), "token_type": tokenTypeRefresh,
		"refresh_uuid": uuid.NewString(), "session_version": int64(0),
		"exp": now.Add(-time.Minute).Unix(),
	})
	refreshToken, err := token.SignedString([]byte(testSigningSecret))
	require.NoError(t, err)

	_, err = svc.RefreshToken(refreshToken)
	require.EqualError(t, err, "invalid refresh token")
}

func TestParseAndValidateTokenRequiresExpiration(t *testing.T) {
	setupAuthConfig(t)
	svc := &service{logger: noopLogger{}, jwtSecret: testSigningSecret}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": tokenIssuer, "aud": tokenAudience, "iat": time.Now().Unix(),
		"token_type": tokenTypeAccess,
	})
	tokenString, err := token.SignedString([]byte(testSigningSecret))
	require.NoError(t, err)

	_, _, err = svc.parseAndValidateToken(tokenString)
	require.Error(t, err, "tokens without exp must not be accepted")
}

func TestGenerateAccessTokenDowngradesUnknownRole(t *testing.T) {
	setupAuthConfig(t)
	svc := &service{logger: noopLogger{}, jwtSecret: "test-secret"}
	token, _, err := svc.generateAccessToken(uuid.New(), "unexpected", uuid.New().String(), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
	_, claims, err := svc.parseAndValidateToken(token)
	require.NoError(t, err)
	require.Equal(t, "viewer", claims["role"])
	require.Equal(t, "hai-idp", claims["iss"])
	require.Equal(t, "hai", claims["aud"])
	require.Equal(t, "access", claims["token_type"])
	require.NotNil(t, claims["iat"])
	require.NotNil(t, claims["exp"])
}

func TestGetSessionFromTokenMapsRolePermissions(t *testing.T) {
	setupAuthConfig(t)

	for _, test := range []struct {
		role          string
		canOperate    bool
		canApprove    bool
		canAdminister bool
	}{
		{role: "owner", canOperate: true, canApprove: true, canAdminister: true},
		{role: "operator", canOperate: true},
		{role: "viewer"},
	} {
		t.Run(test.role, func(t *testing.T) {
			userID := uuid.New()
			svc := &service{
				userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: test.role, IsActive: true}},
				blockListService: fakeBlockListService{},
				logger:           noopLogger{},
				jwtSecret:        "test-secret",
			}
			token, _, err := svc.generateAccessToken(userID, test.role, uuid.NewString(), time.Now().Add(time.Hour).Unix())
			require.NoError(t, err)
			seedTestAccessAuthority(t, svc, token)

			session, err := svc.GetSessionFromToken(token)
			require.NoError(t, err)
			require.True(t, session.Authenticated)
			require.Equal(t, userID.String(), session.Subject)
			require.Equal(t, test.role, session.Role)
			require.True(t, session.Permissions.CanRead)
			require.Equal(t, test.canOperate, session.Permissions.CanOperate)
			require.Equal(t, test.canApprove, session.Permissions.CanApprove)
			require.Equal(t, test.canAdminister, session.Permissions.CanAdminister)
		})
	}
}

func TestLogoutRejectsTokenWithoutUserID(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":          tokenIssuer,
		"aud":          tokenAudience,
		"iat":          time.Now().Unix(),
		"token_type":   tokenTypeAccess,
		"access_uuid":  uuid.New().String(),
		"refresh_uuid": uuid.New().String(),
		"refresh_exp":  time.Now().Add(time.Hour).Unix(),
		"exp":          time.Now().Add(time.Minute).Unix(),
	})
	tokenString, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	svc := &service{
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}

	err = svc.Logout(tokenString)
	require.EqualError(t, err, "user ID not found in the token")
}

func TestRefreshTokenRejectsAccessToken(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}

	accessToken, _, err := svc.generateAccessToken(
		userID,
		"owner",
		uuid.NewString(),
		time.Now().Add(time.Hour).Unix(),
	)
	require.NoError(t, err)

	_, err = svc.RefreshToken(accessToken)
	require.EqualError(t, err, "invalid refresh token")
}

func TestAccessTokenConsumersRejectRefreshToken(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}
	enableTestSessionAuthority(t, svc)
	refreshToken, _, _, err := svc.generateRefreshToken(userID)
	require.NoError(t, err)

	authenticated, err := svc.IsUserAuthenticated(refreshToken)
	require.False(t, authenticated)
	require.EqualError(t, err, "invalid accessToken")
	_, err = svc.GetIdFromToken(refreshToken)
	require.EqualError(t, err, "invalid accessToken")
	_, err = svc.GetSessionFromToken(refreshToken)
	require.EqualError(t, err, "invalid accessToken")
	require.EqualError(t, svc.ChangePassword(refreshToken, "current-password", "new-password-2026"), "invalid accessToken")
}

func TestAccessTokenConsumersRejectIndividuallyRevokedAccessToken(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	accessUUID := uuid.NewString()
	refreshUUID := uuid.NewString()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": tokenIssuer, "aud": tokenAudience, "iat": time.Now().Unix(),
		"token_type": tokenTypeAccess, "user_id": userID.String(),
		"access_uuid": accessUUID, "refresh_uuid": refreshUUID,
		"refresh_exp": time.Now().Add(time.Hour).Unix(), "exp": time.Now().Add(time.Minute).Unix(),
		"session_version": 0,
	})
	tokenString, err := token.SignedString([]byte(testSigningSecret))
	require.NoError(t, err)
	userService := &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}}
	svc := &service{
		userService:      userService,
		blockListService: &inMemoryBlockListService{blocked: map[string]bool{accessUUID: true}},
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}

	t.Run("authentication check", func(t *testing.T) {
		ok, err := svc.IsUserAuthenticated(tokenString)
		require.False(t, ok)
		require.ErrorIs(t, err, ErrRevokedSession)
	})
	t.Run("identity lookup", func(t *testing.T) {
		_, err := svc.GetIdFromToken(tokenString)
		require.ErrorIs(t, err, ErrRevokedSession)
	})
	t.Run("session lookup", func(t *testing.T) {
		_, err := svc.GetSessionFromToken(tokenString)
		require.ErrorIs(t, err, ErrRevokedSession)
	})
	t.Run("password change", func(t *testing.T) {
		err := svc.ChangePassword(tokenString, "current-password", "new-password-2026")
		require.ErrorIs(t, err, ErrRevokedSession)
		require.Empty(t, userService.updatedPassword, "revoked sessions must not reach password mutation")
	})
}

func TestSessionOperationsFailClosedWithoutRevocationStore(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}
	enableTestSessionAuthority(t, svc)
	refreshToken, refreshUUID, refreshExpires, err := svc.generateRefreshToken(userID)
	require.NoError(t, err)
	accessToken, _, err := svc.generateAccessToken(userID, "owner", refreshUUID, refreshExpires)
	require.NoError(t, err)
	svc.blockListService = nil

	t.Run("initial session issuance", func(t *testing.T) {
		token, id, expires, err := svc.generateRefreshToken(userID)
		require.Error(t, err)
		require.Empty(t, token)
		require.Empty(t, id)
		require.Zero(t, expires)
	})

	t.Run("access authentication", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Errorf("IsUserAuthenticated panicked without a revocation store: %v", recovered)
			}
		}()
		ok, err := svc.IsUserAuthenticated(accessToken)
		require.False(t, ok)
		require.Error(t, err)
	})
	t.Run("refresh", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Errorf("RefreshToken panicked without a revocation store: %v", recovered)
			}
		}()
		_, err := svc.RefreshToken(refreshToken)
		require.Error(t, err)
	})
	t.Run("logout", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Errorf("Logout panicked without a revocation store: %v", recovered)
			}
		}()
		require.Error(t, svc.Logout(accessToken))
	})
}

func TestParseAndValidateTokenRejectsUnexpectedHMACAlgorithm(t *testing.T) {
	setupAuthConfig(t)
	svc := &service{logger: noopLogger{}, jwtSecret: "test-secret"}
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.MapClaims{
		"iss":        tokenIssuer,
		"aud":        tokenAudience,
		"iat":        time.Now().Unix(),
		"token_type": tokenTypeAccess,
		"exp":        time.Now().Add(time.Minute).Unix(),
	})
	tokenString, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	_, _, err = svc.parseAndValidateToken(tokenString)
	require.Error(t, err)
}

func TestParseAndValidateTokenRejectsWrongIssuerOrAudience(t *testing.T) {
	setupAuthConfig(t)
	svc := &service{logger: noopLogger{}, jwtSecret: "test-secret"}
	for _, claims := range []jwt.MapClaims{
		{
			"iss": tokenIssuer, "aud": "other-service", "iat": time.Now().Unix(),
			"token_type": tokenTypeAccess, "exp": time.Now().Add(time.Minute).Unix(),
		},
		{
			"iss": "other-issuer", "aud": tokenAudience, "iat": time.Now().Unix(),
			"token_type": tokenTypeAccess, "exp": time.Now().Add(time.Minute).Unix(),
		},
		{
			"token_type": tokenTypeAccess, "exp": time.Now().Add(time.Minute).Unix(),
		},
	} {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("test-secret"))
		require.NoError(t, err)

		_, _, err = svc.parseAndValidateToken(tokenString)
		require.Error(t, err)
	}
}

func TestRefreshTokenRejectsInactiveOrBlockedUsers(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	blockedUntil := time.Now().Add(time.Hour)
	for _, user := range []*models.User{
		{ID: userID, Role: "owner", IsActive: false},
		{ID: userID, Role: "owner", IsActive: true, IsBlocked: true, BlockedUntil: &blockedUntil},
	} {
		svc := &service{
			userService:      &fakeUserService{userByID: user},
			blockListService: fakeBlockListService{},
			logger:           noopLogger{},
			jwtSecret:        "test-secret",
		}
		// Signing is not registration: these unavailable accounts must never
		// acquire positive durable authority just to exercise rejection.
		refreshToken, err := svc.generateRefreshTokenWithIdentity(userID, uuid.NewString(), uuid.NewString(), time.Now().Add(time.Hour).Unix(), 0)
		require.NoError(t, err)

		_, err = svc.RefreshToken(refreshToken)
		require.EqualError(t, err, "user is unavailable")
	}
}

func TestAccessAndSessionChecksRejectUnavailableUsers(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	blockedUntil := time.Now().Add(time.Hour)
	for _, user := range []*models.User{
		{ID: userID, Role: "owner", IsActive: false},
		{ID: userID, Role: "owner", IsActive: true, IsBlocked: true},
		{ID: userID, Role: "owner", IsActive: true, IsBlocked: true, BlockedUntil: &blockedUntil},
	} {
		svc := &service{
			userService:      &fakeUserService{userByID: user},
			blockListService: fakeBlockListService{},
			logger:           noopLogger{},
			jwtSecret:        "test-secret",
		}
		enableTestSessionAuthority(t, svc)
		token, _, err := svc.generateAccessToken(userID, "owner", uuid.NewString(), time.Now().Add(time.Hour).Unix())
		require.NoError(t, err)

		authenticated, err := svc.IsUserAuthenticated(token)
		require.False(t, authenticated)
		require.EqualError(t, err, "user is unavailable")
		_, err = svc.GetSessionFromToken(token)
		require.EqualError(t, err, "user is unavailable")
	}
}

func TestSessionRejectsStalePrivilegedTokenRole(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "viewer", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}
	token, _, err := svc.generateAccessToken(userID, "owner", uuid.NewString(), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
	seedTestAccessAuthority(t, svc, token)

	session, err := svc.GetSessionFromToken(token)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.Nil(t, session)
	authenticated, err := svc.IsUserAuthenticated(token)
	require.ErrorIs(t, err, ErrRevokedSession)
	require.False(t, authenticated)
}

func TestSessionDowngradesUnknownStoredRoleToViewer(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "legacy-admin", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}
	token, _, err := svc.generateAccessToken(userID, "viewer", uuid.NewString(), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
	seedTestAccessAuthority(t, svc, token)

	session, err := svc.GetSessionFromToken(token)
	require.NoError(t, err)
	require.Equal(t, "viewer", session.Role)
	require.True(t, session.Permissions.CanRead)
	require.False(t, session.Permissions.CanOperate)
	require.False(t, session.Permissions.CanApprove)
	require.False(t, session.Permissions.CanAdminister)
}

func TestLoginDoesNotClearIndefiniteBlock(t *testing.T) {
	setupAuthConfig(t)
	userService := &fakeUserService{
		userByEmail: &models.User{
			ID:        uuid.New(),
			Email:     "operator@example.com",
			Role:      "operator",
			IsActive:  true,
			IsBlocked: true,
		},
	}
	svc := &service{
		userService: userService,
		logger:      noopLogger{},
	}

	_, err := svc.Login(" OPERATOR@example.com ", "irrelevant")
	require.EqualError(t, err, "account is blocked")
	require.Nil(t, userService.updatedUser)
	require.Equal(t, "operator@example.com", userService.lookupEmail)
}

func TestLogoutAttemptsBothRevocations(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	blockList := &recordingBlockListService{
		errorsByCall: []error{errors.New("refresh storage failed"), nil},
	}
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}},
		blockListService: blockList,
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}
	refreshID := uuid.NewString()
	token, _, err := svc.generateAccessToken(userID, "owner", refreshID, time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
	seedTestAccessAuthority(t, svc, token)

	err = svc.Logout(token)
	require.ErrorContains(t, err, "revoke refresh token")
	require.Len(t, blockList.added, 2)
	require.Equal(t, []string{refreshID}, blockList.families)
	require.Equal(t, refreshID, blockList.added[0])
	require.NotEqual(t, blockList.added[0], blockList.added[1])
}

func TestLogoutRevokesEveryAccessTokenInRefreshSession(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	blockList := &inMemoryBlockListService{blocked: make(map[string]bool)}
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true, SessionVersion: 4}},
		blockListService: blockList,
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}
	enableTestSessionAuthority(t, svc)
	refreshToken, refreshUUID, refreshExpires, err := svc.generateRefreshTokenWithVersion(userID, 4)
	require.NoError(t, err)
	firstAccess, _, err := svc.generateAccessTokenWithVersion(userID, "owner", refreshUUID, refreshExpires, 4)
	require.NoError(t, err)
	secondAccess, _, err := svc.generateAccessTokenWithVersion(userID, "owner", refreshUUID, refreshExpires, 4)
	require.NoError(t, err)

	require.NoError(t, svc.Logout(firstAccess))

	authenticated, err := svc.IsUserAuthenticated(secondAccess)
	require.False(t, authenticated, "logging out must revoke sibling access tokens minted from the same refresh session")
	require.ErrorIs(t, err, ErrRevokedSession)
	_, err = svc.RefreshToken(refreshToken)
	require.Error(t, err, "logging out must also prevent the refresh token from minting further access tokens")
}

func TestLogoutDuringRefreshInvalidatesAlreadyRotatedChildSession(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	blocklist := &inMemoryBlockListService{}
	svc := &service{
		userService:      &fakeUserService{userByID: &models.User{ID: userID, Role: "owner", IsActive: true}},
		blockListService: blocklist,
		logger:           noopLogger{},
		jwtSecret:        testSigningSecret,
	}
	enableTestSessionAuthority(t, svc)
	refreshToken, refreshUUID, refreshExpires, err := svc.generateRefreshToken(userID)
	require.NoError(t, err)
	accessToken, _, err := svc.generateAccessToken(userID, "owner", refreshUUID, refreshExpires)
	require.NoError(t, err)

	rotated, err := svc.RefreshToken(refreshToken)
	require.NoError(t, err)
	authenticated, err := svc.IsUserAuthenticated(rotated.AccessToken)
	require.NoError(t, err)
	require.True(t, authenticated)

	// A logout request that began before the parallel refresh must still revoke
	// the whole family, including the already-minted child token pair.
	require.NoError(t, svc.Logout(accessToken))
	authenticated, err = svc.IsUserAuthenticated(rotated.AccessToken)
	require.False(t, authenticated)
	require.ErrorIs(t, err, ErrRevokedSession)
	_, err = svc.RefreshToken(rotated.RefreshToken)
	require.ErrorIs(t, err, ErrRevokedSession)
}

func TestChangePasswordPassesPlaintextPasswordToUserService(t *testing.T) {
	userID := uuid.New()
	userService := &fakeUserService{
		userByID: &models.User{
			ID:                 userID,
			Email:              "user@example.com",
			Role:               "owner",
			Password:           "old-hash",
			IsActive:           true,
			ResetPasswordToken: "stale-token",
			ResetTokenExpires:  ptrTime(time.Now().Add(time.Hour)),
		},
	}
	svc := &service{
		userService:      userService,
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}

	token, _, err := svc.generateAccessToken(userID, "owner", uuid.New().String(), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
	seedTestAccessAuthority(t, svc, token)
	err = svc.ChangePassword(token, "current-password", "new-password")
	require.NoError(t, err)
	require.Equal(t, "new-password", userService.updatedPassword)
	require.NotNil(t, userService.updatedUser)
	require.Empty(t, userService.updatedUser.ResetPasswordToken)
	require.Nil(t, userService.updatedUser.ResetTokenExpires)
}

func TestChangePasswordRejectsWeakPasswordBeforeUpdatingUser(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	userService := &fakeUserService{
		userByID: &models.User{
			ID: userID, Email: "user@example.com", IsActive: true,
		},
	}
	svc := &service{
		userService: userService,
		logger:      noopLogger{},
		jwtSecret:   "test-secret",
	}

	err := svc.ChangePassword(signedAccessToken(t, userID, "test-secret"), "current-password", "weak-pass")

	require.ErrorIs(t, err, ErrRegistrationPasswordWeak)
	require.Empty(t, userService.updatedPassword, "weak replacement password must not reach the password hasher")
	require.Nil(t, userService.updatedUser, "weak replacement password must not mutate the user")
}

func TestChangePasswordPreservesActionableAccountErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "current password required", err: users.ErrCurrentPasswordRequired},
		{name: "current password incorrect", err: users.ErrInvalidCurrentPassword},
		{name: "replacement password too short", err: utils.ErrPasswordTooShort},
		{name: "replacement password too long", err: utils.ErrPasswordTooLong},
	} {
		t.Run(test.name, func(t *testing.T) {
			userID := uuid.New()
			userService := &fakeUserService{
				userByID:            &models.User{ID: userID, Role: "owner", IsActive: true},
				passwordUpdateError: test.err,
			}
			svc := &service{
				userService:      userService,
				blockListService: fakeBlockListService{},
				logger:           noopLogger{},
				jwtSecret:        "test-secret",
			}

			token, _, err := svc.generateAccessToken(userID, "owner", uuid.New().String(), time.Now().Add(time.Hour).Unix())
			require.NoError(t, err)
			seedTestAccessAuthority(t, svc, token)
			err = svc.ChangePassword(token, "current-password", "new-password")

			require.ErrorIs(t, err, test.err)
			require.Empty(t, userService.updatedPassword)
		})
	}
}

func TestConfirmPasswordResetRejectsEmptyTokenBeforeLookup(t *testing.T) {
	userService := &fakeUserService{}
	svc := &service{
		userService: userService,
		logger:      noopLogger{},
	}

	err := svc.ConfirmPasswordReset("   ", "new-password")
	require.EqualError(t, err, "invalid token")
	require.Empty(t, userService.lookupResetToken)
}

func TestConfirmPasswordResetRejectsWeakPasswordBeforeLookup(t *testing.T) {
	userService := &fakeUserService{}
	svc := &service{
		userService: userService,
		logger:      noopLogger{},
	}

	err := svc.ConfirmPasswordReset("reset-token", "short-pass")
	require.ErrorIs(t, err, ErrRegistrationPasswordWeak)
	require.Empty(t, userService.lookupResetToken)
}

func TestConfirmPasswordResetRejectsUnknownToken(t *testing.T) {
	userService := &fakeUserService{}
	svc := &service{userService: userService, logger: noopLogger{}}

	err := svc.ConfirmPasswordReset("unknown-reset-token", "new-password")
	require.EqualError(t, err, "invalid token")
	require.Equal(t, "unknown-reset-token", userService.lookupResetToken)
	require.Zero(t, userService.consumeResetCalls)
}

func TestConfirmPasswordResetClassifiesStorageFailuresAsUnavailable(t *testing.T) {
	lookupService := &fakeUserService{resetLookupErr: errors.New("database unavailable")}
	lookupAuth := &service{userService: lookupService, logger: noopLogger{}}
	require.ErrorIs(t, lookupAuth.ConfirmPasswordReset("reset-token", "new-password"), ErrPasswordResetUnavailable)

	user := &models.User{
		ID: uuid.New(), ResetPasswordToken: utils.PasswordResetTokenDigest("reset-token"),
		ResetTokenExpires: ptrTime(time.Now().Add(time.Hour)),
	}
	consumeService := &fakeUserService{userByResetToken: user, consumeResetErr: errors.New("database unavailable")}
	consumeAuth := &service{userService: consumeService, logger: noopLogger{}}
	require.ErrorIs(t, consumeAuth.ConfirmPasswordReset("reset-token", "new-password"), ErrPasswordResetUnavailable)
}

func TestConfirmPasswordResetRejectsExpiredToken(t *testing.T) {
	userService := &fakeUserService{
		userByResetToken: &models.User{
			ID:                 uuid.New(),
			ResetPasswordToken: utils.PasswordResetTokenDigest("expired-reset-token"),
			ResetTokenExpires:  ptrTime(time.Now().Add(-time.Second)),
		},
	}
	svc := &service{userService: userService, logger: noopLogger{}}

	err := svc.ConfirmPasswordReset("expired-reset-token", "new-password")
	require.ErrorIs(t, err, ErrInvalidPasswordReset)
	require.Zero(t, userService.consumeResetCalls)
}

func TestConfirmPasswordResetUpdatesPasswordAndClearsToken(t *testing.T) {
	userID := uuid.New()
	userService := &fakeUserService{
		userByResetToken: &models.User{
			ID:                 userID,
			Email:              "user@example.com",
			Password:           "old-hash",
			ResetPasswordToken: utils.PasswordResetTokenDigest("reset-token"),
			ResetTokenExpires:  ptrTime(time.Now().Add(time.Hour)),
		},
	}
	svc := &service{
		userService: userService,
		logger:      noopLogger{},
	}

	err := svc.ConfirmPasswordReset("reset-token", "new-password")
	require.NoError(t, err)
	require.Equal(t, "reset-token", userService.lookupResetToken)
	require.Equal(t, 1, userService.consumeResetCalls)
	require.Equal(t, userID, userService.consumedResetUserID)
	require.Equal(t, "reset-token", userService.consumedResetToken)
	require.Equal(t, "new-password", userService.updatedPassword)
	require.NotNil(t, userService.updatedUser)
	require.Empty(t, userService.updatedUser.ResetPasswordToken)
	require.Nil(t, userService.updatedUser.ResetTokenExpires)
}

func TestSuccessfulPasswordResetRevokesExistingAccessAndRefreshTokens(t *testing.T) {
	userID := uuid.New()
	user := &models.User{
		ID:                 userID,
		Email:              "user@example.com",
		Role:               "operator",
		IsActive:           true,
		SessionVersion:     7,
		ResetPasswordToken: utils.PasswordResetTokenDigest("reset-token"),
		ResetTokenExpires:  ptrTime(time.Now().Add(time.Hour)),
	}
	userService := &fakeUserService{userByID: user, userByResetToken: user}
	svc := &service{
		userService:      userService,
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}
	enableTestSessionAuthority(t, svc)
	refreshToken, refreshID, refreshExp, err := svc.generateRefreshTokenWithVersion(userID, user.SessionVersion)
	require.NoError(t, err)
	accessToken, _, err := svc.generateAccessTokenWithVersion(userID, "operator", refreshID, refreshExp, user.SessionVersion)
	require.NoError(t, err)

	require.NoError(t, svc.ConfirmPasswordReset("reset-token", "new-strong-password"))
	require.Equal(t, int64(8), userService.userByID.SessionVersion)

	authenticated, err := svc.IsUserAuthenticated(accessToken)
	require.False(t, authenticated)
	require.ErrorIs(t, err, ErrRevokedSession)
	_, err = svc.RefreshToken(refreshToken)
	require.ErrorIs(t, err, ErrRevokedSession)
}

func TestRequestPasswordResetPersistsDigestAndDeliversBearerToken(t *testing.T) {
	setupAuthConfig(t)
	userService := &fakeUserService{userByEmail: &models.User{ID: uuid.New(), Email: "operator@example.com", SessionVersion: 7}}
	var sentToken string
	logger := &captureLogger{}
	svc := &service{
		userService: userService,
		passwordResetter: fakePasswordResetSender{
			configured: true,
			onSend:     func(_ string, token string, _ time.Time) { sentToken = token },
		},
		logger: logger,
	}

	token, _, err := svc.RequestPasswordReset("operator@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, token, sentToken)
	require.Equal(t, int64(7), userService.storedResetSessionVersion)
	require.NotEqual(t, token, userService.updatedUser.ResetPasswordToken)
	require.Equal(t, utils.PasswordResetTokenDigest(token), userService.updatedUser.ResetPasswordToken)
	logOutput := strings.Join(logger.messages, "\n")
	require.Contains(t, logOutput, "Successfully sent password-reset email")
	require.NotContains(t, logOutput, "operator@example.com")
}

func TestRequestPasswordResetDoesNotSendTokenForStaleAccountVersion(t *testing.T) {
	setupAuthConfig(t)
	userService := &fakeUserService{
		userByEmail:     &models.User{ID: uuid.New(), Email: "operator@example.com", SessionVersion: 3},
		storeResetError: users.ErrConcurrentUserUpdate,
	}
	sendCalls := 0
	svc := &service{
		userService: userService,
		passwordResetter: fakePasswordResetSender{
			configured: true,
			onSend:     func(string, string, time.Time) { sendCalls++ },
		},
		logger: noopLogger{},
	}

	_, _, err := svc.RequestPasswordReset("operator@example.com")

	require.EqualError(t, err, "failed to update user")
	require.Equal(t, int64(3), userService.storedResetSessionVersion)
	require.Zero(t, sendCalls, "an obsolete account snapshot must not receive a password-reset link")
	require.Nil(t, userService.updatedUser)
}

func TestRequestPasswordResetDoesNotPersistTokenWhenEmailDeliveryIsUnavailable(t *testing.T) {
	setupAuthConfig(t)
	userService := &fakeUserService{userByEmail: &models.User{ID: uuid.New(), Email: "operator@example.com"}}
	svc := &service{
		userService:      userService,
		passwordResetter: fakePasswordResetSender{configured: false},
		logger:           noopLogger{},
	}

	_, _, err := svc.RequestPasswordReset("operator@example.com")
	require.EqualError(t, err, "password reset email delivery is not configured")
	require.Nil(t, userService.updatedUser)
}

func TestRequestPasswordResetClearsTokenWhenEmailDeliveryFails(t *testing.T) {
	setupAuthConfig(t)
	userService := &fakeUserService{userByEmail: &models.User{ID: uuid.New(), Email: "operator@example.com"}}
	logger := &captureLogger{}
	svc := &service{
		userService:      userService,
		passwordResetter: fakePasswordResetSender{configured: true, err: errors.New("550 recipient operator@example.com rejected")},
		logger:           logger,
	}

	_, _, err := svc.RequestPasswordReset("operator@example.com")
	require.EqualError(t, err, "failed to send password reset email")
	require.NotNil(t, userService.updatedUser)
	require.Empty(t, userService.updatedUser.ResetPasswordToken)
	require.Nil(t, userService.updatedUser.ResetTokenExpires)
	logOutput := strings.Join(logger.messages, "\n")
	require.Contains(t, logOutput, "Password reset email delivery failed")
	require.NotContains(t, logOutput, "operator@example.com")
	require.NotContains(t, logOutput, "550 recipient")
}

func TestRequestPasswordResetLogsOnlyRedactedSMTPStage(t *testing.T) {
	setupAuthConfig(t)
	userService := &fakeUserService{userByEmail: &models.User{ID: uuid.New(), Email: "operator@example.com"}}
	providerDetail := errors.New("550 recipient operator@example.com rejected password=synthetic-secret")
	logger := &captureLogger{}
	svc := &service{
		userService: userService,
		passwordResetter: fakePasswordResetSender{
			configured: true,
			err:        services.NewPasswordResetDeliveryError("authentication", providerDetail),
		},
		logger: logger,
	}

	_, _, err := svc.RequestPasswordReset("operator@example.com")
	require.EqualError(t, err, "failed to send password reset email")
	logOutput := strings.Join(logger.messages, "\n")
	require.Contains(t, logOutput, "Password reset email delivery failed during SMTP stage authentication")
	require.NotContains(t, logOutput, "operator@example.com")
	require.NotContains(t, logOutput, "synthetic-secret")
	require.NotContains(t, logOutput, providerDetail.Error())
}

func TestAuthCapabilitiesReflectConfiguredOptionalPaths(t *testing.T) {
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "client-secret")
	t.Setenv("GOOGLE_LOGIN_REDIRECT_URL", "http://localhost/api/v1/auth/google/callback")
	svc := &service{jwtSecret: "test-secret", passwordResetter: fakePasswordResetSender{configured: true}}

	capabilities := svc.Capabilities()
	require.True(t, capabilities.GoogleLoginEnabled)
	require.True(t, capabilities.PasswordRecoveryEmailEnabled)
}

func TestAuthCapabilitiesAdvertiseOnlyFullyConfiguredLocalPreview(t *testing.T) {
	setupAuthConfig(t)
	config.LocalPreviewConfig.Enabled = true
	config.LocalPreviewConfig.OwnerEmail = "owner@example.com"
	config.LocalPreviewConfig.GatewayHostBind = "127.0.0.1"
	config.LocalPreviewConfig.GatewaySecret = strings.Repeat("a", 64)
	svc := &service{jwtSecret: "test-secret"}

	require.True(t, svc.Capabilities().LocalPreviewEnabled)
}

func TestReadinessRequiresDatabaseAndSessionRevocationStore(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	redisErr := errors.New("redis unavailable")
	databaseCalls := 0
	blockList := &readinessBlockListService{err: redisErr}
	svc := &service{
		databaseReady: func(context.Context) error {
			databaseCalls++
			return nil
		},
		blockListService: blockList,
	}
	enableTestSessionAuthority(t, svc)

	if err := svc.Readiness(context.Background()); !errors.Is(err, redisErr) {
		t.Fatalf("Readiness() error = %v, want Redis error", err)
	}
	if databaseCalls != 1 || blockList.pingCalls != 1 {
		t.Fatalf("readiness checks database=%d redis=%d, want each exactly once", databaseCalls, blockList.pingCalls)
	}

	databaseCalls = 0
	blockList.pingCalls = 0
	svc.databaseReady = func(context.Context) error {
		databaseCalls++
		return databaseErr
	}
	if err := svc.Readiness(context.Background()); !errors.Is(err, databaseErr) {
		t.Fatalf("Readiness() error = %v, want database error", err)
	}
	if databaseCalls != 1 || blockList.pingCalls != 0 {
		t.Fatalf("database failure should stop readiness checks; database=%d redis=%d", databaseCalls, blockList.pingCalls)
	}
}

type readinessBlockListService struct {
	fakeBlockListService
	err       error
	pingCalls int
}

func (s *readinessBlockListService) Ping(context.Context) error {
	s.pingCalls++
	return s.err
}

func TestLocalPreviewLoginRequiresExplicitConfigAndOwner(t *testing.T) {
	setupAuthConfig(t)
	config.LocalPreviewConfig.Enabled = true
	config.LocalPreviewConfig.OwnerEmail = "owner@example.com"
	config.LocalPreviewConfig.GatewaySecret = strings.Repeat("a", 64)
	ownerID := uuid.New()
	svc := &service{
		userService:      &fakeUserService{userByEmail: &models.User{ID: ownerID, Email: "owner@example.com", Role: "owner", IsActive: true}},
		blockListService: fakeBlockListService{},
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}

	enableTestSessionAuthority(t, svc)
	tokens, err := svc.LocalPreviewLogin()
	require.NoError(t, err)
	require.NotEmpty(t, tokens.AccessToken)

	config.LocalPreviewConfig.Enabled = false
	_, err = svc.LocalPreviewLogin()
	require.EqualError(t, err, "local preview is not enabled")
}

func TestLocalPreviewLoginRejectsBlockedOwner(t *testing.T) {
	setupAuthConfig(t)
	config.LocalPreviewConfig.Enabled = true
	config.LocalPreviewConfig.OwnerEmail = "owner@example.com"
	config.LocalPreviewConfig.GatewaySecret = strings.Repeat("a", 64)
	svc := &service{
		userService: &fakeUserService{userByEmail: &models.User{
			ID:        uuid.New(),
			Email:     "owner@example.com",
			Role:      "owner",
			IsActive:  true,
			IsBlocked: true,
		}},
		logger:    noopLogger{},
		jwtSecret: "test-secret",
	}

	_, err := svc.LocalPreviewLogin()
	require.EqualError(t, err, "local preview owner session is unavailable")
}

func TestLocalPreviewLoginRejectsInvalidConfiguredBind(t *testing.T) {
	setupAuthConfig(t)
	config.LocalPreviewConfig.Enabled = true
	config.LocalPreviewConfig.OwnerEmail = "owner@example.com"
	config.LocalPreviewConfig.GatewayHostBind = "0.0.0.0"
	config.LocalPreviewConfig.GatewaySecret = strings.Repeat("a", 64)
	svc := &service{
		userService: &fakeUserService{userByEmail: &models.User{ID: uuid.New(), Email: "owner@example.com", Role: "owner", IsActive: true}},
		logger:      noopLogger{},
		jwtSecret:   "test-secret",
	}

	_, err := svc.LocalPreviewLogin()
	require.EqualError(t, err, "local preview is not enabled")
}

func setupAuthConfig(t *testing.T) {
	t.Helper()
	t.Setenv("LOGGER_TOPIC", "logs")
	t.Setenv("MAIL_TOPIC", "mail")
	t.Setenv("BROKERS_ADDR", "localhost:9092")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_NAME", "automation_hub")
	t.Setenv("PASSWORD_RESET_TOPIC", "password-reset")
	t.Setenv("ACCOUNT_BLOCKED_TOPIC", "account-blocked")
	t.Setenv("ACCOUNT_CREATED_TOPIC", "account-created")
	t.Setenv("JWT_SECRET", testSigningSecret)
	t.Setenv("BLOCKING_TIME_EXPONENTIATION_BASIS", "1")
	t.Setenv("MAX_LOGIN_ATTEMPTS_BEFORE_BLOCK", "3")
	t.Setenv("LOCAL_LOGIN_BYPASS_ENABLED", "false")
	t.Setenv("GATEWAY_HOST_BIND", "127.0.0.1")
	require.NoError(t, config.Setup())
}

func signedAccessToken(t *testing.T, userID uuid.UUID, secret string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":          tokenIssuer,
		"aud":          tokenAudience,
		"iat":          time.Now().Unix(),
		"token_type":   tokenTypeAccess,
		"user_id":      userID.String(),
		"access_uuid":  uuid.New().String(),
		"refresh_uuid": uuid.New().String(),
		"refresh_exp":  time.Now().Add(time.Hour).Unix(),
		"exp":          time.Now().Add(time.Minute).Unix(),
	})
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return tokenString
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

type fakeUserService struct {
	userByID                  *models.User
	userByEmail               *models.User
	userByResetToken          *models.User
	lookupResetToken          string
	lookupEmail               string
	updatedPassword           string
	updatedUser               *models.User
	passwordUpdateError       error
	storeResetError           error
	storedResetSessionVersion int64
	consumeResetCalls         int
	consumedResetUserID       uuid.UUID
	consumedResetToken        string
	resetLookupErr            error
	consumeResetErr           error
	createUserErr             error
	concurrentGoogleUser      *models.User
	createUserCalls           int
	getUserByEmailCalls       int
}

func (f *fakeUserService) CreateUser(user models.User) (*models.User, error) {
	f.createUserCalls++
	if f.createUserErr != nil {
		if f.concurrentGoogleUser != nil {
			f.userByEmail = f.concurrentGoogleUser
		}
		return nil, f.createUserErr
	}
	return &user, nil
}

func (f *fakeUserService) GetUserByID(id uuid.UUID) (*models.User, error) {
	if f.userByID != nil && f.userByID.ID == id {
		return f.userByID, nil
	}
	if f.userByEmail != nil && f.userByEmail.ID == id {
		return f.userByEmail, nil
	}
	return nil, errors.New("user not found")
}

func (f *fakeUserService) GetUserByEmail(email string) (*models.User, error) {
	f.getUserByEmailCalls++
	f.lookupEmail = email
	if f.userByEmail == nil {
		return nil, users.ErrUserNotFound
	}
	return f.userByEmail, nil
}

func (f *fakeUserService) GetUserByResetToken(token string) (*models.User, error) {
	f.lookupResetToken = token
	if f.resetLookupErr != nil {
		return nil, f.resetLookupErr
	}
	if f.userByResetToken == nil {
		return nil, users.ErrUserNotFound
	}
	return f.userByResetToken, nil
}

func (f *fakeUserService) StorePasswordResetToken(id uuid.UUID, expectedSessionVersion int64, tokenDigest string, expiresAt time.Time) error {
	f.storedResetSessionVersion = expectedSessionVersion
	if f.storeResetError != nil {
		return f.storeResetError
	}
	var current *models.User
	if f.userByEmail != nil && f.userByEmail.ID == id {
		current = f.userByEmail
	} else if f.userByID != nil && f.userByID.ID == id {
		current = f.userByID
	}
	if current == nil {
		return errors.New("user not found")
	}
	if current.SessionVersion != expectedSessionVersion {
		return users.ErrConcurrentUserUpdate
	}
	updated := *current
	updated.ResetPasswordToken = tokenDigest
	updated.ResetTokenExpires = &expiresAt
	f.updatedUser = &updated
	f.userByEmail = &updated
	if f.userByID != nil && f.userByID.ID == id {
		f.userByID = &updated
	}
	return nil
}

func (f *fakeUserService) CompleteSuccessfulLogin(user models.User) (*models.User, error) {
	user.FailedAttempts = 0
	user.LastAttempt = nil
	user.IsBlocked = false
	user.BlockedUntil = nil
	f.updatedUser = &user
	if f.userByID != nil && f.userByID.ID == user.ID {
		f.userByID = &user
	}
	if f.userByEmail != nil && f.userByEmail.ID == user.ID {
		f.userByEmail = &user
	}
	return &user, nil
}

func (f *fakeUserService) UpdateUser(user models.User) (*models.User, error) {
	f.updatedUser = &user
	return &user, nil
}

func (f *fakeUserService) DeleteUser(id uuid.UUID) error {
	return nil
}

func (f *fakeUserService) GetAllUsers(p *utils.Pagination) ([]*models.User, error) {
	return nil, nil
}

func (f *fakeUserService) UpdatePasswordWithCurrentPassword(id uuid.UUID, expectedSessionVersion int64, currentPassword, newPassword string) error {
	if currentPassword == "" {
		return users.ErrCurrentPasswordRequired
	}
	if f.passwordUpdateError != nil {
		return f.passwordUpdateError
	}
	return f.UpdatePassword(id, expectedSessionVersion, newPassword)
}

func (f *fakeUserService) UpdateAccount(id uuid.UUID, expectedSessionVersion int64, currentPassword, email, newPassword string) (*models.User, error) {
	if newPassword != "" {
		if err := f.UpdatePasswordWithCurrentPassword(id, expectedSessionVersion, currentPassword, newPassword); err != nil {
			return nil, err
		}
	}
	if f.updatedUser != nil {
		if email != "" {
			f.updatedUser.Email = email
		}
		return f.updatedUser, nil
	}
	return f.userByID, nil
}

func (f *fakeUserService) UpdatePassword(id uuid.UUID, expectedSessionVersion int64, newPassword string) error {
	f.updatedPassword = newPassword
	for _, user := range []*models.User{f.userByID, f.userByEmail, f.userByResetToken} {
		if user == nil || user.ID != id {
			continue
		}
		if user.SessionVersion != expectedSessionVersion {
			return users.ErrConcurrentUserUpdate
		}
		updated := *user
		updated.FirstAccess = false
		updated.SessionVersion++
		updated.ResetPasswordToken = ""
		updated.ResetTokenExpires = nil
		f.updatedUser = &updated
		if f.userByID == user {
			f.userByID = &updated
		}
		if f.userByEmail == user {
			f.userByEmail = &updated
		}
		if f.userByResetToken == user {
			f.userByResetToken = &updated
		}
	}
	return nil
}

func (f *fakeUserService) ConsumePasswordResetToken(id uuid.UUID, token, newPassword string) error {
	f.consumeResetCalls++
	f.consumedResetUserID = id
	f.consumedResetToken = token
	if f.consumeResetErr != nil {
		return f.consumeResetErr
	}
	if f.userByResetToken == nil || f.userByResetToken.ID != id ||
		f.userByResetToken.ResetPasswordToken != utils.PasswordResetTokenDigest(token) ||
		f.userByResetToken.ResetTokenExpires == nil || !f.userByResetToken.ResetTokenExpires.After(time.Now()) {
		return users.ErrInvalidPasswordResetToken
	}
	f.updatedPassword = newPassword
	updated := *f.userByResetToken
	updated.ResetPasswordToken = ""
	updated.ResetTokenExpires = nil
	updated.FirstAccess = false
	updated.SessionVersion++
	f.updatedUser = &updated
	f.userByResetToken = &updated
	if f.userByID != nil && f.userByID.ID == id {
		f.userByID = &updated
	}
	return nil
}

func (f *fakeUserService) ClearPasswordResetToken(id uuid.UUID, token string) error {
	digest := utils.PasswordResetTokenDigest(token)
	if f.updatedUser != nil && f.updatedUser.ID == id && f.updatedUser.ResetPasswordToken == digest {
		updated := *f.updatedUser
		updated.ResetPasswordToken = ""
		updated.ResetTokenExpires = nil
		f.updatedUser = &updated
	}
	return nil
}

type fakeBlockListService struct{}

func (fakeBlockListService) RegisterRefreshSession(string, string, time.Duration) error {
	return nil
}

func (fakeBlockListService) IsRefreshSessionActive(string, string) (bool, error) {
	return true, nil
}

func (fakeBlockListService) AddToBlockList(jwtUUID string, expirationTime time.Duration) error {
	return nil
}

func (fakeBlockListService) IsInBlockList(jwtUUID string) (bool, error) {
	return false, nil
}

func (fakeBlockListService) IsRefreshFamilyRevoked(string) (bool, error) {
	return false, nil
}

func (fakeBlockListService) RevokeRefreshFamily(string, time.Duration) error {
	return nil
}

func (fakeBlockListService) RotateRefreshToken(_, _, _ string, _, _ time.Duration, replacement string) (string, bool, error) {
	return replacement, true, nil
}

type inMemoryBlockListService struct {
	mu             sync.Mutex
	blocked        map[string]bool
	families       map[string]bool
	rotated        map[string]string
	activeFamilies map[string]bool
	activeTokens   map[string]string
}

func (s *inMemoryBlockListService) RegisterRefreshSession(refreshUUID, familyUUID string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked[refreshUUID] || s.families[familyUUID] || s.activeFamilies[familyUUID] || s.activeTokens[refreshUUID] != "" {
		return errors.New("session exists or revoked")
	}
	if s.activeFamilies == nil {
		s.activeFamilies = make(map[string]bool)
		s.activeTokens = make(map[string]string)
	}
	s.activeFamilies[familyUUID] = true
	s.activeTokens[refreshUUID] = familyUUID
	return nil
}

func (s *inMemoryBlockListService) IsRefreshSessionActive(refreshUUID, familyUUID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeFamilies[familyUUID] && s.activeTokens[refreshUUID] == familyUUID && !s.blocked[refreshUUID] && !s.families[familyUUID], nil
}

func (s *inMemoryBlockListService) AddToBlockList(jwtUUID string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked == nil {
		s.blocked = make(map[string]bool)
	}
	s.blocked[jwtUUID] = true
	delete(s.activeTokens, jwtUUID)
	delete(s.rotated, jwtUUID)
	return nil
}

func (s *inMemoryBlockListService) IsInBlockList(jwtUUID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blocked[jwtUUID], nil
}

func (s *inMemoryBlockListService) IsRefreshFamilyRevoked(familyUUID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.families[familyUUID], nil
}

func (s *inMemoryBlockListService) RevokeRefreshFamily(familyUUID string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.families == nil {
		s.families = make(map[string]bool)
	}
	s.families[familyUUID] = true
	delete(s.activeFamilies, familyUUID)
	return nil
}

func (s *inMemoryBlockListService) RotateRefreshToken(refreshUUID, familyUUID, replacementUUID string, _, _ time.Duration, replacement string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.families[familyUUID] || !s.activeFamilies[familyUUID] {
		return "", false, nil
	}
	if cached := s.rotated[refreshUUID]; cached != "" {
		return cached, false, nil
	}
	if s.blocked[refreshUUID] || s.activeTokens[refreshUUID] != familyUUID {
		return "", false, nil
	}
	if s.blocked == nil {
		s.blocked = make(map[string]bool)
	}
	if s.rotated == nil {
		s.rotated = make(map[string]string)
	}
	s.blocked[refreshUUID] = true
	delete(s.activeTokens, refreshUUID)
	s.activeTokens[replacementUUID] = familyUUID
	s.rotated[refreshUUID] = replacement
	return replacement, true, nil
}

type recordingBlockListService struct {
	added        []string
	families     []string
	errorsByCall []error
}

func (*recordingBlockListService) RegisterRefreshSession(string, string, time.Duration) error {
	return nil
}

func (*recordingBlockListService) IsRefreshSessionActive(string, string) (bool, error) {
	return true, nil
}

func (f *recordingBlockListService) AddToBlockList(jwtUUID string, _ time.Duration) error {
	f.added = append(f.added, jwtUUID)
	call := len(f.added) - 1
	if call < len(f.errorsByCall) {
		return f.errorsByCall[call]
	}
	return nil
}

func (*recordingBlockListService) IsInBlockList(string) (bool, error) {
	return false, nil
}

func (*recordingBlockListService) IsRefreshFamilyRevoked(string) (bool, error) {
	return false, nil
}

func (f *recordingBlockListService) RevokeRefreshFamily(familyUUID string, _ time.Duration) error {
	f.families = append(f.families, familyUUID)
	return nil
}

func (*recordingBlockListService) RotateRefreshToken(_, _, _ string, _, _ time.Duration, replacement string) (string, bool, error) {
	return replacement, true, nil
}

type noopLogger struct{}

func (noopLogger) Info(message string, args ...interface{})  {}
func (noopLogger) Error(message string, args ...interface{}) {}
func (noopLogger) Warn(message string, args ...interface{})  {}
func (noopLogger) Debug(message string, args ...interface{}) {}

type captureLogger struct{ messages []string }

func (l *captureLogger) Info(message string, args ...interface{}) {
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}
func (l *captureLogger) Error(message string, args ...interface{}) {
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}
func (l *captureLogger) Warn(message string, args ...interface{}) {
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}
func (l *captureLogger) Debug(message string, args ...interface{}) {
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}

type fakePasswordResetSender struct {
	configured bool
	err        error
	onSend     func(email, token string, expiresAt time.Time)
}

type mockGoogleOAuthProvider struct {
	email       string
	exchangeErr error
}

func (mockGoogleOAuthProvider) Configured() bool { return true }
func (mockGoogleOAuthProvider) AuthCodeURL() (string, error) {
	return "https://mock-provider.invalid/authorize", nil
}
func (m mockGoogleOAuthProvider) Exchange(_ context.Context, code, state string) (string, error) {
	if m.exchangeErr != nil {
		return "", m.exchangeErr
	}
	if code != "mock-code" || state != "mock-state" {
		return "", errors.New("unexpected mock OAuth request")
	}
	return m.email, nil
}

func (f fakePasswordResetSender) Configured() bool { return f.configured }
func (f fakePasswordResetSender) SendPasswordReset(email, token string, expiresAt time.Time) error {
	if f.onSend != nil {
		f.onSend(email, token, expiresAt)
	}
	return f.err
}

var _ iservice.PasswordResetSender = fakePasswordResetSender{}

var _ IService = (*service)(nil)
