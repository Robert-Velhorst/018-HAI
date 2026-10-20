package verification

import (
	"testing"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
)

func TestMemoryPromotionPreservesMeaningfulEvidenceCharacters(t *testing.T) {
	for _, test := range []struct{ name, claim, source string }{
		{"different accents", "Jos\u00e9 is ready", "Jos\u00e8 is ready."},
		{"different currencies", "The allowance is \u20ac50", "The allowance is $50."},
		{"different project identifiers", "Project TASK-A is active", "Project TASK A is active."},
		{"different version separators", "Project version is 2.5", "Project version is 2/5."},
		{"different non-Latin names", "Project \u6771\u4eac is active", "Project \u5927\u962a is active."},
		{"different URL structure", "Source is https://example.invalid/a?b=c", "Source is https://example.invalid/a/b/c."},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, run, claim, evidence := memoryPromotionFixture()
			claim.ClaimText = test.claim
			evidence.Snippet = test.source
			repository := &capturingMemoryRepository{}
			service := NewService(&fakeVerificationRepository{}, nil, memory.NewService(repository)).(*service)
			outcome := service.storeVerifiedMemory(request, &run, []models.VerificationClaim{claim}, []models.VerificationEvidence{evidence})
			if outcome.Stored != 0 || outcome.Skipped != 1 || len(repository.created) != 0 {
				t.Fatalf("different evidence text entered memory: outcome=%#v writes=%d", outcome, len(repository.created))
			}
		})
	}
}

func TestEvidenceMatchingRetainsUnicodeAndFormattingCompatibility(t *testing.T) {
	for _, test := range []struct{ name, claim, source string }{
		{"accented name and whitespace", "Jos\u00e9 is ready", "JOS\u00c9  is\tready!"},
		{"non-Latin statement", "\u6771\u4eac", "\u6771\u4eac."},
		{"quoted statement", "Jos\u00e9 is ready", "\u201cJos\u00e9 is ready.\u201d"},
		{"unchanged currency", "The allowance is \u20ac50", "The allowance is \u20ac50."},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !matchesEvidenceSentence(test.claim, test.source) {
				t.Fatal("same Unicode source sentence did not match")
			}
		})
	}
}

func TestMemoryPromotionAcceptsUnchangedNonLatinEvidence(t *testing.T) {
	request, run, claim, evidence := memoryPromotionFixture()
	claim.ClaimText = "\u6771\u4eac"
	evidence.Snippet = "\u6771\u4eac."
	repository := &capturingMemoryRepository{}
	service := NewService(&fakeVerificationRepository{}, nil, memory.NewService(repository)).(*service)
	outcome := service.storeVerifiedMemory(request, &run, []models.VerificationClaim{claim}, []models.VerificationEvidence{evidence})
	if outcome.Stored != 1 || outcome.Skipped != 0 || len(repository.created) != 1 || repository.created[0].Content != claim.ClaimText {
		t.Fatalf("unchanged non-Latin source sentence was not preserved: outcome=%#v writes=%d", outcome, len(repository.created))
	}
}
