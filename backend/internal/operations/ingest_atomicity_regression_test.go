package operations

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// Embed only Repository so optional atomic capabilities are deliberately hidden.
type ingestAtomicLegacyProbe struct {
	Repository
	lookups int
	creates int
	updates int
	appends int
	find    func(string, string, string) (*models.Operation, bool, error)
}

func (r *ingestAtomicLegacyProbe) FindByDedupeKey(owner, workspace, key string) (*models.Operation, bool, error) {
	r.lookups++
	if r.find != nil {
		return r.find(owner, workspace, key)
	}
	return r.Repository.FindByDedupeKey(owner, workspace, key)
}

func (r *ingestAtomicLegacyProbe) Create(op *models.Operation) (*models.Operation, error) {
	r.creates++
	return r.Repository.Create(op)
}

func (r *ingestAtomicLegacyProbe) Update(op *models.Operation) (*models.Operation, error) {
	r.updates++
	return r.Repository.Update(op)
}

func (r *ingestAtomicLegacyProbe) AppendEvent(event *models.OperationEvent) error {
	r.appends++
	return r.Repository.AppendEvent(event)
}

type ingestAtomicProbe struct {
	*ingestAtomicLegacyProbe
	creationCalls int
	mutationCalls int
	createPair    func(*models.Operation, *models.OperationEvent) (*models.Operation, error)
	updatePair    func(*models.Operation, *models.OperationEvent) (*models.Operation, error)
}

func (r *ingestAtomicProbe) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.creationCalls++
	return r.createPair(op, event)
}

func (r *ingestAtomicProbe) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.mutationCalls++
	return r.updatePair(op, event)
}

var _ AtomicCreationRepository = (*ingestAtomicProbe)(nil)
var _ AtomicMutationRepository = (*ingestAtomicProbe)(nil)

func ingestAtomicPair(t *testing.T) (models.Operation, models.OperationEvent) {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	op, err := NewOperation(sampleInput(), now)
	if err != nil {
		t.Fatal(err)
	}
	op.ID = uuid.New()
	return op, models.OperationEvent{
		OperationID: op.ID,
		EventType:   "created",
		ActorType:   string(OwnerHAI),
		AfterStatus: string(StatusNew),
		PayloadJSON: "{}",
		CreatedAt:   now,
	}
}

func ingestAtomicSnapshot(repo *MemoryRepository) (map[uuid.UUID]models.Operation, []models.OperationEvent) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	ops := make(map[uuid.UUID]models.Operation, len(repo.ops))
	for id, op := range repo.ops {
		ops[id] = op
	}
	return ops, append([]models.OperationEvent(nil), repo.events...)
}

func ingestAtomicAssertSnapshot(t *testing.T, repo *MemoryRepository, ops map[uuid.UUID]models.Operation, events []models.OperationEvent) {
	t.Helper()
	afterOps, afterEvents := ingestAtomicSnapshot(repo)
	if !reflect.DeepEqual(afterOps, ops) || !reflect.DeepEqual(afterEvents, events) {
		t.Fatalf("unexpected operation/audit changes: operations=%+v events=%+v", afterOps, afterEvents)
	}
}

func ingestAtomicAssertNoSplit(t *testing.T, repo *ingestAtomicLegacyProbe) {
	t.Helper()
	if repo.creates != 0 || repo.updates != 0 || repo.appends != 0 {
		t.Fatalf("split persistence attempted: creates=%d updates=%d appends=%d", repo.creates, repo.updates, repo.appends)
	}
}

