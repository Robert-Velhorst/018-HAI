package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// trelloTestServer returns an httptest server that serves a small read-only
// board and records every request method and whether credentials were sent.
func trelloTestServer(t *testing.T, cardsJSON string) (*httptest.Server, *[]string, *[]string) {
	t.Helper()
	methods := &[]string{}
	cardQueries := &[]string{}
	var fixtureCards []trelloCard
	if err := json.Unmarshal([]byte(cardsJSON), &fixtureCards); err != nil {
		t.Fatalf("decode card fixture: %v", err)
	}
	fixtureActions := make([]trelloAction, 0)
	for _, card := range fixtureCards {
		for _, action := range card.Actions {
			if action.Data.Card.ID == "" {
				action.Data.Card.ID = card.ID
			}
			fixtureActions = append(fixtureActions, action)
		}
	}
	cardsJSON = string(trelloTestCardPayload([]byte(cardsJSON)))
	actionsJSON, err := json.Marshal(fixtureActions)
	if err != nil {
		t.Fatalf("encode action fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*methods = append(*methods, r.Method)
		if r.URL.Query().Has("key") || r.URL.Query().Has("token") {
			http.Error(w, "credentials must not be sent in query parameters", http.StatusBadRequest)
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
			_, _ = w.Write([]byte(`[{"id":"list-1","name":"Doing"},{"id":"list-2","name":"Done"}]`))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			_, _ = w.Write(actionsJSON)
		case strings.HasSuffix(r.URL.Path, "/cards"):
			*cardQueries = append(*cardQueries, r.URL.RawQuery)
			_, _ = w.Write(trelloTestCardPayload([]byte(cardsJSON)))
		default: // board metadata
			_, _ = w.Write(trelloTestBoardJSON("Client Delivery"))
		}
	}))
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	return server, methods, cardQueries
}

func newTrelloSource(id uuid.UUID, target, cursor string) *models.ConnectedSource {
	return &models.ConnectedSource{
		ID:                id,
		OwnerIdentity:     "alice",
		ConnectorKey:      trelloConnectorKey,
		Name:              "Delivery board",
		Category:          "project_board",
		Enabled:           true,
		Status:            "active",
		SyncFrequency:     "manual",
		SyncTarget:        target,
		DefaultProjectKey: "018-HAI",
		Cursor:            cursor,
	}
}

func configureTrelloTest(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv(trelloBaseURLEnv, baseURL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	if parsed, err := url.Parse(baseURL); err == nil && sourceHTTPHostIsLoopback(parsed.Hostname()) {
		t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", sourceHTTPURLAddress(parsed))
	}
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)
}

const testTrelloAccountMemberID = "000000000000000000000001"
const testTrelloCanonicalBoardID = "0123456789abcdef01234567"

func trelloTestBoardJSON(name string) []byte {
	data, err := json.Marshal(trelloBoard{
		ID:       testTrelloCanonicalBoardID,
		Name:     name,
		ShortURL: "https://trello.com/b/abc123XY/test-board",
	})
	if err != nil {
		panic(err)
	}
	return data
}

func trelloTestCardPayload(payload []byte) []byte {
	var cards []trelloCard
	if err := json.Unmarshal(payload, &cards); err != nil {
		return payload
	}
	for index := range cards {
		if strings.TrimSpace(cards[index].IDBoard) == "" {
			cards[index].IDBoard = testTrelloCanonicalBoardID
		}
	}
	encoded, err := json.Marshal(cards)
	if err != nil {
		panic(err)
	}
	return encoded
}

func testTrelloMongoID(value int) string {
	return fmt.Sprintf("%024x", value)
}

func trelloTestFullCardPagePayload(t *testing.T, newestID int) []byte {
	t.Helper()
	cards := make([]trelloCard, trelloCardPageSize)
	for index := range cards {
		id := testTrelloMongoID(newestID - index)
		cards[index] = trelloCard{
			ID: id, IDBoard: testTrelloCanonicalBoardID, Name: "Card " + id,
			DateLastActivity: "2026-07-12T00:00:00Z", IDList: "list-1",
		}
	}
	payload, err := json.Marshal(cards)
	if err != nil {
		t.Fatalf("marshal full card page: %v", err)
	}
	return payload
}

func TestValidateTrelloActionPageOrderRejectsGapsAndShuffledPages(t *testing.T) {
	newest := testTrelloMongoID(30)
	middle := testTrelloMongoID(20)
	oldest := testTrelloMongoID(10)
	tests := []struct {
		name    string
		before  string
		ids     []string
		wantErr bool
	}{
		{name: "first page descending", ids: []string{newest, middle, oldest}},
		{name: "inclusive boundary then older page", before: middle, ids: []string{middle, oldest}},
		{name: "older page", before: middle, ids: []string{oldest}},
		{name: "shuffled page", ids: []string{middle, newest}, wantErr: true},
		{name: "newer than prior boundary", before: middle, ids: []string{newest, oldest}, wantErr: true},
		{name: "repeated action in page", ids: []string{newest, newest}, wantErr: true},
		{name: "invalid pagination boundary", before: "not-an-id", ids: []string{oldest}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page := make([]trelloAction, 0, len(test.ids))
			for _, id := range test.ids {
				page = append(page, trelloAction{ID: id})
			}
			err := validateTrelloActionPageOrder(page, test.before)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateTrelloActionPageOrder error = %v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func assertTrelloCursor(t *testing.T, value, wantActivity string) map[string]struct{} {
	t.Helper()
	activity, cardIDs, ok, err := parseTrelloCursor(value)
	if err != nil {
		t.Fatalf("parseTrelloCursor: %v", err)
	}
	want, err := time.Parse(time.RFC3339Nano, wantActivity)
	if err != nil {
		t.Fatalf("parse expected activity: %v", err)
	}
	if !ok || !activity.Equal(want) {
		t.Fatalf("cursor activity = %s (present=%t), want %s", activity, ok, want)
	}
	return cardIDs
}

func TestSyncTrelloImportsCardsWithProvenanceAndCursor(t *testing.T) {
	cards := `[
		{"id":"card-1","name":"Prepare client quote","desc":"Draft the quote","shortUrl":"https://trello.com/c/card-1","due":"2026-07-20T09:00:00Z","dateLastActivity":"2026-07-10T12:00:00Z","idList":"list-1","labels":[{"name":"priority","color":"red"}],"actions":[{"id":"comment-2","type":"commentCard","date":"2026-07-10T11:00:00Z","idMemberCreator":"member-1","data":{"text":"Attach the signed proposal."},"memberCreator":{"fullName":"Robert"}},{"id":"comment-1","type":"commentCard","date":"2026-07-09T10:00:00Z","idMemberCreator":"member-2","data":{"text":"Please verify the amount."},"memberCreator":{"username":"reviewer"}}],"checklists":[{"id":"checklist-1","name":"Delivery","pos":1,"checkItems":[{"id":"item-2","name":"Send quote","state":"incomplete","pos":2},{"id":"item-1","name":"Verify amount","state":"complete","pos":1}]}],"attachments":[{"id":"attachment-1","name":"proposal.pdf","url":"https://example.test/proposal.pdf","mimeType":"application/pdf","bytes":2048,"isUpload":true}]},
		{"id":"card-2","name":"Archived idea","dateLastActivity":"2026-07-05T08:00:00Z","idList":"list-2","closed":true}
	]`
	server, methods, cardQueries := trelloTestServer(t, cards)
	defer server.Close()

	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// A card already closed before it was imported is not materialized as a new
	// HAI source record; only the open card reaches extraction.
	if result.Job.ItemsSeen != 1 {
		t.Fatalf("ItemsSeen = %d, want 1 (untracked closed card skipped)", result.Job.ItemsSeen)
	}
	cursorIDs := assertTrelloCursor(t, result.Job.CursorAfter, "2026-07-10T12:00:00Z")
	if len(cursorIDs) != 0 || !strings.HasPrefix(result.Job.CursorAfter, trelloCursorV2Prefix) {
		t.Fatalf("cursor IDs = %#v, cursor=%q; want a timestamp-only v2 cursor", cursorIDs, result.Job.CursorAfter)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	if len(repo.rawItems) != 1 {
		t.Fatalf("raw items = %d, want only the open card; a pre-closed card must not create a source record", len(repo.rawItems))
	}
	if !strings.Contains(result.Extractions[0].SourceURI, "trello.com/c/card-1") {
		t.Fatalf("extraction provenance = %q, want card shortUrl", result.Extractions[0].SourceURI)
	}
	if result.Extractions[0].ProjectKey != "018-HAI" {
		t.Fatalf("project key = %q, want default", result.Extractions[0].ProjectKey)
	}
	for _, expected := range []string{"Comments (2)", "Please verify the amount.", "Attach the signed proposal.", "Checklists (1)", "[x] Verify amount", "[ ] Send quote", "Attachments (1; metadata only)", "proposal.pdf", "https://example.test/proposal.pdf"} {
		if !strings.Contains(result.Extractions[0].Text, expected) {
			t.Fatalf("extraction text does not contain %q: %q", expected, result.Extractions[0].Text)
		}
	}
	if strings.Index(result.Extractions[0].Text, "Please verify the amount.") > strings.Index(result.Extractions[0].Text, "Attach the signed proposal.") {
		t.Fatalf("comments were not normalized into chronological order: %q", result.Extractions[0].Text)
	}
	if len(*cardQueries) != 1 {
		t.Fatalf("card queries = %d, want 1", len(*cardQueries))
	}
	for _, expected := range []string{"filter=all", "limit=1000", "sort=-id", "attachments=true", "checklists=all"} {
		if !strings.Contains((*cardQueries)[0], expected) {
			t.Fatalf("card query %q does not request %q", (*cardQueries)[0], expected)
		}
	}
	query, err := url.ParseQuery((*cardQueries)[0])
	if err != nil {
		t.Fatalf("parse card query: %v", err)
	}
	if got := query.Get("checkItem_fields"); got != trelloCheckItemFields {
		t.Fatalf("checkItem_fields = %q, want adapter-supported fields %q", got, trelloCheckItemFields)
	}
	if strings.Contains(strings.ToLower(query.Get("checkItem_fields")), "duecomplete") {
		t.Fatalf("checkItem_fields requests undocumented dueComplete: %q", query.Get("checkItem_fields"))
	}
	if strings.Contains((*cardQueries)[0], "actions=") || strings.Contains((*cardQueries)[0], "actions_limit=") {
		t.Fatalf("card query must not request nested actions: %q", (*cardQueries)[0])
	}
	if !repo.hasAudit("source.synced") {
		t.Fatalf("expected trello sync audit record")
	}
	// Read-only guarantee: every request the adapter made must be a GET.
	for _, method := range *methods {
		if method != http.MethodGet {
			t.Fatalf("trello adapter issued a %s request; the connector must be read-only", method)
		}
	}
}

func TestSyncTrelloImportsCardSchedulingAndAssignments(t *testing.T) {
	const (
		memberA = "00000000000000000000000a"
		memberB = "00000000000000000000000b"
		unknown = "00000000000000000000000c"
	)
	cards := `[{"id":"0123456789abcdef01234567","name":"Prepare review","start":"2026-09-20T09:00:00.000Z","due":"2026-09-30T17:00:00.000Z","dueComplete":true,"dateLastActivity":"2026-09-20T10:00:00Z","idList":"list-1","idMembers":["` + memberB + `","` + memberA + `","` + unknown + `"],"members":[{"id":"` + memberB + `","fullName":"Zoe Example","username":"zoe"},{"id":"` + memberA + `","fullName":"Alice Example","username":"alice"}]}]`
	server, _, cardQueries := trelloTestServer(t, cards)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want one assigned card", len(result.Extractions))
	}
	text := result.Extractions[0].Text
	for _, expected := range []string{
		"Start: 2026-09-20T09:00:00.000Z",
		"Due: 2026-09-30T17:00:00.000Z (completed)",
		"Assigned members: Alice Example (@alice), Zoe Example (@zoe), ",
		unknown,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("imported card text does not contain %q: %q", expected, text)
		}
	}
	if strings.Index(text, "Alice Example (@alice)") > strings.Index(text, "Zoe Example (@zoe)") {
		t.Fatalf("assigned members are not rendered in deterministic order: %q", text)
	}
	if len(*cardQueries) != 1 {
		t.Fatalf("card queries = %d, want one cards request", len(*cardQueries))
	}
	query, err := url.ParseQuery((*cardQueries)[0])
	if err != nil {
		t.Fatalf("parse card query: %v", err)
	}
	for _, field := range []string{"start", "dueComplete", "idMembers"} {
		if !strings.Contains(","+query.Get("fields")+",", ","+field+",") {
			t.Errorf("requested card fields %q omit %q", query.Get("fields"), field)
		}
	}
	if query.Get("members") != "true" || query.Get("member_fields") != "fullName,username" {
		t.Errorf("member query = members:%q member_fields:%q; want public assignee names only", query.Get("members"), query.Get("member_fields"))
	}
	rawItems, err := repo.FindRawItems(sourceID)
	if err != nil || len(rawItems) != 1 {
		t.Fatalf("raw source items = %d, err=%v; want one", len(rawItems), err)
	}
	if !strings.Contains(rawItems[0].Metadata, "dueComplete=true") || !strings.Contains(rawItems[0].Metadata, "members=3") {
		t.Errorf("source metadata omitted due completion or assignee count: %q", rawItems[0].Metadata)
	}
}

func TestTrelloAssignedMemberRenderingIsStableAndPreservesUnresolvedIDs(t *testing.T) {
	const (
		memberA = "00000000000000000000000a"
		memberB = "00000000000000000000000b"
		unknown = "00000000000000000000000c"
	)
	first := trelloCard{
		IDMembers: []string{memberB, memberA, unknown},
		Members: []trelloMember{
			{ID: memberB, FullName: "Zoe Example", Username: "zoe"},
			{ID: memberA, FullName: "Alice Example", Username: "alice"},
		},
	}
	second := trelloCard{
		IDMembers: []string{strings.ToUpper(unknown), strings.ToUpper(memberA), strings.ToUpper(memberB)},
		Members: []trelloMember{
			{ID: memberA, FullName: "Alice Example", Username: "alice"},
			{ID: memberB, FullName: "Zoe Example", Username: "zoe"},
		},
	}
	got := strings.Join(trelloAssignedMemberNames(first), ", ")
	if want := "Alice Example (@alice), Zoe Example (@zoe), " + unknown; got != want {
		t.Fatalf("assigned members = %q, want %q", got, want)
	}
	if other := strings.Join(trelloAssignedMemberNames(second), ", "); other != got {
		t.Fatalf("reordered provider members changed import content: %q != %q", other, got)
	}
	if got := strings.Join(trelloAssignedMemberNames(trelloCard{
		IDMembers: []string{memberA}, Members: []trelloMember{{ID: memberA, Username: "solo"}},
	}), ", "); got != "@solo" {
		t.Fatalf("username-only assignee = %q, want @solo", got)
	}
}

func TestSyncTrelloClosedCardTombstonesExistingProjection(t *testing.T) {
	openCard := `[{"id":"closure-card","name":"Prepare case file","shortUrl":"https://trello.com/c/closure-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1","checklists":[{"id":"checklist-1","name":"Evidence","pos":1,"checkItems":[{"id":"open-item","name":"Verify source links","state":"incomplete","pos":1}]}]}]`
	closedCard := `[{"id":"closure-card","name":"Prepare case file","desc":"This later source payload must not replace the preserved evidence.","shortUrl":"https://trello.com/c/closure-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1","closed":true}]`
	cardResponse := &atomic.Value{}
	cardResponse.Store(openCard)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Trello method = %q, want GET", r.Method)
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
			if got := r.URL.Query().Get("filter"); got != "all" {
				t.Errorf("Trello cards filter = %q, want all", got)
			}
			_, _ = w.Write(trelloTestCardPayload([]byte(cardResponse.Load().(string))))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Delivery board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", "")
	source.OwnerIdentity = "alice"
	repo := newFakeSourceRepo(source)
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)

	openResult, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("initial open-card sync: %v", err)
	}
	if len(openResult.Extractions) != 1 || len(workflowSpy.requests) != 1 {
		t.Fatalf("initial sync extractions/workflows = %d/%d, want 1/1", len(openResult.Extractions), len(workflowSpy.requests))
	}
	openExtraction := openResult.Extractions[0]
	if openExtraction.ContentType != "trello_card" || openExtraction.Archived {
		t.Fatalf("initial extraction = %#v, want an active Trello card projection", openExtraction)
	}
	openRawItems, err := repo.FindRawItems(source.ID)
	if err != nil || len(openRawItems) != 1 {
		t.Fatalf("initial raw items = %d, err=%v; want one", len(openRawItems), err)
	}
	openRaw := openRawItems[0]
	priorAuditIDs := make(map[uuid.UUID]bool, len(repo.auditLogs))
	for _, audit := range repo.auditLogs {
		priorAuditIDs[audit.ID] = true
	}
	if len(priorAuditIDs) == 0 {
		t.Fatal("initial sync did not create audit history")
	}

	cardResponse.Store(closedCard)
	closedResult, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("closed-card sync: %v", err)
	}
	if closedResult.Job.ItemsSeen != 1 || closedResult.Job.ItemsUpdated != 1 || closedResult.Job.ItemsAdded != 0 || closedResult.Job.ItemsFailed != 0 {
		t.Fatalf("closure sync counts = %#v, want one update and no additions or failures", closedResult.Job)
	}
	if len(closedResult.Extractions) != 1 {
		t.Fatalf("closure result extractions = %d, want the updated projection", len(closedResult.Extractions))
	}
	closedExtraction := closedResult.Extractions[0]
	if closedExtraction.ID != openExtraction.ID || closedExtraction.RawItemID != openExtraction.RawItemID || closedExtraction.ContentType != trelloClosedCardItemType || !closedExtraction.Archived {
		t.Fatalf("closed extraction = %#v, want same record with explicit closed/inactive state", closedExtraction)
	}
	if closedExtraction.Text != openExtraction.Text || closedExtraction.Summary != openExtraction.Summary || closedExtraction.Tasks != openExtraction.Tasks || closedExtraction.SourceURI != openExtraction.SourceURI || closedExtraction.SourceLabel != openExtraction.SourceLabel || closedExtraction.ContentHash != openExtraction.ContentHash {
		t.Fatalf("closure overwrote source evidence or provenance: before=%#v after=%#v", openExtraction, closedExtraction)
	}
	rawItems, err := repo.FindRawItems(source.ID)
	if err != nil || len(rawItems) != 1 {
		t.Fatalf("raw items after closure = %d, err=%v; closure must not delete or duplicate the source record", len(rawItems), err)
	}
	closedRaw := rawItems[0]
	if closedRaw.ID != openRaw.ID || closedRaw.ItemType != trelloClosedCardItemType {
		t.Fatalf("closed raw record = %#v, want the same raw record explicitly marked closed", closedRaw)
	}
	if closedRaw.ExternalID != openRaw.ExternalID || closedRaw.Title != openRaw.Title || closedRaw.SourceURI != openRaw.SourceURI || closedRaw.Content != openRaw.Content || closedRaw.Metadata != openRaw.Metadata || closedRaw.ContentHash != openRaw.ContentHash {
		t.Fatalf("closure overwrote raw evidence or provenance: before=%#v after=%#v", openRaw, closedRaw)
	}
	if len(repo.extractions) != 1 {
		t.Fatalf("stored extractions = %d, want the original projection retained", len(repo.extractions))
	}
	if len(workflowSpy.requests) != 1 {
		t.Fatalf("workflow intake requests = %d, want no new workflow from closure", len(workflowSpy.requests))
	}
	if len(workflowSpy.retractions) != 1 || workflowSpy.retractions[0].sourceType != trelloConnectorKey || workflowSpy.retractions[0].sourceID != openExtraction.ID.String() {
		t.Fatalf("workflow retractions = %#v, want the existing Trello workflow retracted by its original source identity", workflowSpy.retractions)
	}
	for auditID := range priorAuditIDs {
		found := false
		for _, audit := range repo.auditLogs {
			if audit.ID == auditID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("prior source audit %s was removed during closure", auditID)
		}
	}
	if !repo.hasAudit("source.trello_card_closed") {
		t.Fatal("closure did not append an explicit Trello closure audit event")
	}
}

