package agentruntime

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/safety"

	"github.com/gin-gonic/gin"
)

func TestOpenClawEcosystemHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	initialPath := filepath.Join(root, "openclaw-main.zip")
	updatedPath := filepath.Join(root, "openclaw-next.zip")
	if err := writeMinimalOpenClawZip(initialPath); err != nil {
		t.Fatalf("create initial ecosystem file: %v", err)
	}
	if err := writeMinimalOpenClawZip(updatedPath); err != nil {
		t.Fatalf("create updated ecosystem file: %v", err)
	}

	adapter := &openClawAdapter{
		enabled:         true,
		executable:      "openclaw",
		workspace:       root,
		workspaceRoot:   root,
		ecosystemPath:   initialPath,
		agentCLIEnabled: true,
		allowedHost:     map[string]bool{"127.0.0.1": true},
	}
	registry := NewRegistry(adapter)
	handler := NewHandlerWithEcosystemMutationAuthorizer(
		registry,
		allowingEcosystemMutationAuthorizer(nil),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	r.GET("/agent-runtimes/openclaw/ecosystem", handler.OpenClawEcosystem)
	r.PATCH("/agent-runtimes/openclaw/ecosystem", handler.SetOpenClawEcosystem)
	r.POST("/agent-runtimes/openclaw/ecosystem/refresh", handler.RefreshOpenClawEcosystem)
	r.POST("/agent-runtimes/openclaw/ecosystem/upload", handler.UploadOpenClawEcosystem)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agent-runtimes/openclaw/ecosystem", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /openclaw/ecosystem status = %d", w.Code)
	}
	var info Info
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode openclaw ecosystem response: %v", err)
	}
	if info.ID != "openclaw" {
		t.Fatalf("expected openclaw runtime info, got %q", info.ID)
	}

	body, err := json.Marshal(map[string]string{"ecosystemPath": updatedPath})
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/agent-runtimes/openclaw/ecosystem", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	addEcosystemAuthorizationHeaders(req)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH /openclaw/ecosystem status = %d, body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode updated openclaw runtime response: %v", err)
	}
	if info.EcosystemPath != updatedPath {
		t.Fatalf("expected ecosystemPath %q, got %q", updatedPath, info.EcosystemPath)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/agent-runtimes/openclaw/ecosystem", bytes.NewBufferString(`{"ecosystemPath":""}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for empty path, got %d", w.Code)
	}

	randomPath := filepath.Join(root, "not-openclaw.txt")
	if err := os.WriteFile(randomPath, []byte("not an ecosystem"), 0o644); err != nil {
		t.Fatalf("create random ecosystem file: %v", err)
	}
	body, err = json.Marshal(map[string]string{"ecosystemPath": randomPath})
	if err != nil {
		t.Fatalf("marshal invalid patch body: %v", err)
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/agent-runtimes/openclaw/ecosystem", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for non-zip ecosystem file, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/agent-runtimes/openclaw/ecosystem/refresh", nil)
	addEcosystemAuthorizationHeaders(req)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /openclaw/ecosystem/refresh status = %d", w.Code)
	}

	invalidBody := &bytes.Buffer{}
	invalidWriter := multipart.NewWriter(invalidBody)
	part, err := invalidWriter.CreateFormFile("ecosystem", "openclaw-main.zip")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("PK\x03\x04fake-zip-content")); err != nil {
		t.Fatalf("write upload content: %v", err)
	}
	if err := invalidWriter.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/agent-runtimes/openclaw/ecosystem/upload", invalidBody)
	req.Header.Set("Content-Type", invalidWriter.FormDataContentType())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid zip upload status 400, got %d", w.Code)
	}

	validZipPath := filepath.Join(root, "openclaw-valid.zip")
	validFile, err := os.Create(validZipPath)
	if err != nil {
		t.Fatalf("create valid zip file: %v", err)
	}
	writerZip := zip.NewWriter(validFile)
	entry, err := writerZip.Create("openclaw-main/package.json")
	if err != nil {
		t.Fatalf("zip entry create: %v", err)
	}
	if _, err := entry.Write([]byte(`{"name":"openclaw","version":"0.0.1"}`)); err != nil {
		t.Fatalf("zip payload write: %v", err)
	}
	if err := writerZip.Close(); err != nil {
		t.Fatalf("close zip file writer: %v", err)
	}
	if err := validFile.Close(); err != nil {
		t.Fatalf("close valid zip file: %v", err)
	}
	zipPayload, err := os.ReadFile(validZipPath)
	if err != nil {
		t.Fatalf("create valid zip payload: %v", err)
	}
	zipBody := &bytes.Buffer{}
	zipWriter := multipart.NewWriter(zipBody)
	part, err = zipWriter.CreateFormFile("ecosystem", filepath.Base(validZipPath))
	if err != nil {
		t.Fatalf("create valid form file: %v", err)
	}
	if _, err := part.Write(zipPayload); err != nil {
		t.Fatalf("write valid zip upload content: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/agent-runtimes/openclaw/ecosystem/upload", zipBody)
	req.Header.Set("Content-Type", zipWriter.FormDataContentType())
	addEcosystemAuthorizationHeaders(req)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected valid zip upload status 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if info.EcosystemPath == "" {
		t.Fatalf("expected upload to update ecosystem path")
	}
}

