package resilience

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/pgtestguard"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresSchemaContractDocumentsMigration0023(t *testing.T) {
	required := []string{
		"migration=0023",
		"resilience_idempotency_records",
		"resilience_leases",
		"resilience_worker_heartbeats",
		"resilience_circuits",
		"resilience_retry_records",
		"resilience_recovery_records",
		"resilience_event_records",
		"scope=owner_id+workspace_id",
		"payload=jsonb-object<=1MiB",
		"counters=numeric(20,0)",
		"primary-keys=idempotency(owner_id,workspace_id,idempotency_key)",
		"resilience_leases_scope_work_idx",
		"resilience_worker_heartbeats_scope_worker_idx",
		"resilience_circuits_scope_circuit_idx",
		"resilience_retries_scope_work_sequence_idx",
		"resilience_retries_scope_requested_idx",
		"resilience_recoveries_scope_work_sequence_idx",
		"resilience_recoveries_scope_requested_idx",
		"resilience_events_scope_sequence_idx",
		"event-hash-unique-per-scope",
		"append-only=retry+recovery+event",
		"authority=advisory-only",
	}
	for _, value := range required {
		if !strings.Contains(postgresSchemaContract, value) {
			t.Fatalf("postgres schema contract is missing %q", value)
		}
	}
	for _, forbidden := range []string{"dispatch=true", "execute=true", "grant-authority", "consume-approval"} {
		if strings.Contains(postgresSchemaContract, forbidden) {
			t.Fatalf("postgres schema contract contains forbidden authority %q", forbidden)
		}
	}
}

func TestPostgresRepositoryNilFailsClosed(t *testing.T) {
	repository := NewPostgresRepository(nil)
	if _, err := repository.GetLease(context.Background(), testScope, "work-1"); err == nil {
		t.Fatal("nil database must fail closed")
	}
	var nilRepository *PostgresRepository
	if _, err := nilRepository.ListEvents(context.Background(), testScope, 10); err == nil {
		t.Fatal("nil repository must fail closed")
	}
	if _, err := repository.GetLease(nil, testScope, "work-1"); err == nil {
		t.Fatal("nil context must fail closed")
	}
}

func TestStrictPostgresDecodeRejectsUnknownCorruptAndCrossScopePayloads(t *testing.T) {
	record := IdempotencyRecord{
		ContractVersion: ContractVersion,
		Scope:           testScope,
		WorkID:          "work-1",
		IdempotencyKey:  strings.Repeat("a", 64),
		PayloadHash:     testPayload,
		RecordedAt:      testNow,
	}
	payload, err := marshalPostgresRecord("idempotency record", record)
	if err != nil {
		t.Fatal(err)
	}
	row := postgresIdempotencyRow{
		OwnerID: record.Scope.OwnerID, WorkspaceID: record.Scope.WorkspaceID,
		IdempotencyKey: record.IdempotencyKey, WorkID: record.WorkID,
		PayloadHash: record.PayloadHash, ContractVersion: record.ContractVersion,
		RecordedAt: record.RecordedAt, Payload: string(payload),
	}
	if _, err := decodePostgresIdempotency(row, testScope); err != nil {
		t.Fatalf("valid row rejected: %v", err)
	}

	row.Payload = strings.TrimSuffix(string(payload), "}") + `,"unexpected":true}`
	if _, err := decodePostgresIdempotency(row, testScope); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("unknown JSON field error=%v, want ErrStateConflict", err)
	}
	row.Payload = string(payload)
	row.WorkID = "metadata-tampered"
	if _, err := decodePostgresIdempotency(row, testScope); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("metadata mismatch error=%v, want ErrStateConflict", err)
	}
	row.WorkID = record.WorkID
	if _, err := decodePostgresIdempotency(row, Scope{OwnerID: "other-owner", WorkspaceID: testScope.WorkspaceID}); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("cross-owner decode error=%v, want ErrStateConflict", err)
	}
}

