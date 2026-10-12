package ambient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/memoryengine"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

type ambientScanTrace struct {
	calls                 []string
	contexts              []context.Context
	boundary              string
	stop                  func()
	failure               error
	settlementFailure     bool
	settlementScopeNil    bool
	settlementScopeError  error
	settlementError       error
	alterOutcome          func(*models.AmbientScan)
	alterCreation         func(*models.AmbientScan)
	stored                *models.AmbientScan
	legacy, saves, prunes int
	outcomeWasLive        bool
}

func (p *ambientScanTrace) hit(stage string) {
	p.calls = append(p.calls, stage)
	if p.boundary == stage && p.stop != nil {
		p.stop()
	}
}

type ambientContextRepositoryProbe struct {
	*ambientRepositoryStub
	trace *ambientScanTrace
	ctx   context.Context
}

func (r *ambientContextRepositoryProbe) withScanContext(ctx context.Context) (Repository, error) {
	r.trace.contexts = append(r.trace.contexts, ctx)
	if len(r.trace.contexts) > 1 {
		if r.trace.settlementScopeNil || r.trace.settlementScopeError != nil {
			return nil, r.trace.settlementScopeError
		}
	}
	return &ambientContextRepositoryProbe{ambientRepositoryStub: r.ambientRepositoryStub, trace: r.trace, ctx: ctx}, nil
}

func (r *ambientContextRepositoryProbe) EnsureNeeds([]models.AmbientNeed) error {
	r.trace.hit("ensure")
	return nil
}
func (r *ambientContextRepositoryProbe) Needs() ([]models.AmbientNeed, error) {
	r.trace.hit("needs")
	return defaultNeeds(), nil
}
func (r *ambientContextRepositoryProbe) CreateScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	copy := *scan
	if copy.ID == uuid.Nil {
		copy.ID = uuid.New()
	}
	if r.trace.alterCreation != nil {
		r.trace.alterCreation(&copy)
	}
	r.trace.stored = &copy
	r.trace.hit("create")
	return &copy, nil
}
func (r *ambientContextRepositoryProbe) UpdateScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	r.trace.hit("settle")
	r.trace.outcomeWasLive = r.ctx.Err() == nil
	if r.trace.settlementError != nil {
		return nil, r.trace.settlementError
	}
	if r.trace.settlementFailure || r.ctx.Err() != nil {
		return nil, ErrScanOutcomeUnconfirmed
	}
	copy := *scan
	if r.trace.alterOutcome != nil {
		r.trace.alterOutcome(scan)
		copy = *scan
	}
	r.trace.stored = &copy
	return &copy, nil
}
func (r *ambientContextRepositoryProbe) FindOpportunityByFingerprint(string) (*models.AmbientOpportunity, error) {
	r.trace.hit("find")
	return nil, nil
}
func (r *ambientContextRepositoryProbe) SaveOpportunity(item *models.AmbientOpportunity) (*models.AmbientOpportunity, error) {
	r.trace.saves++
	r.trace.hit("save")
	return item, nil
}
func (r *ambientContextRepositoryProbe) Opportunities(string, int) ([]models.AmbientOpportunity, error) {
	r.trace.hit("list")
	return nil, nil
}
func (r *ambientContextRepositoryProbe) PruneScans(int) error {
	r.trace.prunes++
	r.trace.hit("prune")
	return nil
}

type ambientContextWorkflowProbe struct {
	WorkflowService
	trace *ambientScanTrace
	ctx   context.Context
}

func (w *ambientContextWorkflowProbe) Dashboard() (*workflow.WorkflowDashboard, error) {
	w.trace.hit("dashboard")
	return &workflow.WorkflowDashboard{}, nil
}
func (w *ambientContextWorkflowProbe) Items(bool) ([]models.WorkflowItem, error) {
	w.trace.hit("items")
	return []models.WorkflowItem{{ID: uuid.New(), Title: "Review blocked work", CurrentState: workflow.StateBlocked, NextAction: "Review evidence", Confidence: 1, RiskLevel: "low"}}, nil
}
func (w *ambientContextWorkflowProbe) RunDueOpenLoops(workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	w.trace.legacy++
	return &workflow.OpenLoopRunSummary{}, nil
}
func (w *ambientContextWorkflowProbe) RunDueOpenLoopsContext(ctx context.Context, _ workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	w.ctx = ctx
	w.trace.hit("followup")
	return &workflow.OpenLoopRunSummary{Triggered: 1, Resolved: 1}, ctx.Err()
}
func (w *ambientContextWorkflowProbe) RunDue(workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error) {
	w.trace.hit("task")
	return &workflow.WorkflowRunSummary{Completed: 1}, nil
}