func TestOpenClawUploadRejectsOversizedMultipartBeforeParsing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewRegistry(testOpenClawAdapter(t.TempDir(), "")))
	router := mutationTestRouter(handler)
	request := httptest.NewRequest(
		http.MethodPost,
		"/agent-runtimes/openclaw/ecosystem/upload",
		bytes.NewBufferString("oversized"),
	)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=hai")
	request.ContentLength = maxOpenClawZipUploadBytes + openClawMultipartOverhead + 1
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized ecosystem upload status = %d, want %d: %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestAgentRuntimeSkillsAndStopHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	adapter := &fakeAdapter{info: Info{ID: "openclaw", Name: "OpenClaw", Enabled: true, Configured: true, ExecutionEnabled: true}}
	handler := NewHandler(NewRegistry(adapter))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	r.GET("/agent-runtimes/:id/skills", handler.Skills)
	r.POST("/agent-runtimes/:id/tasks/:taskId/stop", handler.StopTask)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agent-runtimes/openclaw/skills", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /agent-runtimes/openclaw/skills status = %d, body=%s", w.Code, w.Body.String())
	}
	var skills []Skill
	if err := json.Unmarshal(w.Body.Bytes(), &skills); err != nil {
		t.Fatalf("decode runtime skills: %v", err)
	}
	if len(skills) != 1 || skills[0].RuntimeID != "openclaw" || skills[0].Name != "test" {
		t.Fatalf("skills = %#v", skills)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/agent-runtimes/openclaw/tasks/task-123/stop", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST stop task status = %d, body=%s", w.Code, w.Body.String())
	}
	var stop StopResult
	if err := json.Unmarshal(w.Body.Bytes(), &stop); err != nil {
		t.Fatalf("decode stop result: %v", err)
	}
	if stop.RuntimeID != "openclaw" || stop.TaskID != "task-123" || stop.Status != "blocked" ||
		!strings.Contains(stop.Message, "owner-bound") {
		t.Fatalf("stop result = %#v", stop)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/agent-runtimes/missing/skills", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing runtime skills should return 404, got %d", w.Code)
	}
}

func TestRuntimeMutationHandlersRequireVerifiedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adapter := &fakeAdapter{info: Info{ID: "openclaw", Enabled: true, Configured: true, ExecutionEnabled: true}}
	handler := NewHandler(NewRegistry(adapter))
	r := gin.New()
	r.POST("/agent-runtimes/:id/tasks/:taskId/stop", handler.StopTask)
	r.POST("/agent-runtimes/openclaw/ecosystem/refresh", handler.RefreshOpenClawEcosystem)

	for _, path := range []string{
		"/agent-runtimes/openclaw/tasks/task-123/stop",
		"/agent-runtimes/openclaw/ecosystem/refresh",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d: %s", path, w.Code, http.StatusUnauthorized, w.Body.String())
		}
	}
}

