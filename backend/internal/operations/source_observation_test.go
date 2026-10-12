package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func sourceObservationTestStart() SourceObservationStart {
	in := sourceIdentityContractInput()
	return SourceObservationStart{
		OwnerUserID: in.OwnerUserID, WorkspaceID: in.WorkspaceID,
		OriginID:     uuid.MustParse("00000000-0000-4000-8000-000000000901"),
		ConfigDigest: strings.Repeat("a", 64),
	}
}

func sourceObservationTestInput() NewOperationInput {
	in := sourceIdentityContractInput()
	origin := sourceObservationTestStart().OriginID
	in.AccountFeedID = &origin
	return in
}

func sourceObservationTestService(repo *MemoryRepository) *Service {
	svc := NewService(repo)
	svc.now = func() time.Time { return sourceIdentityContractTime }
	return svc
}

func sourceObservationTestIngest(t *testing.T, svc *Service, start SourceObservationStart, in NewOperationInput) (models.Operation, SourceObservation) {
	t.Helper()
	var observed SourceObservation
	var result IngestResult
	err := svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
		var ok bool
		observed, ok = CurrentSourceObservation(ctx)
		if !ok {
			return errors.New("callback has no active source observation")
		}
		var err error
		result, err = svc.IngestContext(ctx, in)
		return err
	})
	if err != nil || !result.Created || result.Operation.ID == uuid.Nil {
		t.Fatalf("observed intake: created=%t operation=%s error=%v", result.Created, result.Operation.ID, err)
	}
	return result.Operation, observed
}

func assertSourceObservationMetadata(t *testing.T, op models.Operation, observed SourceObservation) {
	t.Helper()
	if op.SourceObservationID == nil || *op.SourceObservationID != observed.ID || op.SourceObservationGeneration != observed.Generation {
		t.Fatalf("operation lost server observation: id=%v generation=%d want=%s/%d", op.SourceObservationID, op.SourceObservationGeneration, observed.ID, observed.Generation)
	}
}

func TestMemorySourceObservationGenerationsAreTenantScopedAcrossOrigins(t *testing.T) {
	repo := NewMemoryRepository()
	start := sourceObservationTestStart()
	seen := map[uuid.UUID]bool{}
	for _, tc := range []struct {
		name       string
		start      SourceObservationStart
		generation int64
	}{
		{"first", start, 1},
		{"same_origin", start, 2},
		{"other_origin", SourceObservationStart{OwnerUserID: start.OwnerUserID, WorkspaceID: start.WorkspaceID, OriginID: uuid.MustParse("00000000-0000-4000-8000-000000000902"), ConfigDigest: strings.Repeat("b", 64)}, 3},
		{"other_owner", SourceObservationStart{OwnerUserID: "other-owner", WorkspaceID: start.WorkspaceID, OriginID: start.OriginID, ConfigDigest: start.ConfigDigest}, 1},
		{"other_workspace", SourceObservationStart{OwnerUserID: start.OwnerUserID, WorkspaceID: "other-workspace", OriginID: start.OriginID, ConfigDigest: start.ConfigDigest}, 1},
		{"original_tenant", start, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.BeginSourceObservation(context.Background(), tc.start)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID == uuid.Nil || seen[got.ID] || got.Generation != tc.generation || got.StartedAt.IsZero() ||
				got.OwnerUserID != tc.start.OwnerUserID || got.WorkspaceID != tc.start.WorkspaceID || got.OriginID != tc.start.OriginID || got.ConfigDigest != tc.start.ConfigDigest {
				t.Fatalf("wrong durable observation allocation: %+v want generation=%d start=%+v", got, tc.generation, tc.start)
			}
			seen[got.ID] = true
			if stored, ok := repo.observations[got.ID]; !ok || stored != got || repo.observationClocks[sourceObservationScope{got.OwnerUserID, got.WorkspaceID}] != got.Generation {
				t.Fatal("returned observation was not stored with its tenant clock")
			}
		})
	}
	if len(repo.ops) != 0 || len(repo.events) != 0 || len(repo.claims) != 0 {
		t.Fatal("allocating observations wrote operations, audit events, or claims")
	}
}

type sourceObservationTestCallResult struct {
	observation SourceObservation
	err         error
	panicValue  any
}

func sourceObservationTestBoundedCall(t *testing.T, repo *MemoryRepository, held bool, call func() (SourceObservation, error)) sourceObservationTestCallResult {
	t.Helper()
	if held {
		repo.mu.Lock()
		defer func() {
			if held {
				repo.mu.Unlock()
			}
		}()
	}
	done := make(chan sourceObservationTestCallResult, 1)
	go func() {
		result := sourceObservationTestCallResult{}
		defer func() {
			result.panicValue = recover()
			done <- result
		}()
		result.observation, result.err = call()
	}()
	select {
	case result := <-done:
		return result
	case <-time.After(2 * time.Second):
		t.Error("source observation call did not return before the held mutex was released")
		if held {
			repo.mu.Unlock()
			held = false
		}
		select {
		case result := <-done:
			return result
		case <-time.After(2 * time.Second):
			t.Fatal("source observation call did not finish after cleanup")
			return sourceObservationTestCallResult{}
		}
	}
}

