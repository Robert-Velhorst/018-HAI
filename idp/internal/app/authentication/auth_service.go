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
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"math"
	"net/mail"
	"strings"
	"time"
)

var (
	minimumPasswordLength       = 12
	ErrRegistrationEmailInvalid = errors.New("enter a valid email address")
	ErrRegistrationPasswordWeak = errors.New("password must contain at least 12 characters")
	ErrRegistrationEmailInUse   = errors.New("an account with this email already exists")
)

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
	tokenIssuer      = "hai-idp"
	tokenAudience    = "hai"
)

type service struct {
	userService      users.UserService
	hasher           utils.PasswordHasher
	blockListService iservice.TokenBlockListService
	logger           iservice.Logger
	sender           iservice.MessageSender
	passwordResetter iservice.PasswordResetSender
	jwtSecret        string
}

func NewService(userService users.UserService, hasher utils.PasswordHasher, sender iservice.MessageSender,
	blockListService iservice.TokenBlockListService, logger iservice.Logger, jwtSecret string,
	passwordResetter ...iservice.PasswordResetSender) IService {
	var resetter iservice.PasswordResetSender
	if len(passwordResetter) > 0 {
		resetter = passwordResetter[0]
	}
	return &service{
		userService:      userService,
		hasher:           hasher,
		blockListService: blockListService,
		logger:           logger,
		sender:           sender,
		passwordResetter: resetter,
		jwtSecret:        jwtSecret,
	}
}

func GetDefaultAuthService() (IService, error) {
	logger, err := services.NewKafkaLogger(config.KafkaConfig.BrokersAddr, config.KafkaConfig.LoggerTopic)
	if err != nil {
		return nil, err
	}
	userService, err := users.GetDefaultUserService()
	if err != nil {
		return nil, err
	}
	hasher := config.AuthenticationConfig.PasswordHasher
	sender, err := services.NewKafkaMessageSender()
	if err != nil {
		return nil, err
	}
	blockListService := services.NewRedisTokenBlockListService()
	passwordResetter := services.NewSMTPPasswordResetSender(
		config.MailConfig.Host,
		config.MailConfig.Port,
		config.MailConfig.Username,
		config.MailConfig.Password,
		config.MailConfig.From,
		config.MailConfig.RequireStartTLS,
		config.MailConfig.DeliveryTimeout,
	)
	return NewService(userService, hasher, sender, blockListService, logger, config.AuthenticationConfig.JwtSecret, passwordResetter), nil
}

func (a *service) Capabilities() dto.AuthCapabilities {
	googleOAuth := newGoogleOAuth(a.jwtSecret)
	return dto.AuthCapabilities{
		GoogleLoginEnabled:           googleOAuth.Configured(),
		PasswordRecoveryEmailEnabled: a.passwordResetter != nil && a.passwordResetter.Configured(),
		LocalPreviewEnabled:          config.LocalPreviewConfig != nil && config.LocalPreviewConfig.Enabled && config.LocalPreviewConfig.OwnerEmail != "",
	}
}

