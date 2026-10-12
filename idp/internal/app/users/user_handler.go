package users

import (
	"automation-hub-idp/internal/app/dto"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"net/mail"
	"strings"
)

type Handler struct {
	userService UserService
}

func NewHandler(userService UserService) *Handler {
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
// @Router /users [patch]
func (h *Handler) Update(c *gin.Context) {
	var user dto.UserRequest
	var errorResponse dto.ErrorResponse
	if err := c.ShouldBindJSON(&user); err != nil {
		errorResponse.Message = "Invalid request body"
		errorResponse.ErrorCode = http.StatusBadRequest
		c.JSON(http.StatusBadRequest, errorResponse)
		return
	}
	email := strings.TrimSpace(strings.ToLower(user.Email))
	if user.Password == "" && email == "" {
		errorResponse.Message = "Provide an email address or password to update"
		errorResponse.ErrorCode = http.StatusBadRequest
		c.JSON(http.StatusBadRequest, errorResponse)
		return
	}
	if email != "" {
		parsedEmail, err := mail.ParseAddress(email)
		if err != nil || parsedEmail.Address != email {
			errorResponse.Message = "Invalid email address"
			errorResponse.ErrorCode = http.StatusBadRequest
			c.JSON(http.StatusBadRequest, errorResponse)
			return
		}
	}

	temp, ok := c.Get("userID")
	if !ok {
		errorResponse.Message = "Unauthorized"
		errorResponse.ErrorCode = http.StatusUnauthorized
		c.JSON(http.StatusUnauthorized, errorResponse)
		return
	}
	userID, ok := temp.(uuid.UUID)
	if !ok {
		errorResponse.Message = "Unauthorized"
		errorResponse.ErrorCode = http.StatusUnauthorized
		c.JSON(http.StatusUnauthorized, errorResponse)
		return
	}

	// check if userRequest.password is not empty
	if user.Password != "" {
		err := h.userService.UpdatePassword(userID, user.Password)
		if err != nil {
			if errors.Is(err, ErrPasswordWeak) {
				errorResponse.Message = ErrPasswordWeak.Error()
				errorResponse.ErrorCode = http.StatusBadRequest
				c.JSON(http.StatusBadRequest, errorResponse)
				return
			}
			errorResponse.Message = "Error updating user"
			errorResponse.ErrorCode = http.StatusInternalServerError
			c.JSON(http.StatusInternalServerError, errorResponse)
			return
		}
	}

	userToUpdate, err := h.userService.GetUserByID(userID)
	if err != nil || userToUpdate == nil {
		errorResponse.Message = "Error updating user"
		errorResponse.ErrorCode = http.StatusInternalServerError
		c.JSON(http.StatusInternalServerError, errorResponse)
		return
	}

	updatedUser := userToUpdate
	if email != "" && email != userToUpdate.Email {
		userToUpdate.Email = email
		updatedUser, err = h.userService.UpdateUser(*userToUpdate)
		if err != nil || updatedUser == nil {
			errorResponse.Message = "Error updating user"
			errorResponse.ErrorCode = http.StatusInternalServerError
			c.JSON(http.StatusInternalServerError, errorResponse)
			return
		}
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
	userID, ok := temp.(uuid.UUID)
	if !ok {
		errorResponse.Message = "Unauthorized"
		errorResponse.ErrorCode = http.StatusUnauthorized
		c.JSON(http.StatusUnauthorized, errorResponse)
		return
	}

	user, err := h.userService.GetUserByID(userID)
	if err != nil || user == nil {
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
