package memory

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func at(base time.Time, minutes int) time.Time {
	return base.Add(time.Duration(minutes) * time.Minute)
}

func sampleMemories() []models.ContextMemory {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return []models.ContextMemory{
		{
			ID: uuid.New(), Kind: "preference", Content: "Prefer local Ollama models before cloud models.",
			Summary: "local first llm", Tags: "llm,routing", Confidence: 0.9,
			CreatedAt: at(base, 0), UpdatedAt: at(base, 30),
		},
		{
			ID: uuid.New(), Kind: "project", Content: "The dashboard is built with Angular and the backend in Go.",
			Summary: "stack", Tags: "frontend,angular", Confidence: 0.6,
			CreatedAt: at(base, 10), UpdatedAt: at(base, 50),
		},
		{
			ID: uuid.New(), Kind: "preference", Content: "Always require approval before running automations.",
			Summary: "approval gate", Tags: "safety,approval", Confidence: 0.75,
			CreatedAt: at(base, 20), UpdatedAt: at(base, 40),
		},
	}
}

func TestQuerySearchMatchesAllTokens(t *testing.T) {
	result := Query(sampleMemories(), QueryParams{Search: "angular backend"})
	if result.Total != 1 {
		t.Fatalf("search total = %d, want 1", result.Total)
	}
	if result.Items[0].Kind != "project" {
		t.Fatalf("search returned wrong memory kind %q", result.Items[0].Kind)
	}

	// A token that appears in no memory yields zero results.
	none := Query(sampleMemories(), QueryParams{Search: "angular kubernetes"})
	if none.Total != 0 {
		t.Fatalf("AND search total = %d, want 0", none.Total)
	}
}

func TestQueryFiltersByKindAndTag(t *testing.T) {
	byKind := Query(sampleMemories(), QueryParams{Kind: "PREFERENCE"})
	if byKind.Total != 2 {
		t.Fatalf("kind filter total = %d, want 2", byKind.Total)
	}

	byTag := Query(sampleMemories(), QueryParams{Tag: "Approval"})
	if byTag.Total != 1 || byTag.Items[0].Kind != "preference" {
		t.Fatalf("tag filter got total=%d, want the approval preference", byTag.Total)
	}

	// Tag matching is exact per tag, not substring: "front" must not match "frontend".
	noSub := Query(sampleMemories(), QueryParams{Tag: "front"})
	if noSub.Total != 0 {
		t.Fatalf("tag substring leaked: total = %d, want 0", noSub.Total)
	}
}

func TestQueryDefaultSortIsUpdatedAtDesc(t *testing.T) {
	result := Query(sampleMemories(), QueryParams{})
	if result.Sort != "updatedAt" || result.Order != "desc" {
		t.Fatalf("defaults = %s/%s, want updatedAt/desc", result.Sort, result.Order)
	}
	// Newest UpdatedAt first: project(50) > preference-approval(40) > preference-local(30).
	if result.Items[0].Summary != "stack" || result.Items[2].Summary != "local first llm" {
		t.Fatalf("updatedAt desc order wrong: %q ... %q", result.Items[0].Summary, result.Items[2].Summary)
	}
}

func TestQuerySortByConfidenceAsc(t *testing.T) {
	result := Query(sampleMemories(), QueryParams{Sort: "confidence", Order: "asc"})
	got := []float64{result.Items[0].Confidence, result.Items[1].Confidence, result.Items[2].Confidence}
	want := []float64{0.6, 0.75, 0.9}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("confidence asc order = %v, want %v", got, want)
		}
	}
}

func TestQuerySortByKindAlphabetical(t *testing.T) {
	result := Query(sampleMemories(), QueryParams{Sort: "kind", Order: "asc"})
	if result.Items[0].Kind != "preference" || result.Items[2].Kind != "project" {
		t.Fatalf("kind asc order wrong: %q ... %q", result.Items[0].Kind, result.Items[2].Kind)
	}
}

func TestQuerySortByRelevance(t *testing.T) {
	// "models" appears twice in the local-Ollama memory content, but relevance
	// counts distinct tokens; "local models" should rank that memory top.
	result := Query(sampleMemories(), QueryParams{Search: "local models", Sort: "relevance"})
	if result.Total == 0 {
		t.Fatalf("relevance search returned nothing")
	}
	if result.Items[0].Summary != "local first llm" {
		t.Fatalf("relevance top = %q, want local first llm", result.Items[0].Summary)
	}
}

