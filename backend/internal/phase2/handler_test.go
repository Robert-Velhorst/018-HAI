package phase2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestBackgroundRunHTTPStatusIsActionable(t *testing.T) {
	if got := backgroundRunHTTPStatus(background.ErrBusy); got != http.StatusConflict {
		t.Fatalf("busy status = %d, want %d", got, http.StatusConflict)
	}
	if got := backgroundRunHTTPStatus(context.Canceled); got != http.StatusServiceUnavailable {
		t.Fatalf("cancelled status = %d, want %d", got, http.StatusServiceUnavailable)
	}
	if got := backgroundRunHTTPStatus(errors.New("storage unavailable")); got != http.StatusInternalServerError {
		t.Fatalf("unexpected failure status = %d, want %d", got, http.StatusInternalServerError)
	}
}

func TestOperationLookupErrorDoesNotExposeUnexpectedStorageFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	writeOperationLookupError(context, errors.New(`database password=not-for-http at C:\\private`))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	for _, forbidden := range []string{"password", "not-for-http", "C:\\\\private"} {
		if strings.Contains(strings.ToLower(recorder.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
	if !strings.Contains(recorder.Body.String(), "operation details are unavailable") {
		t.Fatalf("response = %s", recorder.Body.String())
	}
}

const feedJSON = `[
  {"externalId":"a1","title":"Organize workspace notes","body":"Consolidate personal notes into a local file"},
  {"externalId":"a2","title":"Pay invoice to landlord","body":"Send payment for the rent invoice"}
]`

func newTestServer(t *testing.T) (*gin.Engine, *Module) {
	t.Helper()
	m := newTestModule(t)
	return newTestRouter(m, "local-operator", true), m
}

func newTestModule(t *testing.T) *Module {
	t.Helper()
	feedsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(feedsDir, "inbox.json"), []byte(feedJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		OwnerUserID:  "local-operator",
		WorkspaceID:  "local",
		WorkspaceDir: t.TempDir(),
		FeedsDir:     feedsDir,
		FeedFiles:    []string{"inbox.json"},
		Mode:         autonomypolicy.ModeAutonomousSafe,
	}
	return NewModuleWithEvidencePackRepository(
		operations.NewService(operations.NewMemoryRepository()),
		cfg,
		newTestExecutionAuthorizationService(t),
		newTestEvidencePackRepository(),
	)
}

func newTestRouter(m *Module, subject string, setSubject bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if setSubject {
		r.Use(func(c *gin.Context) {
			c.Set("subject", subject)
			c.Next()
		})
	}
	h := m.Handler()
	ops := r.Group("/operations")
	ops.GET("", h.ListOperations)
	ops.GET("/overview", h.Overview)
	ops.GET("/dashboard", h.Dashboard)
	ops.GET("/:id", h.GetOperation)
	ops.GET("/:id/events", h.OperationEvents)
	ops.GET("/:id/approvals", h.Approvals)
	ops.GET("/:id/approval-preview", h.ApprovalPreview)
	ops.POST("/:id/approve", h.Approve)
	ops.POST("/:id/reject", h.Reject)
	ops.POST("/:id/later", h.Later)
	ops.POST("/:id/block-similar", h.BlockSimilar)
	ops.POST("/:id/run", h.RunOperation)
	ops.POST("/:id/evidence-pack", h.GenerateEvidencePack)
	r.GET("/evidence-packs/:id", h.GetEvidencePack)
	r.POST("/background/run", h.RunBackground)
	r.GET("/account-feeds", h.ListFeeds)
	return r
}

func do(t *testing.T, r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func createSafeExecutableOperation(t *testing.T, svc *operations.Service) models.Operation {
	t.Helper()
	created, err := svc.Ingest(operations.NewOperationInput{
		OwnerUserID:   "local-operator",
		WorkspaceID:   "local",
		Title:         "Run a bounded safe worker task",
		OperationType: "safe_worker_test",
		SourceType:    "test",
		DedupeKey:     "safe-worker-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("ingest safe operation: %v", err)
	}
	op := created.Operation
	op.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	op.RiskLevel = string(operations.RiskLow)
	op.AutonomyLevel = string(operations.AutonomyAuto)
	op.OwnerType = string(operations.OwnerHAI)
	op.RequiresApproval = false
	classified, err := svc.Transition(op, operations.StatusClassified, "hai", "", "safe execution classified")
	if err != nil {
		t.Fatalf("classify safe operation: %v", err)
	}
	return *classified
}

func TestRunOperationUsesExactDurableClaimAndVerifiesSuccess(t *testing.T) {
	r, module := newTestServer(t)
	op := createSafeExecutableOperation(t, module.svc)

	w := do(t, r, http.MethodPost, "/operations/"+op.ID.String()+"/run")
	if w.Code != http.StatusOK {
		t.Fatalf("run operation: status %d body %s", w.Code, w.Body.String())
	}
	var response struct {
		Operation struct {
			Status string `json:"status"`
		} `json:"operation"`
		Verified bool `json:"verified"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode run response: %v", err)
	}
	if !response.Verified || response.Operation.Status != string(operations.StatusCompleted) {
		t.Fatalf("run response = %#v, want verified completed operation", response)
	}
	artifacts, err := os.ReadDir(module.cfg.WorkspaceDir)
	if err != nil {
		t.Fatalf("read safe-worker workspace: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("safe worker artifacts = %d, want exactly one", len(artifacts))
	}
}

type phase2TestControl struct {
	mode          autonomypolicy.Mode
	emergencyStop bool
}

func (c phase2TestControl) Mode() autonomypolicy.Mode { return c.mode }
func (c phase2TestControl) EmergencyStop() bool       { return c.emergencyStop }

func TestRunOperationRechecksCurrentRuntimePolicy(t *testing.T) {
	tests := []struct {
		name       string
		control    phase2TestControl
		blockRules bool
	}{
		{name: "read only mode", control: phase2TestControl{mode: autonomypolicy.ModeReadOnly}},
		{name: "paused mode", control: phase2TestControl{mode: autonomypolicy.ModePaused}},
		{name: "draft only mode", control: phase2TestControl{mode: autonomypolicy.ModeDraftOnly}},
		{name: "emergency stop", control: phase2TestControl{mode: autonomypolicy.ModeAutonomousSafe, emergencyStop: true}},
		{name: "operator block rule", control: phase2TestControl{mode: autonomypolicy.ModeAutonomousSafe}, blockRules: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, module := newTestServer(t)
			module.control = test.control
			if test.blockRules {
				module.blockRules.Add(BlockRule{
					OperationType: "safe_worker_test",
					Reason:        "operator paused similar work",
				})
			}
			op := createSafeExecutableOperation(t, module.svc)

			response := do(t, r, http.MethodPost, "/operations/"+op.ID.String()+"/run")
			if response.Code != http.StatusConflict {
				t.Fatalf("run under current policy: status %d body %s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "current runtime policy") {
				t.Fatalf("policy rejection is not actionable: %s", response.Body.String())
			}
			stored, err := module.svc.Get("local-operator", "local", op.ID)
			if err != nil || stored.Status != string(operations.StatusClassified) {
				t.Fatalf("operation after denied execution = %#v, %v; want unchanged classified status", stored, err)
			}
			artifacts, err := os.ReadDir(module.cfg.WorkspaceDir)
			if err != nil {
				t.Fatalf("read workspace after denied execution: %v", err)
			}
			if len(artifacts) != 0 {
				t.Fatalf("denied execution created %d workspace artifacts", len(artifacts))
			}
			claim, err := module.svc.ClaimOperation(
				context.Background(), "local-operator", "local", op.ID, uuid.New(), time.Minute,
			)
			if err != nil {
				t.Fatalf("policy denial left the operation claim locked: %v", err)
			}
			if err := module.svc.ReleaseClaim(context.Background(), claim.Claim); err != nil {
				t.Fatalf("release test claim: %v", err)
			}
		})
	}
}

func TestRunOperationClaimFailureHasNoFilesystemEffect(t *testing.T) {
	r, module := newTestServer(t)
	op := createSafeExecutableOperation(t, module.svc)
	claimed, err := module.svc.ClaimOperation(
		context.Background(), "local-operator", "local", op.ID, uuid.New(), time.Minute,
	)
	if err != nil || claimed == nil {
		t.Fatalf("hold competing operation claim: claim=%#v err=%v", claimed, err)
	}

	w := do(t, r, http.MethodPost, "/operations/"+op.ID.String()+"/run")
	if w.Code != http.StatusConflict {
		t.Fatalf("run with competing claim: status %d body %s, want conflict", w.Code, w.Body.String())
	}
	artifacts, err := os.ReadDir(module.cfg.WorkspaceDir)
	if err != nil {
		t.Fatalf("read safe-worker workspace: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("claim failure produced %d filesystem artifacts", len(artifacts))
	}
	stored, err := module.svc.Get("local-operator", "local", op.ID)
	if err != nil || stored.Status != string(operations.StatusClassified) {
		t.Fatalf("operation after rejected claim = %#v, %v; want unchanged classified state", stored, err)
	}
}

func TestExactOperationClaimsAreRaceSafe(t *testing.T) {
	_, module := newTestServer(t)
	op := createSafeExecutableOperation(t, module.svc)
	start := make(chan struct{})
	results := make(chan struct {
		claim *operations.ClaimedOperation
		err   error
	}, 2)
	for range 2 {
		go func(workerID uuid.UUID) {
			<-start
			claim, err := module.svc.ClaimOperation(
				context.Background(), "local-operator", "local", op.ID, workerID, time.Minute,
			)
			results <- struct {
				claim *operations.ClaimedOperation
				err   error
			}{claim: claim, err: err}
		}(uuid.New())
	}
	close(start)

	var winner *operations.ClaimedOperation
	claims := 0
	for range 2 {
		result := <-results
		if result.err == nil && result.claim != nil {
			claims++
			winner = result.claim
			continue
		}
		if !errors.Is(result.err, operations.ErrOperationClaimed) {
			t.Fatalf("competing target claim error = %v, want ErrOperationClaimed", result.err)
		}
	}
	if claims != 1 || winner == nil || winner.Operation.ID != op.ID {
		t.Fatalf("exact operation claim winners = %d, winner=%#v", claims, winner)
	}
	if err := module.svc.ReleaseClaim(context.Background(), winner.Claim); err != nil {
		t.Fatalf("release winning test claim: %v", err)
	}
}

func TestStaleExactClaimCannotProduceEffect(t *testing.T) {
	_, module := newTestServer(t)
	op := createSafeExecutableOperation(t, module.svc)
	stale, err := module.svc.ClaimOperation(
		context.Background(), "local-operator", "local", op.ID, uuid.New(), time.Minute,
	)
	if err != nil || stale == nil {
		t.Fatalf("create first claim: claim=%#v err=%v", stale, err)
	}
	if err := module.svc.ReleaseClaim(context.Background(), stale.Claim); err != nil {
		t.Fatalf("release first claim: %v", err)
	}
	current, err := module.svc.ClaimOperation(
		context.Background(), "local-operator", "local", op.ID, uuid.New(), time.Minute,
	)
	if err != nil || current == nil {
		t.Fatalf("create replacement claim: claim=%#v err=%v", current, err)
	}
	if current.Claim.Generation <= stale.Claim.Generation {
		t.Fatalf("claim generation did not advance: stale=%d current=%d", stale.Claim.Generation, current.Claim.Generation)
	}
	if _, err := background.ExecuteSafeOperationClaimed(
		context.Background(), module.svc, module.broker, stale.Operation, stale.Claim, time.Now().UTC(),
		func(op models.Operation) bool {
			return module.SafeExecutionPolicyAllows(op.Title, op.Description, op.OperationType)
		},
	); !errors.Is(err, operations.ErrClaimLost) {
		t.Fatalf("execute with stale claim error = %v, want ErrClaimLost", err)
	}
	artifacts, err := os.ReadDir(module.cfg.WorkspaceDir)
	if err != nil {
		t.Fatalf("read safe-worker workspace after stale claim: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("stale claim produced %d filesystem artifacts", len(artifacts))
	}
	outcome, err := background.ExecuteSafeOperationClaimed(
		context.Background(), module.svc, module.broker, current.Operation, current.Claim, time.Now().UTC(),
		func(op models.Operation) bool {
			return module.SafeExecutionPolicyAllows(op.Title, op.Description, op.OperationType)
		},
	)
	if err != nil || !outcome.Verified {
		t.Fatalf("execute with current claim = %#v, %v; want verified success", outcome, err)
	}
}

func TestBackgroundRunAndDashboardKeepSourceWorkApprovalGated(t *testing.T) {
	r, _ := newTestServer(t)

	// Trigger a background pass.
	w := do(t, r, http.MethodPost, "/background/run")
	if w.Code != http.StatusOK {
		t.Fatalf("background run: status %d body %s", w.Code, w.Body.String())
	}
	var rep struct {
		OperationsCreated int `json:"operationsCreated"`
		Verified          int `json:"verified"`
		AwaitingApproval  int `json:"awaitingApproval"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.OperationsCreated != 2 || rep.Verified != 0 || rep.AwaitingApproval != 2 {
		t.Fatalf("unexpected report: %+v", rep)
	}

	// Dashboard reflects the results.
	w = do(t, r, http.MethodGet, "/operations/dashboard")
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard: status %d", w.Code)
	}
	var dash operations.Dashboard
	if err := json.Unmarshal(w.Body.Bytes(), &dash); err != nil {
		t.Fatal(err)
	}
	if dash.DoneWhileAway != 0 || dash.NeedsRobert != 2 {
		t.Fatalf("dashboard counts wrong: %+v", dash)
	}

	// Source-fed work remains visible for owner review; a background pass cannot
	// create a completed operation or host effect without an exact owner receipt.
	w = do(t, r, http.MethodGet, "/operations?status=completed")
	var listed struct {
		Operations []struct {
			ID                 string `json:"id"`
			VerificationStatus string `json:"verificationStatus"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Operations) != 0 {
		t.Fatalf("unapproved source operations completed during background pass: %+v", listed.Operations)
	}
	awaiting := do(t, r, http.MethodGet, "/operations?status=awaiting_approval")
	if awaiting.Code != http.StatusOK || !strings.Contains(awaiting.Body.String(), "awaiting_approval") {
		t.Fatalf("owner review queue missing source work: status %d body %s", awaiting.Code, awaiting.Body.String())
	}
}

func TestOverviewReturnsDashboardAndFilteredOperationsInOneResponse(t *testing.T) {
	r, m := newTestServer(t)
	createCompletedSourceOperation(t, r, m)

	w := do(t, r, http.MethodGet, "/operations/overview?status=completed")
	if w.Code != http.StatusOK {
		t.Fatalf("overview: status %d body %s", w.Code, w.Body.String())
	}
	var overview struct {
		Dashboard  operations.Dashboard `json:"dashboard"`
		Operations []struct {
			Status string `json:"status"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	if overview.Dashboard.DoneWhileAway != 1 || overview.Dashboard.NeedsRobert != 1 {
		t.Fatalf("unexpected dashboard: %+v", overview.Dashboard)
	}
	if len(overview.Operations) != 1 || overview.Operations[0].Status != string(operations.StatusCompleted) {
		t.Fatalf("unexpected filtered operations: %+v", overview.Operations)
	}
}

func TestBackgroundRunScopesFeedsAndProcessingToAuthenticatedOwner(t *testing.T) {
	m := newTestModule(t)
	r := newTestRouter(m, "caller-owner", true)

	_, err := m.RunConfiguredBackground(t.Context())
	if err != nil {
		t.Fatalf("configured owner background run: %v", err)
	}
	configuredBefore, err := m.Service().List(operations.Filter{
		OwnerUserID: "local-operator",
		WorkspaceID: "local",
		Limit:       50,
	})
	if err != nil {
		t.Fatalf("list configured owner operations: %v", err)
	}
	if len(configuredBefore) != 2 {
		t.Fatalf("configured run created %d operations, want 2", len(configuredBefore))
	}

	w := do(t, r, http.MethodPost, "/background/run")
	if w.Code != http.StatusOK {
		t.Fatalf("background run: status %d body %s", w.Code, w.Body.String())
	}

	callerOps, err := m.Service().List(operations.Filter{
		OwnerUserID: "caller-owner",
		WorkspaceID: "local",
		Limit:       50,
	})
	if err != nil {
		t.Fatalf("list caller operations: %v", err)
	}
	if len(callerOps) != 2 {
		t.Fatalf("caller-scoped run created %d caller operations, want 2", len(callerOps))
	}
	for _, op := range callerOps {
		if op.OwnerUserID != "caller-owner" {
			t.Fatalf("operation %s owner = %q, want caller-owner", op.ID, op.OwnerUserID)
		}
	}

	configuredAfter, err := m.Service().List(operations.Filter{
		OwnerUserID: "local-operator",
		WorkspaceID: "local",
		Limit:       50,
	})
	if err != nil {
		t.Fatalf("list configured owner operations after caller run: %v", err)
	}
	if len(configuredAfter) != len(configuredBefore) {
		t.Fatalf("caller run changed configured owner operation count from %d to %d", len(configuredBefore), len(configuredAfter))
	}
	configuredReceipts, err := m.execAuth.List(t.Context(), "local-operator", 10)
	if err != nil {
		t.Fatalf("list configured owner execution receipts: %v", err)
	}
	callerReceipts, err := m.execAuth.List(t.Context(), "caller-owner", 10)
	if err != nil {
		t.Fatalf("list caller execution receipts: %v", err)
	}
	if len(configuredReceipts) != 0 || len(callerReceipts) != 0 {
		t.Fatalf(
			"source-derived work executed before owner approval: execution receipts configured=%d caller=%d",
			len(configuredReceipts),
			len(callerReceipts),
		)
	}
	for _, op := range append(configuredBefore, callerOps...) {
		if op.Status != string(operations.StatusAwaitingApproval) {
			t.Fatalf("source-derived operation %s for owner %q has status %q before approval", op.ID, op.OwnerUserID, op.Status)
		}
	}
	before := make(map[string]operationsSnapshot, len(configuredBefore))
	for _, op := range configuredBefore {
		before[op.ID.String()] = operationsSnapshot{status: op.Status, version: op.Version, updatedAt: op.UpdatedAt}
	}
	for _, op := range configuredAfter {
		want, ok := before[op.ID.String()]
		if !ok {
			t.Fatalf("caller run created configured-owner operation %s", op.ID)
		}
		if op.Status != want.status || op.Version != want.version || !op.UpdatedAt.Equal(want.updatedAt) {
			t.Fatalf("caller run mutated configured-owner operation %s", op.ID)
		}
	}
}

type operationsSnapshot struct {
	status    string
	version   int64
	updatedAt time.Time
}

func TestBackgroundRunRejectsMissingOrBlankAuthenticatedOwner(t *testing.T) {
	for _, tc := range []struct {
		name       string
		subject    string
		setSubject bool
	}{
		{name: "missing", setSubject: false},
		{name: "blank", subject: " \t ", setSubject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModule(t)
			r := newTestRouter(m, tc.subject, tc.setSubject)

			w := do(t, r, http.MethodPost, "/background/run")
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body %s", w.Code, http.StatusUnauthorized, w.Body.String())
			}

			for _, owner := range []string{"local-operator", tc.subject} {
				ops, err := m.Service().List(operations.Filter{
					OwnerUserID: owner,
					WorkspaceID: "local",
					Limit:       50,
				})
				if err != nil {
					t.Fatalf("list operations: %v", err)
				}
				if len(ops) != 0 {
					t.Fatalf("rejected request created or processed %d operations for owner %q", len(ops), owner)
				}
			}
		})
	}
}

func TestAccountFeedsListed(t *testing.T) {
	r, _ := newTestServer(t)
	w := do(t, r, http.MethodGet, "/account-feeds")
	if w.Code != http.StatusOK {
		t.Fatalf("feeds: status %d", w.Code)
	}
	var got struct {
		Feeds []struct {
			Name string `json:"name"`
		} `json:"feeds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Feeds) != 1 || got.Feeds[0].Name != "inbox" {
		t.Fatalf("expected the inbox feed to be listed, got %+v", got.Feeds)
	}
}

func TestRunRefusesNonSafeOperation(t *testing.T) {
	r, _ := newTestServer(t)
	// Ingest + classify so the high-risk op reaches awaiting_approval.
	do(t, r, http.MethodPost, "/background/run")

	w := do(t, r, http.MethodGet, "/operations?status=awaiting_approval")
	var listed struct {
		Operations []struct {
			ID string `json:"id"`
		} `json:"operations"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	var highRiskID string
	for _, candidate := range listed.Operations {
		var detail struct {
			RiskLevel string `json:"riskLevel"`
		}
		response := do(t, r, http.MethodGet, "/operations/"+candidate.ID)
		if response.Code != http.StatusOK {
			t.Fatalf("load awaiting operation: status %d body %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode awaiting operation: %v", err)
		}
		if detail.RiskLevel == string(operations.RiskHigh) {
			highRiskID = candidate.ID
			break
		}
	}
	if highRiskID == "" {
		t.Fatalf("expected a high-risk operation awaiting approval, got %d operations", len(listed.Operations))
	}
	// Attempting to run it must be refused (no real runtime in 2A).
	w = do(t, r, http.MethodPost, "/operations/"+highRiskID+"/run")
	if w.Code != http.StatusConflict {
		t.Fatalf("running a non-safe operation must be refused with 409, got %d", w.Code)
	}
}
