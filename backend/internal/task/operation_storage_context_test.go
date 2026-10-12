package task

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type taskStorageContextKey struct{}

type taskStorageContextFactory interface {
	WithTaskStateContext(context.Context) (TaskStateRepository, error)
}

type taskStorageTransport struct {
	contexts []context.Context
	block    bool
}

func (p *taskStorageTransport) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("offline prepare refused")
}
func (p *taskStorageTransport) ExecContext(ctx context.Context, _ string, _ ...any) (sql.Result, error) {
	p.contexts = append(p.contexts, ctx)
	return nil, errors.New("offline SQL refused")
}
func (p *taskStorageTransport) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("offline query refused")
}
func (p *taskStorageTransport) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return &sql.Row{}
}
func (p *taskStorageTransport) BeginTx(ctx context.Context, _ *sql.TxOptions) (gorm.ConnPool, error) {
	p.contexts = append(p.contexts, ctx)
	if p.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, errors.New("offline transaction refused")
}

func TestTaskStateEnteredTransactionReceivesDeadline(t *testing.T) {
	pool := &taskStorageTransport{block: true}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	repo, err := NewPostgresTaskStateRepository(db).WithTaskStateContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.ClaimTaskOperation("alice", "deadline-transport", strings.Repeat("a", 64), "plan", "worker", time.Now(), time.Minute)
	if !errors.Is(err, context.DeadlineExceeded) || len(pool.contexts) != 1 || db.Statement.Context.Err() != nil {
		t.Fatalf("entered transaction ignored deadline or cancelled shared root: %v", err)
	}
}

type borrowedTaskStorageTransport struct{ taskStorageTransport }

func (*borrowedTaskStorageTransport) Commit() error   { return nil }
func (*borrowedTaskStorageTransport) Rollback() error { return nil }

func TestTaskStateContextRefusesUnownedStorageRoots(t *testing.T) {
	for _, kind := range []string{"nil", "dry_run", "borrowed_transaction", "nil_context"} {
		t.Run(kind, func(t *testing.T) {
			var pool gorm.ConnPool = &taskStorageTransport{}
			if kind == "borrowed_transaction" {
				pool = &borrowedTaskStorageTransport{}
			}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, DryRun: kind == "dry_run", Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "nil" {
				db = nil
			}
			var ctx context.Context = context.Background()
			if kind == "nil_context" {
				ctx = nil
			}
			if _, err := NewPostgresTaskStateRepository(db).WithTaskStateContext(ctx); !errors.Is(err, ErrTaskStorageContextUnavailable) {
				t.Fatalf("unowned root accepted: %v", err)
			}
		})
	}
}

func TestTaskStorageUnavailableHTTPDoesNotExposePrivateFailure(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	writeTaskOperationError(c, errors.Join(ErrTaskStorageContextUnavailable, errors.New("private storage details")), "failed")
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private storage details") {
		t.Fatalf("missing owned storage not safely represented: %d %s", response.Code, response.Body.String())
	}
}

func TestTaskStatePostgresContextReachesTransactionWithoutMutatingRoot(t *testing.T) {
	pool := &taskStorageTransport{}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	root := NewPostgresTaskStateRepository(db)
	factory, ok := any(root).(taskStorageContextFactory)
	if !ok {
		t.Fatal("task-state storage lacks owned context capability")
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), taskStorageContextKey{}, "alice-operation"), time.Second)
	defer cancel()
	repo, err := factory.WithTaskStateContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.ClaimTaskOperation("alice", "ctx-test", strings.Repeat("a", 64), "run", "worker", time.Now(), time.Minute)
	if err == nil || len(pool.contexts) != 1 || pool.contexts[0].Value(taskStorageContextKey{}) != "alice-operation" {
		t.Fatal("SQL transaction dropped owned context")
	}
	if db.Statement.Context.Value(taskStorageContextKey{}) != nil || db.Statement.Context.Err() != nil {
		t.Fatal("task-state context changed shared database root")
	}
	cancel()
	if _, err := factory.WithTaskStateContext(ctx); !errors.Is(err, context.Canceled) || len(pool.contexts) != 1 {
		t.Fatalf("cancelled scope entered SQL: %v", err)
	}
}

type taskStorageScopeProbe struct {
	*MemoryTaskStateRepository
	contexts []context.Context
}

func (r *taskStorageScopeProbe) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	r.contexts = append(r.contexts, ctx)
	return r, ctx.Err()
}

func TestTaskOperationAdmissionOwnsBoundedStorageContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), taskStorageContextKey{}, "request-lifetime")
	repo := &taskStorageScopeProbe{MemoryTaskStateRepository: NewMemoryTaskStateRepository()}
	svc := &service{stateRepository: repo}
	_, _ = svc.withTaskOperation(IntakeRequest{ExecutionContext: ctx, OwnerIdentity: "alice", Request: "Read notes", IdempotencyKey: "bounded-admission"}, "plan", func(IntakeRequest) (*CompletionPlan, error) {
		return nil, errors.New("controlled execution refusal")
	})
	if len(repo.contexts) == 0 || repo.contexts[0].Value(taskStorageContextKey{}) != "request-lifetime" {
		t.Fatal("task operation did not scope storage to owned request")
	}
	if _, ok := repo.contexts[0].Deadline(); !ok {
		t.Fatal("task storage admission has no deadline")
	}
}

type legacyTaskStorage struct{ TaskStateRepository }

func TestOwnedTaskOperationRefusesContextFreeStorageBeforeClaim(t *testing.T) {
	svc := &service{stateRepository: &legacyTaskStorage{NewMemoryTaskStateRepository()}}
	executed := false
	_, err := svc.withTaskOperation(IntakeRequest{ExecutionContext: context.Background(), OwnerIdentity: "alice", Request: "Read notes"}, "plan", func(IntakeRequest) (*CompletionPlan, error) {
		executed = true
		return nil, nil
	})
	if !errors.Is(err, ErrTaskStorageContextUnavailable) || executed {
		t.Fatal("owned operation fell back to context-free storage")
	}
}

type blockingTaskHeartbeatStorage struct {
	TaskStateRepository
	ctx     context.Context
	entered chan context.Context
}

func (r *blockingTaskHeartbeatStorage) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	return &blockingTaskHeartbeatStorage{TaskStateRepository: r.TaskStateRepository, ctx: ctx, entered: r.entered}, ctx.Err()
}

func (r *blockingTaskHeartbeatStorage) HeartbeatTaskOperation(string, uuid.UUID, string, int64, time.Time) (bool, error) {
	r.entered <- r.ctx
	<-r.ctx.Done()
	return false, r.ctx.Err()
}

func TestTaskHeartbeatStopCancelsEnteredStorageAndJoinsWorker(t *testing.T) {
	memory := NewMemoryTaskStateRepository()
	claim, err := memory.ClaimTaskOperation("alice", "heartbeat-context", strings.Repeat("a", 64), "run", "worker", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	repo := &blockingTaskHeartbeatStorage{TaskStateRepository: memory, entered: make(chan context.Context, 1)}
	svc := &service{stateRepository: repo}
	stop := svc.startTaskOperationHeartbeatWithInterval(claim, "worker", time.Millisecond)
	defer stop()
	select {
	case ctx := <-repo.entered:
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("heartbeat storage has no timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not enter storage")
	}
	stopped := make(chan bool, 1)
	go func() { stopped <- stop() }()
	select {
	case lost := <-stopped:
		if !lost {
			t.Fatal("interrupted heartbeat write was considered confirmed")
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat stop did not cancel entered write and join")
	}
}
