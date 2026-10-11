package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type trelloSliceFixtureRepo struct {
	*fakeSourceRepo
	state   *models.TrelloSyncState
	receipt *models.TrelloWebhookReceipt
}

const trelloTestCanonicalBoardID = "0123456789abcdef01234567"

func TestBeginTrelloSyncGenerationResetsPerCycleProgress(t *testing.T) {
	lastSuccessful := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	previousActivity := lastSuccessful.Add(-time.Hour)
	state := models.TrelloSyncState{
		Generation: 4, Phase: trelloPhaseIdle, CardCursor: "old-card", ActionCursor: "old-action",
		PagesProcessed: 18, RecordsProcessed: 742, LastSuccessfulAt: &lastSuccessful,
		MaxCardActivityAt: &previousActivity,
	}
	now := time.Date(2026, 9, 25, 13, 30, 0, 0, time.FixedZone("CEST", 2*60*60))

	beginTrelloSyncGeneration(&state, now)

	if state.Generation != 5 || state.Phase != trelloPhaseIncrementalActions {
		t.Fatalf("generation state = (%d, %q), want (5, %q)", state.Generation, state.Phase, trelloPhaseIncrementalActions)
	}
	if state.PagesProcessed != 0 || state.RecordsProcessed != 0 {
		t.Fatalf("new generation progress = (%d pages, %d records), want zeroed per-cycle progress", state.PagesProcessed, state.RecordsProcessed)
	}
	if state.CardCursor != "" || state.ActionCursor != "" || state.MaxCardActivityAt != nil {
		t.Fatalf("new generation retained old page state: %+v", state)
	}
	if state.LastSuccessfulAt == nil || !state.LastSuccessfulAt.Equal(lastSuccessful) {
		t.Fatalf("new generation lost last successful timestamp: %v", state.LastSuccessfulAt)
	}
	wantActionSince := lastSuccessful.Add(-trelloActionOverlap)
	if state.ActionSince == nil || !state.ActionSince.Equal(wantActionSince) {
		t.Fatalf("action overlap = %v, want %v", state.ActionSince, wantActionSince)
	}
	if !state.CycleStartedAt.Equal(now.UTC()) {
		t.Fatalf("cycle started at %v, want UTC-normalized %v", state.CycleStartedAt, now.UTC())
	}
}

func (r *trelloSliceFixtureRepo) PrepareTrelloSyncState(source *models.ConnectedSource, job *models.SourceSyncJob, now time.Time) (*models.TrelloSyncState, error) {
	if r.state == nil {
		boardID, err := trelloBoardID(source.SyncTarget)
		if err != nil {
			return nil, err
		}
		r.state = &models.TrelloSyncState{
			SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: boardID,
			LogicalJobID: trelloUUIDPointer(job.ID), Generation: 1, Phase: trelloPhaseBackfillCards,
			CycleStartedAt: now.UTC(),
		}
	}
	if r.state.OwnerIdentity != source.OwnerIdentity {
		return nil, ErrTrelloSyncBindingChanged
	}
	copyState := *r.state
	return &copyState, nil
}

func (r *trelloSliceFixtureRepo) FindTrelloSyncState(uuid.UUID) (*models.TrelloSyncState, error) {
	if r.state == nil {
		return nil, gorm.ErrRecordNotFound
	}
	copyState := *r.state
	return &copyState, nil
}

func (r *trelloSliceFixtureRepo) FindTrelloWebhookReceipt(sourceID, receiptID uuid.UUID) (*models.TrelloWebhookReceipt, error) {
	if r.receipt == nil || r.receipt.SourceID != sourceID || r.receipt.ID != receiptID {
		return nil, gorm.ErrRecordNotFound
	}
	copyReceipt := *r.receipt
	return &copyReceipt, nil
}

func (*trelloSliceFixtureRepo) QueueTrelloWebhookReceipt(*models.ConnectedSource, *models.TrelloWebhookReceipt, *models.DurableJob) (bool, error) {
	return false, errors.New("not used by Trello page fixture")
}

func (*trelloSliceFixtureRepo) ClaimTrelloWebhookSync(uuid.UUID, uuid.UUID, string, time.Time) (*trelloWebhookSyncClaim, error) {
	return nil, errors.New("not used by Trello page fixture")
}

func (*trelloSliceFixtureRepo) BindTrelloWebhookSyncJob(uuid.UUID, int64, int, uuid.UUID, time.Time) error {
	return errors.New("not used by Trello page fixture")
}

func (*trelloSliceFixtureRepo) FailTrelloWebhookSyncAttempt(uuid.UUID, int64, int, int, time.Time) (bool, error) {
	return false, errors.New("not used by Trello page fixture")
}

func (*trelloSliceFixtureRepo) CompleteTrelloWebhookSyncBatch(uuid.UUID, int64, int, uuid.UUID, int, time.Time) (trelloWebhookSyncCompletion, error) {
	return trelloWebhookSyncCompletion{}, errors.New("not used by Trello page fixture")
}

func (*trelloSliceFixtureRepo) CompleteTrelloWebhookReceipt(uuid.UUID, uuid.UUID, string, time.Time) error {
	return errors.New("not used by Trello page fixture")
}

func (r *trelloSliceFixtureRepo) FindTrelloStaleCardsAfter(sourceID uuid.UUID, cycleStartedAt time.Time, afterCardID string, limit int) ([]models.SourceRawItem, error) {
	items, err := r.fakeSourceRepo.FindRawItems(sourceID)
	if err != nil {
		return nil, err
	}
	cards := make([]models.SourceRawItem, 0, len(items))
	for _, item := range items {
		cardID := strings.TrimPrefix(item.ExternalID, trelloCardExternalIDPrefix)
		if item.ItemType == "trello_card" && strings.HasPrefix(item.ExternalID, trelloCardExternalIDPrefix) && cardID > afterCardID && item.FetchedAt.Before(cycleStartedAt) {
			cards = append(cards, item)
		}
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].ExternalID < cards[j].ExternalID })
	if len(cards) > limit {
		cards = cards[:limit]
	}
	return cards, nil
}

