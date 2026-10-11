package executionapproval

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"automation-hub-backend/internal/executionauth"
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
	additional []ApprovalResolverRegistration
}

// ApprovalResolverRegistration adds a server-owned approval namespace without
// weakening the three canonical resolvers. Prefixes must be explicit and
// disjoint so a reference is always routed to exactly one durable source.
type ApprovalResolverRegistration struct {
	Prefix   string
	Resolver executionauth.ApprovalResolver
}

func RegisterApprovalResolver(
	prefix string,
	resolver executionauth.ApprovalResolver,
) ApprovalResolverRegistration {
	return ApprovalResolverRegistration{Prefix: prefix, Resolver: resolver}
}

var _ executionauth.ApprovalResolver = (*CompositeResolver)(nil)

func NewCompositeResolver(
	taskReview executionauth.ApprovalResolver,
	workflow executionauth.ApprovalResolver,
	portfolio executionauth.ApprovalResolver,
	additional ...ApprovalResolverRegistration,
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
	seen := map[string]struct{}{
		taskReviewPrefix:        {},
		workflowDecisionPrefix:  {},
		portfolioDecisionPrefix: {},
	}
	for index := range additional {
		additional[index].Prefix = strings.TrimSpace(additional[index].Prefix)
		if additional[index].Prefix == "" ||
			!strings.HasSuffix(additional[index].Prefix, ":") ||
			isNilApprovalResolver(additional[index].Resolver) {
			return nil, fmt.Errorf("%w: additional approval resolver is invalid", ErrInvalidRequest)
		}
		if _, exists := seen[additional[index].Prefix]; exists {
			return nil, fmt.Errorf("%w: duplicate approval resolver prefix", ErrInvalidRequest)
		}
		seen[additional[index].Prefix] = struct{}{}
	}
	return &CompositeResolver{
		taskReview: taskReview,
		workflow:   workflow,
		portfolio:  portfolio,
		additional: append([]ApprovalResolverRegistration(nil), additional...),
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
		isNilApprovalResolver(r.portfolio) {
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
	default:
		for _, registration := range r.additional {
			if strings.HasPrefix(sourceID, registration.Prefix) {
				return registration.Resolver.Resolve(
					ctx,
					ownerIdentity,
					sourceID,
					bindingDigest,
				)
			}
		}
		return executionauth.ResolvedApproval{}, ErrUnsupportedApprovalReference
	}
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
