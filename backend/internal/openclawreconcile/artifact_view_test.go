package openclawreconcile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestFinalizeArtifactViewKeepsExhaustionVisibleAndMetadataAvailable(t *testing.T) {
	nextAttempt := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	item := models.OpenClawGatewayArtifactReceipt{ArtifactDigest: strings.Repeat("a", 64)}
	view := &ArtifactView{
		Attempts:      8,
		NextAttemptAt: &nextAttempt,
		Items:         []models.OpenClawGatewayArtifactReceipt{item},
	}
	receipt := models.OpenClawGatewaySessionReceipt{Status: "terminal", TerminalStatus: "completed"}

	finalizeArtifactViewState(view, receipt)

	if view.State != "retry_exhausted" {
		t.Fatalf("state = %q, want retry_exhausted", view.State)
	}
	if view.NextAttemptAt != nil {
		t.Fatalf("exhausted collection advertises an automatic retry at %v", view.NextAttemptAt)
	}
	if len(view.Items) != 1 || view.Items[0].ArtifactDigest != item.ArtifactDigest {
		t.Fatalf("existing artifact metadata was not preserved: %#v", view.Items)
	}
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal public artifact view: %v", err)
	}
	if strings.Contains(string(body), "nextAttemptAt") || !strings.Contains(string(body), item.ArtifactDigest) {
		t.Fatalf("public view advertises a retry or hides existing metadata: %s", body)
	}
}

func TestOpenClawAuditEventsOnlyExposeTheirOwnRetainedArtifacts(t *testing.T) {
	for _, launchType := range []string{
		"agent_runtime_openclaw_artifacts",
		"agent_runtime_openclaw_usage",
		"agent_runtime_openclaw_artifact_download",
		"agent_runtime_openclaw_artifact_retain",
		"agent_runtime_openclaw_artifact_forget",
		"agent_runtime_openclaw_artifact_provenance_upgrade",
		"agent_runtime_openclaw_review",
	} {
		if !requiresRetainedArtifactForEvent(launchType) {
			t.Errorf("audit event %q may expose unretained execution files", launchType)
		}
	}
	for _, launchType := range []string{"agent_runtime_openclaw_terminal", "agent_runtime", "agent_runtime_host_completion"} {
		if requiresRetainedArtifactForEvent(launchType) {
			t.Errorf("source execution event %q was treated as an audit event", launchType)
		}
	}

	first := models.OpenClawGatewayArtifactReceipt{ArtifactDigest: strings.Repeat("a", 64)}
	second := models.OpenClawGatewayArtifactReceipt{ArtifactDigest: strings.Repeat("b", 64)}
	filtered := filterArtifactsToRetained([]models.OpenClawGatewayArtifactReceipt{first, second}, map[string]struct{}{second.ArtifactDigest: {}})
	if len(filtered) != 1 || filtered[0].ArtifactDigest != second.ArtifactDigest {
		t.Fatalf("audit event exposed artifacts other than its exact retained copy: %#v", filtered)
	}
}

func TestFinalizeArtifactViewReportsActiveFinalClaimInsteadOfExhaustion(t *testing.T) {
	nextAttempt := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	view := &ArtifactView{Attempts: artifactCollectionMax, NextAttemptAt: &nextAttempt, collectionActive: true, Items: []models.OpenClawGatewayArtifactReceipt{}}
	receipt := models.OpenClawGatewaySessionReceipt{Status: "terminal", TerminalStatus: "completed"}

	finalizeArtifactViewState(view, receipt)

	if view.State != "collecting" || view.NextAttemptAt != nil {
		t.Fatalf("active final claim should be shown as collecting, not exhausted or due: %#v", view)
	}
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "collectionActive") || strings.Contains(string(body), "claimToken") {
		t.Fatalf("internal claim data leaked in public view: %s", body)
	}
}

func TestFinalizeArtifactViewTreatsExpiredFinalClaimAsExhausted(t *testing.T) {
	view := &ArtifactView{Attempts: artifactCollectionMax, collectionActive: false, Items: []models.OpenClawGatewayArtifactReceipt{}}
	receipt := models.OpenClawGatewaySessionReceipt{Status: "terminal", TerminalStatus: "completed"}

	finalizeArtifactViewState(view, receipt)

	if view.State != "retry_exhausted" {
		t.Fatalf("expired final claim state = %q, want retry_exhausted", view.State)
	}
}

