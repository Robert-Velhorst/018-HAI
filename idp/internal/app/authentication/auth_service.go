package authentication

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/services"
	"automation-hub-idp/internal/app/services/iservice"
	"automation-hub-idp/internal/app/users"
	"automation-hub-idp/internal/app/utils"
	"automation-hub-idp/internal/infra"
	"context"
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"math"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const minimumPasswordLength = utils.MinimumPasswordLength

var (
	ErrRegistrationEmailInvalid    = errors.New("enter a valid email address")
	ErrRegistrationPasswordWeak    = utils.ErrPasswordTooShort
	ErrRegistrationPasswordTooLong = utils.ErrPasswordTooLong
	ErrRegistrationEmailInUse      = errors.New("an account with this email already exists")
	ErrInvalidPasswordReset        = errors.New("invalid token")
	ErrPasswordResetUnavailable    = errors.New("password reset service unavailable")
	ErrRevokedSession              = errors.New("session is no longer valid")
)

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
	tokenIssuer      = "hai-idp"
	tokenAudience    = "hai"
)

type service struct {
	userService          users.UserAccountUpdateService
	loginFailureRecorder LoginFailureRecorder
	googleOAuthFactory   func(string) googleOAuthProvider
	hasher               utils.PasswordHasher
	blockListService     iservice.TokenBlockListService
	sessionRepository    irepository.SessionRepository
	logger               iservice.Logger
	sender               iservice.MessageSender
	passwordResetter     iservice.PasswordResetSender
	jwtSecret            string
	databaseReady        func(context.Context) error
	ownedResources       *services.OwnedResources
}

func (a *service) Close() error {
	if a == nil {
		return nil
	}
	return a.ownedResources.Close()
}

type LoginFailureRecorder interface {
	RecordLoginFailure(id uuid.UUID, now time.Time, maxAttempts int, baseBlockDuration time.Duration) (*models.User, bool, error)
}

type googleOAuthProvider interface {
	Configured() bool
	AuthCodeURL() (string, error)
	Exchange(context.Context, string, string) (string, error)
}

func (a *service) googleOAuthProvider() googleOAuthProvider {
	if a.googleOAuthFactory != nil {
		return a.googleOAuthFactory(a.jwtSecret)
	}
	return newGoogleOAuth(a.jwtSecret)
}

func passwordMeetsMinimumLength(password string) bool {
	return utf8.RuneCountInString(password) >= minimumPasswordLength
}

func NewService(userService users.UserAccountUpdateService, loginFailureRecorder LoginFailureRecorder, hasher utils.PasswordHasher, sender iservice.MessageSender,
	blockListService iservice.TokenBlockListService, logger iservice.Logger, jwtSecret string, sessionRepository irepository.SessionRepository,
	passwordResetter ...iservice.PasswordResetSender) IService {
	var resetter iservice.PasswordResetSender
	if len(passwordResetter) > 0 {
		resetter = passwordResetter[0]
	}
	return &service{
		userService:          userService,
		loginFailureRecorder: loginFailureRecorder,
		hasher:               hasher,
		blockListService:     blockListService,
		sessionRepository:    sessionRepository,
		logger:               logger,
		sender:               sender,
		passwordResetter:     resetter,
		jwtSecret:            jwtSecret,
	}
}

func GetDefaultAuthService() (IService, error) {
	database, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	authService, err := NewDefaultAuthServiceWithDatabase(database)
	if err != nil {
		if pool, poolErr := database.DB(); poolErr == nil {
			err = errors.Join(err, pool.Close())
		}
	}
	return authService, err
}

// NewDefaultAuthServiceWithDatabase borrows the caller-owned initialized database.
func NewDefaultAuthServiceWithDatabase(database *gorm.DB) (result IService, err error) {
	owned := &services.OwnedResources{}
	defer func() {
		if result == nil {
			err = errors.Join(err, owned.Close())
		}
	}()
	if database == nil || database.Config == nil {
		return nil, errors.New("authentication database is required")
	}
	logger, err := services.DefaultLogger()
	if err != nil {
		return nil, err
	}
	owned.Add(logger)
	databaseSQL, err := database.DB()
	if err != nil {
		return nil, err
	}
	hasher := config.AuthenticationConfig.PasswordHasher
	userRepository := repositories.NewGormUserRepository(database, logger)
	loginFailureRecorder, ok := userRepository.(LoginFailureRecorder)
	if !ok {
		return nil, errors.New("default user repository does not support atomic login failure recording")
	}
	userService := users.NewUserService(userRepository, logger, hasher)
	sender, err := services.DefaultMessageSender()
	if err != nil {
		return nil, err
	}
	owned.Add(sender)
	blockListService := services.NewRedisTokenBlockListService()
	owned.Add(blockListService)
	passwordResetter := services.NewSMTPPasswordResetSender(
		config.MailConfig.Host,
		config.MailConfig.Port,
		config.MailConfig.Username,
		config.MailConfig.Password,
		config.MailConfig.From,
		config.MailConfig.RequireStartTLS,
	)
	authService := NewService(userService, loginFailureRecorder, hasher, sender, blockListService, logger, config.AuthenticationConfig.JwtSecret, repositories.NewGormSessionRepository(database), passwordResetter)
	if concrete, ok := authService.(*service); ok {
		concrete.databaseReady = databaseSQL.PingContext
		concrete.ownedResources = owned
	}
	return authService, nil
}

// Readiness verifies the dependencies required to validate and revoke sessions.
// Liveness remains independent so transient store outages do not restart the process.
func (a *service) Readiness(ctx context.Context) error {
	if a == nil || a.databaseReady == nil {
		return errors.New("authentication database readiness is unavailable")
	}
	if err := a.databaseReady(ctx); err != nil {
		return fmt.Errorf("authentication database is not ready: %w", err)
	}
	if a.sessionRepository == nil {
		return irepository.ErrSessionAuthorityUnavailable
	}
	if err := a.sessionRepository.Ready(ctx); err != nil {
		return fmt.Errorf("durable session authority is not ready: %w", err)
	}
	if a.blockListService == nil {
		return errors.New("session revocation store is unavailable")
	}
	pinger, ok := a.blockListService.(interface{ Ping(context.Context) error })
	if !ok {
		return errors.New("session revocation readiness is unavailable")
	}
	if err := pinger.Ping(ctx); err != nil {
		return fmt.Errorf("session revocation store is not ready: %w", err)
	}
	return nil
}