func TestQueryPaginationSlicesAndCounts(t *testing.T) {
	items := sampleMemories()
	page1 := Query(items, QueryParams{Sort: "confidence", Order: "asc", Page: 1, PageSize: 2})
	if page1.Total != 3 || page1.TotalPages != 2 || len(page1.Items) != 2 {
		t.Fatalf("page1 total=%d totalPages=%d items=%d, want 3/2/2", page1.Total, page1.TotalPages, len(page1.Items))
	}
	page2 := Query(items, QueryParams{Sort: "confidence", Order: "asc", Page: 2, PageSize: 2})
	if len(page2.Items) != 1 || page2.Items[0].Confidence != 0.9 {
		t.Fatalf("page2 should hold the single highest-confidence remainder, got %d items", len(page2.Items))
	}

	// A page beyond the end is empty but echoes the requested page, never panics.
	beyond := Query(items, QueryParams{Page: 99, PageSize: 2})
	if len(beyond.Items) != 0 || beyond.Page != 99 {
		t.Fatalf("beyond-range page got items=%d page=%d, want 0/99", len(beyond.Items), beyond.Page)
	}
}

func TestQueryEqualTimestampsUseStableIDOrderAcrossPages(t *testing.T) {
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	lowID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	highID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	items := []models.ContextMemory{
		{ID: lowID, Kind: "preference", CreatedAt: at, UpdatedAt: at},
		{ID: highID, Kind: "preference", CreatedAt: at, UpdatedAt: at},
	}

	for iteration := 0; iteration < 100; iteration++ {
		// Reverse the repository order to prove pagination is independent from
		// map iteration or insertion order when all visible sort keys tie.
		items[0], items[1] = items[1], items[0]
		page1 := Query(items, QueryParams{Sort: "updatedAt", Order: "desc", Page: 1, PageSize: 1})
		page2 := Query(items, QueryParams{Sort: "updatedAt", Order: "desc", Page: 2, PageSize: 1})
		if page1.Items[0].ID != highID || page2.Items[0].ID != lowID {
			t.Fatalf("iteration %d IDs = %s/%s, want %s/%s", iteration, page1.Items[0].ID, page2.Items[0].ID, highID, lowID)
		}
	}
}

func TestQueryNormalizesBounds(t *testing.T) {
	tooBig := Query(sampleMemories(), QueryParams{PageSize: 5000})
	if tooBig.PageSize != maxPageSize {
		t.Fatalf("pageSize clamp = %d, want %d", tooBig.PageSize, maxPageSize)
	}
	zero := Query(sampleMemories(), QueryParams{PageSize: 0, Page: 0})
	if zero.PageSize != defaultPageSize || zero.Page != 1 {
		t.Fatalf("defaults = size %d page %d, want %d/1", zero.PageSize, zero.Page, defaultPageSize)
	}
}

func TestQueryBoundsTextAndTokenWork(t *testing.T) {
	search := strings.Repeat("token ", 200) + "unreachable-tail"
	result := Query(sampleMemories(), QueryParams{
		Search: search,
		Kind:   strings.Repeat("k", maxKindLength+20),
		Tag:    strings.Repeat("t", maxTagLength+20),
	})
	if len([]rune(result.Search)) > maxSearchLength || len([]rune(result.Kind)) > maxKindLength || len([]rune(result.Tag)) > maxTagLength {
		t.Fatalf("normalized query exceeded bounds: search=%d kind=%d tag=%d", len([]rune(result.Search)), len([]rune(result.Kind)), len([]rune(result.Tag)))
	}
	if len(searchTokensOf(strings.Join([]string{
		"one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
		"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen",
	}, " "))) != maxSearchTokens {
		t.Fatal("search token work was not bounded")
	}
}

func TestQueryHandlesPlatformMaximumPageWithoutOverflow(t *testing.T) {
	result := Query(sampleMemories(), QueryParams{Page: int(^uint(0) >> 1), PageSize: maxPageSize})
	if len(result.Items) != 0 || result.Page != int(^uint(0)>>1) {
		t.Fatalf("maximum page result = %#v", result)
	}
}

func TestQueryEmptyInputIsSafe(t *testing.T) {
	result := Query(nil, QueryParams{Search: "anything"})
	if result.Total != 0 || result.TotalPages != 0 {
		t.Fatalf("empty input total=%d totalPages=%d, want 0/0", result.Total, result.TotalPages)
	}
	if result.Items == nil {
		t.Fatalf("Items must be a non-nil empty slice for clean JSON, got nil")
	}
}

func TestQueryDoesNotMutateInput(t *testing.T) {
	items := sampleMemories()
	firstIDBefore := items[0].ID
	_ = Query(items, QueryParams{Sort: "confidence", Order: "asc"})
	if items[0].ID != firstIDBefore {
		t.Fatalf("Query reordered the caller's slice")
	}
}