func TestSourceObservationRefusesNilCanceledAndLockedDeadlineContexts(t *testing.T) {
	for _, path := range []string{"repository", "service"} {
		for _, mode := range []string{"nil", "canceled", "locked_deadline"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				repo := NewMemoryRepository()
				svc := sourceObservationTestService(repo)
				start := sourceObservationTestStart()
				var ctx context.Context
				var cancel context.CancelFunc
				var want error
				switch mode {
				case "nil":
					want = ErrInvalidSourceObservation
				case "canceled":
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
					want = context.Canceled
				case "locked_deadline":
					ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
					want = context.DeadlineExceeded
				}
				if cancel != nil {
					defer cancel()
				}
				called := false
				result := sourceObservationTestBoundedCall(t, repo, true, func() (SourceObservation, error) {
					if path == "repository" {
						return repo.BeginSourceObservation(ctx, start)
					}
					err := svc.WithSourceObservation(ctx, start, func(context.Context) error {
						called = true
						return nil
					})
					return SourceObservation{}, err
				})
				if result.panicValue != nil || result.err == nil || (want != nil && !errors.Is(result.err, want)) || called || result.observation.ID != uuid.Nil || result.observation.Generation != 0 {
					t.Fatalf("invalid context accepted/panicked: result=%+v callback=%t want=%v", result, called, want)
				}
				if len(repo.observations) != 0 || len(repo.observationClocks) != 0 {
					t.Fatal("refused context wrote a ticket or tenant clock")
				}
				got, err := repo.BeginSourceObservation(context.Background(), start)
				if err != nil || got.Generation != 1 {
					t.Fatalf("refused call consumed an observation generation: %+v / %v", got, err)
				}
			})
		}
	}
}

func TestSourceObservationRejectsInvalidStartWithoutAllocating(t *testing.T) {
	for _, path := range []string{"repository", "service"} {
		for _, field := range []string{"owner", "padded_owner", "invalid_utf8_owner", "workspace", "padded_workspace", "origin", "config", "uppercase_config", "short_config", "nonhex_config"} {
			t.Run(path+"/"+field, func(t *testing.T) {
				repo := NewMemoryRepository()
				valid := sourceObservationTestStart()
				bad := valid
				switch field {
				case "owner":
					bad.OwnerUserID = " "
				case "padded_owner":
					bad.OwnerUserID = " " + valid.OwnerUserID
				case "invalid_utf8_owner":
					bad.OwnerUserID = "owner\xff"
				case "workspace":
					bad.WorkspaceID = " "
				case "padded_workspace":
					bad.WorkspaceID = valid.WorkspaceID + " "
				case "origin":
					bad.OriginID = uuid.Nil
				case "config":
					bad.ConfigDigest = ""
				case "uppercase_config":
					bad.ConfigDigest = strings.Repeat("A", 64)
				case "short_config":
					bad.ConfigDigest = strings.Repeat("a", 63)
				case "nonhex_config":
					bad.ConfigDigest = strings.Repeat("x", 64)
				}
				called := false
				var err error
				if path == "repository" {
					_, err = repo.BeginSourceObservation(context.Background(), bad)
				} else {
					err = NewService(repo).WithSourceObservation(context.Background(), bad, func(context.Context) error {
						called = true
						return nil
					})
				}
				if !errors.Is(err, ErrInvalidSourceObservation) || called {
					t.Fatalf("invalid %s accepted: error=%v callback=%t", field, err, called)
				}
				if len(repo.observations) != 0 || len(repo.observationClocks) != 0 {
					t.Fatal("invalid start wrote a ticket or tenant clock")
				}
				got, err := repo.BeginSourceObservation(context.Background(), valid)
				if err != nil || got.Generation != 1 {
					t.Fatalf("invalid start advanced tenant generation: %+v / %v", got, err)
				}
			})
		}
	}
}

type sourceObservationUnsupportedRepository struct{ Repository }

func TestSourceObservationUnsupportedRepositoryNeverCallsCallback(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(sourceObservationUnsupportedRepository{Repository: repo})
	before := sourceIdentitySnapshot(repo)
	called := false
	err := svc.WithSourceObservation(context.Background(), sourceObservationTestStart(), func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrSourceObservationUnsupported) || called {
		t.Fatalf("unsupported repository ran callback: error=%v callback=%t", err, called)
	}
	sourceIdentityAssertUnchanged(t, repo, before)
	got, err := repo.BeginSourceObservation(context.Background(), sourceObservationTestStart())
	if err != nil || got.Generation != 1 {
		t.Fatalf("unsupported boundary allocated an observation: %+v / %v", got, err)
	}
}

