package workflow

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type reminderStopContextKey struct{}

func TestReminderStopTransactionInheritsCallerContext(t *testing.T) {
	for _, scenario := range []string{"live", "canceled-at-begin", "already-canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), reminderStopContextKey{}, "owned-reminder"))
			defer cancel()
			pool := &followUpContextTransactionPool{err: errors.New("offline transaction boundary")}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "already-canceled" {
				cancel()
			}
			if scenario == "canceled-at-begin" {
				pool.cancel = cancel
			}
			result, err := (&GormRepository{DB: db.WithContext(ctx)}).ProcessReminderDelivery(reminderDeliveryCandidate{Authorization: models.WorkflowReminderDeliveryAuthorization{ID: uuid.New(), OwnerIdentity: "alice"}}, &reminderDeliverySinkSpy{})
			if result != nil || err == nil {
				t.Fatal("offline transaction reported delivery")
			}
			if scenario == "already-canceled" {
				if !errors.Is(err, context.Canceled) || len(pool.contexts) != 0 {
					t.Fatal("canceled reminder opened a context-free transaction")
				}
			} else {
				if len(pool.contexts) != 1 || pool.contexts[0].Value(reminderStopContextKey{}) != "owned-reminder" {
					t.Fatal("transaction detached from its caller")
				}
				deadline, ok := pool.contexts[0].Deadline()
				if !ok || time.Until(deadline) > 30*time.Second {
					t.Fatal("transaction lost cooperative deadline")
				}
				if scenario == "canceled-at-begin" && !errors.Is(err, context.Canceled) {
					t.Fatal("transaction ignored caller stop")
				}
			}
			if db.Statement.Context.Err() != nil {
				t.Fatal("scoped stop mutated shared repository")
			}
		})
	}
}

type reminderContextQueryPool struct {
	*followUpContextTransactionPool
	queryContext context.Context
	query        string
	arguments    []any
}

func (p *reminderContextQueryPool) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	p.queryContext, p.query, p.arguments = ctx, query, arguments
	return nil, errors.New("offline query boundary")
}

func TestReminderContextGormScopesReadsWithoutMutatingRoot(t *testing.T) {
	pool := &reminderContextQueryPool{followUpContextTransactionPool: &followUpContextTransactionPool{}}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	repo := &GormRepository{DB: db}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), reminderStopContextKey{}, "query"))
	defer cancel()
	scoped, err := repo.withReminderDeliveryContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.withReminderDeliveryContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if scoped == other || scoped.(*GormRepository).DB == db || db.Statement.Context == ctx {
		t.Fatal("scope changed shared storage")
	}
	_, err = scoped.(*GormRepository).FindDueReminderDeliveryAuthorizations("alice", time.Now().UTC(), 3, ReminderDeliveryMaxAttempts)
	ownerPlaceholder := regexp.MustCompile(`authz\.owner_identity = \$(\d+)`).FindStringSubmatch(pool.query)
	if len(ownerPlaceholder) != 2 {
		t.Fatalf("owner predicate absent from query: %q", pool.query)
	}
	index, parseErr := strconv.Atoi(ownerPlaceholder[1])
	if err == nil || pool.queryContext != ctx || parseErr != nil || index < 1 || index > len(pool.arguments) || pool.arguments[index-1] != "alice" {
		t.Fatal("actual due-query transport lost context or owner")
	}
	cancel()
	if _, err := repo.withReminderDeliveryContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled request entered storage scope")
	}
	if other.(*GormRepository).DB.Statement.Context.Err() != nil || db.Statement.Context.Err() != nil {
		t.Fatal("worker stop changed another storage scope")
	}
	if _, err := repo.withReminderDeliveryContext(nil); !errors.Is(err, ErrReminderDeliveryContextUnavailable) {
		t.Fatal("nil context created storage scope")
	}
	for _, unsafe := range []*GormRepository{nil, {DB: db.Session(&gorm.Session{DryRun: true})}, {DB: db.Session(&gorm.Session{DisableNestedTransaction: true})}} {
		if value, err := unsafe.withReminderDeliveryContext(context.Background()); value != nil || !errors.Is(err, ErrReminderDeliveryContextUnavailable) {
			t.Fatal("unsafe root admitted contextual reminders")
		}
	}
}

type reminderStopSchedulerProbe struct {
	*schedulerBoundaryProbe
	reminderContext context.Context
	contextualCalls int
}

