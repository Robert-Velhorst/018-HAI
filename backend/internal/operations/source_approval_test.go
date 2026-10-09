package operations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func sourceApprovalFixture(t *testing.T) (*Service, *MemoryRepository, models.Operation) {
	t.Helper()
	repo := NewMemoryRepository()
	service := NewService(repo)
	sourceID := uuid.New()
	created, err := service.Ingest(NewOperationInput{
		OwnerUserID: "owner-1", WorkspaceID: "local", Title: "Review imported request",
		Description: "Prepare a local review note", OperationType: "review",
		SourceType: "email", SourceID: &sourceID, SourceRevisionHash: strings.Repeat("a", 64),
		DedupeKey: "approval-fixture-" + sourceID.String(), EvidenceJSON: `{"messageId":"m-1"}`,
	})
	if err != nil {
		t.Fatalf("ingest source operation: %v", err)
	}
	op := created.Operation
	op.CurrentDecision = string(DecisionAskRobert)
	op.RequiresApproval = true
	op.RiskLevel = string(RiskMedium)
	op.AutonomyLevel = string(AutonomyApproval)
	classified, err := service.Transition(op, StatusClassified, string(OwnerHAI), "", "source classified for owner review")
	if err != nil {
		t.Fatalf("classify operation: %v", err)
	}
	awaiting, err := service.Transition(*classified, StatusAwaitingApproval, string(OwnerHAI), "", "awaiting owner approval")
	if err != nil {
		t.Fatalf("request approval: %v", err)
	}
	return service, repo, *awaiting
}

func sourceApprovalObservedFixture(t *testing.T, managed bool) (*Service, *MemoryRepository, models.Operation, SourceObservationStart) {
	t.Helper()
	repo := NewMemoryRepository()
	service := sourceObservationTestService(repo)
	start := sourceObservationTestStart()
	if managed {
		start.RegistryManaged = true
		start.ConfigVersion = 1
		if err := service.WithRegistrySourceConfiguration(context.Background(), start, true, func() error { return nil }); err != nil {
			t.Fatalf("seed managed source origin: %v", err)
		}
	}
	op, _ := sourceObservationTestIngest(t, service, start, sourceHeadTestInput("A"))
	op.CurrentDecision = string(DecisionAskRobert)
	op.RequiresApproval = true
	op.RiskLevel = string(RiskMedium)
	op.AutonomyLevel = string(AutonomyApproval)
	classified, err := service.Transition(op, StatusClassified, string(OwnerHAI), "", "source classified for owner review")
	if err != nil {
		t.Fatalf("classify observed operation: %v", err)
	}
	awaiting, err := service.Transition(*classified, StatusAwaitingApproval, string(OwnerHAI), "", "awaiting owner approval")
	if err != nil {
		t.Fatalf("request approval for observed operation: %v", err)
	}
	return service, repo, *awaiting, start
}

func sourceApprovalCandidate(t *testing.T, op models.Operation, preview SourceApprovalPreview) (models.Operation, models.OperationEvent) {
	t.Helper()
	now := sourceLedgerTime(time.Now())
	receipt := SourceApprovalReceipt{Schema: sourceApprovalReceiptSchema, ID: uuid.New(), OperationID: op.ID,
		OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID, ApprovedBy: op.OwnerUserID,
		ApprovedAt: now, ReviewedUpdatedAt: op.UpdatedAt.UTC(), Version: op.Version, RevisionDigest: preview.RevisionDigest}
	updated, event, err := ApplyTransition(op, StatusApproved, string(OwnerRobert), op.OwnerUserID,
		"owner approved exact source-derived operation revision", now)
	if err != nil {
		t.Fatalf("build approval transition: %v", err)
	}
	updated.OwnerType = string(OwnerRobert)
	payload, err := json.Marshal(sourceApprovalEventPayload{SourceApproval: &receipt})
	if err != nil {
		t.Fatalf("encode approval receipt: %v", err)
	}
	event.PayloadJSON = string(payload)
	return updated, event
}