func (a *service) Register(userDTO dto.UserDTO) (*dto.UserResponse, error) {
	email := strings.TrimSpace(strings.ToLower(userDTO.Email))
	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || parsedEmail.Address != email {
		return nil, ErrRegistrationEmailInvalid
	}
	if len(userDTO.Password) < minimumPasswordLength {
		return nil, ErrRegistrationPasswordWeak
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

	if user.IsBlocked && user.BlockedUntil != nil && !now.Before(*user.BlockedUntil) {
		user.IsBlocked = false
		user.FailedAttempts = 0
		user.BlockedUntil = nil
		user, err = a.userService.UpdateUser(*user)
		if err != nil {
			return nil, errors.New("failed to unblock account")
		}
	}

	hashErr := a.hasher.Compare(user.Password, password)
	if hashErr != nil {
		user.FailedAttempts++
		user.LastAttempt = &now
		if user.FailedAttempts >= config.AuthenticationConfig.MaxLoginAttemptsBeforeBlock {
			blockDuration := calculateBlockDuration(user.FailedAttempts)
			blockedUntil := now.Add(blockDuration)
			user.BlockedUntil = &blockedUntil
			user.IsBlocked = true
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
		_, updateErr := a.userService.UpdateUser(*user)
		if updateErr != nil {
			a.logger.Error("Failed to update user after failed login: %v", updateErr)
		}
		a.logger.Warn("Hash comparison failed for user %s: %v", email, hashErr)
		return nil, errors.New("invalid credentials")
	}

	// Successful authentication must not trigger the failed-attempt cooldown.
	// Otherwise a valid second browser/session is rejected for the configured
	// interval even though no brute-force signal exists.
	user.FailedAttempts = 0
	user.LastAttempt = nil
	_, updateErr := a.userService.UpdateUser(*user)
	if updateErr != nil {
		a.logger.Error("Failed to reset failed attempts for user %s: %v", email, updateErr)
	}

	td := &dto.TokenDetails{}
	td.RefreshToken, td.RefreshUUID, td.RtExpires, err = a.generateRefreshToken(user.ID)
	if err != nil {
		a.logger.Error("Failed to generate refresh token for user %s: %v", email, err)
		return nil, errors.New("failed to generate refresh token")
	}
	td.AccessToken, td.AtExpires, err = a.generateAccessToken(user.ID, userRole(user.Role), td.RefreshUUID, td.RtExpires)
	if err != nil {
		a.logger.Error("Failed to generate access token for user %s: %v", email, err)
		return nil, errors.New("failed to generate access token")
	}

	a.logger.Info("Successfully logged in user: %s", email)

	return td, nil
}

// issueSession mints an access/refresh token pair for an already-authenticated
// user. Shared by password login and Google login so both produce identical
// sessions.
func (a *service) issueSession(userID uuid.UUID, role string) (*dto.TokenDetails, error) {
	td := &dto.TokenDetails{}
	var err error
	td.RefreshToken, td.RefreshUUID, td.RtExpires, err = a.generateRefreshToken(userID)
	if err != nil {
		return nil, errors.New("failed to generate refresh token")
	}
	td.AccessToken, td.AtExpires, err = a.generateAccessToken(userID, userRole(role), td.RefreshUUID, td.RtExpires)
	if err != nil {
		return nil, errors.New("failed to generate access token")
	}
	return td, nil
}

// GoogleAuthURL returns the Google consent URL for "Sign in with Google".
func (a *service) GoogleAuthURL() (string, error) {
	oauth := newGoogleOAuth(a.jwtSecret)
	if !oauth.Configured() {
		return "", errors.New("google login is not configured")
	}
	return oauth.AuthCodeURL()
}

// LoginWithGoogle completes the Google authorization-code flow, resolves the
// account by verified email (creating it on first sign-in), and issues a session.
func (a *service) LoginWithGoogle(ctx context.Context, code, state string) (*dto.TokenDetails, error) {
	oauth := newGoogleOAuth(a.jwtSecret)
	if !oauth.Configured() {
		return nil, errors.New("google login is not configured")
	}
	email, err := oauth.Exchange(ctx, code, state)
	if err != nil {
		return nil, err
	}

	user, err := a.userService.GetUserByEmail(email)
	if errors.Is(err, users.ErrUserNotFound) {
		user, err = a.createGoogleUser(email)
		if err != nil {
			a.logger.Error("Failed to provision Google user %s: %v", email, err)
			return nil, errors.New("failed to provision account")
		}
		a.logger.Info("Provisioned new user via Google sign-in: %s", email)
	} else if err != nil || user == nil {
		a.logger.Error("Failed to resolve Google user %s: %v", email, err)
		return nil, errors.New("failed to resolve account")
	}
	if !user.IsActive || (user.IsBlocked && (user.BlockedUntil == nil || time.Now().Before(*user.BlockedUntil))) {
		a.logger.Warn("Unavailable user attempted Google sign-in: %s", email)
		return nil, errors.New("account is unavailable")
	}

	a.logger.Info("Successfully logged in user via Google: %s", email)
	return a.issueSession(user.ID, user.Role)
}

// LocalPreviewLogin issues a normal owner session only when the administrator
// explicitly enabled the local-preview flag. IDP startup rejects that flag
// unless GATEWAY_HOST_BIND is an explicit loopback address. The handler also
// denies non-loopback Host values as defense in depth, but Host is not trusted
// as the boundary that enables this mode.
func (a *service) LocalPreviewLogin() (*dto.TokenDetails, error) {
	if config.LocalPreviewConfig == nil || !config.LocalPreviewConfig.Enabled || config.LocalPreviewConfig.OwnerEmail == "" {
		return nil, errors.New("local preview is not enabled")
	}
	user, err := a.userService.GetUserByEmail(config.LocalPreviewConfig.OwnerEmail)
	if err != nil || user == nil || user.Role != "owner" || !user.IsActive ||
		(user.IsBlocked && (user.BlockedUntil == nil || time.Now().Before(*user.BlockedUntil))) {
		a.logger.Warn("Local preview owner session was unavailable")
		return nil, errors.New("local preview owner session is unavailable")
	}
	return a.issueSession(user.ID, user.Role)
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

func calculateBlockDuration(failedLoginAttempts int) time.Duration {
	exponent := float64(failedLoginAttempts - config.AuthenticationConfig.MaxLoginAttemptsBeforeBlock)
	initialBlockDuration := time.Duration(config.AuthenticationConfig.BaseBlockDurationMinutes) * time.Minute
	return initialBlockDuration * time.Duration(math.Pow(2, exponent))
}

func (a *service) Logout(ctx context.Context, accessToken string) error {
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
	if !ok {
		a.logger.Warn("Access UUID not found in the token for user: %s", userID)
		return errors.New("access UUID not found in the token")
	}

	refreshUUID, ok := claims["refresh_uuid"].(string)
	if !ok {
		a.logger.Warn("Refresh UUID not found in the token for user: %s", userID)
		return errors.New("refresh UUID not found in the token")
	}

	// Calculates the expiration time of the tokens to define the time they remain on the block list.
	refreshExpFloat, ok := claims["refresh_exp"].(float64)
	if !ok {
		a.logger.Warn("Refresh expiration time not found in the token for user: %s", userID)
		return errors.New("refresh expiration time not found in the token")
	}
	refreshExp := int64(refreshExpFloat)
	rtDuration := time.Until(time.Unix(refreshExp, 0))

	atExpiresFloat, ok := claims["exp"].(float64)
	if !ok {
		a.logger.Warn("Expiration time not found in the token for user: %s", userID)
		return errors.New("expiration time not found in the token")
	}
	atExpires := int64(atExpiresFloat)
	atDuration := time.Until(time.Unix(atExpires, 0))

	// Attempt both revocations even when one backing-store operation fails.
	// Refresh is blocked first because it can mint new access tokens.
	var revokeErrors []error
	if refreshErr := a.blockListService.AddToBlockList(ctx, refreshUUID, rtDuration); refreshErr != nil {
		a.logger.Error("Failed to add refresh token to block list for user: %s, Error: %v", userID, refreshErr)
		revokeErrors = append(revokeErrors, fmt.Errorf("revoke refresh token: %w", refreshErr))
	}
	if accessErr := a.blockListService.AddToBlockList(ctx, accessUUID, atDuration); accessErr != nil {
		a.logger.Error("Failed to add access token to block list for user: %s, Error: %v", userID, accessErr)
		revokeErrors = append(revokeErrors, fmt.Errorf("revoke access token: %w", accessErr))
	}
	if len(revokeErrors) > 0 {
		return errors.Join(revokeErrors...)
	}

	a.logger.Info("Successfully logged out and blocked tokens for user: %s with accessUUID: %s and refreshUUID: %s", userID, accessUUID, refreshUUID)
	return nil
}

func (a *service) RefreshToken(ctx context.Context, refreshToken string) (*dto.TokenDetails, error) {
	_, claims, err := a.parseAndValidateToken(refreshToken)
	if err != nil {
		a.logger.Error("Error parsing refresh token: %v", err)
		return nil, errors.New("invalid refresh token")
	}
	if !hasTokenType(claims, tokenTypeRefresh) {
		return nil, errors.New("invalid refresh token")
	}

	refreshUUID, ok := claims["refresh_uuid"].(string)
	if !ok {
		a.logger.Warn("Refresh UUID not found in the token")
		return nil, errors.New("refresh UUID not found in the token")
	}

	// Check if the refresh token is on the block list
	isBlocked, err := a.blockListService.IsInBlockList(ctx, refreshUUID)
	if err != nil {
		a.logger.Error("Failed to check blockList status: %v", err)
		return nil, errors.New("error checking blockList status")
	}
	if isBlocked {
		a.logger.Warn("Refresh token is blocked")
		return nil, errors.New("refresh token is blocked")
	}

	// Renew the access token using the refresh token's claims
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
	user, err := a.activeUser(userID)
	if err != nil {
		a.logger.Warn("User is unavailable while refreshing an access token: %s", userID)
		return nil, err
	}
	newAccessToken, atExpires, err := a.generateAccessToken(userID, userRole(user.Role), refreshUUID, refreshExp)
	if err != nil {
		a.logger.Error("Failed to generate new access token: %v", err)
		return nil, err
	}

	td := &dto.TokenDetails{
		AccessToken:  newAccessToken,
		AtExpires:    atExpires,
		RefreshToken: refreshToken,
		RefreshUUID:  refreshUUID,
		RtExpires:    refreshExp,
	}

	a.logger.Info("Successfully renewed access token for user: %s", userID.String())

	return td, nil
}

func (a *service) IsUserAuthenticated(ctx context.Context, accessToken string) (bool, error) {
	_, claims, err := a.parseAndValidateToken(accessToken)
	if err != nil {
		a.logger.Error("Error parsing accessToken: %v", err)
		return false, err
	}
	if !hasTokenType(claims, tokenTypeAccess) {
		return false, errors.New("invalid accessToken")
	}
	// Check if the accessToken is in the blockList
	accessUUID, ok := claims["access_uuid"].(string)
	if !ok {
		a.logger.Warn("Access UUID not found in the accessToken")
		return false, errors.New("invalid accessToken")
	}

	isBlocked, err := a.blockListService.IsInBlockList(ctx, accessUUID)
	if err != nil {
		a.logger.Error("Error checking accessToken in block list: %v", err)
		return false, err
	}

	if isBlocked {
		a.logger.Warn("Token is blocked")
		return false, errors.New("accessToken is blocked")
	}
	if _, err := a.activeUserFromClaims(claims); err != nil {
		return false, err
	}

	return true, nil
}

func (a *service) RequestPasswordReset(ctx context.Context, email string) (string, time.Time, error) {
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
		a.logger.Error("Error fetching user by email: %v", err)
		return "", time.Time{}, errors.New("invalid email")
	}

	// Generate a reset token
	resetToken := uuid.New().String()
	resetTokenExpires := time.Now().Add(time.Hour * config.AuthenticationConfig.ExpirationTimeResetTokenHours)

	if err := a.userService.StorePasswordReset(user.ID, resetToken, resetTokenExpires); err != nil {
		a.logger.Error("Error storing password reset: %v", err)
		return "", time.Time{}, errors.New("failed to update user")
	}

	if err := a.passwordResetter.SendPasswordReset(ctx, email, resetToken, resetTokenExpires); err != nil {
		a.logger.Error("Error sending password-reset email: %v", err)
		if rollbackErr := a.userService.ClearPasswordResetIfToken(user.ID, resetToken); rollbackErr != nil {
			a.logger.Error("Failed to remove undelivered reset token: %v", rollbackErr)
		}
		return "", time.Time{}, errors.New("failed to send password reset email")
	}

	a.logger.Info("Successfully sent password-reset email to user: %s", email)
	return resetToken, resetTokenExpires, nil
}

func (a *service) ConfirmPasswordReset(token, newPassword string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("invalid token")
	}
	if len(newPassword) < minimumPasswordLength {
		return ErrRegistrationPasswordWeak
	}

	if err := a.userService.CompletePasswordReset(token, newPassword); err != nil {
		if errors.Is(err, users.ErrInvalidResetToken) {
			return errors.New("invalid token")
		}
		a.logger.Error("Error completing password reset: %v", err)
		return errors.New("failed to change password")
	}

	return nil
}

func (a *service) ChangePassword(accessToken string, newPassword string) error {
	if newPassword == "" {
		return errors.New("new password is required")
	}

	_, claims, err := a.parseAndValidateToken(accessToken)
	if err != nil {
		a.logger.Error("Error parsing accessToken: %v", err)
		return errors.New("invalid accessToken")
	}
	if !hasTokenType(claims, tokenTypeAccess) {
		return errors.New("invalid accessToken")
	}
	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		a.logger.Warn("User ID not found in the accessToken")
		return errors.New("invalid accessToken")
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		a.logger.Error("Error parsing userID: %v", err)
		return errors.New("invalid user ID format")
	}
	user, err := a.userService.GetUserByID(userID)
	if err != nil {
		a.logger.Error("Error fetching user by email: %v", err)
		return errors.New("invalid email")
	}

	user.ResetPasswordToken = ""
	user.ResetTokenExpires = nil

	updateErr := a.userService.UpdatePassword(user.ID, newPassword)
	if updateErr != nil {
		a.logger.Error("Error updating user password: %v", updateErr)
		return errors.New("failed to update password")
	}
	_, err = a.userService.UpdateUser(*user)
	if err != nil {
		a.logger.Error("Error updating user: %v", err)
		return errors.New("failed to update user")
	}

	a.logger.Info("Successfully changed password for user: %s", userIDStr)
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
	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		a.logger.Warn("User ID not found in the accessToken")
		return uuid.UUID{}, errors.New("invalid accessToken")
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		a.logger.Error("Error parsing userID: %v", err)
		return uuid.UUID{}, errors.New("invalid user ID format")
	}
	return userID, nil
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
		Authenticated: true,
		Subject:       user.ID.String(),
		Role:          role,
		Permissions:   permissions,
	}, nil
}