func TestSyncTrelloMissingBoardCardArchivesProjectionWithoutDeletingEvidence(t *testing.T) {
	const activeCard = `[{"id":"missing-card","name":"Keep this case record","desc":"Original evidence must remain intact.","shortUrl":"https://trello.com/c/missing-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1"}]`
	cardResponse := &atomic.Value{}
	cardResponse.Store(activeCard)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Trello method = %q, want GET", r.Method)
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
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", "")
	repo := newFakeSourceRepo(source)
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	created, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("initial card sync: %v", err)
	}
	if len(created.Extractions) != 1 || len(workflowSpy.requests) != 1 {
		t.Fatalf("initial extractions/workflows = %d/%d, want 1/1", len(created.Extractions), len(workflowSpy.requests))
	}
	prior := created.Extractions[0]
	cardResponse.Store(`[]`)

	removed, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("reconcile card missing from board: %v", err)
	}
	if removed.Job.ItemsSeen != 1 || removed.Job.ItemsUpdated != 1 || removed.Job.ItemsFailed != 0 || len(removed.Extractions) != 1 {
		t.Fatalf("missing-card sync = %#v, extractions=%d; want one successful retained-state update", removed.Job, len(removed.Extractions))
	}
	updated := removed.Extractions[0]
	if updated.ID != prior.ID || updated.ContentType != trelloUnavailableCardItemType || !updated.Archived || updated.Text != prior.Text || updated.SourceURI != prior.SourceURI {
		t.Fatalf("missing-card extraction = %#v, want same archived projection with retained content and provenance", updated)
	}
	rawItems, err := repo.FindRawItems(source.ID)
	if err != nil || len(rawItems) != 1 {
		t.Fatalf("raw records = %d err=%v; want one retained record", len(rawItems), err)
	}
	if rawItems[0].ItemType != trelloUnavailableCardItemType || rawItems[0].Content == "" || rawItems[0].ContentHash == "" || rawItems[0].SourceURI != prior.SourceURI {
		t.Fatalf("missing-card raw record = %#v, want source state updated with raw content, hash, and URI retained", rawItems[0])
	}
	if len(workflowSpy.retractions) != 1 || workflowSpy.retractions[0].sourceID != prior.ID.String() {
		t.Fatalf("workflow retractions = %#v, want the source-derived workflow retracted", workflowSpy.retractions)
	}
	if !repo.hasAudit("source.trello_card_unavailable") {
		t.Fatal("missing-card reconciliation did not append an audit event")
	}

	cardResponse.Store(activeCard)
	returned, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil || len(returned.Extractions) != 1 {
		t.Fatalf("card return sync = result %#v err=%v; want one restored extraction", returned, err)
	}
	restored := returned.Extractions[0]
	if restored.ID != prior.ID || restored.ContentType != "trello_card" || restored.Archived {
		t.Fatalf("returned card extraction = %#v, want prior evidence restored without archive", restored)
	}
	if _, err := service.ArchiveExtraction(restored.ID, true); err != nil {
		t.Fatalf("operator archive: %v", err)
	}
	workflowRequestsBeforeArchivedReturn := len(workflowSpy.requests)
	cardResponse.Store(`[]`)
	if _, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync}); err != nil {
		t.Fatalf("second missing-card reconciliation: %v", err)
	}
	cardResponse.Store(activeCard)
	operatorArchivedReturn, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil || len(operatorArchivedReturn.Extractions) != 1 {
		t.Fatalf("operator-archived return sync = result %#v err=%v; want one refreshed extraction", operatorArchivedReturn, err)
	}
	if !operatorArchivedReturn.Extractions[0].Archived {
		t.Fatal("Trello source return overrode the operator's explicit archive")
	}
	if len(workflowSpy.requests) != workflowRequestsBeforeArchivedReturn {
		t.Fatalf("operator-archived card return created a workflow: requests=%d want %d", len(workflowSpy.requests), workflowRequestsBeforeArchivedReturn)
	}
}

func TestSyncTrelloRetriesOlderChangedCardAfterWorkflowIntakeFailure(t *testing.T) {
	const initialCards = `[{"id":"old-card","name":"Case task","shortUrl":"https://trello.com/c/old-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1","checklists":[{"id":"checklist-1","name":"Evidence","pos":1,"checkItems":[{"id":"item-1","name":"Verify original source links","state":"incomplete","pos":1}]}]},{"id":"cursor-card","name":"Current note","shortUrl":"https://trello.com/c/cursor-card","dateLastActivity":"2026-09-24T12:00:00Z","idList":"list-1"}]`
	const changedCards = `[{"id":"old-card","name":"Case task","shortUrl":"https://trello.com/c/old-card","dateLastActivity":"2026-09-24T10:00:00Z","idList":"list-1","checklists":[{"id":"checklist-1","name":"Evidence","pos":1,"checkItems":[{"id":"item-1","name":"Review retained source links","state":"incomplete","pos":1}]}]},{"id":"cursor-card","name":"Current note","shortUrl":"https://trello.com/c/cursor-card","dateLastActivity":"2026-09-24T12:00:00Z","idList":"list-1"}]`
	cardResponse := &atomic.Value{}
	cardResponse.Store(initialCards)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", "")
	repo := newFakeSourceRepo(source)
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	initial, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if len(initial.Extractions) != 2 {
		t.Fatalf("initial extractions = %d, want both cards", len(initial.Extractions))
	}
	oldRawBeforeFailure, err := repo.FindRawItem(source.ID, "trello:card:old-card")
	if err != nil || oldRawBeforeFailure.ContentHash == "" {
		t.Fatalf("initial card checkpoint = %#v err=%v", oldRawBeforeFailure, err)
	}
	priorContentHash := oldRawBeforeFailure.ContentHash
	cardResponse.Store(changedCards)
	workflowSpy.intakeErr = errors.New("temporary intake failure")
	failed, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("partial sync should return a persisted result: %v", err)
	}
	if failed.Job.Status != "failed" || failed.Job.CursorAfter != failed.Job.CursorBefore || failed.Job.ItemsFailed != 1 {
		t.Fatalf("failed update = %#v, want one retryable failure, failed status, and retained cursor", failed.Job)
	}
	oldRaw, err := repo.FindRawItem(source.ID, "trello:card:old-card")
	if err != nil || oldRaw.ContentHash != priorContentHash || !strings.Contains(oldRaw.Content, "Review retained source links") {
		t.Fatalf("failed card checkpoint = %#v err=%v; want new content retained with the prior processed hash for retry", oldRaw, err)
	}

	workflowSpy.intakeErr = nil
	retried, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("retry sync: %v", err)
	}
	if retried.Job.ItemsFailed != 0 || len(retried.Extractions) != 1 {
		t.Fatalf("retry sync = %#v extractions=%d, want only the changed pending card reprocessed", retried.Job, len(retried.Extractions))
	}
	oldRaw, err = repo.FindRawItem(source.ID, "trello:card:old-card")
	if err != nil || oldRaw.ContentHash == "" || oldRaw.ContentHash == priorContentHash || !strings.Contains(oldRaw.Content, "Review retained source links") {
		t.Fatalf("successfully retried raw card = %#v err=%v", oldRaw, err)
	}
}

