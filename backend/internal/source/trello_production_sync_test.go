package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestTrelloProductionSyncActionWatermarkCoversLongRunningCycle(t *testing.T) {
	cycleStarted := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	completed := cycleStarted.Add(72 * time.Hour)
	nextStarted := completed.Add(time.Hour)
	for _, test := range []struct {
		name     string
		previous time.Time
		want     time.Time
	}{
		{name: "long running cycle", previous: cycleStarted, want: cycleStarted.Add(-trelloActionOverlap)},
		{name: "legacy missing cycle start", want: completed.Add(-trelloActionOverlap)},
		{name: "inconsistent later cycle start", previous: completed.Add(time.Minute), want: completed.Add(-trelloActionOverlap)},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := models.TrelloSyncState{Generation: 4, Phase: trelloPhaseIdle, CycleStartedAt: test.previous, LastSuccessfulAt: &completed}
			beginTrelloSyncGeneration(&state, nextStarted)
			if state.ActionSince == nil || !state.ActionSince.Equal(test.want) {
				t.Fatalf("action replay starts at %v, want %v", state.ActionSince, test.want)
			}
			if !state.CycleStartedAt.Equal(nextStarted) || !state.LastSuccessfulAt.Equal(completed) {
				t.Fatal("generation reset changed completion evidence or lost the new cycle start")
			}
		})
	}
}

func TestTrelloProductionSyncCanonicalCardCheckpoint(t *testing.T) {
	cardID := testTrelloMongoID(0xabc)
	newTrelloDurablePageServer(t,
		func(*http.Request) []trelloCard {
			return []trelloCard{{ID: " " + strings.ToUpper(cardID) + " ", IDBoard: strings.ToUpper(trelloTestCanonicalBoardID), Name: "Card", DateLastActivity: "2026-09-25T11:00:00Z"}}
		},
		func(*http.Request) []trelloAction { return []trelloAction{} },
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	slice, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare canonical card page: %v", err)
	}
	if len(slice.Items) != 1 || slice.Items[0].ExternalID != trelloCardExternalIDPrefix+cardID ||
		len(slice.SeenCardExternalIDs) != 1 || slice.SeenCardExternalIDs[0] != slice.Items[0].ExternalID || slice.Page.CursorAfter != cardID {
		t.Fatal("imported card, observed identity, and page boundary are not canonical and identical")
	}
	if err := validateTrelloPageCursorProgress(&slice.Current, &slice.Next, slice.Page, slice.Receipts, slice.SeenCardExternalIDs, false, job.Status); err != nil {
		t.Fatalf("prepared card page cannot be checkpointed: %v", err)
	}
}

func TestTrelloProductionSyncCanonicalActionCardReceipt(t *testing.T) {
	cardID := testTrelloMongoID(0xabc)
	actionID := testTrelloMongoID(0xdef)
	newTrelloDurablePageServer(t,
		func(*http.Request) []trelloCard { return []trelloCard{} },
		func(*http.Request) []trelloAction {
			return []trelloAction{{ID: strings.ToUpper(actionID), Type: "commentCard", Date: "2026-09-25T11:00:00Z", Data: trelloActionData{Text: "Evidence", Card: trelloActionCard{ID: " " + strings.ToUpper(cardID) + " "}}}}
		},
	)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source), state: &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY", LogicalJobID: trelloUUIDPointer(job.ID),
		Generation: 1, Phase: trelloPhaseBackfillActions, CycleStartedAt: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	}}
	slice, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("prepare canonical action page: %v", err)
	}
	if len(slice.Receipts) != 1 || slice.Receipts[0].ActionID != actionID || slice.Receipts[0].CardID != cardID ||
		len(slice.Items) != 1 || !strings.Contains(slice.Items[0].Metadata, ";card="+cardID+";") {
		t.Fatal("action receipt and import metadata did not normalize the provider card identity")
	}
	if err := validateTrelloPageCursorProgress(&slice.Current, &slice.Next, slice.Page, slice.Receipts, nil, false, job.Status); err != nil {
		t.Fatalf("prepared action page cannot be checkpointed: %v", err)
	}
}