func TestOpenClawMutationFailsClosedWithoutAuthorizer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	archive := filepath.Join(root, "openclaw-main.zip")
	if err := writeMinimalOpenClawZip(archive); err != nil {
		t.Fatalf("write OpenClaw archive: %v", err)
	}
	adapter := testOpenClawAdapter(root, archive)
	handler := NewHandler(NewRegistry(adapter))
	router := mutationTestRouter(handler)

	req := httptest.NewRequest(
		http.MethodPost,
		"/agent-runtimes/openclaw/ecosystem/refresh",
		nil,
	)
	addEcosystemAuthorizationHeaders(req)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing authorizer status=%d body=%s", response.Code, response.Body.String())
	}
	if adapter.inventoryLoaded {
		t.Fatal("missing authorizer mutated the OpenClaw inventory cache")
	}

	get := httptest.NewRecorder()
	router.ServeHTTP(
		get,
		httptest.NewRequest(http.MethodGet, "/agent-runtimes/openclaw/ecosystem", nil),
	)
	if get.Code != http.StatusOK {
		t.Fatalf("read-only ecosystem endpoint status=%d body=%s", get.Code, get.Body.String())
	}
}

func TestOpenClawInvalidMutationDoesNotConsumeAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	archive := filepath.Join(root, "openclaw-main.zip")
	if err := writeMinimalOpenClawZip(archive); err != nil {
		t.Fatalf("write OpenClaw archive: %v", err)
	}
	var calls atomic.Int32
	handler := NewHandlerWithEcosystemMutationAuthorizer(
		NewRegistry(testOpenClawAdapter(root, archive)),
		allowingEcosystemMutationAuthorizer(func(EcosystemMutationAuthorizationRequest) {
			calls.Add(1)
		}),
	)
	router := mutationTestRouter(handler)

	req := httptest.NewRequest(
		http.MethodPatch,
		"/agent-runtimes/openclaw/ecosystem",
		bytes.NewBufferString(`{"ecosystemPath":""}`),
	)
	req.Header.Set("Content-Type", "application/json")
	addEcosystemAuthorizationHeaders(req)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid path status=%d body=%s", response.Code, response.Body.String())
	}

	invalidBody := &bytes.Buffer{}
	writer := multipart.NewWriter(invalidBody)
	part, err := writer.CreateFormFile("ecosystem", "openclaw-main.zip")
	if err != nil {
		t.Fatalf("create invalid form: %v", err)
	}
	if _, err := part.Write([]byte("not a zip")); err != nil {
		t.Fatalf("write invalid form: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close invalid form: %v", err)
	}
	req = httptest.NewRequest(
		http.MethodPost,
		"/agent-runtimes/openclaw/ecosystem/upload",
		invalidBody,
	)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	addEcosystemAuthorizationHeaders(req)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid upload status=%d body=%s", response.Code, response.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid input consumed authorization %d times", calls.Load())
	}
}

func TestOpenClawExactAuthorizedMutationSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	initial := filepath.Join(root, "openclaw-main.zip")
	target := filepath.Join(root, "openclaw-next.zip")
	for _, archive := range []string{initial, target} {
		if err := writeMinimalOpenClawZip(archive); err != nil {
			t.Fatalf("write OpenClaw archive: %v", err)
		}
	}
	var captured EcosystemMutationAuthorizationRequest
	handler := NewHandlerWithEcosystemMutationAuthorizer(
		NewRegistry(testOpenClawAdapter(root, initial)),
		allowingEcosystemMutationAuthorizer(func(request EcosystemMutationAuthorizationRequest) {
			captured = request
		}),
	)
	router := mutationTestRouter(handler)
	body, _ := json.Marshal(map[string]string{"ecosystemPath": target})
	req := httptest.NewRequest(
		http.MethodPatch,
		"/agent-runtimes/openclaw/ecosystem",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	addEcosystemAuthorizationHeaders(req)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("authorized mutation status=%d body=%s", response.Code, response.Body.String())
	}
	if captured.OwnerIdentity != "alice" ||
		captured.ActorIdentity != "alice" ||
		captured.Action != openClawSetPathAction ||
		captured.ResourceType != openClawResourceType ||
		captured.ResourceID != openClawResourceID ||
		captured.RuntimeID != "openclaw" ||
		!isLowerSHA256(captured.EffectDigest) ||
		captured.ApprovalSourceID == "" ||
		captured.ApprovalBindingDigest == "" {
		t.Fatalf("authorization request was not exactly bound: %#v", captured)
	}
	var info Info
	if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode mutation response: %v", err)
	}
	if !sameFilePath(info.EcosystemPath, target) {
		t.Fatalf("ecosystem path=%q want=%q", info.EcosystemPath, target)
	}
}

