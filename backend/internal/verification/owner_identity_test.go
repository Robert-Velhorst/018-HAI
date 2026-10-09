package verification

import (
	"testing"

	"automation-hub-backend/internal/source"
)

type countingAnswerSearcher struct{ calls int }

func (s *countingAnswerSearcher) Search(source.SearchRequest) (*source.SearchResult, error) {
	s.calls++
	return &source.SearchResult{}, nil
}

func TestAnswerRejectsMissingOwnerBeforeCreatingRunOrSearchingSources(t *testing.T) {
	repository := &fakeVerificationRepository{}
	searcher := &countingAnswerSearcher{}
	service := NewService(repository, searcher, nil)

	if _, err := service.Answer(AnswerRequest{OwnerIdentity: " \t ", Question: "Find my record"}); err == nil {
		t.Fatal("Answer accepted a missing owner identity")
	}
	if len(repository.runs) != 0 {
		t.Fatalf("ownerless answer created verification runs: %#v", repository.runs)
	}
	if searcher.calls != 0 {
		t.Fatalf("ownerless answer searched connected sources %d times", searcher.calls)
	}
}

func TestAnswerNormalizesOwnerBeforePersistingAndSearching(t *testing.T) {
	repository := &fakeVerificationRepository{}
	searcher := &countingAnswerSearcher{}
	service := NewService(repository, searcher, nil)

	result, err := service.Answer(AnswerRequest{OwnerIdentity: " alice ", Question: "Find my record"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if result.Run.OwnerIdentity != "alice" {
		t.Fatalf("stored owner identity = %q, want trimmed identity", result.Run.OwnerIdentity)
	}
	if searcher.calls == 0 {
		t.Fatal("Answer did not search the configured source")
	}
}
