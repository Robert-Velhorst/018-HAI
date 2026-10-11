package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/apierror"

	"github.com/gin-gonic/gin"
)

func TestRespondErrorRedactsCredentialDataAtHTTPBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, err := range map[string]*apierror.Error{
		"password and cookie": apierror.New(apierror.CodeValidation, "provider password=synthetic-password").
			WithDetail("Cookie", "session=synthetic-session"),
		"credential assignments": apierror.New(apierror.CodeValidation, "provider passphrase=synthetic-passphrase").
			WithDetail("diagnostic", "private_key=synthetic-key encryptionKey=synthetic-encryption"),
		"nested JSON field": apierror.New(apierror.CodeValidation, "provider validation failed").
			WithDetail("diagnostic", `{"nested":{"Bearer synthetic-field-credential":"synthetic-unsafe-value"},"status":"keep"}`),
		"directly populated field": {
			Code: apierror.CodeValidation, Message: "provider validation failed",
			Details: map[string]string{"Bearer synthetic-field-credential": "synthetic-unsafe-value"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			err.WithDetail("field", "name is required")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)
			respondError(c, err)
			if rec.Code != http.StatusUnprocessableEntity || strings.Contains(rec.Body.String(), "synthetic-") {
				t.Fatalf("HTTP response leaked credentials or changed validation status: %d %s", rec.Code, rec.Body.String())
			}
			var response apierror.Envelope
			if decodeErr := json.Unmarshal(rec.Body.Bytes(), &response); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if response.Error == nil || response.Error.Code != apierror.CodeValidation || response.Error.Details["field"] != "name is required" {
				t.Fatalf("ordinary HTTP error context was not preserved: %s", rec.Body.String())
			}
		})
	}
}

func TestRespondErrorWritesEnvelopeAndStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	respondError(c, apierror.New(apierror.CodeNotFound, "memory not found").WithDetail("id", "abc"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "not_found" || body.Error.Message != "memory not found" || body.Error.Details["id"] != "abc" {
		t.Fatalf("envelope wrong: %+v", body.Error)
	}
}

func TestRespondErrConvenience(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)

	respondErr(c, apierror.CodeValidation, "content is required")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}
