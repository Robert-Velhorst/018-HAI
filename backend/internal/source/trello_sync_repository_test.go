package source

import (
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func trelloCursorContractFixture(phase, before string, recordCount int) (models.TrelloSyncState, models.TrelloSyncState, models.TrelloSyncPage) {
	sourceID := uuid.New()
	jobID := uuid.New()
	current := models.TrelloSyncState{
		SourceID: sourceID, OwnerIdentity: "alice", BoardID: "abc123XY",
		LogicalJobID: trelloUUIDPointer(jobID), Generation: 4, Phase: phase,
		CycleStartedAt: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC),
		PagesProcessed: 7, RecordsProcessed: 25,
	}
	if strings.Contains(phase, "action") {
		current.ActionCursor = before
	} else {
		current.CardCursor = before
	}
	next := current
	next.PagesProcessed++
	next.RecordsProcessed += int64(recordCount)
	page := models.TrelloSyncPage{
		SourceID: sourceID, LogicalJobID: jobID, Generation: current.Generation,
		Phase: phase, CursorBefore: before, RecordCount: recordCount,
		RequestCount: 1, ResponseBytes: 64, Fingerprint: strings.Repeat("a", 64),
	}
	return current, next, page
}

func trelloCardExternalIDs(values ...int) []string {
	ids := make([]string, len(values))
	for index, value := range values {
		ids[index] = trelloCardExternalIDPrefix + testTrelloMongoID(value)
	}
	return ids
}

func trelloActionReceipts(sourceID uuid.UUID, values ...int) []models.TrelloActionReceipt {
	receipts := make([]models.TrelloActionReceipt, len(values))
	for index, value := range values {
		receipts[index] = models.TrelloActionReceipt{
			SourceID: sourceID, ActionID: testTrelloMongoID(value), CardID: testTrelloMongoID(100),
			OccurredAt: time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC), Fingerprint: strings.Repeat("b", 64),
		}
	}
	return receipts
}

func TestValidateTrelloPageCursorProgressCardsRequireOrderedIdentityAndForwardCursor(t *testing.T) {
	before := testTrelloMongoID(1000)
	current, next, page := trelloCursorContractFixture(trelloPhaseBackfillCards, before, 2)
	next.Phase = trelloPhaseBackfillActions
	next.CardCursor = ""
	page.CursorAfter = testTrelloMongoID(998)
	ids := trelloCardExternalIDs(999, 998)
	if err := validateTrelloPageCursorProgress(&current, &next, page, nil, ids, false, "running"); err != nil {
		t.Fatalf("valid short backfill page rejected: %v", err)
	}

	tests := []struct {
		name string
		page models.TrelloSyncPage
		ids  []string
		next models.TrelloSyncState
	}{
		{name: "wrong final cursor", page: func() models.TrelloSyncPage { p := page; p.CursorAfter = testTrelloMongoID(997); return p }(), ids: ids, next: next},
		{name: "non-descending identities", page: page, ids: trelloCardExternalIDs(999, 1001), next: next},
		{name: "does not advance before cursor", page: func() models.TrelloSyncPage { p := page; p.CursorAfter = before; return p }(), ids: trelloCardExternalIDs(1000, 999), next: next},
		{name: "phase not advanced on short page", page: page, ids: ids, next: current},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTrelloPageCursorProgress(&current, &test.next, test.page, nil, test.ids, false, "running"); err == nil {
				t.Fatal("invalid card page was accepted")
			}
		})
	}
}