func TestOpenClawMismatchedAuthorizationReceiptIsDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	initial := filepath.Join(root, "openclaw-main.zip")
	target := filepath.Join(root, "openclaw-next.zip")
	for _, archive := range []string{initial, target} {
		if err := writeMinimalOpenClawZip(archive); err != nil {
			t.Fatalf("write OpenClaw archive: %v", err)
		}
	}
	authorizer := allowingEcosystemMutationAuthorizer(nil)
	handler := NewHandlerWithEcosystemMutationAuthorizer(
		NewRegistry(testOpenClawAdapter(root, initial)),
		EcosystemMutationAuthorizerFunc(func(
			ctx context.Context,
			request EcosystemMutationAuthorizationRequest,
			consumer string,
			target string,
		) (EcosystemMutationAuthorizationReceipt, error) {
			receipt, err := authorizer.AuthorizeAndConsumeEcosystemMutation(
				ctx,
				request,
				consumer,
				target,
			)
			receipt.EffectDigest = strings.Repeat("f", 64)
			return receipt, err
		}),
	)
	router := mutationTestRouter(handler)
	body, _ := json.Marshal(map[string]string{"ecosystemPath": target})
	req := httptest.NewRequest(
		http.MethodPatch,
		"/agent-runtimes/openclaw/ecosystem",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	addEcosystemAuthorizationHeaders(req)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusForbidden {
		t.Fatalf("mismatched receipt status=%d body=%s", response.Code, response.Body.String())
	}
	current, _ := testOpenClawAdapterState(handler)
	if !sameFilePath(current, initial) {
		t.Fatalf("mismatched receipt changed ecosystem path to %q", current)
	}
}

func TestOpenClawEmergencyStopAfterConsumptionBlocksEffect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engaged := false
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(
		func() (bool, string, error) {
			return engaged, "operator stopped ecosystem mutation", nil
		},
	))
	defer restore()

	root := t.TempDir()
	initial := filepath.Join(root, "openclaw-main.zip")
	target := filepath.Join(root, "openclaw-next.zip")
	for _, archive := range []string{initial, target} {
		if err := writeMinimalOpenClawZip(archive); err != nil {
			t.Fatalf("write OpenClaw archive: %v", err)
		}
	}
	handler := NewHandlerWithEcosystemMutationAuthorizer(
		NewRegistry(testOpenClawAdapter(root, initial)),
		allowingEcosystemMutationAuthorizer(func(EcosystemMutationAuthorizationRequest) {
			engaged = true
		}),
	)
	router := mutationTestRouter(handler)
	body, _ := json.Marshal(map[string]string{"ecosystemPath": target})
	req := httptest.NewRequest(
		http.MethodPatch,
		"/agent-runtimes/openclaw/ecosystem",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	addEcosystemAuthorizationHeaders(req)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusLocked {
		t.Fatalf("post-consumption stop status=%d body=%s", response.Code, response.Body.String())
	}
	current, _ := testOpenClawAdapterState(handler)
	if !sameFilePath(current, initial) {
		t.Fatalf("emergency stop changed ecosystem path to %q", current)
	}
}