func TestFetchTrelloIncludesCommentCreatorInSelectedActionFields(t *testing.T) {
	const cardID = "000000000000000000000123"
	const commentText = "Please confirm the evidence bundle."
	const copiedCommentText = "Copied note from the original card."
	var actionFields, actionFilter string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[{"id":"list-1","name":"Inbox"}]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write(trelloTestCardPayload([]byte(fmt.Sprintf(`[{"id":%q,"name":"Legal review","dateLastActivity":"2026-09-24T12:00:00Z","idList":"list-1"}]`, cardID))))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			query := r.URL.Query()
			actionFields = query.Get("fields")
			actionFilter = query.Get("filter")
			action := map[string]any{
				"id": testTrelloMongoID(124), "type": "commentCard", "date": "2026-09-24T11:00:00Z",
				"idMemberCreator": "member-1",
				"data":            map[string]any{"text": commentText, "card": map[string]any{"id": cardID}},
			}
			copiedAction := map[string]any{
				"id": testTrelloMongoID(123), "type": "copyCommentCard", "date": "2026-09-24T11:30:00Z",
				"idMemberCreator": "member-2",
				"data":            map[string]any{"text": copiedCommentText, "card": map[string]any{"id": cardID}},
			}
			// Trello applies `fields` to the action shape; the separate
			// memberCreator=true switch only expands that selected field.
			if query.Get("memberCreator") == "true" && strings.Contains(","+actionFields+",", ",memberCreator,") {
				action["memberCreator"] = map[string]any{"fullName": "Reviewer Name", "username": "reviewer"}
			}
			if err := json.NewEncoder(w).Encode([]map[string]any{action, copiedAction}); err != nil {
				t.Errorf("encode action response: %v", err)
			}
		default:
			_, _ = w.Write(trelloTestBoardJSON("Review board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	items, _, err := fetchTrelloSource(t.Context(), newTrelloSource(uuid.New(), "abc123XY", ""))
	if err != nil {
		t.Fatalf("fetchTrelloSource: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want one card", len(items))
	}
	if !strings.Contains(items[0].Content, "Reviewer Name: "+commentText) {
		t.Fatalf("normalized card omitted comment creator name: %q", items[0].Content)
	}
	if !strings.Contains(items[0].Content, "Comments (2):") || !strings.Contains(items[0].Content, "(copied comment): "+copiedCommentText) {
		t.Fatalf("normalized card omitted or failed to identify copied comment: %q", items[0].Content)
	}
	if actionFilter != trelloCommentActionTypes {
		t.Fatalf("action filter = %q, want %q", actionFilter, trelloCommentActionTypes)
	}
	if !strings.Contains(","+actionFields+",", ",memberCreator,") {
		t.Fatalf("action fields = %q, want memberCreator selected alongside memberCreator=true", actionFields)
	}
}

func TestSyncTrelloUncheckedChecklistItemsCreateReviewGatedWorkflow(t *testing.T) {
	cards := `[{"id":"checklist-card","name":"Prepare case file","shortUrl":"https://trello.com/c/checklist-card","dateLastActivity":"2026-09-20T10:00:00Z","idList":"list-1","checklists":[{"id":"checklist-1","name":"Evidence","pos":1,"checkItems":[{"id":"open-item","name":"Verify source links","state":"incomplete","pos":1},{"id":"done-item","name":"Send final letter","state":"complete","pos":2}]}]}]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", "")
	source.OwnerIdentity = "alice"
	repo := newFakeSourceRepo(source)
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	result, err := service.Sync(source.ID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want one card", len(result.Extractions))
	}
	gotTasks := result.Extractions[0].Tasks
	if !strings.Contains(gotTasks, "Evidence: Verify source links") {
		t.Fatalf("extracted tasks = %q, want the open checklist item with checklist context", gotTasks)
	}
	if strings.Contains(gotTasks, "Send final letter") {
		t.Fatalf("completed checklist item became an actionable task: %q", gotTasks)
	}
	if len(workflowSpy.requests) != 1 {
		t.Fatalf("workflow requests = %d, want one candidate for the unchecked item", len(workflowSpy.requests))
	}
	request := workflowSpy.requests[0]
	if !request.RequiresReview || request.SourceType != trelloConnectorKey || request.ProjectKey != "" || request.ProjectKeyHint != "018-HAI" {
		t.Fatalf("workflow intake = %#v, want Trello source, unverified project hint, and mandatory review", request)
	}
	if !strings.Contains(request.Input, "Evidence: Verify source links") || request.SourceURI != "https://trello.com/c/checklist-card" {
		t.Fatalf("workflow candidate lost checklist action or source provenance: %#v", request)
	}
}

func TestFetchTrelloDoesNotInventProjectKeyFromBoardName(t *testing.T) {
	server, _, _ := trelloTestServer(t, `[{"id":"card-1","name":"Unmatched card","shortUrl":"https://trello.com/c/card-1","dateLastActivity":"2026-09-20T10:00:00Z","idList":"list-1"}]`)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", "")
	source.DefaultProjectKey = ""
	items, _, err := fetchTrelloSource(t.Context(), source)
	if err != nil {
		t.Fatalf("fetchTrelloSource: %v", err)
	}
	if len(items) != 1 || items[0].ProjectKey != "" {
		t.Fatalf("imported items = %#v, want one item with no guessed project key", items)
	}
}

func TestFetchTrelloIncludesClosedListsWhenResolvingCardContext(t *testing.T) {
	const cards = `[{"id":"archived-list-card","name":"Retained project work","dateLastActivity":"2026-09-20T10:00:00Z","idList":"closed-list"}]`
	var listsFilter string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Trello method = %q, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			listsFilter = r.URL.Query().Get("filter")
			if listsFilter == "all" {
				_, _ = w.Write([]byte(`[{"id":"closed-list","name":"Archived delivery"}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write(trelloTestCardPayload([]byte(cards)))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Delivery"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	items, _, err := fetchTrelloSource(t.Context(), newTrelloSource(uuid.New(), "abc123XY", ""))
	if err != nil {
		t.Fatalf("fetchTrelloSource: %v", err)
	}
	if listsFilter != "all" {
		t.Fatalf("lists filter = %q, want all lists to preserve card-to-list context", listsFilter)
	}
	if len(items) != 1 || !strings.Contains(items[0].Content, "List: Archived delivery") {
		t.Fatalf("imported items = %#v, want retained card associated with its closed list", items)
	}
}

func TestTrelloConnectionHealthReflectsSuccessfulSyncHistory(t *testing.T) {
	configureTrelloTest(t, "https://api.trello.com")
	lastSynced := time.Date(2026, 9, 20, 10, 30, 0, 0, time.UTC)
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	source.LastSyncedAt = &lastSynced
	service := NewService(newFakeSourceRepo(source), &fakeSourceMemoryService{})

	health, err := service.(ConnectionHealthService).ConnectionHealth(source.ID)
	if err != nil {
		t.Fatalf("ConnectionHealth: %v", err)
	}
	if health.Status != "previously_verified" || health.Authorized {
		t.Fatalf("health = %#v, want historical verification without claiming current authorization", health)
	}
	if health.LastSyncedAt == nil || !health.LastSyncedAt.Equal(lastSynced) {
		t.Fatalf("LastSyncedAt = %v, want successful sync time %v", health.LastSyncedAt, lastSynced)
	}
	if !strings.Contains(health.Reason, lastSynced.Format(time.RFC3339)) || !strings.Contains(health.Reason, "fresh sync") {
		t.Fatalf("health reason = %q, want last successful time and current recheck guidance", health.Reason)
	}
}

func TestTrelloImportItemRetainsCanonicalProvenanceWhenURLFieldsAreMissing(t *testing.T) {
	item := trelloImportItem(trelloCard{ID: "card-without-url", Name: "Evidence"}, "Board", "Inbox", "project")
	if item.SourceURI != "https://trello.com/c/card-without-url" {
		t.Fatalf("SourceURI = %q, want canonical card URL fallback", item.SourceURI)
	}
}

func TestSyncTrelloPreservesComplexCardAcceptanceShape(t *testing.T) {
	actions := make([]map[string]any, 0, 30)
	for i := 0; i < 30; i++ {
		actions = append(actions, map[string]any{
			"id": fmt.Sprintf("comment-%02d", i+1), "type": "commentCard",
			"date":          fmt.Sprintf("2026-07-%02dT10:00:00Z", i%28+1),
			"data":          map[string]any{"text": fmt.Sprintf("Operational comment %02d", i+1)},
			"memberCreator": map[string]any{"fullName": "Robert"},
		})
	}
	attachments := []map[string]any{
		{"id": "a-1", "name": "requirements.txt", "url": "https://example.test/requirements.txt", "mimeType": "text/plain", "bytes": 120},
		{"id": "a-2", "name": "evidence.pdf", "url": "https://example.test/evidence.pdf", "mimeType": "application/pdf", "bytes": 2048},
		{"id": "a-3", "name": "walkthrough.png", "url": "https://example.test/walkthrough.png", "mimeType": "image/png", "bytes": 4096},
		{"id": "a-4", "name": "demo.mp4", "url": "https://example.test/demo.mp4", "mimeType": "video/mp4", "bytes": 8192},
		{"id": "a-5", "name": "Drive evidence", "url": "https://drive.google.com/file/d/example/view", "mimeType": "text/html"},
	}
	cards, err := json.Marshal([]map[string]any{{
		"id": "complex-card", "name": "HAI operational card", "desc": "Build the real engine.",
		"shortUrl": "https://trello.com/c/complex", "dateLastActivity": "2026-07-30T12:00:00Z",
		"idList": "list-1", "actions": actions, "attachments": attachments,
		"checklists": []map[string]any{{"id": "check-1", "name": "Acceptance", "pos": 1, "checkItems": []map[string]any{{"id": "item-1", "name": "Verify source links", "state": "incomplete", "pos": 1}}}},
	}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	server, methods, _ := trelloTestServer(t, string(cards))
	defer server.Close()
	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	text := result.Extractions[0].Text
	for _, expected := range []string{"Comments (30)", "Operational comment 01", "Operational comment 30", "Checklists (1)", "Attachments (5; metadata only)", "requirements.txt", "evidence.pdf", "walkthrough.png", "demo.mp4", "https://drive.google.com/file/d/example/view"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("complex card extraction missing %q", expected)
		}
	}
	rawItems, err := repo.FindRawItems(sourceID)
	if err != nil || len(rawItems) != 1 {
		t.Fatalf("FindRawItems: items=%#v err=%v", rawItems, err)
	}
	if !strings.Contains(rawItems[0].Metadata, "comments=30") || !strings.Contains(rawItems[0].Metadata, "attachments=5") || !strings.Contains(rawItems[0].Metadata, "attachmentContentFetched=false") {
		t.Fatalf("complex card metadata = %q", rawItems[0].Metadata)
	}
	for _, method := range *methods {
		if method != http.MethodGet {
			t.Fatalf("complex Trello intake issued %s, want GET only", method)
		}
	}
}

func TestSyncTrelloIncrementalSkipsUnchangedCards(t *testing.T) {
	cards := `[
		{"id":"card-old","name":"Stale card","shortUrl":"https://trello.com/c/old","dateLastActivity":"2026-07-01T00:00:00Z","idList":"list-1"},
		{"id":"card-new","name":"Fresh activity","shortUrl":"https://trello.com/c/new","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"}
	]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()

	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	sourceID := uuid.New()
	// Cursor sits between the two cards' activity timestamps.
	source := newTrelloSource(sourceID, "abc123XY", "2026-07-05T00:00:00Z")
	repo := newFakeSourceRepo(source)
	old := trelloImportItem(trelloCard{
		ID: "card-old", Name: "Stale card", ShortURL: "https://trello.com/c/old",
		DateLastActivity: "2026-07-01T00:00:00Z", IDList: "list-1",
	}, "Client Delivery", "Doing", source.DefaultProjectKey)
	if _, err := repo.SaveRawItem(&models.SourceRawItem{
		SourceID: sourceID, ExternalID: old.ExternalID, ItemType: old.ItemType,
		Title: old.Title, Content: old.Content, SourceURI: old.SourceURI,
		ContentHash: hashText(old.Title + "|" + old.Content),
	}); err != nil {
		t.Fatalf("seed previously synced card: %v", err)
	}
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 1 {
		t.Fatalf("ItemsSeen = %d, want 1 (only the card changed after the cursor)", result.Job.ItemsSeen)
	}
	if len(result.Extractions) != 1 || !strings.Contains(result.Extractions[0].SourceURI, "/c/new") {
		t.Fatalf("expected only the freshly-updated card, got %#v", result.Extractions)
	}
	cursorIDs := assertTrelloCursor(t, result.Job.CursorAfter, "2026-07-12T00:00:00Z")
	if len(cursorIDs) != 0 || !strings.HasPrefix(result.Job.CursorAfter, trelloCursorV2Prefix) {
		t.Fatalf("cursor IDs = %#v, cursor=%q; want a timestamp-only v2 cursor", cursorIDs, result.Job.CursorAfter)
	}
}

func TestFetchTrelloSourceMigratesLegacyRFC3339CursorAndReplaysBoundary(t *testing.T) {
	const activity = "2026-07-12T00:00:00Z"
	cards := `[
		{"id":"card-at-boundary","name":"Boundary card","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"},
		{"id":"card-older","name":"Older card","dateLastActivity":"2026-07-11T23:59:59Z","idList":"list-1"}
	]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()
	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	source := newTrelloSource(uuid.New(), "abc123XY", activity)
	items, migratedCursor, err := fetchTrelloSource(t.Context(), source)
	if err != nil {
		t.Fatalf("fetch legacy cursor: %v", err)
	}
	if len(items) != 1 || items[0].ExternalID != "trello:card:card-at-boundary" {
		t.Fatalf("legacy boundary items = %#v, want only card-at-boundary", items)
	}
	cursorIDs := assertTrelloCursor(t, migratedCursor, activity)
	if len(cursorIDs) != 0 || !strings.HasPrefix(migratedCursor, trelloCursorV2Prefix) {
		t.Fatalf("migrated cursor IDs = %#v, cursor=%q; want timestamp-only v2", cursorIDs, migratedCursor)
	}

	source.Cursor = migratedCursor
	items, nextCursor, err := fetchTrelloSource(t.Context(), source)
	if err != nil {
		t.Fatalf("fetch migrated cursor: %v", err)
	}
	if len(items) != 1 || items[0].ExternalID != "trello:card:card-at-boundary" || nextCursor != migratedCursor {
		t.Fatalf("subsequent fetch = items %#v, cursor %q; want the tied boundary replayed with an unchanged cursor", items, nextCursor)
	}
}

func TestParseTrelloCursorReadsLegacyVersionOne(t *testing.T) {
	const activity = "2026-07-12T00:00:00Z"
	payload := []byte(`{"t":"2026-07-12T00:00:00Z","i":["card-existing"]}`)
	legacy := trelloCursorV1Prefix + "j|" + base64.RawURLEncoding.EncodeToString(payload)
	parsedActivity, ids, present, err := parseTrelloCursor(legacy)
	wantActivity, _ := time.Parse(time.RFC3339, activity)
	if err != nil || !present || !parsedActivity.Equal(wantActivity) || len(ids) != 1 {
		t.Fatalf("parse v1 cursor = (%s, %#v, %t, %v), want its timestamp and legacy card ID", parsedActivity, ids, present, err)
	}
	if _, ok := ids["card-existing"]; !ok {
		t.Fatalf("v1 cursor IDs = %#v, want card-existing", ids)
	}

	compact := trelloCursorV1Prefix + "h|" + activity + "|" + base64.RawURLEncoding.EncodeToString(make([]byte, trelloCardIDByteLength))
	parsedActivity, ids, present, err = parseTrelloCursor(compact)
	if err != nil || !present || !parsedActivity.Equal(wantActivity) || len(ids) != 1 {
		t.Fatalf("parse compact v1 cursor = (%s, %#v, %t, %v), want its timestamp and legacy card ID", parsedActivity, ids, present, err)
	}
	if _, ok := ids["000000000000000000000000"]; !ok {
		t.Fatalf("compact v1 cursor IDs = %#v, want the packed zero ID", ids)
	}
}

func TestParseTrelloCursorMigratesGenericLegacyAndRejectsUnknownValues(t *testing.T) {
	const legacy = "2026-07-12T00:00:00Z:0"
	activity, ids, present, err := parseTrelloCursor(legacy)
	if err != nil || !present || !activity.Equal(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)) || len(ids) != 0 {
		t.Fatalf("parse generic legacy cursor = (%s, %#v, %t, %v), want its RFC3339 time and no IDs", activity, ids, present, err)
	}
	if _, _, present, err := parseTrelloCursor("not-a-trello-cursor"); err == nil || present {
		t.Fatalf("parse malformed cursor = (present=%t, err=%v), want an error rather than a fresh-source cursor", present, err)
	}
}

func TestSyncTrelloRejectsMalformedCursorWithoutProviderRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	const badCursor = "corrupt-cursor"
	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", badCursor))
	if _, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync}); err == nil || !strings.Contains(err.Error(), "parse trello cursor") {
		t.Fatalf("Sync error = %v, want malformed cursor rejection", err)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != badCursor || requests != 0 {
		t.Fatalf("cursor=%q requests=%d, want unchanged cursor and no provider request", stored.Cursor, requests)
	}
}

func TestSyncTrelloEmptyBoardKeepsValidCursorAndFindsLaterCard(t *testing.T) {
	hasCard := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			if hasCard {
				_, _ = w.Write(trelloTestCardPayload([]byte(`[{"id":"card-added-later","name":"Later card","dateLastActivity":"2026-09-24T12:00:00Z","idList":"list-1"}]`)))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	service := NewService(repo, &fakeSourceMemoryService{})
	first, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("initial empty-board Sync: %v", err)
	}
	if _, _, present, err := parseTrelloCursor(first.Job.CursorAfter); err != nil || !present || first.Job.CursorAfter != trelloCursorV2Prefix {
		t.Fatalf("empty-board cursor = %q, present=%t err=%v; want a valid Trello cursor", first.Job.CursorAfter, present, err)
	}

	hasCard = true
	second, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync after card appeared: %v", err)
	}
	if second.Job.ItemsSeen != 1 || len(second.Extractions) != 1 || second.Extractions[0].SourceURI != "https://trello.com/c/card-added-later" {
		t.Fatalf("later-card sync = seen %d, extractions %#v; want the newly visible card", second.Job.ItemsSeen, second.Extractions)
	}
}

