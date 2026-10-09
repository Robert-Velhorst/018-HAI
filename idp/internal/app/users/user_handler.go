package users

import (
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/utils"
	"bytes"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
	"net/http"
)

type Handler struct {
	userService AccountUpdateService
}

const maxUserJSONBodyBytes = 16 * 1024

func bindUserJSON(c *gin.Context, destination any) error {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxUserJSONBodyBytes))
	if err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if userJSONHasTrailingValue(body) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return c.ShouldBindJSON(destination)
}

func userJSONHasTrailingValue(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var first json.RawMessage
	if err := decoder.Decode(&first); err != nil {
		return false
	}
	var trailing json.RawMessage
	return decoder.Decode(&trailing) != io.EOF
}

func NewHandler(userService AccountUpdateService) *Handler {
	return &Handler{
		userService: userService,
	}
}

// Update
// @Summary Update a user
// @Description Update a user
// @Tags Users
// @Accept json
// @Produce json
// @Param user body dto.UserRequest true "User object"
// @Success 200 {object} dto.UserResponse
// @Failure 400 {object} dto.ErrorResponse
// @Failure 401 {object} dto.ErrorResponse
// @Failure 500 {object} dto.ErrorResponse
// @Router /user [patch]
func (h *Handler) Update(c *gin.Context) {
	var user dto.UserRequest
	var errorResponse dto.ErrorResponse
	if err := bindUserJSON(c, &user); err != nil {
		errorResponse.Message = "Invalid request body"
		errorResponse.ErrorCode = http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			errorResponse.ErrorCode = http.StatusRequestEntityTooLarge
		}
		c.JSON(errorResponse.ErrorCode, errorResponse)
		return
	}

	temp, ok := c.Get("userID")
	if !ok {
		errorResponse.Message = "Unauthorized"
		errorResponse.ErrorCode = http.StatusUnauthorized
		c.JSON(http.StatusUnauthorized, errorResponse)
		return
	}
	userID := temp.(uuid.UUID)
	sessionValue, ok := c.Get("authSession")
	if !ok {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Message: "Please log in again", ErrorCode: http.StatusUnauthorized})
		return
	}
	session, ok := sessionValue.(*dto.AuthSession)
	if !ok || session == nil || session.Subject != userID.String() {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Message: "Please log in again", ErrorCode: http.StatusUnauthorized})
		return
	}

	updatedUser, err := h.userService.UpdateAccount(userID, session.SessionVersion, user.CurrentPassword, user.Email, user.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidEmail) || errors.Is(err, utils.ErrPasswordTooShort) || errors.Is(err, utils.ErrPasswordTooLong) ||
			errors.Is(err, ErrCurrentPasswordRequired) || errors.Is(err, ErrInvalidCurrentPassword) {
			c.JSON(http.StatusBadRequest, dto.ErrorResponse{Message: err.Error(), ErrorCode: http.StatusBadRequest})
			return
		}
		if errors.Is(err, ErrConcurrentUserUpdate) {
			c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Message: "Your session changed. Please log in again.", ErrorCode: http.StatusUnauthorized})
			return
		}
		if errors.Is(err, ErrUserAlreadyExists) {
			c.JSON(http.StatusConflict, dto.ErrorResponse{Message: "An account with this email already exists", ErrorCode: http.StatusConflict})
			return
		}
		errorResponse.Message = "Error updating user"
		errorResponse.ErrorCode = http.StatusInternalServerError
		c.JSON(http.StatusInternalServerError, errorResponse)
		return
	}
	userResponse := dto.UserResponse{
		ID:    updatedUser.ID,
		Email: updatedUser.Email,
	}
	c.JSON(http.StatusOK, userResponse)
}

// GetCurrentUser
// @Summary GetCurrentUser
// @Description GetCurrentUser
// @Tags Users
// @Produce json
// @Success 200 {object} dto.UserResponse
// @Failure 401 {object} dto.ErrorResponse
// @Failure 404 {object} dto.ErrorResponse
// @Router /user [get]
func (h *Handler) GetCurrentUser(c *gin.Context) {
	var errorResponse dto.ErrorResponse
	temp, ok := c.Get("userID")
	if !ok {
		errorResponse.Message = "Unauthorized"
		errorResponse.ErrorCode = http.StatusUnauthorized
		c.JSON(http.StatusUnauthorized, errorResponse)
		return
	}
	userID := temp.(uuid.UUID)

	user, err := h.userService.GetUserByID(userID)
	if err != nil {
		errorResponse.Message = "User not found"
		errorResponse.ErrorCode = http.StatusNotFound
		c.JSON(http.StatusNotFound, errorResponse)
		return
	}
	userResponse := dto.UserResponse{
		ID:    user.ID,
		Email: user.Email,
	}
	c.JSON(http.StatusOK, userResponse)
}