func TestOpenClawAuthorizedUploadReplacesAndDeletesManagedArchive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldFile, err := os.CreateTemp("", "openclaw-ecosystem-old-*.zip")
	if err != nil {
		t.Fatalf("create old managed archive: %v", err)
	}
	oldPath := oldFile.Name()
	if err := oldFile.Close(); err != nil {
		t.Fatalf("close old managed archive: %v", err)
	}
	if err := writeMinimalOpenClawZip(oldPath); err != nil {
		t.Fatalf("write old managed archive: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(oldPath) })

	root := t.TempDir()
	adapter := testOpenClawAdapter(root, oldPath)
	var captured EcosystemMutationAuthorizationRequest
	handler := NewHandlerWithEcosystemMutationAuthorizer(
		NewRegistry(adapter),
		allowingEcosystemMutationAuthorizer(func(request EcosystemMutationAuthorizationRequest) {
			captured = request
		}),
	)
	router := mutationTestRouter(handler)
	payloadPath := filepath.Join(root, "openclaw-upload.zip")
	if err := writeMinimalOpenClawZip(payloadPath); err != nil {
		t.Fatalf("write upload archive: %v", err)
	}
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("read upload archive: %v", err)
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("ecosystem", "openclaw-upload.zip")
	if err != nil {
		t.Fatalf("create upload form: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write upload form: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close upload form: %v", err)
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/agent-runtimes/openclaw/ecosystem/upload",
		body,
	)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	addEcosystemAuthorizationHeaders(req)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("authorized upload status=%d body=%s", response.Code, response.Body.String())
	}
	if captured.Action != openClawUploadAction ||
		captured.EffectDigest == "" ||
		captured.OwnerIdentity != "alice" {
		t.Fatalf("upload authorization request=%#v", captured)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("previous managed archive was not deleted: %v", err)
	}
	current, _ := adapter.ecosystemState()
	if current == "" || sameFilePath(current, oldPath) {
		t.Fatalf("managed upload did not select a new archive: %q", current)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("selected managed archive does not exist: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(current) })
}

func TestValidateOpenClawZipRejectsUnsafeEntryPath(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "openclaw-unsafe.zip")
	if err := writeZipEntries(zipPath, map[string]string{
		"../escape/package.json": `{"name":"openclaw"}`,
	}); err != nil {
		t.Fatalf("write unsafe zip: %v", err)
	}
	if err := validateOpenClawZip(zipPath); err == nil {
		t.Fatalf("expected unsafe zip path to be rejected")
	}
}

func TestValidateOpenClawZipRejectsNonOpenClawArchive(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "not-openclaw.zip")
	if err := writeZipEntries(zipPath, map[string]string{
		"random-project/package.json":   `{"name":"random-project"}`,
		"random-project/docs/readme.md": "not openclaw",
	}); err != nil {
		t.Fatalf("write non-openclaw zip: %v", err)
	}
	if err := validateOpenClawZip(zipPath); err == nil {
		t.Fatalf("expected non-OpenClaw zip to be rejected")
	}
}

func TestValidateOpenClawZipRejectsDuplicateEntries(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "openclaw-duplicate.zip")
	if err := writeZipEntryList(zipPath, []zipEntry{
		{name: "openclaw-main/package.json", content: `{"name":"openclaw"}`},
		{name: "openclaw-main/PACKAGE.json", content: `{"name":"not-openclaw"}`},
	}); err != nil {
		t.Fatalf("write duplicate zip: %v", err)
	}
	if err := validateOpenClawZip(zipPath); err == nil {
		t.Fatalf("expected duplicate zip entries to be rejected")
	}
}

func TestValidateOpenClawZipAcceptsSkillsOnlyArchive(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "openclaw-main.zip")
	if err := writeZipEntries(zipPath, map[string]string{
		"openclaw-main/.agents/skills/agent-transcript/SKILL.md":                              "transcript skill",
		"openclaw-main/.agents/skills/claw-score/references/completeness/openclaw-app-sdk.md": "reference",
	}); err != nil {
		t.Fatalf("write skills-only zip: %v", err)
	}
	if err := validateOpenClawZip(zipPath); err != nil {
		t.Fatalf("expected skills-only OpenClaw zip to be accepted: %v", err)
	}
}

func TestValidateOpenClawEcosystemPathRejectsRandomDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"random-project"}`), 0o644); err != nil {
		t.Fatalf("write random package: %v", err)
	}
	if err := validateOpenClawEcosystemPath(root); err == nil {
		t.Fatalf("expected random directory to be rejected")
	}
}

func TestValidateOpenClawEcosystemPathAcceptsSkillsOnlyCheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills", "agent-transcript"), 0o755); err != nil {
		t.Fatalf("create skill path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agents", "skills", "agent-transcript", "SKILL.md"), []byte("transcript skill"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	if err := validateOpenClawEcosystemPath(root); err != nil {
		t.Fatalf("expected skills-only checkout to be accepted: %v", err)
	}
}

func TestValidateOpenClawEcosystemPathAcceptsExtractedCheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
		t.Fatalf("create skill marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"openclaw"}`), 0o644); err != nil {
		t.Fatalf("write openclaw package: %v", err)
	}
	if err := validateOpenClawEcosystemPath(root); err != nil {
		t.Fatalf("expected extracted checkout to be accepted: %v", err)
	}
}

