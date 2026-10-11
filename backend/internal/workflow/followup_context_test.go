package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type contextFollowUpHandlerService struct {
	Service
	legacyCalls, contextCalls int
	gotContext                context.Context
	gotOwner                  string
	gotLimit                  int
}

func (s *contextFollowUpHandlerService) RunDueOpenLoopsForOwner(string, RunDueRequest) (*OpenLoopRunSummary, error) {
	s.legacyCalls++
	return &OpenLoopRunSummary{}, nil
}

func (s *contextFollowUpHandlerService) RunDueOpenLoopsForOwnerContext(ctx context.Context, owner string, request RunDueRequest) (*OpenLoopRunSummary, error) {
	s.contextCalls++
	s.gotContext, s.gotOwner, s.gotLimit = ctx, owner, request.Limit
	return &OpenLoopRunSummary{}, nil
}

func TestFollowUpHTTPRejectsInvalidInputBeforeExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, owner, body string
		status            int
	}{
		{"missing owner", "", `{"limit":5}`, http.StatusUnauthorized},
		{"malformed", "alice", `{"limit":`, http.StatusBadRequest},
		{"wrong type", "alice", `{"limit":"all"}`, http.StatusBadRequest},
		{"null", "alice", `null`, http.StatusBadRequest},
		{"negative limit", "alice", `{"limit":-1}`, http.StatusBadRequest},
		{"excessive limit", "alice", `{"limit":51}`, http.StatusBadRequest},
		{"second object", "alice", `{"limit":5} {}`, http.StatusBadRequest},
		{"trailing garbage", "alice", `{"limit":5} invalid`, http.StatusBadRequest},
		{"oversized body", "alice", `{"limit":5,"padding":"` + strings.Repeat("x", 4096) + `"}`, http.StatusBadRequest},
		{"oversized trailing whitespace", "alice", `{"limit":5}` + strings.Repeat(" ", 4096), http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := &contextFollowUpHandlerService{}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/workflow/run-due-open-loops", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			if test.owner != "" {
				c.Set(identity.ContextSubjectKey, test.owner)
			}
			NewHandler(engine).RunDueOpenLoops(c)
			if response.Code != test.status || engine.legacyCalls != 0 || engine.contextCalls != 0 {
				t.Fatalf("invalid request executed: status=%d legacy=%d contextual=%d", response.Code, engine.legacyCalls, engine.contextCalls)
			}
		})
	}
}

func TestFollowUpHTTPPreservesRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &contextFollowUpHandlerService{}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/workflow/run-due-open-loops", strings.NewReader(`{"limit":5}`)).WithContext(ctx)
	c.Set(identity.ContextSubjectKey, "alice")
	NewHandler(engine).RunDueOpenLoops(c)
	if response.Code != http.StatusOK || engine.legacyCalls != 0 || engine.contextCalls != 1 || engine.gotContext != ctx || engine.gotOwner != "alice" || engine.gotLimit != 5 {
		t.Fatalf("request context discarded: status=%d legacy=%d contextual=%d owner=%q limit=%d", response.Code, engine.legacyCalls, engine.contextCalls, engine.gotOwner, engine.gotLimit)
	}
	cancel()
	if !errors.Is(engine.gotContext.Err(), context.Canceled) {
		t.Fatal("request cancellation did not reach service")
	}
}

type followUpContextProbe struct {
	scopes, reads, claims, commits, releases int
	contexts                                 []context.Context
	cancel                                   context.CancelFunc
	boundary                                 string
}

type contextualFollowUpMemoryRepo struct {
	*fakeWorkflowRepo
	ctx   context.Context
	probe *followUpContextProbe
}

func (r *contextualFollowUpMemoryRepo) withFollowUpContext(ctx context.Context) (Repository, error) {
	r.probe.scopes++
	return &contextualFollowUpMemoryRepo{fakeWorkflowRepo: r.fakeWorkflowRepo, ctx: ctx, probe: r.probe}, nil
}