func (a *service) Capabilities() dto.AuthCapabilities {
	googleOAuth := a.googleOAuthProvider()
	return dto.AuthCapabilities{
		GoogleLoginEnabled:           googleOAuth.Configured(),
		PasswordRecoveryEmailEnabled: a.passwordResetter != nil && a.passwordResetter.Configured(),
		LocalPreviewEnabled:          config.LocalPreviewConfig.LocalPreviewSessionAllowed(),
	}
}

func (a *service) Register(userDTO dto.UserDTO) (*dto.UserResponse, error) {
	email := strings.TrimSpace(strings.ToLower(userDTO.Email))
	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || parsedEmail.Address != email {
		return nil, ErrRegistrationEmailInvalid
	}
	if err := utils.ValidatePassword(userDTO.Password); err != nil {
		return nil, err
	}

	hashedPassword, err := a.hasher.Hash(userDTO.Password)
	if err != nil {
		a.logger.Error("Error generating hashed password for user with email: %s, %v", userDTO.Email, err)
		return nil, errors.New("failed to register user due to internal error")
	}

	user := models.User{
		Email:    email,
		Password: hashedPassword,
		Role:     "operator",
	}

	userCreated, err := a.userService.CreateUser(user)
	if err != nil {
		a.logger.Error("Error creating user: %v", err)
		if errors.Is(err, users.ErrUserAlreadyExists) {
			return nil, ErrRegistrationEmailInUse
		}
		return nil, errors.New("failed to create user")
	}

	a.logger.Info("Successfully registered user: %s", user.Email)
	msg := struct {
		Email string
	}{
		Email: user.Email,
	}
	err = a.sender.Send(config.AuthenticationConfig.AccountCreatedTopic, msg)
	if err != nil {
		a.logger.Error("Error sending account created message: %v", err)
	}

	return &dto.UserResponse{
		ID:    userCreated.ID,
		Email: userCreated.Email,
	}, nil
}

func (a *service) Login(email, password string) (*dto.TokenDetails, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	user, err := a.userService.GetUserByEmail(email)
	if err != nil || user == nil || !user.IsActive {
		a.logger.Error("Error fetching user by email: %v", err)
		return nil, errors.New("invalid credentials")
	}

	now := time.Now()
	if user.IsBlocked && (user.BlockedUntil == nil || now.Before(*user.BlockedUntil)) {
		a.logger.Warn("Login attempt for blocked user: %s", email)
		return nil, errors.New("account is blocked")
	}

	// Check for rapid subsequent login attempts
	if user.LastAttempt != nil && now.Sub(*user.LastAttempt) < config.AuthenticationConfig.MinTimeBetweenAttemptsSeconds*time.Second {
		a.logger.Warn("Rapid subsequent login attempt detected for user: %s", email)
		return nil, errors.New("please wait a moment before trying again")
	}

	hashErr := a.hasher.Compare(user.Password, password)
	if hashErr != nil {
		if a.loginFailureRecorder == nil {
			a.logger.Error("Atomic login failure recorder is unavailable for user %s", email)
			return nil, errors.New("invalid credentials")
		}
		updatedUser, blockedNow, recordErr := a.loginFailureRecorder.RecordLoginFailure(
			user.ID,
			now,
			config.AuthenticationConfig.MaxLoginAttemptsBeforeBlock,
			time.Duration(config.AuthenticationConfig.BaseBlockDurationMinutes)*time.Minute,
		)
		if recordErr != nil || updatedUser == nil {
			a.logger.Error("Failed to atomically record login failure for user %s: %v", email, recordErr)
		} else if blockedNow {
			blockedUntil := time.Time{}
			if updatedUser.BlockedUntil != nil {
				blockedUntil = *updatedUser.BlockedUntil
			}
			a.logger.Warn("User %s is blocked until %s", email, blockedUntil.String())
			msg := struct {
				Email        string
				BlockedUntil time.Time
			}{
				Email:        email,
				BlockedUntil: blockedUntil,
			}
			if sendErr := a.sender.Send(config.AuthenticationConfig.AccountBlockedTopic, msg); sendErr != nil {
				a.logger.Error("Failed to send account blocked message: %v", sendErr)
			}
		}
		a.logger.Warn("Hash comparison failed for user %s: %v", email, hashErr)
		return nil, errors.New("invalid credentials")
	}

	// The repository serializes this narrow reset with failed-attempt lockouts
	// and returns current account state instead of persisting our old snapshot.
	user, updateErr := a.userService.CompleteSuccessfulLogin(*user)
	if updateErr != nil {
		a.logger.Error("Failed to reset failed attempts for user %s: %v", email, updateErr)
		if errors.Is(updateErr, users.ErrConcurrentUserUpdate) {
			return nil, errors.New("account changed during login; please try again")
		}
		return nil, errors.New("failed to update login state")
	}
	if user == nil {
		return nil, errors.New("failed to update login state")
	}

	td, err := a.issueSession(user)
	if err != nil {
		return nil, err
	}

	a.logger.Info("Successfully logged in user: %s", email)

	return td, nil
}

