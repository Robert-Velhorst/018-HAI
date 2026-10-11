package memory

import (
	"context"
	"fmt"
	"strings"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"gorm.io/gorm"
)

const memorySearchExpression = "LOWER(COALESCE(content, '') || ' ' || COALESCE(summary, '') || ' ' || COALESCE(tags, ''))"

// QueryForOwner applies owner isolation, filters, deterministic ordering, and
// pagination before rows leave PostgreSQL. Search remains literal AND-token
// matching, preserving the public in-memory Query contract.
func (r *GormRepository) QueryForOwner(
	ctx context.Context,
	ownerIdentity string,
	projectKey string,
	includeArchived bool,
	params QueryParams,
) (PageResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p := params.normalized()
	query := r.scopeQuery(r.DB.WithContext(ctx), ownerIdentity, projectKey, includeArchived)
	if p.Kind != "" {
		query = query.Where("LOWER(kind) = LOWER(?)", p.Kind)
	}
	if p.Tag != "" {
		query = query.Where(`EXISTS (
			SELECT 1
			FROM unnest(string_to_array(COALESCE(tags, ''), ',')) AS memory_tag
			WHERE LOWER(BTRIM(memory_tag)) = LOWER(?)
		)`, p.Tag)
	}
	for _, token := range searchTokensOf(p.Search) {
		query = query.Where(memorySearchExpression+` LIKE ? ESCAPE E'\\'`, "%"+escapeLikeToken(token)+"%")
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return PageResult{}, err
	}
	maxInt := int64(int(^uint(0) >> 1))
	if count > maxInt {
		return PageResult{}, fmt.Errorf("memory query result exceeds platform capacity")
	}
	result := pageResult(p, int(count), []models.ContextMemory{})
	start, ok := pageStart(p, result.Total)
	if !ok {
		return result, nil
	}

	if err := query.
		Order(memoryQueryOrder(p)).
		Offset(start).
		Limit(p.PageSize).
		Find(&result.Items).Error; err != nil {
		return PageResult{}, err
	}
	return result, nil
}

func (r *GormRepository) scopeQuery(db *gorm.DB, ownerIdentity, projectKey string, includeArchived bool) *gorm.DB {
	query := db.Model(&models.ContextMemory{})
	if ownerIdentity = strings.TrimSpace(ownerIdentity); ownerIdentity != "" {
		if identity.CanReadLegacyOwnerlessData(ownerIdentity) {
			query = query.Where("owner_identity = ? OR owner_identity = '' OR owner_identity IS NULL", ownerIdentity)
		} else {
			query = query.Where("owner_identity = ?", ownerIdentity)
		}
	}
	if projectKey = strings.TrimSpace(projectKey); projectKey != "" {
		query = query.Where("project_key = ?", projectKey)
	}
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	return query
}

func memoryQueryOrder(p QueryParams) string {
	direction := "DESC"
	nulls := "NULLS LAST"
	if p.Order == "asc" {
		direction = "ASC"
		nulls = "NULLS FIRST"
	}
	suffix := direction + " " + nulls
	switch p.Sort {
	case "createdAt":
		return "created_at " + suffix + ", updated_at " + suffix + ", id " + direction
	case "confidence":
		return "confidence " + suffix + ", updated_at " + suffix + ", id " + direction
	case "kind":
		return "LOWER(kind) " + suffix + ", updated_at " + suffix + ", id " + direction
	default:
		// Relevance uses AND semantics, so every matching row contains every
		// distinct token. Updated time is therefore the deterministic tie-break.
		return "updated_at " + suffix + ", created_at " + suffix + ", id " + direction
	}
}

func escapeLikeToken(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	return strings.ReplaceAll(value, "_", `\_`)
}