func (r *contextualFollowUpMemoryRepo) FindDashboardOpenLoopsForOwner(owner string, now time.Time) ([]models.WorkflowOpenLoop, error) {
	r.probe.reads++
	r.probe.contexts = append(r.probe.contexts, r.ctx)
	loops, err := r.fakeWorkflowRepo.FindDashboardOpenLoopsForOwner(owner, now)
	if r.probe.boundary == "lookup" {
		r.probe.cancel()
	}
	return loops, err
}

func (r *contextualFollowUpMemoryRepo) ClaimDueOpenLoopForOwner(owner string, id uuid.UUID, claim string, now, until time.Time) (*models.WorkflowOpenLoop, bool, error) {
	r.probe.claims++
	r.probe.contexts = append(r.probe.contexts, r.ctx)
	loop, owned, err := r.fakeWorkflowRepo.ClaimDueOpenLoopForOwner(owner, id, claim, now, until)
	if r.probe.boundary == "claim" {
		r.probe.cancel()
	}
	return loop, owned, err
}

func (r *contextualFollowUpMemoryRepo) commitDueOpenLoop(owner string, workflowID, loopID uuid.UUID, claim string) (*followUpCommit, error) {
	r.probe.commits++
	r.probe.contexts = append(r.probe.contexts, r.ctx)
	if r.probe.boundary == "transaction" {
		r.probe.cancel()
		return nil, r.ctx.Err()
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	committed, err := r.fakeWorkflowRepo.commitDueOpenLoop(owner, workflowID, loopID, claim)
	if r.probe.boundary == "committed" {
		r.probe.cancel()
	}
	return committed, err
}

func (r *contextualFollowUpMemoryRepo) releaseDueOpenLoopClaim(owner string, workflowID, loopID uuid.UUID, claim string) (bool, error) {
	r.probe.releases++
	return r.fakeWorkflowRepo.releaseDueOpenLoopClaim(owner, workflowID, loopID, claim)
}

func (r *contextualFollowUpMemoryRepo) CreateDecision(decision *models.WorkflowDecision) (*models.WorkflowDecision, error) {
	stored, err := r.fakeWorkflowRepo.CreateDecision(decision)
	if r.probe.boundary == "emergency_decision" {
		r.probe.cancel()
	}
	return stored, err
}

type contextualFollowUpProjector struct {
	calls  int
	ctx    context.Context
	cancel context.CancelFunc
}

func (p *contextualFollowUpProjector) ProjectOperationalRecord(ctx context.Context, _ lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error) {
	p.calls++
	p.ctx = ctx
	if p.cancel != nil {
		p.cancel()
	}
	return lifeontology.OperationalProjectionResult{}, ctx.Err()
}

func TestFollowUpContextStopsAtLookupClaimTransactionAndConfirmedCommit(t *testing.T) {
	for _, boundary := range []string{"lookup", "claim", "transaction", "committed", "graph"} {
		t.Run(boundary, func(t *testing.T) {
			base, _, loop := followUpMemoryFixture(t)
			loop.Status, loop.ClaimID, loop.LeaseUntil = "open", "", nil
			base.openLoops[loop.WorkflowID][0] = loop
			other := loop
			other.ID = uuid.New()
			if _, err := base.CreateOpenLoop(&other); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probe := &followUpContextProbe{cancel: cancel, boundary: boundary}
			repo := &contextualFollowUpMemoryRepo{fakeWorkflowRepo: base, probe: probe}
			projector := &contextualFollowUpProjector{}
			if boundary == "graph" {
				projector.cancel = cancel
			}
			engine := NewService(repo).(*service)
			engine.lifeOntologyProjector = projector
			summary, err := engine.RunDueOpenLoopsForOwnerContext(ctx, " alice ", RunDueRequest{Limit: 5})
			if !errors.Is(err, context.Canceled) || probe.scopes != 1 || probe.reads != 1 || probe.releases != 0 || repo.ctx != nil {
				t.Fatalf("cancellation released/repeated work or mutated shared scope: err=%v probe=%+v", err, probe)
			}
			for _, received := range probe.contexts {
				if received != ctx {
					t.Fatal("store boundary lost execution context")
				}
			}
			wantClaims, wantCommits, wantProposals := 1, 1, 0
			switch boundary {
			case "lookup":
				wantClaims, wantCommits = 0, 0
			case "claim":
				wantCommits = 0
			case "committed", "graph":
				wantProposals = 1
			}
			if probe.claims != wantClaims || probe.commits != wantCommits || len(base.proposals[loop.WorkflowID]) != wantProposals {
				t.Fatalf("new work after cancellation: claims=%d commits=%d proposals=%d", probe.claims, probe.commits, len(base.proposals[loop.WorkflowID]))
			}
			if boundary == "lookup" && summary != nil {
				t.Fatal("lookup cancellation reported completed processing")
			}
			if boundary == "claim" || boundary == "transaction" {
				if base.openLoops[loop.WorkflowID][0].Status != "processing" || base.openLoops[loop.WorkflowID][0].ClaimID == "" || base.openLoops[loop.WorkflowID][0].LeaseUntil == nil {
					t.Fatal("uncertain claim was erased instead of left for recovery")
				}
			}
			if wantProposals == 1 && (summary == nil || summary.Triggered != 1 || len(base.decisions[loop.WorkflowID]) != 1 || base.openLoops[loop.WorkflowID][0].Status != "triggered") {
				t.Fatal("known committed result was discarded after cancellation")
			}
			if base.openLoops[loop.WorkflowID][1].Status != "open" {
				t.Fatal("second loop claimed after cancellation")
			}
			if boundary == "graph" {
				if projector.calls != 1 || projector.ctx != ctx || len(base.events[loop.WorkflowID]) != 1 {
					t.Fatal("graph lost context or generated post-cancellation audit work")
				}
			} else if projector.calls != 0 {
				t.Fatal("advisory graph started after cancellation")
			}
		})
	}
}

func TestFollowUpContextRequiresOwnerContextAndRepositoryCapability(t *testing.T) {
	base, _, loop := followUpMemoryFixture(t)
	base.openLoops[loop.WorkflowID][0].Status = "open"
	probe := &followUpContextProbe{}
	repo := &contextualFollowUpMemoryRepo{fakeWorkflowRepo: base, probe: probe}
	engine := NewService(repo).(*service)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		ctx   context.Context
		owner string
	}{{nil, "alice"}, {context.Background(), " "}, {canceled, "alice"}} {
		if _, err := engine.RunDueOpenLoopsForOwnerContext(test.ctx, test.owner, RunDueRequest{}); err == nil {
			t.Fatal("invalid context/owner accepted")
		}
	}
	if probe.scopes != 0 || probe.reads != 0 || probe.claims != 0 {
		t.Fatal("invalid contextual execution reached repository")
	}
	if _, err := NewService(base).(ContextualFollowUpService).RunDueOpenLoopsForOwnerContext(context.Background(), "alice", RunDueRequest{}); !errors.Is(err, ErrFollowUpContextUnavailable) {
		t.Fatal("legacy repository silently used without context support")
	}
	if _, err := (*service)(nil).RunDueOpenLoopsForOwnerContext(context.Background(), "alice", RunDueRequest{}); !errors.Is(err, ErrFollowUpContextUnavailable) {
		t.Fatal("nil service did not fail closed")
	}
}

