package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/phase2"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestSourceOperationApprovalUsesVerifiedOwnerScopeAndApprovePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousSecret, previousMode := config.AppConfig.JWTSecret, config.AppConfig.RunMode
	config.AppConfig.JWTSecret = "source-approval-route-test-secret"
	config.AppConfig.RunMode = "test"
	t.Cleanup(func() {
		config.AppConfig.JWTSecret = previousSecret
		config.AppConfig.RunMode = previousMode
	})

	service := operations.NewService(operations.NewMemoryRepository())
	owner := "operation-owner"
	configuredWorkerOwner := "configured-worker-owner"
	sourceID := uuid.New()
	created, err := service.Ingest(operations.NewOperationInput{
		OwnerUserID: owner, WorkspaceID: "local", Title: "Review imported email",
		Description: "Prepare a local review note", OperationType: "review", SourceType: "email",
		SourceID: &sourceID, SourceRevisionHash: strings.Repeat("c", 64), DedupeKey: "approval-route-" + sourceID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	op := created.Operation
	op.CurrentDecision = string(operations.DecisionAskRobert)
	op.RequiresApproval = true
	op.RiskLevel = string(operations.RiskMedium)
	op.AutonomyLevel = string(operations.AutonomyApproval)
	classified, err := service.Transition(op, operations.StatusClassified, string(operations.OwnerHAI), "", "classified for owner review")
	if err != nil {
		t.Fatal(err)
	}
	awaiting, err := service.Transition(*classified, operations.StatusAwaitingApproval, string(operations.OwnerHAI), "", "awaiting owner")
	if err != nil {
		t.Fatal(err)
	}

	module := phase2.NewModule(service, phase2.Config{
		OwnerUserID: configuredWorkerOwner, WorkspaceID: "local", WorkspaceDir: filepath.Join(t.TempDir(), "workspace"),
	})
	engine := gin.New()
	api := engine.Group("/api/v1")
	api.Use(identityMiddleware())
	initializePhase2Routes(api, module.Handler())
	path := "/api/v1/operations/" + awaiting.ID.String()

	otherOwner := performSourceApprovalIdentityRequest(engine, http.MethodPost, path+"/approve", `{}`, "different-principal", "owner")
	if otherOwner.Code != http.StatusNotFound {
		t.Fatalf("different owner approval status=%d body=%s, want scoped not-found", otherOwner.Code, otherOwner.Body.String())
	}
	if !strings.Contains(otherOwner.Body.String(), `"error":"operation not found"`) || strings.Contains(otherOwner.Body.String(), awaiting.Title) {
		t.Fatalf("cross-owner response disclosed operation details: %s", otherOwner.Body.String())
	}
	viewerPreview := performSourceApprovalIdentityRequest(engine, http.MethodGet, path+"/approval-preview", "", owner, "viewer")
	if viewerPreview.Code != http.StatusOK {
		t.Fatalf("viewer preview status=%d body=%s", viewerPreview.Code, viewerPreview.Body.String())
	}
	var preview struct {
		Revision struct {
			Version int64  `json:"version"`
			Digest  string `json:"revisionDigest"`
		} `json:"revision"`
	}
	if err := json.Unmarshal(viewerPreview.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"expectedVersion":%d,"revisionDigest":%q}`, preview.Revision.Version, preview.Revision.Digest)
	viewerApproval := performSourceApprovalIdentityRequest(engine, http.MethodPost, path+"/approve", body, owner, "viewer")
	if viewerApproval.Code != http.StatusForbidden {
		t.Fatalf("viewer approval status=%d body=%s, want forbidden", viewerApproval.Code, viewerApproval.Body.String())
	}
	current, err := service.Get(owner, "local", awaiting.ID)
	if err != nil || current.Status != string(operations.StatusAwaitingApproval) {
		t.Fatalf("unauthorized requests changed operation: current=%#v err=%v", current, err)
	}
	approved := performSourceApprovalIdentityRequest(engine, http.MethodPost, path+"/approve", body, owner, "owner")
	if approved.Code != http.StatusOK {
		t.Fatalf("owner exact-revision approval status=%d body=%s", approved.Code, approved.Body.String())
	}

	workerOwnedSource := uuid.New()
	workerOperation, err := service.Ingest(operations.NewOperationInput{
		OwnerUserID: configuredWorkerOwner, WorkspaceID: "local", Title: "Review scheduled-feed item",
		Description: "Prepare a local review note", OperationType: "review", SourceType: "email",
		SourceID: &workerOwnedSource, SourceRevisionHash: strings.Repeat("d", 64), DedupeKey: "worker-owner-" + workerOwnedSource.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	workerCandidate := workerOperation.Operation
	workerCandidate.CurrentDecision = string(operations.DecisionAskRobert)
	workerCandidate.RequiresApproval = true
	workerCandidate.RiskLevel = string(operations.RiskMedium)
	workerCandidate.AutonomyLevel = string(operations.AutonomyApproval)
	classified, err = service.Transition(workerCandidate, operations.StatusClassified, string(operations.OwnerHAI), "", "classified")
	if err != nil {
		t.Fatal(err)
	}
	workerAwaiting, err := service.Transition(*classified, operations.StatusAwaitingApproval, string(operations.OwnerHAI), "", "awaiting owner")
	if err != nil {
		t.Fatal(err)
	}
	principal := uuid.NewString()
	workerPath := "/api/v1/operations/" + workerAwaiting.ID.String()
	unauthorizedWorkerPreview := performSourceApprovalIdentityRequest(engine, http.MethodGet, workerPath+"/approval-preview", "", principal, "viewer")
	if unauthorizedWorkerPreview.Code != http.StatusNotFound {
		t.Fatalf("viewer access to configured worker scope status=%d body=%s", unauthorizedWorkerPreview.Code, unauthorizedWorkerPreview.Body.String())
	}
	ownerPreview := performSourceApprovalIdentityRequest(engine, http.MethodGet, workerPath+"/approval-preview", "", principal, "owner")
	if ownerPreview.Code != http.StatusOK {
		t.Fatalf("installation owner cannot preview worker-owned operation: status=%d body=%s", ownerPreview.Code, ownerPreview.Body.String())
	}
	var workerRevision struct {
		Revision struct {
			Version int64  `json:"version"`
			Digest  string `json:"revisionDigest"`
		} `json:"revision"`
	}
	if err := json.Unmarshal(ownerPreview.Body.Bytes(), &workerRevision); err != nil {
		t.Fatal(err)
	}
	workerApprovalBody := fmt.Sprintf(`{"expectedVersion":%d,"revisionDigest":%q}`, workerRevision.Revision.Version, workerRevision.Revision.Digest)
	workerApproval := performSourceApprovalIdentityRequest(engine, http.MethodPost, workerPath+"/approve", workerApprovalBody, principal, "owner")
	if workerApproval.Code != http.StatusOK {
		t.Fatalf("verified installation owner could not approve worker-scope operation: status=%d body=%s", workerApproval.Code, workerApproval.Body.String())
	}
	var workerApprovalResponse struct {
		ApprovalReceipt operations.SourceApprovalReceipt `json:"approvalReceipt"`
	}
	if err := json.Unmarshal(workerApproval.Body.Bytes(), &workerApprovalResponse); err != nil {
		t.Fatal(err)
	}
	if workerApprovalResponse.ApprovalReceipt.ApprovedBy != principal || workerApprovalResponse.ApprovalReceipt.OwnerUserID != configuredWorkerOwner {
		t.Fatalf("configured worker owner mismatch lost accurate principal/scope audit: %#v", workerApprovalResponse.ApprovalReceipt)
	}
}

func performSourceApprovalIdentityRequest(engine *gin.Engine, method, path, body, subject, role string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	now := time.Now().UTC()
	request.Header.Set("Authorization", "Bearer "+identity.SignToken(identity.Claims{
		Subject: subject, Role: role, Issuer: "hai-idp", Audience: "hai", TokenType: "access",
		IssuedAt: now.Unix(), Expiry: now.Add(time.Hour).Unix(),
	}, config.AppConfig.JWTSecret))
	engine.ServeHTTP(recorder, request)
	return recorder
}