func TestTrelloProductionSyncInventoryCheckpointsBeforeRetryRequestCap(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("provider method = %s, want read-only GET", r.Method)
			http.Error(w, "read only", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_ = json.NewEncoder(w).Encode(trelloTokenInfo{IDMember: testTrelloAccountMemberID, Permissions: []trelloTokenPermission{{ModelType: "Board", Read: true}}})
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_ = json.NewEncoder(w).Encode([]trelloList{})
		case strings.HasPrefix(r.URL.Path, "/1/cards/"):
			if attempts.Add(1)%2 == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_ = json.NewEncoder(w).Encode(trelloCard{ID: strings.TrimPrefix(r.URL.Path, "/1/cards/"), IDBoard: trelloTestCanonicalBoardID})
		default:
			_ = json.NewEncoder(w).Encode(trelloBoard{ID: trelloTestCanonicalBoardID, ShortURL: "https://trello.com/b/abc123XY"})
		}
	}))
	t.Cleanup(server.Close)
	configureTrelloTest(t, server.URL)
	cycleStarted := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source), state: &models.TrelloSyncState{
		SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY", LogicalJobID: trelloUUIDPointer(job.ID),
		Generation: 1, Phase: trelloPhaseReconcileInventory, CycleStartedAt: cycleStarted,
	}}
	for index := 1; index <= trelloMaxRequestsPerSync; index++ {
		if _, err := repo.SaveRawItem(&models.SourceRawItem{
			ID: uuid.New(), SourceID: source.ID, ExternalID: trelloCardExternalIDPrefix + testTrelloMongoID(index),
			ItemType: "trello_card", FetchedAt: cycleStarted.Add(-time.Hour),
		}); err != nil {
			t.Fatalf("seed stale card: %v", err)
		}
	}
	slice, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("request retries discarded verified inventory progress: %v", err)
	}
	wantVerified := (trelloMaxRequestsPerSync - 3) / 2
	if slice.Complete || slice.Page.RequestCount != trelloMaxRequestsPerSync || len(slice.InventoryCheckedCardIDs) != wantVerified ||
		len(slice.VerifiedBoardCardExternalIDs) != wantVerified || slice.Next.CardCursor != testTrelloMongoID(wantVerified) || len(slice.Items) != 0 {
		t.Fatalf("inventory page did not stop at its last verified card: complete=%v requests=%d checked=%d verified=%d cursor=%q",
			slice.Complete, slice.Page.RequestCount, len(slice.InventoryCheckedCardIDs), len(slice.VerifiedBoardCardExternalIDs), slice.Next.CardCursor)
	}
	if err := validateTrelloPageCursorProgress(&slice.Current, &slice.Next, slice.Page, nil, nil, false, job.Status); err != nil {
		t.Fatalf("bounded inventory progress cannot be checkpointed: %v", err)
	}
	repo.state = &slice.Next
	attempts.Store(0)
	resumed, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	if err != nil {
		t.Fatalf("resume inventory after retries: %v", err)
	}
	if len(resumed.InventoryCheckedCardIDs) == 0 || resumed.InventoryCheckedCardIDs[0] != testTrelloMongoID(wantVerified+1) || resumed.Next.CardCursor <= slice.Next.CardCursor {
		t.Fatal("resumed inventory replayed completed cards or skipped the unverified card")
	}
}

func TestTrelloProductionSyncWebhookRefreshBindsVerifiedShortLink(t *testing.T) {
	const cardID = "dddddddddddddddddddddddd"
	for _, test := range []struct {
		name      string
		shortLink string
		wantError bool
	}{
		{name: "verified configured short link", shortLink: "abc123XY"},
		{name: "different provider short link", shortLink: "abc123XZ", wantError: true},
		{name: "short links remain case sensitive", shortLink: "abc123xy", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("provider method = %s, want GET", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				var value any
				switch {
				case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
					value = trelloTokenInfo{IDMember: testTrelloAccountMemberID, Permissions: []trelloTokenPermission{{ModelType: "Board", Read: true}}}
				case r.URL.Path == "/1/boards/abc123XY":
					value = trelloBoard{ID: trelloTestCanonicalBoardID, ShortURL: "https://trello.com/b/" + test.shortLink}
				case r.URL.Path == "/1/cards/"+cardID:
					value = trelloCard{ID: cardID, IDBoard: trelloTestCanonicalBoardID, IDList: testTrelloMongoID(1), DateLastActivity: "2026-09-25T11:00:00Z"}
				case r.URL.Path == "/1/lists/"+testTrelloMongoID(1):
					value = trelloList{ID: testTrelloMongoID(1), Name: "Doing"}
				case r.URL.Path == "/1/cards/"+cardID+"/actions":
					value = []trelloAction{}
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(value)
			}))
			t.Cleanup(server.Close)
			configureTrelloTest(t, server.URL)
			source := newTrelloSource(uuid.New(), "abc123XY", "")
			repo := newFakeSourceRepo(source)
			receipt := &models.TrelloWebhookReceipt{ID: uuid.New(), SourceID: source.ID, BoardID: "abc123XY", ActionID: testTrelloMongoID(2), CardID: cardID, ActionType: "deleteComment"}
			refresh, err := NewService(repo, nil).(*service).fetchTrelloWebhookCommentRefresh(context.Background(), source, receipt)
			if test.wantError {
				if err == nil || len(refresh.Items) != 0 {
					t.Fatal("mismatched board alias produced comment refresh items")
				}
				return
			}
			if err != nil || refresh.CardID != cardID || len(refresh.Items) != 1 {
				t.Fatalf("verified short-link board cannot refresh comments: card=%q items=%d err=%v", refresh.CardID, len(refresh.Items), err)
			}
		})
	}
}

