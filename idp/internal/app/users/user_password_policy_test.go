package users

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUpdatePasswordRejectsWeakPasswordBeforeRepositoryLookup(t *testing.T) {
	service := NewUserService(new(MockUserRepository), nil, nil)

	err := service.UpdatePassword(uuid.New(), "short")

	require.ErrorIs(t, err, ErrPasswordWeak)
}

func TestUpdateReturnsBadRequestForWeakPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	service := NewUserService(new(MockUserRepository), nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	router.PATCH("/users", NewHandler(service).Update)

	request := httptest.NewRequest(http.MethodPatch, "/users", bytes.NewBufferString(`{"email":"operator@example.com","password":"short"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
