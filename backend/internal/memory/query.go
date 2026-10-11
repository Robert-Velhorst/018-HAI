package memory

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"automation-hub-backend/internal/models"
)

// QueryParams describes search, filter, sort, and pagination options for
// listing context memories. It is consumed by the pure Query function so the
// same behaviour can be unit-tested without any database or HTTP layer.
type QueryParams struct {
	Search   string // free-text; matched (AND semantics) against content, summary, tags
	Kind     string // exact kind filter (case-insensitive)
	Tag      string // single tag filter (case-insensitive, exact tag match)
	Sort     string // updatedAt|createdAt|confidence|kind|relevance (default updatedAt)
	Order    string // asc|desc (default desc)
	Page     int    // 1-based page number (default 1)
	PageSize int    // items per page (default 20, max 100)
}

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxSearchLength = 512
	maxKindLength   = 50
	maxTagLength    = 255
	maxSearchTokens = 16
)

// PageResult is the paginated envelope returned by memory queries. It echoes
// the effective (normalized) query parameters so a client can render controls
// truthfully and never guess what filtering/sorting actually happened.
type PageResult struct {
	Items      []models.ContextMemory `json:"items"`
	Total      int                    `json:"total"`
	Page       int                    `json:"page"`
	PageSize   int                    `json:"pageSize"`
	TotalPages int                    `json:"totalPages"`
	Sort       string                 `json:"sort"`
	Order      string                 `json:"order"`
	Search     string                 `json:"search,omitempty"`
	Kind       string                 `json:"kind,omitempty"`
	Tag        string                 `json:"tag,omitempty"`
}

// normalized applies safe defaults and bounds so out-of-range or empty input
// can never produce a panic or an unbounded page.
func (p QueryParams) normalized() QueryParams {
	out := p
	out.Search = boundedQueryText(p.Search, maxSearchLength)
	out.Kind = boundedQueryText(p.Kind, maxKindLength)
	out.Tag = boundedQueryText(p.Tag, maxTagLength)

	switch strings.ToLower(strings.TrimSpace(p.Sort)) {
	case "createdat":
		out.Sort = "createdAt"
	case "confidence":
		out.Sort = "confidence"
	case "kind":
		out.Sort = "kind"
	case "relevance":
		out.Sort = "relevance"
	default:
		out.Sort = "updatedAt"
	}

	if strings.EqualFold(strings.TrimSpace(p.Order), "asc") {
		out.Order = "asc"
	} else {
		out.Order = "desc"
	}

	if out.Page < 1 {
		out.Page = 1
	}
	if out.PageSize <= 0 {
		out.PageSize = defaultPageSize
	}
	if out.PageSize > maxPageSize {
		out.PageSize = maxPageSize
	}
	return out
}

// Query filters, sorts, and paginates the supplied memories deterministically.
//
// It is a pure function: no I/O and no shared state, so it is safe to unit test
// in isolation and safe to call from any handler after loading rows. The input
// slice is never mutated.
func Query(items []models.ContextMemory, params QueryParams) PageResult {
	p := params.normalized()

	// Filter.
	searchTokens := searchTokensOf(p.Search)
	filtered := make([]models.ContextMemory, 0, len(items))
	for _, item := range items {
		if p.Kind != "" && !strings.EqualFold(item.Kind, p.Kind) {
			continue
		}
		if p.Tag != "" && !hasTag(item.Tags, p.Tag) {
			continue
		}
		if len(searchTokens) > 0 && !matchesAllTokens(item, searchTokens) {
			continue
		}
		filtered = append(filtered, item)
	}

	// Sort (stable, so equal keys keep input order).
	sortMemories(filtered, p, searchTokens)

	total := len(filtered)
	pageItems := []models.ContextMemory{}
	if start, ok := pageStart(p, total); ok {
		end := start + p.PageSize
		if end > total {
			end = total
		}
		pageItems = append(pageItems, filtered[start:end]...)
	}

	return pageResult(p, total, pageItems)
}

