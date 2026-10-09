package source

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const githubEvidenceTestCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestResolveGitHubQualityEvidenceRequiresPersistedOwnerScopedRecords(t *testing.T) {
	tests := []struct {
		name       string
		owner      string
		uri        string
		mutate     func(*githubEvidenceTestFixture)
		wantCommit bool
	}{
		{name: "verified commit", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), wantCommit: true},
		{name: "query is rejected", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit) + "?claim=verified"},
		{name: "fragment is rejected", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit) + "#verified"},
		{name: "misleading host is rejected", owner: "owner-1", uri: "https://github.com.evil.invalid/acme/demo/commit/" + githubEvidenceTestCommit},
		{name: "wrong owner", owner: "owner-2", uri: githubCommitEvidenceURI(githubEvidenceTestCommit)},
		{name: "disabled source", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { f.source.Enabled = false }},
		{name: "revoked source", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { revoked := f.now.Add(-time.Hour); f.source.RevokedAt = &revoked }},
		{name: "non-active source", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { f.source.Status = "paused" }},
		{name: "stale source sync", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) {
			stale := f.now.Add(-25 * time.Hour)
			f.source.LastSyncedAt = &stale
			f.job.CompletedAt = &stale
		}},
		{name: "sync did not succeed", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { f.job.Status = "failed" }},
		{name: "stale raw record", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) {
			stale := f.now.Add(-25 * time.Hour)
			f.raw.FetchedAt = stale
			f.raw.UpdatedAt = stale
		}},
		{name: "configured repository mismatch", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { f.source.SyncTarget = "acme/other" }},
		{name: "raw external id mismatch", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { f.raw.ExternalID = "github:commit:" + strings.Repeat("b", 40) }},
		{name: "raw record type mismatch", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { f.raw.ItemType = "github_pull_request" }},
		{name: "extraction uri mismatch", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { f.extraction.SourceURI += "/" }},
		{name: "wrong connector", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) { f.source.ConnectorKey = "gitlab" }},
		{name: "commit proof comes from the matching record, not prose", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), wantCommit: true, mutate: func(f *githubEvidenceTestFixture) {
			f.raw.Title = "Tests passed and README updated"
			f.raw.Content = "success completed README setup docs"
			f.raw.Metadata = `{"source":"github","repository":"acme/demo","kind":"commit","status":"completed","conclusion":"success","fetched_at":"2026-09-24T11:00:00Z"}`
		}},
		{name: "unknown metadata rejected", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) {
			f.raw.Metadata = `{"source":"github","repository":"acme/demo","kind":"commit","untrusted":true}`
		}},
		{name: "stale importer observation", owner: "owner-1", uri: githubCommitEvidenceURI(githubEvidenceTestCommit), mutate: func(f *githubEvidenceTestFixture) {
			f.raw.Metadata = `{"source":"github","repository":"acme/demo","kind":"commit","fetched_at":"2026-09-23T11:00:00Z"}`
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newGitHubEvidenceTestFixture(t, "commit", githubEvidenceTestCommit)
			if test.mutate != nil {
				test.mutate(fixture)
			}
			fixture.repo.jobs[0] = *fixture.job
			resolver := newGitHubQualityEvidenceResolver(fixture.repo, func() time.Time { return fixture.now })
			got, err := resolver.ResolveGitHubQualityEvidence(test.owner, []workflow.GitHubQualityEvidenceReference{{
				SourceType:         "github",
				SourceID:           fixture.extraction.ID.String(),
				SourceURI:          test.uri,
				ExpectedRepository: "acme/demo",
			}})
			if err != nil {
				t.Fatalf("ResolveGitHubQualityEvidence: %v", err)
			}
			if got.Commit != test.wantCommit {
				t.Fatalf("Commit = %v, want %v (evidence=%#v)", got.Commit, test.wantCommit, got)
			}
			if test.wantCommit {
				if fixture.repo.lastSyncJobLimit != githubEvidenceSyncJobLimit || len(fixture.repo.lastSyncJobSourceIDs) != 1 || fixture.repo.lastSyncJobSourceIDs[0] != fixture.source.ID {
					t.Fatalf("sync lookup was not bounded and source-scoped: limit=%d sources=%v", fixture.repo.lastSyncJobLimit, fixture.repo.lastSyncJobSourceIDs)
				}
			}
		})
	}
}