func TestSourceApprovalReceiptBindsExactRevisionAndIsAuditable(t *testing.T) {
	service, _, op := sourceApprovalFixture(t)
	preview, err := service.PreviewSourceApproval(op)
	if err != nil {
		t.Fatal(err)
	}
	approved, receipt, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest)
	if err != nil {
		t.Fatalf("approve exact revision: %v", err)
	}
	if approved.Status != string(StatusApproved) || approved.Version != op.Version+1 {
		t.Fatalf("approved state = status %q version %d", approved.Status, approved.Version)
	}
	if receipt.OperationID != op.ID || receipt.Version != op.Version || receipt.ApprovedBy != op.OwnerUserID || receipt.RevisionDigest != preview.RevisionDigest {
		t.Fatalf("receipt does not bind reviewed revision: %#v preview=%#v", receipt, preview)
	}
	if _, err := service.SourceApprovalForExecution(*approved); err != nil {
		t.Fatalf("approved exact revision rejected: %v", err)
	}
	wrongStatus := *approved
	wrongStatus.Status = string(StatusReady)
	if _, err := service.SourceApprovalForExecution(wrongStatus); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("approval receipt accepted wrong lifecycle status: %v", err)
	}
	events, err := service.listSourceApprovalEvents(op.ID)
	if err != nil || len(events) < 4 {
		t.Fatalf("events=%d err=%v; expected durable creation/classification/review/approval trail", len(events), err)
	}
	var approvalEvent *models.OperationEvent
	for i := range events {
		var audited sourceApprovalEventPayload
		if json.Unmarshal([]byte(events[i].PayloadJSON), &audited) == nil && audited.SourceApproval != nil && audited.SourceApproval.ID == receipt.ID {
			approvalEvent = &events[i]
			break
		}
	}
	if approvalEvent == nil || approvalEvent.ActorID != op.OwnerUserID || approvalEvent.ActorType != string(OwnerRobert) || approvalEvent.BeforeStatus != string(StatusAwaitingApproval) || approvalEvent.AfterStatus != string(StatusApproved) {
		t.Fatalf("approval receipt missing owner audit provenance: event=%#v", approvalEvent)
	}
}