func (a *service) generateAccessToken(userID uuid.UUID, role, refreshUUID string, refreshExp int64) (string, int64, error) {
	now := time.Now()
	expires := now.Add(time.Minute * config.AuthenticationConfig.AccessTokenDurationMinutes).Unix()

	claims := jwt.MapClaims{}
	claims["iss"] = tokenIssuer
	claims["aud"] = tokenAudience
	claims["iat"] = now.Unix()
	claims["user_id"] = userID.String()
	claims["role"] = userRole(role)
	claims["token_type"] = tokenTypeAccess
	claims["access_uuid"] = uuid.New().String()
	claims["refresh_uuid"] = refreshUUID
	claims["refresh_exp"] = refreshExp
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
	refreshUUID := uuid.New().String()
	now := time.Now()
	expires := now.Add(config.AuthenticationConfig.RefreshTokenDurationDays).Unix()

	claims := jwt.MapClaims{}
	claims["iss"] = tokenIssuer
	claims["aud"] = tokenAudience
	claims["iat"] = now.Unix()
	claims["refresh_uuid"] = refreshUUID
	claims["user_id"] = userID.String()
	claims["token_type"] = tokenTypeRefresh
	claims["exp"] = expires

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	refreshToken, err := token.SignedString([]byte(a.jwtSecret))
	return refreshToken, refreshUUID, expires, err
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
	userIDValue, ok := claims["user_id"].(string)
	if !ok {
		return nil, errors.New("invalid accessToken")
	}
	userID, err := uuid.Parse(userIDValue)
	if err != nil {
		return nil, errors.New("invalid accessToken")
	}
	return a.activeUser(userID)
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
		return int64(typedValue), true
	case int64:
		return typedValue, true
	case int:
		return int64(typedValue), true
	default:
		return 0, false
	}
}