func TestFetchTrelloSourceReplaysAllCardsAtCursorTimestamp(t *testing.T) {
	const activity = "2026-07-12T00:00:00Z"
	activityTime, ok := parseTrelloTime(activity)
	if !ok {
		t.Fatalf("parse activity %q", activity)
	}
	initialCursor, err := encodeTrelloCursor(activityTime, map[string]struct{}{"card-already-seen": {}})
	if err != nil {
		t.Fatalf("encode initial cursor: %v", err)
	}
	cards := `[
		{"id":"card-already-seen","name":"Unchanged card","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"},
		{"id":"card-updated-at-boundary","name":"Updated card","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"},
		{"id":"card-new-at-boundary","name":"New card","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"}
	]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()
	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	source := newTrelloSource(uuid.New(), "abc123XY", initialCursor)
	items, nextCursor, err := fetchTrelloSource(t.Context(), source)
	if err != nil {
		t.Fatalf("fetch equal-time cards: %v", err)
	}
	gotIDs := make(map[string]struct{}, len(items))
	for _, item := range items {
		gotIDs[item.ExternalID] = struct{}{}
	}
	for _, wantID := range []string{"trello:card:card-already-seen", "trello:card:card-updated-at-boundary", "trello:card:card-new-at-boundary"} {
		if _, ok := gotIDs[wantID]; !ok {
			t.Errorf("equal-time items missing %q: %#v", wantID, gotIDs)
		}
	}
	if len(gotIDs) != 3 {
		t.Fatalf("equal-time items = %#v, want all three tied IDs replayed", gotIDs)
	}
	cursorIDs := assertTrelloCursor(t, nextCursor, activity)
	if len(cursorIDs) != 0 || !strings.HasPrefix(nextCursor, trelloCursorV2Prefix) {
		t.Fatalf("cursor IDs = %#v, cursor=%q; want bounded timestamp-only cursor", cursorIDs, nextCursor)
	}

	source.Cursor = nextCursor
	items, dedupedCursor, err := fetchTrelloSource(t.Context(), source)
	if err != nil {
		t.Fatalf("fetch after equal-time import: %v", err)
	}
	if len(items) != 3 || dedupedCursor != nextCursor {
		t.Fatalf("subsequent fetch = items %#v, cursor %q; want all tied cards replayed with unchanged cursor", items, dedupedCursor)
	}
}

func TestTrelloTimestampCursorIsBoundedRegardlessOfTieSize(t *testing.T) {
	activity, ok := parseTrelloTime("2026-07-12T00:00:00.123456789Z")
	if !ok {
		t.Fatal("parse cursor activity")
	}
	cardIDs := make(map[string]struct{}, 1000)
	for index := 0; index < 1000; index++ {
		cardIDs[fmt.Sprintf("%024x", index)] = struct{}{}
	}
	cursor, err := encodeTrelloCursor(activity, cardIDs)
	if err != nil {
		t.Fatalf("encode timestamp cursor: %v", err)
	}
	if len(cursor) > trelloCursorMaxLength {
		t.Fatalf("cursor length = %d, exceeds persistence limit %d", len(cursor), trelloCursorMaxLength)
	}
	if cursor != trelloCursorV2Prefix+activity.Format(time.RFC3339Nano) {
		t.Fatalf("cursor = %q, want timestamp-only v2 encoding", cursor)
	}
	if parsedActivity, ok := parseTrelloTime(cursor); !ok || !parsedActivity.Equal(activity) {
		t.Fatalf("parseTrelloTime(%q) = %s, %t; want cursor activity", cursor, parsedActivity, ok)
	}
	_, parsedIDs, ok, err := parseTrelloCursor(cursor)
	if err != nil || !ok || len(parsedIDs) != 0 {
		t.Fatalf("parse timestamp cursor: IDs=%d present=%t err=%v; want a present cursor without stored card IDs", len(parsedIDs), ok, err)
	}
}

func TestSyncTrelloLargeTimestampTieIsLosslessAndIncremental(t *testing.T) {
	const (
		tiedActivity  = "2026-09-24T12:00:00.123456789Z"
		olderActivity = "2026-09-24T11:59:59Z"
		newActivity   = "2026-09-24T12:00:01Z"
		tieCount      = 36
	)
	cards := make([]trelloCard, 0, tieCount+2)
	wantExternalIDs := make(map[string]struct{}, tieCount+2)
	for index := 0; index < tieCount; index++ {
		id := testTrelloMongoID(3000 - index)
		wantExternalIDs["trello:card:"+id] = struct{}{}
		cards = append(cards, trelloCard{
			ID: id, Name: "Tied card " + id, ShortURL: "https://trello.com/c/" + id,
			DateLastActivity: tiedActivity, IDList: "list-1",
		})
	}
	olderID := testTrelloMongoID(2900)
	wantExternalIDs["trello:card:"+olderID] = struct{}{}
	cards = append(cards, trelloCard{
		ID: olderID, Name: "Older card", ShortURL: "https://trello.com/c/" + olderID,
		DateLastActivity: olderActivity, IDList: "list-1",
	})
	encodeCards := func(values []trelloCard) string {
		t.Helper()
		payload, err := json.Marshal(values)
		if err != nil {
			t.Fatalf("marshal cards: %v", err)
		}
		return string(payload)
	}
	cardResponse := &atomic.Value{}
	cardResponse.Store(encodeCards(cards))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[{"id":"list-1","name":"Doing"}]`))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			if got := r.URL.Query().Get("filter"); got != "all" {
				t.Errorf("cards filter = %q, want all", got)
			}
			_, _ = w.Write(trelloTestCardPayload([]byte(cardResponse.Load().(string))))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Delivery board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	service := NewService(repo, &fakeSourceMemoryService{})
	assertStoredExternalIDs := func(stage string, expected map[string]struct{}) {
		t.Helper()
		raw, err := repo.FindRawItems(sourceID)
		if err != nil {
			t.Fatalf("%s FindRawItems: %v", stage, err)
		}
		got := make(map[string]struct{}, len(raw))
		for _, item := range raw {
			got[item.ExternalID] = struct{}{}
		}
		if len(raw) != len(expected) || len(got) != len(expected) {
			t.Fatalf("%s raw records=%d unique external IDs=%d; want %d without duplicates", stage, len(raw), len(got), len(expected))
		}
		for externalID := range expected {
			if _, exists := got[externalID]; !exists {
				t.Errorf("%s missing source card %q", stage, externalID)
			}
		}
	}
	first, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if first.Job.ItemsSeen != tieCount+1 || first.Job.ItemsAdded != tieCount+1 {
		t.Fatalf("initial sync counts = seen %d, added %d; want all %d cards", first.Job.ItemsSeen, first.Job.ItemsAdded, tieCount+1)
	}
	if len(first.Job.CursorAfter) > trelloCursorMaxLength || !strings.HasPrefix(first.Job.CursorAfter, trelloCursorV2Prefix) {
		t.Fatalf("initial cursor length=%d cursor=%q; want bounded timestamp-only v2", len(first.Job.CursorAfter), first.Job.CursorAfter)
	}
	if ids := assertTrelloCursor(t, first.Job.CursorAfter, tiedActivity); len(ids) != 0 {
		t.Fatalf("initial cursor contains per-card IDs: %#v", ids)
	}
	assertStoredExternalIDs("initial sync", wantExternalIDs)

	second, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("same-timestamp replay: %v", err)
	}
	if second.Job.ItemsSeen != 0 || second.Job.ItemsUpdated != 0 || second.Job.ItemsAdded != 0 {
		t.Fatalf("unchanged replay counts = seen %d, updated %d, added %d; want unchanged cards skipped", second.Job.ItemsSeen, second.Job.ItemsUpdated, second.Job.ItemsAdded)
	}
	assertStoredExternalIDs("same-timestamp replay", wantExternalIDs)

	newID := testTrelloMongoID(5000)
	wantExternalIDs["trello:card:"+newID] = struct{}{}
	cards = append([]trelloCard{{
		ID: newID, Name: "Later card", ShortURL: "https://trello.com/c/" + newID,
		DateLastActivity: newActivity, IDList: "list-1",
	}}, cards...)
	cardResponse.Store(encodeCards(cards))
	third, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("sync after high-water advances: %v", err)
	}
	if third.Job.ItemsSeen != 1 || third.Job.ItemsAdded != 1 || third.Job.ItemsUpdated != 0 {
		t.Fatalf("advanced sync counts = seen %d, added %d, updated %d; want only the new card", third.Job.ItemsSeen, third.Job.ItemsAdded, third.Job.ItemsUpdated)
	}
	if ids := assertTrelloCursor(t, third.Job.CursorAfter, newActivity); len(ids) != 0 {
		t.Fatalf("advanced cursor contains per-card IDs: %#v", ids)
	}
	assertStoredExternalIDs("advanced sync", wantExternalIDs)

	fourth, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("sync after high-water advances again: %v", err)
	}
	if fourth.Job.ItemsSeen != 0 || fourth.Job.ItemsAdded != 0 || fourth.Job.ItemsUpdated != 0 {
		t.Fatalf("post-advance sync counts = seen %d, added %d, updated %d; want unchanged cards skipped", fourth.Job.ItemsSeen, fourth.Job.ItemsAdded, fourth.Job.ItemsUpdated)
	}
	assertStoredExternalIDs("post-advance sync", wantExternalIDs)
}

