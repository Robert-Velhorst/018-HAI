package braincatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	ossInsightCollectionsURL        = "https://api.ossinsight.io/v1/collections/"
	maxOSSInsightCollectionsBytes   = 256 << 10
	ossInsightCollectionReviewAgent = "HAI-BrainCatalog/1.0"
	ossInsightRequestMaxAttempts    = 3
	ossInsightRetryBaseDelay        = 100 * time.Millisecond
	ossInsightRequestTimeout        = 30 * time.Second
)

// OSSInsightCollectionReviewer checks the one fixed public collection list.
// It deliberately does not accept a user-provided URL or fetch repository
// code: this endpoint exists only to make catalog drift visible to an admin.
type OSSInsightCollectionReviewer interface {
	ReviewCollections() (OSSInsightCollectionReview, error)
}

type OSSInsightCollectionContextReviewer interface {
	ReviewCollectionsContext(context.Context) (OSSInsightCollectionReview, error)
}

func reviewCollections(ctx context.Context, reviewer OSSInsightCollectionReviewer) (OSSInsightCollectionReview, error) {
	if contextual, ok := reviewer.(OSSInsightCollectionContextReviewer); ok {
		return contextual.ReviewCollectionsContext(ctx)
	}
	return reviewer.ReviewCollections()
}

// OSSInsightCollectionReview reports source freshness without turning source
// discovery into an adoption or activation decision.
type OSSInsightCollectionReview struct {
	CheckedAt       string   `json:"checkedAt"`
	SourceURL       string   `json:"sourceUrl"`
	Available       bool     `json:"available"`
	ExpectedTotal   int      `json:"expectedTotal"`
	CurrentTotal    int      `json:"currentTotal"`
	NewCollections  []string `json:"newCollections,omitempty"`
	MissingExpected []string `json:"missingExpected,omitempty"`
	Message         string   `json:"message"`
}

type ossInsightCollectionReviewer struct {
	client *http.Client
	now    func() time.Time
}

func NewOSSInsightCollectionReviewer(client *http.Client) OSSInsightCollectionReviewer {
	if client == nil {
		client = &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &ossInsightCollectionReviewer{client: client, now: time.Now}
}

func (r *ossInsightCollectionReviewer) ReviewCollections() (OSSInsightCollectionReview, error) {
	return r.ReviewCollectionsContext(context.Background())
}

func (r *ossInsightCollectionReviewer) ReviewCollectionsContext(parent context.Context) (OSSInsightCollectionReview, error) {
	expected := expectedOSSInsightCollections()
	review := OSSInsightCollectionReview{
		CheckedAt: r.now().UTC().Format(time.RFC3339), SourceURL: ossInsightCollectionsURL, ExpectedTotal: len(expected),
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, ossInsightRequestTimeout)
	defer cancel()
	resp, err := ossInsightGET(ctx, r.client, ossInsightCollectionsURL)
	if err != nil {
		return review, fmt.Errorf("OSS Insight collection request failed: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Data struct {
			Rows []struct {
				Name string `json:"name"`
			} `json:"rows"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOSSInsightCollectionsBytes)).Decode(&payload); err != nil {
		return review, fmt.Errorf("OSS Insight collection response was invalid")
	}
	current := map[string]struct{}{}
	for _, row := range payload.Data.Rows {
		if name := strings.TrimSpace(row.Name); name != "" {
			current[name] = struct{}{}
		}
	}
	if len(current) == 0 {
		return review, fmt.Errorf("OSS Insight collection response contained no collection names")
	}

	review.Available = true
	review.CurrentTotal = len(current)
	for name := range current {
		if _, ok := expected[name]; !ok {
			review.NewCollections = append(review.NewCollections, name)
		}
	}
	for name := range expected {
		if _, ok := current[name]; !ok {
			review.MissingExpected = append(review.MissingExpected, name)
		}
	}
	sort.Strings(review.NewCollections)
	sort.Strings(review.MissingExpected)
	if len(review.NewCollections) == 0 && len(review.MissingExpected) == 0 {
		review.Message = "The OSS Insight collection list matches HAI's recorded snapshot. This check did not install, enable, or approve any project."
	} else {
		review.Message = "OSS Insight collection drift was detected. Review the changed categories before changing HAI's catalog; this check did not install, enable, approve, or execute any project."
	}
	return review, nil
}

// ossInsightGET performs a bounded, read-only request against the fixed OSS
// Insight endpoints. The public collection service occasionally responds with
// a transient 429 or 5xx while its indexes are rebuilding. Retrying those
// failures prevents a partial catalog scan from being presented as source
// drift, while non-transient 4xx responses still fail immediately.
func ossInsightGET(ctx context.Context, client *http.Client, requestURL string) (*http.Response, error) {
	if client == nil {
		return nil, fmt.Errorf("OSS Insight HTTP client is unavailable")
	}
	for attempt := 1; attempt <= ossInsightRequestMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("could not prepare OSS Insight request")
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", ossInsightCollectionReviewAgent)
		resp, err := client.Do(req)
		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		transient := err != nil || (resp != nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError))
		status := 0
		if resp != nil {
			status = resp.StatusCode
			resp.Body.Close()
		}
		if !transient || attempt == ossInsightRequestMaxAttempts {
			if err != nil {
				return nil, fmt.Errorf("request failed after %d attempt(s): %w", attempt, err)
			}
			return nil, fmt.Errorf("request returned HTTP %d after %d attempt(s)", status, attempt)
		}
		if err := waitForCatalogRetry(ctx, attempt); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("request retry loop ended unexpectedly")
}

func waitForCatalogRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt) * ossInsightRetryBaseDelay
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func expectedOSSInsightCollections() map[string]struct{} {
	expected := map[string]struct{}{}
	for _, entry := range CollectionScreenings() {
		expected[entry.Collection] = struct{}{}
	}
	return expected
}
