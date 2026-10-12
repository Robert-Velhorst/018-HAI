package background

import (
	"strings"
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestSafePayloadUsesCompleteOperationIdentity(t *testing.T) {
	first := models.Operation{ID: uuid.MustParse("12345678-0000-4000-8000-000000000001"), SourceRevisionHash: "one"}
	second := models.Operation{ID: uuid.MustParse("12345678-0000-4000-8000-000000000002"), SourceRevisionHash: "two"}
	a, b := safePayload(first), safePayload(second)
	if a.ArtifactName == b.ArtifactName {
		t.Fatal("different operations sharing a short prefix collide")
	}
	for _, op := range []models.Operation{first, second} {
		payload := safePayload(op)
		if payload.ArtifactName != "operation-"+op.ID.String()+".txt" || !strings.Contains(payload.Marker, op.ID.String()) || !strings.HasSuffix(payload.Marker, op.SourceRevisionHash) {
			t.Fatalf("payload lost complete identity or revision: %+v", payload)
		}
	}
}
