package authentication

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/services/iservice"
	"automation-hub-idp/internal/app/users"
	"automation-hub-idp/internal/app/utils"
	"context"
	"errors"
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
		jwtSecret:        "test-secret",
	}

	refreshToken, refreshUUID, refreshExp, err := svc.generateRefreshToken(userID)
	require.NoError(t, err)

	tokenDetails, err := svc.RefreshToken(context.Background(), refreshToken)
	require.NoError(t, err)
	require.NotEmpty(t, tokenDetails.AccessToken)
	require.Equal(t, refreshToken, tokenDetails.RefreshToken)
	require.Equal(t, refreshUUID, tokenDetails.RefreshUUID)
	require.Equal(t, refreshExp, tokenDetails.RtExpires)
	_, claims, err := svc.parseAndValidateToken(tokenDetails.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "owner", claims["role"])
}

func TestGenerateAccessTokenDowngradesUnknownRole(t *testing.T) {
	setupAuthConfig(t)
	svc := &service{logger: noopLogger{}, jwtSecret: "test-secret"}
	token, _, err := svc.generateAccessToken(uuid.New(), "unexpected", uuid.New().String(), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
	_, claims, err := svc.parseAndValidateToken(token)
	require.NoError(t, err)
	require.Equal(t, "viewer", claims["role"])
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
				userService: &fakeUserService{userByID: &models.User{ID: userID, Role: test.role, IsActive: true}},
				logger:      noopLogger{},
				jwtSecret:   "test-secret",
			}
			token, _, err := svc.generateAccessToken(userID, test.role, uuid.NewString(), time.Now().Add(time.Hour).Unix())
			require.NoError(t, err)

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

	err = svc.Logout(context.Background(), tokenString)
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

	_, err = svc.RefreshToken(context.Background(), accessToken)
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
	refreshToken, _, _, err := svc.generateRefreshToken(userID)
	require.NoError(t, err)

	authenticated, err := svc.IsUserAuthenticated(context.Background(), refreshToken)
	require.False(t, authenticated)
	require.EqualError(t, err, "invalid accessToken")
	_, err = svc.GetIdFromToken(refreshToken)
	require.EqualError(t, err, "invalid accessToken")
	_, err = svc.GetSessionFromToken(refreshToken)
	require.EqualError(t, err, "invalid accessToken")
	require.EqualError(t, svc.ChangePassword(refreshToken, "new-password-2026"), "invalid accessToken")
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
		refreshToken, _, _, err := svc.generateRefreshToken(userID)
		require.NoError(t, err)

		_, err = svc.RefreshToken(context.Background(), refreshToken)
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
		token, _, err := svc.generateAccessToken(userID, "owner", uuid.NewString(), time.Now().Add(time.Hour).Unix())
		require.NoError(t, err)

		authenticated, err := svc.IsUserAuthenticated(context.Background(), token)
		require.False(t, authenticated)
		require.EqualError(t, err, "user is unavailable")
		_, err = svc.GetSessionFromToken(token)
		require.EqualError(t, err, "user is unavailable")
	}
}

func TestSessionUsesCurrentStoredRoleInsteadOfTokenRole(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService: &fakeUserService{userByID: &models.User{ID: userID, Role: "viewer", IsActive: true}},
		logger:      noopLogger{},
		jwtSecret:   "test-secret",
	}
	token, _, err := svc.generateAccessToken(userID, "owner", uuid.NewString(), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)

	session, err := svc.GetSessionFromToken(token)
	require.NoError(t, err)
	require.Equal(t, "viewer", session.Role)
	require.False(t, session.Permissions.CanOperate)
	require.False(t, session.Permissions.CanApprove)
	require.False(t, session.Permissions.CanAdminister)
}

func TestSessionDowngradesUnknownStoredRoleToViewer(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	svc := &service{
		userService: &fakeUserService{userByID: &models.User{ID: userID, Role: "legacy-admin", IsActive: true}},
		logger:      noopLogger{},
		jwtSecret:   "test-secret",
	}
	token, _, err := svc.generateAccessToken(userID, "owner", uuid.NewString(), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)

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
		blockListService: blockList,
		logger:           noopLogger{},
		jwtSecret:        "test-secret",
	}
	token, _, err := svc.generateAccessToken(userID, "owner", "refresh-id", time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)

	err = svc.Logout(context.Background(), token)
	require.ErrorContains(t, err, "revoke refresh token")
	require.Len(t, blockList.added, 2)
	require.Equal(t, "refresh-id", blockList.added[0])
	require.NotEqual(t, blockList.added[0], blockList.added[1])
}