// issueSession mints an access/refresh token pair for an already-authenticated
// user. Shared by password login and Google login so both produce identical
// sessions.
func (a *service) issueSession(user *models.User) (*dto.TokenDetails, error) {
	if user == nil {
		return nil, errors.New("user is unavailable")
	}
	td := &dto.TokenDetails{}
	var err error
	td.RefreshToken, td.RefreshUUID, td.RtExpires, user, err = a.createRefreshSession(user)
	if err != nil {
		return nil, errors.New("failed to generate refresh token")
	}
	td.AccessToken, td.AtExpires, err = a.generateAccessTokenWithVersion(user.ID, userRole(user.Role), td.RefreshUUID, td.RtExpires, user.SessionVersion)
	if err != nil {
		a.revokeFailedIssuance(user, td.RefreshUUID, td.RtExpires)
		return nil, errors.New("failed to generate access token")
	}
	_, claims, err := a.parseAndValidateToken(td.AccessToken)
	if err != nil {
		return nil, err
	}
	if _, err := a.activeUserFromClaims(claims); err != nil {
		a.revokeFailedIssuance(user, td.RefreshUUID, td.RtExpires)
		return nil, err
	}
	return td, nil
}

// GoogleAuthURL returns the Google consent URL for "Sign in with Google".
func (a *service) GoogleAuthURL() (string, error) {
	oauth := a.googleOAuthProvider()
	if !oauth.Configured() {
		if a.logger != nil {
			a.logger.Warn("Google sign-in is unavailable because OAuth configuration is incomplete or invalid")
		}
		return "", errors.New("google login is not configured")
	}
	url, err := oauth.AuthCodeURL()
	if err != nil && a.logger != nil {
		a.logger.Warn("Google sign-in authorization URL generation failed")
	}
	return url, err
}

// LoginWithGoogle completes the Google authorization-code flow, resolves the
// account by verified email (creating it on first sign-in), and issues a session.
func (a *service) LoginWithGoogle(ctx context.Context, code, state string) (*dto.TokenDetails, error) {
	oauth := a.googleOAuthProvider()
	if !oauth.Configured() {
		if a.logger != nil {
			a.logger.Warn("Google sign-in is unavailable because OAuth configuration is incomplete or invalid")
		}
		return nil, errors.New("google login is not configured")
	}
	email, err := oauth.Exchange(ctx, code, state)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("Google sign-in failed during authorization-code exchange or identity verification")
		}
		return nil, err
	}

	user, err := a.userService.GetUserByEmail(email)
	if errors.Is(err, users.ErrUserNotFound) {
		user, err = a.createGoogleUser(email)
		if errors.Is(err, users.ErrUserAlreadyExists) {
			// Another first-time sign-in may have created this identity after the
			// lookup but before the repository's unique-email constraint ran.
			user, err = a.userService.GetUserByEmail(email)
		}
		if err != nil {
			if a.logger != nil {
				a.logger.Error("Failed to provision the Google sign-in account")
			}
			return nil, errors.New("failed to provision account")
		}
		if user == nil {
			if a.logger != nil {
				a.logger.Error("Google account lookup returned no user after provisioning")
			}
			return nil, errors.New("failed to provision account")
		}
		if a.logger != nil {
			a.logger.Info("Provisioned a new user via Google sign-in")
		}
	} else if err != nil || user == nil {
		if a.logger != nil {
			a.logger.Error("Failed to resolve the Google sign-in account")
		}
		return nil, errors.New("failed to resolve account")
	}
	if !user.IsActive || (user.IsBlocked && (user.BlockedUntil == nil || time.Now().Before(*user.BlockedUntil))) {
		if a.logger != nil {
			a.logger.Warn("Unavailable account attempted Google sign-in")
		}
		return nil, errors.New("account is unavailable")
	}

	if a.logger != nil {
		a.logger.Info("Successfully logged in user via Google")
	}
	tokens, err := a.issueSession(user)
	if err != nil && a.logger != nil {
		a.logger.Error("Google sign-in session could not be issued")
	}
	return tokens, err
}

// LocalPreviewLogin issues a normal owner session only when the administrator
// explicitly enabled the local-preview flag and validated its numeric loopback
// gateway bind. The handler additionally requires the actual TCP peer to be
// loopback; proxy headers are not an authentication mechanism.
func (a *service) LocalPreviewLogin() (*dto.TokenDetails, error) {
	if !config.LocalPreviewConfig.LocalPreviewSessionAllowed() {
		return nil, errors.New("local preview is not enabled")
	}
	user, err := a.userService.GetUserByEmail(config.LocalPreviewConfig.OwnerEmail)
	if err != nil || user == nil || user.Role != "owner" || !user.IsActive ||
		(user.IsBlocked && (user.BlockedUntil == nil || time.Now().Before(*user.BlockedUntil))) {
		a.logger.Warn("Local preview owner session was unavailable")
		return nil, errors.New("local preview owner session is unavailable")
	}
	return a.issueSession(user)
}

// createGoogleUser provisions an account for a Google identity. The password is
// a random, unusable value: the account authenticates through Google, never a
// typed password, but the column stays non-empty and non-guessable.
func (a *service) createGoogleUser(email string) (*models.User, error) {
	hashed, err := a.hasher.Hash(uuid.NewString() + uuid.NewString())
	if err != nil {
		return nil, err
	}
	return a.userService.CreateUser(models.User{Email: email, Password: hashed, IsActive: true, Role: "operator"})
}