func TestAtomicIngestMemoryCreationCopiesInputs(t *testing.T) {
	for _, explicitEventID := range []bool{false, true} {
		name := "generated_event_id"
		if explicitEventID {
			name = "explicit_event_id"
		}
		t.Run(name, func(t *testing.T) {
			repo := NewMemoryRepository()
			op, event := ingestAtomicPair(t)
			if explicitEventID {
				event.ID = uuid.New()
			}
			beforeOp, beforeEvent := op, event
			created, err := repo.CreateWithEvent(&op, &event)
			if err != nil || created == nil || !reflect.DeepEqual(*created, beforeOp) {
				t.Fatalf("creation result: operation=%+v err=%v", created, err)
			}
			if !reflect.DeepEqual(op, beforeOp) || !reflect.DeepEqual(event, beforeEvent) {
				t.Fatal("creation mutated caller inputs")
			}
			ops, events := ingestAtomicSnapshot(repo)
			if len(ops) != 1 || !reflect.DeepEqual(ops[op.ID], beforeOp) || len(events) != 1 || events[0].ID == uuid.Nil {
				t.Fatalf("creation pair not stored: operations=%+v events=%+v", ops, events)
			}
			wantEvent := beforeEvent
			if !explicitEventID {
				wantEvent.ID = events[0].ID
			}
			if !reflect.DeepEqual(events[0], wantEvent) {
				t.Fatalf("creation audit mismatch: got=%+v want=%+v", events[0], wantEvent)
			}
			op.Title = "caller edit"
			event.Message = "caller edit"
			created.Title = "return value edit"
			ingestAtomicAssertSnapshot(t, repo, ops, events)
		})
	}
}

func TestAtomicIngestServiceCreationBindsAuditBeforeWrite(t *testing.T) {
	base := NewMemoryRepository()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	repo := &ingestAtomicProbe{ingestAtomicLegacyProbe: &ingestAtomicLegacyProbe{Repository: base}}
	repo.createPair = func(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
		if op.ID == uuid.Nil || op.Version != 1 || op.Status != string(StatusNew) ||
			event.OperationID != op.ID || event.EventType != "created" || event.ActorType != string(OwnerHAI) ||
			event.BeforeStatus != "" || event.AfterStatus != op.Status || event.PayloadJSON != "{}" ||
			!op.CreatedAt.Equal(now) || !op.UpdatedAt.Equal(now) || !event.CreatedAt.Equal(now) {
			t.Fatalf("unbound or malformed pre-write pair: operation=%+v event=%+v", op, event)
		}
		return base.CreateWithEvent(op, event)
	}
	svc := NewService(repo)
	svc.now = func() time.Time { return now }
	in := sampleInput()
	beforeInput := in
	result, err := svc.Ingest(in)
	if err != nil || !result.Created || repo.creationCalls != 1 || repo.mutationCalls != 0 {
		t.Fatalf("creation path: result=%+v err=%v creations=%d mutations=%d", result, err, repo.creationCalls, repo.mutationCalls)
	}
	if !reflect.DeepEqual(in, beforeInput) {
		t.Fatal("Ingest changed caller input")
	}
	ops, events := ingestAtomicSnapshot(base)
	if len(ops) != 1 || len(events) != 1 || !reflect.DeepEqual(ops[result.Operation.ID], result.Operation) || events[0].OperationID != result.Operation.ID {
		t.Fatalf("creation result/storage mismatch: operations=%+v events=%+v", ops, events)
	}
	ingestAtomicAssertNoSplit(t, repo.ingestAtomicLegacyProbe)
}

func TestAtomicIngestServiceCreationFailureNoPartialWrites(t *testing.T) {
	for _, failure := range []string{"repository_error", "duplicate_event_id"} {
		t.Run(failure, func(t *testing.T) {
			base := NewMemoryRepository()
			op, event := ingestAtomicPair(t)
			if _, err := base.CreateWithEvent(&op, &event); err != nil {
				t.Fatal(err)
			}
			ops, events := ingestAtomicSnapshot(base)
			failureErr := errors.New("creation audit unavailable")
			repo := &ingestAtomicProbe{ingestAtomicLegacyProbe: &ingestAtomicLegacyProbe{Repository: base}}
			repo.createPair = func(candidate *models.Operation, audit *models.OperationEvent) (*models.Operation, error) {
				if failure == "repository_error" {
					return nil, failureErr
				}
				copyAudit := *audit
				copyAudit.ID = events[0].ID
				return base.CreateWithEvent(candidate, &copyAudit)
			}
			in := sampleInput()
			in.DedupeKey = "distinct-creation"
			result, err := NewService(repo).Ingest(in)
			if err == nil || result.Created || result.Operation.ID != uuid.Nil ||
				(failure == "repository_error" && !errors.Is(err, failureErr)) || repo.creationCalls != 1 || repo.mutationCalls != 0 {
				t.Fatalf("creation failure: result=%+v err=%v creations=%d mutations=%d", result, err, repo.creationCalls, repo.mutationCalls)
			}
			ingestAtomicAssertNoSplit(t, repo.ingestAtomicLegacyProbe)
			ingestAtomicAssertSnapshot(t, base, ops, events)
		})
	}
}