func TestPostgresRepositoryLifecycle(t *testing.T) {
	db := resiliencePostgresTestDB(t)
	repository := NewPostgresRepository(db)
	ctx := context.Background()

	key := strings.Repeat("a", 64)
	idempotency := IdempotencyRecord{
		ContractVersion: ContractVersion, Scope: testScope, WorkID: "work-1",
		IdempotencyKey: key, PayloadHash: testPayload, RecordedAt: testNow,
	}
	stored, created, err := repository.CreateIdempotency(ctx, idempotency)
	if err != nil || !created || stored.WorkID != idempotency.WorkID {
		t.Fatalf("create idempotency: stored=%+v created=%v err=%v", stored, created, err)
	}
	_, created, err = repository.CreateIdempotency(ctx, idempotency)
	if err != nil || created {
		t.Fatalf("duplicate idempotency: created=%v err=%v", created, err)
	}
	conflict := idempotency
	conflict.PayloadHash = strings.Repeat("c", 64)
	if _, _, err := repository.CreateIdempotency(ctx, conflict); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("idempotency conflict=%v", err)
	}
	if _, err := repository.LookupIdempotency(ctx, Scope{OwnerID: "other-owner", WorkspaceID: testScope.WorkspaceID}, key); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("cross-owner lookup=%v", err)
	}

	lease := repositoryTestLease(t, testScope, "worker-1", testNow)
	if err := repository.CompareAndSwapLease(ctx, testScope, lease.WorkID, nil, lease); err != nil {
		t.Fatal(err)
	}
	renewed, err := DecideLeaseHeartbeat(lease, LeaseHeartbeat{
		ContractVersion: ContractVersion, Scope: testScope, WorkID: lease.WorkID,
		WorkerID: lease.WorkerID, Generation: lease.Generation,
		ObservedAt: testNow.Add(10 * time.Second), TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompareAndSwapLease(ctx, testScope, lease.WorkID, &lease, *renewed.Lease); err != nil {
		t.Fatal(err)
	}
	if err := repository.CompareAndSwapLease(ctx, testScope, lease.WorkID, &lease, *renewed.Lease); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale lease update=%v", err)
	}

	heartbeat := WorkerHeartbeat{ContractVersion: ContractVersion, Scope: testScope, WorkerID: "worker-1", Sequence: 1, ObservedAt: testNow}
	if err := repository.CompareAndSwapWorkerHeartbeat(ctx, testScope, heartbeat.WorkerID, nil, heartbeat); err != nil {
		t.Fatal(err)
	}
	nextHeartbeat := heartbeat
	nextHeartbeat.Sequence++
	nextHeartbeat.ObservedAt = nextHeartbeat.ObservedAt.Add(time.Second)
	if err := repository.CompareAndSwapWorkerHeartbeat(ctx, testScope, heartbeat.WorkerID, &heartbeat, nextHeartbeat); err != nil {
		t.Fatal(err)
	}

	circuit, err := NewCircuitState(testScope, "provider-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompareAndSwapCircuit(ctx, testScope, circuit.CircuitID, 0, circuit); err != nil {
		t.Fatal(err)
	}
	circuitDecision, err := AfterCircuitAttempt(testScope, circuit, AttemptFailed, testNow, CircuitPolicy{
		FailureThreshold: 2, OpenDuration: time.Minute, MaxHalfOpenProbes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompareAndSwapCircuit(ctx, testScope, circuit.CircuitID, circuit.Revision, circuitDecision.State); err != nil {
		t.Fatal(err)
	}

	retryDecision, err := DecideRetry(testScope, "work-1", 1, testFailure(FailureTransient), testRetryPolicy(), testNow)
	if err != nil {
		t.Fatal(err)
	}
	retry := RetryRecord{ContractVersion: ContractVersion, Scope: testScope, WorkID: "work-1", Sequence: 1,
		RequestedAt: testNow, Policy: testRetryPolicy(), Decision: retryDecision, Authority: advisoryBoundary()}
	if err := repository.AppendRetry(ctx, 0, retry); err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendRetry(ctx, 0, retry); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale retry append=%v", err)
	}

	recoveryRequest := RecoveryRequest{ContractVersion: ContractVersion, Scope: testScope, WorkID: "work-1",
		Now: testNow, HeartbeatMaxAge: time.Minute, RetryPolicy: testRetryPolicy()}
	recoveryDecision, err := DecideRecovery(recoveryRequest)
	if err != nil {
		t.Fatal(err)
	}
	recovery := RecoveryRecord{ContractVersion: ContractVersion, Scope: testScope, WorkID: "work-1",
		Sequence: 1, RequestedAt: testNow, Request: recoveryRequest, Decision: recoveryDecision, Authority: advisoryBoundary()}
	if err := repository.AppendRecovery(ctx, 0, recovery); err != nil {
		t.Fatal(err)
	}

	event := ControlEvent{ContractVersion: ContractVersion, Scope: testScope, Type: "test.recorded",
		SubjectID: "work-1", OccurredAt: testNow, Sequence: 1}
	eventHash, err := EventHash(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendEvent(ctx, EventRecord{Event: event, Hash: eventHash, Authority: advisoryBoundary()}); err != nil {
		t.Fatal(err)
	}
	badEvent := event
	badEvent.Sequence = 3
	badEvent.PreviousHash = eventHash
	badHash, _ := EventHash(badEvent)
	if err := repository.AppendEvent(ctx, EventRecord{Event: badEvent, Hash: badHash, Authority: advisoryBoundary()}); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("event chain gap=%v", err)
	}

	if retries, err := repository.ListRetries(ctx, testScope, "work-1", 10); err != nil || len(retries) != 1 {
		t.Fatalf("list retries len=%d err=%v", len(retries), err)
	}
	if recoveries, err := repository.ListRecoveries(ctx, testScope, "work-1", 10); err != nil || len(recoveries) != 1 {
		t.Fatalf("list recoveries len=%d err=%v", len(recoveries), err)
	}
	if events, err := repository.ListEvents(ctx, testScope, 10); err != nil || len(events) != 1 {
		t.Fatalf("list events len=%d err=%v", len(events), err)
	}
}

func TestPostgresRepositoryAtomicIdempotency(t *testing.T) {
	db := resiliencePostgresTestDB(t)
	repository := NewPostgresRepository(db)
	record := IdempotencyRecord{ContractVersion: ContractVersion, Scope: testScope, WorkID: "work-atomic",
		IdempotencyKey: strings.Repeat("d", 64), PayloadHash: testPayload, RecordedAt: testNow}
	var created atomic.Int32
	var failures atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, wasCreated, err := repository.CreateIdempotency(context.Background(), record)
			if err != nil {
				failures.Add(1)
				return
			}
			if wasCreated {
				created.Add(1)
			}
		}()
	}
	wait.Wait()
	if failures.Load() != 0 || created.Load() != 1 {
		t.Fatalf("atomic idempotency created=%d failures=%d", created.Load(), failures.Load())
	}
}

