package authentication

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/dto"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

const (
	authSubrequestHeader         = "X-HAI-Auth-Subrequest"
	verifiedAccessTokenHeader    = "X-HAI-Verified-Access-Token"
	refreshedAccessCookieHeader  = "X-HAI-Refreshed-Access-Cookie"
	refreshedRefreshCookieHeader = "X-HAI-Refreshed-Refresh-Cookie"
	localPreviewGatewayHeader    = "X-HAI-Local-Preview-Gateway-Secret"
	authSubrequestHeaderExpected = "1"
	authenticatedTokenContextKey = "hai.authenticated-access-token"
	maxAuthJSONBodyBytes         = 16 * 1024
)

func safeAuthenticationReturnURL(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if !strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "//") || strings.Contains(candidate, "\\") || strings.ContainsAny(candidate, "\r\n") || strings.HasPrefix(candidate, "/login") {
		return "/"
	}
	return candidate
}

type Handler struct {
	authService            IService
	passwordResetRateLimit *passwordResetLimiter
	publicAuthRateLimit    *publicAuthRateLimiter
}

// Close releases only the handler-owned rate-limit client, not its auth service.
func (h *Handler) Close() error {
	if h == nil {
		return nil
	}
	return errors.Join(h.passwordResetRateLimit.Close(), h.publicAuthRateLimit.Close())
}

func NewHandler(authService IService) *Handler {
	return newHandler(authService, newMemoryOnlyPasswordResetLimiter())
}

func NewHandlerWithRedisPasswordResetLimiter(authService IService, redisAddress string) *Handler {
	secret := ""
	if config.AuthenticationConfig != nil {
		secret = config.AuthenticationConfig.JwtSecret
	}
	handler := newHandler(authService, newPasswordResetLimiter(redisAddress, secret))
	handler.publicAuthRateLimit = newPublicAuthRateLimiter(redisAddress, secret)
	return handler
}

func newHandler(authService IService, limiter *passwordResetLimiter) *Handler {
	return &Handler{
		authService:            authService,
		passwordResetRateLimit: limiter,
		publicAuthRateLimit:    newMemoryPublicAuthRateLimiter(),
	}
}

func bindAuthJSON(c *gin.Context, destination any) error {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxAuthJSONBodyBytes))
	if err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if authJSONHasTrailingValue(body) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return c.ShouldBindJSON(destination)
}

func authJSONHasTrailingValue(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var first json.RawMessage
	if err := decoder.Decode(&first); err != nil {
		return false
	}
	var trailing json.RawMessage
	return decoder.Decode(&trailing) != io.EOF
}

func authJSONErrorStatus(err error) int {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// Capabilities exposes only optional login-path availability; it never returns
// provider settings, credentials, or account existence information.
func (h *Handler) Capabilities(c *gin.Context) {
	capabilities := h.authService.Capabilities()
	if !config.LocalPreviewConfig.LocalPreviewSessionAllowed() || !isLoopbackBrowserRequest(c.Request) {
		capabilities.LocalPreviewEnabled = false
	}
	c.JSON(http.StatusOK, capabilities)
}

// Register
// @Summary Register a new user
// @Description Register a new user
// @Tags Authentication
// @Accept json
// @Produce json
// @Param body body dto.UserDTO true "User registration details"
// @Success 200 {object} dto.UserDTO
// @Failure 400 {object} dto.ErrorResponse
// @Failure 409 {object} dto.ErrorResponse
// @Failure 500 {object} dto.ErrorResponse
// @Router /auth/register [post]
func (h *Handler) Register(c *gin.Context) {
	if !h.enforcePublicAuthRateLimit(c, "register") {
		return
	}
	var userDTO dto.UserDTO
	var errorResponse dto.ErrorResponse
	if err := bindAuthJSON(c, &userDTO); err != nil {
		errorResponse.Message = "Invalid request body"
		errorResponse.ErrorCode = authJSONErrorStatus(err)
		c.JSON(errorResponse.ErrorCode, errorResponse)
		return
	}

	response, err := h.authService.Register(userDTO)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrRegistrationEmailInvalid), errors.Is(err, ErrRegistrationPasswordWeak), errors.Is(err, ErrRegistrationPasswordTooLong):
			status = http.StatusBadRequest
		case errors.Is(err, ErrRegistrationEmailInUse):
			status = http.StatusConflict
		}
		errorResponse.Message = err.Error()
		errorResponse.ErrorCode = status
		c.JSON(status, errorResponse)
		return
	}

	c.JSON(http.StatusOK, response)
}