func TestFetchTrelloSourcePagesCardsBeyondOnePageAndDeduplicatesBoundary(t *testing.T) {
	firstPage := make([]trelloCard, trelloCardPageSize)
	for index := range firstPage {
		id := testTrelloMongoID(2000 - index)
		firstPage[index] = trelloCard{
			ID: id, Name: "Card " + id,
			DateLastActivity: time.Unix(1_800_000_000+int64(2000-index), 0).UTC().Format(time.RFC3339),
			IDList:           "list-1",
		}
	}
	boundaryID := testTrelloMongoID(1001)
	firstPage[len(firstPage)-1].ID = strings.ToUpper(boundaryID)
	secondPage := []trelloCard{
		{ID: boundaryID, Name: "Inclusive boundary", DateLastActivity: firstPage[len(firstPage)-1].DateLastActivity, IDList: "list-1"},
		{ID: testTrelloMongoID(1000), Name: "Oldest card", DateLastActivity: "2026-01-01T00:00:00Z", IDList: "list-1"},
	}
	firstJSON, err := json.Marshal(firstPage)
	if err != nil {
		t.Fatalf("marshal first card page: %v", err)
	}
	secondJSON, err := json.Marshal(secondPage)
	if err != nil {
		t.Fatalf("marshal second card page: %v", err)
	}
	var cardQueries []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			cardQueries = append(cardQueries, r.URL.Query())
			if r.URL.Query().Get("before") == "" {
				_, _ = w.Write(trelloTestCardPayload(firstJSON))
				return
			}
			if r.URL.Query().Get("before") != testTrelloMongoID(1001) {
				http.Error(w, "unexpected card boundary", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(trelloTestCardPayload(secondJSON))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	items, cursor, err := fetchTrelloSource(t.Context(), newTrelloSource(uuid.New(), "abc123XY", ""))
	if err != nil {
		t.Fatalf("fetchTrelloSource: %v", err)
	}
	if len(items) != trelloCardPageSize+1 {
		t.Fatalf("items = %d, want %d unique cards", len(items), trelloCardPageSize+1)
	}
	boundaryExternalID := "trello:card:" + boundaryID
	boundaryCount := 0
	for _, item := range items {
		if item.ExternalID == boundaryExternalID {
			boundaryCount++
		}
		if item.ExternalID == "trello:card:"+strings.ToUpper(boundaryID) {
			t.Fatalf("card external ID retained noncanonical case: %q", item.ExternalID)
		}
	}
	if boundaryCount != 1 {
		t.Fatalf("boundary card occurrences = %d, want one canonical source identity", boundaryCount)
	}
	if len(cardQueries) != 2 || cardQueries[1].Get("before") != testTrelloMongoID(1001) {
		t.Fatalf("card pagination queries = %#v, want second request before the last Mongo ID", cardQueries)
	}
	if got := assertTrelloCursor(t, cursor, time.Unix(1_800_002_000, 0).UTC().Format(time.RFC3339)); len(got) != 0 || !strings.HasPrefix(cursor, trelloCursorV2Prefix) {
		t.Fatalf("latest cursor IDs = %#v, cursor=%q; want timestamp-only v2", got, cursor)
	}
}

func TestFetchTrelloSourcePagesMoreThan300CommentsAndDeduplicatesBoundary(t *testing.T) {
	cardID := testTrelloMongoID(9000)
	cardPayload, err := json.Marshal([]trelloCard{{ID: cardID, Name: "Long-running card", DateLastActivity: "2026-07-12T00:00:00Z", IDList: "list-1"}})
	if err != nil {
		t.Fatalf("marshal card fixture: %v", err)
	}
	makeAction := func(id int) trelloAction {
		return trelloAction{
			ID: testTrelloMongoID(id), Type: "commentCard",
			Date:          time.Unix(1_800_000_000+int64(id), 0).UTC().Format(time.RFC3339),
			Data:          trelloActionData{Text: fmt.Sprintf("Comment %d", id), Card: trelloActionCard{ID: cardID}},
			MemberCreator: trelloMember{FullName: "Reviewer"},
		}
	}
	pageOne := make([]trelloAction, trelloActionPageSize)
	for index := range pageOne {
		pageOne[index] = makeAction(2000 - index)
	}
	boundaryActionID := testTrelloMongoID(1001)
	pageOne[len(pageOne)-1].ID = strings.ToUpper(boundaryActionID)
	pageTwo := []trelloAction{makeAction(1001), makeAction(1000)}
	pageTwo[0].Data.Card.ID = strings.ToUpper(cardID)
	pagePayload := func(value []trelloAction) []byte {
		payload, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatalf("marshal actions: %v", marshalErr)
		}
		return payload
	}
	var actionQueries []url.Values
	var cardQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			cardQuery = r.URL.Query()
			_, _ = w.Write(trelloTestCardPayload(cardPayload))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			query := r.URL.Query()
			actionQueries = append(actionQueries, query)
			switch query.Get("before") {
			case "":
				_, _ = w.Write(pagePayload(pageOne))
			case testTrelloMongoID(1001):
				_, _ = w.Write(pagePayload(pageTwo))
			default:
				http.Error(w, "unexpected action boundary", http.StatusBadRequest)
			}
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	items, _, err := fetchTrelloSource(t.Context(), newTrelloSource(uuid.New(), "abc123XY", ""))
	if err != nil {
		t.Fatalf("fetchTrelloSource: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want one card", len(items))
	}
	content := items[0].Content
	if !strings.Contains(content, "Comments (1001):") || !strings.Contains(content, "Comment 1000") || !strings.Contains(content, "Comment 2000") {
		if len(content) > 300 {
			content = content[:300]
		}
		t.Fatalf("comment extraction did not include all 1001 comments: %q", content)
	}
	if strings.Contains(cardQuery.Encode(), "actions=") || cardQuery.Get("limit") != strconv.Itoa(trelloCardPageSize) {
		t.Fatalf("card query included actions or wrong limit: %v", cardQuery)
	}
	if len(actionQueries) != 2 || actionQueries[1].Get("before") != testTrelloMongoID(1001) {
		t.Fatalf("action queries = %#v, want two pages using before boundary", actionQueries)
	}
	for _, query := range actionQueries {
		if query.Get("filter") != trelloCommentActionTypes || query.Get("limit") != strconv.Itoa(trelloActionPageSize) {
			t.Fatalf("action query = %v, want bounded comment-action page", query)
		}
	}
}

func TestTrelloRateLimitRetriesAndRecoversWithinBound(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "provider details must not leak", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.getJSON(t.Context(), "/1/test", nil, &response); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if !response.OK || attempts != 2 || client.requestsUsed != 2 {
		t.Fatalf("response=%+v attempts=%d requests=%d, want success after one bounded retry", response, attempts, client.requestsUsed)
	}
}

func TestTrelloRateLimitHeadersDeferNextRequest(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-rate-limit-api-token-remaining", "1")
		w.Header().Set("x-rate-limit-api-token-interval-ms", "10000")
		w.Header().Set("x-rate-limit-api-key-remaining", "0")
		w.Header().Set("x-rate-limit-api-key-interval-ms", "12500")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.getJSON(t.Context(), "/1/first", nil, &response); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if !response.OK {
		t.Fatal("first response was not decoded")
	}
	err = client.getJSON(t.Context(), "/1/second", nil, &response)
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 13*time.Second {
		t.Fatalf("second request error = %#v, want deferred retry after longest exhausted window rounded up", err)
	}
	if attempts != 1 || client.requestsUsed != 1 {
		t.Fatalf("attempts=%d requests=%d, want no second provider request", attempts, client.requestsUsed)
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Fatalf("deferred error exposed credentials: %v", err)
	}
}

func TestTrelloRateLimit429UsesLongestProviderWindow(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.Header().Set("x-rate-limit-api-token-remaining", "0")
		w.Header().Set("x-rate-limit-api-token-interval-ms", "10000")
		http.Error(w, "provider details must not leak", http.StatusTooManyRequests)
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	var response any
	err = client.getJSON(t.Context(), "/1/test", nil, &response)
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 10*time.Second {
		t.Fatalf("rate-limit error = %#v, want retry after the advertised 10-second window", err)
	}
	if attempts != 1 || client.requestsUsed != 1 {
		t.Fatalf("attempts=%d requests=%d, want no immediate retry despite Retry-After: 0", attempts, client.requestsUsed)
	}
}

func TestTrelloServerErrorPreservesRetryAfter(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "4")
		http.Error(w, "provider details must not leak", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	var response any
	err = client.getJSON(t.Context(), "/1/test", nil, &response)
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 4*time.Second {
		t.Fatalf("server error = %#v, want transient provider error with four-second retry", err)
	}
	if attempts != 1 || client.requestsUsed != 1 {
		t.Fatalf("attempts=%d requests=%d, want durable retry rather than immediate retry", attempts, client.requestsUsed)
	}
	if strings.Contains(err.Error(), "provider details") {
		t.Fatalf("error exposed provider body: %v", err)
	}
}

func TestTrelloTokenEndpointEscapesOpaqueTokenExactlyOnce(t *testing.T) {
	const token = "test/token?with%reserved"
	const wantEscapedPath = "/1/tokens/test%2Ftoken%3Fwith%25reserved"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if got := r.URL.EscapedPath(); got != wantEscapedPath {
			t.Errorf("escaped path = %q, want %q", got, wantEscapedPath)
		}
		if r.URL.Query().Has("token") || r.URL.Query().Has("key") {
			t.Errorf("query contains credentials: %q", r.URL.RawQuery)
		}
		if got, want := r.Header.Get("Authorization"), trelloAuthorizationHeader("test-key", token); got != want {
			t.Errorf("authorization = %q, want escaped OAuth credentials", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: token}

	if err := validateTrelloReadOnlyToken(t.Context(), client); err != nil {
		t.Fatalf("validateTrelloReadOnlyToken: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one", requests)
	}
}

func TestTrelloRateLimitExhaustionIsBoundedAndDoesNotExposeResponseBody(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		http.Error(w, "token=body-secret-provider-detail", http.StatusTooManyRequests)
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	var response any
	const pathToken = "endpoint-path-secret"
	err = client.getJSON(t.Context(), "/1/tokens/"+pathToken, nil, &response)
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("getJSON error = %v, want bounded rate-limit error", err)
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 0 {
		t.Fatalf("rate-limit error = %#v, want retryable provider error with zero delay", err)
	}
	if attempts != trelloRateLimitMaxRetries+1 || client.requestsUsed != attempts {
		t.Fatalf("attempts=%d requests=%d, want %d attempts", attempts, client.requestsUsed, trelloRateLimitMaxRetries+1)
	}
	if strings.Contains(err.Error(), "body-secret") || strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), pathToken) {
		t.Fatalf("rate-limit error exposed sensitive response or credential data: %v", err)
	}
}