func TestResolveGitHubQualityEvidenceRejectsOtherOwnerAuthorizedRepository(t *testing.T) {
	fixture := newGitHubEvidenceTestFixture(t, "commit", githubEvidenceTestCommit)
	otherRepositoryURI := "https://github.com/acme/other/commit/" + githubEvidenceTestCommit
	fixture.source.SyncTarget = "acme/other"
	fixture.raw.SourceURI = otherRepositoryURI
	fixture.raw.Metadata = marshalGitHubEvidenceTestMetadata(t, githubImportMetadata{
		Source: "github", Repository: "acme/other", Kind: "commit",
	})
	fixture.extraction.SourceURI = otherRepositoryURI
	resolver := newGitHubQualityEvidenceResolver(fixture.repo, func() time.Time { return fixture.now })
	ref := workflow.GitHubQualityEvidenceReference{
		SourceType: "github", SourceID: fixture.extraction.ID.String(), SourceURI: otherRepositoryURI,
		ExpectedRepository: "acme/demo",
	}

	got, err := resolver.ResolveGitHubQualityEvidence("owner-1", []workflow.GitHubQualityEvidenceReference{ref})
	if err != nil {
		t.Fatalf("ResolveGitHubQualityEvidence: %v", err)
	}
	if got.Commit {
		t.Fatalf("commit from a different owner-authorized repository satisfied the workflow gate: %#v", got)
	}

	ref.ExpectedRepository = "acme/other"
	got, err = resolver.ResolveGitHubQualityEvidence("owner-1", []workflow.GitHubQualityEvidenceReference{ref})
	if err != nil {
		t.Fatalf("ResolveGitHubQualityEvidence for the bound repository: %v", err)
	}
	if !got.Commit {
		t.Fatalf("properly bound commit evidence was rejected: %#v", got)
	}
}

func TestResolveGitHubQualityEvidenceRequiresWorkflowRepositoryBinding(t *testing.T) {
	fixture := newGitHubEvidenceTestFixture(t, "commit", githubEvidenceTestCommit)
	resolver := newGitHubQualityEvidenceResolver(fixture.repo, func() time.Time { return fixture.now })
	got, err := resolver.ResolveGitHubQualityEvidence("owner-1", []workflow.GitHubQualityEvidenceReference{{
		SourceType: "github", SourceID: fixture.extraction.ID.String(), SourceURI: githubCommitEvidenceURI(githubEvidenceTestCommit),
	}})
	if err != nil {
		t.Fatalf("ResolveGitHubQualityEvidence: %v", err)
	}
	if got.Commit || got.WorkflowSuccess || got.DocsChanged {
		t.Fatalf("evidence without an expected repository was accepted: %#v", got)
	}
}

