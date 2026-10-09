package hostruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestHandlerRequiresDedicatedBridgeToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(newTestService(newMemoryRepository()), Config{Enabled: true, Token: strings.Repeat("a", 32)})
	router := gin.New()
	handler.RegisterRoutes(router)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/leases", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestHandlerBlocksLeaseConfirmationWhenIsolationIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := strings.Repeat("b", 32)
	handler := NewHandler(NewService(newMemoryRepository()), Config{Enabled: true, Token: token})
	router := gin.New()
	handler.RegisterRoutes(router)

	leaseRequest := httptest.NewRequest(http.MethodPost, "/leases", nil)
	leaseRequest.Header.Set("Authorization", "Bearer "+token)
	leaseResponse := httptest.NewRecorder()
	router.ServeHTTP(leaseResponse, leaseRequest)
	if leaseResponse.Code != http.StatusNoContent {
		t.Fatalf("lease status = %d, want empty queue while execution is blocked", leaseResponse.Code)
	}

	jobID := "11111111-1111-4111-8111-111111111111"
	confirmRequest := httptest.NewRequest(http.MethodPost, "/leases/"+jobID+"/confirm", bytes.NewBufferString(`{"leaseToken":"`+token+`"}`))
	confirmRequest.Header.Set("Authorization", "Bearer "+token)
	confirmRequest.Header.Set("Content-Type", "application/json")
	confirmResponse := httptest.NewRecorder()
	router.ServeHTTP(confirmResponse, confirmRequest)
	if confirmResponse.Code != http.StatusLocked || !strings.Contains(confirmResponse.Body.String(), `"code":"execution_isolation_unavailable"`) {
		t.Fatalf("confirmation status = %d: %s; want explicit isolation block", confirmResponse.Code, confirmResponse.Body.String())
	}
}