func TestSourceObservationRefusesNestedScopesAndNilCallbackWithoutAllocation(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo)
	start := sourceObservationTestStart()
	if err := svc.WithSourceObservation(context.Background(), start, nil); !errors.Is(err, ErrSourceObservationUnsupported) {
		t.Fatalf("nil callback error = %v", err)
	}
	if len(repo.observations) != 0 || len(repo.observationClocks) != 0 {
		t.Fatal("nil callback allocated durable observation state")
	}
	err := svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
		for _, nested := range []context.Context{ctx, context.WithoutCancel(ctx)} {
			called := false
			err := svc.WithSourceObservation(nested, start, func(context.Context) error {
				called = true
				return nil
			})
			if !errors.Is(err, ErrInvalidSourceObservation) || called {
				return fmt.Errorf("nested scope accepted: error=%v callback=%t", err, called)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.observations) != 1 || repo.observationClocks[sourceObservationScope{start.OwnerUserID, start.WorkspaceID}] != 1 {
		t.Fatal("nested scopes advanced the durable tenant counter")
	}
}

func TestSourceObservationCallbackErrorKeepsAllocatedGeneration(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo)
	marker := errors.New("observation callback failed")
	var observed SourceObservation
	err := svc.WithSourceObservation(context.Background(), sourceObservationTestStart(), func(ctx context.Context) error {
		var ok bool
		observed, ok = CurrentSourceObservation(ctx)
		if !ok {
			return errors.New("source observation was not minted before callback")
		}
		return marker
	})
	if !errors.Is(err, marker) || observed.ID == uuid.Nil || observed.Generation != 1 {
		t.Fatalf("callback error lost server allocation: %+v / %v", observed, err)
	}
	if repo.observations[observed.ID] != observed {
		t.Fatal("callback error erased or changed its durable observation")
	}
	next, err := repo.BeginSourceObservation(context.Background(), sourceObservationTestStart())
	if err != nil || next.Generation != 2 || next.ID == observed.ID {
		t.Fatalf("failed callback reused its observation: %+v / %v", next, err)
	}
}

func TestSourceObservationCopiesScopeAndRejectsRetainedContextIntake(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceObservationTestStart()
	var saved context.Context
	var observed SourceObservation
	var op models.Operation
	if _, ok := CurrentSourceObservation(nil); ok {
		t.Fatal("nil context has observation authority")
	}
	if _, ok := CurrentSourceObservation(context.Background()); ok {
		t.Fatal("ordinary context has observation authority")
	}
	err := svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
		saved = ctx
		var ok bool
		observed, ok = CurrentSourceObservation(ctx)
		if !ok || observed.ID == uuid.Nil || observed.Generation != 1 || observed.StartedAt.IsZero() ||
			observed.OwnerUserID != start.OwnerUserID || observed.WorkspaceID != start.WorkspaceID || observed.OriginID != start.OriginID || observed.ConfigDigest != start.ConfigDigest {
			return fmt.Errorf("incorrect active server observation: %+v", observed)
		}
		copy := observed
		copy.ID, copy.Generation, copy.ConfigDigest = uuid.New(), 900, "forged"
		if copy == observed {
			return errors.New("scope-copy fixture did not change its independent value")
		}
		stillActive, ok := CurrentSourceObservation(ctx)
		if !ok || stillActive != observed {
			return errors.New("mutating a scope copy changed authority")
		}
		if !repo.mu.TryLock() {
			return errors.New("observation callback retained the repository mutex")
		}
		stored, present := repo.observations[observed.ID]
		repo.mu.Unlock()
		if !present || stored != observed {
			return errors.New("observation was not persisted before callback")
		}
		child, cancel := context.WithCancel(ctx)
		cancel()
		if _, ok := CurrentSourceObservation(child); ok {
			return errors.New("canceled supplied child retained authority")
		}
		if _, ok := CurrentSourceObservation(ctx); !ok {
			return errors.New("canceling a child revoked its live parent")
		}
		result, err := svc.IngestContext(ctx, sourceObservationTestInput())
		op = result.Operation
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSourceObservationMetadata(t, op, observed)
	before := sourceIdentitySnapshot(repo)
	for _, ctx := range []context.Context{saved, context.WithoutCancel(saved)} {
		if _, ok := CurrentSourceObservation(ctx); ok {
			t.Fatal("retained context extended authority after callback return")
		}
		in := sourceObservationTestInput()
		in.DedupeKey = "retained-context-must-not-create"
		got, err := svc.IngestContext(ctx, in)
		want := ErrInvalidSourceObservation
		if ctx.Err() != nil {
			want = ctx.Err()
		}
		if !errors.Is(err, want) || got.Created || got.Operation.ID != uuid.Nil {
			t.Fatalf("inactive observation fell back to unsequenced intake: %+v / %v", got, err)
		}
		sourceIdentityAssertUnchanged(t, repo, before)
	}
}

