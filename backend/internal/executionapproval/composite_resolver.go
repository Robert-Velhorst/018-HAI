package executionapproval

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/opscontrol"
	"gorm.io/gorm"
)

var ErrUnsupportedApprovalReference = errors.New("unsupported execution approval reference")

// CompositeResolver dispatches an approval reference to exactly one durable
// source. It never tries another resolver after the selected resolver rejects
// the evidence, preventing an unavailable or invalid approval from being
// weakened by fallback behavior.
type CompositeResolver struct {
	taskReview executionauth.ApprovalResolver
	workflow   executionauth.ApprovalResolver
	portfolio  executionauth.ApprovalResolver
	control    executionauth.ApprovalResolver
}

var _ executionauth.ApprovalResolver = (*CompositeResolver)(nil)

func NewCompositeResolver(
	taskReview executionauth.ApprovalResolver,
	workflow executionauth.ApprovalResolver,
	portfolio executionauth.ApprovalResolver,
	control executionauth.ApprovalResolver,
) (*CompositeResolver, error) {
	if isNilApprovalResolver(taskReview) {
		return nil, fmt.Errorf("%w: task review resolver is required", ErrInvalidRequest)
	}
	if isNilApprovalResolver(workflow) {
		return nil, fmt.Errorf("%w: workflow approval resolver is required", ErrInvalidRequest)
	}
	if isNilApprovalResolver(portfolio) {
		return nil, fmt.Errorf("%w: portfolio approval resolver is required", ErrInvalidRequest)
	}
	if isNilApprovalResolver(control) {
		return nil, fmt.Errorf("%w: owner control approval resolver is required", ErrInvalidRequest)
	}
	return &CompositeResolver{
		taskReview: taskReview,
		workflow:   workflow,
		portfolio:  portfolio,
		control:    control,
	}, nil
}

func (r *CompositeResolver) Resolve(
	ctx context.Context,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (executionauth.ResolvedApproval, error) {
	if r == nil ||
		isNilApprovalResolver(r.taskReview) ||
		isNilApprovalResolver(r.workflow) ||
		isNilApprovalResolver(r.portfolio) ||
		isNilApprovalResolver(r.control) {
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: approval resolver composite is not configured",
			ErrInvalidRequest,
		)
	}
	switch {
	case strings.HasPrefix(sourceID, taskReviewPrefix):
		return r.taskReview.Resolve(ctx, ownerIdentity, sourceID, bindingDigest)
	case strings.HasPrefix(sourceID, workflowDecisionPrefix):
		return r.workflow.Resolve(ctx, ownerIdentity, sourceID, bindingDigest)
	case strings.HasPrefix(sourceID, portfolioDecisionPrefix):
		return r.portfolio.Resolve(ctx, ownerIdentity, sourceID, bindingDigest)
	case strings.HasPrefix(sourceID, opscontrol.OwnerControlApprovalPrefix):
		return r.control.Resolve(ctx, ownerIdentity, sourceID, bindingDigest)
	default:
		return executionauth.ResolvedApproval{}, ErrUnsupportedApprovalReference
	}
}

func (r *CompositeResolver) ResolveInPostgresTransaction(
	ctx context.Context,
	tx *gorm.DB,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (executionauth.ResolvedApproval, error) {
	if r == nil || isNilApprovalResolver(r.taskReview) {
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: task review resolver is unavailable",
			ErrInvalidRequest,
		)
	}
	if !strings.HasPrefix(sourceID, taskReviewPrefix) {
		return executionauth.ResolvedApproval{}, ErrUnsupportedApprovalReference
	}
	resolver, ok := r.taskReview.(executionauth.PostgresTransactionApprovalResolver)
	if !ok || resolver == nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf(
			"%w: task review resolver cannot lock approval state in the execution transaction",
			ErrInvalidRequest,
		)
	}
	return resolver.ResolveInPostgresTransaction(ctx, tx, ownerIdentity, sourceID, bindingDigest)
}

const portfolioDecisionPrefix = "portfolio-decision:"

func isNilApprovalResolver(resolver executionauth.ApprovalResolver) bool {
	if resolver == nil {
		return true
	}
	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
