package workflow

import (
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestGitHubQualityGatesDoNotTrustCallerURLsLabelsOrClaims(t *testing.T) {
	repo := newFakeWorkflowRepo()
	item := models.WorkflowItem{
		ID:          uuid.New(),
		TaskType:    "technical",
		SourceType:  "github",
		SourceID:    "caller-controlled-id",
		SourceURI:   "https://github.com/acme/repo/commit/fake?next=/actions/runs/99",
		SourceLabel: "README setup documentation updated; tests completed successfully",
	}
	for _, gate := range qualityGatesForAnalysis(item.ID, inputAnalysis{taskType: item.TaskType}) {
		if _, err := repo.CreateQualityGate(&gate); err != nil {
			t.Fatalf("CreateQualityGate(%q): %v", gate.Gate, err)
		}
	}
	if _, err := repo.CreateSourceLink(&models.WorkflowSourceLink{
		WorkflowID: item.ID, SourceType: "github", SourceID: item.SourceID,
		SourceURI: item.SourceURI, SourceLabel: item.SourceLabel, Relationship: "origin",
	}); err != nil {
		t.Fatalf("CreateSourceLink: %v", err)
	}
	if _, err := repo.CreateEvidenceClaim(&models.WorkflowEvidenceClaim{
		WorkflowID: item.ID, ClaimText: "Actions tests completed successfully; README updated",
		SourceURI: item.SourceURI, SourceLabel: item.SourceLabel, Reliability: "direct_source", Status: "source_linked",
	}); err != nil {
		t.Fatalf("CreateEvidenceClaim: %v", err)
	}

	service := NewService(repo).(*service)
	result := service.evaluateQualityGates(item, &TaskRunResult{Passed: true, VerificationStatus: "verified"})
	if result.Passed || !result.ReviewRequired {
		t.Fatalf("caller-controlled pointers satisfied mandatory GitHub gates: %+v", result)
	}
	stored, err := repo.FindQualityGates(item.ID)
	if err != nil {
		t.Fatalf("FindQualityGates: %v", err)
	}
	for _, gate := range []string{"GitHub commit exists", "tests or build evidence", "README/setup updated", "Windows 11 operational path"} {
		if !hasGateStatus(stored, gate, "needs_review") {
			t.Errorf("gate %q was not held for review: %+v", gate, stored)
		}
	}
}

func TestGitHubQualityEvidenceReferencesStayBoundToTheWorkflowOriginRepository(t *testing.T) {
	item := models.WorkflowItem{
		SourceType: "github",
		SourceID:   "issue-12",
		SourceURI:  "https://github.com/acme/expected/issues/12",
	}
	links := []models.WorkflowSourceLink{
		{SourceType: "github", SourceURI: "https://github.com/acme/expected/pull/9", Relationship: "origin"},
		{SourceType: "github", SourceURI: "https://github.com/acme/other/commit/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Relationship: "related"},
	}
	claims := []models.WorkflowEvidenceClaim{{SourceURI: "https://github.com/acme/other/actions/runs/123"}}

	references := githubQualityEvidenceReferences(item, links, claims)
	if len(references) != 4 {
		t.Fatalf("references = %d, want 4", len(references))
	}
	for index, reference := range references {
		if reference.ExpectedRepository != "acme/expected" {
			t.Errorf("reference %d expected repository = %q, want acme/expected", index, reference.ExpectedRepository)
		}
	}
	if references[2].SourceURI != links[1].SourceURI {
		t.Fatalf("related cross-repository candidate was not retained for resolver rejection: %#v", references[2])
	}
}

func TestGitHubQualityEvidenceReferencesFailClosedWithoutOneCanonicalOrigin(t *testing.T) {
	tests := []struct {
		name  string
		item  models.WorkflowItem
		links []models.WorkflowSourceLink
	}{
		{
			name:  "non-GitHub source with only related GitHub link",
			item:  models.WorkflowItem{SourceType: "trello", SourceURI: "https://trello.example/card/1"},
			links: []models.WorkflowSourceLink{{SourceType: "github", SourceURI: "https://github.com/acme/repo/commit/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Relationship: "related"}},
		},
		{
			name:  "conflicting primary source and origin link",
			item:  models.WorkflowItem{SourceType: "github", SourceURI: "https://github.com/acme/repo/issues/1"},
			links: []models.WorkflowSourceLink{{SourceType: "github", SourceURI: "https://github.com/acme/other/issues/2", Relationship: "origin"}},
		},
		{
			name: "query-bearing origin",
			item: models.WorkflowItem{SourceType: "github", SourceURI: "https://github.com/acme/repo/issues/1?redirect=other"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for index, reference := range githubQualityEvidenceReferences(test.item, test.links, nil) {
				if reference.ExpectedRepository != "" {
					t.Errorf("reference %d unexpectedly bound to %q", index, reference.ExpectedRepository)
				}
			}
		})
	}
}