func TestSourceObservationWithoutCancelCannotRestoreCanceledAuthority(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := svc.WithSourceObservation(parent, sourceObservationTestStart(), func(ctx context.Context) error {
		detached := context.WithoutCancel(ctx)
		if _, ok := CurrentSourceObservation(ctx); !ok {
			return errors.New("live observation context missing")
		}
		cancel()
		before := sourceIdentitySnapshot(repo)
		for _, retained := range []context.Context{ctx, detached, context.WithoutCancel(ctx)} {
			if _, ok := CurrentSourceObservation(retained); ok {
				return errors.New("detached context regained canceled observation authority")
			}
			got, err := svc.IngestContext(retained, sourceObservationTestInput())
			want := ErrInvalidSourceObservation
			if retained.Err() != nil {
				want = retained.Err()
			}
			if !errors.Is(err, want) || got.Created || got.Operation.ID != uuid.Nil {
				return errors.New("canceled observation permitted unsequenced intake")
			}
		}
		if !reflect.DeepEqual(sourceIdentitySnapshot(repo), before) {
			return errors.New("canceled observation changed ledger state")
		}
		return parent.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled callback error = %v", err)
	}
}

func TestSourceObservationIntakeRequiresIdentityAndMatchingTenant(t *testing.T) {
	for _, kind := range []string{"source_less", "other_owner", "other_workspace", "missing_origin", "other_origin"} {
		t.Run(kind, func(t *testing.T) {
			repo := NewMemoryRepository()
			svc := sourceObservationTestService(repo)
			in := sourceObservationTestInput()
			switch kind {
			case "source_less":
				in.SourceProvider, in.SourceAccount, in.SourceExternalID = "", "", ""
			case "other_owner":
				in.OwnerUserID = "other-owner"
			case "other_workspace":
				in.WorkspaceID = "other-workspace"
			case "missing_origin":
				in.AccountFeedID = nil
			case "other_origin":
				origin := uuid.New()
				in.AccountFeedID = &origin
			}
			if _, err := NewOperation(in, sourceIdentityContractTime); err != nil {
				t.Fatalf("otherwise-valid intake fixture: %v", err)
			}
			before := sourceIdentitySnapshot(repo)
			err := svc.WithSourceObservation(context.Background(), sourceObservationTestStart(), func(ctx context.Context) error {
				got, err := svc.IngestContext(ctx, in)
				if !errors.Is(err, ErrInvalidSourceObservation) || got.Created || got.Operation.ID != uuid.Nil {
					return fmt.Errorf("invalid observed intake accepted: %+v / %v", got, err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			sourceIdentityAssertUnchanged(t, repo, before)
		})
	}
}

func TestSourceObservationRawPayloadCannotMintMetadataAndLegacyIntakeStaysUnsequenced(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		for _, identified := range []bool{false, true} {
			t.Run(fmt.Sprintf("contextual=%t/identified=%t", contextual, identified), func(t *testing.T) {
				repo := NewMemoryRepository()
				svc := sourceObservationTestService(repo)
				in := sourceObservationTestInput()
				if !identified {
					in.SourceProvider, in.SourceAccount, in.SourceExternalID = "", "", ""
				}
				wire, err := json.Marshal(in)
				if err != nil {
					t.Fatal(err)
				}
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(wire, &raw); err != nil {
					t.Fatal(err)
				}
				raw["SourceObservationID"] = json.RawMessage(`"00000000-0000-4000-8000-000000000999"`)
				raw["SourceObservationGeneration"] = json.RawMessage(`999`)
				wire, err = json.Marshal(raw)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(wire, &in); err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"SourceObservationID", "SourceObservationGeneration"} {
					if _, exists := reflect.TypeOf(in).FieldByName(field); exists {
						t.Fatalf("public intake exposes caller observation authority: %s", field)
					}
				}
				var result IngestResult
				if contextual {
					result, err = svc.IngestContext(context.Background(), in)
				} else {
					result, err = svc.Ingest(in)
				}
				if err != nil || !result.Created || result.Operation.SourceObservationID != nil || result.Operation.SourceObservationGeneration != 0 {
					t.Fatalf("ordinary intake became sequenced or incompatible: %+v / %v", result, err)
				}
			})
		}
	}
}

type sourceObservationCompletion struct {
	op          models.Operation
	observation SourceObservation
	err         error
}

func TestSourceObservationGenerationFollowsStartNotCompletionOrder(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceObservationTestStart()
	firstStarted := make(chan SourceObservation, 1)
	allowFirst := make(chan struct{})
	firstDone := make(chan sourceObservationCompletion, 1)
	var release sync.Once
	unblock := func() { release.Do(func() { close(allowFirst) }) }
	defer unblock()
	go func() {
		result := sourceObservationCompletion{}
		result.err = svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
			var ok bool
			result.observation, ok = CurrentSourceObservation(ctx)
			if !ok {
				return errors.New("first callback missing observation")
			}
			firstStarted <- result.observation
			select {
			case <-allowFirst:
			case <-time.After(5 * time.Second):
				return errors.New("first callback release timed out")
			}
			in := sourceObservationTestInput()
			in.DedupeKey, in.SourceRevisionHash = "first-start-late-completion", "revision:first"
			got, err := svc.IngestContext(ctx, in)
			result.op = got.Operation
			return err
		})
		firstDone <- result
	}()
	var first SourceObservation
	select {
	case first = <-firstStarted:
	case result := <-firstDone:
		t.Fatalf("first callback failed before starting: %v", result.err)
	case <-time.After(2 * time.Second):
		t.Fatal("first observation did not start")
	}
	secondDone := make(chan sourceObservationCompletion, 1)
	go func() {
		result := sourceObservationCompletion{}
		result.err = NewService(repo).WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
			var ok bool
			result.observation, ok = CurrentSourceObservation(ctx)
			if !ok {
				return errors.New("second callback missing observation")
			}
			in := sourceObservationTestInput()
			in.DedupeKey, in.SourceRevisionHash = "second-start-first-completion", "revision:second"
			got, err := svc.IngestContext(ctx, in)
			result.op = got.Operation
			return err
		})
		secondDone <- result
	}()
	var second sourceObservationCompletion
	select {
	case second = <-secondDone:
	case <-time.After(2 * time.Second):
		unblock()
		t.Fatal("second observation could not complete while first callback was waiting")
	}
	select {
	case result := <-firstDone:
		t.Fatalf("first observation completed before its explicit release: %v", result.err)
	default:
	}
	unblock()
	var completedFirst sourceObservationCompletion
	select {
	case completedFirst = <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first observation did not complete after release")
	}
	if !errors.Is(completedFirst.err, ErrSourceHeadSuperseded) || completedFirst.op.ID != uuid.Nil || second.err != nil || first.Generation != 1 || second.observation.Generation != 2 || first.ID == second.observation.ID {
		t.Fatalf("observation order or intake failed: first=%+v second=%+v", completedFirst, second)
	}
	for _, result := range []sourceObservationCompletion{second} {
		stored, err := repo.GetByID(start.OwnerUserID, start.WorkspaceID, result.op.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertSourceObservationMetadata(t, *stored, result.observation)
	}
	old, found, err := repo.FindByDedupeKey(start.OwnerUserID, start.WorkspaceID, "first-start-late-completion")
	if err != nil || found || old != nil {
		t.Fatal("late old observation must be refused before operation creation")
	}
	if retained, ok := repo.observations[first.ID]; !ok || !sameObservationRecord(retained, first) {
		t.Fatal("refusing a late result erased its observation history")
	}
}

