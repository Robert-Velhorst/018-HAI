package haios

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type ownerMetricSnapshot struct {
	Automations            int64  `gorm:"column:automations"`
	UnhealthyAutomations   int64  `gorm:"column:unhealthy_automations"`
	OpenAutomationAlerts   int64  `gorm:"column:open_automation_alerts"`
	ConnectedSources       int64  `gorm:"column:connected_sources"`
	SourceExtractions      int64  `gorm:"column:source_extractions"`
	SourceReview           int64  `gorm:"column:source_review"`
	WorkflowItems          int64  `gorm:"column:workflow_items"`
	WorkflowApprovals      int64  `gorm:"column:workflow_approvals"`
	WorkflowReview         int64  `gorm:"column:workflow_review"`
	WorkflowProposalReview int64  `gorm:"column:workflow_proposal_review"`
	WorkflowQualityReview  int64  `gorm:"column:workflow_quality_review"`
	DueOpenLoops           int64  `gorm:"column:due_open_loops"`
	ContextMemories        int64  `gorm:"column:context_memories"`
	VerificationRuns       int64  `gorm:"column:verification_runs"`
	VerificationReview     int64  `gorm:"column:verification_review"`
	AmbientProposals       int64  `gorm:"column:ambient_proposals"`
	AmbientApprovalQueue   int64  `gorm:"column:ambient_approval_queue"`
	AmbientLastScan        string `gorm:"column:ambient_last_scan"`
}

func (snapshot ownerMetricSnapshot) ReviewTotal() int64 {
	return snapshot.VerificationReview +
		snapshot.SourceReview +
		snapshot.OpenAutomationAlerts +
		snapshot.WorkflowReview +
		snapshot.WorkflowProposalReview +
		snapshot.WorkflowQualityReview +
		snapshot.DueOpenLoops
}

func (h *Handler) loadMetricSnapshot(ctx context.Context, ownerIdentity string, now time.Time) (ownerMetricSnapshot, error) {
	var snapshot ownerMetricSnapshot
	if h == nil || h.db == nil {
		return snapshot, errors.New("HAI OS database is unavailable")
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return snapshot, errors.New("HAI OS owner identity is required")
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}

	const query = `
WITH
owner_sources AS MATERIALIZED (
	SELECT id, status
	FROM connected_sources
	WHERE owner_identity = @owner
),
owner_workflows AS MATERIALIZED (
	SELECT id, current_state, approval_status, archived
	FROM workflow_items
	WHERE owner_identity = @owner
),
active_owner_workflows AS MATERIALIZED (
	SELECT id, current_state, approval_status
	FROM owner_workflows
	WHERE archived = false
),
owner_verification_runs AS MATERIALIZED (
	SELECT id
	FROM verification_runs
	WHERE owner_identity = @owner
)
SELECT
	(SELECT COUNT(*) FROM automations) AS automations,
	(SELECT COUNT(*) FROM automations WHERE status IN ('warning', 'degraded', 'broken')) AS unhealthy_automations,
	(SELECT COUNT(*) FROM automation_alerts WHERE status = 'open') AS open_automation_alerts,
	(SELECT COUNT(*) FROM owner_sources WHERE status <> 'revoked') AS connected_sources,
	(SELECT COUNT(*)
	 FROM source_extractions extraction
	 JOIN owner_sources source ON source.id = extraction.source_id
	 WHERE source.status <> 'revoked'
	   AND extraction.archived = false) AS source_extractions,
	(SELECT COUNT(*)
	 FROM source_extractions extraction
	 JOIN owner_sources source ON source.id = extraction.source_id
	 WHERE source.status <> 'revoked'
	   AND extraction.archived = false
	   AND (extraction.uncertain = true OR extraction.sensitive = true)) AS source_review,
	(SELECT COUNT(*) FROM active_owner_workflows) AS workflow_items,
	(SELECT COUNT(*)
	 FROM active_owner_workflows
	 WHERE current_state = 'needs_approval') AS workflow_approvals,
	(SELECT COUNT(*)
	 FROM active_owner_workflows
	 WHERE current_state IN ('needs_approval', 'blocked') OR approval_status = 'pending') AS workflow_review,
	(SELECT COUNT(*)
	 FROM workflow_proposals proposal
	 JOIN active_owner_workflows workflow ON workflow.id = proposal.workflow_id
	 WHERE proposal.status = 'open') AS workflow_proposal_review,
	(SELECT COUNT(*)
	 FROM workflow_quality_gates quality_gate
	 JOIN active_owner_workflows workflow ON workflow.id = quality_gate.workflow_id
	 WHERE quality_gate.status IN ('needs_review', 'failed')) AS workflow_quality_review,
	(SELECT COUNT(*)
	 FROM workflow_open_loops open_loop
	 JOIN active_owner_workflows workflow ON workflow.id = open_loop.workflow_id
	 WHERE open_loop.status = 'open'
	   AND (open_loop.follow_up_at IS NULL OR open_loop.follow_up_at <= @now)) AS due_open_loops,
	(SELECT COUNT(*)
	 FROM context_memories
	 WHERE owner_identity = @owner AND archived = false) AS context_memories,
	(SELECT COUNT(*) FROM owner_verification_runs) AS verification_runs,
	(SELECT COUNT(*)
	 FROM verification_claims claim
	 JOIN owner_verification_runs run ON run.id = claim.run_id
	 WHERE claim.needs_review = true
	    OR claim.status IN ('unsupported', 'uncertain', 'conflicting', 'needs_review')) AS verification_review,
	(SELECT COUNT(*)
	 FROM ambient_opportunities
	 WHERE owner_identity = @owner
	   AND source_type LIKE 'pursuit\_%' ESCAPE '\'
	   AND status = 'proposed') AS ambient_proposals,
	(SELECT COUNT(*)
	 FROM ambient_opportunities
	 WHERE owner_identity = @owner
	   AND source_type LIKE 'pursuit\_%' ESCAPE '\'
	   AND status = 'proposed'
	   AND requires_approval = true) AS ambient_approval_queue,
	COALESCE((
		SELECT status
		FROM ambient_scans
		WHERE owner_identity = @owner
		ORDER BY started_at DESC, id DESC
		LIMIT 1
	), '') AS ambient_last_scan`

	err := h.db.WithContext(ctx).Raw(
		query,
		sql.Named("owner", ownerIdentity),
		sql.Named("now", now.UTC()),
	).Scan(&snapshot).Error
	return snapshot, err
}