func (r *trelloSliceFixtureRepo) StartTrelloSyncJob(uuid.UUID, uuid.UUID, time.Time) (*models.SourceSyncJob, error) {
	return nil, fmt.Errorf("not used by Trello page fixture")
}

func (r *trelloSliceFixtureRepo) CommitTrelloSyncPage(*models.ConnectedSource, *models.SourceSyncJob, *models.TrelloSyncState, *models.TrelloSyncState, models.TrelloSyncPage, []models.TrelloActionReceipt, []string, []string, bool) (*models.ConnectedSource, *models.SourceSyncJob, *models.TrelloSyncState, error) {
	return nil, nil, nil, fmt.Errorf("not used by Trello page fixture")
}

func newTrelloDurablePageServer(t *testing.T, cardPage func(*http.Request) []trelloCard, actionPage func(*http.Request) []trelloAction, cardLookups ...func(string) (trelloCard, int)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Trello method=%s, want GET-only read connector", r.Method)
			http.Error(w, "read only", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Query().Has("key") || r.URL.Query().Has("token") {
			t.Errorf("Trello credentials leaked into query: %s", r.URL.RawQuery)
			http.Error(w, "credentials in query", http.StatusBadRequest)
			return
		}
		if got, want := r.Header.Get("Authorization"), trelloAuthorizationHeader("test-key", "test-read-token"); got != want {
			t.Errorf("Trello Authorization=%q, want configured read credentials", got)
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[{"id":"list-1","name":"Doing"}]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			page := cardPage(r)
			for index := range page {
				if strings.TrimSpace(page[index].IDBoard) == "" {
					page[index].IDBoard = trelloTestCanonicalBoardID
				}
			}
			cards, _ := json.Marshal(page)
			_, _ = w.Write(cards)
		case strings.HasPrefix(r.URL.Path, "/1/cards/"):
			id := strings.TrimPrefix(r.URL.Path, "/1/cards/")
			if len(cardLookups) == 0 {
				http.NotFound(w, r)
				return
			}
			card, status := cardLookups[0](id)
			if status < http.StatusOK || status >= http.StatusMultipleChoices {
				http.Error(w, "provider lookup failed", status)
				return
			}
			_ = json.NewEncoder(w).Encode(card)
		case strings.HasSuffix(r.URL.Path, "/actions"):
			actions, _ := json.Marshal(actionPage(r))
			_, _ = w.Write(actions)
		default:
			_, _ = w.Write([]byte(`{"id":"0123456789abcdef01234567","name":"HAI board","url":"https://trello.com/b/abc123XY","shortUrl":"https://trello.com/b/abc123XY"}`))
		}
	}))
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Cleanup(server.Close)
	configureTrelloTest(t, server.URL)
	return server
}

func TestTrelloDurableSyncRejectsMismatchedBoardMetadataBeforeReadingLists(t *testing.T) {
	var downstreamReads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case r.URL.Path == "/1/boards/abc123XY":
			_, _ = w.Write([]byte(`{"id":"0123456789abcdef01234567","name":"Unexpected board","shortUrl":"https://trello.com/b/def456GH/unexpected-board"}`))
		default:
			downstreamReads++
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	service := NewService(repo, nil).(*service)
	if _, err := service.prepareTrelloSyncSlice(context.Background(), source, job); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("prepareTrelloSyncSlice error = %v, want mismatched board metadata rejection", err)
	}
	if downstreamReads != 0 {
		t.Fatalf("requests after mismatched board metadata = %d, want none", downstreamReads)
	}
}

func TestTrelloDurableRejectsInvalidIDOnShortInitialCardPageBeforeImport(t *testing.T) {
	newTrelloDurablePageServer(t,
		func(*http.Request) []trelloCard {
			return []trelloCard{{ID: "not-a-mongo-id", Name: "Malformed card", DateLastActivity: "2026-09-20T12:00:00Z", IDList: "list-1"}}
		},
		func(*http.Request) []trelloAction { return nil },
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	svc := NewService(repo, nil).(*service)

	slice, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
	if err == nil || !strings.Contains(err.Error(), "invalid ID") {
		t.Fatalf("prepareTrelloSyncSlice error = %v, want malformed card ID rejection", err)
	}
	if slice != nil {
		t.Fatalf("malformed card page returned an importable slice: %#v", slice)
	}
	if len(repo.rawItems) != 0 || len(repo.extractions) != 0 {
		t.Fatalf("malformed page caused source writes: raw=%d extractions=%d", len(repo.rawItems), len(repo.extractions))
	}
}

func TestTrelloDurableRejectsCardFromDifferentBoardBeforeImport(t *testing.T) {
	newTrelloDurablePageServer(t,
		func(*http.Request) []trelloCard {
			return []trelloCard{{
				ID: testTrelloMongoID(42), IDBoard: "fedcba9876543210fedcba98",
				Name: "Card from another board", DateLastActivity: "2026-09-20T12:00:00Z",
			}}
		},
		func(*http.Request) []trelloAction { return nil },
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	svc := NewService(repo, nil).(*service)

	slice, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "board") {
		t.Fatalf("prepareTrelloSyncSlice error = %v, want mismatched card board rejection", err)
	}
	if slice != nil {
		t.Fatalf("mismatched-board response returned an importable slice: %#v", slice)
	}
	if len(repo.rawItems) != 0 || len(repo.extractions) != 0 {
		t.Fatalf("mismatched-board page caused source writes: raw=%d extractions=%d", len(repo.rawItems), len(repo.extractions))
	}
}

type trelloCountingReader struct {
	remaining int
	read      int
}

func (r *trelloCountingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for index := 0; index < n; index++ {
		p[index] = 'x'
	}
	r.remaining -= n
	r.read += n
	return n, nil
}

func TestTrelloPageResponseReadStopsAtRemainingAggregateBudgetPlusOne(t *testing.T) {
	const remaining = int64(7)
	budget := &trelloSyncBudget{pageBytes: trelloMaxPageBytesPerSync - remaining}
	limit, aggregateRemaining := trelloPageResponseReadLimit(20<<20, budget)
	reader := &trelloCountingReader{remaining: 20 << 20}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		t.Fatal(err)
	}
	if limit != remaining || aggregateRemaining != remaining {
		t.Fatalf("read limits = (%d, %d), want remaining aggregate budget %d", limit, aggregateRemaining, remaining)
	}
	if reader.read != int(remaining+1) || len(body) != int(remaining+1) {
		t.Fatalf("consumed %d bytes and retained %d; want only remaining budget plus one byte", reader.read, len(body))
	}
}