func TestSourceObservationDuplicateDoesNotBackfillUnsequencedRows(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	in := sourceObservationTestInput()
	old, err := svc.IngestContext(context.Background(), in)
	if err != nil || !old.Created || old.Operation.SourceObservationID != nil || old.Operation.SourceObservationGeneration != 0 {
		t.Fatalf("unsequenced fixture: %+v / %v", old, err)
	}
	before := sourceIdentitySnapshot(repo)
	err = svc.WithSourceObservation(context.Background(), sourceObservationTestStart(), func(ctx context.Context) error {
		got, err := svc.IngestContext(ctx, in)
		if !errors.Is(err, ErrSourceHeadReconciliation) || got.Created || got.Operation.ID != uuid.Nil {
			return errors.New("unsequenced duplicate must require reconciliation without returning authority")
		}
		return err
	})
	if !errors.Is(err, ErrSourceHeadReconciliation) {
		t.Fatal(err)
	}
	sourceIdentityAssertUnchanged(t, repo, before)
	stored, err := repo.GetByID(in.OwnerUserID, in.WorkspaceID, old.Operation.ID)
	if err != nil || stored == nil || stored.SourceObservationID != nil || stored.SourceObservationGeneration != 0 {
		t.Fatalf("old row was backfilled: %+v / %v", stored, err)
	}
}

