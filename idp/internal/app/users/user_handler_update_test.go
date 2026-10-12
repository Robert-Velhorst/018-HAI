package users

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUpdatePreservesEmailForPasswordOnlyRequest(t *testing.T) {
	userID := uuid.New()
	service := &recordingUserService{user: &models.User{ID: userID, Email: "operator@example.com"}}
	recorder := performUserUpdate(t, userID, service, `{"password":"a-strong-password"}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "a-strong-password", service.password)
	require.Nil(t, service.updatedUser)
	require.Contains(t, recorder.Body.String(), "operator@example.com")
}

func TestUpdateRejectsEmptyRequest(t *testing.T) {
	service := &recordingUserService{}
	recorder := performUserUpdate(t, uuid.New(), service, `{}`)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Nil(t, service.updatedUser)
	require.Empty(t, service.password)
}

func TestUpdateRejectsInvalidEmail(t *testing.T) {
	service := &recordingUserService{}
	recorder := performUserUpdate(t, uuid.New(), service, `{"email":"not-an-email"}`)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Nil(t, service.updatedUser)
	require.Empty(t, service.password)
}

func TestUserHandlersRejectInvalidIdentityContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &recordingUserService{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "not-a-uuid")
		c.Next()
	})
	handler := NewHandler(service)
	router.PATCH("/users", handler.Update)
	router.GET("/user", handler.GetCurrentUser)

	updateRequest := httptest.NewRequest(http.MethodPatch, "/users", bytes.NewBufferString(`{"email":"operator@example.com"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRecorder := httptest.NewRecorder()
	router.ServeHTTP(updateRecorder, updateRequest)
	require.Equal(t, http.StatusUnauthorized, updateRecorder.Code)

	currentRequest := httptest.NewRequest(http.MethodGet, "/user", nil)
	currentRecorder := httptest.NewRecorder()
	router.ServeHTTP(currentRecorder, currentRequest)
	require.Equal(t, http.StatusUnauthorized, currentRecorder.Code)
}

func TestGetCurrentUserRejectsMissingUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &recordingUserService{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", uuid.New())
		c.Next()
	})
	router.GET("/user", NewHandler(service).GetCurrentUser)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/user", nil))

	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func performUserUpdate(t *testing.T, userID uuid.UUID, service UserService, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	router.PATCH("/users", NewHandler(service).Update)

	request := httptest.NewRequest(http.MethodPatch, "/users", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

type recordingUserService struct {
	user        *models.User
	updatedUser *models.User
	password    string
}

func (s *recordingUserService) CreateUser(models.User) (*models.User, error)     { return nil, nil }
func (s *recordingUserService) GetUserByID(uuid.UUID) (*models.User, error)      { return s.user, nil }
func (s *recordingUserService) GetUserByEmail(string) (*models.User, error)      { return nil, nil }
func (s *recordingUserService) GetUserByResetToken(string) (*models.User, error) { return nil, nil }
func (s *recordingUserService) StorePasswordResetToken(uuid.UUID, int64, string, time.Time) error {
	return nil
}
func (s *recordingUserService) CompleteSuccessfulLogin(models.User) (*models.User, error) {
	return nil, nil
}
func (s *recordingUserService) UpdateUser(user models.User) (*models.User, error) {
	s.updatedUser = &user
	return &user, nil
}
func (s *recordingUserService) DeleteUser(uuid.UUID) error { return nil }
func (s *recordingUserService) GetAllUsers(*utils.Pagination) ([]*models.User, error) {
	return nil, nil
}
func (s *recordingUserService) UpdatePassword(_ uuid.UUID, password string) error {
	s.password = password
	return nil
}
func (s *recordingUserService) StorePasswordReset(uuid.UUID, string, time.Time) error { return nil }
func (s *recordingUserService) ClearPasswordResetIfToken(uuid.UUID, string) error     { return nil }
func (s *recordingUserService) CompletePasswordReset(string, string) error            { return nil }
func (s *recordingUserService) ClearPasswordResetToken(uuid.UUID, string) error       { return nil }
func (s *recordingUserService) ConsumePasswordResetToken(uuid.UUID, string, string) error {
	return nil
}