func TestCurrentGitHubImporterProvesOnlyBoundCommitExistence(t *testing.T) {
	commit := newGitHubEvidenceTestFixture(t, "commit", githubEvidenceTestCommit)
	run := newGitHubEvidenceTestFixture(t, "workflow_run", "12345")
	pr := newGitHubEvidenceTestFixture(t, "pull_request", "7")
	addGitHubEvidenceFixture(t, commit, run)
	addGitHubEvidenceFixture(t, commit, pr)

	runItem, _ := githubImportItem(map[string]any{
		"id": float64(12345), "status": "completed", "conclusion": "success", "name": "Tests passed",
		"head_sha": githubEvidenceTestCommit, "html_url": githubRunEvidenceURI("12345"),
	}, "workflow_run", "project", "acme/demo")
	prItem, _ := githubImportItem(map[string]any{
		"number": float64(7), "merged": true, "merged_at": "2026-09-24T11:30:00Z",
		"merge_commit_sha": githubEvidenceTestCommit, "head": map[string]any{"sha": githubEvidenceTestCommit},
		"title": "README updated", "body": "The setup documentation was updated.", "html_url": githubPullEvidenceURI("7"),
	}, "pull_request", "project", "acme/demo")
	for _, record := range []struct {
		name string
		item ImportItem
		raw  *models.SourceRawItem
	}{
		{name: "Actions run", item: runItem, raw: run.raw},
		{name: "pull request", item: prItem, raw: pr.raw},
	} {
		t.Run(record.name, func(t *testing.T) {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(record.item.Metadata), &metadata); err != nil {
				t.Fatalf("decode imported metadata: %v", err)
			}
			for _, unsupported := range []string{"jobs", "steps", "check_runs", "changed_file_paths", "files"} {
				if _, persisted := metadata[unsupported]; persisted {
					t.Fatalf("current importer unexpectedly persisted unsupported evidence field %q", unsupported)
				}
			}
			record.raw.Title = record.item.Title
			record.raw.Content = record.item.Content
			importedMetadata, ok := decodeGitHubImportMetadata(record.item.Metadata)
			if !ok {
				t.Fatal("current importer produced invalid metadata")
			}
			importedMetadata.FetchedAt = commit.now.Add(-time.Minute).Format(time.RFC3339Nano)
			record.raw.Metadata = marshalGitHubEvidenceTestMetadata(t, importedMetadata)
		})
	}

	resolver := newGitHubQualityEvidenceResolver(commit.repo, func() time.Time { return commit.now })
	got, err := resolver.ResolveGitHubQualityEvidence("owner-1", []workflow.GitHubQualityEvidenceReference{
		{SourceType: "github", SourceID: commit.extraction.ID.String(), SourceURI: githubCommitEvidenceURI(githubEvidenceTestCommit), ExpectedRepository: "acme/demo"},
		{SourceType: "github", SourceID: run.extraction.ID.String(), SourceURI: githubRunEvidenceURI("12345"), ExpectedRepository: "acme/demo"},
		{SourceType: "github", SourceID: pr.extraction.ID.String(), SourceURI: githubPullEvidenceURI("7"), ExpectedRepository: "acme/demo"},
	})
	if err != nil {
		t.Fatalf("ResolveGitHubQualityEvidence: %v", err)
	}
	if !got.Commit {
		t.Fatalf("persisted commit record did not support commit-exists gate: %#v", got)
	}
	if got.WorkflowSuccess || got.DocsChanged {
		t.Fatalf("run labels/status or PR prose/merge metadata were promoted to unsupported quality evidence: %#v", got)
	}
}

func TestResolveGitHubQualityEvidenceValidatesActionsStatusAndConclusion(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		conclusion string
		want       bool
	}{
		{name: "completed failure", status: "completed", conclusion: "failure"},
		{name: "in progress with success conclusion", status: "in_progress", conclusion: "success"},
		{name: "completed success without linked target commit", status: "completed", conclusion: "success"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newGitHubEvidenceTestFixture(t, "workflow_run", "12345")
			fixture.raw.Title = "All tests passed"
			fixture.raw.Content = "success; completed; build passed"
			fixture.raw.Metadata = marshalGitHubEvidenceTestMetadata(t, githubImportMetadata{
				Source: "github", Repository: "acme/demo", Kind: "workflow_run", Status: test.status, Conclusion: test.conclusion,
			})
			resolver := newGitHubQualityEvidenceResolver(fixture.repo, func() time.Time { return fixture.now })
			got, err := resolver.ResolveGitHubQualityEvidence("owner-1", []workflow.GitHubQualityEvidenceReference{{
				SourceType: "github", SourceID: fixture.extraction.ID.String(), SourceURI: githubRunEvidenceURI("12345"), ExpectedRepository: "acme/demo",
			}})
			if err != nil {
				t.Fatalf("ResolveGitHubQualityEvidence: %v", err)
			}
			if got.WorkflowSuccess != test.want {
				t.Fatalf("WorkflowSuccess = %v, want %v", got.WorkflowSuccess, test.want)
			}
		})
	}
}