func TestSourceApprovalRevisionDigestBindsEveryActionPolicyAndExecutionField(t *testing.T) {
	_, _, op := sourceApprovalFixture(t)
	base, err := sourceOperationRevisionDigest(op)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*models.Operation)
	}{
		{"id", func(v *models.Operation) { v.ID = uuid.New() }},
		{"owner", func(v *models.Operation) { v.OwnerUserID += "-changed" }},
		{"workspace", func(v *models.Operation) { v.WorkspaceID += "-changed" }},
		{"title", func(v *models.Operation) { v.Title += " changed" }},
		{"description", func(v *models.Operation) { v.Description += " changed" }},
		{"sourceType", func(v *models.Operation) { v.SourceType += "-changed" }},
		{"sourceId", func(v *models.Operation) { id := uuid.New(); v.SourceID = &id }},
		{"sourceUri", func(v *models.Operation) { v.SourceURI += " changed" }},
		{"sourceReceivedAt", func(v *models.Operation) { now := time.Now().UTC(); v.SourceReceivedAt = &now }},
		{"sourceRevisionHash", func(v *models.Operation) { v.SourceRevisionHash = strings.Repeat("b", 64) }},
		{"sourceProvider", func(v *models.Operation) { v.SourceProvider += "-changed" }},
		{"sourceAccount", func(v *models.Operation) { v.SourceAccount += "-changed" }},
		{"sourceExternalId", func(v *models.Operation) { v.SourceExternalID += "-changed" }},
		{"sourceIdentityHash", func(v *models.Operation) { v.SourceIdentityHash = strings.Repeat("b", 64) }},
		{"sourceObservationId", func(v *models.Operation) { id := uuid.New(); v.SourceObservationID = &id }},
		{"sourceObservationGeneration", func(v *models.Operation) { v.SourceObservationGeneration++ }},
		{"projectKey", func(v *models.Operation) { v.ProjectKey += "-changed" }},
		{"pursuitId", func(v *models.Operation) { id := uuid.New(); v.PursuitID = &id }},
		{"workflowId", func(v *models.Operation) { id := uuid.New(); v.WorkflowID = &id }},
		{"accountFeedId", func(v *models.Operation) { id := uuid.New(); v.AccountFeedID = &id }},
		{"operationType", func(v *models.Operation) { v.OperationType += "-changed" }},
		{"riskLevel", func(v *models.Operation) { v.RiskLevel = string(RiskHigh) }},
		{"autonomyLevel", func(v *models.Operation) { v.AutonomyLevel = "different-autonomy" }},
		{"ownerType", func(v *models.Operation) { v.OwnerType = string(OwnerRobert) }},
		{"currentDecision", func(v *models.Operation) { v.CurrentDecision = string(DecisionBlock) }},
		{"requiresApproval", func(v *models.Operation) { v.RequiresApproval = !v.RequiresApproval }},
		{"approvalId", func(v *models.Operation) { id := uuid.New(); v.ApprovalID = &id }},
		{"recommendedAction", func(v *models.Operation) { v.RecommendedAction += " changed" }},
		{"evidence", func(v *models.Operation) { v.EvidenceJSON = `{"changed":true}` }},
		{"worldModelState", func(v *models.Operation) { v.WorldModelStateJSON = `{"changed":true}` }},
		{"runtimeId", func(v *models.Operation) { v.RuntimeID = "different-runtime" }},
		{"modelProviderId", func(v *models.Operation) { v.ModelProviderID = "different-provider" }},
		{"modelId", func(v *models.Operation) { v.ModelID = "different-model" }},
		{"verificationStatus", func(v *models.Operation) { v.VerificationStatus = string(VerificationPassed) }},
		{"resultSummary", func(v *models.Operation) { v.ResultSummary = "different result" }},
		{"lastError", func(v *models.Operation) { v.LastError = "different error" }},
		{"dedupeKey", func(v *models.Operation) { v.DedupeKey += "-changed" }},
		{"nextReviewAt", func(v *models.Operation) { now := time.Now().UTC(); v.NextReviewAt = &now }},
		{"createdAt", func(v *models.Operation) { v.CreatedAt = v.CreatedAt.Add(time.Second) }},
		{"updatedAt", func(v *models.Operation) { v.UpdatedAt = v.UpdatedAt.Add(time.Second) }},
		{"completedAt", func(v *models.Operation) { now := time.Now().UTC(); v.CompletedAt = &now }},
		{"version", func(v *models.Operation) { v.Version++ }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			changed := op
			tc.edit(&changed)
			digest, err := sourceOperationRevisionDigest(changed)
			if err != nil {
				t.Fatal(err)
			}
			if digest == base {
				t.Fatalf("mutation of %s did not change revision digest", tc.name)
			}
		})
	}
	// Status is validated separately against the receipt's audited transition.
	lifecycle := op
	lifecycle.Status = string(StatusApproved)
	digest, err := sourceOperationRevisionDigest(lifecycle)
	if err != nil || digest != base {
		t.Fatalf("status lifecycle change altered revision digest: %s err=%v", digest, err)
	}
}

func TestSourceApprovalRejectsUnapprovedAndNonOwner(t *testing.T) {
	service, _, op := sourceApprovalFixture(t)
	if _, err := service.SourceApprovalForExecution(op); !errors.Is(err, ErrSourceApprovalRequired) {
		t.Fatalf("unapproved source operation error = %v", err)
	}
	preview, err := service.PreviewSourceApproval(op)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ApproveSourceDerived(op, "another-user", preview.Version, preview.RevisionDigest); !errors.Is(err, ErrSourceApprovalRequired) {
		t.Fatalf("non-owner approval error = %v", err)
	}
	if current, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID); err != nil || current.Status != string(StatusAwaitingApproval) {
		t.Fatalf("refused approval mutated state: current=%#v err=%v", current, err)
	}
}