func TestSourceObservationImmutableAcrossOrdinaryAuditedAndClaimedMutationPaths(t *testing.T) {
	for _, path := range []string{"update", "atomic_update", "context_update", "save", "transition", "claimed", "claimed_service"} {
		for _, kind := range []string{"id", "generation", "clear", "backfill", "origin", "clear_origin"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				repo := NewMemoryRepository()
				svc := sourceObservationTestService(repo)
				in := sourceObservationTestInput()
				var op models.Operation
				var observed SourceObservation
				if kind == "backfill" {
					result, ingestErr := svc.IngestContext(context.Background(), in)
					if ingestErr != nil || !result.Created {
						t.Fatalf("seed unsequenced operation: %+v / %v", result, ingestErr)
					}
					op = result.Operation
					ticket, beginErr := repo.BeginSourceObservation(context.Background(), sourceObservationTestStart())
					if beginErr != nil {
						t.Fatal(beginErr)
					}
					observed = ticket
				} else {
					op, observed = sourceObservationTestIngest(t, svc, sourceObservationTestStart(), in)
				}
				worker := uuid.MustParse("00000000-0000-4000-8000-000000000903")
				var claim ExecutionClaim
				if strings.HasPrefix(path, "claimed") {
					beforeClaim := sourceIdentitySnapshot(repo)
					claimed, err := svc.ClaimNext(context.Background(), op.OwnerUserID, op.WorkspaceID, worker, time.Minute)
					if kind == "backfill" {
						if err != nil || claimed != nil {
							t.Fatalf("unsequenced identified operation became claimable: %+v / %v", claimed, err)
						}
						sourceIdentityAssertUnchanged(t, repo, beforeClaim)
						return
					}
					if err != nil || claimed == nil || claimed.Operation.ID != op.ID {
						t.Fatalf("claim immutable-observation fixture: %+v / %v", claimed, err)
					}
					op, claim = claimed.Operation, claimed.Claim
				}
				before := sourceIdentitySnapshot(repo)
				bad := op
				switch kind {
				case "id":
					id := uuid.New()
					bad.SourceObservationID = &id
				case "generation":
					bad.SourceObservationGeneration++
				case "clear":
					bad.SourceObservationID, bad.SourceObservationGeneration = nil, 0
				case "backfill":
					id := observed.ID
					bad.SourceObservationID, bad.SourceObservationGeneration = &id, observed.Generation
				case "origin":
					origin := uuid.New()
					bad.AccountFeedID = &origin
				case "clear_origin":
					bad.AccountFeedID = nil
				}
				var got *models.Operation
				var err error
				switch path {
				case "save":
					got, err = svc.Save(bad, "metadata_changed", "test", "observation must remain immutable")
				case "transition":
					got, err = svc.Transition(bad, StatusClassified, "test", "", "observation must remain immutable")
				case "claimed_service":
					got, err = svc.TransitionClaimed(context.Background(), claim, bad, StatusClassified, "test", "", "observation must remain immutable")
				case "claimed":
					var event models.OperationEvent
					bad, event, err = ApplyTransition(bad, StatusClassified, "test", "", "observation must remain immutable", sourceIdentityContractTime.Add(time.Second))
					if err != nil {
						t.Fatalf("valid mutated fixture transition: %v", err)
					}
					got, err = repo.TransitionClaimed(context.Background(), claim, bad, event, true)
				default:
					bad.Version++
					bad.UpdatedAt = sourceIdentityContractTime.Add(time.Second)
					event := models.OperationEvent{OperationID: bad.ID, EventType: "metadata_changed", ActorType: "test", AfterStatus: bad.Status, PayloadJSON: "{}", CreatedAt: bad.UpdatedAt}
					switch path {
					case "update":
						got, err = repo.Update(&bad)
					case "atomic_update":
						got, err = repo.UpdateWithEvent(&bad, &event)
					case "context_update":
						got, err = repo.UpdateWithEventContext(context.Background(), &bad, &event)
					}
				}
				if !errors.Is(err, ErrSourceIdentityImmutable) || got != nil {
					t.Fatalf("observation mutation accepted/wrong error: %+v / %v", got, err)
				}
				sourceIdentityAssertUnchanged(t, repo, before)
			})
		}
	}
}

