package agentruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOpenClawManagedArchiveRollbackIsApprovedAndSurvivesRestart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	initialPath := filepath.Join(root, "initial-openclaw.zip")
	if err := writeZipEntries(initialPath, map[string]string{
		"openclaw-main/package.json": `{"name":"openclaw","version":"1.0.0"}`,
	}); err != nil {
		t.Fatal(err)
	}
	newArchivePath := filepath.Join(root, "new-openclaw.zip")
	if err := writeZipEntries(newArchivePath, map[string]string{
		"openclaw-main/package.json": `{"name":"openclaw","version":"2.0.0"}`,
	}); err != nil {
		t.Fatal(err)
	}
	newArchive, err := os.ReadFile(newArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	adapter := testOpenClawAdapter(root, initialPath)
	var rollbackAuthorizationRequest EcosystemMutationAuthorizationRequest
	handler := NewHandlerWithEcosystemMutationAuthorization(
		NewRegistry(adapter),
		allowingEcosystemMutationAuthorizer(func(request EcosystemMutationAuthorizationRequest) {
			if request.Action == openClawRollbackAction {
				rollbackAuthorizationRequest = request
			}
		}),
		EcosystemMutationApprovalPreparerFunc(func(owner, taskID, digest string) (EcosystemMutationAuthorization, error) {
			return EcosystemMutationAuthorization{
				IdempotencyKey:        "prepared-rollback",
				TaskID:                taskID,
				ApprovalSourceID:      "opscontrol-owner:test",
				ApprovalBindingDigest: digest,
			}, nil
		}),
	)
	router := mutationTestRouter(handler)
	performManagedArchiveUpload(t, router, handler, newArchive)
	selectedAfterUpload, _ := adapter.ecosystemState()
	if sameFilePath(selectedAfterUpload, initialPath) {
		t.Fatal("upload did not select the new archive")
	}
	if !adapter.Info().EcosystemRollbackAvailable {
		t.Fatal("runtime info did not expose the verified rollback option")
	}

	preparedResponse := httptest.NewRecorder()
	preparedRequest := httptest.NewRequest(http.MethodPost, "/agent-runtimes/openclaw/ecosystem/approval/rollback", nil)
	router.ServeHTTP(preparedResponse, preparedRequest)
	if preparedResponse.Code != http.StatusOK {
		t.Fatalf("prepare rollback status=%d body=%s", preparedResponse.Code, preparedResponse.Body.String())
	}
	var authorization EcosystemMutationAuthorization
	if err := json.NewDecoder(preparedResponse.Body).Decode(&authorization); err != nil {
		t.Fatalf("decode rollback authorization: %v", err)
	}

	body, err := json.Marshal(authorization)
	if err != nil {
		t.Fatal(err)
	}
	rollbackResponse := httptest.NewRecorder()
	rollbackRequest := httptest.NewRequest(http.MethodPost, "/agent-runtimes/openclaw/ecosystem/rollback", bytes.NewReader(body))
	rollbackRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rollbackResponse, rollbackRequest)
	if rollbackResponse.Code != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", rollbackResponse.Code, rollbackResponse.Body.String())
	}
	if rollbackAuthorizationRequest.Action != openClawRollbackAction || !rollbackAuthorizationRequest.Reversible {
		t.Fatalf("rollback authorization was not classified as reversible: %#v", rollbackAuthorizationRequest)
	}
	rolledBackPath, _ := adapter.ecosystemState()
	if sameFilePath(rolledBackPath, selectedAfterUpload) {
		t.Fatal("rollback left the newly uploaded archive selected")
	}
	if !adapter.Info().EcosystemRollbackAvailable {
		t.Fatal("runtime info did not expose the previous selection after rollback")
	}
	if _, err := os.Stat(selectedAfterUpload); err != nil {
		t.Fatalf("rollback should retain the archive it switched away from: %v", err)
	}

	t.Setenv("AGENT_RUNTIME_WORKSPACE_ROOT", root)
	t.Setenv("OPENCLAW_WORKSPACE", root)
	t.Setenv("OPENCLAW_ECOSYSTEM_PATH", "")
	restarted := newOpenClawAdapterFromEnv()
	reloadedPath, _ := restarted.ecosystemState()
	if !sameFilePath(reloadedPath, rolledBackPath) {
		t.Fatalf("restart after rollback selected %q, want %q", reloadedPath, rolledBackPath)
	}
}

func TestOpenClawManagedArchiveReloadFailsClosedOnDigestMismatch(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	uploadPath := filepath.Join(root, "new-openclaw.zip")
	if err := writeMinimalOpenClawZip(uploadPath); err != nil {
		t.Fatal(err)
	}
	upload, err := os.ReadFile(uploadPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(upload)
	adapter := testOpenClawAdapter(root, workspace)
	managedPath, err := adapter.persistManagedArchive(bytes.NewReader(upload), hex.EncodeToString(digest[:]), int64(len(upload)))
	if err != nil {
		t.Fatalf("persist test archive: %v", err)
	}
	prepared, err := adapter.prepareEcosystemPath(managedPath, true)
	if err != nil {
		t.Fatalf("prepare selection: %v", err)
	}
	if _, err := adapter.withEcosystemCommitFence(func() error {
		return adapter.commitManagedArchiveSelectionLocked(prepared, hex.EncodeToString(digest[:]), "")
	}); err != nil {
		t.Fatalf("persist selection manifest: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("tampered archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_RUNTIME_WORKSPACE_ROOT", root)
	t.Setenv("OPENCLAW_WORKSPACE", workspace)
	t.Setenv("OPENCLAW_ECOSYSTEM_PATH", workspace)
	restarted := newOpenClawAdapterFromEnv()
	selected, _ := restarted.ecosystemState()
	if selected != "" {
		t.Fatalf("corrupt persisted archive remained selected: %q", selected)
	}
	if !strings.Contains(strings.Join(restarted.Info().MissingConfiguration, " "), "failed integrity validation") {
		t.Fatalf("integrity failure was not surfaced: %#v", restarted.Info().MissingConfiguration)
	}
}

func performManagedArchiveUpload(t *testing.T, router *gin.Engine, handler *Handler, payload []byte) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("ecosystem", "openclaw-upload.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/agent-runtimes/openclaw/ecosystem/upload", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	addExactEcosystemAuthorizationHeaders(t, request, exactUploadEffect(t, handler, payload))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", response.Code, response.Body.String())
	}
}
