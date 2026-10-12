package source

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	githubEvidenceMaxReferences = 64
	githubEvidenceSyncJobLimit  = 50
	githubEvidenceMaxAge        = 24 * time.Hour
	githubEvidenceMaxMetadata   = 256 * 1024
)

var githubSlugPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var githubCommitID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type githubQualityEvidenceRepository interface {
	FindMutableSourceForOwner(id uuid.UUID, ownerIdentity string) (*models.ConnectedSource, error)
	FindSyncJobsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceSyncJob, error)
	FindExtraction(id uuid.UUID) (*models.SourceExtraction, error)
	FindRawItem(sourceID uuid.UUID, externalID string) (*models.SourceRawItem, error)
}

type githubQualityEvidenceResolver struct {
	repo githubQualityEvidenceRepository
	now  func() time.Time
}

type githubEvidenceRecordReference struct {
	repository string
	kind       string
	identity   string
	externalID string
	itemType   string
}

type githubImportMetadata struct {
	Source         string `json:"source"`
	Repository     string `json:"repository"`
	Kind           string `json:"kind"`
	Updated        string `json:"updated,omitempty"`
	FetchedAt      string `json:"fetched_at"`
	Status         string `json:"status,omitempty"`
	Conclusion     string `json:"conclusion,omitempty"`
	ReviewRequired bool   `json:"reviewRequired,omitempty"`
	HeadSHA        string `json:"head_sha,omitempty"`
	WorkflowName   string `json:"workflow_name,omitempty"`
	WorkflowPath   string `json:"workflow_path,omitempty"`
	WorkflowEvent  string `json:"workflow_event,omitempty"`
	MergeCommitSHA string `json:"merge_commit_sha,omitempty"`
	Merged         bool   `json:"merged,omitempty"`
}

// NewGitHubQualityEvidenceResolver returns a resolver backed only by the
// connected-source repository. It deliberately performs no GitHub API calls.
func NewGitHubQualityEvidenceResolver(repo Repository) workflow.GitHubQualityEvidenceResolver {
	if repo == nil {
		return nil
	}
	return &githubQualityEvidenceResolver{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

var _ workflow.GitHubQualityEvidenceResolver = (*githubQualityEvidenceResolver)(nil)

func (r *githubQualityEvidenceResolver) ResolveGitHubQualityEvidence(
	ownerIdentity string,
	references []workflow.GitHubQualityEvidenceReference,
) (workflow.GitHubQualityEvidence, error) {
	evidence := workflow.GitHubQualityEvidence{}
	if r == nil || r.repo == nil {
		return evidence, errors.New("GitHub evidence repository is unavailable")
	}
	if ownerIdentity == "" || strings.TrimSpace(ownerIdentity) != ownerIdentity {
		return evidence, nil
	}
	if len(references) > githubEvidenceMaxReferences {
		return evidence, fmt.Errorf("GitHub evidence reference limit exceeded")
	}
	now := time.Now().UTC()
	if r.now != nil {
		now = r.now().UTC()
	}

	sources := make(map[uuid.UUID]*models.ConnectedSource)
	checkedSources := make(map[uuid.UUID]bool)
	checkedSyncs := make(map[uuid.UUID]bool)
	validatedSyncs := make(map[uuid.UUID]bool)
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if reference.SourceType != "github" {
			continue
		}
		recordRef, ok := parseGitHubEvidenceURI(reference.SourceURI)
		if !ok {
			continue
		}
		expectedRepository := canonicalGitHubRepositoryTarget(reference.ExpectedRepository)
		if expectedRepository == "" || !strings.EqualFold(expectedRepository, recordRef.repository) {
			continue
		}
		extractionID, err := parseCanonicalUUID(reference.SourceID)
		if err != nil {
			continue
		}
		key := extractionID.String() + "\x00" + reference.SourceURI
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}

		extraction, err := r.repo.FindExtraction(extractionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return workflow.GitHubQualityEvidence{}, fmt.Errorf("load GitHub evidence extraction: %w", err)
		}
		if extraction == nil || extraction.ID != extractionID || extraction.Archived ||
			extraction.SourceID == uuid.Nil || extraction.RawItemID == uuid.Nil ||
			extraction.SourceURI != reference.SourceURI {
			continue
		}

		sourceID := extraction.SourceID
		if !checkedSources[sourceID] {
			source, sourceErr := r.repo.FindMutableSourceForOwner(sourceID, ownerIdentity)
			switch {
			case errors.Is(sourceErr, gorm.ErrRecordNotFound):
				checkedSources[sourceID] = true
			case sourceErr != nil:
				return workflow.GitHubQualityEvidence{}, fmt.Errorf("load owner-scoped GitHub source: %w", sourceErr)
			default:
				sources[sourceID] = source
				checkedSources[sourceID] = true
			}
		}
		source := sources[sourceID]
		if !validGitHubEvidenceSource(source, ownerIdentity, recordRef.repository, now) {
			continue
		}
		if !checkedSyncs[sourceID] {
			valid, validationErr := r.sourceHasRecentSuccessfulSync(source, now)
			if validationErr != nil {
				return workflow.GitHubQualityEvidence{}, validationErr
			}
			validatedSyncs[sourceID] = valid
			checkedSyncs[sourceID] = true
		}
		if !validatedSyncs[sourceID] {
			continue
		}

		if extraction.ContentType != recordRef.itemType {
			continue
		}
		raw, err := r.repo.FindRawItem(sourceID, recordRef.externalID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return workflow.GitHubQualityEvidence{}, fmt.Errorf("load GitHub raw evidence item: %w", err)
		}
		if !validGitHubRawEvidence(raw, extraction, recordRef, reference.SourceURI, now) {
			continue
		}
		metadata, ok := decodeGitHubImportMetadata(raw.Metadata)
		if !ok || metadata.Source != "github" || !strings.EqualFold(metadata.Repository, recordRef.repository) || metadata.Kind != recordRef.kind ||
			!freshGitHubImporterObservation(metadata.FetchedAt, now) {
			continue
		}
		// Workflow-run imports currently contain only run-level status and labels.
		// They do not persist job or step results, so they cannot prove that any
		// test/build check actually ran and succeeded. Keep WorkflowSuccess false
		// until the importer provides concrete, linked check-level evidence.
		switch recordRef.kind {
		case "commit":
			evidence.Commit = true
		}
	}
	return evidence, nil
}