func TestChangePasswordPassesPlaintextPasswordToUserService(t *testing.T) {
	userID := uuid.New()
	userService := &fakeUserService{
		userByID: &models.User{
			ID:                 userID,
			Email:              "user@example.com",
			Password:           "old-hash",
			ResetPasswordToken: "stale-token",
			ResetTokenExpires:  ptrTime(time.Now().Add(time.Hour)),
		},
	}
	svc := &service{
		userService: userService,
		logger:      noopLogger{},
		jwtSecret:   "test-secret",
	}

	err := svc.ChangePassword(signedAccessToken(t, userID, "test-secret"), "new-password")
	require.NoError(t, err)
	require.Equal(t, "new-password", userService.updatedPassword)
	require.NotNil(t, userService.updatedUser)
	require.Empty(t, userService.updatedUser.ResetPasswordToken)
	require.Nil(t, userService.updatedUser.ResetTokenExpires)
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

func TestConfirmPasswordResetUpdatesPasswordAndClearsToken(t *testing.T) {
	userService := &fakeUserService{}
	svc := &service{
		userService: userService,
		logger:      noopLogger{},
	}

	err := svc.ConfirmPasswordReset("reset-token", "new-password")
	require.NoError(t, err)
	require.Equal(t, "reset-token", userService.completedResetToken)
	require.Equal(t, "new-password", userService.completedResetPassword)
}

func TestConfirmPasswordResetMapsConsumedOrExpiredTokenToInvalid(t *testing.T) {
	userService := &fakeUserService{completeResetErr: users.ErrInvalidResetToken}
	svc := &service{userService: userService, logger: noopLogger{}}

	err := svc.ConfirmPasswordReset("replayed-token", "new-password")
	require.EqualError(t, err, "invalid token")
	require.Equal(t, "replayed-token", userService.completedResetToken)
}

func TestRequestPasswordResetDoesNotPersistTokenWhenEmailDeliveryIsUnavailable(t *testing.T) {
	setupAuthConfig(t)
	userService := &fakeUserService{userByEmail: &models.User{ID: uuid.New(), Email: "operator@example.com"}}
	svc := &service{
		userService:      userService,
		passwordResetter: fakePasswordResetSender{configured: false},
		logger:           noopLogger{},
	}

	_, _, err := svc.RequestPasswordReset(context.Background(), "operator@example.com")
	require.EqualError(t, err, "password reset email delivery is not configured")
	require.Empty(t, userService.storedResetToken)
}

func TestRequestPasswordResetClearsTokenWhenEmailDeliveryFails(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	userService := &fakeUserService{userByEmail: &models.User{ID: userID, Email: "operator@example.com"}}
	svc := &service{
		userService:      userService,
		passwordResetter: fakePasswordResetSender{configured: true, err: errors.New("smtp unavailable")},
		logger:           noopLogger{},
	}

	_, _, err := svc.RequestPasswordReset(context.Background(), "operator@example.com")
	require.EqualError(t, err, "failed to send password reset email")
	require.Equal(t, userID, userService.storedResetUserID)
	require.NotEmpty(t, userService.storedResetToken)
	require.Equal(t, userService.storedResetToken, userService.clearedResetToken)
	require.Equal(t, userID, userService.clearedResetUserID)
}

func TestRequestPasswordResetStoresDeliveredTokenWithoutRollback(t *testing.T) {
	setupAuthConfig(t)
	userID := uuid.New()
	userService := &fakeUserService{userByEmail: &models.User{ID: userID, Email: "operator@example.com"}}
	svc := &service{
		userService:      userService,
		passwordResetter: fakePasswordResetSender{configured: true},
		logger:           noopLogger{},
	}

	token, expiresAt, err := svc.RequestPasswordReset(context.Background(), "operator@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, token, userService.storedResetToken)
	require.Equal(t, expiresAt, userService.storedResetExpiresAt)
	require.Empty(t, userService.clearedResetToken)
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

func TestLocalPreviewLoginRequiresExplicitConfigAndOwner(t *testing.T) {
	setupAuthConfig(t)
	config.LocalPreviewConfig.Enabled = true
	config.LocalPreviewConfig.OwnerEmail = "owner@example.com"
	ownerID := uuid.New()
	svc := &service{
		userService: &fakeUserService{userByEmail: &models.User{ID: ownerID, Email: "owner@example.com", Role: "owner", IsActive: true}},
		logger:      noopLogger{},
		jwtSecret:   "test-secret",
	}

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

func setupAuthConfig(t *testing.T) {
	t.Helper()
	t.Setenv("LOGGER_TOPIC", "logs")
	t.Setenv("MAIL_TOPIC", "mail")
	t.Setenv("BROKERS_ADDR", "localhost:9092")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_NAME", "automation_hub")
	t.Setenv("DB_USER", "test_user")
	t.Setenv("DB_PASSWORD", "test_password")
	t.Setenv("RUN_MODE", "test")
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
	userByID               *models.User
	userByEmail            *models.User
	userByResetToken       *models.User
	lookupResetToken       string
	lookupEmail            string
	updatedPassword        string
	updatedUser            *models.User
	completedResetToken    string
	completedResetPassword string
	completeResetErr       error
	storedResetUserID      uuid.UUID
	storedResetToken       string
	storedResetExpiresAt   time.Time
	clearedResetUserID     uuid.UUID
	clearedResetToken      string
}

func (f *fakeUserService) CreateUser(user models.User) (*models.User, error) {
	return &user, nil
}

func (f *fakeUserService) GetUserByID(id uuid.UUID) (*models.User, error) {
	if f.userByID == nil {
		return nil, errors.New("user not found")
	}
	return f.userByID, nil
}

func (f *fakeUserService) GetUserByEmail(email string) (*models.User, error) {
	f.lookupEmail = email
	if f.userByEmail == nil {
		return nil, users.ErrUserNotFound
	}
	return f.userByEmail, nil
}

func (f *fakeUserService) GetUserByResetToken(token string) (*models.User, error) {
	f.lookupResetToken = token
	if f.userByResetToken == nil {
		return nil, errors.New("user not found")
	}
	return f.userByResetToken, nil
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

func (f *fakeUserService) UpdatePassword(id uuid.UUID, newPassword string) error {
	f.updatedPassword = newPassword
	return nil
}

func (f *fakeUserService) CompletePasswordReset(token, newPassword string) error {
	f.completedResetToken = token
	f.completedResetPassword = newPassword
	return f.completeResetErr
}

func (f *fakeUserService) StorePasswordReset(id uuid.UUID, token string, expiresAt time.Time) error {
	f.storedResetUserID = id
	f.storedResetToken = token
	f.storedResetExpiresAt = expiresAt
	return nil
}

func (f *fakeUserService) ClearPasswordResetIfToken(id uuid.UUID, token string) error {
	f.clearedResetUserID = id
	f.clearedResetToken = token
	return nil
}

type fakeBlockListService struct{}

func (fakeBlockListService) AddToBlockList(context.Context, string, time.Duration) error {
	return nil
}

func (fakeBlockListService) IsInBlockList(context.Context, string) (bool, error) {
	return false, nil
}

type recordingBlockListService struct {
	added        []string
	errorsByCall []error
}

func (f *recordingBlockListService) AddToBlockList(_ context.Context, jwtUUID string, _ time.Duration) error {
	f.added = append(f.added, jwtUUID)
	call := len(f.added) - 1
	if call < len(f.errorsByCall) {
		return f.errorsByCall[call]
	}
	return nil
}

func (*recordingBlockListService) IsInBlockList(context.Context, string) (bool, error) {
	return false, nil
}

type noopLogger struct{}

func (noopLogger) Info(message string, args ...interface{})  {}
func (noopLogger) Error(message string, args ...interface{}) {}
func (noopLogger) Warn(message string, args ...interface{})  {}
func (noopLogger) Debug(message string, args ...interface{}) {}

type fakePasswordResetSender struct {
	configured bool
	err        error
}

func (f fakePasswordResetSender) Configured() bool { return f.configured }
func (f fakePasswordResetSender) SendPasswordReset(context.Context, string, string, time.Time) error {
	return f.err
}

var _ iservice.PasswordResetSender = fakePasswordResetSender{}

var _ IService = (*service)(nil)