func TestValidateTrelloPageCursorProgressFullCardPageRequiresStrictAdvance(t *testing.T) {
	before := testTrelloMongoID(5000)
	current, next, page := trelloCursorContractFixture(trelloPhaseIncrementalCards, before, trelloCardPageSize)
	ids := make([]string, trelloCardPageSize)
	for index := range ids {
		ids[index] = trelloCardExternalIDPrefix + testTrelloMongoID(4999-index)
	}
	page.CursorAfter = strings.TrimPrefix(ids[len(ids)-1], trelloCardExternalIDPrefix)
	next.CardCursor = page.CursorAfter
	if err := validateTrelloPageCursorProgress(&current, &next, page, nil, ids, false, "running"); err != nil {
		t.Fatalf("valid full card page rejected: %v", err)
	}

	next.CardCursor = before
	page.CursorAfter = before
	ids[len(ids)-1] = trelloCardExternalIDPrefix + before
	if err := validateTrelloPageCursorProgress(&current, &next, page, nil, ids, false, "running"); err == nil {
		t.Fatal("full card page that did not advance was accepted")
	}
}

func TestValidateTrelloPageCursorProgressActionsBindReceiptsAndBoundary(t *testing.T) {
	before := testTrelloMongoID(1000)
	current, next, page := trelloCursorContractFixture(trelloPhaseIncrementalActions, before, 3)
	next.Phase = trelloPhaseIncrementalCards
	next.ActionCursor = ""
	receipts := trelloActionReceipts(current.SourceID, 998, 997)
	page.CursorAfter = testTrelloMongoID(997)
	if err := validateTrelloPageCursorProgress(&current, &next, page, receipts, nil, false, "running"); err != nil {
		t.Fatalf("valid action page with one inclusive boundary rejected: %v", err)
	}

	badDuplicate := append(append([]models.TrelloActionReceipt(nil), receipts...), receipts[1])
	if err := validateTrelloPageCursorProgress(&current, &next, page, badDuplicate, nil, false, "running"); err == nil {
		t.Fatal("duplicate action receipt was accepted")
	}
	badFingerprint := append([]models.TrelloActionReceipt(nil), receipts...)
	badFingerprint[0].Fingerprint = "not-a-sha256"
	if err := validateTrelloPageCursorProgress(&current, &next, page, badFingerprint, nil, false, "running"); err == nil {
		t.Fatal("malformed action receipt fingerprint was accepted")
	}
	page.CursorAfter = testTrelloMongoID(996)
	if err := validateTrelloPageCursorProgress(&current, &next, page, receipts, nil, false, "running"); err == nil {
		t.Fatal("action cursor not bound to final receipt was accepted")
	}
}

func TestValidateTrelloPageCursorProgressInventoryMonotonicityAndCompletion(t *testing.T) {
	before := testTrelloMongoID(200)
	current, next, page := trelloCursorContractFixture(trelloPhaseReconcileInventory, before, 2)
	page.CursorAfter = testTrelloMongoID(202)
	next.CardCursor = page.CursorAfter
	if err := validateTrelloPageCursorProgress(&current, &next, page, nil, nil, false, "running"); err != nil {
		t.Fatalf("valid ascending inventory checkpoint rejected: %v", err)
	}

	page.CursorAfter = testTrelloMongoID(199)
	next.CardCursor = page.CursorAfter
	if err := validateTrelloPageCursorProgress(&current, &next, page, nil, nil, false, "running"); err == nil {
		t.Fatal("backward inventory cursor was accepted")
	}

	current, next, page = trelloCursorContractFixture(trelloPhaseReconcileInventory, before, 1)
	page.CursorAfter = testTrelloMongoID(201)
	next.Phase = trelloPhaseIdle
	next.CardCursor = ""
	next.LogicalJobID = nil
	if err := validateTrelloPageCursorProgress(&current, &next, page, nil, nil, true, "completed"); err != nil {
		t.Fatalf("valid completed inventory checkpoint rejected: %v", err)
	}
	if err := validateTrelloPageCursorProgress(&current, &next, page, nil, nil, true, "running"); err == nil {
		t.Fatal("completion with a nonterminal job status was accepted")
	}
}

