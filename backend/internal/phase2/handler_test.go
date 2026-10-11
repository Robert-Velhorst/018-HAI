package phase2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/operations"

	"github.com/gin-gonic/gin"
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
	ops.GET("/dashboard", h.Dashboard)
	ops.GET("/:id", h.GetOperation)
	ops.GET("/:id/events", h.OperationEvents)
	ops.GET("/:id/approvals", h.Approvals)
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

func TestBackgroundRunAndDashboardOverHTTP(t *testing.T) {
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
	if rep.OperationsCreated != 2 || rep.Verified != 1 || rep.AwaitingApproval != 1 {
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
	if dash.DoneWhileAway != 1 || dash.NeedsRobert != 1 {
		t.Fatalf("dashboard counts wrong: %+v", dash)
	}

	// The completed operation is listed and carries an audit trail.
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
	if len(listed.Operations) != 1 {
		t.Fatalf("want 1 completed operation, got %d", len(listed.Operations))
	}
	if listed.Operations[0].VerificationStatus != string(operations.VerificationPassed) {
		t.Fatalf("completed op must be verification-passed")
	}

	w = do(t, r, http.MethodGet, "/operations/"+listed.Operations[0].ID+"/events")
	if w.Code != http.StatusOK {
		t.Fatalf("events: status %d", w.Code)
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
	if len(configuredReceipts) != 1 || len(callerReceipts) != 1 {
		t.Fatalf(
			"execution receipts configured=%d caller=%d, want one owner-scoped receipt each",
			len(configuredReceipts),
			len(callerReceipts),
		)
	}
	if configuredReceipts[0].OwnerIdentity != "local-operator" ||
		callerReceipts[0].OwnerIdentity != "caller-owner" {
		t.Fatalf(
			"execution receipt owners configured=%q caller=%q",
			configuredReceipts[0].OwnerIdentity,
			callerReceipts[0].OwnerIdentity,
		)
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
	if len(listed.Operations) != 1 {
		t.Fatalf("want the high-risk op awaiting approval, got %d", len(listed.Operations))
	}
	// Attempting to run it must be refused (no real runtime in 2A).
	w = do(t, r, http.MethodPost, "/operations/"+listed.Operations[0].ID+"/run")
	if w.Code != http.StatusConflict {
		t.Fatalf("running a non-safe operation must be refused with 409, got %d", w.Code)
	}
}
