package braincatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxGitHubMetadataBytes int64 = 256 << 10

// UpstreamReviewer rechecks a fixed catalog entry. Implementations must not
// resolve arbitrary user-provided URLs or fetch source archives.
type UpstreamReviewer interface {
	Review(Entry) (UpstreamReview, error)
}

type UpstreamContextReviewer interface {
	ReviewContext(context.Context, Entry) (UpstreamReview, error)
}

func reviewUpstream(ctx context.Context, reviewer UpstreamReviewer, entry Entry) (UpstreamReview, error) {
	if contextual, ok := reviewer.(UpstreamContextReviewer); ok {
		return contextual.ReviewContext(ctx, entry)
	}
	return reviewer.Review(entry)
}

type githubUpstreamReviewer struct {
	client *http.Client
	now    func() time.Time
}

func NewUpstreamReviewer(client *http.Client) UpstreamReviewer {
	if client == nil {
		client = &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &githubUpstreamReviewer{client: client, now: time.Now}
}

func (r *githubUpstreamReviewer) Review(entry Entry) (UpstreamReview, error) {
	return r.ReviewContext(context.Background(), entry)
}

func (r *githubUpstreamReviewer) ReviewContext(parent context.Context, entry Entry) (UpstreamReview, error) {
	review := UpstreamReview{
		ID: entry.ID, Name: entry.Name, UpstreamURL: entry.UpstreamURL,
		CheckedAt: r.now().UTC().Format(time.RFC3339), Disposition: entry.Status,
	}
	owner, repo, err := githubRepositoryPath(entry.UpstreamURL)
	if err != nil {
		return review, err
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, ossInsightRequestTimeout)
	defer cancel()
	resp, err := githubMetadataGET(ctx, r.client, "https://api.github.com/repos/"+owner+"/"+repo)
	if err != nil {
		return review, fmt.Errorf("upstream metadata request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		review.Message = "GitHub did not find the configured upstream repository. HAI has not changed its catalog disposition."
		applyReadinessAssessment(entry, &review)
		return review, nil
	}
	if resp.StatusCode != http.StatusOK {
		return review, fmt.Errorf("upstream metadata request returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		FullName      string `json:"full_name"`
		HTMLURL       string `json:"html_url"`
		Archived      bool   `json:"archived"`
		DefaultBranch string `json:"default_branch"`
		PushedAt      string `json:"pushed_at"`
		License       *struct {
			SPDXID string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGitHubMetadataBytes)).Decode(&payload); err != nil {
		return review, fmt.Errorf("upstream metadata response was invalid")
	}
	review.Available = true
	review.ResolvedRepository = strings.TrimSpace(payload.FullName)
	if review.ResolvedRepository == "" {
		review.ResolvedRepository = owner + "/" + repo
	}
	review.ResolvedUpstreamURL = strings.TrimSpace(payload.HTMLURL)
	if review.ResolvedUpstreamURL == "" {
		review.ResolvedUpstreamURL = "https://github.com/" + review.ResolvedRepository
	}
	review.RepositoryMoved = !strings.EqualFold(review.ResolvedRepository, owner+"/"+repo)
	review.Archived = payload.Archived
	review.DefaultBranch = strings.TrimSpace(payload.DefaultBranch)
	review.PushedAt = strings.TrimSpace(payload.PushedAt)
	if payload.License != nil {
		review.License = strings.TrimSpace(payload.License.SPDXID)
	}
	if review.License == "" {
		review.License = "not reported"
	}
	if review.RepositoryMoved {
		review.Message = "GitHub reports that this configured upstream now resolves to " + review.ResolvedRepository + ". HAI has not changed the catalog record; review the rename or transfer before any adoption work."
	} else if review.Archived {
		review.Message = "GitHub reports this upstream as archived. HAI has not changed its catalog disposition; review the adoption decision before any further work."
	} else {
		review.Message = "GitHub metadata was retrieved. This recheck does not install, enable, approve, or execute the project."
	}
	applyReadinessAssessment(entry, &review)
	return review, nil
}

// githubMetadataGET only reaches GitHub's fixed public metadata endpoint for
// a catalog-derived owner/repository pair. It retries transient rate-limit and
// server responses, while leaving permanent client failures observable.
func githubMetadataGET(ctx context.Context, client *http.Client, requestURL string) (*http.Response, error) {
	if client == nil {
		return nil, fmt.Errorf("GitHub metadata HTTP client is unavailable")
	}
	for attempt := 1; attempt <= ossInsightRequestMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("could not prepare upstream metadata request")
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", ossInsightCollectionReviewAgent)
		resp, err := client.Do(req)
		if err == nil && resp != nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound) {
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
				return nil, fmt.Errorf("metadata request failed after %d attempt(s): %w", attempt, err)
			}
			return nil, fmt.Errorf("metadata request returned HTTP %d after %d attempt(s)", status, attempt)
		}
		if err := waitForCatalogRetry(ctx, attempt); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("metadata request retry loop ended unexpectedly")
}

func githubRepositoryPath(rawURL string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("catalog upstream URL must be a plain HTTPS GitHub repository URL")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("catalog upstream URL must identify exactly one GitHub repository")
	}
	return parts[0], parts[1], nil
}
