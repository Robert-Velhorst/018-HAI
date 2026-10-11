package verification

import (
	"testing"

	"github.com/google/uuid"
)

func TestAnswerDoesNotPromoteTestPassToGeneralVerification(t *testing.T) {
	resolver := EvidenceAuthorityResolverFunc(func(_ AnswerRequest, _ EvidenceInput) EvidenceAuthorityResolution {
		return EvidenceAuthorityResolution{Trusted: true, Authority: "ci_test", Primary: true}
	})
	service := NewServiceWithAuthorityResolver(&fakeVerificationRepository{}, nil, nil, resolver)

	result, err := service.Answer(AnswerRequest{
		OwnerIdentity: "alice",
		Question:      "Did the API smoke test pass?",
		DraftAnswer:   "The API smoke test passed successfully.",
		Mode:          ModeGrounded,
		ExternalEvidence: []EvidenceInput{{
			SourceType: "test_result",
			SourceID:   uuid.NewString(),
			SourceURI:  "ci://runs/42/tests/smoke",
			Snippet:    "The API smoke test passed successfully.",
		}},
	})
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != StatusTestPassed {
		t.Fatalf("claim status = %#v, want one test_passed claim", result.Claims)
	}
	if result.Run.Status != StatusTestPassed {
		t.Fatalf("run status = %q, want %q", result.Run.Status, StatusTestPassed)
	}
}
