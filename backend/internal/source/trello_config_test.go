package source

import (
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
)

func TestValidateTrelloSourceRequestReportsMissingExpectedMemberID(t *testing.T) {
	t.Setenv(trelloAPIKeyEnv, "test-key")
	t.Setenv(trelloReadTokenEnv, "test-read-token")
	t.Setenv(trelloOwnerIdentityEnv, "alice")
	t.Setenv(trelloAccountMemberIDEnv, "")

	err := validateTrelloSourceRequest(CreateSourceRequest{
		OwnerIdentity: "alice",
		SyncTarget:    "abc123XY",
	}, models.SourceConnector{Category: "project_board"})
	if err == nil || !strings.Contains(err.Error(), trelloAccountMemberIDEnv) {
		t.Fatalf("configuration error = %v, want guidance naming %s", err, trelloAccountMemberIDEnv)
	}
}