func (a *service) Logout(accessToken string) error {
	_, claims, err := a.parseAndValidateToken(accessToken)
	if err != nil {
		a.logger.Error("Error parsing access token: %v", err)
		return errors.New("invalid access token")
	}
	if !hasTokenType(claims, tokenTypeAccess) {
		return errors.New("invalid access token")
	}
	userID, ok := claims["user_id"].(string)
	if !ok {
		a.logger.Error("User ID not found in the access token")
		return errors.New("user ID not found in the token")
	}

	accessUUID, ok := claims["access_uuid"].(string)
	if !ok || strings.TrimSpace(accessUUID) == "" {
		a.logger.Warn("Access UUID not found in the token for user: %s", userID)
		return errors.New("access UUID not found in the token")
	}

	refreshUUID, ok := claims["refresh_uuid"].(string)
	if !ok || strings.TrimSpace(refreshUUID) == "" {
		a.logger.Warn("Refresh UUID not found in the token for user: %s", userID)
		return errors.New("refresh UUID not found in the token")
	}

	// Calculates the expiration time of the tokens to define the time they remain on the block list.
	refreshExp, ok := unixClaim(claims, "refresh_exp")
	if !ok || refreshExp <= time.Now().Unix() {
		a.logger.Warn("Refresh expiration time not found in the token for user: %s", userID)
		return errors.New("refresh expiration time not found in the token")
	}
	rtDuration := positiveTokenTTL(refreshExp)
	familyUUID, ok := refreshFamilyFromClaims(claims)
	if !ok {
		return errors.New("refresh family not found in the token")
	}

	atExpires, ok := unixClaim(claims, "exp")
	if !ok || atExpires <= 0 || atExpires > refreshExp {
		a.logger.Warn("Expiration time not found in the token for user: %s", userID)
		return errors.New("expiration time not found in the token")
	}
	atDuration := positiveTokenTTL(atExpires)

	// Attempt both revocations even when one backing-store operation fails.
	// Refresh is blocked first because it can mint new access tokens.
	var revokeErrors []error
	identity, identityErr := sessionIdentityFromClaims(claims)
	if identityErr != nil {
		return identityErr
	}
	if err := a.revokeDurableFamily(identity, uuid.Nil); err != nil {
		revokeErrors = append(revokeErrors, fmt.Errorf("revoke durable refresh family: %w", err))
	}
	if a.blockListService == nil {
		revokeErrors = append(revokeErrors, errors.New("session revocation store is unavailable"))
		return errors.Join(revokeErrors...)
	}
	if familyErr := a.blockListService.RevokeRefreshFamily(familyUUID, rtDuration); familyErr != nil {
		a.logger.Error("Failed to revoke refresh family for user: %s, Error: %v", userID, familyErr)
		revokeErrors = append(revokeErrors, fmt.Errorf("revoke refresh family: %w", familyErr))
	}
	if refreshErr := a.blockListService.AddToBlockList(refreshUUID, rtDuration); refreshErr != nil {
		a.logger.Error("Failed to add refresh token to block list for user: %s, Error: %v", userID, refreshErr)
		revokeErrors = append(revokeErrors, fmt.Errorf("revoke refresh token: %w", refreshErr))
	}
	if accessErr := a.blockListService.AddToBlockList(accessUUID, atDuration); accessErr != nil {
		a.logger.Error("Failed to add access token to block list for user: %s, Error: %v", userID, accessErr)
		revokeErrors = append(revokeErrors, fmt.Errorf("revoke access token: %w", accessErr))
	}
	if len(revokeErrors) > 0 {
		return errors.Join(revokeErrors...)
	}

	a.logger.Info("Successfully logged out and blocked tokens for user: %s with accessUUID: %s and refreshUUID: %s", userID, accessUUID, refreshUUID)
	return nil
}

func (a *service) RefreshToken(refreshToken string) (*dto.TokenDetails, error) {
	if a.blockListService == nil {
		return nil, errors.New("session revocation store is unavailable")
	}
	_, claims, err := a.parseAndValidateToken(refreshToken)
	if err != nil {
		a.logger.Error("Error parsing refresh token: %v", err)
		return nil, errors.New("invalid refresh token")
	}
	if !hasTokenType(claims, tokenTypeRefresh) {
		return nil, errors.New("invalid refresh token")
	}

	refreshUUID, ok := claims["refresh_uuid"].(string)
	if !ok || strings.TrimSpace(refreshUUID) == "" {
		a.logger.Warn("Refresh UUID not found in the token")
		return nil, errors.New("refresh UUID not found in the token")
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		a.logger.Warn("User ID not found in the refresh token")
		return nil, errors.New("user ID not found in the token")
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		a.logger.Error("Error parsing user ID from claims: %v", err)
		return nil, err
	}

	refreshExp, ok := unixClaim(claims, "exp")
	if !ok {
		a.logger.Warn("Refresh expiration time not found in the token for user: %s", userID)
		return nil, errors.New("refresh expiration time not found in the token")
	}
	if refreshExp <= time.Now().Unix() {
		return nil, ErrRevokedSession
	}
	familyUUID, ok := refreshFamilyFromClaims(claims)
	if !ok {
		return nil, errors.New("refresh family not found in the token")
	}
	user, err := a.activeUserFromClaims(claims)
	if err != nil {
		a.logger.Warn("User is unavailable while refreshing an access token: %s", userID)
		return nil, err
	}
	newRefreshToken, newRefreshUUID, err := a.generateRefreshTokenInFamilyWithVersion(user.ID, familyUUID, refreshExp, user.SessionVersion)
	if err != nil {
		a.logger.Error("Failed to generate replacement refresh token: %v", err)
		return nil, err
	}
	newAccessToken, atExpires, err := a.generateAccessTokenForFamilyWithVersion(user.ID, userRole(user.Role), newRefreshUUID, familyUUID, refreshExp, user.SessionVersion)
	if err != nil {
		a.logger.Error("Failed to generate new access token: %v", err)
		return nil, err
	}
	issued := &dto.TokenDetails{
		AccessToken:  newAccessToken,
		AtExpires:    atExpires,
		RefreshToken: newRefreshToken,
		RefreshUUID:  newRefreshUUID,
		RtExpires:    refreshExp,
	}
	payload, err := a.encryptRefreshRotationPayload(issued, refreshUUID)
	if err != nil {
		return nil, err
	}
	remaining := time.Until(time.Unix(refreshExp, 0))
	if remaining <= 0 {
		return nil, ErrRevokedSession
	}
	if a.sessionRepository == nil {
		return nil, irepository.ErrSessionAuthorityUnavailable
	}
	identity, err := sessionIdentityFromClaims(claims)
	if err != nil {
		return nil, err
	}
	replacementID, err := uuid.Parse(newRefreshUUID)
	if err != nil {
		return nil, ErrRevokedSession
	}
	ctx, cancel := sessionAuthorityContext()
	decision, err := a.sessionRepository.Rotate(ctx, identity, replacementID, payload, refreshRotationReplayGrace)
	cancel()
	if err != nil {
		return nil, sessionAuthorityError(err)
	}
	if decision == nil {
		return nil, irepository.ErrSessionAuthorityUnavailable
	}
	if decision.Revoked {
		// SQL revocation has committed before this application-level denial.
		if err := a.blockListService.RevokeRefreshFamily(familyUUID, remaining); err != nil {
			return nil, errors.New("refresh service unavailable")
		}
		return nil, ErrRevokedSession
	}
	if decision.Receipt == nil {
		return nil, ErrRevokedSession
	}
	receipt := decision.Receipt
	if receipt.PredecessorUUID != identity.RefreshUUID || receipt.FamilyID != identity.FamilyID {
		return nil, ErrRevokedSession
	}
	canonical, err := a.decryptRefreshRotationPayload(receipt.EncryptedPair, refreshUUID)
	if err != nil || canonical.RefreshUUID != receipt.ReplacementUUID.String() {
		return nil, ErrRevokedSession
	}
	graceRemaining := time.Until(receipt.ReplayUntil)
	if graceRemaining <= 0 {
		return nil, ErrRevokedSession
	}
	rotatedPayload, _, err := a.blockListService.RotateRefreshToken(
		refreshUUID,
		familyUUID,
		receipt.ReplacementUUID.String(),
		remaining,
		graceRemaining,
		receipt.EncryptedPair,
	)
	if err != nil {
		a.logger.Error("Failed to rotate refresh token: %v", err)
		return nil, errors.New("refresh service unavailable")
	}
	if rotatedPayload == "" {
		if err := a.revokeDurableFamily(identity, receipt.ReplacementUUID); err != nil {
			return nil, err
		}
		return nil, ErrRevokedSession
	}
	if rotatedPayload != receipt.EncryptedPair {
		return nil, ErrRevokedSession
	}
	result, err := a.decryptRefreshRotationPayload(rotatedPayload, refreshUUID)
	if err != nil {
		a.logger.Error("Failed to decode refresh rotation result: %v", err)
		return nil, errors.New("refresh service unavailable")
	}
	if err := a.validateRefreshRotationResult(result, claims); err != nil {
		return nil, err
	}

	td := &dto.TokenDetails{
		AccessToken:  result.AccessToken,
		AtExpires:    result.AtExpires,
		RefreshToken: result.RefreshToken,
		RefreshUUID:  result.RefreshUUID,
		RtExpires:    result.RtExpires,
	}

	a.logger.Info("Successfully renewed access token for user: %s", userID.String())

	return td, nil
}