func TestTrelloSourceCanSyncRequiresEnabledUnrevokedAndConnectedState(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		source *models.ConnectedSource
		want   bool
	}{
		{name: "active", source: &models.ConnectedSource{Enabled: true, Status: "active"}, want: true},
		{name: "paused", source: &models.ConnectedSource{Enabled: true, Status: "paused"}},
		{name: "revoked status", source: &models.ConnectedSource{Enabled: true, Status: "revoked"}},
		{name: "reconnect required", source: &models.ConnectedSource{Enabled: true, Status: "reconnect_required"}},
		{name: "disabled", source: &models.ConnectedSource{Enabled: false, Status: "active"}},
		{name: "revocation timestamp", source: &models.ConnectedSource{Enabled: true, Status: "active", RevokedAt: &now}},
		{name: "nil source", source: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := trelloSourceCanSync(test.source); got != test.want {
				t.Fatalf("trelloSourceCanSync() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTrelloResumeJobIDForOwnerRequiresAQueuedOwnerBoundManualJob(t *testing.T) {
	sourceID := uuid.New()
	previousID := uuid.New()
	replacementID := uuid.New()
	completedAt := time.Now().UTC()
	state := models.TrelloSyncState{
		SourceID: sourceID, OwnerIdentity: "alice", Phase: trelloPhaseBackfillCards,
		LogicalJobID: trelloUUIDPointer(previousID),
	}
	previous := models.SourceSyncJob{
		ID: previousID, SourceID: sourceID, OwnerIdentity: "alice",
		Mode: ModeManualAsyncSync, Status: "cancelled", CompletedAt: &completedAt,
	}
	ownerQueuedJob := models.SourceSyncJob{
		ID: replacementID, SourceID: sourceID, OwnerIdentity: "alice",
		Mode: ModeManualAsyncSync, Status: "queued",
	}
	tests := []struct {
		name       string
		previous   models.SourceSyncJob
		candidates []models.SourceSyncJob
		wantID     uuid.UUID
		wantErr    error
	}{
		{name: "owner queued request takes over terminal checkpoint", previous: previous, candidates: []models.SourceSyncJob{ownerQueuedJob}, wantID: replacementID},
		{name: "owner running request takes over terminal checkpoint", previous: previous, candidates: []models.SourceSyncJob{{ID: replacementID, SourceID: sourceID, OwnerIdentity: "alice", Mode: ModeManualAsyncSync, Status: "running"}}, wantID: replacementID},
		{name: "without an explicit active request keep terminal checkpoint bound", previous: previous, wantID: previousID},
		{name: "another owner cannot take over", previous: previous, candidates: []models.SourceSyncJob{{ID: replacementID, SourceID: sourceID, OwnerIdentity: "bob", Mode: ModeManualAsyncSync, Status: "queued"}}, wantID: previousID},
		{name: "another source cannot take over", previous: previous, candidates: []models.SourceSyncJob{{ID: replacementID, SourceID: uuid.New(), OwnerIdentity: "alice", Mode: ModeManualAsyncSync, Status: "queued"}}, wantID: previousID},
		{name: "scheduled job cannot take over a cancelled manual checkpoint", previous: previous, candidates: []models.SourceSyncJob{{ID: replacementID, SourceID: sourceID, OwnerIdentity: "alice", Mode: ModeScheduledSync, Status: "queued"}}, wantID: previousID},
		{name: "active checkpoint cannot be preempted", previous: models.SourceSyncJob{ID: previousID, SourceID: sourceID, OwnerIdentity: "alice", Mode: ModeManualAsyncSync, Status: "running"}, candidates: []models.SourceSyncJob{ownerQueuedJob}, wantID: previousID},
		{name: "ambiguous active requests fail closed", previous: previous, candidates: []models.SourceSyncJob{ownerQueuedJob, {ID: uuid.New(), SourceID: sourceID, OwnerIdentity: "alice", Mode: ModeManualAsyncSync, Status: "queued"}}, wantErr: ErrTrelloSyncAlreadyActive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotID, err := trelloResumeJobIDForOwner(&state, &test.previous, test.candidates)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("resume job selection error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resume job selection: %v", err)
			}
			if gotID == nil || *gotID != test.wantID {
				t.Fatalf("resume job ID = %v, want %s", gotID, test.wantID)
			}
		})
	}
}