func TestResolveGitHubQualityEvidenceDoesNotInferWorkflowSuccessFromRunLabels(t *testing.T) {
	const targetCommit = githubEvidenceTestCommit
	const otherCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tests := []struct {
		name          string
		headSHA       string
		workflowName  string
		workflowPath  string
		workflowEvent string
		attachCommit  bool
		wantSuccess   bool
	}{
		{name: "test-labeled workflow without job evidence", headSHA: targetCommit, workflowName: "Go tests", workflowPath: ".github/workflows/tests.yml", workflowEvent: "push", attachCommit: true},
		{name: "different commit", headSHA: otherCommit, workflowName: "Go tests", workflowPath: ".github/workflows/tests.yml", workflowEvent: "push", attachCommit: true},
		{name: "no linked commit", headSHA: targetCommit, workflowName: "Go tests", workflowPath: ".github/workflows/tests.yml", workflowEvent: "push"},
		{name: "successful deployment workflow", headSHA: targetCommit, workflowName: "Deploy production", workflowPath: ".github/workflows/deploy.yml", workflowEvent: "push", attachCommit: true},
		{name: "ambiguous CI workflow", headSHA: targetCommit, workflowName: "CI", workflowPath: ".github/workflows/ci.yml", workflowEvent: "push", attachCommit: true},
		{name: "test marker does not override deployment path", headSHA: targetCommit, workflowName: "Run tests then deploy", workflowPath: ".github/workflows/deploy.yml", workflowEvent: "push", attachCommit: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := newGitHubEvidenceTestFixture(t, "workflow_run", "12345")
			run.raw.Metadata = marshalGitHubEvidenceTestMetadata(t, githubImportMetadata{
				Source: "github", Repository: "acme/demo", Kind: "workflow_run",
				Status: "completed", Conclusion: "success", HeadSHA: test.headSHA,
				WorkflowName: test.workflowName, WorkflowPath: test.workflowPath, WorkflowEvent: test.workflowEvent,
			})
			references := []workflow.GitHubQualityEvidenceReference{{
				SourceType: "github", SourceID: run.extraction.ID.String(), SourceURI: githubRunEvidenceURI("12345"), ExpectedRepository: "acme/demo",
			}}
			if test.attachCommit {
				commit := newGitHubEvidenceTestFixture(t, "commit", targetCommit)
				addGitHubEvidenceFixture(t, run, commit)
				references = append(references, workflow.GitHubQualityEvidenceReference{
					SourceType: "github", SourceID: commit.extraction.ID.String(), SourceURI: githubCommitEvidenceURI(targetCommit), ExpectedRepository: "acme/demo",
				})
			}
			resolver := newGitHubQualityEvidenceResolver(run.repo, func() time.Time { return run.now })
			got, err := resolver.ResolveGitHubQualityEvidence("owner-1", references)
			if err != nil {
				t.Fatalf("ResolveGitHubQualityEvidence: %v", err)
			}
			if got.WorkflowSuccess != test.wantSuccess {
				t.Fatalf("WorkflowSuccess = %v, want %v (evidence=%#v)", got.WorkflowSuccess, test.wantSuccess, got)
			}
		})
	}
}