func TestTrelloRateLimitHonorsRetryAfterWithinConservativeWaitBudget(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "3")
		http.Error(w, "retry later", http.StatusTooManyRequests)
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	started := time.Now()
	var response any
	err = client.getJSON(t.Context(), "/1/test", nil, &response)
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("getJSON error = %v, want bounded retry-wait error", err)
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 3*time.Second {
		t.Fatalf("rate-limit error = %#v, want retry after 3 seconds", err)
	}
	if attempts != 1 || time.Since(started) > time.Second {
		t.Fatalf("attempts=%d elapsed=%s, want no retry or long wait", attempts, time.Since(started))
	}
}

func TestTrelloRateLimitUsesProviderWindowWhenRetryAfterIsAbsent(t *testing.T) {
	if got := trelloRetryAfterHeader(""); got != "10" {
		t.Fatalf("retry-after fallback = %q, want 10 seconds", got)
	}
	if got := trelloRetryAfterHeader("invalid"); got != "10" {
		t.Fatalf("invalid retry-after fallback = %q, want 10 seconds", got)
	}
	if got := trelloRetryAfterHeader("86401"); got != "86401" {
		t.Fatalf("long provider delay = %q, want preserved provider value", got)
	}
}

func TestTrelloRateLimitWithoutRetryAfterDefersToProviderWindow(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "provider details must not leak", http.StatusTooManyRequests)
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	var response any
	err = client.getJSON(t.Context(), "/1/test", nil, &response)
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("getJSON error = %v, want retryable rate-limit error", err)
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 10*time.Second {
		t.Fatalf("rate-limit error = %#v, want retryable error deferred for 10 seconds", err)
	}
	if attempts != 1 || client.requestsUsed != 1 {
		t.Fatalf("attempts=%d requests=%d, want no immediate retry without Retry-After", attempts, client.requestsUsed)
	}
}

func TestSyncTrelloRateLimitWithoutRetryAfterPreservesCursor(t *testing.T) {
	const existingCursor = "2026-07-01T00:00:00Z"
	cardID := testTrelloMongoID(9000)
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write(trelloTestCardPayload([]byte(fmt.Sprintf(`[{"id":%q,"name":"Card","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"}]`, cardID))))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			attempts++
			http.Error(w, "provider response must not leak", http.StatusTooManyRequests)
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	if _, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync}); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("Sync error = %v, want bounded rate-limit failure", err)
	}
	if attempts != 1 {
		t.Fatalf("rate-limit attempts = %d, want one attempt before durable retry", attempts)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor {
		t.Fatalf("source cursor = %q, want unchanged %q after rate-limit exhaustion", stored.Cursor, existingCursor)
	}
}

func TestTrelloRequestBudgetStopsBeforeSendingAnotherRequest(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token", requestsUsed: trelloMaxRequestsPerSync}
	var response map[string]any
	err = client.getJSON(t.Context(), "/1/test", nil, &response)
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || providerErr.Retryable() || !strings.Contains(err.Error(), "request budget") || attempts != 0 {
		t.Fatalf("getJSON error=%v attempts=%d, want budget rejection before I/O", err, attempts)
	}
}

func TestTrelloRequestRejectsNilContextBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	var response map[string]any
	err = trelloGetJSONContextWithRateLimitState(nil, base, "test-key", "test-token", "/1/test", nil, &response, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil-context request error = %v, want context validation failure", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want no network request for a nil context", requests)
	}
	if _, _, err := fetchTrelloSourceSnapshot(nil, nil, nil, false); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil-context source fetch error = %v, want context validation failure", err)
	}
}

func TestTrelloCredentialsUseAuthorizationHeaderAndNeverQuery(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want GET", r.Method)
		}
		if r.URL.Query().Has("key") || r.URL.Query().Has("token") {
			t.Errorf("request query exposed credentials: %q", r.URL.RawQuery)
		}
		if got, want := r.Header.Get("Authorization"), trelloAuthorizationHeader("test-key", "test-token"); got != want {
			t.Errorf("Authorization = %q, want documented OAuth credentials", got)
		}
		if got := r.URL.Query().Get("fields"); got != "name" {
			t.Errorf("fields query = %q, want preserved caller parameter", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	query := url.Values{"fields": {"name"}, "key": {"caller-key"}, "token": {"caller-token"}}
	var response map[string]any
	if err := trelloGetJSONContext(t.Context(), base, "test-key", "test-token", "/1/test", query, &response); err != nil {
		t.Fatalf("trelloGetJSONContext: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one", requests)
	}
	if query.Get("key") != "caller-key" || query.Get("token") != "caller-token" {
		t.Fatalf("caller query was mutated: %v", query)
	}
}

func TestTrelloRequestRespectsCallerCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want GET", r.Method)
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		var response map[string]any
		result <- client.getJSON(ctx, "/1/test", nil, &response)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach the local test server")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("getJSON succeeded after its caller canceled the request")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("getJSON did not stop after caller cancellation")
	}
}

func TestTrelloRequestAppliesConfiguredHTTPTimeout(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_TIMEOUT_SECONDS", "1")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want GET", r.Method)
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	var response map[string]any
	startedAt := time.Now()
	err = client.getJSON(context.Background(), "/1/test", nil, &response)
	if err == nil {
		t.Fatal("getJSON succeeded without a response from the stalled local server")
	}
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second {
		t.Fatalf("request timeout took %s, want the configured one-second bound", elapsed)
	}
	select {
	case <-started:
	default:
		t.Fatal("timed-out request never reached the local test server")
	}
}

func TestTrelloRequestDoesNotFollowRedirects(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want GET", r.Method)
		}
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/1/start" {
			http.Redirect(w, r, "/1/redirect-target", http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
	var response map[string]any
	if err := client.getJSON(context.Background(), "/1/start", nil, &response); err == nil {
		t.Fatal("getJSON accepted a redirect response")
	}
	if len(paths) != 1 || paths[0] != "/1/start" {
		t.Fatalf("request paths = %#v, want exactly the original GET without following the redirect", paths)
	}
}

func TestTrelloAuthorizationFailuresDistinguishCredentialsFromResourceAccess(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		publicMessage string
	}{
		{name: "unauthorized credentials", status: http.StatusUnauthorized, publicMessage: "rejected the configured API key or token"},
		{name: "forbidden board access", status: http.StatusForbidden, publicMessage: "denied access to this board or resource"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private-provider-body-secret", test.status)
			}))
			defer server.Close()
			t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
			t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
			base, err := url.Parse(server.URL)
			if err != nil {
				t.Fatalf("parse test URL: %v", err)
			}
			var response map[string]any
			err = trelloGetJSONContext(t.Context(), base, "test-api-key", "test-read-token", "/1/boards/abc123XY", nil, &response)
			var providerErr *providerSyncError
			if !errors.As(err, &providerErr) || providerErr.upstreamStatus != test.status {
				t.Fatalf("provider error = %#v, want upstream status %d", err, test.status)
			}
			if !strings.Contains(err.Error(), test.publicMessage) || strings.Contains(err.Error(), "private-provider-body-secret") ||
				strings.Contains(err.Error(), "test-api-key") || strings.Contains(err.Error(), "test-read-token") {
				t.Fatalf("public error = %q, want safe diagnostic containing %q", err, test.publicMessage)
			}
		})
	}
}

func TestTrelloAuthorizationHeaderEscapesQuotedCredentials(t *testing.T) {
	got := trelloAuthorizationHeader(`key"value`, `token\value`)
	want := `OAuth oauth_consumer_key="key\"value", oauth_token="token\\value"`
	if got != want {
		t.Fatalf("authorization header = %q, want %q", got, want)
	}
}

func TestSyncTrelloDoesNotAdvanceCursorWhenLaterCardPageIsTruncated(t *testing.T) {
	const existingCursor = "2026-07-01T00:00:00Z"
	firstPage := make([]trelloCard, trelloCardPageSize)
	for index := range firstPage {
		id := testTrelloMongoID(5000 - index)
		firstPage[index] = trelloCard{ID: id, Name: "Card", DateLastActivity: "2026-07-12T00:00:00Z", IDList: "list-1"}
	}
	firstPayload, err := json.Marshal(firstPage)
	if err != nil {
		t.Fatalf("marshal cards: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			if r.URL.Query().Get("before") == "" {
				_, _ = w.Write(trelloTestCardPayload(firstPayload))
				return
			}
			_, _ = w.Write([]byte(`[{"id":"truncated`))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	if _, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync}); err == nil || !strings.Contains(err.Error(), "could not be processed") {
		t.Fatalf("Sync error = %v, want a safe provider failure for malformed later page", err)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor {
		t.Fatalf("source cursor = %q, want unchanged %q after incomplete page", stored.Cursor, existingCursor)
	}
}

