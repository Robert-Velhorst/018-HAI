package automation

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrApprovalIssuanceContextUnavailable = errors.New("owned approval proof issuance is unavailable")

type ContextualApprovalProofIssuer interface {
	IssueApprovalProofContext(context.Context, uuid.UUID, TaskApprovalProofRequest) (*ApprovalProof, error)
}

type ContextualApprovalProofSigner interface {
	IssueContext(context.Context, ApprovalProofIssueRequest) (*ApprovalProof, error)
}

func (s *service) IssueApprovalProofContext(ctx context.Context, id uuid.UUID, request TaskApprovalProofRequest) (*ApprovalProof, error) {
	if ctx == nil {
		return nil, ErrApprovalIssuanceContextUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, approvalRegistrationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	factory, ok := s.repo.(ContextualAutomationRepository)
	if !ok {
		return nil, ErrApprovalIssuanceContextUnavailable
	}
	repo, err := factory.WithAutomationRepositoryContext(ctx)
	if err != nil || repo == nil {
		return nil, errors.Join(ErrApprovalIssuanceContextUnavailable, err, ctx.Err())
	}
	return s.issueApprovalProofWithRepository(ctx, repo, id, request, true)
}

func (s *approvalProofService) IssueContext(ctx context.Context, request ApprovalProofIssueRequest) (*ApprovalProof, error) {
	if ctx == nil {
		return nil, ErrApprovalIssuanceContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	proof, err := s.Issue(request)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, errors.Join(err, contextErr)
	}
	return proof, err
}

func (s unavailableApprovalProofService) IssueContext(ctx context.Context, request ApprovalProofIssueRequest) (*ApprovalProof, error) {
	if ctx == nil {
		return nil, ErrApprovalIssuanceContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.Issue(request)
}
