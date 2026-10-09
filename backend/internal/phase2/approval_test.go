package phase2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/operations"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// awaitingApprovalID runs a background pass and returns one source-fed item
// waiting for the owner's revision-bound approval.
func awaitingApprovalID(t *testing.T, r *gin.Engine) string {
	t.Helper()
	do(t, r, http.MethodPost, "/background/run")
	w := do(t, r, http.MethodGet, "/operations?status=awaiting_approval")
	var listed struct {
		Operations []struct {
			ID string `json:"id"`
		} `json:"operations"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed.Operations) == 0 {
		t.Fatal("expected source-fed work in the owner approval queue")
	}
	return listed.Operations[0].ID
}

func approveAndRunExactSourceOperation(t *testing.T, r *gin.Engine, m *Module, id string) {
	t.Helper()
	operationID, _ := uuid.Parse(id)
	previewResponse := do(t, r, http.MethodGet, "/operations/"+id+"/approval-preview")
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("approval preview: status %d body %s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview struct {
		Revision struct {
			Version int64  `json:"version"`
			Digest  string `json:"revisionDigest"`
		} `json:"revision"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil || preview.Revision.Version <= 0 || preview.Revision.Digest == "" {
		t.Fatalf("approval preview payload = %s err=%v", previewResponse.Body.String(), err)
	}
	body := fmt.Sprintf(`{"expectedVersion":%d,"revisionDigest":%q}`, preview.Revision.Version, preview.Revision.Digest)
	approved := doBody(t, r, http.MethodPost, "/operations/"+id+"/approve", body)
	if approved.Code != http.StatusOK {
		t.Fatalf("approve exact source revision: status %d body %s", approved.Code, approved.Body.String())
	}
	run := do(t, r, http.MethodPost, "/operations/"+id+"/run")
	if run.Code != http.StatusOK {
		stored, loadErr := m.svc.Get("local-operator", "local", operationID)
		var receiptErr error
		if loadErr == nil {
			_, receiptErr = m.svc.SourceApprovalForExecution(*stored)
		}
		t.Fatalf("run exactly approved source revision: status %d body %s; stored load=%v receipt=%v", run.Code, run.Body.String(), loadErr, receiptErr)
	}
	var result struct {
		Verified  bool `json:"verified"`
		Operation struct {
			Status string `json:"status"`
		} `json:"operation"`
	}
	if err := json.Unmarshal(run.Body.Bytes(), &result); err != nil || !result.Verified || result.Operation.Status != string(operations.StatusCompleted) {
		t.Fatalf("source operation run = %s err=%v; want verified completion", run.Body.String(), err)
	}
}

func createCompletedSourceOperation(t *testing.T, r *gin.Engine, m *Module) string {
	t.Helper()
	backgroundRun := do(t, r, http.MethodPost, "/background/run")
	if backgroundRun.Code != http.StatusOK {
		t.Fatalf("background run: status %d body %s", backgroundRun.Code, backgroundRun.Body.String())
	}
	queued := do(t, r, http.MethodGet, "/operations?status=awaiting_approval")
	var listed struct {
		Operations []struct {
			ID string `json:"id"`
		} `json:"operations"`
	}
	if queued.Code != http.StatusOK {
		t.Fatalf("list owner approval queue: status %d body %s", queued.Code, queued.Body.String())
	}
	if err := json.Unmarshal(queued.Body.Bytes(), &listed); err != nil || len(listed.Operations) == 0 {
		t.Fatalf("owner approval queue = %s err=%v", queued.Body.String(), err)
	}
	id := listed.Operations[0].ID
	approveAndRunExactSourceOperation(t, r, m, id)
	return id
}

func TestRejectDismissesOperation(t *testing.T) {
	r, _ := newTestServer(t)
	id := awaitingApprovalID(t, r)
	w := do(t, r, http.MethodPost, "/operations/"+id+"/reject")
	if w.Code != http.StatusOK {
		t.Fatalf("reject: status %d body %s", w.Code, w.Body.String())
	}
	got := do(t, r, http.MethodGet, "/operations/"+id)
	var op struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(got.Body.Bytes(), &op)
	if op.Status != string(operations.StatusDismissed) {
		t.Fatalf("rejected op must be dismissed, got %q", op.Status)
	}
}

func TestLaterPostponesOperation(t *testing.T) {
	r, _ := newTestServer(t)
	id := awaitingApprovalID(t, r)
	w := do(t, r, http.MethodPost, "/operations/"+id+"/later")
	if w.Code != http.StatusOK {
		t.Fatalf("later: status %d body %s", w.Code, w.Body.String())
	}
	var op struct {
		Status       string  `json:"status"`
		NextReviewAt *string `json:"nextReviewAt"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &op)
	if op.Status != string(operations.StatusAwaitingApproval) {
		t.Fatalf("postponed op must remain awaiting_approval, got %q", op.Status)
	}
	if op.NextReviewAt == nil {
		t.Fatalf("postponed op must have a next review time")
	}
}

func TestBlockSimilarBlocksAndAutoBlocksFuture(t *testing.T) {
	r, m := newTestServer(t)
	id := awaitingApprovalID(t, r)
	w := do(t, r, http.MethodPost, "/operations/"+id+"/block-similar")
	if w.Code != http.StatusOK {
		t.Fatalf("block-similar: status %d body %s", w.Code, w.Body.String())
	}
	got := do(t, r, http.MethodGet, "/operations/"+id)
	var op struct {
		Status        string `json:"status"`
		OperationType string `json:"operationType"`
	}
	_ = json.Unmarshal(got.Body.Bytes(), &op)
	if op.Status != string(operations.StatusBlocked) {
		t.Fatalf("block-similar op must be blocked, got %q", op.Status)
	}
	// A future operation of the same type must be auto-blocked by the rule.
	blocked, reason := m.blockRules.ShouldBlock(op.OperationType, "another payment task")
	if !blocked || reason == "" {
		t.Fatalf("block rule must auto-block future ops of the same type")
	}
	// Drive it through the worker: ingest a matching op and confirm it blocks.
	in := operations.NewOperationInput{
		OwnerUserID: "local-operator", WorkspaceID: "local", Title: "Pay another landlord invoice",
		Description: "pay the rent invoice", OperationType: op.OperationType, SourceType: "test", DedupeKey: "blk-1",
	}
	if _, err := m.Service().Ingest(in); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Worker().WithBlockRules(m.blockRules).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	blockedList, _ := m.Service().List(operations.Filter{OwnerUserID: "local-operator", WorkspaceID: "local", Status: operations.StatusBlocked})
	if len(blockedList) < 2 {
		t.Fatalf("the matching future operation must be auto-blocked, got %d blocked", len(blockedList))
	}
}

func TestApprovalsListsProvenance(t *testing.T) {
	r, _ := newTestServer(t)
	id := awaitingApprovalID(t, r)
	preview := do(t, r, http.MethodGet, "/operations/"+id+"/approval-preview")
	var binding struct {
		Revision struct {
			Version int64  `json:"version"`
			Digest  string `json:"revisionDigest"`
		} `json:"revision"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &binding); err != nil || binding.Revision.Version <= 0 || binding.Revision.Digest == "" {
		t.Fatalf("approval preview = %s err=%v", preview.Body.String(), err)
	}
	body := fmt.Sprintf(`{"expectedVersion":%d,"revisionDigest":%q}`, binding.Revision.Version, binding.Revision.Digest)
	if response := doBody(t, r, http.MethodPost, "/operations/"+id+"/approve", body); response.Code != http.StatusOK {
		t.Fatalf("approve exact revision: status %d body %s", response.Code, response.Body.String())
	}
	w := do(t, r, http.MethodGet, "/operations/"+id+"/approvals")
	if w.Code != http.StatusOK {
		t.Fatalf("approvals: status %d", w.Code)
	}
	var res struct {
		Approvals []map[string]any `json:"approvals"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Approvals) < 2 { // awaiting_approval + approved
		t.Fatalf("approvals must include the awaiting + approved provenance, got %d", len(res.Approvals))
	}
}

func TestSourceApprovalHTTPRejectsMissingReviewBindingWithoutMutation(t *testing.T) {
	r, _ := newTestServer(t)
	id := awaitingApprovalID(t, r)
	response := doBody(t, r, http.MethodPost, "/operations/"+id+"/approve", `{}`)
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("approval without reviewed binding status=%d body=%s, want 428", response.Code, response.Body.String())
	}
	current := do(t, r, http.MethodGet, "/operations/"+id)
	var operation struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &operation); err != nil || operation.Status != string(operations.StatusAwaitingApproval) {
		t.Fatalf("missing binding mutated operation: response=%s err=%v", current.Body.String(), err)
	}
}

func TestSourceApprovalHTTPRequiresAndReturnsExactRevisionReceipt(t *testing.T) {
	r, _ := newTestServer(t)
	id := awaitingApprovalID(t, r)
	previewResponse := do(t, r, http.MethodGet, "/operations/"+id+"/approval-preview")
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("approval preview: status %d body %s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview struct {
		Revision struct {
			Version int64  `json:"version"`
			Digest  string `json:"revisionDigest"`
		} `json:"revision"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil || preview.Revision.Version <= 0 || preview.Revision.Digest == "" {
		t.Fatalf("approval preview payload = %s err=%v", previewResponse.Body.String(), err)
	}
	body := fmt.Sprintf(`{"expectedVersion":%d,"revisionDigest":%q}`, preview.Revision.Version, preview.Revision.Digest)
	approvedResponse := doBody(t, r, http.MethodPost, "/operations/"+id+"/approve", body)
	if approvedResponse.Code != http.StatusOK {
		t.Fatalf("approve exact revision: status %d body %s", approvedResponse.Code, approvedResponse.Body.String())
	}
	var approved struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		ApprovalReceipt struct {
			OperationID    string `json:"operationId"`
			Version        int64  `json:"version"`
			RevisionDigest string `json:"revisionDigest"`
		} `json:"approvalReceipt"`
	}
	if err := json.Unmarshal(approvedResponse.Body.Bytes(), &approved); err != nil || approved.Status != string(operations.StatusApproved) {
		t.Fatalf("approved response = %s err=%v", approvedResponse.Body.String(), err)
	}
	if approved.ApprovalReceipt.OperationID != approved.ID || approved.ApprovalReceipt.Version != preview.Revision.Version || approved.ApprovalReceipt.RevisionDigest != preview.Revision.Digest {
		t.Fatalf("response receipt does not match preview: %#v / %#v", approved, preview)
	}
}

func TestSourceApprovalHTTPRejectsStaleReviewPreview(t *testing.T) {
	r, module := newTestServer(t)
	id := awaitingApprovalID(t, r)
	preview := do(t, r, http.MethodGet, "/operations/"+id+"/approval-preview")
	var value struct {
		Revision struct {
			Version int64  `json:"version"`
			Digest  string `json:"revisionDigest"`
		} `json:"revision"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	op, err := module.Service().Get("local-operator", "local", uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	op.Title += " changed after preview"
	if _, err := module.Service().Save(*op, "operation_updated", string(operations.OwnerHAI), "updated after approval preview"); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"expectedVersion":%d,"revisionDigest":%q}`, value.Revision.Version, value.Revision.Digest)
	response := doBody(t, r, http.MethodPost, "/operations/"+id+"/approve", body)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale approval status=%d body=%s, want conflict", response.Code, response.Body.String())
	}
}

func doBody(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	return response
}