func TestSourceApprovalRejectsStaleVersionOrMutatedRevision(t *testing.T) {
	service, _, op := sourceApprovalFixture(t)
	preview, err := service.PreviewSourceApproval(op)
	if err != nil {
		t.Fatal(err)
	}
	changed := op
	changed.Title = "Changed after owner review"
	updated, err := service.Save(changed, "operation_updated", string(OwnerHAI), "reviewed text changed")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ApproveSourceDerived(*updated, op.OwnerUserID, preview.Version, preview.RevisionDigest); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("stale preview error = %v", err)
	}
	if _, err := service.SourceApprovalForExecution(*updated); !errors.Is(err, ErrSourceApprovalRequired) {
		t.Fatalf("unapproved changed operation error = %v", err)
	}
}

func TestSourceApprovalRejectsSupersededAcceptedRevisionBeforeReceipt(t *testing.T) {
	service, repo, op, start := sourceApprovalObservedFixture(t, false)
	preview, err := service.PreviewSourceApproval(op)
	if err != nil {
		t.Fatalf("preview accepted source head: %v", err)
	}
	_, _ = sourceObservationTestIngest(t, service, start, sourceHeadTestInput("B"))
	if _, err := service.PreviewSourceApproval(op); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("preview accepted superseded source revision: %v", err)
	}
	eventsBefore := len(repo.events)
	updated, event := sourceApprovalCandidate(t, op, preview)
	if _, err := repo.ApproveSourceDerived(context.Background(), op, updated, event, preview.RevisionDigest); !errors.Is(err, ErrSourceHeadSuperseded) {
		t.Fatalf("memory approval transaction accepted superseded source revision: %v", err)
	}
	if _, _, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("approval accepted superseded source revision: %v", err)
	}
	if len(repo.events) != eventsBefore {
		t.Fatalf("stale approval wrote an event: before=%d after=%d", eventsBefore, len(repo.events))
	}
	current, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
	if err != nil || current.Status != string(StatusAwaitingApproval) || current.Version != op.Version {
		t.Fatalf("stale approval mutated operation: current=%+v err=%v", current, err)
	}
}

func TestSourceApprovalRejectsHeadObservationMissingFromLedger(t *testing.T) {
	service, repo, op, _ := sourceApprovalObservedFixture(t, false)
	preview, err := service.PreviewSourceApproval(op)
	if err != nil {
		t.Fatalf("preview accepted source head: %v", err)
	}

	repo.mu.Lock()
	key := sourceHeadKey{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash}
	head := repo.sourceHeads[key]
	head.ObservationID = uuid.New()
	head.ObservationGeneration++
	repo.sourceHeads[key] = head
	repo.mu.Unlock()

	if _, err := service.PreviewSourceApproval(op); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("preview accepted a head referencing a missing observation: %v", err)
	}
	eventsBefore := len(repo.events)
	if _, _, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("approval accepted a head referencing a missing observation: %v", err)
	}
	if len(repo.events) != eventsBefore {
		t.Fatalf("invalid-observation approval wrote an event: before=%d after=%d", eventsBefore, len(repo.events))
	}
	current, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
	if err != nil || current.Status != string(StatusAwaitingApproval) || current.Version != op.Version {
		t.Fatalf("invalid-observation approval mutated operation: current=%+v err=%v", current, err)
	}
}