func TestAtomicIngestUnsupportedCreationAndReadOnlyDuplicate(t *testing.T) {
	base := NewMemoryRepository()
	repo := &ingestAtomicLegacyProbe{Repository: base}
	result, err := NewService(repo).Ingest(sampleInput())
	if !errors.Is(err, ErrAtomicCreationUnsupported) || result.Created || result.Operation.ID != uuid.Nil {
		t.Fatalf("unsupported creation accepted: result=%+v err=%v", result, err)
	}
	ingestAtomicAssertNoSplit(t, repo)
	ops, events := ingestAtomicSnapshot(base)
	if len(ops) != 0 || len(events) != 0 {
		t.Fatal("unsupported creation left partial state")
	}
	op, event := ingestAtomicPair(t)
	op.EvidenceJSON = `{"revision":1}`
	op.SourceEvidenceRawSHA256 = rawEvidenceSHA256(op.EvidenceJSON)
	if _, err := base.CreateWithEvent(&op, &event); err != nil {
		t.Fatal(err)
	}
	ops, events = ingestAtomicSnapshot(base)
	for _, evidence := range []string{"", " \t ", "{}", " {} ", op.EvidenceJSON} {
		in := sampleInput()
		in.EvidenceJSON = evidence
		result, err := NewService(repo).Ingest(in)
		if err != nil || result.Created || !reflect.DeepEqual(result.Operation, op) {
			t.Fatalf("read-only duplicate evidence=%q: result=%+v err=%v", evidence, result, err)
		}
		ingestAtomicAssertNoSplit(t, repo)
		ingestAtomicAssertSnapshot(t, base, ops, events)
	}
}

func TestAtomicIngestNormalizesScopeButRetainsOpaqueDedupe(t *testing.T) {
	for _, workspace := range []string{" \t ", "", " project-a \t"} {
		t.Run("workspace="+workspace, func(t *testing.T) {
			base := NewMemoryRepository()
			wantWorkspace := "local"
			if workspace == " project-a \t" {
				wantWorkspace = "project-a"
			}
			in := sampleInput()
			in.OwnerUserID = " \tuser-1 \n"
			in.WorkspaceID = workspace
			in.DedupeKey = " \tOpaque:CaseSensitive/Key \n"
			repo := &ingestAtomicProbe{ingestAtomicLegacyProbe: &ingestAtomicLegacyProbe{Repository: base}, createPair: base.CreateWithEvent}
			repo.find = func(owner, space, key string) (*models.Operation, bool, error) {
				if owner != "user-1" || space != wantWorkspace || key != in.DedupeKey {
					t.Fatalf("lookup scope/key mismatch: owner=%q workspace=%q key=%q", owner, space, key)
				}
				return base.FindByDedupeKey(owner, space, key)
			}
			first, err := NewService(repo).Ingest(in)
			if err != nil || !first.Created || first.Operation.OwnerUserID != "user-1" ||
				first.Operation.WorkspaceID != wantWorkspace || first.Operation.DedupeKey != in.DedupeKey {
				t.Fatalf("normalized creation: result=%+v err=%v", first, err)
			}
			ops, events := ingestAtomicSnapshot(base)
			in.OwnerUserID, in.WorkspaceID = "user-1", wantWorkspace
			second, err := NewService(repo).Ingest(in)
			if err != nil || second.Created || !reflect.DeepEqual(second.Operation, first.Operation) || repo.creationCalls != 1 || repo.mutationCalls != 0 {
				t.Fatalf("normalized duplicate: result=%+v err=%v", second, err)
			}
			ingestAtomicAssertNoSplit(t, repo.ingestAtomicLegacyProbe)
			ingestAtomicAssertSnapshot(t, base, ops, events)
		})
	}
}

