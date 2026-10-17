//go:build integration

package accountfeed

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test creates synthetic accounts/listings. The caller must start and own
// a disposable Autoposter instance; never point it at a personal installation.
func TestAutoposterRealProducerToPostgres(t *testing.T) {
	base := os.Getenv("AUTOPOSTER_TEST_URL")
	if os.Getenv("AUTOPOSTER_TEST_DISPOSABLE") != "1" || base == "" || os.Getenv("HAI_TEST_DATABASE_DSN") == "" {
		t.Skip("disposable Autoposter and PostgreSQL not enabled")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() {
		t.Fatal("integration test requires literal loopback HTTP")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("HAI_TEST_DATABASE_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("could not open disposable PostgreSQL")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	call := func(method, path, token string, payload any, expected int) map[string]any {
		body, _ := json.Marshal(payload)
		req, err := http.NewRequest(method, base+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("local Autoposter request failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != expected {
			t.Fatalf("%s %s returned %d, expected %d", method, path, resp.StatusCode, expected)
		}
		var result map[string]any
		if expected != 204 {
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatal("invalid JSON response")
			}
		}
		return result
	}
	suffix := uuid.NewString()
	registration := call("POST", "/api/auth/register", "", map[string]any{"email": "hai-test-" + suffix + "@example.com", "name": "Synthetic HAI test", "password": "synthetic-only-" + suffix}, 200)
	ownerToken := registration["token"].(string)
	me := call("GET", "/api/auth/me", ownerToken, nil, 200)
	credential := call("POST", "/api/hai/tokens", ownerToken, map[string]any{"name": "Disposable HAI test", "expires_days": 1}, 200)
	var listingID float64
	for i := 0; i < 101; i++ {
		listing := call("POST", "/api/listings", ownerToken, map[string]any{"title": fmt.Sprintf("Synthetic chair %d", i), "internal_notes": "PRIVATE_TEST_NOTE_NOT_FOR_HAI"}, 200)
		if i == 0 {
			listingID = listing["id"].(float64)
		}
	}
	ops := operations.NewService(operations.NewGormRepository(db))
	opts := FetchOptions{Autoposter: &AutoposterConnection{BaseURL: base, Token: credential["token"].(string), RemoteOwner: fmt.Sprint(me["id"]), OwnerUserID: "hai-test-" + suffix, WorkspaceID: "local"}}
	scope := FeedScope{OwnerUserID: opts.Autoposter.OwnerUserID, WorkspaceID: opts.Autoposter.WorkspaceID}
	reg, err := NewPostgresRegistry(context.Background(), ops, nil, opts)
	if err != nil {
		t.Fatal("could not initialize durable account-feed registry")
	}
	feed, err := reg.ConfigureAutoposter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	if _, err := reg.PatchContext(context.Background(), scope, feed.ID, FeedPatch{Enabled: &enabled}); err != nil {
		t.Fatal("could not explicitly enable disposable source")
	}
	syncFeed := func(r *Registry, count int) SyncReport {
		rep, err := r.SyncContext(context.Background(), scope, feed.ID)
		if err != nil {
			t.Fatalf("durable feed sync failed: %v", err)
		}
		if len(rep.Errors) != 0 || rep.ItemsRead != count {
			t.Fatalf("real producer sync: %+v", rep)
		}
		return rep
	}
	first := syncFeed(reg, 101)
	if first.OperationsCreated != 101 || first.HasMore {
		t.Fatalf("multi-page ingestion: %+v", first)
	}
	syncFeed(reg, 0)
	path := fmt.Sprintf("/api/listings/%.0f", listingID)
	call("PATCH", path, ownerToken, map[string]any{"title": "Updated synthetic chair"}, 200)
	syncFeed(reg, 1)
	rows, err := ops.List(operations.Filter{OwnerUserID: opts.Autoposter.OwnerUserID, WorkspaceID: "local", Limit: 200})
	if err != nil || len(rows) != 101 {
		t.Fatal("unexpected persisted reference count")
	}
	found := false
	for _, row := range rows {
		if row.Title == "Updated synthetic chair" {
			found = true
		}
		if strings.Contains(row.EvidenceJSON, "PRIVATE_TEST_NOTE") {
			t.Fatal("private source notes leaked")
		}
	}
	if !found {
		t.Fatal("source update did not reach PostgreSQL")
	}
	call("DELETE", path, ownerToken, nil, 204)
	if syncFeed(reg, 1).TombstonesApplied != 1 {
		t.Fatal("delete did not propagate")
	}
	t.Run("blocked_control_times_out_without_acknowledgment", func(t *testing.T) {
		key := referenceKey(feed)
		encoded, _ := json.Marshal(key)
		lockID := uuid.NewSHA1(uuid.NameSpaceURL, append([]byte("reference-checkpoint:"), encoded...))
		lock := db.Begin()
		if lock.Error != nil {
			t.Fatal("could not start synthetic blocker")
		}
		defer lock.Rollback()
		if err := lock.Exec("SELECT pg_advisory_xact_lock(?, ?)", int32(binary.BigEndian.Uint32(lockID[:4])), int32(binary.BigEndian.Uint32(lockID[4:8]))).Error; err != nil {
			t.Fatal("could not hold source-control lock")
		}
		// A missing application deadline must fail this test, not hang its runner.
		release := time.AfterFunc(7*time.Second, func() { lock.Rollback() })
		defer release.Stop()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("subject", opts.Autoposter.OwnerUserID)
		c.Params = gin.Params{{Key: "id", Value: feed.ID.String()}}
		c.Request = httptest.NewRequest("PATCH", "/api/v1/account-feeds/"+feed.ID.String(), strings.NewReader(`{"enabled":false}`))
		started := time.Now()
		NewHandler(reg, opts.Autoposter.OwnerUserID, "local").Patch(c)
		if elapsed := time.Since(started); w.Code != http.StatusServiceUnavailable || elapsed > 6500*time.Millisecond {
			t.Fatalf("blocked control was not bounded/rejected: status=%d elapsed=%s", w.Code, elapsed)
		}
		release.Stop()
		lock.Rollback()
		stored, err := ops.GetReferenceSourceControl(context.Background(), key)
		health, healthErr := reg.ListContext(context.Background(), scope)
		if err != nil || !stored.Enabled || stored.Revision != 1 || healthErr != nil || len(health) != 1 || !health[0].Feed.Enabled {
			t.Fatal("timed-out control changed durable state")
		}
		t.Log("held PostgreSQL source lock: HTTP 503 within deadline; no control mutation")
	})
	t.Log("phase: tombstone applied; disabling source")
	control := func(r *Registry, enabled bool) {
		// Exercise the actual owner-scoped HTTP handler against the real DB.
		// Authenticated identity is supplied by this fixture, not a live login.
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("subject", opts.Autoposter.OwnerUserID)
		c.Params = gin.Params{{Key: "id", Value: feed.ID.String()}}
		c.Request = httptest.NewRequest("PATCH", "/api/v1/account-feeds/"+feed.ID.String(), strings.NewReader(fmt.Sprintf(`{"enabled":%t}`, enabled)))
		NewHandler(r, opts.Autoposter.OwnerUserID, "local").Patch(c)
		var saved Feed
		if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &saved) != nil || saved.Enabled != enabled {
			t.Fatalf("source control not acknowledged: status=%d", recorder.Code)
		}
	}
	control(reg, false)
	t.Log("phase: disable acknowledged; closing original database pool")
	// Close the original connection pool. A fresh DB connection, service, and
	// registry must resume PostgreSQL progress without any in-process cache.
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	t.Log("phase: database pool closed; reopening source")
	db, err = gorm.Open(postgres.Open(os.Getenv("HAI_TEST_DATABASE_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("could not reopen disposable PostgreSQL")
	}
	reopened, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	ops = operations.NewService(operations.NewGormRepository(db))
	restarted, err := NewPostgresRegistry(context.Background(), ops, nil, opts)
	if err != nil {
		t.Fatal("could not initialize restarted feed registry")
	}
	if restored, err := restarted.ConfigureAutoposter(context.Background()); err != nil || restored.Enabled {
		t.Fatalf("restart lost disabled source state: %v", err)
	}
	if stopped, _ := restarted.SyncContext(context.Background(), scope, feed.ID); len(stopped.Errors) != 1 || !strings.Contains(stopped.Errors[0], "disabled") || stopped.ItemsRead != 0 {
		t.Fatal("disabled source synced after DB/service restart")
	}
	control(restarted, true)
	syncFeed(restarted, 0)
	rows, err = ops.List(operations.Filter{OwnerUserID: opts.Autoposter.OwnerUserID, WorkspaceID: "local", Limit: 200})
	if err != nil || len(rows) != 101 {
		t.Fatal("restart duplicated references")
	}
	tombstones := 0
	for _, row := range rows {
		if row.Status != "archived" || row.OperationType != "read_only_reference" {
			t.Fatal("import enabled executable work")
		}
		var evidence autoposterRecord
		if err := json.Unmarshal([]byte(row.EvidenceJSON), &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence.Deleted {
			tombstones++
			if row.Description != "" || row.SourceURI != "" || strings.Contains(row.EvidenceJSON, "synthetic chair") {
				t.Fatal("tombstone retained former content")
			}
		}
	}
	if tombstones != 1 {
		t.Fatal("restart lost deletion state")
	}
	call("DELETE", fmt.Sprintf("/api/hai/tokens/%.0f", credential["id"].(float64)), ownerToken, nil, 204)
	failed, _ := restarted.SyncContext(context.Background(), scope, feed.ID)
	if len(failed.Errors) != 1 || !strings.Contains(failed.Errors[0], "401") {
		t.Fatal("revoked token was not rejected")
	}
	t.Log("verified: 101 records across pages -> PostgreSQL; update; tombstone; HTTP-handler disable retained across fresh DB connection/service/registry; explicit enable resumes with zero replayed records; revoked token; no private notes or executable work")
}