func TestFollowUpContextStopsEmergencyAuditAfterCancellation(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	base, _, loop := followUpMemoryFixture(t)
	base.openLoops[loop.WorkflowID][0].Status = "open"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &followUpContextProbe{cancel: cancel, boundary: "emergency_decision"}
	repo := &contextualFollowUpMemoryRepo{fakeWorkflowRepo: base, probe: probe}
	_, err := NewService(repo).(ContextualFollowUpService).RunDueOpenLoopsForOwnerContext(ctx, "alice", RunDueRequest{})
	if !errors.Is(err, context.Canceled) || probe.claims != 0 || len(base.events[loop.WorkflowID]) != 0 || len(base.decisions[loop.WorkflowID]) != 1 {
		t.Fatalf("emergency path ignored cancellation or lost recorded decision: err=%v claims=%d events=%d decisions=%d", err, probe.claims, len(base.events[loop.WorkflowID]), len(base.decisions[loop.WorkflowID]))
	}
}

func TestFollowUpContextDoesNotSelectAnotherOwner(t *testing.T) {
	base, _, loop := followUpMemoryFixture(t)
	base.openLoops[loop.WorkflowID][0].Status = "open"
	probe := &followUpContextProbe{}
	repo := &contextualFollowUpMemoryRepo{fakeWorkflowRepo: base, probe: probe}
	summary, err := NewService(repo).(ContextualFollowUpService).RunDueOpenLoopsForOwnerContext(context.Background(), "bob", RunDueRequest{})
	if err != nil || summary.Checked != 0 || probe.claims != 0 || len(base.proposals[loop.WorkflowID]) != 0 {
		t.Fatal("contextual follow-up escaped owner scope")
	}
}