func TestSourceObservationMemoryReadsDefensivelyCopyObservationPointer(t *testing.T) {
	for _, path := range []string{"intake_result", "get", "dedupe", "context_dedupe", "list", "list_due", "claim_next", "claim_operation", "claimed_transition"} {
		t.Run(path, func(t *testing.T) {
			repo := NewMemoryRepository()
			svc := sourceObservationTestService(repo)
			op, observed := sourceObservationTestIngest(t, svc, sourceObservationTestStart(), sourceObservationTestInput())
			var returned *models.Operation
			var err error
			switch path {
			case "intake_result":
				returned = &op
			case "get":
				returned, err = repo.GetByID(op.OwnerUserID, op.WorkspaceID, op.ID)
			case "dedupe":
				returned, _, err = repo.FindByDedupeKey(op.OwnerUserID, op.WorkspaceID, op.DedupeKey)
			case "context_dedupe":
				returned, _, err = repo.FindByDedupeKeyContext(context.Background(), op.OwnerUserID, op.WorkspaceID, op.DedupeKey)
			case "list", "list_due":
				var rows []models.Operation
				if path == "list" {
					rows, err = repo.List(Filter{OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID})
				} else {
					rows, err = repo.ListDue(op.OwnerUserID, op.WorkspaceID, 10)
				}
				if len(rows) != 1 {
					t.Fatalf("list fixture returned %d rows / %v", len(rows), err)
				}
				returned = &rows[0]
			case "claim_next", "claim_operation", "claimed_transition":
				worker := uuid.MustParse("00000000-0000-4000-8000-000000000904")
				var claimed *ClaimedOperation
				if path == "claim_operation" {
					op.AutonomyLevel, op.CurrentDecision = string(AutonomyAuto), string(DecisionRunSafeLocalWorker)
					classified, transitionErr := svc.Transition(op, StatusClassified, "test", "", "classify safe claim fixture")
					if transitionErr != nil || classified == nil {
						t.Fatalf("classify claim fixture: %+v / %v", classified, transitionErr)
					}
					claimed, err = svc.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, worker, time.Minute)
				} else {
					claimed, err = svc.ClaimNext(context.Background(), op.OwnerUserID, op.WorkspaceID, worker, time.Minute)
				}
				if err != nil || claimed == nil {
					t.Fatalf("claim pointer-copy fixture: %+v / %v", claimed, err)
				}
				if path == "claimed_transition" {
					returned, err = svc.TransitionClaimed(context.Background(), claimed.Claim, claimed.Operation, StatusClassified, "test", "", "unchanged observation")
				} else {
					returned = &claimed.Operation
				}
			}
			if err != nil || returned == nil || returned.SourceObservationID == nil {
				t.Fatalf("healthy read/write path failed: %+v / %v", returned, err)
			}
			*returned.SourceObservationID = uuid.New()
			if returned.AccountFeedID == nil {
				t.Fatal("healthy result lost its origin pointer")
			}
			*returned.AccountFeedID = uuid.New()
			stored, err := repo.GetByID(op.OwnerUserID, op.WorkspaceID, op.ID)
			if err != nil || stored == nil {
				t.Fatalf("get stored operation after pointer mutation: %+v / %v", stored, err)
			}
			assertSourceObservationMetadata(t, *stored, observed)
			if stored.AccountFeedID == nil || *stored.AccountFeedID != observed.OriginID {
				t.Fatal("mutating a returned origin pointer changed stored provenance")
			}
		})
	}
}

func TestSourceObservationMemoryWritesDefensivelyCopyInputPointers(t *testing.T) {
	for _, path := range []string{"create", "atomic_create", "context_create", "update", "atomic_update", "context_update", "claimed_transition"} {
		t.Run(path, func(t *testing.T) {
			repo := NewMemoryRepository()
			svc := sourceObservationTestService(repo)
			start := sourceObservationTestStart()
			var input models.Operation
			var observed SourceObservation
			var returned *models.Operation
			var err error
			var claim ExecutionClaim
			if strings.HasSuffix(path, "create") {
				observed, err = repo.BeginSourceObservation(context.Background(), start)
				if err != nil {
					t.Fatal(err)
				}
				input = sourceIdentityContractOperation(t, sourceObservationTestInput())
			} else {
				input, observed = sourceObservationTestIngest(t, svc, start, sourceObservationTestInput())
				if path == "claimed_transition" {
					claimed, claimErr := svc.ClaimNext(context.Background(), input.OwnerUserID, input.WorkspaceID, uuid.New(), time.Minute)
					if claimErr != nil || claimed == nil {
						t.Fatalf("claim write fixture: %+v / %v", claimed, claimErr)
					}
					input, claim = claimed.Operation, claimed.Claim
				}
			}
			// These are independent caller-owned allocations, not pointers borrowed
			// from the stored record or the production cloning helper.
			id, origin := observed.ID, observed.OriginID
			input.SourceObservationID, input.SourceObservationGeneration, input.AccountFeedID = &id, observed.Generation, &origin
			event := models.OperationEvent{OperationID: input.ID, EventType: "created", ActorType: string(OwnerHAI), AfterStatus: input.Status, PayloadJSON: "{}", CreatedAt: input.CreatedAt}
			switch path {
			case "create":
				returned, err = repo.Create(&input)
			case "atomic_create":
				returned, err = repo.CreateWithEvent(&input, &event)
			case "context_create":
				returned, err = repo.CreateWithEventContext(context.Background(), &input, &event)
			case "claimed_transition":
				input, event, err = ApplyTransition(input, StatusClassified, "test", "", "healthy unchanged observation", sourceIdentityContractTime.Add(time.Second))
				if err == nil {
					returned, err = repo.TransitionClaimed(context.Background(), claim, input, event, false)
				}
			default:
				input.Version++
				input.UpdatedAt = sourceIdentityContractTime.Add(time.Second)
				input.Title = "Healthy display-only change"
				event.EventType, event.CreatedAt = "metadata_changed", input.UpdatedAt
				switch path {
				case "update":
					returned, err = repo.Update(&input)
				case "atomic_update":
					returned, err = repo.UpdateWithEvent(&input, &event)
				case "context_update":
					returned, err = repo.UpdateWithEventContext(context.Background(), &input, &event)
				}
			}
			if err != nil || returned == nil {
				t.Fatalf("healthy write fixture: %+v / %v", returned, err)
			}
			*input.SourceObservationID, *input.AccountFeedID = uuid.New(), uuid.New()
			assertSourceObservationMetadata(t, *returned, observed)
			if returned.AccountFeedID == nil || *returned.AccountFeedID != observed.OriginID {
				t.Fatal("returned write snapshot aliases caller origin pointer")
			}
			stored, err := repo.GetByID(start.OwnerUserID, start.WorkspaceID, input.ID)
			if err != nil || stored == nil {
				t.Fatalf("stored write snapshot: %+v / %v", stored, err)
			}
			assertSourceObservationMetadata(t, *stored, observed)
			if stored.AccountFeedID == nil || *stored.AccountFeedID != observed.OriginID {
				t.Fatal("stored write aliases caller origin pointer")
			}
		})
	}
}

