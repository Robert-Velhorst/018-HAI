package automation

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestLaunchHandlerRequiresServerOwnedApprovalAndIgnoresClientClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetCalls++
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	automationID := uuid.New()
	mandateID := uuid.NewString()
	repository := newFakeAutomationRepo(&models.Automation{
		ID:                 automationID,
		Name:               "Owner-scoped launch",
		URLPath:            "owner-scoped-launch",
		LaunchType:         "api",
		LaunchTarget:       "GET " + target.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	authorizer := &recordingExecutionAuthorizer{}
	service := NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(
		&contextualConfigurationReadProbe{Repository: repository},
		events.Publisher{},
		nil,
		newUnitTestApprovalProofService(),
		authorizer,
	)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/automations/:id/launch", handler.Launch)
	body := []byte(`{
		"ownerIdentity":"mallory",
		"actorIdentity":"mallory",
		"approvalSourceId":"forged",
		"approvalBindingDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"task":"Read current state",
		"projectKey":"018-hai",
		"mandateId":"` + mandateID + `"
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/automations/"+automationID.String()+"/launch",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "handler-owner-scope-test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if authorizer.calls.Load() != 0 || targetCalls != 0 {
		t.Fatalf("unapproved request reached authorization/network: authorization calls=%d target calls=%d", authorizer.calls.Load(), targetCalls)
	}
	if len(repository.launchEvents) != 1 {
		t.Fatalf("blocked owner request was not audited: %#v", repository.launchEvents)
	}
	if event := repository.launchEvents[0]; event.OwnerIdentity != "alice" || event.Status != "blocked" || !event.RequiresApproval {
		t.Fatalf("untrusted claims changed the owner or approval result: %#v", event)
	}
}

func TestLaunchHandlerRejectsMalformedOptionalLaunchContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&service{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/automations/:id/launch", handler.Launch)
	request := httptest.NewRequest(
		http.MethodPost,
		"/automations/"+uuid.NewString()+"/launch",
		bytes.NewBufferString(`{"mandateId":`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestLaunchHandlerRequiresIdempotencyKeyForMutatingLaunch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	automationID := uuid.New()
	repository := newFakeAutomationRepo(&models.Automation{
		ID:           automationID,
		LaunchType:   "api",
		LaunchTarget: "POST http://127.0.0.1:1/mutate",
	})
	handler := NewHandler(newTestService(&contextualConfigurationReadProbe{Repository: repository}, events.Publisher{}))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/automations/:id/launch", handler.Launch)
	request := httptest.NewRequest(http.MethodPost, "/automations/"+automationID.String()+"/launch", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, body = %s; want 428 for missing retry identity", response.Code, response.Body.String())
	}
	if len(repository.launchIntents) != 0 || len(repository.launchEvents) != 0 {
		t.Fatalf("missing idempotency key must fail before intent or effect processing: intents=%#v outcomes=%#v", repository.launchIntents, repository.launchEvents)
	}
}

func TestLaunchHandlerRejectsConflictingIdempotencyKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(&service{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.POST("/automations/:id/launch", handler.Launch)
	request := httptest.NewRequest(http.MethodPost, "/automations/"+uuid.NewString()+"/launch", bytes.NewBufferString(`{"idempotencyKey":"body-key"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "header-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400 for conflicting keys", response.Code, response.Body.String())
	}
}
