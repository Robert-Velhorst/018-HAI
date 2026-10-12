package frameworkregistry

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type legacyConstitutionStore struct{ Repository }

func TestExecutionConstitutionRefusesContextFreeRepository(t *testing.T) {
	service, err := NewService(legacyConstitutionStore{NewMemoryRepository()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ActiveConstitutionContext(context.Background(), "robert"); err == nil {
		t.Fatal("context-free repository supplied execution policy")
	}
	if _, _, err := service.ActiveConstitution("robert"); err != nil {
		t.Fatalf("ordinary non-execution policy read changed: %v", err)
	}
}

func TestMemoryConstitutionLookupCancelsDuringLockWait(t *testing.T) {
	repo := NewMemoryRepository()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := repo.ListConstitutionsContext(ctx, "robert")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lookup error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lookup ignored cancellation while policy mutex was held")
	}
}

// Exercise database/sql connection admission, not PostgreSQL locks or queries.
type constitutionPoolConnector struct{}
type constitutionPoolDriver struct{}
type constitutionPoolConnection struct{}

func (constitutionPoolConnector) Connect(context.Context) (driver.Conn, error) {
	return constitutionPoolConnection{}, nil
}
func (constitutionPoolConnector) Driver() driver.Driver { return constitutionPoolDriver{} }
func (constitutionPoolDriver) Open(string) (driver.Conn, error) {
	return constitutionPoolConnection{}, nil
}
func (constitutionPoolConnection) Close() error { return nil }
func (constitutionPoolConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected query after connection admission")
}
func (constitutionPoolConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}

func TestGormConstitutionLookupCancelsOnSaturatedConnectionPool(t *testing.T) {
	sqlDB := sql.OpenDB(constitutionPoolConnector{})
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	held, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewGormRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.EvaluateConstitutionExecutionPolicyContext(ctx, ConstitutionExecutionPolicyRequest{
			OwnerIdentity: "robert", RequiredAuthority: 1,
			RequestedCapabilities: []string{ConstitutionCapabilityLocalExecution},
		})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("saturated pool lookup = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("execution policy lookup ignored context on saturated connection pool")
	}
}