func TestSourceApprovalRejectsDisabledOrReconfiguredOrigin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		digest  string
		enabled bool
	}{
		{name: "disabled", digest: strings.Repeat("a", 64), enabled: false},
		{name: "reconfigured", digest: strings.Repeat("b", 64), enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, op, start := sourceApprovalObservedFixture(t, true)
			preview, err := service.PreviewSourceApproval(op)
			if err != nil {
				t.Fatalf("preview accepted source head: %v", err)
			}
			start.ConfigVersion++
			start.ConfigDigest = tc.digest
			if err := service.WithRegistrySourceConfiguration(context.Background(), start, tc.enabled, func() error { return nil }); err != nil {
				t.Fatalf("change source origin configuration: %v", err)
			}
			if _, err := service.PreviewSourceApproval(op); !errors.Is(err, ErrSourceApprovalStale) {
				t.Fatalf("preview accepted %s origin: %v", tc.name, err)
			}
			eventsBefore := len(repo.events)
			if _, _, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest); !errors.Is(err, ErrSourceApprovalStale) {
				t.Fatalf("approval accepted %s origin: %v", tc.name, err)
			}
			if len(repo.events) != eventsBefore {
				t.Fatalf("stale approval wrote an event: before=%d after=%d", eventsBefore, len(repo.events))
			}
		})
	}
}

type sourceApprovalSQLState struct {
	op                 models.Operation
	head               SourceHead
	origin             SourceOrigin
	phase              int
	active             bool
	commits, rollbacks int
	writes             int
	supersedeOnCommit  bool
}

type sourceApprovalSQLConnector struct{ state *sourceApprovalSQLState }
type sourceApprovalSQLDriver struct{}
type sourceApprovalSQLConnection struct{ state *sourceApprovalSQLState }
type sourceApprovalSQLTransaction struct{ state *sourceApprovalSQLState }
type sourceApprovalSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func sourceApprovalSQLRow(op models.Operation) ([]string, []driver.Value) {
	idValue := func(value *uuid.UUID) driver.Value {
		if value == nil {
			return nil
		}
		return value.String()
	}
	timeValue := func(value *time.Time) driver.Value {
		if value == nil {
			return nil
		}
		return *value
	}
	columns := []string{
		"id", "owner_user_id", "workspace_id", "title", "description", "source_type", "source_id", "source_uri",
		"source_received_at", "source_revision_hash", "source_provider", "source_account", "source_external_id",
		"source_identity_hash", "source_observation_id", "source_observation_generation", "project_key", "pursuit_id",
		"workflow_id", "account_feed_id", "operation_type", "status", "risk_level", "autonomy_level", "owner_type",
		"current_decision", "requires_approval", "approval_id", "recommended_action", "evidence_json", "world_model_state_json",
		"runtime_id", "model_provider_id", "model_id", "verification_status", "result_summary", "last_error", "dedupe_key",
		"next_review_at", "created_at", "updated_at", "completed_at", "version",
	}
	values := []driver.Value{
		op.ID.String(), op.OwnerUserID, op.WorkspaceID, op.Title, op.Description, op.SourceType, idValue(op.SourceID), op.SourceURI,
		timeValue(op.SourceReceivedAt), op.SourceRevisionHash, op.SourceProvider, op.SourceAccount, op.SourceExternalID,
		op.SourceIdentityHash, idValue(op.SourceObservationID), op.SourceObservationGeneration, op.ProjectKey, idValue(op.PursuitID),
		idValue(op.WorkflowID), idValue(op.AccountFeedID), op.OperationType, op.Status, op.RiskLevel, op.AutonomyLevel, op.OwnerType,
		op.CurrentDecision, op.RequiresApproval, idValue(op.ApprovalID), op.RecommendedAction, op.EvidenceJSON, op.WorldModelStateJSON,
		op.RuntimeID, op.ModelProviderID, op.ModelID, op.VerificationStatus, op.ResultSummary, op.LastError, op.DedupeKey,
		timeValue(op.NextReviewAt), op.CreatedAt, op.UpdatedAt, timeValue(op.CompletedAt), op.Version,
	}
	return columns, values
}

func (c sourceApprovalSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &sourceApprovalSQLConnection{state: c.state}, nil
}

