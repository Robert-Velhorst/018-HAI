package accountfeed

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSourceConfigurationDigestIncludesPrivateURLWithoutExposingIt(t *testing.T) {
	base := Feed{
		ID: uuid.MustParse("b672b128-4b5f-409b-885f-678fa3d8067c"), Name: "private source",
		Provider: "gmail", SourceType: SourceHTTPJSONFeed,
		OwnerUserID: "owner", WorkspaceID: "local", Enabled: true, ConfigVersion: 7,
		URL: "https://alice:first-password@example.com/feed?token=first-token#first-fragment",
	}
	variants := []string{
		"https://alice:second-password@example.com/feed?token=first-token#first-fragment",
		"https://alice:first-password@example.com/feed?token=second-token#first-fragment",
		"https://alice:first-password@example.com/feed?token=first-token#second-fragment",
	}
	publicBase, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	start := base.SourceObservationStart()
	for _, rawURL := range variants {
		candidate := base
		candidate.URL = rawURL
		publicCandidate, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if string(publicCandidate) != string(publicBase) {
			t.Fatal("fixture must have identical redacted public configurations")
		}
		changed := candidate.SourceObservationStart()
		if changed.ConfigDigest == start.ConfigDigest {
			t.Fatal("private configuration change did not invalidate its authority digest")
		}
		if changed.OriginID != base.ID || changed.ConfigVersion != 7 || !changed.RegistryManaged ||
			changed.OwnerUserID != base.OwnerUserID || changed.WorkspaceID != base.WorkspaceID {
			t.Fatal("private hashing changed canonical source identity or version")
		}
		publicStart, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(publicStart), "example.com") {
			t.Fatal("public observation exposed the provider endpoint")
		}
		for _, secret := range []string{"alice", "first-password", "second-password", "first-token", "second-token", "first-fragment", "second-fragment"} {
			if strings.Contains(string(publicStart), secret) || strings.Contains(string(publicCandidate), secret) {
				t.Fatal("public provenance or feed exposed private URL details")
			}
		}
		if candidate.URL != rawURL || candidate.SourceObservationStart().ConfigDigest != changed.ConfigDigest {
			t.Fatal("hashing or public marshaling mutated the private configuration")
		}
	}
	if base.SourceObservationStart() != start {
		t.Fatal("hashing is not deterministic")
	}
}