func TestFinalizeArtifactViewKeepsLegacyMetadataStateBeforeExhaustion(t *testing.T) {
	nextAttempt := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	view := &ArtifactView{
		Attempts:      7,
		NextAttemptAt: &nextAttempt,
		Items:         []models.OpenClawGatewayArtifactReceipt{{ArtifactDigest: strings.Repeat("b", 64)}},
	}
	receipt := models.OpenClawGatewaySessionReceipt{Status: "terminal", TerminalStatus: "completed"}

	finalizeArtifactViewState(view, receipt)

	if view.State != "metadata_available" || view.NextAttemptAt == nil {
		t.Fatalf("pre-exhaustion legacy state changed: %#v", view)
	}
}

type artifactViewFake struct {
	owner      string
	calls      int
	eventID    uuid.UUID
	err        error
	auditErr   error
	auditCalls int
}

func (f *artifactViewFake) RecordArtifactDownload(context.Context, ArtifactDownloadBinding, string, int) error {
	f.auditCalls++
	return f.auditErr
}
func (f *artifactViewFake) ArtifactsForEvent(context.Context, string, uuid.UUID) (*ArtifactView, error) {
	return &ArtifactView{State: "pending"}, nil
}
func (f *artifactViewFake) ArtifactForEvent(_ context.Context, owner string, eventID uuid.UUID, digest string) (*ArtifactDownloadBinding, error) {
	f.owner = owner
	f.calls++
	if f.eventID == uuid.Nil {
		f.eventID = eventID
	}
	return &ArtifactDownloadBinding{Receipt: agentruntime.OpenClawGatewayReceipt{OwnerIdentity: owner}, Descriptor: agentruntime.GatewayArtifactDescriptor{Digest: digest}, EventID: f.eventID}, f.err
}

type artifactDownloaderFake struct {
	calls       int
	err         error
	badChecksum bool
}

func (f *artifactDownloaderFake) DownloadOpenClawGatewayArtifact(context.Context, agentruntime.OpenClawGatewayReceipt, agentruntime.GatewayArtifactDescriptor) (*agentruntime.GatewayArtifactContent, error) {
	f.calls++
	if f.badChecksum {
		return &agentruntime.GatewayArtifactContent{Data: []byte("hi"), ContentSHA256: strings.Repeat("0", 64)}, nil
	}
	return &agentruntime.GatewayArtifactContent{Data: []byte("hi"), ContentSHA256: "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4"}, f.err
}

func TestArtifactDownloadFailsClosedBeforeSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		auditErr    error
		badChecksum bool
		auditCalls  int
	}{
		{"audit unavailable", errors.New("private-audit-secret"), false, 1},
		{"checksum invalid", nil, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &artifactViewFake{auditErr: tc.auditErr}
			downloader := &artifactDownloaderFake{badChecksum: tc.badChecksum}
			engine := gin.New()
			engine.GET("/download", func(c *gin.Context) {
				c.Set(identity.ContextSubjectKey, "alice")
				c.Params = gin.Params{{Key: "eventId", Value: uuid.NewString()}, {Key: "digest", Value: strings.Repeat("a", 64)}}
				NewArtifactHandler(store, downloader).Download(c)
			})
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest("GET", "/download", nil))
			if w.Code != 503 || store.auditCalls != tc.auditCalls || downloader.calls != 1 || w.Header().Get("Content-Disposition") != "" || w.Header().Get("X-Content-SHA256") != "" || strings.Contains(w.Body.String(), "private-audit-secret") {
				t.Fatalf("content escaped integrity/audit gate: status=%d audit=%d body=%s", w.Code, store.auditCalls, w.Body.String())
			}
		})
	}
}

func TestArtifactDownloadRejectsEventIDMismatchBeforeContentAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	eventID := uuid.New()
	store := &artifactViewFake{eventID: uuid.New()}
	downloader := &artifactDownloaderFake{}
	engine := gin.New()
	engine.GET("/download", func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Params = gin.Params{{Key: "eventId", Value: eventID.String()}, {Key: "digest", Value: strings.Repeat("a", 64)}}
		NewArtifactHandler(store, downloader).Download(c)
	})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/download", nil))
	if response.Code != http.StatusServiceUnavailable || store.calls != 1 || downloader.calls != 0 || store.auditCalls != 0 || response.Header().Get("Content-Disposition") != "" {
		t.Fatalf("mismatched event binding reached content or audit path: status=%d reads=%d downloads=%d audits=%d body=%s", response.Code, store.calls, downloader.calls, store.auditCalls, response.Body.String())
	}
}