func (r *githubQualityEvidenceResolver) sourceHasRecentSuccessfulSync(source *models.ConnectedSource, now time.Time) (bool, error) {
	if source == nil || source.ID == uuid.Nil || source.LastSyncedAt == nil || !freshGitHubEvidenceTime(*source.LastSyncedAt, now) {
		return false, nil
	}
	jobs, err := r.repo.FindSyncJobsForSources([]uuid.UUID{source.ID}, githubEvidenceSyncJobLimit)
	if err != nil {
		return false, fmt.Errorf("load bounded GitHub sync history: %w", err)
	}
	for _, job := range jobs {
		if job.SourceID == source.ID && job.Status == "completed" && job.ItemsFailed == 0 &&
			job.CompletedAt != nil && freshGitHubEvidenceTime(*job.CompletedAt, now) {
			return true, nil
		}
	}
	return false, nil
}

func validGitHubEvidenceSource(source *models.ConnectedSource, ownerIdentity, repository string, now time.Time) bool {
	if source == nil || source.ID == uuid.Nil || source.OwnerIdentity != ownerIdentity ||
		source.ConnectorKey != "github" || !source.Enabled || source.Status != "active" || source.RevokedAt != nil ||
		source.LastSyncedAt == nil || !freshGitHubEvidenceTime(*source.LastSyncedAt, now) {
		return false
	}
	return strings.EqualFold(canonicalGitHubRepositoryTarget(source.SyncTarget), repository)
}

func validGitHubRawEvidence(
	raw *models.SourceRawItem,
	extraction *models.SourceExtraction,
	recordRef githubEvidenceRecordReference,
	sourceURI string,
	now time.Time,
) bool {
	if raw == nil || extraction == nil || raw.ID == uuid.Nil || raw.ID != extraction.RawItemID ||
		raw.SourceID != extraction.SourceID || raw.ExternalID != recordRef.externalID || raw.ItemType != recordRef.itemType ||
		raw.SourceURI != sourceURI || !rawEvidenceFresh(raw, now) {
		return false
	}
	return true
}

func rawEvidenceFresh(raw *models.SourceRawItem, now time.Time) bool {
	if raw == nil {
		return false
	}
	// FetchedAt is immutable after first insert in the current importer. UpdatedAt
	// is refreshed when the API response is persisted, so use the later persisted
	// observation time while still rejecting zero and future timestamps.
	observedAt := raw.FetchedAt
	if raw.UpdatedAt.After(observedAt) {
		observedAt = raw.UpdatedAt
	}
	return freshGitHubEvidenceTime(observedAt, now)
}