type sourceObservationQueuedRepository struct {
	*MemoryRepository
	phase   string
	arrived chan context.Context
	resume  chan struct{}
}

func (r *sourceObservationQueuedRepository) wait(ctx context.Context) error {
	r.arrived <- ctx
	select {
	case <-r.resume:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("queued observation intake was not released")
	}
}

func (r *sourceObservationQueuedRepository) FindByDedupeKeyContext(ctx context.Context, owner, workspace, key string) (*models.Operation, bool, error) {
	if r.phase == "lookup" {
		if err := r.wait(ctx); err != nil {
			return nil, false, err
		}
	}
	return r.MemoryRepository.FindByDedupeKeyContext(ctx, owner, workspace, key)
}

func (r *sourceObservationQueuedRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if r.phase == "create" {
		if err := r.wait(ctx); err != nil {
			return nil, err
		}
	}
	return r.MemoryRepository.CreateWithEventContext(ctx, op, event)
}

type sourceObservationQueuedIntakeResult struct {
	result IngestResult
	err    error
}

func TestSourceObservationDetachedQueuedIntakeCannotWriteAfterAuthorityEnds(t *testing.T) {
	for _, phase := range []string{"lookup", "create"} {
		for _, ending := range []string{"server_cancel", "callback_return"} {
			t.Run(phase+"/"+ending, func(t *testing.T) {
				base := NewMemoryRepository()
				repo := &sourceObservationQueuedRepository{
					MemoryRepository: base, phase: phase,
					arrived: make(chan context.Context, 1), resume: make(chan struct{}),
				}
				svc := NewService(repo)
				svc.now = func() time.Time { return sourceIdentityContractTime }
				parent, cancel := context.WithCancel(context.Background())
				defer cancel()
				before := sourceIdentitySnapshot(base)
				done := make(chan sourceObservationQueuedIntakeResult, 1)
				started, drained := false, false
				var release sync.Once
				unblock := func() { release.Do(func() { close(repo.resume) }) }
				defer func() {
					unblock()
					if started && !drained {
						select {
						case <-done:
						case <-time.After(2 * time.Second):
							t.Error("queued intake did not exit during cleanup")
						}
					}
				}()
				var queued context.Context
				err := svc.WithSourceObservation(parent, sourceObservationTestStart(), func(ctx context.Context) error {
					detached := context.WithoutCancel(ctx)
					if detached.Err() != nil {
						return errors.New("detached-context fixture is not live")
					}
					started = true
					go func() {
						result, err := svc.IngestContext(detached, sourceObservationTestInput())
						done <- sourceObservationQueuedIntakeResult{result: result, err: err}
					}()
					select {
					case queued = <-repo.arrived:
					case <-time.After(2 * time.Second):
						return errors.New("valid observed intake did not reach repository gate")
					}
					if _, ok := CurrentSourceObservation(queued); !ok || queued.Err() != nil {
						return errors.New("queued intake did not begin under live server authority")
					}
					if ending == "server_cancel" {
						cancel()
						select {
						case <-queued.Done():
						case <-time.After(2 * time.Second):
							return errors.New("server cancellation did not reach detached queued intake inside active callback")
						}
					}
					return nil
				})
				if ending == "server_cancel" {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("server-canceled boundary error = %v", err)
					}
				} else if err != nil {
					t.Fatalf("callback-return boundary error = %v", err)
				}
				if queued == nil {
					t.Fatal("intake did not enter its repository gate")
				}
				select {
				case <-queued.Done():
				case <-time.After(2 * time.Second):
					t.Fatal("queued intake retained detached authority after callback return")
				}
				if _, ok := CurrentSourceObservation(queued); ok {
					t.Fatal("ended authority remained visible to queued intake")
				}
				unblock()
				var result sourceObservationQueuedIntakeResult
				select {
				case result = <-done:
					drained = true
				case <-time.After(2 * time.Second):
					t.Fatal("queued intake did not finish after repository gate release")
				}
				if !errors.Is(result.err, context.Canceled) || result.result.Created || result.result.Operation.ID != uuid.Nil {
					t.Fatalf("detached queued intake wrote or returned success after authority ended: %+v / %v", result.result, result.err)
				}
				sourceIdentityAssertUnchanged(t, base, before)
			})
		}
	}
}
