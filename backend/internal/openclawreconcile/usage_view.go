package openclawreconcile

import (
	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"net/http"
	"strings"
	"time"
)

type UsageView struct {
	State         string                                    `json:"state"`
	Attempts      int                                       `json:"attempts"`
	NextAttemptAt *time.Time                                `json:"nextAttemptAt,omitempty"`
	CapturedAt    *time.Time                                `json:"capturedAt,omitempty"`
	Snapshot      *agentruntime.GatewaySessionUsageSnapshot `json:"snapshot,omitempty"`
}

// Read only persisted usage for this owner's exact launch event. Shared automation
// configuration does not grant access to another operator's runtime receipt.
func (r *Repository) UsageForEvent(ctx context.Context, owner string, eventID uuid.UUID) (*UsageView, error) {
	owner = strings.TrimSpace(owner)
	if r == nil || r.db == nil || owner == "" || eventID == uuid.Nil {
		return nil, fmt.Errorf("usage read unavailable")
	}
	var row models.OpenClawGatewaySessionReceipt
	err := r.db.WithContext(ctx).Table("openclaw_gateway_session_receipts AS receipt").Select("receipt.*").
		Joins("JOIN automation_launch_events AS event ON event.execution_reference = receipt.execution_reference AND event.runtime_task_id = receipt.runtime_task_id AND event.owner_identity = receipt.owner_identity").
		Where("event.id = ? AND event.runtime_type = 'openclaw' AND event.owner_identity = ? AND receipt.owner_identity = ?", eventID, owner, owner).Take(&row).Error
	if err != nil {
		return nil, err
	}
	return usageViewFromReceipt(row, time.Now().UTC())
}

func usageViewFromReceipt(row models.OpenClawGatewaySessionReceipt, now time.Time) (*UsageView, error) {
	v := &UsageView{State: "pending", Attempts: row.UsageAttempts, NextAttemptAt: row.UsageNextAttemptAt}
	if row.SessionID == "" || (row.Status == "terminal" && row.TerminalStatus != "completed" && row.TerminalStatus != "failed") {
		v.State = "unavailable"
	}
	if row.UsageSnapshotJSON == nil {
		if row.UsageAttempts >= 8 {
			v.State = "retry_exhausted"
		}
		return v, nil
	}
	if len(*row.UsageSnapshotJSON) > 65536 || row.TerminalAt == nil || row.Status != "terminal" || (row.TerminalStatus != "completed" && row.TerminalStatus != "failed") {
		return nil, fmt.Errorf("invalid stored usage")
	}
	var snapshot agentruntime.GatewaySessionUsageSnapshot
	if err := json.Unmarshal([]byte(*row.UsageSnapshotJSON), &snapshot); err != nil {
		return nil, fmt.Errorf("invalid stored usage")
	}
	receipt := receiptFromModel(row)
	snapshot.ExecutionReference, snapshot.SessionKey, snapshot.SessionID = receipt.ExecutionReference, receipt.SessionKey, receipt.SessionID
	if err := snapshot.ValidateFor(receipt, *row.TerminalAt, now); err != nil {
		return nil, fmt.Errorf("invalid stored usage")
	}
	v.State, v.Snapshot, v.CapturedAt, v.NextAttemptAt = "captured", &snapshot, row.UsageCapturedAt, nil
	return v, nil
}

type usageViewReader interface {
	UsageForEvent(context.Context, string, uuid.UUID) (*UsageView, error)
}
type UsageHandler struct{ reader usageViewReader }

func NewUsageHandler(reader usageViewReader) *UsageHandler { return &UsageHandler{reader: reader} }
func (h *UsageHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, _ := c.Get(identity.ContextSubjectKey)
	owner, _ := subject.(string)
	if strings.TrimSpace(owner) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}
	event, err := uuid.Parse(c.Param("eventId"))
	if err != nil || event == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid execution ID"})
		return
	}
	if h.reader == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage is temporarily unavailable"})
		return
	}
	result, err := h.reader.UsageForEvent(c.Request.Context(), owner, event)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "No usage record available for this execution"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage is temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}