func TestTrelloDurableSlicesKeepCardAndActionCursorsIndependent(t *testing.T) {
	cardID := testTrelloMongoID(200)
	actionID := testTrelloMongoID(300)
	server := newTrelloDurablePageServer(t,
		func(r *http.Request) []trelloCard {
			if r.URL.Query().Get("before") != "" {
				return nil
			}
			return []trelloCard{{ID: cardID, Name: "Plan", DateLastActivity: "2026-09-20T12:00:00Z", IDList: "list-1", ShortURL: "https://trello.com/c/card200"}}
		},
		func(*http.Request) []trelloAction {
			return []trelloAction{{ID: actionID, Type: "commentCard", Date: "2026-09-20T12:30:00Z", IDMemberCreator: "member-1", MemberCreator: trelloMember{FullName: "Robert"}, Data: trelloActionData{Text: "Review the evidence", Card: trelloActionCard{ID: cardID, Name: "Plan", ShortLink: "card200"}}}}
		},
	)
	parsed, _ := url.Parse(server.URL)
	if parsed.Hostname() == "" {
		t.Fatal("fake Trello endpoint did not start")
	}
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	svc := NewService(repo, nil).(*service)

	cards, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare card slice: %v", err)
	}
	if len(cards.Items) != 1 || cards.Items[0].ExternalID != "trello:card:"+cardID {
		t.Fatalf("card page items = %#v", cards.Items)
	}
	if cards.Next.Phase != trelloPhaseBackfillActions || cards.Next.CardCursor != "" || cards.Next.ActionCursor != "" {
		t.Fatalf("card page state = phase %q card=%q action=%q", cards.Next.Phase, cards.Next.CardCursor, cards.Next.ActionCursor)
	}
	if cards.FinalCursor != "" || cards.Complete {
		t.Fatal("card-page completion advanced the public cursor before action and downstream work")
	}
	repo.state = &cards.Next

	actions, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare action slice: %v", err)
	}
	if len(actions.Items) != 1 || actions.Items[0].ExternalID != "trello:action:"+actionID || len(actions.Receipts) != 1 {
		t.Fatalf("action page items=%#v receipts=%#v", actions.Items, actions.Receipts)
	}
	if actions.Next.Phase != trelloPhaseCatchUpActions || actions.Next.CardCursor != "" || actions.Next.ActionCursor != "" {
		t.Fatalf("action page state = phase %q card=%q action=%q", actions.Next.Phase, actions.Next.CardCursor, actions.Next.ActionCursor)
	}
	if !strings.Contains(actions.Items[0].Metadata, "commentEditsAndDeletes=webhook_required") {
		t.Fatalf("comment limitations are not explicit in source metadata: %q", actions.Items[0].Metadata)
	}
}

func TestTrelloDurableCardPageUsesShortLinkForCanonicalEvidenceFallback(t *testing.T) {
	cardID := testTrelloMongoID(201)
	memberID := testTrelloMongoID(202)
	shortLink := "a1b2c3d4"
	var requestedFields string
	var requestedMembers, requestedMemberFields string
	newTrelloDurablePageServer(t,
		func(r *http.Request) []trelloCard {
			requestedFields = r.URL.Query().Get("fields")
			requestedMembers = r.URL.Query().Get("members")
			requestedMemberFields = r.URL.Query().Get("member_fields")
			return []trelloCard{{
				ID: cardID, ShortLink: shortLink, Name: "Evidence card",
				DateLastActivity: "2026-09-20T12:00:00Z", IDList: "list-1", IDMembers: []string{memberID},
				Members: []trelloMember{{ID: memberID, FullName: "Assigned Owner", Username: "owner"}},
			}}
		},
		func(*http.Request) []trelloAction { return nil },
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	svc := NewService(repo, nil).(*service)

	slice, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare Trello card slice: %v", err)
	}
	if !strings.Contains(requestedFields, "shortLink") {
		t.Fatalf("requested card fields = %q, want shortLink for provenance fallback", requestedFields)
	}
	if requestedMembers != "true" || requestedMemberFields != "fullName,username" {
		t.Fatalf("member expansion = members:%q member_fields:%q; want names in durable card pages", requestedMembers, requestedMemberFields)
	}
	if len(slice.Items) != 1 {
		t.Fatalf("card items = %d, want one", len(slice.Items))
	}
	if got, want := slice.Items[0].SourceURI, "https://trello.com/c/"+shortLink; got != want {
		t.Fatalf("card SourceURI = %q, want canonical short-link URL %q", got, want)
	}
	if !strings.Contains(slice.Items[0].Content, "Assigned members: Assigned Owner (@owner)") {
		t.Fatalf("card content = %q, want resolved assigned member name", slice.Items[0].Content)
	}

	missingLink := trelloImportItem(trelloCard{ID: cardID, Name: "Incomplete provider record"}, "Board", "Inbox", "")
	if missingLink.SourceURI != "" {
		t.Fatalf("card with only an internal Mongo ID has SourceURI %q; want no fabricated public card link", missingLink.SourceURI)
	}
}