func (a *service) IsUserAuthenticated(accessToken string) (bool, error) {
	_, claims, err := a.parseAndValidateToken(accessToken)
	if err != nil {
		a.logger.Error("Error parsing accessToken: %v", err)
		return false, err
	}
	if !hasTokenType(claims, tokenTypeAccess) {
		return false, errors.New("invalid accessToken")
	}
	if _, err := a.activeUserFromClaims(claims); err != nil {
		return false, err
	}

	return true, nil
}

func (a *service) RequestPasswordReset(email string) (string, time.Time, error) {
	if a.passwordResetter == nil || !a.passwordResetter.Configured() {
		a.logger.Warn("Password reset requested while email delivery is not configured")
		return "", time.Time{}, errors.New("password reset email delivery is not configured")
	}
	email = strings.TrimSpace(strings.ToLower(email))
	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || parsedEmail.Address != email {
		return "", time.Time{}, errors.New("invalid email")
	}

	user, err := a.userService.GetUserByEmail(email)
	if err != nil {
		a.logger.Error("Password-reset account lookup failed")
		return "", time.Time{}, errors.New("invalid email")
	}

	resetToken, resetTokenDigest, err := utils.NewPasswordResetToken()
	if err != nil {
		a.logger.Error("Error generating password reset token")
		return "", time.Time{}, errors.New("failed to generate password reset token")
	}
	resetTokenExpires := time.Now().UTC().Add(time.Hour * config.AuthenticationConfig.ExpirationTimeResetTokenHours)

	// Persist only a one-way digest; this narrow update cannot overwrite
	// concurrent login, password-change, or session-version state.
	if err := a.userService.StorePasswordResetToken(user.ID, user.SessionVersion, resetTokenDigest, resetTokenExpires); err != nil {
		a.logger.Error("Error storing password reset token")
		return "", time.Time{}, errors.New("failed to update user")
	}

	if err := a.passwordResetter.SendPasswordReset(email, resetToken, resetTokenExpires); err != nil {
		// SMTP errors can echo recipient data, server responses, or credentials.
		if stage, ok := services.PasswordResetDeliveryFailureStage(err); ok {
			a.logger.Error("Password reset email delivery failed during SMTP stage %s", stage)
		} else {
			a.logger.Error("Password reset email delivery failed")
		}
		if rollbackErr := a.userService.ClearPasswordResetToken(user.ID, resetToken); rollbackErr != nil {
			a.logger.Error("Failed to remove undelivered reset token")
		}
		return "", time.Time{}, errors.New("failed to send password reset email")
	}

	a.logger.Info("Successfully sent password-reset email")
	return resetToken, resetTokenExpires, nil
}