func TestFollowUpBatchContextProcessesOwnersWithExistingProjection(t *testing.T) {
	base, _, loop := followUpMemoryFixture(t)
	loop.Status, loop.ClaimID, loop.LeaseUntil = "open", "", nil
	base.openLoops[loop.WorkflowID][0] = loop
	bob := *base.items[loop.WorkflowID]
	bob.ID, bob.OwnerIdentity = uuid.New(), "bob"
	if _, err := base.CreateItem(&bob); err != nil {
		t.Fatal(err)
	}
	bobLoop := loop
	bobLoop.ID, bobLoop.WorkflowID = uuid.New(), bob.ID
	if _, err := base.CreateOpenLoop(&bobLoop); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &followUpContextProbe{}
	repo := &contextualFollowUpMemoryRepo{fakeWorkflowRepo: base, probe: probe}
	engine := NewService(repo).(*service)
	summary, err := engine.RunDueOpenLoopsContext(ctx, RunDueRequest{Limit: 2})
	if err != nil || summary == nil || summary.Checked != 2 || summary.Triggered != 2 || probe.scopes != 1 || probe.claims != 2 || probe.commits != 2 || repo.ctx != nil {
		t.Fatalf("trusted batch did not use existing scoped projection: err=%v summary=%+v probe=%+v", err, summary, probe)
	}
	for _, id := range []uuid.UUID{loop.WorkflowID, bob.ID} {
		if len(base.proposals[id]) != 1 || len(base.decisions[id]) != 1 || base.openLoops[id][0].Status != "triggered" {
			t.Fatal("owner projection missing or repeated")
		}
	}
	for _, received := range probe.contexts {
		if received != ctx {
			t.Fatal("batch storage discarded context")
		}
	}
	if _, err := engine.RunDueOpenLoopsForOwnerContext(ctx, " ", RunDueRequest{Limit: 2}); err == nil || probe.scopes != 1 {
		t.Fatal("batch capability relaxed authenticated owner validation")
	}
}

func TestFollowUpBatchContextRefusesMissingContextAndRepository(t *testing.T) {
	base, _, loop := followUpMemoryFixture(t)
	base.openLoops[loop.WorkflowID][0].Status = "open"
	probe := &followUpContextProbe{}
	repo := &contextualFollowUpMemoryRepo{fakeWorkflowRepo: base, probe: probe}
	engine := NewService(repo).(*service)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, invalid := range []context.Context{nil, ctx} {
		if _, err := engine.RunDueOpenLoopsContext(invalid, RunDueRequest{}); err == nil {
			t.Fatal("invalid batch context accepted")
		}
	}
	if probe.scopes != 0 || probe.reads != 0 || probe.claims != 0 {
		t.Fatal("invalid batch touched storage")
	}
	if _, err := NewService(base).(ContextualFollowUpBatchService).RunDueOpenLoopsContext(context.Background(), RunDueRequest{}); !errors.Is(err, ErrFollowUpContextUnavailable) {
		t.Fatal("batch fell back to context-free repository")
	}
	if _, err := (*service)(nil).RunDueOpenLoopsContext(context.Background(), RunDueRequest{}); !errors.Is(err, ErrFollowUpContextUnavailable) {
		t.Fatal("nil batch service did not fail closed")
	}
}