func TestTrelloIncrementalInventoryUsesSupportedIDPagination(t *testing.T) {
	cycleStarted := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cardID := testTrelloMongoID(200)
	var cardQuery url.Values
	newTrelloDurablePageServer(t,
		func(r *http.Request) []trelloCard {
			cardQuery = r.URL.Query()
			return []trelloCard{{ID: cardID, Name: "Changed during backfill", DateLastActivity: "2026-09-20T12:30:00Z", IDList: "list-1"}}
		},
		func(*http.Request) []trelloAction { return nil },
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	repo.state = &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
		LogicalJobID: trelloUUIDPointer(job.ID), Generation: 1, Phase: trelloPhaseIncrementalCards,
		CycleStartedAt: cycleStarted,
	}
	svc := NewService(repo, nil).(*service)
	if _, err := svc.prepareTrelloSyncSlice(context.Background(), source, job); err != nil {
		t.Fatalf("prepare first-cycle card catch-up: %v", err)
	}
	if cardQuery.Has("modifiedSince") || cardQuery.Get("sort") != "-id" || cardQuery.Get("limit") != strconv.Itoa(trelloCardPageSize) {
		t.Fatalf("card inventory query = %v; want bounded ID pagination without unsupported modifiedSince", cardQuery)
	}
}

func TestTrelloActionSliceAdvancesIndependentActionCursor(t *testing.T) {
	cardID := testTrelloMongoID(400)
	actions := make([]trelloAction, trelloActionPageSize)
	for index := range actions {
		actions[index] = trelloAction{
			ID: testTrelloMongoID(20_000 - index), Type: "commentCard", Date: "2026-09-20T12:30:00Z",
			Data: trelloActionData{Text: fmt.Sprintf("Comment %d", index), Card: trelloActionCard{ID: cardID}},
		}
	}
	newTrelloDurablePageServer(t, func(*http.Request) []trelloCard { return nil }, func(*http.Request) []trelloAction { return actions })
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	cardCursor := testTrelloMongoID(900)
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source), state: &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
		LogicalJobID: trelloUUIDPointer(job.ID), Generation: 2, Phase: trelloPhaseBackfillActions,
		CardCursor: cardCursor, CycleStartedAt: time.Now().UTC(),
	}}
	svc := NewService(repo, nil).(*service)
	slice, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare action page: %v", err)
	}
	wantActionCursor := actions[len(actions)-1].ID
	if slice.Next.CardCursor != cardCursor || slice.Next.ActionCursor != wantActionCursor || slice.Next.Phase != trelloPhaseBackfillActions {
		t.Fatalf("action page cursors: card=%q action=%q phase=%q", slice.Next.CardCursor, slice.Next.ActionCursor, slice.Next.Phase)
	}
	if slice.Page.CursorBefore != "" || slice.Page.CursorAfter != wantActionCursor {
		t.Fatalf("action page ledger cursors: before=%q after=%q", slice.Page.CursorBefore, slice.Page.CursorAfter)
	}
}

func TestTrelloSyncPhaseTransitionsAreMonotonicAndBounded(t *testing.T) {
	valid := [][2]string{
		{trelloPhaseBackfillCards, trelloPhaseBackfillCards},
		{trelloPhaseBackfillCards, trelloPhaseBackfillActions},
		{trelloPhaseBackfillActions, trelloPhaseCatchUpActions},
		{trelloPhaseCatchUpActions, trelloPhaseIncrementalCards},
		{trelloPhaseIncrementalActions, trelloPhaseIncrementalCards},
		{trelloPhaseIncrementalCards, trelloPhaseReconcileInventory},
		{trelloPhaseReconcileInventory, trelloPhaseReconcileInventory},
		{trelloPhaseReconcileInventory, trelloPhaseIdle},
	}
	for _, transition := range valid {
		if !validTrelloSyncTransition(transition[0], transition[1]) {
			t.Errorf("transition %q -> %q was rejected", transition[0], transition[1])
		}
	}
	invalid := [][2]string{
		{trelloPhaseBackfillCards, trelloPhaseIdle},
		{trelloPhaseBackfillActions, trelloPhaseIncrementalCards},
		{trelloPhaseCatchUpActions, trelloPhaseBackfillCards},
		{trelloPhaseIncrementalCards, trelloPhaseBackfillActions},
		{trelloPhaseIncrementalCards, trelloPhaseIdle},
		{trelloPhaseReconcileInventory, trelloPhaseIncrementalCards},
		{trelloPhaseIdle, trelloPhaseBackfillCards},
	}
	for _, transition := range invalid {
		if validTrelloSyncTransition(transition[0], transition[1]) {
			t.Errorf("transition %q -> %q was allowed", transition[0], transition[1])
		}
	}
}