func TestAtomicIngestOwnerWorkspaceIsolation(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo)
	inputs := []NewOperationInput{sampleInput(), sampleInput(), sampleInput()}
	inputs[1].OwnerUserID = "user-2"
	inputs[2].WorkspaceID = "another-workspace"
	ids := make(map[uuid.UUID]bool)
	for _, in := range inputs {
		result, err := svc.Ingest(in)
		if err != nil || !result.Created || result.Operation.OwnerUserID != in.OwnerUserID || result.Operation.WorkspaceID != in.WorkspaceID || ids[result.Operation.ID] {
			t.Fatalf("cross-scope creation: result=%+v err=%v", result, err)
		}
		ids[result.Operation.ID] = true
		duplicate, err := svc.Ingest(in)
		if err != nil || duplicate.Created || !reflect.DeepEqual(duplicate.Operation, result.Operation) {
			t.Fatalf("cross-scope duplicate: result=%+v err=%v", duplicate, err)
		}
	}
	ops, events := ingestAtomicSnapshot(repo)
	if len(ops) != 3 || len(events) != 3 {
		t.Fatalf("scope isolation counts: operations=%d events=%d", len(ops), len(events))
	}
	for _, event := range events {
		if !ids[event.OperationID] || event.EventType != "created" {
			t.Fatalf("unexpected scoped audit: %+v", event)
		}
		delete(ids, event.OperationID)
	}
}

// Every first lookup must miss before any contender can create its pair.
type ingestAtomicRaceRepository struct {
	*MemoryRepository
	arrivals chan struct{}
	release  chan struct{}
}

func (r *ingestAtomicRaceRepository) FindByDedupeKey(owner, workspace, key string) (*models.Operation, bool, error) {
	op, found, err := r.MemoryRepository.FindByDedupeKey(owner, workspace, key)
	if err == nil && !found {
		r.arrivals <- struct{}{}
		<-r.release
	}
	return op, found, err
}

