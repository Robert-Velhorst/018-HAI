package source

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func newTrelloReopenHarness(t *testing.T) (Service, *atomic.Value, *fakeSourceRepo, uuid.UUID) {
	t.Helper()
	cardResponse := &atomic.Value{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Trello method = %q, want GET", r.Method)
		}
		if r.URL.Query().Has("key") || r.URL.Query().Has("token") {
			http.Error(w, "credentials exposed in query parameters", http.StatusBadRequest)
			return
		}
		if got, want := r.Header.Get("Authorization"), trelloAuthorizationHeader("test-key", "test-read-token"); got != want {
			http.Error(w, "missing credentials", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[{"id":"list-1","name":"Doing"}]`))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write(trelloTestCardPayload([]byte(cardResponse.Load().(string))))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Delivery board"))
		}
	}))
	t.Cleanup(server.Close)
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", "")
	source.OwnerIdentity = "alice"
	repo := newFakeSourceRepo(source)
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, &fakeSourceWorkflowService{})
	return service, cardResponse, repo, source.ID
}

func syncTrelloReopenState(t *testing.T, service Service, response *atomic.Value, payload string, sourceID uuid.UUID) models.SourceExtraction {
	t.Helper()
	response.Store(payload)
	result, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil || len(result.Extractions) != 1 {
		t.Fatalf("sync card state: result=%#v err=%v", result, err)
	}
	return result.Extractions[0]
}

func TestSyncTrelloReopenClearsClosureArchive(t *testing.T) {
	openCard := `[{"id":"reopen-card","name":"Prepare case file","desc":"Collect the verified source documents and prepare the evidence bundle.","shortUrl":"https://trello.com/c/reopen-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1"}]`
	closedCard := `[{"id":"reopen-card","name":"Prepare case file","desc":"This closed payload must not replace the retained evidence.","shortUrl":"https://trello.com/c/reopen-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1","closed":true}]`
	reopenedCard := `[{"id":"reopen-card","name":"Prepare case file","desc":"Collect the verified source documents and prepare the evidence bundle.","shortUrl":"https://trello.com/c/reopen-card","dateLastActivity":"2026-09-24T11:00:00Z","idList":"list-1"}]`
	service, response, _, sourceID := newTrelloReopenHarness(t)
	original := syncTrelloReopenState(t, service, response, openCard, sourceID)
	closed := syncTrelloReopenState(t, service, response, closedCard, sourceID)
	if closed.ID != original.ID || !closed.Archived || closed.ContentType != trelloClosedCardItemType {
		t.Fatalf("closed extraction = %#v; want same extraction archived by closure reconciliation", closed)
	}

	active := syncTrelloReopenState(t, service, response, reopenedCard, sourceID)
	if active.ID != original.ID || active.RawItemID != original.RawItemID || active.ContentType != "trello_card" || active.Archived {
		t.Fatalf("reopened extraction = %#v; want same active Trello extraction", active)
	}
}

func TestSyncTrelloReopenPreservesOperatorArchive(t *testing.T) {
	openCard := `[{"id":"operator-archive-card","name":"Prepare case file","desc":"Collect the verified source documents and prepare the evidence bundle.","shortUrl":"https://trello.com/c/operator-archive-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1"}]`
	closedCard := `[{"id":"operator-archive-card","name":"Prepare case file","desc":"Closed upstream.","shortUrl":"https://trello.com/c/operator-archive-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1","closed":true}]`
	reopenedCard := `[{"id":"operator-archive-card","name":"Prepare case file","desc":"Reopened upstream.","shortUrl":"https://trello.com/c/operator-archive-card","dateLastActivity":"2026-09-24T11:00:00Z","idList":"list-1"}]`

	for _, archiveAfterClosure := range []bool{false, true} {
		name := "operator archive before closure"
		if archiveAfterClosure {
			name = "operator archive after closure"
		}
		t.Run(name, func(t *testing.T) {
			service, response, repo, sourceID := newTrelloReopenHarness(t)
			original := syncTrelloReopenState(t, service, response, openCard, sourceID)
			if !archiveAfterClosure {
				if _, err := service.ArchiveExtraction(original.ID, true); err != nil {
					t.Fatalf("operator archive before closure: %v", err)
				}
			}
			closed := syncTrelloReopenState(t, service, response, closedCard, sourceID)
			if archiveAfterClosure {
				if _, err := service.ArchiveExtraction(closed.ID, true); err != nil {
					t.Fatalf("operator archive after closure: %v", err)
				}
			}
			reopened := syncTrelloReopenState(t, service, response, reopenedCard, sourceID)
			if reopened.ID != original.ID || !reopened.Archived {
				t.Fatalf("reopened extraction = %#v; explicit operator archive must be preserved", reopened)
			}
			if stored := repo.extractions[original.ID]; stored == nil || !stored.Archived {
				t.Fatalf("persisted extraction = %#v; operator archive must remain set", stored)
			}
		})
	}

	t.Run("ambiguous legacy archive remains archived", func(t *testing.T) {
		service, response, repo, sourceID := newTrelloReopenHarness(t)
		_ = syncTrelloReopenState(t, service, response, openCard, sourceID)
		closed := syncTrelloReopenState(t, service, response, closedCard, sourceID)
		repo.auditLogs = append(repo.auditLogs, models.SourceAuditLog{
			ID:        uuid.New(),
			SourceID:  sourceID,
			Action:    "extraction.archived",
			Message:   "archived=true",
			CreatedAt: time.Now().UTC(),
		})
		reopened := syncTrelloReopenState(t, service, response, reopenedCard, sourceID)
		if reopened.ID != closed.ID || !reopened.Archived {
			t.Fatalf("reopened extraction = %#v; ambiguous legacy archive provenance must fail closed", reopened)
		}
	})
}