func TestTrelloSeenCardIDsMatchDurablePageContract(t *testing.T) {
	valid := []string{"trello:card:" + testTrelloMongoID(201), "trello:card:" + testTrelloMongoID(202)}
	if err := validateTrelloSeenCardIDs(trelloPhaseIncrementalCards, 2, valid); err != nil {
		t.Fatalf("valid card inventory: %v", err)
	}
	if err := validateTrelloSeenCardIDs(trelloPhaseBackfillCards, 1, valid); err == nil {
		t.Fatal("card inventory with a mismatched provider-record count was accepted")
	}
	if err := validateTrelloSeenCardIDs(trelloPhaseIncrementalCards, 2, []string{valid[0], valid[0]}); err == nil {
		t.Fatal("duplicate card identity was accepted")
	}
	if err := validateTrelloSeenCardIDs(trelloPhaseIncrementalActions, 0, valid); err == nil {
		t.Fatal("action page accepted card inventory identities")
	}
	if err := validateTrelloSeenCardIDs(trelloPhaseCatchUpActions, 0, nil); err != nil {
		t.Fatalf("empty action-page inventory: %v", err)
	}
	if err := validateTrelloVerifiedBoardCardIDs(trelloPhaseReconcileInventory, nil, valid[1:]); err != nil {
		t.Fatalf("verified board card identity: %v", err)
	}
	if err := validateTrelloVerifiedBoardCardIDs(trelloPhaseBackfillCards, nil, valid[:1]); err == nil {
		t.Fatal("card backfill accepted direct inventory identities")
	}
	if err := validateTrelloVerifiedBoardCardIDs(trelloPhaseIncrementalCards, nil, valid[:1]); err == nil {
		t.Fatal("provider card page accepted direct inventory identities")
	}
	if err := validateTrelloVerifiedBoardCardIDs(trelloPhaseReconcileInventory, valid[:1], valid[:1]); err == nil {
		t.Fatal("duplicate provider-page and direct-verified card identity was accepted")
	}
}