func TestFollowUpBatchContextPreservesCanceledUncertainClaim(t *testing.T) {
	base, _, loop := followUpMemoryFixture(t)
	base.openLoops[loop.WorkflowID][0].Status = "open"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &followUpContextProbe{cancel: cancel, boundary: "claim"}
	repo := &contextualFollowUpMemoryRepo{fakeWorkflowRepo: base, probe: probe}
	_, err := NewService(repo).(ContextualFollowUpBatchService).RunDueOpenLoopsContext(ctx, RunDueRequest{Limit: 2})
	if !errors.Is(err, context.Canceled) || probe.claims != 1 || probe.commits != 0 || probe.releases != 0 || base.openLoops[loop.WorkflowID][0].Status != "processing" {
		t.Fatal("batch cancellation lost claim or started projection")
	}
}

func TestFollowUpGormContextScopesQueriesAndClaimsWithoutChangingRoot(t *testing.T) {
	db := leaseExpiryDryRunDB(t)
	repo := &GormRepository{DB: db}
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	scoped1, err := repo.withFollowUpContext(ctx1)
	if err != nil {
		t.Fatal(err)
	}
	scoped2, err := repo.withFollowUpContext(ctx2)
	if err != nil {
		t.Fatal(err)
	}
	if repo.DB.Statement.Context == ctx1 || repo.DB.Statement.Context == ctx2 || scoped1 == scoped2 {
		t.Fatal("worker contexts mutated shared database")
	}
	var contexts []context.Context
	if err := db.Callback().Query().After("gorm:query").Register("followup_context:query", func(tx *gorm.DB) { contexts = append(contexts, tx.Statement.Context) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("followup_context:update", func(tx *gorm.DB) { contexts = append(contexts, tx.Statement.Context); tx.RowsAffected = 1 }); err != nil {
		t.Fatal(err)
	}
	for i, scoped := range []Repository{scoped1, scoped2} {
		start := len(contexts)
		if _, err := scoped.FindDashboardOpenLoopsForOwner("alice", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, _, err := scoped.ClaimDueOpenLoopForOwner("alice", uuid.New(), "context-worker", time.Now(), time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if len(contexts)-start != 3 {
			t.Fatal("query/claim/refetch were not all inspected")
		}
		want := []context.Context{ctx1, ctx2}[i]
		for _, got := range contexts[start:] {
			if got != want {
				t.Fatal("actual GORM statement discarded its execution context")
			}
		}
	}
	cancel1()
	if _, err := repo.withFollowUpContext(ctx1); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled context accepted new store scope")
	}
	if scoped2.(*GormRepository).DB.Statement.Context.Err() != nil || repo.DB.Statement.Context.Err() != nil {
		t.Fatal("one worker cancellation affected another worker/root")
	}
}

type legacyFollowUpHandlerService struct {
	Service
	calls int
}

func (s *legacyFollowUpHandlerService) RunDueOpenLoopsForOwner(string, RunDueRequest) (*OpenLoopRunSummary, error) {
	s.calls++
	return &OpenLoopRunSummary{}, nil
}

type emptyContextFollowUpHandlerService struct{ Service }

func (*emptyContextFollowUpHandlerService) RunDueOpenLoopsForOwnerContext(context.Context, string, RunDueRequest) (*OpenLoopRunSummary, error) {
	return nil, nil
}

func TestFollowUpHTTPFailsClosedWhenContextualDependenciesAreUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := &legacyFollowUpHandlerService{}
	for _, test := range []struct {
		name    string
		handler *Handler
		status  int
	}{
		{"nil handler", nil, http.StatusServiceUnavailable},
		{"nil service", NewHandler(nil), http.StatusServiceUnavailable},
		{"legacy adapter", NewHandler(legacy), http.StatusServiceUnavailable},
		{"legacy store", NewHandler(NewService(newFakeWorkflowRepo())), http.StatusServiceUnavailable},
		{"null result", NewHandler(&emptyContextFollowUpHandlerService{}), http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/workflow/run-due-open-loops", strings.NewReader(`{}`))
			c.Set(identity.ContextSubjectKey, "alice")
			test.handler.RunDueOpenLoops(c)
			if response.Code != test.status || legacy.calls != 0 {
				t.Fatalf("unavailable dependency executed/reported success: status=%d legacy=%d", response.Code, legacy.calls)
			}
		})
	}
}
