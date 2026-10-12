package source

import (
	"strings"
	"testing"
)

func TestDoclingSourceCannotBeCreatedUntilTheLocalRunnerIsConfigured(t *testing.T) {
	t.Setenv("HAI_DOCLING_ENABLED", "false")
	service := NewService(newFakeSourceRepo(), nil)
	_, err := service.CreateSource(CreateSourceRequest{
		OwnerIdentity: "alice",
		ConnectorKey:  doclingDocumentsConnectorKey,
		Name:          "Legal evidence",
		LocalOnly:     true,
		SyncTarget:    "legal/vivare",
	})
	if err == nil || !strings.Contains(err.Error(), "configured local runner") {
		t.Fatalf("CreateSource error = %v, want local runner configuration requirement", err)
	}
}
