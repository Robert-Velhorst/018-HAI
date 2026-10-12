package users

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUpdatePasswordRejectsWeakPasswordBeforeRepositoryLookup(t *testing.T) {
	service := NewUserService(new(MockUserRepository), nil, nil)

	err := service.UpdatePasswordWithCurrentPassword(uuid.New(), 1, "current-password", "short")

	require.ErrorIs(t, err, utils.ErrPasswordTooShort)
}

func TestUpdateReturnsBadRequestForWeakPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	service := NewUserService(new(MockUserRepository), nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Set("authSession", &dto.AuthSession{Subject: userID.String(), SessionVersion: 1})
		c.Next()
	})
	router.PATCH("/users", NewHandler(service).Update)

	request := httptest.NewRequest(http.MethodPatch, "/users", bytes.NewBufferString(`{"currentPassword":"current-password","email":"operator@example.com","password":"short"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