func TestSyncTrelloRetryableLaterCardPageFailureRetainsCursorAndSkipsPartialImports(t *testing.T) {
	const existingCursor = trelloCursorV2Prefix + "2026-07-01T00:00:00Z"
	firstPage := trelloTestFullCardPagePayload(t, 2000)
	var cardPageAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			if cardPageAttempts.Add(1) == 1 {
				if r.URL.Query().Get("before") != "" {
					t.Errorf("first card page before = %q, want no boundary", r.URL.Query().Get("before"))
				}
				_, _ = w.Write(firstPage)
				return
			}
			if r.URL.Query().Get("before") != testTrelloMongoID(1001) {
				t.Errorf("second card page before = %q, want last first-page ID", r.URL.Query().Get("before"))
			}
			w.Header().Set("Retry-After", "4")
			http.Error(w, "upstream details must not leak", http.StatusServiceUnavailable)
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil {
		t.Fatal("Sync unexpectedly succeeded after a retryable second-page failure")
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 4*time.Second {
		t.Fatalf("later-page error = %#v, want retryable 503 preserving Retry-After", err)
	}
	if got := cardPageAttempts.Load(); got != 2 {
		t.Fatalf("card page attempts = %d, want one initial page and one deferred-retry failure", got)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor {
		t.Fatalf("source cursor = %q, want unchanged %q after later-page failure", stored.Cursor, existingCursor)
	}
	if len(repo.rawItems) != 0 {
		t.Fatalf("raw source items = %d, want no partial imports from an incomplete snapshot", len(repo.rawItems))
	}
}

func TestSyncTrelloCancellationAfter429RetainsCursorAndDoesNotRetry(t *testing.T) {
	const existingCursor = trelloCursorV2Prefix + "2026-07-01T00:00:00Z"
	rateLimited := make(chan struct{})
	var actionAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			cards := []trelloCard{{
				ID: testTrelloMongoID(9000), IDBoard: testTrelloCanonicalBoardID,
				Name: "Card", DateLastActivity: "2026-07-12T00:00:00Z", IDList: "list-1",
			}}
			payload, marshalErr := json.Marshal(cards)
			if marshalErr != nil {
				t.Errorf("marshal card: %v", marshalErr)
				http.Error(w, "fixture error", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(payload)
		case strings.HasSuffix(r.URL.Path, "/actions"):
			if actionAttempts.Add(1) == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				close(rateLimited)
				return
			}
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limited again", http.StatusTooManyRequests)
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	service := NewService(repo, &fakeSourceMemoryService{})
	contextService, ok := service.(interface {
		SyncContext(context.Context, uuid.UUID, ImportRequest) (*SyncResult, error)
	})
	if !ok {
		t.Fatal("source service does not expose its cancellable sync entry point")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, syncErr := contextService.SyncContext(ctx, sourceID, ImportRequest{Mode: ModeIncrementalSync})
		result <- syncErr
	}()
	select {
	case <-rateLimited:
	case <-time.After(3 * time.Second):
		t.Fatal("Trello action request did not reach the local rate-limit fixture")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "interrupted") {
			t.Fatalf("SyncContext error = %v, want cancellation surfaced as an interrupted provider request", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SyncContext did not return after cancellation of the rate-limited request")
	}
	if got := actionAttempts.Load(); got != 1 {
		t.Fatalf("action attempts = %d, want no retry after caller cancellation", got)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor {
		t.Fatalf("source cursor = %q, want unchanged %q after cancellation", stored.Cursor, existingCursor)
	}
	if len(repo.rawItems) != 0 {
		t.Fatalf("raw source items = %d, want no partial imports after cancellation", len(repo.rawItems))
	}
}

func TestSyncTrelloRejectsUnstableIDOnLaterCardPageWithoutAdvancingCursor(t *testing.T) {
	const existingCursor = trelloCursorV2Prefix + "2026-07-01T00:00:00Z"
	firstPage := trelloTestFullCardPagePayload(t, 2000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			if r.URL.Query().Get("before") == "" {
				_, _ = w.Write(firstPage)
				return
			}
			page := []trelloCard{
				{ID: testTrelloMongoID(1001), IDBoard: testTrelloCanonicalBoardID, Name: "Inclusive boundary", DateLastActivity: "2026-07-12T00:00:00Z"},
				{ID: "unstable-provider-id", IDBoard: testTrelloCanonicalBoardID, Name: "Bad identity", DateLastActivity: "2026-07-12T00:00:00Z"},
			}
			payload, marshalErr := json.Marshal(page)
			if marshalErr != nil {
				t.Errorf("marshal invalid-ID page: %v", marshalErr)
				http.Error(w, "fixture error", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(payload)
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "invalid Trello ID") {
		t.Fatalf("Sync error = %v, want fail-closed later-page ID validation", err)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor {
		t.Fatalf("source cursor = %q, want unchanged %q after unstable ID", stored.Cursor, existingCursor)
	}
	if len(repo.rawItems) != 0 {
		t.Fatalf("raw source items = %d, want no partial imports after unstable ID", len(repo.rawItems))
	}
}

func TestSyncTrelloDoesNotAdvanceCursorWhenLaterCommentPageIsTruncated(t *testing.T) {
	const existingCursor = "2026-07-01T00:00:00Z"
	cardID := testTrelloMongoID(9000)
	page := make([]trelloAction, trelloActionPageSize)
	for index := range page {
		page[index] = trelloAction{
			ID: testTrelloMongoID(8000 - index), Type: "commentCard", Date: "2026-07-12T00:00:00Z",
			Data: trelloActionData{Text: "Comment", Card: trelloActionCard{ID: cardID}},
		}
	}
	pagePayload, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal actions: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write(trelloTestCardPayload([]byte(fmt.Sprintf(`[{"id":%q,"name":"Card","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"}]`, cardID))))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			if r.URL.Query().Get("before") == "" {
				_, _ = w.Write(pagePayload)
				return
			}
			_, _ = w.Write([]byte(`[{"id":"truncated`))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	if _, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync}); err == nil || !strings.Contains(err.Error(), "could not be processed") {
		t.Fatalf("Sync error = %v, want a safe provider failure for malformed comment page", err)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor {
		t.Fatalf("source cursor = %q, want unchanged %q after incomplete comments", stored.Cursor, existingCursor)
	}
}

func TestSyncTrelloFailsClosedWhenAggregateCardAndActionRecordCapIsExceeded(t *testing.T) {
	const existingCursor = "2026-07-01T00:00:00Z"
	cardID := testTrelloMongoID(30000)
	cardPayload := fmt.Sprintf(`[{"id":%q,"name":"Card","dateLastActivity":"2026-07-12T00:00:00Z","idList":"list-1"}]`, cardID)
	actionRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write(trelloTestCardPayload([]byte(cardPayload)))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			page := make([]trelloAction, trelloActionPageSize)
			pageStart := 20000 - actionRequests*trelloActionPageSize
			for index := range page {
				page[index] = trelloAction{
					ID: testTrelloMongoID(pageStart - index), Type: "commentCard", Date: "2026-07-12T00:00:00Z",
					Data: trelloActionData{Text: "Comment", Card: trelloActionCard{ID: cardID}},
				}
			}
			actionRequests++
			payload, err := json.Marshal(page)
			if err != nil {
				t.Errorf("marshal action page: %v", err)
				http.Error(w, "test fixture failure", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(payload)
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "aggregate record safety cap") {
		t.Fatalf("Sync error = %v, want aggregate card/action record-cap failure", err)
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || providerErr.Retryable() {
		t.Fatalf("record-cap error = %#v, want a permanent classified failure", err)
	}
	if actionRequests != trelloMaxRecordsPerSync/trelloActionPageSize {
		t.Fatalf("action pages read = %d, want cap to reject the tenth page before importing it", actionRequests)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor {
		t.Fatalf("source cursor = %q, want unchanged %q after aggregate record-cap failure", stored.Cursor, existingCursor)
	}
	if len(repo.rawItems) != 0 {
		t.Fatalf("raw source items = %d, want no partial imports after aggregate record-cap failure", len(repo.rawItems))
	}
}

func TestSyncTrelloFailsClosedWhenBoardListRecordCapIsExceeded(t *testing.T) {
	const existingCursor = "2026-07-01T00:00:00Z"
	lists := make([]trelloList, trelloMaxRecordsPerSync+1)
	for index := range lists {
		lists[index] = trelloList{ID: fmt.Sprintf("list-%d", index), Name: "List"}
	}
	listPayload, err := json.Marshal(lists)
	if err != nil {
		t.Fatalf("marshal list fixture: %v", err)
	}
	cardRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write(listPayload)
		case strings.HasSuffix(r.URL.Path, "/cards"):
			cardRequests++
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	_, err = NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "aggregate record safety cap") {
		t.Fatalf("Sync error = %v, want aggregate list record-cap failure", err)
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || providerErr.Retryable() {
		t.Fatalf("list-cap error = %#v, want a permanent classified failure", err)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor || cardRequests != 0 || len(repo.rawItems) != 0 {
		t.Fatalf("cursor=%q card requests=%d raw items=%d; want unchanged cursor, no card fetch, and no partial imports", stored.Cursor, cardRequests, len(repo.rawItems))
	}
}

func TestSyncTrelloFailsClosedWhenAggregatePageJSONByteCapIsExceeded(t *testing.T) {
	const existingCursor = "2026-07-01T00:00:00Z"
	t.Setenv("CONNECTED_SOURCE_HTTP_MAX_BYTES", "18874368")
	// Surround a valid empty array with whitespace so the limit proves it counts
	// bytes read from the provider, not only bytes retained by JSON decoding.
	cardPayload := strings.Repeat(" ", int(trelloMaxPageBytesPerSync)) + "[]"
	if int64(len(cardPayload)) <= trelloMaxPageBytesPerSync || int64(len(cardPayload)) > sourceHTTPMaxBytes() {
		t.Fatalf("fixture bytes = %d; want greater than aggregate cap %d and within per-response cap %d", len(cardPayload), trelloMaxPageBytesPerSync, sourceHTTPMaxBytes())
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write([]byte(cardPayload))
		case strings.HasSuffix(r.URL.Path, "/actions"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Board"))
		}
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", existingCursor))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "aggregate page JSON safety cap") {
		t.Fatalf("Sync error = %v, want aggregate page JSON byte-cap failure", err)
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || providerErr.Retryable() {
		t.Fatalf("page-byte-cap error = %#v, want a permanent classified failure", err)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != existingCursor {
		t.Fatalf("source cursor = %q, want unchanged %q after aggregate byte-cap failure", stored.Cursor, existingCursor)
	}
	if len(repo.rawItems) != 0 {
		t.Fatalf("raw source items = %d, want no partial imports after aggregate byte-cap failure", len(repo.rawItems))
	}
}

func TestSyncTrelloRequiresCredentials(t *testing.T) {
	server, _, _ := trelloTestServer(t, `[]`)
	defer server.Close()
	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAPIKeyEnv, "")
	t.Setenv(trelloReadTokenEnv, "")

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v, want a not-configured credential error", err)
	}
}

func TestSyncTrelloRejectsWriteCapableToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/1/tokens/") {
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":true}]}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/lists"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write(trelloTestBoardJSON("Client Delivery"))
		}
	}))
	defer server.Close()

	t.Setenv(trelloBaseURLEnv, server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "write-capable-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "write permission") {
		t.Fatalf("Sync error = %v, want write-capable token rejection", err)
	}
}

func TestSyncTrelloRejectsCallerSuppliedItems(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{
		Mode: ModeIncrementalSync,
		Items: []ImportItem{{
			ExternalID: "trello:card:unverified",
			Title:      "Unverified item",
			Content:    "This must not be accepted as live Trello data.",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "read only from the configured Trello API") {
		t.Fatalf("Sync error = %v, want caller-supplied item rejection", err)
	}
}

func TestSyncTrelloRejectsUnallowlistedHost(t *testing.T) {
	t.Setenv(trelloBaseURLEnv, "https://trello.example.net")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(newTrelloSource(sourceID, "abc123XY", ""))
	_, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("error = %v, want allowlist rejection", err)
	}
}

func TestTrelloBaseURLRejectsOtherAllowlistedRemoteHost(t *testing.T) {
	t.Setenv(trelloBaseURLEnv, "https://api.github.com")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "api.github.com,api.trello.com")

	if _, err := trelloBaseURL(); err == nil || !strings.Contains(err.Error(), "Trello credentials may only be sent") {
		t.Fatalf("trelloBaseURL error = %v, want non-Trello credential destination rejection", err)
	}
}

func TestTrelloBaseURLRequiresHTTPSOutsideLoopback(t *testing.T) {
	t.Setenv(trelloBaseURLEnv, "http://api.trello.com")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "api.trello.com")

	if _, err := trelloBaseURL(); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("trelloBaseURL error = %v, want cleartext remote endpoint rejection", err)
	}
}

func TestTrelloBaseURLRejectsNonstandardRemotePort(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "api.trello.com")
	for _, rawURL := range []string{"https://api.trello.com:8443", "https://api.trello.com:444"} {
		t.Run(rawURL, func(t *testing.T) {
			t.Setenv(trelloBaseURLEnv, rawURL)
			if _, err := trelloBaseURL(); err == nil || !strings.Contains(err.Error(), "HTTPS port 443") {
				t.Fatalf("trelloBaseURL(%q) error = %v, want rejection for nonstandard remote port", rawURL, err)
			}
		})
	}

	t.Setenv(trelloBaseURLEnv, "https://api.trello.com:443")
	if _, err := trelloBaseURL(); err != nil {
		t.Fatalf("explicit HTTPS port 443 should be accepted: %v", err)
	}
}

func TestTrelloBaseURLAllowsHTTPForLoopbackTestEndpoint(t *testing.T) {
	t.Setenv(trelloBaseURLEnv, "http://127.0.0.1:12345")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", "127.0.0.1:12345")

	base, err := trelloBaseURL()
	if err != nil {
		t.Fatalf("trelloBaseURL loopback endpoint: %v", err)
	}
	if base.Scheme != "http" || base.Hostname() != "127.0.0.1" {
		t.Fatalf("loopback base URL = %s, want HTTP 127.0.0.1", base)
	}
}

func TestTrelloBaseURLRejectsCredentialsQueryAndFragments(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	for _, rawURL := range []string{
		"http://user:password@127.0.0.1:12345",
		"http://127.0.0.1:12345?token=secret",
		"http://127.0.0.1:12345?",
		"http://127.0.0.1:12345#fragment",
	} {
		t.Run(rawURL, func(t *testing.T) {
			t.Setenv(trelloBaseURLEnv, rawURL)
			if _, err := trelloBaseURL(); err == nil {
				t.Fatalf("trelloBaseURL(%q) unexpectedly accepted an unsafe endpoint", rawURL)
			}
		})
	}
}

func TestTrelloConfiguredRejectsControlCharactersInHeaderCredentials(t *testing.T) {
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, testTrelloAccountMemberID)
	t.Setenv(trelloAPIKeyEnv, "safe-key")
	t.Setenv(trelloReadTokenEnv, "safe-token")
	if !trelloConfigured() {
		t.Fatal("valid Trello read credentials should configure the connector")
	}

	for _, test := range []struct {
		name string
		key  string
		tok  string
	}{
		{name: "embedded newline in key", key: "safe\nkey", tok: "safe-token"},
		{name: "embedded carriage return in token", key: "safe-key", tok: "safe\rtoken"},
		{name: "delete control in token", key: "safe-key", tok: "safe\x7ftoken"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(trelloAPIKeyEnv, test.key)
			t.Setenv(trelloReadTokenEnv, test.tok)
			if trelloConfigured() {
				t.Fatal("Trello credentials containing HTTP header control characters were accepted")
			}
		})
	}
}

func TestTrelloBoardID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare shortLink", in: "abc123XY", want: "abc123XY"},
		{name: "bare 24-character board id", in: "0123456789abcdef01234567", want: "0123456789abcdef01234567"},
		{name: "canonical board URL", in: "https://trello.com/b/abc123XY/client-board", want: "abc123XY"},
		{name: "www Trello board URL", in: "https://www.trello.com/b/abc123XY", want: "abc123XY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := trelloBoardID(tc.in)
			if err != nil || got != tc.want {
				t.Fatalf("trelloBoardID(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestTrelloBoardIDRejectsUnsafeOrNoncanonicalTargetsWithoutEchoingInput(t *testing.T) {
	const secret = "private-query-secret"
	cases := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "invalid bare identifier", in: "has spaces"},
		{name: "bare identifier longer than a shortLink", in: "abc123XYZ"},
		{name: "bare 24-character non-hex id", in: "abcdefghijklmnopqrstuvwx"},
		{name: "bare identifier longer than a Trello id", in: "abcdefghijklmnopqrstuvwxABCDEFGH"},
		{name: "URL with non-shortLink board id", in: "https://trello.com/b/abc123XYZ"},
		{name: "non-Trello host", in: "https://evil.example/b/abc123XY"},
		{name: "Trello host suffix", in: "https://trello.com.evil.example/b/abc123XY"},
		{name: "Trello host in userinfo", in: "https://trello.com@evil.example/b/abc123XY"},
		{name: "userinfo", in: "https://user:pass@trello.com/b/abc123XY"},
		{name: "query", in: "https://trello.com/b/abc123XY/client-board?token=" + secret},
		{name: "empty query", in: "https://trello.com/b/abc123XY?"},
		{name: "fragment", in: "https://trello.com/b/abc123XY#" + secret},
		{name: "empty fragment", in: "https://trello.com/b/abc123XY#"},
		{name: "unexpected path prefix", in: "https://trello.com/account/b/abc123XY"},
		{name: "unexpected path suffix", in: "https://trello.com/b/abc123XY/client-board/extra"},
		{name: "missing board id", in: "https://trello.com/b//client-board"},
		{name: "empty slug", in: "https://trello.com/b/abc123XY/"},
		{name: "empty path segment", in: "https://trello.com/b/abc123XY//client-board"},
		{name: "escaped board id", in: "https://trello.com/b/%61bc123XY"},
		{name: "escaped path separator", in: "https://trello.com/b/abc123XY/client%2Fboard"},
		{name: "non-board route", in: "https://trello.com/c/abc123XY"},
		{name: "insecure scheme", in: "http://trello.com/b/abc123XY"},
		{name: "noncanonical port", in: "https://trello.com:443/b/abc123XY"},
		{name: "malformed escaped path", in: "https://trello.com/b/%zz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := trelloBoardID(tc.in); err == nil {
				t.Fatalf("trelloBoardID(%q) unexpectedly accepted an unsafe target", tc.in)
			} else if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "user:pass") || strings.Contains(err.Error(), "%zz") {
				t.Fatalf("validation error exposed input data: %v", err)
			}
		})
	}
}