func TestResolveGitHubQualityEvidenceRejectsSuccessfulNoOpSmokeTestsRun(t *testing.T) {
	const runID = "12345"
	fixture := newGitHubEvidenceTestFixture(t, "workflow_run", runID)
	commit := newGitHubEvidenceTestFixture(t, "commit", githubEvidenceTestCommit)
	addGitHubEvidenceFixture(t, fixture, commit)

	item, _ := githubImportItem(map[string]any{
		"id": float64(12345), "status": "completed", "conclusion": "success",
		"name": "Smoke tests", "path": ".github/workflows/smoke.yml", "event": "push",
		"head_sha": githubEvidenceTestCommit, "html_url": githubRunEvidenceURI(runID),
	}, "workflow_run", "project", "acme/demo")
	var imported map[string]any
	if err := json.Unmarshal([]byte(item.Metadata), &imported); err != nil {
		t.Fatalf("decode imported workflow metadata: %v", err)
	}
	if _, hasJobs := imported["jobs"]; hasJobs {
		t.Fatal("current importer unexpectedly persisted job evidence; update the test to validate its concrete evidence contract")
	}
	if _, hasSteps := imported["steps"]; hasSteps {
		t.Fatal("current importer unexpectedly persisted step evidence; update the test to validate its concrete evidence contract")
	}
	metadata, ok := decodeGitHubImportMetadata(item.Metadata)
	if !ok {
		t.Fatal("current importer produced invalid workflow metadata")
	}
	metadata.FetchedAt = fixture.now.Add(-time.Minute).Format(time.RFC3339Nano)
	fixture.raw.Title = item.Title
	fixture.raw.Content = item.Content
	fixture.raw.Metadata = marshalGitHubEvidenceTestMetadata(t, metadata)

	resolver := newGitHubQualityEvidenceResolver(fixture.repo, func() time.Time { return fixture.now })
	got, err := resolver.ResolveGitHubQualityEvidence("owner-1", []workflow.GitHubQualityEvidenceReference{
		{SourceType: "github", SourceID: fixture.extraction.ID.String(), SourceURI: githubRunEvidenceURI(runID), ExpectedRepository: "acme/demo"},
		{SourceType: "github", SourceID: commit.extraction.ID.String(), SourceURI: githubCommitEvidenceURI(githubEvidenceTestCommit), ExpectedRepository: "acme/demo"},
	})
	if err != nil {
		t.Fatalf("ResolveGitHubQualityEvidence: %v", err)
	}
	if got.WorkflowSuccess {
		t.Fatalf("successful no-op Smoke tests workflow was treated as quality evidence: %#v", got)
	}
}

func TestResolveGitHubQualityEvidenceKeepsPRDocsGateUnprovenWithoutFetchedFileEvidence(t *testing.T) {
	fixture := newGitHubEvidenceTestFixture(t, "pull_request", "7")
	fixture.raw.Title = "README and setup documentation updated"
	fixture.raw.Content = "This pull request updates the README and setup guide."
	fixture.raw.Metadata = `{"source":"github","repository":"acme/demo","kind":"pull_request","fetched_at":"2026-09-24T11:00:00Z","merged":true,"changed_file_paths":["README.md"]}`
	resolver := newGitHubQualityEvidenceResolver(fixture.repo, func() time.Time { return fixture.now })
	got, err := resolver.ResolveGitHubQualityEvidence("owner-1", []workflow.GitHubQualityEvidenceReference{{
		SourceType: "github", SourceID: fixture.extraction.ID.String(), SourceURI: githubPullEvidenceURI("7"), ExpectedRepository: "acme/demo",
	}})
	if err != nil {
		t.Fatalf("ResolveGitHubQualityEvidence: %v", err)
	}
	if got.DocsChanged {
		t.Fatalf("PR prose or legacy unproven paths satisfied the docs gate: %#v", got)
	}
}

func TestResolveGitHubQualityEvidenceRejectsMalformedReferenceAndOwner(t *testing.T) {
	fixture := newGitHubEvidenceTestFixture(t, "commit", githubEvidenceTestCommit)
	resolver := newGitHubQualityEvidenceResolver(fixture.repo, func() time.Time { return fixture.now })
	for _, owner := range []string{"", " owner-1", "owner-1 "} {
		got, err := resolver.ResolveGitHubQualityEvidence(owner, []workflow.GitHubQualityEvidenceReference{{
			SourceType: "github", SourceID: fixture.extraction.ID.String(), SourceURI: githubCommitEvidenceURI(githubEvidenceTestCommit), ExpectedRepository: "acme/demo",
		}})
		if err != nil || got.Commit {
			t.Fatalf("owner %q resolved evidence=%#v err=%v; want no evidence", owner, got, err)
		}
	}
	tooMany := make([]workflow.GitHubQualityEvidenceReference, githubEvidenceMaxReferences+1)
	if _, err := resolver.ResolveGitHubQualityEvidence("owner-1", tooMany); err == nil {
		t.Fatal("expected bounded reference lookup to reject oversized input")
	}
}

