package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"automation-hub-backend/internal/safety"
	"github.com/gin-gonic/gin"
)

func TestOpenClawCancelledAfterApprovalDoesNotChangeSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	initial, target := filepath.Join(root, "openclaw-old.zip"), filepath.Join(root, "openclaw-new.zip")
	for _, archive := range []string{initial, target} {
		if err := writeMinimalOpenClawZip(archive); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	approvalCalled := false
	handler := NewHandlerWithEcosystemMutationAuthorizer(NewRegistry(testOpenClawAdapter(root, initial)),
		allowingEcosystemMutationAuthorizer(func(EcosystemMutationAuthorizationRequest) {
			approvalCalled = true
			cancel()
		}))
	body, err := json.Marshal(map[string]string{"ecosystemPath": target})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/agent-runtimes/openclaw/ecosystem", bytes.NewReader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	addExactEcosystemAuthorizationHeaders(t, request, exactSetPathEffect(t, handler, target))
	response := httptest.NewRecorder()
	mutationTestRouter(handler).ServeHTTP(response, request)
	if !approvalCalled {
		t.Fatal("request did not reach the approval boundary")
	}
	current, _ := testOpenClawAdapterState(handler)
	if !sameFilePath(current, initial) {
		t.Fatal("cancelled request changed the ecosystem selection")
	}
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("cancelled mutation returned %d, want 408", response.Code)
	}
}

func TestCommitEcosystemMutationChecksCancellationInsideFence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adapter := testOpenClawAdapter(t.TempDir(), "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The provider is evaluated under the commit fence immediately before the callback.
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		cancel()
		return false, "", nil
	}))
	defer restore()
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
	called := false
	if ctx.Err() != nil {
		t.Fatal("request was cancelled before entering the commit fence")
	}
	proceed, err := commitEcosystemMutation(c, adapter, func() error {
		called = true
		return nil
	})
	if called || proceed || err != nil || response.Code != http.StatusRequestTimeout {
		t.Fatalf("cancelled commit: called=%t proceed=%t err=%v status=%d", called, proceed, err, response.Code)
	}
}

func TestCommitEcosystemMutationDoesNotHideErrorAfterCallbackStarted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	called := false
	proceed, err := commitEcosystemMutation(c, testOpenClawAdapter(t.TempDir(), ""), func() error {
		called = true
		return context.Canceled
	})
	if !called || !proceed || err != context.Canceled || response.Body.Len() != 0 {
		t.Fatalf("callback error must remain inspectable: called=%t proceed=%t err=%v body=%s", called, proceed, err, response.Body.String())
	}
}
