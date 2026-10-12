package workflow

import (
	"automation-hub-backend/internal/models"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// GitHubQualityEvidence is derived only from authenticated, owner-scoped
// connected-source records bound to the workflow's primary GitHub origin.
// Caller-provided labels and prose are not evidence. The current importer
// supports Commit from persisted commit records only. WorkflowSuccess remains
// unsupported until it persists a successful, repository- and head-SHA-linked
// check-run/job result with concrete test/build identity and observation time.
// DocsChanged remains unsupported until it persists a complete fetched PR file
// list linked to the merged PR, repository, and merge/head SHA. Windows 11
// validation is also unsupported until a structured platform attestation is
// persisted with repository, commit, runner/OS identity, command/result, and
// observation time.
type GitHubQualityEvidence struct {
	Commit          bool
	WorkflowSuccess bool
	DocsChanged     bool
}

type GitHubQualityEvidenceReference struct {
	SourceType         string
	SourceID           string
	SourceURI          string
	ExpectedRepository string
}

type GitHubQualityEvidenceResolver interface {
	ResolveGitHubQualityEvidence(ownerIdentity string, references []GitHubQualityEvidenceReference) (GitHubQualityEvidence, error)
}

func githubQualityEvidenceReferences(item models.WorkflowItem, links []models.WorkflowSourceLink, claims []models.WorkflowEvidenceClaim) []GitHubQualityEvidenceReference {
	refs := make([]GitHubQualityEvidenceReference, 0, 1+len(links)+len(claims))
	expectedRepository := expectedGitHubRepository(item, links)
	refs = append(refs, GitHubQualityEvidenceReference{
		SourceType: item.SourceType, SourceID: item.SourceID, SourceURI: item.SourceURI,
		ExpectedRepository: expectedRepository,
	})
	for _, link := range links {
		refs = append(refs, GitHubQualityEvidenceReference{
			SourceType: link.SourceType, SourceID: link.SourceID, SourceURI: link.SourceURI,
			ExpectedRepository: expectedRepository,
		})
	}
	for _, claim := range claims {
		refs = append(refs, GitHubQualityEvidenceReference{SourceURI: claim.SourceURI, ExpectedRepository: expectedRepository})
	}
	return refs
}

var (
	workflowGitHubSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	workflowGitHubSHA  = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)
)

// expectedGitHubRepository is deliberately derived only from the workflow's
// primary source or a persisted GitHub origin link. Related links and claim
// URLs are candidate evidence, not authority to change the repository scope.
// Conflicting origins return an empty scope so the evidence resolver fails
// closed rather than selecting whichever repository is convenient.
func expectedGitHubRepository(item models.WorkflowItem, links []models.WorkflowSourceLink) string {
	repositories := map[string]string{}
	if item.SourceType == "github" {
		if repository := githubRepositoryFromWorkflowURI(item.SourceURI); repository != "" {
			repositories[strings.ToLower(repository)] = repository
		}
	}
	for _, link := range links {
		if link.SourceType != "github" || link.Relationship != "origin" {
			continue
		}
		if repository := githubRepositoryFromWorkflowURI(link.SourceURI); repository != "" {
			repositories[strings.ToLower(repository)] = repository
		}
	}
	if len(repositories) != 1 {
		return ""
	}
	for _, repository := range repositories {
		return repository
	}
	return ""
}

func githubRepositoryFromWorkflowURI(value string) string {
	if value == "" || strings.TrimSpace(value) != value {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) < 2 || !validWorkflowGitHubSlug(parts[0]) || !validWorkflowGitHubSlug(parts[1]) {
		return ""
	}
	if len(parts) == 2 {
		// A canonical repository page can bind scope without itself providing
		// quality evidence.
	} else if len(parts) == 4 {
		switch parts[2] {
		case "commit":
			if !workflowGitHubSHA.MatchString(parts[3]) {
				return ""
			}
		case "issues", "pull":
			if !canonicalWorkflowGitHubNumber(parts[3]) {
				return ""
			}
		default:
			return ""
		}
	} else if len(parts) == 5 && parts[2] == "actions" && parts[3] == "runs" && canonicalWorkflowGitHubNumber(parts[4]) {
		// GitHub Actions run URL.
	} else {
		return ""
	}
	if "https://github.com/"+parsed.EscapedPath()[1:] != value {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func validWorkflowGitHubSlug(value string) bool {
	return value != "" && value != "." && value != ".." && workflowGitHubSlug.MatchString(value)
}

func canonicalWorkflowGitHubNumber(value string) bool {
	number, err := strconv.ParseUint(value, 10, 64)
	return err == nil && number > 0 && strconv.FormatUint(number, 10) == value
}