func TestTerminalTrelloSyncJobCanReleaseCheckpointForAuthorizedResume(t *testing.T) {
	completedAt := time.Now().UTC()
	tests := []struct {
		name string
		job  *models.SourceSyncJob
		want bool
	}{
		{name: "failed terminal job", job: &models.SourceSyncJob{Status: "failed", CompletedAt: &completedAt}, want: true},
		{name: "cancelled terminal job", job: &models.SourceSyncJob{Status: "cancelled", CompletedAt: &completedAt}, want: true},
		{name: "active job", job: &models.SourceSyncJob{Status: "running"}, want: false},
		{name: "failure without terminal timestamp", job: &models.SourceSyncJob{Status: "failed"}, want: false},
		{name: "partial failure remains retryable", job: &models.SourceSyncJob{Status: "partial_failure", CompletedAt: &completedAt}, want: false},
		{name: "completed job cannot be rebound", job: &models.SourceSyncJob{Status: "completed", CompletedAt: &completedAt}, want: false},
		{name: "missing job", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := terminalTrelloSyncJob(test.job); got != test.want {
				t.Fatalf("terminalTrelloSyncJob() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTrelloDurableSlicesKeepExistingWorkerCaps(t *testing.T) {
	if trelloMaxRecordsPerSync != 10_000 || trelloMaxPageBytesPerSync != 16<<20 || trelloMaxRequestsPerSync != 90 {
		t.Fatalf("worker caps changed: records=%d bytes=%d requests=%d", trelloMaxRecordsPerSync, trelloMaxPageBytesPerSync, trelloMaxRequestsPerSync)
	}
}

func TestTrelloUnchangedItemSkipsExtractionAndRefreshesMetadata(t *testing.T) {
	source := newTrelloSource(uuid.New(), "abc123XY", "project-a")
	item := ImportItem{
		ExternalID: "trello:card:" + testTrelloMongoID(201), Title: "Stable title", Content: "Stable body",
		SourceURI: "https://trello.com/c/card201", ItemType: "trello_card", ProjectKey: "project-a", Metadata: "dateLastActivity=old",
	}
	repo := newFakeSourceRepo(source)
	oldFetch := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	_, err := repo.SaveRawItem(&models.SourceRawItem{
		ID: uuid.New(), SourceID: source.ID, ExternalID: item.ExternalID, Title: item.Title,
		Content: item.Content, ContentHash: hashText(item.Title + "|" + item.Content), SourceURI: item.SourceURI,
		ItemType: item.ItemType, ProjectKey: item.ProjectKey, Metadata: "dateLastActivity=previous", FetchedAt: oldFetch,
	})
	if err != nil {
		t.Fatalf("save processed Trello item: %v", err)
	}
	svc := NewService(repo, nil).(*service)
	unchanged, err := svc.trelloItemAlreadyProcessed(source, item)
	if err != nil || !unchanged {
		t.Fatalf("unchanged item = %v, %v; want true, nil", unchanged, err)
	}
	stored, err := repo.FindRawItem(source.ID, item.ExternalID)
	if err != nil {
		t.Fatalf("reload metadata checkpoint: %v", err)
	}
	if stored.Metadata != item.Metadata || stored.ContentHash != hashText(item.Title+"|"+item.Content) || !stored.FetchedAt.Equal(oldFetch) {
		t.Fatalf("stored Trello checkpoint = %#v; metadata should refresh without prematurely marking the page processed", stored)
	}
	item.Content = "Changed body"
	unchanged, err = svc.trelloItemAlreadyProcessed(source, item)
	if err != nil || unchanged {
		t.Fatalf("changed item = %v, %v; want false, nil", unchanged, err)
	}
}

func TestTrelloFinalDurableCardPageResumesInventoryReconciliation(t *testing.T) {
	cycleStarted := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	presentID := testTrelloMongoID(202)
	lookedUp := make(map[string]bool)
	newTrelloDurablePageServer(t,
		func(*http.Request) []trelloCard {
			return []trelloCard{{ID: presentID, Name: "Still on board", DateLastActivity: "2026-09-25T11:00:00Z", IDList: "list-1", ShortURL: "https://trello.com/c/present202"}}
		},
		func(*http.Request) []trelloAction { return nil },
		func(id string) (trelloCard, int) {
			lookedUp[id] = true
			switch id {
			case testTrelloMongoID(201):
				return trelloCard{}, http.StatusNotFound
			case testTrelloMongoID(204):
				return trelloCard{ID: id, IDBoard: trelloTestCanonicalBoardID}, http.StatusOK
			case testTrelloMongoID(205):
				return trelloCard{ID: id, IDBoard: "fedcba9876543210fedcba98"}, http.StatusOK
			default:
				return trelloCard{}, http.StatusNotFound
			}
		},
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "project-a")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source), state: &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
		LogicalJobID: trelloUUIDPointer(job.ID), Generation: 2, Phase: trelloPhaseIncrementalCards,
		CycleStartedAt: cycleStarted,
	}}
	for _, raw := range []models.SourceRawItem{
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:card:" + testTrelloMongoID(201), ItemType: "trello_card", Title: "Removed card", SourceURI: "https://trello.com/c/removed201", ProjectKey: "project-a", FetchedAt: cycleStarted.Add(-time.Hour)},
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:card:" + presentID, ItemType: "trello_card", Title: "Still on board", SourceURI: "https://trello.com/c/present202", ProjectKey: "project-a", FetchedAt: cycleStarted.Add(-time.Hour)},
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:card:" + testTrelloMongoID(204), ItemType: "trello_card", Title: "Moved back onto board", ProjectKey: "project-a", FetchedAt: cycleStarted.Add(-time.Hour)},
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:card:" + testTrelloMongoID(205), ItemType: "trello_card", Title: "Moved to another board", ProjectKey: "project-a", FetchedAt: cycleStarted.Add(-time.Hour)},
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:card:" + testTrelloMongoID(203), ItemType: trelloClosedCardItemType, Title: "Already closed", FetchedAt: cycleStarted.Add(-time.Hour)},
	} {
		if _, err := repo.SaveRawItem(&raw); err != nil {
			t.Fatalf("seed existing Trello card: %v", err)
		}
	}
	svc := NewService(repo, nil).(*service)
	cardSlice, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare final card page: %v", err)
	}
	if cardSlice.Complete || cardSlice.Next.Phase != trelloPhaseReconcileInventory {
		t.Fatalf("final incremental card page = phase %q complete=%v; want a durable inventory checkpoint", cardSlice.Next.Phase, cardSlice.Complete)
	}
	if len(cardSlice.SeenCardExternalIDs) != 1 || cardSlice.SeenCardExternalIDs[0] != "trello:card:"+presentID {
		t.Fatalf("provider-observed card identities = %#v, want only the current provider page", cardSlice.SeenCardExternalIDs)
	}
	if len(lookedUp) != 0 {
		t.Fatalf("inventory candidates were verified before the durable reconciliation phase: %#v", lookedUp)
	}
	for _, externalID := range cardSlice.SeenCardExternalIDs {
		raw, err := repo.FindRawItem(source.ID, externalID)
		if err != nil {
			t.Fatalf("find observed card %q: %v", externalID, err)
		}
		raw.FetchedAt = cycleStarted.Add(time.Minute)
		if _, err := repo.SaveRawItem(raw); err != nil {
			t.Fatalf("mark observed card %q as fetched in the committed page: %v", externalID, err)
		}
	}
	repo.state = &cardSlice.Next
	slice, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare resumable inventory reconciliation: %v", err)
	}
	if !slice.Complete || slice.Next.Phase != trelloPhaseIdle {
		t.Fatalf("final inventory reconciliation = phase %q complete=%v; want completed idle state", slice.Next.Phase, slice.Complete)
	}
	if len(slice.VerifiedBoardCardExternalIDs) != 1 || slice.VerifiedBoardCardExternalIDs[0] != "trello:card:"+testTrelloMongoID(204) {
		t.Fatalf("directly verified board identities = %#v, want the card missed due to ID pagination", slice.VerifiedBoardCardExternalIDs)
	}
	var unavailable []ImportItem
	for _, item := range slice.Items {
		if item.ItemType == trelloUnavailableCardItemType {
			unavailable = append(unavailable, item)
		}
	}
	if len(unavailable) != 2 || unavailable[0].ExternalID != "trello:card:"+testTrelloMongoID(201) || unavailable[1].ExternalID != "trello:card:"+testTrelloMongoID(205) {
		t.Fatalf("unavailable cards = %#v, want only confirmed absent or moved cards", unavailable)
	}
	if !strings.Contains(unavailable[0].Metadata, "content_retained=true") {
		t.Fatalf("missing-card state does not preserve source content: %q", unavailable[0].Metadata)
	}
	if slice.Next.RecordsProcessed-cardSlice.Next.RecordsProcessed != int64(slice.Page.RecordCount) {
		t.Fatalf("processed record delta=%d, inventory page record count=%d", slice.Next.RecordsProcessed-cardSlice.Next.RecordsProcessed, slice.Page.RecordCount)
	}
	if !lookedUp[testTrelloMongoID(201)] || !lookedUp[testTrelloMongoID(204)] || !lookedUp[testTrelloMongoID(205)] {
		t.Fatalf("candidate lookup results = %#v; every stale active card must be individually checked", lookedUp)
	}
	if slice.Page.RequestCount > trelloMaxRequestsPerSync || slice.Page.ResponseBytes > trelloMaxPageBytesPerSync {
		t.Fatalf("final reconciliation exceeded durable page budget: requests=%d bytes=%d", slice.Page.RequestCount, slice.Page.ResponseBytes)
	}
}