func (a *service) ConfirmPasswordReset(token, newPassword string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidPasswordReset
	}
	if err := utils.ValidatePassword(newPassword); err != nil {
		return err
	}

	user, err := a.userService.GetUserByResetToken(token)
	if err != nil {
		if errors.Is(err, users.ErrUserNotFound) {
			return ErrInvalidPasswordReset
		}
		a.logger.Error("Error fetching user by reset token")
		return ErrPasswordResetUnavailable
	}

	if user == nil {
		return ErrInvalidPasswordReset
	}

	if user.ResetTokenExpires == nil {
		return ErrInvalidPasswordReset
	}

	if !user.ResetTokenExpires.After(time.Now()) {
		return ErrInvalidPasswordReset
	}

	err = a.userService.ConsumePasswordResetToken(user.ID, token, newPassword)
	if errors.Is(err, users.ErrInvalidPasswordResetToken) {
		return ErrInvalidPasswordReset
	}
	if err != nil {
		a.logger.Error("Error consuming password reset token")
		return ErrPasswordResetUnavailable
	}

	return nil
}

func (a *service) ChangePassword(accessToken, currentPassword, newPassword string) error {
	if newPassword == "" {
		return errors.New("new password is required")
	}
	if currentPassword == "" {
		return users.ErrCurrentPasswordRequired
	}
	if err := utils.ValidatePassword(newPassword); err != nil {
		return err
	}

	_, claims, err := a.parseAndValidateToken(accessToken)
	if err != nil {
		a.logger.Error("Error parsing accessToken: %v", err)
		return errors.New("invalid accessToken")
	}
	if !hasTokenType(claims, tokenTypeAccess) {
		return errors.New("invalid accessToken")
	}
	user, err := a.activeUserFromClaims(claims)
	if err != nil {
		if errors.Is(err, ErrRevokedSession) {
			return ErrRevokedSession
		}
		a.logger.Error("Error resolving authenticated user for password change")
		return errors.New("invalid session")
	}

	updateErr := a.userService.UpdatePasswordWithCurrentPassword(user.ID, user.SessionVersion, currentPassword, newPassword)
	if updateErr != nil {
		if errors.Is(updateErr, users.ErrConcurrentUserUpdate) {
			return ErrRevokedSession
		}
		if errors.Is(updateErr, users.ErrCurrentPasswordRequired) || errors.Is(updateErr, users.ErrInvalidCurrentPassword) ||
			errors.Is(updateErr, utils.ErrPasswordTooShort) || errors.Is(updateErr, utils.ErrPasswordTooLong) {
			return updateErr
		}
		a.logger.Error("Error updating user password")
		return errors.New("failed to update password")
	}

	a.logger.Info("Successfully changed password")
	return nil
}

func (a *service) GetIdFromToken(accessToken string) (uuid.UUID, error) {
	_, claims, err := a.parseAndValidateToken(accessToken)
	if err != nil {
		a.logger.Error("Error parsing accessToken: %v", err)
		return uuid.UUID{}, errors.New("invalid accessToken")
	}
	if !hasTokenType(claims, tokenTypeAccess) {
		return uuid.UUID{}, errors.New("invalid accessToken")
	}
	user, err := a.activeUserFromClaims(claims)
	if err != nil {
		return uuid.UUID{}, err
	}
	return user.ID, nil
}

func (a *service) GetSessionFromToken(accessToken string) (*dto.AuthSession, error) {
	_, claims, err := a.parseAndValidateToken(accessToken)
	if err != nil {
		return nil, errors.New("invalid accessToken")
	}
	if !hasTokenType(claims, tokenTypeAccess) {
		return nil, errors.New("invalid accessToken")
	}

	user, err := a.activeUserFromClaims(claims)
	if err != nil {
		return nil, err
	}
	role := userRole(user.Role)
	permissions := dto.AuthSessionPermissions{CanRead: true}
	switch role {
	case "owner":
		permissions.CanOperate = true
		permissions.CanApprove = true
		permissions.CanAdminister = true
	case "operator":
		permissions.CanOperate = true
	case "viewer":
	}

	return &dto.AuthSession{
		Authenticated:  true,
		Subject:        user.ID.String(),
		Role:           role,
		Permissions:    permissions,
		SessionVersion: user.SessionVersion,
	}, nil
}

func (a *service) generateAccessToken(userID uuid.UUID, role, refreshUUID string, refreshExp int64) (string, int64, error) {
	return a.generateAccessTokenWithVersion(userID, role, refreshUUID, refreshExp, 0)
}

func (a *service) generateAccessTokenWithVersion(userID uuid.UUID, role, refreshUUID string, refreshExp int64, sessionVersion int64) (string, int64, error) {
	return a.generateAccessTokenForFamilyWithVersion(userID, role, refreshUUID, refreshUUID, refreshExp, sessionVersion)
}

func (a *service) generateAccessTokenForFamilyWithVersion(userID uuid.UUID, role, refreshUUID, familyUUID string, refreshExp int64, sessionVersion int64) (string, int64, error) {
	now := time.Now()
	if refreshExp <= now.Unix() || strings.TrimSpace(refreshUUID) == "" || strings.TrimSpace(familyUUID) == "" {
		return "", 0, errors.New("refresh session is expired")
	}
	expires := now.Add(time.Minute * config.AuthenticationConfig.AccessTokenDurationMinutes).Unix()
	if expires > refreshExp {
		expires = refreshExp
	}
	if expires <= now.Unix() {
		return "", 0, errors.New("access token lifetime is not positive")
	}

	claims := jwt.MapClaims{}
	claims["iss"] = tokenIssuer
	claims["aud"] = tokenAudience
	claims["iat"] = now.Unix()
	claims["user_id"] = userID.String()
	claims["role"] = userRole(role)
	claims["token_type"] = tokenTypeAccess
	claims["access_uuid"] = uuid.New().String()
	claims["refresh_uuid"] = refreshUUID
	claims["family_uuid"] = familyUUID
	claims["refresh_exp"] = refreshExp
	claims["session_version"] = sessionVersion
	claims["exp"] = expires

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString([]byte(a.jwtSecret))
	return accessToken, expires, err
}

func userRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "operator", "viewer":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return "viewer"
	}
}

func (a *service) generateRefreshToken(userID uuid.UUID) (string, string, int64, error) {
	return a.generateRefreshTokenWithVersion(userID, 0)
}