// Login
// @Summary Login
// @Description Login
// @Tags Authentication
// @Accept application/json
// @Param body body dto.UserLoginDTO true "User object"
// @Success 200 "Successfully logged in"
// @Failure 400 "Unauthorized"
// @Failure 500 "Internal Server Error"
// @Router /auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	if !h.enforcePublicAuthRateLimit(c, "login") {
		return
	}
	var userLoginDTO dto.UserLoginDTO
	if err := bindAuthJSON(c, &userLoginDTO); err != nil {
		c.JSON(authJSONErrorStatus(err), gin.H{"error": "Invalid request body"})
		return
	}

	tokenDetails, err := h.authService.Login(userLoginDTO.Email, userLoginDTO.Password)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}

	rememberMe := userLoginDTO.RememberMe == nil || *userLoginDTO.RememberMe
	setLoginSessionCookies(c.Writer, tokenDetails, rememberMe)

	c.Status(http.StatusOK)
}

// GoogleLogin redirects the browser to Google's consent screen. The user picks
// their Google account there; nothing sensitive is handled here.
func (h *Handler) GoogleLogin(c *gin.Context) {
	loginURL, err := h.authService.GoogleAuthURL()
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=google_unavailable")
		return
	}
	parsedURL, err := neturl.Parse(loginURL)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=google_unavailable")
		return
	}
	state := parsedURL.Query().Get("state")
	if state == "" {
		c.Redirect(http.StatusFound, "/login?error=google_unavailable")
		return
	}
	var rememberMe *bool
	if preference := c.Query("rememberMe"); preference != "" {
		parsedPreference, err := strconv.ParseBool(preference)
		if err != nil {
			c.Redirect(http.StatusFound, "/login?error=google_unavailable")
			return
		}
		rememberMe = &parsedPreference
	}
	expires := time.Now().Add(10 * time.Minute)
	setGoogleOAuthStateCookie(c.Writer, state, expires)
	setGoogleOAuthReturnURLCookie(c.Writer, safeAuthenticationReturnURL(c.Query("returnUrl")), expires)
	if rememberMe != nil {
		setGoogleRememberMeCookie(c.Writer, *rememberMe, expires)
	} else if _, err := c.Cookie(googleOAuthRememberMeCookie); err == nil {
		clearGoogleRememberMeCookie(c.Writer)
	}
	c.Redirect(http.StatusFound, loginURL)
}

// GoogleCallback is where Google returns after consent. It runs without a HAI
// session (Google calls it directly), protected by the signed state. On success
// it sets the same session cookies as a password login and lands on the app.
func (h *Handler) GoogleCallback(c *gin.Context) {
	state := c.Query("state")
	cookieState, err := c.Cookie(googleOAuthStateCookie)
	returnURL, returnURLErr := c.Cookie(googleOAuthReturnURLCookie)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookieState), []byte(state)) != 1 {
		c.Redirect(http.StatusFound, "/login?error=google_failed")
		return
	}
	rememberMe := googleAuthCookiesShouldPersist(c.Request)
	clearGoogleOAuthStateCookie(c.Writer)
	clearGoogleOAuthReturnURLCookie(c.Writer)
	clearGoogleRememberMeCookie(c.Writer)
	if c.Query("error") != "" {
		c.Redirect(http.StatusFound, "/login?error=google_denied")
		return
	}
	tokenDetails, err := h.authService.LoginWithGoogle(c.Request.Context(), c.Query("code"), state)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=google_failed")
		return
	}
	setLoginSessionCookies(c.Writer, tokenDetails, rememberMe)
	if returnURLErr != nil {
		returnURL = "/"
	}
	c.Redirect(http.StatusFound, safeAuthenticationReturnURL(returnURL))
}

// LocalPreview establishes an ordinary signed owner session for a direct
// loopback caller or the gateway that proves its identity with a dedicated
// secret. Host and Origin remain loopback-only; forwarded headers are ignored.
func (h *Handler) LocalPreview(c *gin.Context) {
	origins := c.Request.Header.Values("Origin")
	previewConfig := config.LocalPreviewConfig
	previewEnabled := previewConfig.LocalPreviewSessionAllowed()
	trustedGateway := previewEnabled && subtle.ConstantTimeCompare(
		[]byte(c.Request.Header.Get(localPreviewGatewayHeader)), []byte(previewConfig.GatewaySecret),
	) == 1
	if !previewEnabled || (!isLoopbackRemoteAddr(c.Request.RemoteAddr) && !trustedGateway) || !isLoopbackHost(c.Request.Host) ||
		len(origins) != 1 || !isLoopbackOrigin(origins[0]) {
		c.Status(http.StatusNotFound)
		return
	}
	tokenDetails, err := h.authService.LocalPreviewLogin()
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	setAccessTokenCookie(c.Writer, tokenDetails.AccessToken, time.Unix(tokenDetails.AtExpires, 0))
	setRefreshTokenCookie(c.Writer, tokenDetails.RefreshToken, time.Unix(tokenDetails.RtExpires, 0))
	c.Status(http.StatusNoContent)
}

