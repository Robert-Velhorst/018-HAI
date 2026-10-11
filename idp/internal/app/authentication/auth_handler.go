package authentication

import (
	"automation-hub-idp/internal/app/dto"
	"crypto/subtle"
	"errors"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

const (
	authSubrequestHeader         = "X-HAI-Auth-Subrequest"
	verifiedAccessTokenHeader    = "X-HAI-Verified-Access-Token"
	authSubrequestHeaderExpected = "1"
	authenticatedTokenContextKey = "hai.authenticated-access-token"
)

type Handler struct {
	authService IService
}

func NewHandler(authService IService) *Handler {
	return &Handler{
		authService: authService,
	}
}

// Capabilities exposes only optional login-path availability; it never returns
// provider settings, credentials, or account existence information.
func (h *Handler) Capabilities(c *gin.Context) {
	c.JSON(http.StatusOK, h.authService.Capabilities())
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
	var userDTO dto.UserDTO
	var errorResponse dto.ErrorResponse
	if err := c.ShouldBindJSON(&userDTO); err != nil {
		errorResponse.Message = "Invalid request body"
		errorResponse.ErrorCode = http.StatusBadRequest
		c.JSON(http.StatusBadRequest, errorResponse)
		return
	}

	response, err := h.authService.Register(userDTO)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrRegistrationEmailInvalid), errors.Is(err, ErrRegistrationPasswordWeak):
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
	var userLoginDTO dto.UserLoginDTO
	if err := c.ShouldBindJSON(&userLoginDTO); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokenDetails, err := h.authService.Login(userLoginDTO.Email, userLoginDTO.Password)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}

	atExpiresTime := time.Unix(tokenDetails.AtExpires, 0)
	rtExpiresTime := time.Unix(tokenDetails.RtExpires, 0)

	setAccessTokenCookie(c.Writer, tokenDetails.AccessToken, atExpiresTime)
	setRefreshTokenCookie(c.Writer, tokenDetails.RefreshToken, rtExpiresTime)

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
	setGoogleOAuthStateCookie(c.Writer, state, time.Now().Add(10*time.Minute))
	c.Redirect(http.StatusFound, loginURL)
}

// GoogleCallback is where Google returns after consent. It runs without a HAI
// session (Google calls it directly), protected by the signed state. On success
// it sets the same session cookies as a password login and lands on the app.
func (h *Handler) GoogleCallback(c *gin.Context) {
	if c.Query("error") != "" {
		clearGoogleOAuthStateCookie(c.Writer)
		c.Redirect(http.StatusFound, "/login?error=google_denied")
		return
	}
	state := c.Query("state")
	cookieState, err := c.Cookie(googleOAuthStateCookie)
	clearGoogleOAuthStateCookie(c.Writer)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookieState), []byte(state)) != 1 {
		c.Redirect(http.StatusFound, "/login?error=google_failed")
		return
	}
	tokenDetails, err := h.authService.LoginWithGoogle(c.Request.Context(), c.Query("code"), state)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=google_failed")
		return
	}
	setAccessTokenCookie(c.Writer, tokenDetails.AccessToken, time.Unix(tokenDetails.AtExpires, 0))
	setRefreshTokenCookie(c.Writer, tokenDetails.RefreshToken, time.Unix(tokenDetails.RtExpires, 0))
	c.Redirect(http.StatusFound, "/")
}

// LocalPreview establishes an ordinary signed owner session for the explicit
// local-preview mode. Startup requires an explicit loopback gateway bind; this
// additional Host check is deny-only defense in depth against rebinding and is
// not the trust root for enabling local preview.
func (h *Handler) LocalPreview(c *gin.Context) {
	if !isLoopbackHost(c.Request.Host) {
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
// @Router /auth/logout [get]
func (h *Handler) Logout(c *gin.Context) {
	value, ok := c.Get(authenticatedTokenContextKey)
	accessToken, tokenOK := value.(string)
	if !ok || !tokenOK || accessToken == "" {
		c.Status(http.StatusUnauthorized)
		return
	}

	err := h.authService.Logout(c.Request.Context(), accessToken)
	clearAuthCookies(c.Writer)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
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
		if isAuthenticated, authErr := h.authService.IsUserAuthenticated(c.Request.Context(), accessToken); authErr == nil && isAuthenticated {
			return accessToken, true
		}
	}

	refreshToken, err := c.Cookie("refresh_token")
	if err != nil || refreshToken == "" {
		return "", false
	}
	newAccessToken, err := h.authService.RefreshToken(c.Request.Context(), refreshToken)
	if err != nil || newAccessToken == nil || newAccessToken.AccessToken == "" {
		return "", false
	}

	setAccessTokenCookie(c.Writer, newAccessToken.AccessToken, time.Unix(newAccessToken.AtExpires, 0))
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
// @Router /auth/request-password-reset [post]
func (h *Handler) RequestPasswordReset(c *gin.Context) {
	email := c.PostForm("email")

	// Do not reveal whether an account exists or whether its delivery channel is available.
	// The reset service records operational errors in its own logs.
	_, _, _ = h.authService.RequestPasswordReset(c.Request.Context(), email)
	response := dto.SuccessResponse{
		Message:    "If recovery is available for this account, reset instructions have been sent",
		StatusCode: http.StatusOK,
	}
	c.JSON(http.StatusOK, response)
}

// ConfirmPasswordReset
// @Summary ConfirmPasswordReset
// @Description ConfirmPasswordReset
// @Tags Authentication
// @Accept application/x-www-form-urlencoded
// @Produce json
// @Param reset-token path string true "reset-token"
// @Param newPassword formData string true "newPassword"
// @Success 200 {object} string
// @Failure 400 {object} dto.ErrorResponse
// @Failure 500 {object} dto.ErrorResponse
// @Router /auth/confirm-password-reset/{reset-token} [post]
func (h *Handler) ConfirmPasswordReset(c *gin.Context) {
	var errorResponse dto.ErrorResponse
	token := c.Param("reset-token")
	newPassword := c.PostForm("newPassword")

	err := h.authService.ConfirmPasswordReset(token, newPassword)
	if err != nil {
		errorResponse.Message = err.Error()
		errorResponse.ErrorCode = http.StatusBadRequest
		c.JSON(http.StatusBadRequest, errorResponse)
		return
	}
	response := dto.SuccessResponse{
		Message:    "Password reset successfully",
		StatusCode: http.StatusOK,
	}
	c.JSON(http.StatusOK, response)
}