func TestPostgresRepositoryConcurrentFencingAndEventChain(t *testing.T) {
	db := resiliencePostgresTestDB(t)
	repository := NewPostgresRepository(db)
	ctx := context.Background()
	lease := repositoryTestLease(t, testScope, "worker-1", testNow)
	if err := repository.CompareAndSwapLease(ctx, testScope, lease.WorkID, nil, lease); err != nil {
		t.Fatal(err)
	}

	candidates := make([]WorkLease, 2)
	for index := range candidates {
		decision, err := DecideLeaseHeartbeat(lease, LeaseHeartbeat{
			ContractVersion: ContractVersion, Scope: testScope, WorkID: lease.WorkID,
			WorkerID: lease.WorkerID, Generation: lease.Generation,
			ObservedAt: testNow.Add(time.Duration(index+1) * time.Second), TTL: time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		candidates[index] = *decision.Lease
	}
	var successes atomic.Int32
	var stale atomic.Int32
	var unexpected atomic.Int32
	var wait sync.WaitGroup
	for index := range candidates {
		candidate := candidates[index]
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := repository.CompareAndSwapLease(context.Background(), testScope, lease.WorkID, &lease, candidate)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrStaleFence):
				stale.Add(1)
			default:
				unexpected.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || stale.Load() != 1 || unexpected.Load() != 0 {
		t.Fatalf("lease CAS successes=%d stale=%d unexpected=%d", successes.Load(), stale.Load(), unexpected.Load())
	}

	first := ControlEvent{ContractVersion: ContractVersion, Scope: testScope, Type: "race.started",
		SubjectID: lease.WorkID, OccurredAt: testNow, Sequence: 1}
	firstHash, err := EventHash(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendEvent(ctx, EventRecord{Event: first, Hash: firstHash, Authority: advisoryBoundary()}); err != nil {
		t.Fatal(err)
	}
	successes.Store(0)
	stale.Store(0)
	unexpected.Store(0)
	for index := 0; index < 2; index++ {
		event := ControlEvent{ContractVersion: ContractVersion, Scope: testScope,
			Type: fmt.Sprintf("race.branch-%d", index), SubjectID: lease.WorkID,
			OccurredAt: testNow.Add(time.Duration(index+1) * time.Second), Sequence: 2, PreviousHash: firstHash}
		hash, hashErr := EventHash(event)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		record := EventRecord{Event: event, Hash: hash, Authority: advisoryBoundary()}
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := repository.AppendEvent(context.Background(), record)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrStaleFence):
				stale.Add(1)
			default:
				unexpected.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || stale.Load() != 1 || unexpected.Load() != 0 {
		t.Fatalf("event append successes=%d stale=%d unexpected=%d", successes.Load(), stale.Load(), unexpected.Load())
	}
}

func TestPostgresRepositoryRejectsCorruptStoredPayload(t *testing.T) {
	db := resiliencePostgresTestDB(t)
	repository := NewPostgresRepository(db)
	lease := repositoryTestLease(t, testScope, "worker-1", testNow)
	if err := repository.CompareAndSwapLease(context.Background(), testScope, lease.WorkID, nil, lease); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE public.resilience_leases
		SET payload = jsonb_set(payload, '{workerId}', '"tampered-worker"'::jsonb)
		WHERE owner_id = ? AND workspace_id = ? AND work_id = ?`,
		testScope.OwnerID, testScope.WorkspaceID, lease.WorkID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetLease(context.Background(), testScope, lease.WorkID); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("corrupt lease error=%v, want ErrStateConflict", err)
	}
}

func resiliencePostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_RESILIENCE_TEST_DATABASE_DSN", "hai_resilience_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	var database string
	if err := db.Raw(`SELECT current_database()`).Scan(&database).Error; err != nil {
		t.Fatal(err)
	}
	if database != "hai_resilience_test" {
		t.Fatalf("refusing destructive schema setup against unexpected database identity %q", database)
	}
	if err := db.Exec(resiliencePostgresDropSQL).Error; err != nil {
		t.Fatalf("drop old resilience test tables: %v", err)
	}
	if err := db.Exec(resiliencePostgresSchemaSQL).Error; err != nil {
		t.Fatalf("create resilience test schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec(resiliencePostgresDropSQL).Error })
	return db
}

const resiliencePostgresDropSQL = `
DROP TABLE IF EXISTS public.resilience_event_records;
DROP TABLE IF EXISTS public.resilience_recovery_records;
DROP TABLE IF EXISTS public.resilience_retry_records;
DROP TABLE IF EXISTS public.resilience_circuits;
DROP TABLE IF EXISTS public.resilience_worker_heartbeats;
DROP TABLE IF EXISTS public.resilience_leases;
DROP TABLE IF EXISTS public.resilience_idempotency_records;`

const resiliencePostgresSchemaSQL = `
CREATE TABLE public.resilience_idempotency_records (
 owner_id text NOT NULL, workspace_id text NOT NULL, idempotency_key char(64) NOT NULL,
 work_id text NOT NULL, payload_hash char(64) NOT NULL, contract_version integer NOT NULL CHECK (contract_version = 1),
 recorded_at timestamptz NOT NULL, payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
 PRIMARY KEY (owner_id, workspace_id, idempotency_key));
CREATE TABLE public.resilience_leases (
 owner_id text NOT NULL, workspace_id text NOT NULL, work_id text NOT NULL, idempotency_key char(64) NOT NULL,
 payload_hash char(64) NOT NULL, worker_id text NOT NULL, generation numeric(20,0) NOT NULL CHECK (generation >= 1),
 lease_state text NOT NULL, acquired_at timestamptz NOT NULL, last_heartbeat_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL, released_at timestamptz, contract_version integer NOT NULL CHECK (contract_version = 1),
 payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'), PRIMARY KEY (owner_id, workspace_id, work_id));
CREATE TABLE public.resilience_worker_heartbeats (
 owner_id text NOT NULL, workspace_id text NOT NULL, worker_id text NOT NULL,
 sequence numeric(20,0) NOT NULL CHECK (sequence >= 1), observed_at timestamptz NOT NULL,
 contract_version integer NOT NULL CHECK (contract_version = 1), payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
 PRIMARY KEY (owner_id, workspace_id, worker_id));
CREATE TABLE public.resilience_circuits (
 owner_id text NOT NULL, workspace_id text NOT NULL, circuit_id text NOT NULL,
 revision numeric(20,0) NOT NULL CHECK (revision >= 1), circuit_phase text NOT NULL, updated_at timestamptz NOT NULL,
 contract_version integer NOT NULL CHECK (contract_version = 1), payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
 PRIMARY KEY (owner_id, workspace_id, circuit_id));
CREATE TABLE public.resilience_retry_records (
 owner_id text NOT NULL, workspace_id text NOT NULL, work_id text NOT NULL,
 sequence numeric(20,0) NOT NULL CHECK (sequence >= 1), requested_at timestamptz NOT NULL,
 contract_version integer NOT NULL CHECK (contract_version = 1), payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
 PRIMARY KEY (owner_id, workspace_id, work_id, sequence));
CREATE TABLE public.resilience_recovery_records (
 owner_id text NOT NULL, workspace_id text NOT NULL, work_id text NOT NULL,
 sequence numeric(20,0) NOT NULL CHECK (sequence >= 1), requested_at timestamptz NOT NULL,
 contract_version integer NOT NULL CHECK (contract_version = 1), payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
 PRIMARY KEY (owner_id, workspace_id, work_id, sequence));
CREATE TABLE public.resilience_event_records (
 owner_id text NOT NULL, workspace_id text NOT NULL, sequence numeric(20,0) NOT NULL CHECK (sequence >= 1),
 event_hash char(64) NOT NULL, previous_hash varchar(64) NOT NULL, event_type text NOT NULL, subject_id text NOT NULL,
 occurred_at timestamptz NOT NULL, contract_version integer NOT NULL CHECK (contract_version = 1),
 payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'), PRIMARY KEY (owner_id, workspace_id, sequence),
 UNIQUE (owner_id, workspace_id, event_hash));
CREATE INDEX resilience_retries_scope_work_sequence_idx ON public.resilience_retry_records (owner_id, workspace_id, work_id, sequence DESC);
CREATE INDEX resilience_recoveries_scope_work_sequence_idx ON public.resilience_recovery_records (owner_id, workspace_id, work_id, sequence DESC);
CREATE INDEX resilience_events_scope_sequence_idx ON public.resilience_event_records (owner_id, workspace_id, sequence DESC);`

func ExamplePostgresRepository_advisoryBoundary() {
	repository := NewPostgresRepository(nil)
	_, _ = repository.GetCircuit(context.Background(), Scope{OwnerID: "owner", WorkspaceID: "workspace"}, "provider")
	fmt.Println("advisory only: no dispatch, execution, authority grant, or approval consumption")
	// Output: advisory only: no dispatch, execution, authority grant, or approval consumption
}
