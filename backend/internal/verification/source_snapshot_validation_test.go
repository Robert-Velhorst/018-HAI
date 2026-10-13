package verification

import (
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/sourceevidence"
	"github.com/google/uuid"
)

func TestSourceSnapshotMatchesExactExtractionAndRawItem(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	extraction := models.SourceExtraction{
		ID: uuid.New(), SourceID: uuid.New(), RawItemID: uuid.New(), ProjectKey: "hai",
		SourceURI: "https://example.test/message/1", ContentHash: "abc123", UpdatedAt: now,
	}
	snapshot := sourceevidence.Snapshot{
		OwnerIdentity: "owner-1", ExtractionID: extraction.ID.String(), SourceID: extraction.SourceID.String(),
		RawItemID: extraction.RawItemID.String(), ProjectKey: extraction.ProjectKey, RawProjectKey: extraction.ProjectKey,
		ExtractionURI: extraction.SourceURI, RawItemURI: extraction.SourceURI,
		ExtractionHash: extraction.ContentHash, RawItemHash: extraction.ContentHash,
		ExtractionPayloadDigest: sourceevidence.ExtractionPayloadDigest(extraction),
		FetchedAt: now.Add(-time.Minute), ExtractionAt: now, ConnectorKey: "gmail",
	}
	snapshot.SnapshotDigest = sourceevidence.SnapshotDigest(snapshot)
	payloadDigest := sourceevidence.ExtractionPayloadDigest(extraction)
	if !sourceSnapshotMatches("owner-1", extraction, snapshot, payloadDigest) {
		t.Fatal("valid owner-bound extraction snapshot was rejected")
	}

	tests := []struct {
		name   string
		mutate func(*sourceevidence.Snapshot)
	}{
		{name: "owner", mutate: func(s *sourceevidence.Snapshot) { s.OwnerIdentity = "owner-2" }},
		{name: "extraction", mutate: func(s *sourceevidence.Snapshot) { s.ExtractionID = uuid.NewString() }},
		{name: "source", mutate: func(s *sourceevidence.Snapshot) { s.SourceID = uuid.NewString() }},
		{name: "raw item", mutate: func(s *sourceevidence.Snapshot) { s.RawItemID = uuid.NewString() }},
		{name: "project", mutate: func(s *sourceevidence.Snapshot) { s.ProjectKey = "other" }},
		{name: "raw project", mutate: func(s *sourceevidence.Snapshot) { s.RawProjectKey = "other" }},
		{name: "extraction uri", mutate: func(s *sourceevidence.Snapshot) { s.ExtractionURI = "https://example.test/other" }},
		{name: "raw uri", mutate: func(s *sourceevidence.Snapshot) { s.RawItemURI = "https://example.test/other" }},
		{name: "extraction hash", mutate: func(s *sourceevidence.Snapshot) { s.ExtractionHash = "other" }},
		{name: "raw hash", mutate: func(s *sourceevidence.Snapshot) { s.RawItemHash = "other" }},
		{name: "payload digest", mutate: func(s *sourceevidence.Snapshot) { s.ExtractionPayloadDigest = "other" }},
		{name: "fetched time", mutate: func(s *sourceevidence.Snapshot) { s.FetchedAt = time.Time{} }},
		{name: "extraction time", mutate: func(s *sourceevidence.Snapshot) { s.ExtractionAt = time.Time{} }},
		{name: "connector", mutate: func(s *sourceevidence.Snapshot) { s.ConnectorKey = " " }},
		{name: "snapshot digest", mutate: func(s *sourceevidence.Snapshot) { s.SnapshotDigest = "invalid" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := snapshot
			test.mutate(&mutated)
			if test.name != "snapshot digest" {
				mutated.SnapshotDigest = sourceevidence.SnapshotDigest(mutated)
			}
			if sourceSnapshotMatches("owner-1", extraction, mutated, payloadDigest) {
				t.Fatal("mismatched snapshot was accepted")
			}
		})
	}
}
