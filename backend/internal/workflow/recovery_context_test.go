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
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type recoveryContextHTTPProbe struct {
	Service
	ctx                context.Context
	owner              string
	legacy, contextual int
	result             *ClaimRecoverySummary
	err                error
}

func (s *recoveryContextHTTPProbe) RecoverStaleClaimsForOwner(string, RunDueRequest) (*ClaimRecoverySummary, error) {
	s.legacy++
	return s.result, s.err
}
func (s *recoveryContextHTTPProbe) RecoverStaleClaimsForOwnerContext(ctx context.Context, owner string, _ RunDueRequest) (*ClaimRecoverySummary, error) {
	s.contextual++
	s.ctx, s.owner = ctx, owner
	return s.result, s.err
}

func TestRecoveryContextHTTPUsesOwnerContextAndRejectsInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"valid", "missing-owner", "null", "malformed", "trailing-json", "oversized", "negative-limit", "excessive-limit", "nil-result", "private-error"} {
		t.Run(scenario, func(t *testing.T) {
			body, owner, status := "{\"limit\":3}", "alice", http.StatusOK
			svc := &recoveryContextHTTPProbe{result: &ClaimRecoverySummary{}}
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
			case "oversized":
				body = `{"padding":"` + strings.Repeat("x", 5000) + `"}`
				status = http.StatusBadRequest
			case "negative-limit":
				body = `{"limit":-1}`
				status = http.StatusBadRequest
			case "excessive-limit":
				body = `{"limit":51}`
				status = http.StatusBadRequest
			case "nil-result":
				svc.result = nil
				status = http.StatusInternalServerError
			case "private-error":
				svc.err = errors.New("token=must-not-leak")
				status = http.StatusInternalServerError
			}
			ctx := context.WithValue(context.Background(), reminderStopContextKey{}, "owned-recovery")
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/workflow/recover-stale", strings.NewReader(body)).WithContext(ctx)
			if owner != "" {
				c.Set(identity.ContextSubjectKey, owner)
			}
			NewHandler(svc).RecoverStaleClaims(c)
			if response.Code != status || svc.legacy != 0 || strings.Contains(response.Body.String(), "must-not-leak") {
				t.Fatalf("unsafe recovery response: status=%d legacy=%d", response.Code, svc.legacy)
			}
			if scenario == "valid" && (svc.ctx != ctx || svc.owner != owner || svc.contextual != 1) {
				t.Fatal("HTTP discarded owned recovery context")
			}
			if (status == http.StatusUnauthorized || status == http.StatusBadRequest) && svc.contextual != 0 {
				t.Fatal("invalid recovery request entered service")
			}
		})
	}
}

type recoveryContextSchedulerProbe struct {
	*schedulerBoundaryProbe
	ctx          context.Context
	callsContext int
}

func (s *recoveryContextSchedulerProbe) RecoverStaleClaimsContext(ctx context.Context, request RunDueRequest) (*ClaimRecoverySummary, error) {
	s.ctx = ctx
	s.callsContext++
	return s.schedulerBoundaryProbe.RecoverStaleClaims(request)
}

func TestRecoveryContextSchedulerUsesOwnedContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), reminderStopContextKey{}, "recovery-sweep")
	svc := &recoveryContextSchedulerProbe{schedulerBoundaryProbe: &schedulerBoundaryProbe{contextualSchedulerProbe: &contextualSchedulerProbe{}}}
	if err := runWorkflowSweep(ctx, svc, 3); err != nil || svc.callsContext != 1 || svc.ctx != ctx {
		t.Fatalf("scheduler discarded recovery context: err=%v calls=%d", err, svc.callsContext)
	}
}

type recoveryAtomicTestRepository interface {
	RecoverExpiredWorkflowClaimAtomic(models.WorkflowItem, time.Time) (*models.WorkflowItem, bool, error)
	RecoverExpiredOpenLoopClaimAtomic(string, models.WorkflowOpenLoop, time.Time) (*models.WorkflowOpenLoop, bool, error)
}

func TestRecoveryContextAtomicTransactionInheritsCaller(t *testing.T) {
	for _, kind := range []string{"workflow", "open-loop"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), reminderStopContextKey{}, "recovery-tx"))
			defer cancel()
			pool := &followUpContextTransactionPool{err: errors.New("offline transaction refusal")}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			repo, ok := any(&GormRepository{DB: db.WithContext(ctx)}).(recoveryAtomicTestRepository)
			if !ok {
				t.Fatal("atomic recovery capability missing")
			}
			if kind == "workflow" {
				_, _, err = repo.RecoverExpiredWorkflowClaimAtomic(models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "alice"}, time.Now().UTC())
			} else {
				_, _, err = repo.RecoverExpiredOpenLoopClaimAtomic("alice", models.WorkflowOpenLoop{ID: uuid.New(), WorkflowID: uuid.New()}, time.Now().UTC())
			}
			if err == nil || len(pool.contexts) != 1 || pool.contexts[0].Value(reminderStopContextKey{}) != "recovery-tx" {
				t.Fatal("recovery transaction lost its context")
			}
			if db.Statement.Context.Err() != nil {
				t.Fatal("recovery mutated root context")
			}
		})
	}
}
