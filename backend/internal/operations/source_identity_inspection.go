package operations

import (
	"context"
	"fmt"
	"strings"

	"automation-hub-backend/internal/models"
)

// SourceIdentityMatches is an observation, never authority to rekey or execute.
type SourceIdentityMatches struct {
	Canonical  *models.Operation
	Historical *models.Operation
}

// InspectSourceIdentity reads both active keys without applying Ingest's
// canonical-match precedence. It performs no creation, refresh or audit write.
func (s *Service) InspectSourceIdentity(ctx context.Context, in NewOperationInput) (SourceIdentityMatches, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SourceIdentityMatches{}, err
	}
	in.OwnerUserID = strings.TrimSpace(in.OwnerUserID)
	in.WorkspaceID = firstNonEmpty(strings.TrimSpace(in.WorkspaceID), "local")
	if in.OwnerUserID == "" || strings.TrimSpace(in.DedupeKey) == "" {
		return SourceIdentityMatches{}, fmt.Errorf("operations: source identity inspection requires scope and key")
	}
	repo, ok := s.repo.(ContextIntakeRepository)
	if !ok {
		return SourceIdentityMatches{}, ErrContextIntakeUnsupported
	}
	lookup := func(key string) (*models.Operation, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		op, found, err := repo.FindByDedupeKeyContext(ctx, in.OwnerUserID, in.WorkspaceID, key)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateIntakeLookup(op, found, in.OwnerUserID, in.WorkspaceID, key); err != nil {
			return nil, err
		}
		return op, nil
	}
	canonical, err := lookup(in.DedupeKey)
	if err != nil {
		return SourceIdentityMatches{}, err
	}
	var historical *models.Operation
	if in.LegacyDedupeKey != "" && in.LegacyDedupeKey != in.DedupeKey {
		historical, err = lookup(in.LegacyDedupeKey)
		if err != nil {
			return SourceIdentityMatches{}, err
		}
	}
	return SourceIdentityMatches{Canonical: canonical, Historical: historical}, nil
}
