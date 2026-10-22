package openclawreconcile

import (
	"testing"

	"automation-hub-backend/internal/models"
)

func TestArtifactMetadataDoesNotOverrideExecutionState(t *testing.T) {
	for _, tc := range []struct {
		name, status, terminalStatus, want string
	}{
		{"running", "pending", "", "waiting_for_completion"},
		{"failed", "terminal", "failed", "unavailable"},
		{"cancelled", "terminal", "cancelled", "unavailable"},
		{"unknown terminal outcome", "terminal", "", "unavailable"},
		{"completed legacy metadata", "terminal", "completed", "metadata_available"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := models.OpenClawGatewayArtifactReceipt{ArtifactDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
			view := &ArtifactView{Items: []models.OpenClawGatewayArtifactReceipt{item}}
			finalizeArtifactViewState(view, models.OpenClawGatewaySessionReceipt{
				Status: tc.status, TerminalStatus: tc.terminalStatus,
			})
			if view.State != tc.want {
				t.Errorf("artifact state=%q, want %q", view.State, tc.want)
			}
			if len(view.Items) != 1 || view.Items[0].ArtifactDigest != item.ArtifactDigest {
				t.Fatal("changing the display state discarded existing metadata")
			}
		})
	}
}
