package infra

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAutomaticGormPingFailureRequiresCallerCleanup(t *testing.T) {
	failure := errors.New("synthetic ping failure")
	connector := &startupTestConnector{pingError: failure}
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if db == nil || !errors.Is(err, failure) || connector.closed.Load() != 0 {
		t.Fatal("unexpected GORM first-ping ownership behavior")
	}
	connector.pingError = nil
	if err := pool.PingContext(context.Background()); err != nil {
		t.Fatal("failed GORM open unexpectedly closed the caller's pool")
	}
}

func TestIDPPostgresPoolOpenClosesFailedPool(t *testing.T) {
	for _, stage := range []string{"ping", "cleanup", "cancelled", "nil_context", "during_ping", "cancelled_after_ping"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("synthetic ping failure")
			closeFailure := errors.New("synthetic close failure")
			connector := &startupTestConnector{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var openCtx context.Context = ctx
			switch stage {
			case "ping", "cleanup":
				connector.pingError = failure
			case "cancelled":
				cancel()
			case "nil_context":
				openCtx = nil
			case "during_ping":
				connector.waitForPingCancellation = true
			case "cancelled_after_ping":
				connector.beforePingReturn = cancel
				connector.ignorePingCancellation = true
			}
			if stage == "cleanup" {
				connector.closeError = closeFailure
			}
			pool := sql.OpenDB(connector)
			t.Cleanup(func() { _ = pool.Close() })
			if stage == "during_ping" {
				var deadlineCancel context.CancelFunc
				openCtx, deadlineCancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer deadlineCancel()
			}
			result, err := initializeIDPPostgresPool(openCtx, pool)
			if result != nil || err == nil {
				t.Fatal("failed or cancelled open returned a database")
			}
			if (stage == "ping" || stage == "cleanup") && !errors.Is(err, failure) {
				t.Fatal("ping failure lost")
			}
			if stage == "cleanup" && !errors.Is(err, closeFailure) {
				t.Fatal("cleanup failure lost")
			}
			if stage == "during_ping" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("caller deadline lost")
			}
			if (stage == "cancelled" || stage == "cancelled_after_ping") && !errors.Is(err, context.Canceled) {
				t.Fatal("caller cancellation lost")
			}
			connector.pingError = nil
			connector.waitForPingCancellation = false
			connector.beforePingReturn = nil
			if err := pool.PingContext(context.Background()); err == nil {
				t.Fatal("failed open retained a usable pool")
			}
			if stage != "cancelled" && stage != "nil_context" && connector.closed.Load() != 1 {
				t.Fatal("connected pool was not closed exactly once")
			}
		})
	}
}

func TestIDPPostgresPoolOpenPreservesSuccessfulPoolAndLimits(t *testing.T) {
	connector := &startupTestConnector{}
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := initializeIDPPostgresPool(context.Background(), pool)
	if err != nil || db == nil || connector.closed.Load() != 0 || pool.Stats().MaxOpenConnections != 8 {
		t.Fatal("successful open lost ownership or connection cap")
	}
	deadline, ok := connector.connectContext.Deadline()
	if !ok || time.Until(deadline) > 10*time.Second {
		t.Fatal("initial connection lacks its opening deadline")
	}
	if db.Statement.Context.Err() != nil {
		t.Fatal("returned database retained a cancelled opening context")
	}
	owned, err := db.DB()
	if err != nil || owned != pool {
		t.Fatal("returned database does not own the opened pool")
	}
	if err := pool.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestIDPConnectorBoundsAcquisitionAndPreservesEarlierDeadline(t *testing.T) {
	connector := &startupTestConnector{}
	bounded := idpBoundedConnector{Connector: connector}
	connection, err := bounded.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	deadline, ok := connector.connectContext.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) < 4*time.Second {
		t.Fatal("connection acquisition lacks five-second bound")
	}
	connector.waitForConnectCancellation = true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	connection, err = bounded.Connect(ctx)
	if connection != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("connector did not preserve earlier caller deadline")
	}
}

func TestIDPPostgresConstructorRejectsMissingOrCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, candidate := range []context.Context{nil, ctx} {
		db, err := NewPostgresDatabaseContext(candidate, "synthetic", "synthetic-password", "synthetic", "127.0.0.1", 5432)
		if db != nil || err == nil {
			t.Fatal("invalid context proceeded to connect")
		}
	}
}

func TestIDPPostgresPoolEnforcesOpenAndIdleConnectionCaps(t *testing.T) {
	connector := &startupTestConnector{}
	pool := sql.OpenDB(connector)
	t.Cleanup(func() { _ = pool.Close() })
	if _, err := initializeIDPPostgresPool(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	var connections []*sql.Conn
	for i := 0; i < 8; i++ {
		connection, err := pool.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = connection.Close() })
		connections = append(connections, connection)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	connection, err := pool.Conn(ctx)
	if connection != nil || !errors.Is(err, context.DeadlineExceeded) || pool.Stats().OpenConnections != 8 {
		t.Fatal("ninth connection bypassed capacity or caller cancellation")
	}
	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if pool.Stats().Idle != 2 || connector.closed.Load() != 6 {
		t.Fatal("returned connections exceeded the two-connection idle cap")
	}
}