func freshGitHubImporterObservation(value string, now time.Time) bool {
	observedAt, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && freshGitHubEvidenceTime(observedAt, now)
}

func freshGitHubEvidenceTime(value, now time.Time) bool {
	return !value.IsZero() && !value.After(now) && now.Sub(value) < githubEvidenceMaxAge
}

func parseCanonicalUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return uuid.Nil, fmt.Errorf("non-canonical UUID")
	}
	return parsed, nil
}

func parseGitHubEvidenceURI(value string) (githubEvidenceRecordReference, bool) {
	if value == "" || strings.TrimSpace(value) != value {
		return githubEvidenceRecordReference{}, false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" {
		return githubEvidenceRecordReference{}, false
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) < 4 || !validGitHubSlugPart(parts[0]) || !validGitHubSlugPart(parts[1]) {
		return githubEvidenceRecordReference{}, false
	}
	repository := parts[0] + "/" + parts[1]
	ref := githubEvidenceRecordReference{repository: repository}
	switch {
	case len(parts) == 4 && parts[2] == "commit" && githubCommitID.MatchString(parts[3]):
		ref.kind = "commit"
		ref.identity = parts[3]
		ref.itemType = "github_commit"
	case len(parts) == 5 && parts[2] == "actions" && parts[3] == "runs" && canonicalPositiveDecimal(parts[4]):
		ref.kind = "workflow_run"
		ref.identity = parts[4]
		ref.itemType = "github_workflow_run"
	case len(parts) == 4 && parts[2] == "pull" && canonicalPositiveDecimal(parts[3]):
		ref.kind = "pull_request"
		ref.identity = parts[3]
		ref.itemType = "github_pull_request"
	default:
		return githubEvidenceRecordReference{}, false
	}
	if "https://github.com/"+parsed.EscapedPath()[1:] != value {
		return githubEvidenceRecordReference{}, false
	}
	ref.externalID = "github:" + ref.kind + ":" + ref.identity
	return ref, true
}

func validGitHubSlugPart(value string) bool {
	return value != "" && value != "." && value != ".." && githubSlugPart.MatchString(value)
}

func canonicalPositiveDecimal(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatUint(parsed, 10) == value
}

func canonicalGitHubRepositoryTarget(value string) string {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\?#%:") {
		return ""
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !validGitHubSlugPart(parts[0]) || !validGitHubSlugPart(parts[1]) {
		return ""
	}
	return value
}

func decodeGitHubImportMetadata(value string) (githubImportMetadata, bool) {
	var metadata githubImportMetadata
	if value == "" || len(value) > githubEvidenceMaxMetadata {
		return metadata, false
	}
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return githubImportMetadata{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return githubImportMetadata{}, false
	}
	if metadata.Source != "github" || canonicalGitHubRepositoryTarget(metadata.Repository) != metadata.Repository {
		return githubImportMetadata{}, false
	}
	return metadata, true
}

func githubImportRecordIdentifier(record map[string]any, kind string) string {
	switch kind {
	case "commit":
		identifier := strings.ToLower(githubString(record, "sha"))
		if githubCommitID.MatchString(identifier) {
			return identifier
		}
		return ""
	case "workflow_run":
		return canonicalGitHubImportIdentifier(record["id"])
	case "pull_request", "issue":
		return canonicalGitHubImportIdentifier(record["number"])
	default:
		return githubString(record, "node_id", "id", "sha", "number", "name")
	}
}

func canonicalGitHubImportIdentifier(value any) string {
	switch typed := value.(type) {
	case string:
		if canonicalPositiveDecimal(typed) {
			return typed
		}
	case json.Number:
		identifier := typed.String()
		if canonicalPositiveDecimal(identifier) {
			return identifier
		}
	case float64:
		const maxExactJSONInteger = 1<<53 - 1
		if typed > 0 && typed <= maxExactJSONInteger && math.Trunc(typed) == typed {
			return strconv.FormatUint(uint64(typed), 10)
		}
	case int:
		if typed > 0 {
			return strconv.Itoa(typed)
		}
	case int64:
		if typed > 0 {
			return strconv.FormatInt(typed, 10)
		}
	case uint:
		if typed > 0 {
			return strconv.FormatUint(uint64(typed), 10)
		}
	case uint64:
		if typed > 0 {
			return strconv.FormatUint(typed, 10)
		}
	}
	return ""
}

func newGitHubQualityEvidenceResolver(repo githubQualityEvidenceRepository, now func() time.Time) *githubQualityEvidenceResolver {
	return &githubQualityEvidenceResolver{repo: repo, now: now}
}