func (a *service) generateRefreshTokenWithVersion(userID uuid.UUID, sessionVersion int64) (string, string, int64, error) {
	if a.userService == nil {
		return "", "", 0, errors.New("user is unavailable")
	}
	user, err := a.activeUser(userID)
	if err != nil {
		return "", "", 0, err
	}
	if user.SessionVersion != sessionVersion {
		return "", "", 0, ErrRevokedSession
	}
	token, id, expires, _, err := a.createRefreshSession(user)
	return token, id, expires, err
}

func (a *service) createRefreshSession(user *models.User) (string, string, int64, *models.User, error) {
	if user == nil {
		return "", "", 0, nil, errors.New("user is unavailable")
	}
	if a.sessionRepository == nil {
		return "", "", 0, nil, irepository.ErrSessionAuthorityUnavailable
	}
	if a.blockListService == nil {
		return "", "", 0, nil, errors.New("session registration store is unavailable")
	}
	refreshUUID := uuid.New().String()
	now := time.Now()
	expires := now.Add(config.AuthenticationConfig.RefreshTokenDurationDays).Unix()
	refreshToken, err := a.generateRefreshTokenWithIdentity(user.ID, refreshUUID, refreshUUID, expires, user.SessionVersion)
	if err != nil {
		return "", "", 0, nil, err
	}
	remaining := time.Until(time.Unix(expires, 0))
	if remaining <= 0 {
		return "", "", 0, nil, ErrRevokedSession
	}
	identity := irepository.SessionIdentity{UserID: user.ID, FamilyID: uuid.MustParse(refreshUUID), RefreshUUID: uuid.MustParse(refreshUUID), SessionVersion: user.SessionVersion, ExpiresAt: time.Unix(expires, 0).UTC()}
	ctx, cancel := sessionAuthorityContext()
	current, err := a.sessionRepository.CreateFamily(ctx, *user, identity)
	cancel()
	if err != nil {
		return "", "", 0, nil, sessionAuthorityError(err)
	}
	if current == nil {
		return "", "", 0, nil, irepository.ErrSessionAuthorityUnavailable
	}
	if err := a.blockListService.RegisterRefreshSession(refreshUUID, refreshUUID, remaining); err != nil {
		a.revokeFailedIssuance(current, refreshUUID, expires)
		return "", "", 0, nil, errors.New("session registration unavailable")
	}
	ctx, cancel = sessionAuthorityContext()
	current, err = a.sessionRepository.Validate(ctx, identity)
	cancel()
	if err != nil || current == nil {
		a.revokeFailedIssuance(user, refreshUUID, expires)
		if err != nil {
			return "", "", 0, nil, sessionAuthorityError(err)
		}
		return "", "", 0, nil, irepository.ErrSessionAuthorityUnavailable
	}
	return refreshToken, refreshUUID, expires, current, nil
}

func (a *service) generateRefreshTokenInFamilyWithVersion(userID uuid.UUID, familyUUID string, expires int64, sessionVersion int64) (string, string, error) {
	refreshUUID := uuid.New().String()
	refreshToken, err := a.generateRefreshTokenWithIdentity(userID, refreshUUID, familyUUID, expires, sessionVersion)
	return refreshToken, refreshUUID, err
}

func (a *service) generateRefreshTokenWithIdentity(userID uuid.UUID, refreshUUID, familyUUID string, expires, sessionVersion int64) (string, error) {
	now := time.Now()
	if strings.TrimSpace(refreshUUID) == "" || strings.TrimSpace(familyUUID) == "" || expires <= now.Unix() {
		return "", errors.New("refresh session is expired")
	}
	claims := jwt.MapClaims{}
	claims["iss"] = tokenIssuer
	claims["aud"] = tokenAudience
	claims["iat"] = now.Unix()
	claims["refresh_uuid"] = refreshUUID
	claims["family_uuid"] = familyUUID
	claims["user_id"] = userID.String()
	claims["token_type"] = tokenTypeRefresh
	claims["session_version"] = sessionVersion
	claims["exp"] = expires

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	refreshToken, err := token.SignedString([]byte(a.jwtSecret))
	return refreshToken, err
}

func (a *service) parseAndValidateToken(tokenString string) (*jwt.Token, jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			a.logger.Error("Unexpected signing method: %v", token.Header["alg"])
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(a.jwtSecret), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
	)

	if err != nil {
		return nil, nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, nil, errors.New("invalid token")
	}

	return token, claims, nil
}

