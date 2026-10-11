package verification

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/source"
)

type blockingVerificationSource struct {
	started chan struct{}
}

func (s *blockingVerificationSource) Search(source.SearchRequest) (*source.SearchResult, error) {
	return nil, errors.New("non-context search path used")
}

func (s *blockingVerificationSource) SearchContext(ctx context.Context, _ source.SearchRequest) (*source.SearchResult, error) {
	close(s.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestAnswerContextStopsBlockedEvidenceSearchAndMarksRunForReview(t *testing.T) {
	repo := &fakeVerificationRepository{}
	connected := &blockingVerificationSource{started: make(chan struct{})}
	service := NewService(repo, connected, nil)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := AnswerWithContext(service, ctx, AnswerRequest{
			OwnerIdentity: "alice", Question: "What is verified?", Mode: ModeStrict,
		})
		done <- err
	}()
	select {
	case <-connected.started:
	case <-time.After(time.Second):
		t.Fatal("verification evidence search did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("verification error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("verification ignored caller cancellation")
	}
	if len(repo.runs) != 1 || repo.runs[0].Status != StatusNeedsReview || repo.runs[0].MissingSources != "verification request stopped before completion" {
		t.Fatalf("canceled verification run = %#v", repo.runs)
	}
	if len(repo.claims) != 0 || len(repo.evidence) != 0 {
		t.Fatalf("canceled verification persisted claims/evidence: claims=%#v evidence=%#v", repo.claims, repo.evidence)
	}
}