func setLoginSessionCookies(w http.ResponseWriter, tokens *dto.TokenDetails, rememberMe bool) {
	accessExpires := time.Unix(tokens.AtExpires, 0)
	refreshExpires := time.Unix(tokens.RtExpires, 0)
	setAccessTokenCookieWithPersistence(w, tokens.AccessToken, accessExpires, rememberMe)
	setRefreshTokenCookieWithPersistence(w, tokens.RefreshToken, refreshExpires, rememberMe)
	setRememberMeCookie(w, rememberMe, refreshExpires)
}

func isLoopbackBrowserRequest(request *http.Request) bool {
	if request == nil || !isLoopbackHost(request.Host) {
		return false
	}
	origins := request.Header.Values("Origin")
	return len(origins) == 0 || (len(origins) == 1 && isLoopbackOrigin(origins[0]))
}

func isLoopbackOrigin(origin string) bool {
	parsed, err := neturl.Parse(strings.TrimSpace(origin))
	if err != nil || parsed == nil || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return false
	}
	return isLoopbackHost(parsed.Host)
}

func hasAllowedLogoutOrigin(request *http.Request) bool {
	if request == nil || request.Host == "" {
		return false
	}
	origins := request.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	origin := origins[0]
	if origin != strings.TrimSpace(origin) {
		return false
	}
	parsed, err := neturl.Parse(origin)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.String() != origin {
		return false
	}
	wantScheme := "http"
	if authCookieSecure() {
		wantScheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, wantScheme) && strings.EqualFold(parsed.Host, request.Host)
}

func (h *Handler) RequireLogoutOrigin(c *gin.Context) {
	if !hasAllowedLogoutOrigin(c.Request) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Next()
}

func isLoopbackRemoteAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isLoopbackHost(requestHost string) bool {
	host := strings.TrimSpace(requestHost)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

// Logout
// @Summary Logout
// @Description Logout
// @Tags Authentication
// @Success 200 "OK"
// @Failure 400 "Unauthorized"
// @Failure 500 "Internal Server Error"
// @Router /auth/logout [post]
func (h *Handler) Logout(c *gin.Context) {
	if !hasAllowedLogoutOrigin(c.Request) {
		c.Status(http.StatusForbidden)
		return
	}
	value, ok := c.Get(authenticatedTokenContextKey)
	accessToken, tokenOK := value.(string)
	if !ok || !tokenOK || accessToken == "" {
		c.Status(http.StatusUnauthorized)
		return
	}

	err := h.authService.Logout(accessToken)
	clearAuthCookies(c.Writer)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}

func (h *Handler) enforcePublicAuthRateLimit(c *gin.Context, action string) bool {
	if h.publicAuthRateLimit == nil {
		c.Header("Retry-After", strconv.Itoa(int(publicAuthRateWindow.Seconds())))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Authentication temporarily unavailable. Try again later."})
		return false
	}
	decision := h.publicAuthRateLimit.allow(c.Request.Context(), action, c.Request.RemoteAddr)
	if !decision.Allowed {
		retryAfter := int(decision.RetryAfter.Round(time.Second).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many authentication attempts. Try again later."})
		return false
	}
	return true
}

// IsUserAuthenticated
// @Summary IsUserAuthenticated
// @Description IsUserAuthenticated
// @Tags Authentication
// @Success 200 "OK"
// @Failure 401 "Unauthorized"
// @Failure 500 "Internal Server Error"
// @Router /auth/is-user-authenticated [get]
func (h *Handler) IsUserAuthenticated(c *gin.Context) {
	accessToken, ok := h.resolveAuthenticatedAccessToken(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}

	// Only nginx's internal auth subrequest asks for the verified token. The
	// public IDP proxy clears this request header and hides the response header.
	if c.GetHeader(authSubrequestHeader) == authSubrequestHeaderExpected {
		c.Header(verifiedAccessTokenHeader, accessToken)
	}
	c.Status(http.StatusOK)
}

// CurrentSession returns only authorization state needed by the dashboard. An
// unauthenticated browser receives an explicit false state rather than a 401 so
// route guards can redirect without creating a console-level network error.
// Request identity headers are never consulted.
func (h *Handler) CurrentSession(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	accessToken, ok := h.resolveAuthenticatedAccessToken(c)
	if !ok {
		c.JSON(http.StatusOK, dto.AuthSession{})
		return
	}
	session, err := h.authService.GetSessionFromToken(accessToken)
	if err != nil {
		c.JSON(http.StatusOK, dto.AuthSession{})
		return
	}
	c.JSON(http.StatusOK, session)
}

func (h *Handler) resolveAuthenticatedAccessToken(c *gin.Context) (string, bool) {
	if accessToken, err := c.Cookie("access_token"); err == nil && accessToken != "" {
		if isAuthenticated, authErr := h.authService.IsUserAuthenticated(accessToken); authErr == nil && isAuthenticated {
			return accessToken, true
		}
	}

	refreshToken, err := c.Cookie("refresh_token")
	if err != nil || refreshToken == "" {
		return "", false
	}
	newAccessToken, err := h.authService.RefreshToken(refreshToken)
	if err != nil || newAccessToken == nil || newAccessToken.AccessToken == "" {
		return "", false
	}

	persistent := authCookiesShouldPersist(c.Request)
	accessExpires := time.Unix(newAccessToken.AtExpires, 0)
	if c.GetHeader(authSubrequestHeader) == authSubrequestHeaderExpected {
		c.Header(refreshedAccessCookieHeader, authCookie("access_token", newAccessToken.AccessToken, accessExpires, persistent).String())
		if newAccessToken.RefreshToken != "" {
			c.Header(refreshedRefreshCookieHeader, authCookie("refresh_token", newAccessToken.RefreshToken, time.Unix(newAccessToken.RtExpires, 0), persistent).String())
		}
	} else {
		setAccessTokenCookieWithPersistence(c.Writer, newAccessToken.AccessToken, accessExpires, persistent)
		if newAccessToken.RefreshToken != "" {
			setRefreshTokenCookieWithPersistence(c.Writer, newAccessToken.RefreshToken, time.Unix(newAccessToken.RtExpires, 0), persistent)
		}
	}
	return newAccessToken.AccessToken, true
}

// RequestPasswordReset
// @Summary RequestPasswordReset
// @Description RequestPasswordReset
// @Tags Authentication
// @Accept application/x-www-form-urlencoded
// @Produce json
// @Param email formData string true "Email"
// @Success 200 {object} string
// @Failure 429 {object} dto.ErrorResponse
// @Router /auth/request-password-reset [post]
func (h *Handler) RequestPasswordReset(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8*1024)
	if err := c.Request.ParseForm(); err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, dto.ErrorResponse{Message: "Invalid request body", ErrorCode: status})
		return
	}
	email := c.Request.PostForm.Get("email")
	decision := h.passwordResetRateLimit.allow(c.Request.Context(), c.ClientIP(), email)
	if !decision.Allowed {
		retryAfter := int(decision.RetryAfter.Round(time.Second).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		c.JSON(http.StatusTooManyRequests, dto.ErrorResponse{
			Message:   "Too many recovery requests. Try again later.",
			ErrorCode: http.StatusTooManyRequests,
		})
		return
	}

	// Do not reveal whether an account exists or whether its delivery channel is available.
	// The reset service records operational errors in its own logs.
	_, _, _ = h.authService.RequestPasswordReset(email)
	response := dto.SuccessResponse{
		Message:    "If recovery is available for this account, reset instructions may be sent. This response does not confirm account existence or email delivery.",
		StatusCode: http.StatusOK,
	}
	c.JSON(http.StatusOK, response)
}

// ConfirmPasswordReset
// @Summary ConfirmPasswordReset
// @Description ConfirmPasswordReset
// @Tags Authentication
// @Accept json
// @Produce json
// @Param body body dto.ConfirmPasswordResetDTO true "One-time reset token and new password"
// @Success 200 {object} string
// @Failure 400 {object} dto.ErrorResponse
// @Failure 500 {object} dto.ErrorResponse
// @Router /auth/confirm-password-reset [post]
func (h *Handler) ConfirmPasswordReset(c *gin.Context) {
	if !h.enforcePublicAuthRateLimit(c, "password_reset_confirm") {
		return
	}

	var errorResponse dto.ErrorResponse
	var request dto.ConfirmPasswordResetDTO
	if err := bindAuthJSON(c, &request); err != nil {
		errorResponse.Message = "Invalid request body"
		errorResponse.ErrorCode = authJSONErrorStatus(err)
		c.JSON(errorResponse.ErrorCode, errorResponse)
		return
	}

	c.Header("Cache-Control", "no-store")
	err := h.authService.ConfirmPasswordReset(request.Token, request.NewPassword)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Password reset is temporarily unavailable. Please try again."
		if errors.Is(err, ErrInvalidPasswordReset) || errors.Is(err, ErrRegistrationPasswordWeak) || errors.Is(err, ErrRegistrationPasswordTooLong) {
			status = http.StatusBadRequest
			message = err.Error()
		}
		errorResponse.Message = message
		errorResponse.ErrorCode = status
		c.JSON(status, errorResponse)
		return
	}
	response := dto.SuccessResponse{
		Message:    "Password reset successfully",
		StatusCode: http.StatusOK,
	}
	c.JSON(http.StatusOK, response)
}