func TestArtifactGetReportsUnavailableWhenRetentionArchiveIsNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	handler := NewArtifactHandler(&artifactViewFake{}, nil)
	engine.GET("/events/:eventId/artifacts", handler.Get)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/events/"+uuid.NewString()+"/artifacts", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"retentionAvailable":false`) || !strings.Contains(response.Body.String(), `"retentionStatus":"unavailable"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestArtifactRetentionKeyErrorDoesNotTreatPrefixAsProofOfRandomness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/retention", func(c *gin.Context) {
		artifactResponseError(c, ErrArtifactRetentionWeakKey)
	})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/retention", nil))
	body := response.Body.String()
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(body, "operator confirmation") ||
		!strings.Contains(body, "cryptographically secure random source") ||
		!strings.Contains(body, "minimum key-diversity checks") ||
		strings.Contains(body, "random-v1:") {
		t.Fatalf("retention key guidance was inaccurate: status=%d body=%s", response.Code, body)
	}
}

func TestUnreadableRetainedArtifactErrorIsSafeAndExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/retention", func(c *gin.Context) { artifactResponseError(c, ErrArtifactRetentionExistingContentUnreadable) })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/retention", nil))
	body := response.Body.String()
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(body, `"code":"retained_copy_unreadable"`) || !strings.Contains(body, "it was preserved") || strings.Contains(body, "ciphertext") {
		t.Fatalf("unreadable retained artifact error was not safely actionable: status=%d body=%s", response.Code, body)
	}
}

func TestArtifactForgetReturnsUnavailableWhenRetentionArchiveIsNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	handler := NewArtifactHandler(nil, nil)
	engine.DELETE("/events/:eventId/artifacts/:digest/retained", handler.Forget)
	digest := strings.Repeat("a", 64)
	request := httptest.NewRequest(http.MethodDelete, "/events/"+uuid.NewString()+"/artifacts/"+digest+"/retained", nil)
	request.Header.Set("If-Match", `"`+strings.Repeat("b", 64)+`"`)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "Encrypted artifact retention is not configured") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestArtifactDownloadHandlerOwnerAndAttachment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, owner, digest     string
		storageErr, downloadErr error
		status, calls           int
	}{
		{"ok", "alice", strings.Repeat("a", 64), nil, nil, 200, 1},
		{"no login", "", strings.Repeat("a", 64), nil, nil, 401, 0},
		{"invalid digest", "alice", "../private", nil, nil, 400, 0},
		{"not owner", "alice", strings.Repeat("a", 64), gorm.ErrRecordNotFound, nil, 404, 0},
		{"storage error", "alice", strings.Repeat("a", 64), errors.New("secret-token"), nil, 503, 0},
		{"remote link", "alice", strings.Repeat("a", 64), nil, agentruntime.ErrOpenClawArtifactURL, 422, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &artifactViewFake{err: tt.storageErr}
			downloader := &artifactDownloaderFake{err: tt.downloadErr}
			engine := gin.New()
			engine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, tt.owner) })
			engine.GET("/download", func(c *gin.Context) {
				c.Params = gin.Params{{Key: "eventId", Value: uuid.NewString()}, {Key: "digest", Value: tt.digest}}
				NewArtifactHandler(store, downloader).Download(c)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/download?owner=mallory", nil)
			req.Header.Set("X-Owner-Identity", "mallory")
			engine.ServeHTTP(w, req)
			if w.Code != tt.status || downloader.calls != tt.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, downloader.calls, w.Body.String())
			}
			if store.calls > 0 && store.owner != "alice" {
				t.Fatal("untrusted owner used")
			}
			if strings.Contains(w.Body.String(), "secret-token") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private data leaked or cached")
			}
			if tt.status == 200 && (w.Body.String() != "hi" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") != "application/octet-stream") {
				t.Fatal("unsafe content response")
			}
		})
	}
}