func TestTrelloInventoryReconciliationFailsClosedOnTransientCardLookupError(t *testing.T) {
	cycleStarted := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	missingID := testTrelloMongoID(214)
	newTrelloDurablePageServer(t,
		func(*http.Request) []trelloCard {
			return []trelloCard{{ID: testTrelloMongoID(202), Name: "Still on board", DateLastActivity: "2026-09-25T11:00:00Z", IDList: "list-1"}}
		},
		func(*http.Request) []trelloAction { return nil },
		func(id string) (trelloCard, int) {
			if id == missingID {
				return trelloCard{}, http.StatusInternalServerError
			}
			return trelloCard{}, http.StatusNotFound
		},
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "project-a")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source), state: &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
		LogicalJobID: trelloUUIDPointer(job.ID), Generation: 2, Phase: trelloPhaseReconcileInventory,
		CycleStartedAt: cycleStarted,
	}}
	if _, err := repo.SaveRawItem(&models.SourceRawItem{
		ID: uuid.New(), SourceID: source.ID, ExternalID: trelloCardExternalIDPrefix + missingID,
		ItemType: "trello_card", Title: "Maybe still on board", FetchedAt: cycleStarted.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed stale card: %v", err)
	}
	_, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || providerErr.upstreamStatus != http.StatusInternalServerError {
		t.Fatalf("transient card verification error = %v, want fail-closed upstream 500", err)
	}
}

func TestTrelloInventoryReconciliationResumesAcrossRequestBudget(t *testing.T) {
	cycleStarted := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	var lookupCount int
	newTrelloDurablePageServer(t,
		func(*http.Request) []trelloCard {
			return []trelloCard{{ID: testTrelloMongoID(202), Name: "Still on board", DateLastActivity: "2026-09-25T11:00:00Z", IDList: "list-1"}}
		},
		func(*http.Request) []trelloAction { return nil },
		func(id string) (trelloCard, int) {
			lookupCount++
			return trelloCard{ID: id, IDBoard: trelloTestCanonicalBoardID}, http.StatusOK
		},
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "project-a")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source), state: &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
		LogicalJobID: trelloUUIDPointer(job.ID), Generation: 2, Phase: trelloPhaseReconcileInventory,
		CycleStartedAt: cycleStarted,
	}}
	for index := 1; index <= trelloMaxRequestsPerSync; index++ {
		id := testTrelloMongoID(index)
		if _, err := repo.SaveRawItem(&models.SourceRawItem{
			ID: uuid.New(), SourceID: source.ID, ExternalID: trelloCardExternalIDPrefix + id,
			ItemType: "trello_card", Title: "Historical card", FetchedAt: cycleStarted.Add(-time.Hour),
		}); err != nil {
			t.Fatalf("seed stale card %d: %v", index, err)
		}
	}
	slice, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare budget-limited final inventory: %v", err)
	}
	if slice.Page.RequestCount != trelloMaxRequestsPerSync || lookupCount != trelloMaxRequestsPerSync-3 {
		t.Fatalf("bounded request totals: page=%d lookups=%d; want %d total and remaining capacity only", slice.Page.RequestCount, lookupCount, trelloMaxRequestsPerSync-3)
	}
	if slice.Complete || slice.Next.Phase != trelloPhaseReconcileInventory || slice.Next.CardCursor != testTrelloMongoID(trelloMaxRequestsPerSync-3) {
		t.Fatalf("first budgeted reconciliation state = phase %q complete=%v cursor=%q", slice.Next.Phase, slice.Complete, slice.Next.CardCursor)
	}
	if len(slice.InventoryCheckedCardIDs) != lookupCount || len(slice.VerifiedBoardCardExternalIDs) != lookupCount {
		t.Fatalf("first-slice reconciled/verified counts = %d/%d, want %d", len(slice.InventoryCheckedCardIDs), len(slice.VerifiedBoardCardExternalIDs), lookupCount)
	}
	for _, externalID := range slice.VerifiedBoardCardExternalIDs {
		raw, err := repo.FindRawItem(source.ID, externalID)
		if err != nil {
			t.Fatalf("find verified card %q: %v", externalID, err)
		}
		raw.FetchedAt = cycleStarted.Add(time.Minute)
		if _, err := repo.SaveRawItem(raw); err != nil {
			t.Fatalf("persist verified card %q for next-slice fixture: %v", externalID, err)
		}
	}
	repo.state = &slice.Next
	lookupCount = 0
	lastSlice, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("resume budgeted inventory reconciliation: %v", err)
	}
	if !lastSlice.Complete || lastSlice.Next.Phase != trelloPhaseIdle || lookupCount != 3 || len(lastSlice.InventoryCheckedCardIDs) != 3 {
		t.Fatalf("final reconciliation slice = complete:%v phase:%q lookups:%d checked:%d", lastSlice.Complete, lastSlice.Next.Phase, lookupCount, len(lastSlice.InventoryCheckedCardIDs))
	}
}

