package openclawmaintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func testPullAPI(t *testing.T, s *Service, loseNextReceipt *bool) *PullClient {
	t.Helper()
	if err := s.db.Model(&Target{}).Where("id IN ?", []string{"companion", "gateway_core"}).Update("next_check", time.Now().Add(24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	t.Setenv("HAI_OPENCLAW_MAINTENANCE_ENABLED", "true")
	t.Setenv("HAI_OPENCLAW_MAINTENANCE_TOKEN", strings.Repeat("a", 64))
	t.Setenv("BACKEND_API_SHARED_KEY", strings.Repeat("b", 64))
	h := NewHandler(s)
	router := gin.New()
	g := router.Group("/api/v1/openclaw-maintenance-worker", h.WorkerAuth)
	g.POST("/leases", h.Lease)
	g.POST("/leases/:id/confirm", h.Confirm)
	g.POST("/leases/:id/permit", h.Permit)
	g.POST("/leases/:id/complete", h.Complete)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-HAI-Backend-Key") != strings.Repeat("b", 64) {
			w.WriteHeader(401)
			return
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, r)
		if strings.HasSuffix(r.URL.Path, "/complete") && response.Code == http.StatusOK && *loseNextReceipt {
			*loseNextReceipt = false
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(response.Code)
		w.Write(response.Body.Bytes())
	}))
	t.Cleanup(server.Close)
	client, err := NewPullClient(server.URL, strings.Repeat("a", 64), strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestPullClientReportsIndependentBackendHealthRejection(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	completeCheckFor(t, s, "companion")
	if err := s.Policy(ctx, "companion", "auto_install_verified", "test-owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, "companion", "2026.9.1"); err != nil {
		t.Fatal(err)
	}
	s.SetHealthVerifier(func(context.Context) bool { return false })
	loseReceipt := false
	client := testPullAPI(t, s, &loseReceipt)
	installs := 0
	err := client.PollOnceWithCapabilities(ctx, verifiedCompanionCapability(), func(ctx context.Context, job Job, permit func() error) (Report, error) {
		if job.Kind != "apply" || permit() != nil {
			t.Fatal("expected one authorized apply execution")
		}
		installs++
		return Report{Installed: job.Version, Available: job.Version, Evidence: job.Evidence, ProviderMetadataAt: freshTestProviderMetadata(), PublisherVerified: true, PublisherPinStatus: "verified", HealthOK: true, ProcessTreeStatus: "verified", Outcome: "ok"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "recorded as needs_review") {
		t.Fatalf("backend health rejection was reported as success: %v", err)
	}
	if installs != 1 {
		t.Fatalf("apply was executed %d times", installs)
	}
	var job Job
	if err := s.db.Where("target = ? AND kind = ?", "companion", "apply").Order("created_at DESC").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	var submitted Report
	if err := json.Unmarshal([]byte(job.Result), &submitted); err != nil {
		t.Fatal(err)
	}
	if job.Status != "needs_review" || submitted.Outcome != "ok" || !submitted.HealthOK {
		t.Fatalf("worker receipt or HAI rejection was not preserved: status=%s receipt=%+v", job.Status, submitted)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.State != "needs_review" || target.Policy != "observe" {
		t.Fatalf("backend health rejection did not fail closed: state=%s policy=%s", target.State, target.Policy)
	}
	var recoveryChecks int64
	if err := s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "companion", "check", "pending").Count(&recoveryChecks).Error; err != nil || recoveryChecks != 1 {
		t.Fatalf("backend health rejection did not queue one read-only recovery check: count=%d err=%v", recoveryChecks, err)
	}
}

// This test exercises the real HTTP handler, service and PostgreSQL ledger.
// Only the OS installer and independent health verifier are substituted.
func TestPullClientPostgresLifecycleAndLostAcknowledgement(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	loseNextReceipt := false
	client := testPullAPI(t, s, &loseNextReceipt)
	var err error
	if err = s.Check(ctx, "companion"); err != nil {
		t.Fatal(err)
	}
	checks, installs := 0, 0
	execute := func(ctx context.Context, j Job, permit func() error) (Report, error) {
		if j.Kind == "check" {
			checks++
			return checkReport(), nil
		}
		if err := permit(); err != nil {
			return Report{}, err
		}
		installs++
		return Report{Installed: j.Version, Available: j.Version, Evidence: j.Evidence, ProviderMetadataAt: freshTestProviderMetadata(), PublisherVerified: true, PublisherPinStatus: "verified", HealthOK: true, ProcessTreeStatus: "verified", Outcome: "ok"}, nil
	}
	if err = client.PollOnceWithCapabilities(ctx, verifiedCompanionCapability(), execute); err != nil {
		t.Fatal(err)
	}
	if err = s.Policy(ctx, "companion", "auto_install_verified", "test-owner"); err != nil {
		t.Fatal(err)
	}
	if err = s.Apply(ctx, "companion", "2026.9.1"); err != nil {
		t.Fatal(err)
	}
	loseNextReceipt = true
	if err = client.PollOnceWithCapabilities(ctx, verifiedCompanionCapability(), execute); err != nil {
		t.Fatal(err)
	}
	if checks != 1 || installs != 1 || loseNextReceipt {
		t.Fatalf("check=%d install=%d lost=%v", checks, installs, loseNextReceipt)
	}
	var target Target
	if err = s.db.First(&target, "id = ?", "companion").Error; err != nil {
		t.Fatal(err)
	}
	if target.Installed != "2026.9.1" || target.VerifiedAt == nil {
		t.Fatalf("update not persisted and verified: %+v", target)
	}
	var count int64
	if err = s.db.Model(&Job{}).Where("target = ? AND kind = ? AND status = ?", "companion", "apply", "completed").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("completed installs=%d error=%v", count, err)
	}
	if err = client.PollOnceWithCapabilities(ctx, verifiedCompanionCapability(), execute); err != nil {
		t.Fatal(err)
	}
	if installs != 1 {
		t.Fatal("completed installation replayed")
	}
}

// Opt-in real Windows/WSL check: no installation, task execution, provider prompt
// or changes to the live HAI database. Receipts stay in a disposable test schema.
func TestPullClientLiveGatewayCheck(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("TEST_OPENCLAW_MAINTENANCE_LIVE_CHECK") != "true" {
		t.Skip("requires explicit live read-only Windows/WSL check opt-in")
	}
	s := testService(t)
	loseReceipt := false
	client := testPullAPI(t, s, &loseReceipt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.Check(ctx, "gateway_core"); err != nil {
		t.Fatal(err)
	}
	w := NewWorker()
	var observed Report
	err := client.PollOnce(ctx, func(ctx context.Context, j Job, permit func() error) (Report, error) {
		if j.Kind != "check" || j.Target != "gateway_core" {
			return Report{}, fmt.Errorf("live test only permits a Gateway version check")
		}
		r, err := w.Execute(ctx, j)
		observed = r
		return r, err
	})
	if err != nil {
		t.Fatal(err)
	}
	var target Target
	if err := s.db.First(&target, "id = ?", "gateway_core").Error; err != nil {
		t.Fatal(err)
	}
	if target.Installed == "" || target.Installed != observed.Installed || target.Available != observed.Available || target.CheckedAt == nil || target.ReceiptID == "" || target.Policy != "observe" {
		t.Fatal("live version evidence was not persisted under observe-only policy")
	}
	t.Logf("Gateway check persisted: installed=%s available=%s publisherVerified=%t", target.Installed, target.Available, observed.PublisherVerified)
}