func pageResult(p QueryParams, total int, items []models.ContextMemory) PageResult {
	totalPages := 0
	if total > 0 {
		totalPages = 1 + (total-1)/p.PageSize
	}
	if items == nil {
		items = []models.ContextMemory{}
	}
	return PageResult{
		Items:      items,
		Total:      total,
		Page:       p.Page,
		PageSize:   p.PageSize,
		TotalPages: totalPages,
		Sort:       p.Sort,
		Order:      p.Order,
		Search:     p.Search,
		Kind:       p.Kind,
		Tag:        p.Tag,
	}
}

func pageStart(p QueryParams, total int) (int, bool) {
	if total <= 0 {
		return 0, false
	}
	maxInt := int(^uint(0) >> 1)
	if p.Page-1 > maxInt/p.PageSize {
		return 0, false
	}
	start := (p.Page - 1) * p.PageSize
	return start, start < total
}

func sortMemories(items []models.ContextMemory, p QueryParams, searchTokens []string) {
	asc := p.Order == "asc"
	sort.SliceStable(items, func(i, j int) bool {
		comparison := compareMemories(items[i], items[j], p.Sort, searchTokens)
		if asc {
			return comparison < 0
		}
		return comparison > 0
	})
}

// compareMemories returns a strict total ordering. In particular, equal primary
// keys must return zero instead of treating both (i,j) and (j,i) as "less";
// otherwise map-backed repository input can move records between pages.
func compareMemories(left, right models.ContextMemory, sortField string, searchTokens []string) int {
	var comparison int
	switch sortField {
	case "createdAt":
		comparison = compareTime(left.CreatedAt, right.CreatedAt)
		if comparison == 0 {
			comparison = compareTime(left.UpdatedAt, right.UpdatedAt)
		}
	case "confidence":
		comparison = compareFloat(left.Confidence, right.Confidence)
		if comparison == 0 {
			comparison = compareTime(left.UpdatedAt, right.UpdatedAt)
		}
	case "kind":
		comparison = strings.Compare(strings.ToLower(left.Kind), strings.ToLower(right.Kind))
		if comparison == 0 {
			comparison = compareTime(left.UpdatedAt, right.UpdatedAt)
		}
	case "relevance":
		comparison = compareInt(relevanceScore(left, searchTokens), relevanceScore(right, searchTokens))
		if comparison == 0 {
			comparison = compareTime(left.UpdatedAt, right.UpdatedAt)
		}
	default: // updatedAt
		comparison = compareTime(left.UpdatedAt, right.UpdatedAt)
		if comparison == 0 {
			comparison = compareTime(left.CreatedAt, right.CreatedAt)
		}
	}
	if comparison != 0 {
		return comparison
	}
	return strings.Compare(left.ID.String(), right.ID.String())
}

func compareTime(left, right time.Time) int {
	if left.Before(right) {
		return -1
	}
	if left.After(right) {
		return 1
	}
	return 0
}

func compareFloat(left, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareInt(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

// relevanceScore counts how many distinct search tokens appear in the memory's
// searchable text. Higher means more relevant.
func relevanceScore(item models.ContextMemory, searchTokens []string) int {
	if len(searchTokens) == 0 {
		return 0
	}
	haystack := haystackOf(item)
	score := 0
	for _, token := range searchTokens {
		if strings.Contains(haystack, token) {
			score++
		}
	}
	return score
}

func matchesAllTokens(item models.ContextMemory, searchTokens []string) bool {
	haystack := haystackOf(item)
	for _, token := range searchTokens {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func haystackOf(item models.ContextMemory) string {
	return strings.ToLower(item.Content + " " + item.Summary + " " + item.Tags)
}

func searchTokensOf(search string) []string {
	fields := strings.Fields(strings.ToLower(search))
	tokens := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		tokens = append(tokens, field)
		if len(tokens) == maxSearchTokens {
			break
		}
	}
	return tokens
}

func boundedQueryText(value string, maxRunes int) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == 0 || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return -1
		}
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, value))
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes])
	}
	return strings.Join(strings.Fields(value), " ")
}

// hasTag reports whether the comma-joined tag string contains an exact,
// case-insensitive match for the requested tag.
func hasTag(joinedTags, tag string) bool {
	want := strings.TrimSpace(strings.ToLower(tag))
	if want == "" {
		return true
	}
	for _, candidate := range strings.Split(joinedTags, ",") {
		if strings.TrimSpace(strings.ToLower(candidate)) == want {
			return true
		}
	}
	return false
}