func TestTrelloProductionSyncWebhookCreatedCommentCanBeArchived(t *testing.T) {
	const (
		cardID        = "dddddddddddddddddddddddd"
		otherCardID   = "eeeeeeeeeeeeeeeeeeeeeeee"
		commentID     = "222222222222222222222222"
		remainingID   = "111111111111111111111111"
		otherActionID = "333333333333333333333333"
	)
	for _, test := range []struct {
		name      string
		legacy    bool
		verified  bool
		wrongID   bool
		wantError bool
	}{
		{name: "canonical creation metadata", verified: true},
		{name: "legacy verified creation metadata", legacy: true, verified: true},
		{name: "unverified legacy metadata rejected", legacy: true, wantError: true},
		{name: "conflicting legacy identity rejected", legacy: true, verified: true, wrongID: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo, receipt, source, extractionIDs, rawItems := seedTrelloWebhookRefresh(t, "deleteComment", cardID, otherCardID, commentID, remainingID, otherActionID)
			creation := *receipt
			creation.ActionID = commentID
			creation.ActionType = "commentCard"
			item := trelloWebhookCommentImportItem(&creation)
			if trelloWebhookMetadataValue(item.Metadata, "action") != commentID || trelloWebhookMetadataValue(item.Metadata, "actionType") != "commentCard" {
				t.Fatal("new callback metadata does not identify the same canonical action as a provider scan")
			}
			externalID := "trello:action:" + commentID
			raw := rawItems[externalID]
			raw.ItemType = trelloCommentCreatedItemType
			raw.Metadata = item.Metadata
			if test.legacy {
				raw.Metadata = strings.Replace(raw.Metadata, "action="+commentID+";", "action=commentCard;", 1)
			}
			if !test.verified {
				raw.Metadata = strings.Replace(raw.Metadata, "webhookVerified=true", "webhookVerified=false", 1)
			}
			if test.wrongID {
				raw.Metadata = strings.Replace(raw.Metadata, "actionId="+commentID+";", "actionId="+otherActionID+";", 1)
			}
			if _, err := repo.SaveRawItem(&raw); err != nil {
				t.Fatalf("seed webhook-created raw item: %v", err)
			}
			extraction, err := repo.FindExtraction(extractionIDs[externalID])
			if err != nil {
				t.Fatalf("load creation extraction: %v", err)
			}
			extraction.ContentType = trelloCommentCreatedItemType
			if _, err := repo.SaveExtraction(extraction); err != nil {
				t.Fatalf("seed webhook-created extraction: %v", err)
			}
			refresh := &trelloWebhookCommentRefresh{Receipt: receipt, CardID: cardID, ActiveActionIDs: map[string]struct{}{remainingID: {}}}
			err = svc.archiveMissingTrelloWebhookComments(context.Background(), source, refresh)
			if (err != nil) != test.wantError {
				t.Fatalf("archive result = %v, want error=%v", err, test.wantError)
			}
			extraction, err = repo.FindExtraction(extractionIDs[externalID])
			if err != nil || extraction.Archived == test.wantError {
				t.Fatalf("comment archive state does not match verified identity: extraction=%+v err=%v", extraction, err)
			}
			retained, err := repo.FindRawItem(source.ID, externalID)
			if err != nil || retained.Content != raw.Content || retained.Metadata != raw.Metadata {
				t.Fatal("comment reconciliation rewrote or removed original webhook evidence")
			}
		})
	}
}

func TestTrelloProductionSyncListRecordsRespectAggregateCap(t *testing.T) {
	var cardReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_ = json.NewEncoder(w).Encode(trelloTokenInfo{IDMember: testTrelloAccountMemberID, Permissions: []trelloTokenPermission{{ModelType: "Board", Read: true}}})
		case strings.HasSuffix(r.URL.Path, "/lists"):
			lists := make([]trelloList, trelloMaxRecordsPerSync+1)
			for index := range lists {
				lists[index] = trelloList{ID: testTrelloMongoID(index + 1), Name: "List"}
			}
			_ = json.NewEncoder(w).Encode(lists)
		case strings.HasSuffix(r.URL.Path, "/cards"):
			cardReads.Add(1)
			_ = json.NewEncoder(w).Encode([]trelloCard{})
		default:
			_ = json.NewEncoder(w).Encode(trelloBoard{ID: trelloTestCanonicalBoardID, ShortURL: "https://trello.com/b/abc123XY"})
		}
	}))
	t.Cleanup(server.Close)
	configureTrelloTest(t, server.URL)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
	repo := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	slice, err := NewService(repo, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
	if err == nil || slice != nil || !strings.Contains(err.Error(), "aggregate record safety cap") || cardReads.Load() != 0 {
		t.Fatalf("oversized list inventory was not rejected before card reads: slice=%v err=%v cardReads=%d", slice, err, cardReads.Load())
	}
}