type ambientContextMemoryProbe struct {
	memoryengine.Service
	trace *ambientScanTrace
}

func (m *ambientContextMemoryProbe) Dashboard() (*memoryengine.CommandDashboard, error) {
	m.trace.hit("memory")
	return &memoryengine.CommandDashboard{}, m.trace.failure
}

func ambientContextFixture(trace *ambientScanTrace) (*service, *ambientContextRepositoryProbe, *ambientContextWorkflowProbe) {
	repo := &ambientContextRepositoryProbe{ambientRepositoryStub: &ambientRepositoryStub{}, trace: trace}
	w := &ambientContextWorkflowProbe{trace: trace}
	return NewService(repo, w, &ambientContextMemoryProbe{trace: trace}).(*service), repo, w
}

func TestAmbientContextServiceStopsAtStorageAndExecutionBoundaries(t *testing.T) {
	t.Setenv("AMBIENT_EXECUTION_ENABLED", "true")
	t.Setenv("AMBIENT_MINIMUM_SCORE", "0")
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	for _, boundary := range []string{"ensure", "create", "needs", "dashboard", "items", "memory", "find", "save", "list", "followup", "task"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			trace := &ambientScanTrace{boundary: boundary, stop: cancel}
			engine, root, w := ambientContextFixture(trace)
			scan, err := engine.ScanContext(ctx, "test", func() bool { return true })
			if !errors.Is(err, context.Canceled) || engine.scanning.Load() || root.ctx != nil || trace.legacy != 0 || trace.prunes != 0 {
				t.Fatalf("cancellation lost or continued work: err=%v calls=%v", err, trace.calls)
			}
			if boundary == "ensure" {
				if scan != nil || len(trace.calls) != 1 || trace.stored != nil {
					t.Fatal("scan admitted after cancellation")
				}
				return
			}
			if scan == nil || scan.Status != "failed" || trace.stored.Status != "failed" || trace.calls[len(trace.calls)-1] != "settle" || trace.calls[len(trace.calls)-2] != boundary {
				t.Fatalf("scan advanced or lost failure outcome: calls=%v scan=%+v", trace.calls, scan)
			}
			if len(trace.contexts) != 2 || trace.contexts[0] != ctx || trace.contexts[1] == ctx || !trace.outcomeWasLive {
				t.Fatal("storage scopes discarded context or recorded outcome with canceled scope")
			}
			deadline, ok := trace.contexts[1].Deadline()
			if !ok || time.Until(deadline) > 2*time.Second {
				t.Fatal("outcome recording is not bounded")
			}
			if boundary == "followup" && (w.ctx != ctx || scan.Advanced != 2) {
				t.Fatal("follow-up context or confirmed partial result discarded")
			}
			if boundary == "task" && scan.Advanced != 3 {
				t.Fatal("confirmed task result discarded")
			}
		})
	}
}

func TestAmbientContextServiceRechecksGateBeforeMoreEffects(t *testing.T) {
	t.Setenv("AMBIENT_EXECUTION_ENABLED", "true")
	t.Setenv("AMBIENT_MINIMUM_SCORE", "0")
	for _, boundary := range []string{"find", "save", "followup"} {
		t.Run(boundary, func(t *testing.T) {
			allowed := true
			trace := &ambientScanTrace{boundary: boundary, stop: func() { allowed = false }}
			engine, _, _ := ambientContextFixture(trace)
			scan, err := engine.ScanContext(context.Background(), "test", func() bool { return allowed })
			if err == nil || scan == nil || scan.Status != "failed" || trace.prunes != 0 || trace.calls[len(trace.calls)-2] != boundary {
				t.Fatalf("safety pause continued scan: err=%v calls=%v", err, trace.calls)
			}
			if boundary == "find" && trace.saves != 0 {
				t.Fatal("proposal written after policy pause")
			}
			if boundary == "followup" && scan.Advanced != 2 {
				t.Fatal("known follow-up result lost during pause")
			}
		})
	}
}