func (a *service) activeUserFromClaims(claims jwt.MapClaims) (*models.User, error) {
	if hasTokenType(claims, tokenTypeAccess) {
		accessExp, validAccessExp := unixClaim(claims, "exp")
		refreshExp, ok := unixClaim(claims, "refresh_exp")
		if !validAccessExp || !ok || accessExp <= time.Now().Unix() || refreshExp <= time.Now().Unix() || accessExp > refreshExp {
			return nil, ErrRevokedSession
		}
		refreshUUID, ok := claims["refresh_uuid"].(string)
		if !ok || strings.TrimSpace(refreshUUID) == "" {
			return nil, ErrRevokedSession
		}
		refreshBlocked, err := a.isTokenBlocked(refreshUUID)
		if err != nil {
			a.logger.Error("Failed to verify refresh session revocation: %v", err)
			return nil, errors.New("failed to verify session")
		}
		if refreshBlocked {
			return nil, ErrRevokedSession
		}
		familyUUID, ok := refreshFamilyFromClaims(claims)
		if !ok {
			return nil, ErrRevokedSession
		}
		familyRevoked, err := a.blockListService.IsRefreshFamilyRevoked(familyUUID)
		if err != nil {
			a.logger.Error("Failed to verify refresh family revocation: %v", err)
			return nil, errors.New("failed to verify session")
		}
		if familyRevoked {
			return nil, ErrRevokedSession
		}
		active, err := a.blockListService.IsRefreshSessionActive(refreshUUID, familyUUID)
		if err != nil {
			a.logger.Error("Failed to verify active refresh session: %v", err)
			return nil, errors.New("failed to verify session")
		}
		if !active {
			return nil, ErrRevokedSession
		}
		accessUUID, ok := claims["access_uuid"].(string)
		if !ok || strings.TrimSpace(accessUUID) == "" {
			return nil, ErrRevokedSession
		}
		accessBlocked, err := a.isTokenBlocked(accessUUID)
		if err != nil {
			a.logger.Error("Failed to verify access token revocation: %v", err)
			return nil, errors.New("failed to verify session")
		}
		if accessBlocked {
			return nil, ErrRevokedSession
		}
	}

	userIDValue, ok := claims["user_id"].(string)
	if !ok {
		return nil, errors.New("invalid accessToken")
	}
	userID, err := uuid.Parse(userIDValue)
	if err != nil {
		return nil, errors.New("invalid accessToken")
	}
	version, err := sessionVersionFromClaims(claims)
	if err != nil {
		return nil, ErrRevokedSession
	}
	if hasTokenType(claims, tokenTypeAccess) {
		if a.sessionRepository == nil {
			return nil, irepository.ErrSessionAuthorityUnavailable
		}
		identity, err := sessionIdentityFromClaims(claims)
		if err != nil {
			return nil, err
		}
		ctx, cancel := sessionAuthorityContext()
		defer cancel()
		current, err := a.sessionRepository.Validate(ctx, identity)
		if err != nil {
			return nil, sessionAuthorityError(err)
		}
		if current == nil {
			return nil, irepository.ErrSessionAuthorityUnavailable
		}
		if !accessRoleMatchesUser(claims, current) {
			return nil, ErrRevokedSession
		}
		return current, nil
	}
	user, err := a.activeUser(userID)
	if err != nil {
		return nil, err
	}
	if version != user.SessionVersion {
		return nil, ErrRevokedSession
	}
	return user, nil
}

func sessionAuthorityContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func accessRoleMatchesUser(claims jwt.MapClaims, user *models.User) bool {
	role, ok := claims["role"].(string)
	return ok && user != nil && role == userRole(user.Role)
}

func sessionAuthorityError(err error) error {
	if errors.Is(err, irepository.ErrSessionRevoked) {
		return ErrRevokedSession
	}
	if errors.Is(err, irepository.ErrSessionUserUnavailable) {
		return errors.New("user is unavailable")
	}
	return irepository.ErrSessionAuthorityUnavailable
}

func (a *service) revokeDurableFamily(id irepository.SessionIdentity, onlyCurrent uuid.UUID) error {
	if a.sessionRepository == nil {
		return irepository.ErrSessionAuthorityUnavailable
	}
	ctx, cancel := sessionAuthorityContext()
	defer cancel()
	var err error
	if onlyCurrent == uuid.Nil {
		err = a.sessionRepository.RevokeFamily(ctx, id)
	} else {
		err = a.sessionRepository.RevokeFamilyIfCurrent(ctx, id, onlyCurrent)
	}
	if err != nil {
		return sessionAuthorityError(err)
	}
	return nil
}

func (a *service) revokeFailedIssuance(user *models.User, refreshID string, expires int64) {
	if user == nil {
		return
	}
	id, err := uuid.Parse(refreshID)
	if err != nil {
		return
	}
	identity := irepository.SessionIdentity{UserID: user.ID, FamilyID: id, RefreshUUID: id, SessionVersion: user.SessionVersion, ExpiresAt: time.Unix(expires, 0).UTC()}
	if err := a.revokeDurableFamily(identity, uuid.Nil); err != nil && a.logger != nil {
		a.logger.Error("Failed to revoke undelivered durable session")
	}
}

func (a *service) isTokenBlocked(tokenID string) (bool, error) {
	if a.blockListService == nil {
		return false, errors.New("session revocation store is unavailable")
	}
	return a.blockListService.IsInBlockList(tokenID)
}

func sessionVersionFromClaims(claims jwt.MapClaims) (int64, error) {
	value, exists := claims["session_version"]
	if !exists {
		return 0, nil
	}
	switch version := value.(type) {
	case float64:
		if version < 0 || version != math.Trunc(version) || version >= float64(1<<63) {
			return 0, errors.New("invalid session version")
		}
		return int64(version), nil
	case int64:
		if version < 0 {
			return 0, errors.New("invalid session version")
		}
		return version, nil
	case int:
		if version < 0 {
			return 0, errors.New("invalid session version")
		}
		return int64(version), nil
	default:
		return 0, errors.New("invalid session version")
	}
}

func (a *service) activeUser(userID uuid.UUID) (*models.User, error) {
	user, err := a.userService.GetUserByID(userID)
	if err != nil || user == nil || !user.IsActive {
		return nil, errors.New("user is unavailable")
	}
	if user.IsBlocked && (user.BlockedUntil == nil || time.Now().Before(*user.BlockedUntil)) {
		return nil, errors.New("user is unavailable")
	}
	return user, nil
}

func hasTokenType(claims jwt.MapClaims, expected string) bool {
	tokenType, ok := claims["token_type"].(string)
	return ok && tokenType == expected
}

func unixClaim(claims jwt.MapClaims, key string) (int64, bool) {
	value, ok := claims[key]
	if !ok {
		return 0, false
	}

	switch typedValue := value.(type) {
	case float64:
		if math.IsNaN(typedValue) || math.IsInf(typedValue, 0) || math.Trunc(typedValue) != typedValue ||
			typedValue < 0 || typedValue >= float64(1<<63) {
			return 0, false
		}
		return int64(typedValue), true
	case int64:
		if typedValue < 0 {
			return 0, false
		}
		return typedValue, true
	case int:
		if typedValue < 0 {
			return 0, false
		}
		return int64(typedValue), true
	default:
		return 0, false
	}
}