func TestHandlerLeasesAndCompletesBoundedJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(newMemoryRepository())
	if _, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-1",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	token := strings.Repeat("a", 32)
	handler := NewHandler(service, Config{Enabled: true, Token: token})
	router := gin.New()
	handler.RegisterRoutes(router)

	leaseRequest := httptest.NewRequest(http.MethodPost, "/leases", nil)
	leaseRequest.Header.Set("Authorization", "Bearer "+token)
	leaseResponse := httptest.NewRecorder()
	router.ServeHTTP(leaseResponse, leaseRequest)
	if leaseResponse.Code != http.StatusOK {
		t.Fatalf("lease status = %d: %s", leaseResponse.Code, leaseResponse.Body.String())
	}
	var lease Lease
	if err := json.Unmarshal(leaseResponse.Body.Bytes(), &lease); err != nil {
		t.Fatalf("decode lease: %v", err)
	}
	confirmPayload := `{"leaseToken":"` + lease.Token + `"}`
	confirmRequest := httptest.NewRequest(http.MethodPost, "/leases/"+lease.Job.ID.String()+"/confirm", bytes.NewBufferString(confirmPayload))
	confirmRequest.Header.Set("Authorization", "Bearer "+token)
	confirmRequest.Header.Set("Content-Type", "application/json")
	confirmResponse := httptest.NewRecorder()
	router.ServeHTTP(confirmResponse, confirmRequest)
	if confirmResponse.Code != http.StatusNoContent {
		t.Fatalf("confirm status = %d: %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: lease.ApprovalDigest, StopRevision: lease.StopRevision}
	beginPayload, _ := json.Marshal(startProtocolRequest{LeaseToken: lease.Token, IntentID: binding.IntentID, ApprovalDigest: binding.ApprovalDigest, StopRevision: binding.StopRevision})
	beginRequest := httptest.NewRequest(http.MethodPost, "/leases/"+lease.Job.ID.String()+"/start-intent", bytes.NewReader(beginPayload))
	beginRequest.Header.Set("Authorization", "Bearer "+token)
	beginRequest.Header.Set("Content-Type", "application/json")
	beginResponse := httptest.NewRecorder()
	router.ServeHTTP(beginResponse, beginRequest)
	if beginResponse.Code != http.StatusOK {
		t.Fatalf("start-intent status = %d: %s", beginResponse.Code, beginResponse.Body.String())
	}
	ackRequest := httptest.NewRequest(http.MethodPost, "/leases/"+lease.Job.ID.String()+"/start-ack", bytes.NewReader(beginPayload))
	ackRequest.Header.Set("Authorization", "Bearer "+token)
	ackRequest.Header.Set("Content-Type", "application/json")
	ackResponse := httptest.NewRecorder()
	router.ServeHTTP(ackResponse, ackRequest)
	if ackResponse.Code != http.StatusNoContent {
		t.Fatalf("start-ack status = %d: %s", ackResponse.Code, ackResponse.Body.String())
	}

	missingExitCodeRequest := httptest.NewRequest(http.MethodPost, "/leases/"+lease.Job.ID.String()+"/complete", bytes.NewBufferString(`{"leaseToken":"`+lease.Token+`","output":"completed"}`))
	missingExitCodeRequest.Header.Set("Authorization", "Bearer "+token)
	missingExitCodeRequest.Header.Set("Content-Type", "application/json")
	missingExitCodeResponse := httptest.NewRecorder()
	router.ServeHTTP(missingExitCodeResponse, missingExitCodeRequest)
	if missingExitCodeResponse.Code != http.StatusBadRequest {
		t.Fatalf("completion without exit code status = %d, want %d", missingExitCodeResponse.Code, http.StatusBadRequest)
	}

	payload := `{"leaseToken":"` + lease.Token + `","exitCode":0,"output":"completed"}`
	completeRequest := httptest.NewRequest(http.MethodPost, "/leases/"+lease.Job.ID.String()+"/complete", bytes.NewBufferString(payload))
	completeRequest.Header.Set("Authorization", "Bearer "+token)
	completeRequest.Header.Set("Content-Type", "application/json")
	completeResponse := httptest.NewRecorder()
	router.ServeHTTP(completeResponse, completeRequest)
	if completeResponse.Code != http.StatusOK {
		t.Fatalf("complete status = %d: %s", completeResponse.Code, completeResponse.Body.String())
	}

	replayResponse := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/leases/"+lease.Job.ID.String()+"/complete", bytes.NewBufferString(payload))
	replayRequest.Header.Set("Authorization", "Bearer "+token)
	replayRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusConflict {
		t.Fatalf("replay completion status = %d, want %d", replayResponse.Code, http.StatusConflict)
	}
}

func TestHandlerBlocksFinalConfirmationWhenEmergencyStopStartsAfterLease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(newMemoryRepository())
	if _, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-confirm-stopped",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	token := strings.Repeat("a", 32)
	handler := NewHandler(service, Config{Enabled: true, Token: token})
	router := gin.New()
	handler.RegisterRoutes(router)

	leaseRequest := httptest.NewRequest(http.MethodPost, "/leases", nil)
	leaseRequest.Header.Set("Authorization", "Bearer "+token)
	leaseResponse := httptest.NewRecorder()
	router.ServeHTTP(leaseResponse, leaseRequest)
	var lease Lease
	if leaseResponse.Code != http.StatusOK || json.Unmarshal(leaseResponse.Body.Bytes(), &lease) != nil {
		t.Fatalf("lease response = %d: %s", leaseResponse.Code, leaseResponse.Body.String())
	}

	t.Setenv("HAI_EMERGENCY_STOP", "true")
	confirmRequest := httptest.NewRequest(http.MethodPost, "/leases/"+lease.Job.ID.String()+"/confirm", bytes.NewBufferString(`{"leaseToken":"`+lease.Token+`"}`))
	confirmRequest.Header.Set("Authorization", "Bearer "+token)
	confirmRequest.Header.Set("Content-Type", "application/json")
	confirmResponse := httptest.NewRecorder()
	router.ServeHTTP(confirmResponse, confirmRequest)
	if confirmResponse.Code != http.StatusLocked {
		t.Fatalf("confirmation while stopped = %d: %s", confirmResponse.Code, confirmResponse.Body.String())
	}
}