func TestNewGitHubQualityEvidenceResolverImplementsWorkflowContract(t *testing.T) {
	fixture := newGitHubEvidenceTestFixture(t, "commit", githubEvidenceTestCommit)
	if resolver := NewGitHubQualityEvidenceResolver(fixture.repo); resolver == nil {
		t.Fatal("NewGitHubQualityEvidenceResolver returned nil")
	}
	if resolver := NewGitHubQualityEvidenceResolver(nil); resolver != nil {
		t.Fatal("nil repository should not produce a usable resolver")
	}
}

func TestGitHubImportItemPreservesStructuredActionsOutcome(t *testing.T) {
	item, _ := githubImportItem(map[string]any{
		"id": float64(12345), "node_id": "WFR_node", "status": "completed", "conclusion": "failure", "head_sha": githubEvidenceTestCommit,
		"name": "  Go test suite ", "path": " .github/workflows/test.yml", "event": "push ",
		"title": "Tests passed", "html_url": githubRunEvidenceURI("12345"),
	}, "workflow_run", "project", "acme/demo")
	if item.ExternalID != "github:workflow_run:12345" {
		t.Fatalf("external ID = %q", item.ExternalID)
	}
	metadata, ok := decodeGitHubImportMetadata(item.Metadata)
	if !ok || metadata.Status != "completed" || metadata.Conclusion != "failure" || metadata.HeadSHA != githubEvidenceTestCommit ||
		metadata.WorkflowName != "  Go test suite " || metadata.WorkflowPath != " .github/workflows/test.yml" || metadata.WorkflowEvent != "push " || !metadata.ReviewRequired {
		t.Fatalf("structured Actions result was not preserved: metadata=%#v valid=%v", metadata, ok)
	}
}

func TestGitHubImportItemPreservesPullRequestMergeBinding(t *testing.T) {
	item, _ := githubImportItem(map[string]any{
		"number": float64(7), "html_url": githubPullEvidenceURI("7"), "merged": false,
		"merged_at": "2026-09-24T11:00:00Z", "merge_commit_sha": githubEvidenceTestCommit,
		"head": map[string]any{"sha": githubEvidenceTestCommit},
	}, "pull_request", "project", "acme/demo")
	metadata, ok := decodeGitHubImportMetadata(item.Metadata)
	if !ok || !metadata.Merged || metadata.HeadSHA != githubEvidenceTestCommit || metadata.MergeCommitSHA != githubEvidenceTestCommit {
		t.Fatalf("pull request merge binding was not preserved: metadata=%#v valid=%v", metadata, ok)
	}
}

func TestGitHubImportItemRejectsNoncanonicalIdentifiers(t *testing.T) {
	for name, identifier := range map[string]any{
		"fractional number": float64(12345.5),
		"leading zero":      "012345",
	} {
		t.Run(name, func(t *testing.T) {
			item, _ := githubImportItem(map[string]any{
				"id": identifier, "status": "completed", "conclusion": "success", "html_url": githubRunEvidenceURI("12345"),
			}, "workflow_run", "project", "acme/demo")
			if item.ExternalID != "" {
				t.Fatalf("noncanonical GitHub identifier produced external ID %q", item.ExternalID)
			}
		})
	}
}

func TestGitHubImportItemDoesNotClaimPRChangedFilePathsWithoutDedicatedFetch(t *testing.T) {
	item, _ := githubImportItem(map[string]any{
		"number": float64(7), "title": "README and setup updated", "body": "docs were updated", "html_url": githubPullEvidenceURI("7"),
	}, "pull_request", "project", "acme/demo")
	itemWithEmbeddedFiles, _ := githubImportItem(map[string]any{
		"number": float64(7), "title": "Unrelated", "html_url": githubPullEvidenceURI("7"),
		"files": []any{map[string]any{"filename": "README.md"}, map[string]any{"filename": "docs/architecture.md"}},
	}, "pull_request", "project", "acme/demo")
	for _, imported := range []ImportItem{item, itemWithEmbeddedFiles} {
		var metadata map[string]any
		if err := json.Unmarshal([]byte(imported.Metadata), &metadata); err != nil {
			t.Fatalf("decode imported metadata: %v", err)
		}
		if _, ok := metadata["changed_file_paths"]; ok {
			t.Fatalf("GitHub PR files were persisted without a dedicated bounded file-list fetch: %s", imported.Metadata)
		}
	}
}