func TestTrelloEmptyInventoryPageHasResponseEvidenceForCheckpoint(t *testing.T) {
	newTrelloDurablePageServer(t,
		func(*http.Request) []trelloCard { return nil },
		func(*http.Request) []trelloAction { return nil },
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "project-a")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	state := &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
		LogicalJobID: trelloUUIDPointer(job.ID), Generation: 1, Phase: trelloPhaseReconcileInventory,
		CycleStartedAt: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source), state: state}
	slice, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare empty inventory reconciliation: %v", err)
	}
	if !slice.Complete || slice.Page.RecordCount != 0 || slice.Page.RequestCount == 0 || slice.Page.ResponseBytes == 0 {
		t.Fatalf("empty inventory evidence = complete:%v records:%d requests:%d bytes:%d; want completed page with measured API responses",
			slice.Complete, slice.Page.RecordCount, slice.Page.RequestCount, slice.Page.ResponseBytes)
	}
	if err := validateTrelloPageCursorProgress(&slice.Current, &slice.Next, slice.Page, nil, nil, true, "completed"); err != nil {
		t.Fatalf("valid empty inventory checkpoint rejected: %v", err)
	}
}

func TestTrelloDurableSlicesProgressBeyondPerWorkerRecordCap(t *testing.T) {
	const totalCards = trelloMaxRecordsPerSync + 1000
	server := newTrelloDurablePageServer(t,
		func(r *http.Request) []trelloCard {
			start := totalCards + 1000
			if before := r.URL.Query().Get("before"); before != "" {
				parsed, err := strconv.ParseInt(before, 16, 64)
				if err != nil {
					t.Errorf("invalid before cursor %q: %v", before, err)
					return nil
				}
				start = int(parsed) - 1
			}
			cards := make([]trelloCard, 0, trelloCardPageSize)
			for len(cards) < trelloCardPageSize && start > 1000 {
				cards = append(cards, trelloCard{ID: testTrelloMongoID(start), Name: "Card", DateLastActivity: "2026-09-20T12:00:00Z", IDList: "list-1"})
				start--
			}
			return cards
		},
		func(*http.Request) []trelloAction { return nil },
	)
	if server == nil {
		t.Fatal("fake Trello server is nil")
	}
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	svc := NewService(repo, nil).(*service)
	state := &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
		LogicalJobID: trelloUUIDPointer(job.ID), Generation: 1, Phase: trelloPhaseBackfillCards,
		CycleStartedAt: time.Now().UTC(),
	}
	var processed int
	for page := 0; page < 11; page++ {
		repo.state = state
		slice, err := svc.prepareTrelloSyncSlice(context.Background(), source, job)
		if err != nil {
			t.Fatalf("slice %d: %v", page, err)
		}
		if slice.Page.RecordCount > trelloMaxRecordsPerSync || slice.Page.ResponseBytes > trelloMaxPageBytesPerSync || slice.Page.RequestCount > trelloMaxRequestsPerSync {
			t.Fatalf("slice %d exceeded a fixed worker cap: records=%d bytes=%d requests=%d", page, slice.Page.RecordCount, slice.Page.ResponseBytes, slice.Page.RequestCount)
		}
		if len(slice.Items) != trelloCardPageSize || slice.Next.CardCursor == state.CardCursor {
			t.Fatalf("slice %d did not advance one full page: items=%d before=%q after=%q", page, len(slice.Items), state.CardCursor, slice.Next.CardCursor)
		}
		processed += len(slice.Items)
		state = &slice.Next
	}
	if processed != totalCards+0 || state.PagesProcessed != 11 || state.Phase != trelloPhaseBackfillCards {
		t.Fatalf("multi-slice backfill processed=%d pages=%d phase=%q", processed, state.PagesProcessed, state.Phase)
	}
	if state.LogicalJobID == nil || *state.LogicalJobID != job.ID {
		t.Fatal("logical job identity was not retained across slices")
	}
}

func TestManualSyncProgressProjectionExposesPhaseAndCounts(t *testing.T) {
	job := &models.SourceSyncJob{
		ID: uuid.New(), SourceID: uuid.New(), Mode: ModeManualAsyncSync, Status: "running",
		ProgressPhase: trelloPhaseBackfillActions, ProgressPages: 12, ProgressRecords: 12000,
		ProgressMessage: trelloSyncCapabilityLimitations,
	}
	durable := &models.DurableJob{Status: models.DurableJobRunning, MaxAttempts: 5}
	view := manualSyncView(job, durable)
	if view.ProgressPhase != trelloPhaseBackfillActions || view.ProgressPages != 12 || view.ProgressRecords != 12000 {
		t.Fatalf("public progress projection = %#v", view)
	}
	if !strings.Contains(view.ProgressMessage, "eventually consistent") || !strings.Contains(view.ProgressMessage, "webhooks") || !strings.Contains(view.ProgressMessage, "not downloaded") {
		t.Fatalf("Trello limitations were not surfaced: %q", view.ProgressMessage)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal progress API view: %v", err)
	}
	if strings.Contains(string(encoded), "CardCursor") || strings.Contains(string(encoded), "ActionCursor") || strings.Contains(string(encoded), "ownerIdentity") {
		t.Fatalf("public progress exposed internal cursor or owner binding: %s", encoded)
	}
}

func TestSourceSyncJobProgressFieldsSerializeForSyncStatusAPI(t *testing.T) {
	job := models.SourceSyncJob{
		ProgressPhase: trelloPhaseBackfillActions, ProgressPages: 8, ProgressRecords: 8000,
		ProgressMessage: trelloSyncCapabilityLimitations,
	}
	encoded, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal source sync job: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode source sync job: %v", err)
	}
	for key, want := range map[string]any{
		"progressPhase": trelloPhaseBackfillActions, "progressPages": float64(8),
		"progressRecords": float64(8000), "progressMessage": trelloSyncCapabilityLimitations,
	} {
		if payload[key] != want {
			t.Errorf("source sync API %s = %#v, want %#v", key, payload[key], want)
		}
	}
}