func TestCanonicalTrelloBoardIDForTargetBindsProviderMetadata(t *testing.T) {
	const boardID = "0123456789abcdef01234567"
	tests := []struct {
		name        string
		target      string
		board       trelloBoard
		wantID      string
		wantErrText string
	}{
		{
			name:   "canonical ID matches",
			target: boardID,
			board:  trelloBoard{ID: boardID},
			wantID: boardID,
		},
		{
			name:        "canonical ID mismatch",
			target:      boardID,
			board:       trelloBoard{ID: "0123456789abcdef01234568"},
			wantErrText: "does not match",
		},
		{
			name:   "short link matches board URL",
			target: "abc123XY",
			board: trelloBoard{
				ID:       boardID,
				ShortURL: "https://trello.com/b/abc123XY/example-board",
			},
			wantID: boardID,
		},
		{
			name:        "short link mismatch",
			target:      "abc123XY",
			board:       trelloBoard{ID: boardID, ShortURL: "https://trello.com/b/def456GH/example-board"},
			wantErrText: "does not match",
		},
		{
			name:        "short link cannot be bound without board URL",
			target:      "abc123XY",
			board:       trelloBoard{ID: boardID},
			wantErrText: "does not match",
		},
		{
			name:        "malformed canonical board ID",
			target:      boardID,
			board:       trelloBoard{ID: "provider-board-id"},
			wantErrText: "omitted its canonical ID",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := canonicalTrelloBoardIDForTarget(test.target, test.board)
			if test.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErrText) {
					t.Fatalf("identity validation error = %v, want text %q", err, test.wantErrText)
				}
				return
			}
			if err != nil || got != test.wantID {
				t.Fatalf("canonicalTrelloBoardIDForTarget() = %q, %v; want %q", got, err, test.wantID)
			}
		})
	}
}

func TestFetchTrelloSourceRejectsQuerySecretBeforeProviderRequest(t *testing.T) {
	const secret = "private-query-secret"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	target := "https://trello.com/b/abc123XY/client-board?token=" + secret
	_, _, err := fetchTrelloSource(t.Context(), newTrelloSource(uuid.New(), target, ""))
	if err == nil {
		t.Fatal("fetchTrelloSource unexpectedly accepted a target containing a query secret")
	}
	if requests != 0 {
		t.Fatalf("provider requests = %d, want none for an invalid board target", requests)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error exposed query secret: %v", err)
	}
}

func TestFetchTrelloSourceRejectsMismatchedBoardIdentityBeforeReadingLists(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want GET", r.Method)
		}
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/1/tokens/") {
			_, _ = w.Write([]byte(`{"idMember":"000000000000000000000001","permissions":[{"modelType":"Board","read":true,"write":false}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"0123456789abcdef01234568","name":"Different board","shortUrl":"https://trello.com/b/def456GH/different-board"}`))
	}))
	defer server.Close()
	configureTrelloTest(t, server.URL)

	_, _, err := fetchTrelloSource(t.Context(), newTrelloSource(uuid.New(), "abc123XY", ""))
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("fetch error = %v, want provider board identity mismatch", err)
	}
	if len(paths) != 2 || !strings.HasPrefix(paths[0], "/1/tokens/") || paths[1] != "/1/boards/abc123XY" {
		t.Fatalf("provider paths = %#v, want only token validation and configured-board metadata reads", paths)
	}
}

func TestFetchTrelloBoardCardsRejectsMissingOrMismatchedBoardIdentity(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	for _, test := range []struct {
		name string
		card string
	}{
		{name: "missing board ID", card: `{"id":"0123456789abcdef01234567","name":"Card"}`},
		{name: "different board ID", card: `{"id":"0123456789abcdef01234567","idBoard":"0123456789abcdef01234568","name":"Card"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("request method = %q, want GET", r.Method)
				}
				if !strings.Contains(r.URL.Query().Get("fields"), "idBoard") {
					t.Errorf("card fields = %q, want idBoard requested", r.URL.Query().Get("fields"))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[" + test.card + "]"))
			}))
			defer server.Close()
			t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
			base, err := url.Parse(server.URL)
			if err != nil {
				t.Fatalf("parse test URL: %v", err)
			}
			client := &trelloAPIClient{base: base, key: "test-key", token: "test-token"}
			if cards, err := fetchTrelloBoardCards(t.Context(), client, testTrelloCanonicalBoardID, &trelloSyncBudget{}); err == nil || cards != nil || !strings.Contains(err.Error(), "matching board identity") {
				t.Fatalf("card fetch = %#v, %v; want fail-closed board identity rejection", cards, err)
			}
		})
	}
}

func TestFetchTrelloReconciliationIncludesNewCardOlderThanCursor(t *testing.T) {
	const activity = "2026-07-10T12:00:00Z"
	cards := `[{"id":"new-old-activity-card","name":"Newly visible card","dateLastActivity":"` + activity + `","idList":"list-1"}]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", trelloCursorV2Prefix+"2026-07-12T00:00:00Z")
	items, nextCursor, err := fetchTrelloSourceWithExistingItems(t.Context(), source, nil)
	if err != nil {
		t.Fatalf("fetch newly visible old-activity card: %v", err)
	}
	if len(items) != 1 || items[0].ExternalID != "trello:card:new-old-activity-card" {
		t.Fatalf("new card items = %#v, want the unseen card despite its activity predating the cursor", items)
	}
	if nextCursor != source.Cursor {
		t.Fatalf("cursor = %q, want unchanged high-water cursor %q", nextCursor, source.Cursor)
	}
}

func TestFetchTrelloReconciliationSkipsUnchangedCardAtCursorBoundary(t *testing.T) {
	const activity = "2026-07-12T00:00:00Z"
	cards := `[{"id":"boundary-card","name":"Boundary card","dateLastActivity":"` + activity + `","idList":"list-1"}]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", trelloCursorV2Prefix+activity)
	current := trelloImportItem(trelloCard{
		ID: "boundary-card", Name: "Boundary card", DateLastActivity: activity, IDList: "list-1",
	}, "Client Delivery", "Doing", source.DefaultProjectKey)
	existing := []models.SourceRawItem{{
		ExternalID: current.ExternalID, ItemType: current.ItemType, Title: current.Title,
		ContentHash: hashText(current.Title + "|" + current.Content),
	}}
	items, nextCursor, err := fetchTrelloSourceWithExistingItems(t.Context(), source, existing)
	if err != nil {
		t.Fatalf("fetch unchanged boundary card: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("unchanged boundary items = %#v, want no redundant re-import", items)
	}
	if nextCursor != source.Cursor {
		t.Fatalf("cursor = %q, want unchanged high-water cursor %q", nextCursor, source.Cursor)
	}
}

func TestFetchTrelloReprocessesOlderCardWhenContentHashChanged(t *testing.T) {
	const activity = "2026-07-10T12:00:00Z"
	cards := `[{"id":"changed-card","name":"Updated plan","desc":"new source content","shortUrl":"https://trello.com/c/changed-card","dateLastActivity":"` + activity + `","idList":"list-1"}]`
	server, _, _ := trelloTestServer(t, cards)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", trelloCursorV2Prefix+"2026-07-12T00:00:00Z")
	current := trelloImportItem(trelloCard{ID: "changed-card", Name: "Updated plan", Desc: "new source content", ShortURL: "https://trello.com/c/changed-card", DateLastActivity: activity, IDList: "list-1"}, "Board", "Doing", source.DefaultProjectKey)
	existing := []models.SourceRawItem{{ExternalID: current.ExternalID, ItemType: "trello_card", ContentHash: hashText("Updated plan|old source content")}}
	items, _, err := fetchTrelloSourceWithExistingItems(t.Context(), source, existing)
	if err != nil {
		t.Fatalf("fetch changed card: %v", err)
	}
	if len(items) != 1 || items[0].ExternalID != current.ExternalID || !strings.Contains(items[0].Content, "new source content") {
		t.Fatalf("changed older card items = %#v, want the card reprocessed despite its old activity timestamp", items)
	}
}

func TestFetchTrelloEmitsRetainedTombstoneForCardMissingFromConfiguredBoard(t *testing.T) {
	server, _, _ := trelloTestServer(t, `[]`)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	source := newTrelloSource(uuid.New(), "abc123XY", trelloCursorV2Prefix+"2026-07-12T00:00:00Z")
	existing := []models.SourceRawItem{{
		ExternalID: "trello:card:previously-seen", ItemType: "trello_card", Title: "Preserve this card",
		SourceURI: "https://trello.com/c/previously-seen", Content: "original evidence", ContentHash: hashText("Preserve this card|original evidence"),
	}}
	items, _, err := fetchTrelloSourceWithExistingItems(t.Context(), source, existing)
	if err != nil {
		t.Fatalf("fetch missing card: %v", err)
	}
	if len(items) != 1 || items[0].ItemType != trelloUnavailableCardItemType || items[0].ExternalID != existing[0].ExternalID || items[0].Content != "" {
		t.Fatalf("missing-card state items = %#v, want a metadata-only tombstone retaining the stable source identity", items)
	}
}

func TestSafeTrelloAttachmentURLDropsSignedQueryAndRejectsUnsafeURLs(t *testing.T) {
	got := safeTrelloAttachmentURL("https://files.example.test/record.pdf?token=private#download")
	if got != "https://files.example.test/record.pdf" {
		t.Fatalf("safe attachment URL = %q, want query and fragment removed", got)
	}
	for _, raw := range []string{"javascript:alert(1)", "http://files.example.test/record.pdf", "https://user:secret@files.example.test/record.pdf", "//files.example.test/record.pdf"} {
		if got := safeTrelloAttachmentURL(raw); got != "" {
			t.Errorf("safeTrelloAttachmentURL(%q) = %q, want empty", raw, got)
		}
	}
	if got := safeTrelloAttachmentURL("https://files.example.test/record.pdf?"); got != "https://files.example.test/record.pdf" {
		t.Errorf("empty-query attachment URL = %q, want normalized URL without query marker", got)
	}
}
