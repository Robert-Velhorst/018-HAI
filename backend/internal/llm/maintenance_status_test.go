package llm

import (
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

func TestMaintenanceReuseRequiresAnExplicitCurrentStatus(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		status     string
		blocks     bool
		age        time.Duration
		wantReuse  bool
		wantBlocks bool
	}{
		{status: "current", wantReuse: true},
		{status: "updated", wantReuse: true},
		{status: "installed", wantReuse: true},
		{status: "provider_managed", wantReuse: true},
		{status: "operator_managed", blocks: true, wantReuse: true, wantBlocks: true},
		{status: "approval_required", blocks: true, wantReuse: true, wantBlocks: true},
		{status: "failed", blocks: true, age: time.Minute, wantReuse: true, wantBlocks: true},
		{status: "not_enforced", wantReuse: false},
		{status: "", wantReuse: false, wantBlocks: true},
		{status: "future_status", wantReuse: false, wantBlocks: true},
	}

	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			age := test.age
			if age == 0 {
				age = time.Hour
			}
			record := models.LLMModelMaintenance{
				Status: test.status, BlocksExecution: test.blocks, CheckedAt: now.Add(-age),
			}
			if got := maintenanceRecordReusable(record, now, 24*time.Hour); got != test.wantReuse {
				t.Fatalf("reusable = %t; want %t", got, test.wantReuse)
			}
			if got := maintenanceStatusBlocksExecution(test.status); got != test.wantBlocks {
				t.Fatalf("blocks execution = %t; want %t", got, test.wantBlocks)
			}
		})
	}
}
