package openclawreconcile

import (
	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUsageViewPreservesPrivacyAndUnknowns(t *testing.T) {
	now := time.Now().UTC()
	row := models.OpenClawGatewaySessionReceipt{ExecutionReference: "private-reference", SessionID: "private-instance", SessionKey: "private-key", CreatedAt: now.Add(-time.Minute), TerminalAt: &now, Status: "terminal", TerminalStatus: "completed"}
	snapshot := agentruntime.GatewaySessionUsageSnapshot{Source: "openclaw.sessions.usage", Scope: "session-instance", StartDate: row.CreatedAt.Format("2006-01-02"), EndDate: now.Format("2006-01-02"), ObservedAt: now, Totals: agentruntime.GatewayUsageTotals{Input: 12, Output: 4, TotalTokens: 16}}
	data, _ := json.Marshal(snapshot)
	raw := string(data)
	row.UsageSnapshotJSON = &raw
	view, err := usageViewFromReceipt(row, now)
	if err != nil || view.State != "captured" || view.Snapshot.Totals.Input != 12 {
		t.Fatalf("view: %#v %v", view, err)
	}
	output, _ := json.Marshal(view)
	if strings.Contains(string(output), "private-") || view.Snapshot.Totals.EstimatedCostUSD != nil {
		t.Fatalf("private or inferred usage: %s", output)
	}
	row.UsageSnapshotJSON = nil
	row.UsageSummary = "legacy text says USD 99"
	view, err = usageViewFromReceipt(row, now)
	if err != nil || view.Snapshot != nil || view.State != "pending" {
		t.Fatal("legacy text was promoted into typed evidence")
	}
	row.UsageAttempts = 8
	view, err = usageViewFromReceipt(row, now)
	if err != nil || view.State != "retry_exhausted" {
		t.Fatal("retry exhaustion hidden")
	}
	raw = `{"source":"invented"}`
	row.UsageSnapshotJSON = &raw
	if _, err = usageViewFromReceipt(row, now); err == nil {
		t.Fatal("corrupt stored snapshot accepted")
	}
}

type usageViewReaderFake struct {
	owner string
	event uuid.UUID
	calls int
	err   error
}

func (r *usageViewReaderFake) UsageForEvent(_ context.Context, owner string, event uuid.UUID) (*UsageView, error) {
	r.owner = owner
	r.event = event
	r.calls++
	return &UsageView{State: "pending"}, r.err
}

func TestUsageHandlerUsesVerifiedIdentityAndHidesStorageErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	for _, test := range []struct {
		subject string
		err     error
		status  int
	}{{"", nil, 401}, {"alice", nil, 200}, {"alice", gorm.ErrRecordNotFound, 404}, {"alice", context.DeadlineExceeded, 503}} {
		reader := &usageViewReaderFake{err: test.err}
		engine := gin.New()
		engine.Use(func(c *gin.Context) {
			if test.subject != "" {
				c.Set(identity.ContextSubjectKey, test.subject)
			}
		})
		engine.GET("/usage/:eventId", NewUsageHandler(reader).Get)
		response := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/usage/"+id.String()+"?owner=mallory", nil)
		req.Header.Set("X-Owner-Identity", "mallory")
		engine.ServeHTTP(response, req)
		if response.Code != test.status {
			t.Fatalf("status %d: %s", response.Code, response.Body.String())
		}
		if test.subject == "" && reader.calls != 0 {
			t.Fatal("unauthenticated storage read")
		}
		if test.subject != "" && (reader.owner != "alice" || reader.event != id) {
			t.Fatal("untrusted owner or event used")
		}
		if strings.Contains(response.Body.String(), "deadline") {
			t.Fatal("internal storage error disclosed")
		}
	}
}