func TestAmbientContextServiceRefusesInvalidContextAndUnsupportedRepository(t *testing.T) {
	trace := &ambientScanTrace{}
	engine, _, _ := ambientContextFixture(trace)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, invalid := range []context.Context{nil, ctx} {
		if _, err := engine.ScanContext(invalid, "test"); err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	if len(trace.calls) != 0 || len(trace.contexts) != 0 {
		t.Fatal("invalid context reached storage")
	}
	if _, err := NewService(&ambientRepositoryStub{}, nil, nil).(ContextualScanService).ScanContext(context.Background(), "test"); !errors.Is(err, ErrScanContextUnavailable) {
		t.Fatal("legacy repository bypassed contextual capability")
	}
	if _, err := (*service)(nil).ScanContext(context.Background(), "test"); !errors.Is(err, ErrScanContextUnavailable) {
		t.Fatal("nil service did not fail closed")
	}
}

func TestAmbientContextServiceRecordsRedactedFailureAndUnconfirmedAcknowledgement(t *testing.T) {
	for _, unconfirmed := range []bool{false, true} {
		trace := &ambientScanTrace{failure: errors.New("token=must-not-be-stored"), settlementFailure: unconfirmed}
		engine, _, _ := ambientContextFixture(trace)
		scan, err := engine.ScanContext(context.Background(), "test")
		if err == nil || scan == nil || strings.Contains(scan.ErrorMessage, "must-not-be-stored") || !strings.Contains(scan.ErrorMessage, "[REDACTED]") {
			t.Fatal("failed scan did not redact recognized credential")
		}
		want := "failed"
		if unconfirmed {
			want = "outcome_unconfirmed"
		}
		if scan.Status != want || (unconfirmed && !errors.Is(err, ErrScanOutcomeUnconfirmed)) {
			t.Fatal("unacknowledged outcome reported as settled")
		}
	}
}

func TestAmbientContextMissingSettlementScopeDoesNotPanic(t *testing.T) {
	trace := &ambientScanTrace{failure: errors.New("source unavailable"), settlementScopeNil: true}
	engine, _, _ := ambientContextFixture(trace)
	scan, err := engine.ScanContext(context.Background(), "test")
	if scan == nil || scan.Status != "outcome_unconfirmed" || !errors.Is(err, ErrScanOutcomeUnconfirmed) || engine.scanning.Load() {
		t.Fatalf("missing settlement scope lost uncertainty: scan=%+v err=%v", scan, err)
	}
}

func TestAmbientContextSettlementErrorsPreserveUncertaintyAndCauses(t *testing.T) {
	for _, scope := range []bool{true, false} {
		cause := errors.New("source unavailable")
		storage := errors.New("storage token=must-not-leak")
		trace := &ambientScanTrace{failure: cause}
		if scope {
			trace.settlementScopeError = storage
		} else {
			trace.settlementError = storage
		}
		engine, _, _ := ambientContextFixture(trace)
		scan, err := engine.ScanContext(context.Background(), "test")
		if scan == nil || scan.Status != "outcome_unconfirmed" || !errors.Is(err, ErrScanOutcomeUnconfirmed) || !errors.Is(err, cause) || !errors.Is(err, storage) {
			t.Fatalf("settlement error identity discarded: scope=%v scan=%+v err=%v", scope, scan, err)
		}
	}
}

func TestAmbientContextInvalidOutcomeCannotSettleOrPrune(t *testing.T) {
	t.Setenv("AMBIENT_EXECUTION_ENABLED", "false")
	mutations := map[string]func(*models.AmbientScan){
		"state":        func(s *models.AmbientScan) { s.Status = "running" },
		"id":           func(s *models.AmbientScan) { s.ID = uuid.New() },
		"owner":        func(s *models.AmbientScan) { s.OwnerIdentity = "foreign-owner" },
		"start":        func(s *models.AmbientScan) { s.StartedAt = s.StartedAt.Add(time.Second) },
		"finish":       func(s *models.AmbientScan) { s.CompletedAt = nil },
		"counts":       func(s *models.AmbientScan) { s.Advanced++ },
		"finish-value": func(s *models.AmbientScan) { *s.CompletedAt = s.CompletedAt.Add(time.Second) },
	}
	for name, mutate := range mutations {
		for _, failed := range []bool{true, false} {
			t.Run(name+map[bool]string{true: "-failure", false: "-completion"}[failed], func(t *testing.T) {
				trace := &ambientScanTrace{alterOutcome: mutate}
				if failed {
					trace.failure = errors.New("source unavailable")
				}
				engine, _, _ := ambientContextFixture(trace)
				scan, err := engine.ScanContext(context.Background(), "test")
				if scan == nil || scan.Status != "outcome_unconfirmed" || !errors.Is(err, ErrScanOutcomeUnconfirmed) || trace.prunes != 0 || scan.OwnerIdentity != "" || scan.Advanced != 0 || engine.scanning.Load() {
					t.Fatalf("invalid outcome accepted or changed known result: scan=%+v err=%v calls=%v", scan, err, trace.calls)
				}
			})
		}
	}
}

func TestAmbientContextSuccessfulExecutionPreservesKnownResults(t *testing.T) {
	t.Setenv("AMBIENT_EXECUTION_ENABLED", "true")
	t.Setenv("AMBIENT_MINIMUM_SCORE", "0")
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	trace := &ambientScanTrace{}
	engine, root, w := ambientContextFixture(trace)
	ctx := context.Background()
	scan, err := engine.ScanContext(ctx, "test", func() bool { return true })
	if err != nil || scan == nil || scan.Status != "completed" || scan.Advanced != 3 || scan.Created != 1 || trace.saves != 1 || trace.prunes != 1 || w.ctx != ctx || root.ctx != nil || trace.legacy != 0 || engine.scanning.Load() {
		t.Fatalf("known results or execution context lost: scan=%+v err=%v calls=%v", scan, err, trace.calls)
	}
}

func TestAmbientContextAcknowledgementAllowsDatabaseTimestampPrecision(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	completed := time.Now().UTC()
	scan := &models.AmbientScan{ID: uuid.New(), Status: "completed", StartedAt: started, CompletedAt: &completed, Created: 2, Advanced: 1}
	ack := snapshotScan(scan)
	ack.StartedAt = ack.StartedAt.Truncate(time.Microsecond)
	*ack.CompletedAt = ack.CompletedAt.Truncate(time.Microsecond)
	ack.CreatedAt, ack.UpdatedAt = started, completed
	if !scanOutcomeMatches(*scan, &ack) {
		t.Fatal("database timestamp precision or managed metadata refused")
	}
}

func TestAmbientContextInvalidCreationCannotStartMoreWork(t *testing.T) {
	t.Setenv("AMBIENT_EXECUTION_ENABLED", "false")
	for name, mutate := range map[string]func(*models.AmbientScan){
		"id":     func(s *models.AmbientScan) { s.ID = uuid.Nil },
		"owner":  func(s *models.AmbientScan) { s.OwnerIdentity = "foreign-owner" },
		"state":  func(s *models.AmbientScan) { s.Status = "completed" },
		"start":  func(s *models.AmbientScan) { s.StartedAt = s.StartedAt.Add(time.Second) },
		"counts": func(s *models.AmbientScan) { s.Advanced++ },
	} {
		t.Run(name, func(t *testing.T) {
			trace := &ambientScanTrace{alterCreation: mutate}
			engine, _, _ := ambientContextFixture(trace)
			scan, err := engine.ScanContext(context.Background(), "test")
			if !errors.Is(err, ErrScanOutcomeUnconfirmed) || scan == nil || scan.Status != "outcome_unconfirmed" || scan.OwnerIdentity != "" || scan.Advanced != 0 || len(trace.calls) != 2 || trace.calls[1] != "create" || trace.saves != 0 || trace.prunes != 0 {
				t.Fatalf("unacknowledged creation advanced work: scan=%+v err=%v calls=%v", scan, err, trace.calls)
			}
		})
	}
}
