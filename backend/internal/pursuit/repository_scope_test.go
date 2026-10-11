package pursuit

import (
	"strings"
	"testing"
)

func TestOwnerVisibilitySQLAllowsLegacyRowsOnlyForConfiguredOwner(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "legacy-owner")
	operatorClause := strings.ToLower(ownerVisibilitySQL("pursuits.owner_identity", "operator"))
	if strings.Contains(operatorClause, " or ") || strings.Contains(operatorClause, "is null") {
		t.Fatalf("operator clause includes ownerless rows: %s", operatorClause)
	}
	legacyClause := strings.ToLower(ownerVisibilitySQL("pursuits.owner_identity", "legacy-owner"))
	if !strings.Contains(legacyClause, " or ") || !strings.Contains(legacyClause, "is null") {
		t.Fatalf("migration owner clause excludes ownerless rows: %s", legacyClause)
	}
}
