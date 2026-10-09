package users

import (
	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUpdateRejectsOversizedJSONBeforeAccessingUserService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PATCH("/user", NewHandler(nil).Update)
	request := httptest.NewRequest(http.MethodPatch, "/user", strings.NewReader(strings.Repeat(" ", maxUserJSONBodyBytes+1)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
	if strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("error response exposed request data: %s", recorder.Body.String())
	}
}

func TestUpdateRejectsOversizedSuffixesAndMultipleJSONValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "oversized trailing whitespace",
			body:       `{"email":"owner@example.com"}` + strings.Repeat(" ", maxUserJSONBodyBytes),
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:       "oversized trailing junk",
			body:       `{"email":"owner@example.com"}` + strings.Repeat("x", maxUserJSONBodyBytes),
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:       "duplicate JSON values",
			body:       `{"email":"owner@example.com"}{"email":"other@example.com"}`,
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.PATCH("/user", NewHandler(nil).Update)
			request := httptest.NewRequest(http.MethodPatch, "/user", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestUpdateAcceptsTrailingWhitespaceAfterOneJSONValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PATCH("/user", NewHandler(nil).Update)
	request := httptest.NewRequest(http.MethodPatch, "/user", strings.NewReader(`{"email":"owner@example.com"}`+" \t\r\n"))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d after successful JSON binding reaches auth", recorder.Code, http.StatusUnauthorized)
	}
}

func TestUpdateRejectsPasswordsOutsidePolicyBeforeHashing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "too short", password: "weak-pass", wantErr: utils.ErrPasswordTooShort},
		{name: "too long for bcrypt", password: strings.Repeat("a", utils.MaximumPasswordBytes+1), wantErr: utils.ErrPasswordTooLong},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := new(MockUserRepository)
			hasher := new(MockPasswordHasher)
			userID := uuid.New()
			userService := NewUserService(repo, nil, hasher)
			router := gin.New()
			router.PATCH("/user", func(c *gin.Context) {
				c.Set("userID", userID)
				c.Set("authSession", &dto.AuthSession{Subject: userID.String(), SessionVersion: 4})
				c.Next()
			}, NewHandler(userService).Update)

			request := httptest.NewRequest(http.MethodPatch, "/user", strings.NewReader(fmt.Sprintf(`{"currentPassword":"current-password","password":%q}`, test.password)))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), test.wantErr.Error())
			hasher.AssertNotCalled(t, "Hash", test.password)
			hasher.AssertNotCalled(t, "Compare", mock.Anything, mock.Anything)
			repo.AssertNotCalled(t, "UpdateAccount", userID, int64(4), mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestUpdatePasswordRequiresCurrentPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name                string
		currentPassword     string
		compareErr          error
		wantStatus          int
		wantPasswordPersist bool
		wantError           string
	}{
		{name: "correct current password", currentPassword: "correct-current-password", wantStatus: http.StatusOK, wantPasswordPersist: true},
		{name: "wrong current password", currentPassword: "wrong-current-password", compareErr: utils.ErrPasswordMismatch, wantStatus: http.StatusBadRequest, wantError: ErrInvalidCurrentPassword.Error()},
		{name: "missing current password", wantStatus: http.StatusBadRequest, wantError: ErrCurrentPasswordRequired.Error()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := new(MockUserRepository)
			hasher := new(MockPasswordHasher)
			userID := uuid.New()
			existingUser := &models.User{ID: userID, Email: "owner@example.com", Password: "existing-password-hash", SessionVersion: 4}
			userService := NewUserService(repo, nil, hasher)
			router := gin.New()
			router.PATCH("/user", func(c *gin.Context) {
				c.Set("userID", userID)
				c.Set("authSession", &dto.AuthSession{Subject: userID.String(), SessionVersion: 4})
				c.Next()
			}, NewHandler(userService).Update)

			requestBody := fmt.Sprintf(`{"password":"new-strong-password","currentPassword":%q}`, test.currentPassword)
			request := httptest.NewRequest(http.MethodPatch, "/user", strings.NewReader(requestBody))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			if test.currentPassword != "" {
				repo.On("FindByID", userID).Return(existingUser, nil).Once()
				hasher.On("Compare", "existing-password-hash", test.currentPassword).Return(test.compareErr).Once()
			}
			if test.wantPasswordPersist {
				hasher.On("Hash", "new-strong-password").Return("new-password-hash", nil).Once()
				repo.On("UpdateAccount", userID, int64(4), (*string)(nil), "new-password-hash", mock.AnythingOfType("time.Time")).Return(nil).Once()
			}

			router.ServeHTTP(recorder, request)

			require.Equal(t, test.wantStatus, recorder.Code, recorder.Body.String())
			if test.wantError != "" {
				require.Contains(t, recorder.Body.String(), test.wantError)
			}
			if !test.wantPasswordPersist {
				hasher.AssertNotCalled(t, "Hash", "new-strong-password")
				repo.AssertNotCalled(t, "UpdateAccount", userID, int64(4), mock.Anything, mock.Anything, mock.Anything)
			}
			if test.wantPasswordPersist {
				require.Contains(t, recorder.Body.String(), "owner@example.com", "omitting email must preserve the existing account address")
			}
			repo.AssertExpectations(t)
			hasher.AssertExpectations(t)
		})
	}
}
