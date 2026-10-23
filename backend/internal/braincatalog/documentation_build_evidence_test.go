package braincatalog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func screeningEvidenceRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate the screening evidence test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func readScreeningEvidence(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("could not read build evidence %s: %v", path, err)
	}
	return string(data)
}

func TestScreeningLedgerSeparatesHistoricalSnapshotAndBuildEvidence(t *testing.T) {
	root := screeningEvidenceRoot(t)
	ledger := readScreeningEvidence(t, root, "docs/ossinsight-screening-ledger.md")
	prose := strings.Join(strings.Fields(ledger), " ")
	for _, required := range []string{
		"## Historical snapshot and build evidence",
		"[docker-compose.local.yml](../docker-compose.local.yml)",
		"not a live installation",
	} {
		if !strings.Contains(prose, required) {
			t.Errorf("screening ledger does not expose its evidence boundary %q", required)
		}
	}
	if strings.Contains(ledger, "Pinned Postgres 17 pgvector image") {
		t.Error("the current pgvector row still claims the retired PostgreSQL 17 build target")
	}
}

func TestRunnerProfilePinEvidencePointsToTheBuiltManifests(t *testing.T) {
	root := screeningEvidenceRoot(t)
	ledger := readScreeningEvidence(t, root, "docs/ossinsight-screening-ledger.md")
	for _, profile := range []struct{ id, runner string }{
		{"pydantic-ai", "pydantic-ai-runner"},
		{"deepeval", "deepeval-runner"},
		{"deepteam", "deepteam-runner"},
	} {
		t.Run(profile.id, func(t *testing.T) {
			entry, ok := EntryByID(profile.id)
			if !ok || entry.Status != StatusIntegrated {
				t.Fatal("the reviewed profile or its integration boundary disappeared")
			}
			prefix := "services/" + profile.runner + "/"
			for _, manifest := range []string{"requirements.txt", "requirements.lock"} {
				path := prefix + manifest
				readScreeningEvidence(t, root, path)
				if !strings.Contains(entry.VerificationNote, path) {
					t.Errorf("profile note does not link its current build evidence: %s", path)
				}
				if !strings.Contains(ledger, "../"+path) {
					t.Errorf("screening ledger does not link build evidence: %s", path)
				}
			}
			dockerfile := readScreeningEvidence(t, root, prefix+"Dockerfile")
			if !strings.Contains(dockerfile, "COPY requirements.txt") || !strings.Contains(dockerfile, "-r requirements.txt") {
				t.Error("the documented Docker build no longer installs requirements.txt; review the evidence links")
			}
			if strings.Contains(entry.VerificationNote, "HAI pins ") {
				t.Error("a historical verification note still presents a copied version as the current HAI pin")
			}
		})
	}
}