func (s *reminderStopSchedulerProbe) RunDueReminderDeliveriesContext(ctx context.Context, request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	s.reminderContext = ctx
	s.contextualCalls++
	return s.schedulerBoundaryProbe.RunDueReminderDeliveries(request)
}

func TestReminderStopSchedulerUsesOwnedContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), reminderStopContextKey{}, "scheduled")
	probe := &reminderStopSchedulerProbe{schedulerBoundaryProbe: &schedulerBoundaryProbe{contextualSchedulerProbe: &contextualSchedulerProbe{}}}
	if err := runWorkflowSweep(ctx, probe, 3); err != nil {
		t.Fatal(err)
	}
	if probe.contextualCalls != 1 || probe.reminderContext != ctx {
		t.Fatal("scheduler invoked context-free reminder delivery")
	}
}

type reminderStopHTTPService struct {
	Service
	ReminderDeliveryService
	contextualCalls, legacyCalls int
	ctx                          context.Context
	owner                        string
	result                       *ReminderDeliveryRunSummary
	err                          error
}

type reminderLegacyHTTPService struct {
	Service
	ReminderDeliveryService
	calls int
}

func (s *reminderLegacyHTTPService) RunDueReminderDeliveriesForOwner(string, RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	s.calls++
	return &ReminderDeliveryRunSummary{}, nil
}

func TestReminderContextHTTPRejectsLegacyAdapterWithoutFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &reminderLegacyHTTPService{}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/workflow/reminder-deliveries/run-due", strings.NewReader("{}"))
	c.Set(identity.ContextSubjectKey, "alice")
	NewHandler(svc).RunDueReminderDeliveries(c)
	if response.Code != http.StatusServiceUnavailable || svc.calls != 0 {
		t.Fatal("HTTP silently fell back to context-free reminders")
	}
}

func (s *reminderStopHTTPService) RunDueReminderDeliveriesForOwner(string, RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	s.legacyCalls++
	return s.result, s.err
}
func (s *reminderStopHTTPService) RunDueReminderDeliveriesForOwnerContext(ctx context.Context, owner string, _ RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	s.contextualCalls++
	s.ctx, s.owner = ctx, owner
	return s.result, s.err
}

func TestReminderStopHTTPUsesRequestContextAndRejectsInvalidOutcome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"valid", "missing-owner", "null", "malformed", "trailing-json", "trailing-garbage", "too-large", "negative-limit", "excessive-limit", "nil-result", "private-error", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			body, owner, status := "{\"limit\":3}", "alice", http.StatusOK
			svc := &reminderStopHTTPService{result: &ReminderDeliveryRunSummary{}}
			switch scenario {
			case "missing-owner":
				owner = ""
				status = http.StatusUnauthorized
			case "null":
				body = "null"
				status = http.StatusBadRequest
			case "malformed":
				body = "{"
				status = http.StatusBadRequest
			case "trailing-json":
				body = "{} {}"
				status = http.StatusBadRequest
			case "trailing-garbage":
				body = "{} not-json"
				status = http.StatusBadRequest
			case "too-large":
				body = "{}" + strings.Repeat(" ", 4096)
				status = http.StatusBadRequest
			case "negative-limit":
				body = "{\"limit\":-1}"
				status = http.StatusBadRequest
			case "excessive-limit":
				body = "{\"limit\":101}"
				status = http.StatusBadRequest
			case "nil-result":
				svc.result = nil
				status = http.StatusInternalServerError
			case "private-error":
				svc.err = errors.New("token=must-not-leak")
				status = http.StatusInternalServerError
			case "unavailable":
				svc.err = errors.Join(ErrReminderDeliveryContextUnavailable, errors.New("token=must-not-leak"))
				status = http.StatusServiceUnavailable
			}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodPost, "/workflow/run-due-reminder-deliveries", strings.NewReader(body)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			if owner != "" {
				c.Set(identity.ContextSubjectKey, owner)
			}
			NewHandler(svc).RunDueReminderDeliveries(c)
			if response.Code != status || svc.legacyCalls != 0 || strings.Contains(response.Body.String(), "must-not-leak") {
				t.Fatalf("unsafe response: status=%d legacy=%d", response.Code, svc.legacyCalls)
			}
			if scenario == "valid" && (svc.contextualCalls != 1 || svc.ctx != ctx || svc.owner != owner) {
				t.Fatal("HTTP reminder discarded owner/context")
			}
			if (status == http.StatusUnauthorized || status == http.StatusBadRequest) && svc.contextualCalls != 0 {
				t.Fatal("invalid HTTP input invoked service")
			}
		})
	}
}