func writeMinimalOpenClawZip(path string) error {
	return writeZipEntries(path, map[string]string{
		"openclaw-main/package.json":                    `{"name":"openclaw","version":"0.0.1"}`,
		"openclaw-main/.agents/skills/example/SKILL.md": "example skill",
	})
}

func writeZipEntries(path string, entries map[string]string) error {
	ordered := make([]zipEntry, 0, len(entries))
	for name, content := range entries {
		ordered = append(ordered, zipEntry{name: name, content: content})
	}
	return writeZipEntryList(path, ordered)
}

type zipEntry struct {
	name    string
	content string
}

func writeZipEntryList(path string, entries []zipEntry) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for _, item := range entries {
		entry, err := writer.Create(item.name)
		if err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
		if _, err := entry.Write([]byte(item.content)); err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func mutationTestRouter(handler *Handler) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.GET("/agent-runtimes/openclaw/ecosystem", handler.OpenClawEcosystem)
	router.PATCH("/agent-runtimes/openclaw/ecosystem", handler.SetOpenClawEcosystem)
	router.POST("/agent-runtimes/openclaw/ecosystem/refresh", handler.RefreshOpenClawEcosystem)
	router.POST("/agent-runtimes/openclaw/ecosystem/upload", handler.UploadOpenClawEcosystem)
	return router
}

func testOpenClawAdapter(root string, ecosystemPath string) *openClawAdapter {
	return &openClawAdapter{
		enabled:         true,
		executable:      "openclaw",
		workspace:       root,
		workspaceRoot:   root,
		ecosystemPath:   ecosystemPath,
		agentCLIEnabled: true,
		allowedHost:     map[string]bool{"127.0.0.1": true},
	}
}

func testOpenClawAdapterState(handler *Handler) (string, string) {
	adapter, ok := handler.registry.OpenClawAdapter()
	if !ok || adapter == nil {
		return "", ""
	}
	return adapter.ecosystemState()
}

func addEcosystemAuthorizationHeaders(request *http.Request) {
	request.Header.Set("X-HAI-Idempotency-Key", "openclaw-mutation-test")
	request.Header.Set("X-HAI-Task-ID", "task-openclaw-mutation")
	request.Header.Set(
		"X-HAI-Approval-Source",
		"task-review:11111111-1111-4111-8111-111111111111",
	)
	request.Header.Set(
		"X-HAI-Approval-Binding-Digest",
		strings.Repeat("a", 64),
	)
}

func allowingEcosystemMutationAuthorizer(
	beforeReturn func(EcosystemMutationAuthorizationRequest),
) EcosystemMutationAuthorizer {
	return EcosystemMutationAuthorizerFunc(func(
		_ context.Context,
		request EcosystemMutationAuthorizationRequest,
		consumer string,
		executionTarget string,
	) (EcosystemMutationAuthorizationReceipt, error) {
		if consumer != "agentruntime.openclaw-ecosystem" {
			return EcosystemMutationAuthorizationReceipt{},
				errors.New("unexpected ecosystem mutation consumer")
		}
		if executionTarget != openClawMutationTarget+request.EffectDigest {
			return EcosystemMutationAuthorizationReceipt{},
				errors.New("unexpected ecosystem mutation target")
		}
		if beforeReturn != nil {
			beforeReturn(request)
		}
		now := time.Now().UTC()
		return EcosystemMutationAuthorizationReceipt{
			ReceiptID:             "22222222-2222-4222-8222-222222222222",
			DecisionDigest:        strings.Repeat("d", 64),
			Outcome:               "authorized",
			OwnerIdentity:         request.OwnerIdentity,
			ActorIdentity:         request.ActorIdentity,
			TaskID:                request.TaskID,
			Action:                request.Action,
			Stage:                 request.Stage,
			ResourceType:          request.ResourceType,
			ResourceID:            request.ResourceID,
			RuntimeID:             request.RuntimeID,
			ApprovalSourceID:      request.ApprovalSourceID,
			ApprovalBindingDigest: request.ApprovalBindingDigest,
			ApprovalDecisionID:    "11111111-1111-4111-8111-111111111111",
			ApprovedBy:            "alice",
			ApprovedAt:            now,
			ApprovalExpiresAt:     now.Add(5 * time.Minute),
			EffectDigest:          request.EffectDigest,
			EvaluatedAt:           now,
		}, nil
	})
}