type githubEvidenceTestFixture struct {
	repo       *fakeSourceRepo
	source     *models.ConnectedSource
	raw        *models.SourceRawItem
	extraction *models.SourceExtraction
	job        *models.SourceSyncJob
	now        time.Time
}

func newGitHubEvidenceTestFixture(t *testing.T, kind, identity string) *githubEvidenceTestFixture {
	t.Helper()
	now := time.Date(2026, time.September, 24, 12, 0, 0, 0, time.UTC)
	sourceID := uuid.New()
	rawID := uuid.New()
	extractionID := uuid.New()
	uri := ""
	itemType := "github_" + kind
	var metadata githubImportMetadata
	switch kind {
	case "commit":
		uri = githubCommitEvidenceURI(identity)
		metadata = githubImportMetadata{Source: "github", Repository: "acme/demo", Kind: kind}
	case "workflow_run":
		uri = githubRunEvidenceURI(identity)
		metadata = githubImportMetadata{Source: "github", Repository: "acme/demo", Kind: kind, Status: "completed", Conclusion: "success"}
	case "pull_request":
		uri = githubPullEvidenceURI(identity)
		metadata = githubImportMetadata{Source: "github", Repository: "acme/demo", Kind: kind}
	default:
		t.Fatalf("unsupported test record kind %q", kind)
	}
	lastSynced := now.Add(-time.Hour)
	metadata.FetchedAt = lastSynced.Format(time.RFC3339Nano)
	source := &models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "owner-1", ConnectorKey: "github", Category: "github", Enabled: true,
		Status: "active", SyncTarget: "acme/demo", LastSyncedAt: &lastSynced,
	}
	raw := &models.SourceRawItem{
		ID: rawID, SourceID: sourceID, ExternalID: fmt.Sprintf("github:%s:%s", kind, identity), ItemType: itemType,
		Title: "Imported record", Content: "persisted source content", SourceURI: uri, Metadata: marshalGitHubEvidenceTestMetadata(t, metadata),
		FetchedAt: lastSynced, UpdatedAt: lastSynced,
	}
	extraction := &models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: rawID, ContentType: itemType, SourceURI: uri,
	}
	job := &models.SourceSyncJob{ID: uuid.New(), SourceID: sourceID, Status: "completed", CompletedAt: &lastSynced}
	repo := newFakeSourceRepo(source)
	repo.rawItems[rawID] = raw
	repo.extractions[extractionID] = extraction
	repo.jobs = []models.SourceSyncJob{*job}
	return &githubEvidenceTestFixture{repo: repo, source: source, raw: raw, extraction: extraction, job: job, now: now}
}

func addGitHubEvidenceFixture(t *testing.T, base, extra *githubEvidenceTestFixture) {
	t.Helper()
	base.repo.sources[extra.source.ID] = extra.source
	base.repo.rawItems[extra.raw.ID] = extra.raw
	base.repo.extractions[extra.extraction.ID] = extra.extraction
	base.repo.jobs = append(base.repo.jobs, *extra.job)
}

func marshalGitHubEvidenceTestMetadata(t *testing.T, metadata githubImportMetadata) string {
	t.Helper()
	if metadata.FetchedAt == "" {
		metadata.FetchedAt = time.Date(2026, time.September, 24, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal test metadata: %v", err)
	}
	return string(data)
}

func githubCommitEvidenceURI(identity string) string {
	return "https://github.com/acme/demo/commit/" + identity
}

func githubRunEvidenceURI(identity string) string {
	return "https://github.com/acme/demo/actions/runs/" + identity
}

func githubPullEvidenceURI(identity string) string {
	return "https://github.com/acme/demo/pull/" + identity
}