func (sourceApprovalSQLConnector) Driver() driver.Driver { return sourceApprovalSQLDriver{} }
func (sourceApprovalSQLDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use sourceApprovalSQLConnector")
}
func (*sourceApprovalSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (*sourceApprovalSQLConnection) Close() error { return nil }
func (c *sourceApprovalSQLConnection) Begin() (driver.Tx, error) {
	if c.state.active {
		return nil, errors.New("nested source approval transaction")
	}
	c.state.active = true
	c.state.phase = 0
	return &sourceApprovalSQLTransaction{state: c.state}, nil
}
func (c *sourceApprovalSQLConnection) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Begin()
}
func (c *sourceApprovalSQLConnection) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !c.state.active || query != "SET LOCAL lock_timeout = '5s'" || c.state.phase != 0 {
		c.state.writes++
		return nil, fmt.Errorf("unexpected SQL write at source approval fence: %s", query)
	}
	return driver.RowsAffected(0), nil
}
func (c *sourceApprovalSQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !c.state.active {
		return nil, errors.New("source approval SQL outside transaction")
	}
	switch {
	case strings.HasPrefix(query, `SELECT * FROM "operation_source_origins"`) && strings.Contains(query, "FOR UPDATE"):
		if c.state.phase != 0 || len(args) < 3 || args[0].Value != c.state.op.OwnerUserID || args[1].Value != c.state.op.WorkspaceID || fmt.Sprint(args[2].Value) != c.state.origin.OriginID.String() {
			return nil, errors.New("source origin was not locked first in the operation tenant")
		}
		c.state.phase = 1
		o := c.state.origin
		return &sourceApprovalSQLRows{columns: []string{"owner_user_id", "workspace_id", "origin_id", "config_digest", "config_epoch", "registry_managed", "enabled", "registry_config_version"}, values: [][]driver.Value{{o.OwnerUserID, o.WorkspaceID, o.OriginID.String(), o.ConfigDigest, o.ConfigEpoch, o.RegistryManaged, o.Enabled, o.ConfigVersion}}}, nil
	case strings.HasPrefix(query, `SELECT * FROM "operation_source_heads"`) && strings.Contains(query, "FOR UPDATE"):
		if c.state.phase != 1 || len(args) < 3 || args[0].Value != c.state.op.OwnerUserID || args[1].Value != c.state.op.WorkspaceID || args[2].Value != c.state.op.SourceIdentityHash {
			return nil, errors.New("source head was not locked after origin and before operation")
		}
		c.state.phase = 2
		h := c.state.head
		return &sourceApprovalSQLRows{columns: []string{"owner_user_id", "workspace_id", "source_identity_hash", "origin_id", "operation_id", "revision_hash", "observation_id", "observation_generation", "config_epoch", "state"}, values: [][]driver.Value{{h.OwnerUserID, h.WorkspaceID, h.SourceIdentityHash, h.OriginID.String(), h.OperationID.String(), h.RevisionHash, h.ObservationID.String(), h.ObservationGeneration, h.ConfigEpoch, h.State}}}, nil
	case strings.HasPrefix(query, `SELECT * FROM "operations"`) && strings.Contains(query, "FOR UPDATE"):
		if c.state.phase != 2 || len(args) < 3 || fmt.Sprint(args[0].Value) != c.state.op.ID.String() || args[1].Value != c.state.op.OwnerUserID || args[2].Value != c.state.op.WorkspaceID {
			return nil, errors.New("operation was not locked after origin and source head")
		}
		c.state.phase = 3
		columns, values := sourceApprovalSQLRow(c.state.op)
		return &sourceApprovalSQLRows{columns: columns, values: [][]driver.Value{values}}, nil
	case strings.Contains(query, `count(*) FROM "operation_source_observations"`):
		if (c.state.phase != 3 || len(args) != 5) && (c.state.phase != 4 || len(args) != 7) {
			return nil, errors.New("operation observation was not checked after all authority locks")
		}
		if len(args) == 5 {
			c.state.phase = 4
		} else {
			c.state.phase = 5
		}
		return &sourceApprovalSQLRows{columns: []string{"count"}, values: [][]driver.Value{{int64(1)}}}, nil
	default:
		return nil, fmt.Errorf("unexpected source approval SQL query: %s", query)
	}
}
func (r *sourceApprovalSQLRows) Columns() []string { return r.columns }
func (r *sourceApprovalSQLRows) Close() error      { return nil }
func (r *sourceApprovalSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
func (tx *sourceApprovalSQLTransaction) Commit() error {
	tx.state.active = false
	tx.state.commits++
	if tx.state.supersedeOnCommit {
		tx.state.supersedeOnCommit = false
		tx.state.head.OperationID = uuid.New()
	}
	return nil
}
func (tx *sourceApprovalSQLTransaction) Rollback() error {
	tx.state.active = false
	tx.state.rollbacks++
	return nil
}

func TestGormSourceApprovalRechecksSupersededHeadAtPostgresApprovalBoundary(t *testing.T) {
	_, memory, op, _ := sourceApprovalObservedFixture(t, false)
	memory.mu.Lock()
	origin := memory.sourceOrigins[sourceOriginKey{op.OwnerUserID, op.WorkspaceID, *op.AccountFeedID}]
	head := memory.sourceHeads[sourceHeadKey{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash}]
	memory.mu.Unlock()
	state := &sourceApprovalSQLState{op: op, head: head, origin: origin, supersedeOnCommit: true}
	sqlDB := sql.OpenDB(sourceApprovalSQLConnector{state: state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(NewGormRepository(db))
	preview, err := service.PreviewSourceApprovalContext(context.Background(), op)
	if err != nil {
		t.Fatalf("Postgres-backed preview rejected current accepted head: %v", err)
	}
	if state.phase != 5 || state.commits != 1 || state.rollbacks != 0 {
		t.Fatalf("source approval preview did not lock origin -> head -> operation -> observation: %+v", state)
	}
	reviewed := op
	updated, event := sourceApprovalCandidate(t, op, preview)
	_, err = NewGormRepository(db).ApproveSourceDerived(context.Background(), reviewed, updated, event, preview.RevisionDigest)
	if !errors.Is(err, ErrSourceHeadSuperseded) {
		t.Fatalf("Postgres approval boundary accepted a head superseded after preview: %v", err)
	}
	if state.phase != 4 || state.commits != 1 || state.rollbacks != 1 || state.writes != 0 {
		t.Fatalf("stale approval did not relock origin -> head -> operation and reject before writes: %+v", state)
	}
}

func TestSourceApprovalReceiptCannotAuthorizeMutatedApprovedRevision(t *testing.T) {
	service, _, op := sourceApprovalFixture(t)
	preview, _ := service.PreviewSourceApproval(op)
	approved, _, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	changed := *approved
	changed.Description += " changed after approval"
	mutated, err := service.Save(changed, "operation_updated", string(OwnerHAI), "post-approval mutation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SourceApprovalForExecution(*mutated); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("mutated approved revision error = %v", err)
	}
}

func TestSourceApprovalReceiptConsumedOnceWithFencedRunningEvent(t *testing.T) {
	service, repo, op := sourceApprovalFixture(t)
	for i := 0; i < 125; i++ {
		old := time.Now().UTC().Add(-time.Duration(125-i) * time.Minute)
		if err := repo.AppendEvent(&models.OperationEvent{
			OperationID: op.ID, EventType: "legacy_note", ActorType: string(OwnerHAI),
			Message: "historical operation event", PayloadJSON: "{}", CreatedAt: old,
		}); err != nil {
			t.Fatalf("append historical event %d: %v", i, err)
		}
	}
	preview, _ := service.PreviewSourceApproval(op)
	approved, receipt, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatalf("claim approved operation: %v", err)
	}
	if claimed.Operation.Version != approved.Version {
		t.Fatalf("claim version=%d approved version=%d", claimed.Operation.Version, approved.Version)
	}
	prepared := claimed.Operation
	prepared.RuntimeID = sourceApprovalSafeRuntimeID
	prepared.VerificationStatus = string(VerificationPending)
	running, err := service.ConsumeSourceApprovalClaimed(context.Background(), claimed.Claim, claimed.Operation, prepared, string(OwnerHAI), "worker")
	if err != nil {
		t.Fatalf("consume approval: %v", err)
	}
	if err := service.ValidateConsumedSourceApproval(*running); err != nil {
		t.Fatalf("consumed receipt failed final gate: %v", err)
	}
	if _, err := service.ConsumeSourceApprovalClaimed(context.Background(), claimed.Claim, *approved, prepared, string(OwnerHAI), "worker-replay"); !errors.Is(err, ErrSourceApprovalReplayed) {
		t.Fatalf("replayed receipt error = %v", err)
	}
	events, err := service.listSourceApprovalEvents(op.ID)
	if err != nil {
		t.Fatal(err)
	}
	var consumed int
	for _, event := range events {
		var payload sourceApprovalEventPayload
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil && payload.ConsumedReceipt == receipt.ID.String() {
			consumed++
			if event.BeforeStatus != string(StatusApproved) || event.AfterStatus != string(StatusRunning) || event.ActorID != "worker" {
				t.Fatalf("consumption audit provenance = %#v", event)
			}
		}
	}
	if consumed != 1 {
		t.Fatalf("durable receipt consumption events=%d, want exactly one", consumed)
	}
}

func TestSourceApprovalReceiptRejectsStaleOperationAtConsumption(t *testing.T) {
	service, _, op := sourceApprovalFixture(t)
	preview, _ := service.PreviewSourceApproval(op)
	approved, _, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stale := *approved
	stale.RecommendedAction = "different operation"
	if _, err := service.ConsumeSourceApprovalClaimed(context.Background(), claimed.Claim, *approved, stale, string(OwnerHAI), "worker"); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("stale consumption error = %v", err)
	}
	stale = *approved
	stale.RuntimeID = sourceApprovalSafeRuntimeID
	stale.VerificationStatus = string(VerificationPending)
	stale.WorldModelStateJSON = `{"unrelatedPolicyContext":"mutated"}`
	if _, err := service.ConsumeSourceApprovalClaimed(context.Background(), claimed.Claim, *approved, stale, string(OwnerHAI), "worker"); !errors.Is(err, ErrSourceApprovalStale) {
		t.Fatalf("mutated non-execution world-model state error = %v", err)
	}
}

func TestSourceApprovalFinalGateRejectsMutatedExecutionSnapshot(t *testing.T) {
	service, _, op := sourceApprovalFixture(t)
	preview, _ := service.PreviewSourceApproval(op)
	approved, _, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	prepared := claimed.Operation
	prepared.RuntimeID = sourceApprovalSafeRuntimeID
	prepared.VerificationStatus = string(VerificationPending)
	running, err := service.ConsumeSourceApprovalClaimed(context.Background(), claimed.Claim, *approved, prepared, string(OwnerHAI), "worker")
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*models.Operation)
	}{
		{"worldModelState", func(v *models.Operation) { v.WorldModelStateJSON = `{"changed":true}` }},
		{"runtimeId", func(v *models.Operation) { v.RuntimeID = "other-runtime" }},
		{"modelProviderId", func(v *models.Operation) { v.ModelProviderID = "other-provider" }},
		{"modelId", func(v *models.Operation) { v.ModelID = "other-model" }},
		{"verificationStatus", func(v *models.Operation) { v.VerificationStatus = string(VerificationPassed) }},
		{"resultSummary", func(v *models.Operation) { v.ResultSummary = "unexpected result" }},
		{"lastError", func(v *models.Operation) { v.LastError = "unexpected error" }},
		{"updatedAt", func(v *models.Operation) { v.UpdatedAt = v.UpdatedAt.Add(time.Second) }},
		{"completedAt", func(v *models.Operation) { now := time.Now().UTC(); v.CompletedAt = &now }},
		{"nextReviewAt", func(v *models.Operation) { now := time.Now().UTC(); v.NextReviewAt = &now }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			changed := *running
			tc.edit(&changed)
			if err := service.ValidateConsumedSourceApproval(changed); !errors.Is(err, ErrSourceApprovalStale) {
				t.Fatalf("mutated execution snapshot gate error=%v, want stale", err)
			}
		})
	}
}