func TestHandlerSignalsDurableCancellationAndAcceptsVerifiedTerminationAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := newMemoryRepository()
	service := newTestService(repository)
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "handler-stop",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	lease, err := service.Lease("windows-dsh", "deepseek-harness")
	if err != nil || lease == nil {
		t.Fatalf("Lease = %#v, %v", lease, err)
	}
	confirmAndAcknowledgeStart(t, service, lease)
	if _, revoked, err := service.CancelTask(context.Background(), job.OwnerIdentity, job.TaskID, job.ID); err != nil || revoked {
		t.Fatalf("CancelTask active execution revoked=%v err=%v; want request, not premature cancellation", revoked, err)
	}

	token := strings.Repeat("s", 32)
	handler := NewHandler(service, Config{Enabled: true, Token: token})
	router := gin.New()
	handler.RegisterRoutes(router)
	request := httptest.NewRequest(http.MethodPost, "/leases/"+job.ID.String()+"/confirm", bytes.NewBufferString(`{"leaseToken":"`+lease.Token+`"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var signal map[string]string
	if response.Code != http.StatusConflict || json.Unmarshal(response.Body.Bytes(), &signal) != nil || signal["code"] != "cancellation_requested" {
		t.Fatalf("heartbeat cancellation signal = %d %s", response.Code, response.Body.String())
	}

	ordinary := httptest.NewRequest(http.MethodPost, "/leases/"+job.ID.String()+"/complete", bytes.NewBufferString(`{"leaseToken":"`+lease.Token+`","exitCode":0}`))
	ordinary.Header.Set("Authorization", "Bearer "+token)
	ordinary.Header.Set("Content-Type", "application/json")
	ordinaryResponse := httptest.NewRecorder()
	router.ServeHTTP(ordinaryResponse, ordinary)
	if ordinaryResponse.Code != http.StatusConflict || !strings.Contains(ordinaryResponse.Body.String(), `"code":"cancellation_requested"`) {
		t.Fatalf("ordinary completion during stop = %d %s; want acknowledgment challenge", ordinaryResponse.Code, ordinaryResponse.Body.String())
	}

	ack := httptest.NewRequest(http.MethodPost, "/leases/"+job.ID.String()+"/complete", bytes.NewBufferString(`{"leaseToken":"`+lease.Token+`","exitCode":-1,"error":"stopped","cancellationRequested":true,"terminationVerified":true}`))
	ack.Header.Set("Authorization", "Bearer "+token)
	ack.Header.Set("Content-Type", "application/json")
	ackResponse := httptest.NewRecorder()
	router.ServeHTTP(ackResponse, ack)
	var cancelled Job
	if ackResponse.Code != http.StatusOK || json.Unmarshal(ackResponse.Body.Bytes(), &cancelled) != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("verified cancellation response = %d %s", ackResponse.Code, ackResponse.Body.String())
	}
	if cancelled.CancelRequestedAt == nil || cancelled.LeaseDigest != "" || cancelled.CompletedAt == nil {
		t.Fatalf("terminal cancellation lacks durable request/lease cleanup: %#v", cancelled)
	}
}

func TestHandlerKeepsWorkerIdleWhenEmergencyStopIsActive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(newMemoryRepository())
	if _, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-stopped",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	token := strings.Repeat("a", 32)
	handler := NewHandler(service, Config{Enabled: true, Token: token})
	router := gin.New()
	handler.RegisterRoutes(router)
	t.Setenv("HAI_EMERGENCY_STOP", "true")

	request := httptest.NewRequest(http.MethodPost, "/leases", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("lease while stopped status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