func TestAtomicIngestConcurrentIdenticalInputsOnePair(t *testing.T) {
	const contenders = 8
	base := NewMemoryRepository()
	repo := &ingestAtomicRaceRepository{MemoryRepository: base, arrivals: make(chan struct{}, contenders), release: make(chan struct{})}
	svc := NewService(repo)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	type outcome struct {
		result IngestResult
		err    error
	}
	results := make(chan outcome, contenders)
	for i := 0; i < contenders; i++ {
		go func() {
			result, err := svc.Ingest(sampleInput())
			results <- outcome{result: result, err: err}
		}()
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for i := 0; i < contenders; i++ {
		select {
		case <-repo.arrivals:
		case <-timer.C:
			close(repo.release)
			t.Fatal("concurrent initial lookups did not reach the bounded barrier")
		}
	}
	close(repo.release)
	createdCount := 0
	var winner models.Operation
	for i := 0; i < contenders; i++ {
		select {
		case got := <-results:
			if got.err != nil || got.result.Operation.ID == uuid.Nil {
				t.Fatalf("concurrent ingest: result=%+v err=%v", got.result, got.err)
			}
			if i == 0 {
				winner = got.result.Operation
			} else if !reflect.DeepEqual(got.result.Operation, winner) {
				t.Fatal("concurrent ingests returned different winner snapshots")
			}
			if got.result.Created {
				createdCount++
			}
		case <-timer.C:
			t.Fatal("concurrent ingestion did not finish within five seconds")
		}
	}
	ops, events := ingestAtomicSnapshot(base)
	if createdCount != 1 || len(ops) != 1 || len(events) != 1 || events[0].OperationID != winner.ID || events[0].EventType != "created" {
		t.Fatalf("concurrent duplicate pair: created=%d operations=%+v events=%+v", createdCount, ops, events)
	}
}

func TestAtomicIngestCollisionRefreshesScopedWinnerWithAudit(t *testing.T) {
	base := NewMemoryRepository()
	winner, event := ingestAtomicPair(t)
	winner.EvidenceJSON = `{"winner":true}`
	winner.SourceEvidenceRawSHA256 = rawEvidenceSHA256(winner.EvidenceJSON)
	winner.WorldModelStateJSON = `{"outcomeUncertain":true}`
	if _, err := base.CreateWithEvent(&winner, &event); err != nil {
		t.Fatal(err)
	}
	other := winner
	other.ID, other.OwnerUserID = uuid.New(), "other-owner"
	otherEvent := event
	otherEvent.ID, otherEvent.OperationID = uuid.New(), other.ID
	if _, err := base.CreateWithEvent(&other, &otherEvent); err != nil {
		t.Fatal(err)
	}
	ops, events := ingestAtomicSnapshot(base)
	repo := &ingestAtomicProbe{ingestAtomicLegacyProbe: &ingestAtomicLegacyProbe{Repository: base}}
	repo.find = func(owner, workspace, key string) (*models.Operation, bool, error) {
		if owner != winner.OwnerUserID || workspace != winner.WorkspaceID || key != winner.DedupeKey {
			t.Fatalf("collision lookup escaped scope: %q %q %q", owner, workspace, key)
		}
		if repo.lookups == 1 {
			return nil, false, nil
		}
		return base.FindByDedupeKey(owner, workspace, key)
	}
	repo.createPair = base.CreateWithEvent
	repo.updatePair = base.UpdateWithEvent
	now := winner.UpdatedAt.Add(time.Minute)
	svc := NewService(repo)
	svc.now = func() time.Time { return now }
	in := sampleInput()
	in.OwnerUserID, in.WorkspaceID = " user-1 ", " local "
	in.EvidenceJSON = `{"loser":true}`
	result, err := svc.Ingest(in)
	want := winner
	want.EvidenceJSON, want.SourceEvidenceRawSHA256 = in.EvidenceJSON, rawEvidenceSHA256(in.EvidenceJSON)
	want.UpdatedAt, want.Version = now, winner.Version+1
	if err != nil || result.Created || !reflect.DeepEqual(result.Operation, want) || repo.lookups != 2 || repo.creationCalls != 1 || repo.mutationCalls != 1 {
		t.Fatalf("collision winner: result=%+v err=%v lookups=%d creations=%d mutations=%d", result, err, repo.lookups, repo.creationCalls, repo.mutationCalls)
	}
	ingestAtomicAssertNoSplit(t, repo.ingestAtomicLegacyProbe)
	newOps, newEvents := ingestAtomicSnapshot(base)
	if len(newOps) != len(ops) || len(newEvents) != len(events)+1 || !reflect.DeepEqual(newOps[winner.ID], want) || !reflect.DeepEqual(newOps[other.ID], ops[other.ID]) {
		t.Fatalf("collision refresh escaped scope or missed audit: operations=%+v events=%+v", newOps, newEvents)
	}
	last := newEvents[len(newEvents)-1]
	if last.OperationID != winner.ID || last.EventType != "source_evidence_refreshed" {
		t.Fatalf("collision refresh audit mismatch: %+v", last)
	}
}

func TestAtomicIngestCollisionCannotRefreshClaimedWinner(t *testing.T) {
	base := NewMemoryRepository()
	winner := mutationSeed(t, base)
	repo := &ingestAtomicProbe{ingestAtomicLegacyProbe: &ingestAtomicLegacyProbe{Repository: base}, updatePair: base.UpdateWithEvent}
	repo.find = func(owner, workspace, key string) (*models.Operation, bool, error) {
		if repo.lookups == 1 {
			return nil, false, nil
		}
		return base.FindByDedupeKey(owner, workspace, key)
	}
	var acquired *ClaimedOperation
	repo.createPair = func(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
		var err error
		acquired, err = base.ClaimOperation(context.Background(), winner.OwnerUserID, winner.WorkspaceID, winner.ID, uuid.New(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return base.CreateWithEvent(op, event)
	}
	ops, events := ingestAtomicSnapshot(base)
	in := sampleInput()
	in.EvidenceJSON = `{"mustNotRefresh":true}`
	result, err := NewService(repo).Ingest(in)
	if !errors.Is(err, ErrOperationClaimed) || result.Created || result.Operation.ID != uuid.Nil || repo.lookups != 2 || repo.creationCalls != 1 || repo.mutationCalls != 1 {
		t.Fatalf("creation race silently refreshed/accepted claimed evidence: result=%+v err=%v", result, err)
	}
	ingestAtomicAssertNoSplit(t, repo.ingestAtomicLegacyProbe)
	ingestAtomicAssertSnapshot(t, base, ops, events)
	if acquired == nil || base.RenewClaim(context.Background(), acquired.Claim, time.Minute) != nil {
		t.Fatal("refused collision refresh changed the winning lease")
	}
}

func TestAtomicIngestInvalidInputRejectedBeforeLookupOrMutation(t *testing.T) {
	for _, field := range []string{"owner", "title", "operation_type", "source_type", "dedupe", "evidence_json"} {
		t.Run(field, func(t *testing.T) {
			base := NewMemoryRepository()
			mutationSeed(t, base)
			ops, events := ingestAtomicSnapshot(base)
			repo := &ingestAtomicProbe{ingestAtomicLegacyProbe: &ingestAtomicLegacyProbe{Repository: base}, createPair: base.CreateWithEvent, updatePair: base.UpdateWithEvent}
			in := sampleInput()
			in.EvidenceJSON = `{"mustNotRefresh":true}`
			switch field {
			case "owner":
				in.OwnerUserID = " \t"
			case "title":
				in.Title = " \t"
			case "operation_type":
				in.OperationType = " \t"
			case "source_type":
				in.SourceType = " \t"
			case "dedupe":
				in.DedupeKey = " \t"
			case "evidence_json":
				in.EvidenceJSON = `{"unfinished":`
			}
			result, err := NewService(repo).Ingest(in)
			if err == nil || result.Created || result.Operation.ID != uuid.Nil || repo.lookups != 0 || repo.creationCalls != 0 || repo.mutationCalls != 0 {
				t.Fatalf("invalid input reached persistence: result=%+v err=%v lookups=%d creations=%d mutations=%d", result, err, repo.lookups, repo.creationCalls, repo.mutationCalls)
			}
			ingestAtomicAssertNoSplit(t, repo.ingestAtomicLegacyProbe)
			ingestAtomicAssertSnapshot(t, base, ops, events)
		})
	}
}

func TestAtomicIngestEvidenceRefreshAuditedAndPreservesStoredState(t *testing.T) {
	base := NewMemoryRepository()
	before := mutationSeed(t, base)
	approvalID := uuid.New()
	before.Status = string(StatusAwaitingApproval)
	before.RiskLevel = string(RiskHigh)
	before.AutonomyLevel = string(AutonomyApproval)
	before.OwnerType = string(OwnerRobert)
	before.CurrentDecision = string(DecisionAskRobert)
	before.RequiresApproval = true
	before.ApprovalID = &approvalID
	before.WorldModelStateJSON = `{"beforeEffect":false,"outcomeUncertain":true}`
	before.EvidenceJSON = `{"revision":1}`
	before.SourceEvidenceRawSHA256 = rawEvidenceSHA256(before.EvidenceJSON)
	before.ResultSummary = "stored review decision"
	before.VerificationStatus = string(VerificationFailed)
	base.mu.Lock()
	base.ops[before.ID] = before
	base.mu.Unlock()
	now := before.UpdatedAt.Add(time.Minute)
	repo := &ingestAtomicProbe{ingestAtomicLegacyProbe: &ingestAtomicLegacyProbe{Repository: base}, createPair: base.CreateWithEvent, updatePair: base.UpdateWithEvent}
	svc := NewService(repo)
	svc.now = func() time.Time { return now }
	in := sampleInput()
	in.Title, in.Description, in.SourceType = "incoming replacement title", "incoming description", "trello"
	in.SourceRevisionHash = "incoming revision"
	in.EvidenceJSON = `{"revision":2}`
	result, err := svc.Ingest(in)
	want := before
	want.EvidenceJSON, want.SourceEvidenceRawSHA256 = in.EvidenceJSON, rawEvidenceSHA256(in.EvidenceJSON)
	want.UpdatedAt, want.Version = now, before.Version+1
	if err != nil || result.Created || !reflect.DeepEqual(result.Operation, want) || repo.creationCalls != 0 || repo.mutationCalls != 1 {
		t.Fatalf("evidence refresh changed stored state or bypassed Save: result=%+v err=%v", result, err)
	}
	ops, events := ingestAtomicSnapshot(base)
	if len(ops) != 1 || !reflect.DeepEqual(ops[before.ID], want) || len(events) != 1 {
		t.Fatalf("refresh storage: operations=%+v events=%+v", ops, events)
	}
	audit := events[0]
	if audit.ID == uuid.Nil || audit.OperationID != before.ID || audit.EventType != "source_evidence_refreshed" ||
		audit.ActorType != string(OwnerHAI) || audit.AfterStatus != before.Status || audit.PayloadJSON != "{}" || !audit.CreatedAt.Equal(now) {
		t.Fatalf("refresh audit mismatch: %+v", audit)
	}
	ingestAtomicAssertNoSplit(t, repo.ingestAtomicLegacyProbe)
}

func TestAtomicIngestEvidenceRefreshRefusalLeavesNoChanges(t *testing.T) {
	for _, refusal := range []string{"claimed", "stale", "repository_error", "duplicate_event_id", "unsupported"} {
		t.Run(refusal, func(t *testing.T) {
			base := NewMemoryRepository()
			before := mutationSeed(t, base)
			legacy := &ingestAtomicLegacyProbe{Repository: base}
			probe := &ingestAtomicProbe{ingestAtomicLegacyProbe: legacy, createPair: base.CreateWithEvent, updatePair: base.UpdateWithEvent}
			var repo Repository = probe
			var want error
			switch refusal {
			case "claimed":
				if _, err := base.ClaimOperation(context.Background(), before.OwnerUserID, before.WorkspaceID, before.ID, uuid.New(), time.Minute); err != nil {
					t.Fatal(err)
				}
				want = ErrOperationClaimed
			case "stale":
				stale := before
				winning := before
				winning.Version++
				winning.ResultSummary = "winning concurrent update"
				if _, err := base.UpdateWithEvent(&winning, &models.OperationEvent{OperationID: before.ID, EventType: "winner", AfterStatus: before.Status, PayloadJSON: "{}"}); err != nil {
					t.Fatal(err)
				}
				legacy.find = func(string, string, string) (*models.Operation, bool, error) {
					copyOp := stale
					return &copyOp, true, nil
				}
				want = ErrStaleOperation
			case "repository_error":
				want = errors.New("refresh audit unavailable")
				probe.updatePair = func(*models.Operation, *models.OperationEvent) (*models.Operation, error) { return nil, want }
			case "duplicate_event_id":
				eventID := uuid.New()
				if err := base.AppendEvent(&models.OperationEvent{ID: eventID, OperationID: before.ID, EventType: "existing_audit"}); err != nil {
					t.Fatal(err)
				}
				probe.updatePair = func(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
					audit := *event
					audit.ID = eventID
					return base.UpdateWithEvent(op, &audit)
				}
			case "unsupported":
				repo = legacy
				want = ErrAtomicMutationsUnsupported
			}
			ops, events := ingestAtomicSnapshot(base)
			in := sampleInput()
			in.EvidenceJSON = `{"mustNotPersist":true}`
			result, err := NewService(repo).Ingest(in)
			wantMutationCalls := 1
			if refusal == "unsupported" {
				wantMutationCalls = 0
			}
			if err == nil || (want != nil && !errors.Is(err, want)) || result.Created || result.Operation.ID != uuid.Nil ||
				probe.creationCalls != 0 || probe.mutationCalls != wantMutationCalls {
				t.Fatalf("refresh refusal: result=%+v err=%v want=%v creations=%d mutations=%d", result, err, want, probe.creationCalls, probe.mutationCalls)
			}
			ingestAtomicAssertNoSplit(t, legacy)
			ingestAtomicAssertSnapshot(t, base, ops, events)
		})
	}
}

func TestAtomicIngestMemoryMalformedCreationPairsNoWrites(t *testing.T) {
	for _, malformed := range []string{"nil_operation", "nil_operation_id", "zero_version", "later_version", "non_new_status", "nil_event", "nil_event_operation_id", "wrong_operation", "wrong_event_type", "wrong_actor", "before_status", "wrong_after_status", "invalid_evidence_json", "empty_evidence_json", "invalid_payload_json", "empty_payload_json", "blank_operation_type", "blank_source_type"} {
		t.Run(malformed, func(t *testing.T) {
			repo := NewMemoryRepository()
			op, event := ingestAtomicPair(t)
			operation, audit := &op, &event
			switch malformed {
			case "nil_operation":
				operation = nil
			case "nil_operation_id":
				op.ID, event.OperationID = uuid.Nil, uuid.Nil
			case "zero_version":
				op.Version = 0
			case "later_version":
				op.Version = 2
			case "non_new_status":
				op.Status, event.AfterStatus = string(StatusReady), string(StatusReady)
			case "nil_event":
				audit = nil
			case "nil_event_operation_id":
				event.OperationID = uuid.Nil
			case "wrong_operation":
				event.OperationID = uuid.New()
			case "wrong_event_type":
				event.EventType = "source_evidence_refreshed"
			case "wrong_actor":
				event.ActorType = string(OwnerRobert)
			case "before_status":
				event.BeforeStatus = string(StatusNew)
			case "wrong_after_status":
				event.AfterStatus = string(StatusReady)
			case "invalid_evidence_json":
				op.EvidenceJSON = `{"unfinished":`
			case "empty_evidence_json":
				op.EvidenceJSON = ""
			case "invalid_payload_json":
				event.PayloadJSON = `{"unfinished":`
			case "empty_payload_json":
				event.PayloadJSON = ""
			case "blank_operation_type":
				op.OperationType = " \t"
			case "blank_source_type":
				op.SourceType = " \t"
			}
			beforeOp, beforeEvent := op, event
			ops, events := ingestAtomicSnapshot(repo)
			created, err := repo.CreateWithEvent(operation, audit)
			if created != nil || !errors.Is(err, ErrInvalidCreationEvent) {
				t.Fatalf("malformed creation accepted: operation=%+v err=%v", created, err)
			}
			if !reflect.DeepEqual(op, beforeOp) || !reflect.DeepEqual(event, beforeEvent) {
				t.Fatal("refused creation mutated caller inputs")
			}
			ingestAtomicAssertSnapshot(t, repo, ops, events)
		})
	}
}

func TestAtomicIngestMemoryDuplicateIdentitiesNoWrites(t *testing.T) {
	for _, collision := range []string{"operation_id", "event_id", "active_dedupe"} {
		t.Run(collision, func(t *testing.T) {
			repo := NewMemoryRepository()
			stored, storedEvent := ingestAtomicPair(t)
			storedEvent.ID = uuid.New()
			if _, err := repo.CreateWithEvent(&stored, &storedEvent); err != nil {
				t.Fatal(err)
			}
			op, event := ingestAtomicPair(t)
			op.DedupeKey = "distinct-candidate"
			var want error
			switch collision {
			case "operation_id":
				op.ID, event.OperationID = stored.ID, stored.ID
			case "event_id":
				event.ID = storedEvent.ID
			case "active_dedupe":
				op.DedupeKey = stored.DedupeKey
				want = ErrDuplicateDedupeKey
			}
			beforeOp, beforeEvent := op, event
			ops, events := ingestAtomicSnapshot(repo)
			created, err := repo.CreateWithEvent(&op, &event)
			if created != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("duplicate identity accepted: operation=%+v err=%v", created, err)
			}
			if !reflect.DeepEqual(op, beforeOp) || !reflect.DeepEqual(event, beforeEvent) {
				t.Fatal("duplicate refusal mutated caller inputs")
			}
			ingestAtomicAssertSnapshot(t, repo, ops, events)
		})
	}
}
