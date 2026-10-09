package brainskills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCatalogPinsReviewedSourcesWithoutBundling(t *testing.T) {
	if SourceCommit != "33375500bcea98d610eb30ce10ac4e59b89c390d" {
		t.Fatal("upstream commit changed")
	}
	if SourceCommitDate != "2026-09-24T16:20:35Z" {
		t.Fatal("upstream commit date changed")
	}
	want := map[string]struct{ path, sourceHash, licenseHash, guidanceHash string }{
		"frontend-design":   {"skills/frontend-design/SKILL.md", "d91970639e9f5c37682ac7ab60094d35f1c7c1f38d731bd56396563aee10c1d3", "0d542e0c8804e39aa7f37eb00da5a762149dc682d7829451287e11b938e94594", "42afa3c5e1d970c766dcb269485b891818bbb59da43e2f609476f88f923e626d"},
		"mcp-builder":       {"skills/mcp-builder/SKILL.md", "0f4592dcb53cf2b5d6b7febee6b4152018b565551a1c29e3c612f57b218ab295", "bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362", "f489360e858ac5cad27561867de6639a43bd2ebd529aa8db01618df020116ce0"},
		"webapp-testing":    {"skills/webapp-testing/SKILL.md", "51b7349e77ec63b7744a6f63647e7566a0b4d2e301121cc10e8c2113af6556a2", "bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362", "5ea4cccd94c47ed4ae95d6597fcc17d3f7c4eb7f1de73432777b28f65c18fb96"},
		"discernment-nudge": {"skills/discernment-nudge/SKILL.md", "9191177c4a8ef11a20dace786d708506b22d43e748c71287bb823de0dc812dad", "bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362", "b2bac32da6aa70d6685f3a9cbd637d2f2b549c868e25d284cdd69b809ee4ce07"},
		"skill-creator":     {"skills/skill-creator/SKILL.md", "dcd4803e61e913e6fc27294184cd3a71f09f5e924ff20c8a9a20173e7b3c2bcf", "bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362", "ec5ab96166c9799d4d7c16cea2ca5a7a3136e7d83f4e1d35e8601bea0709dca4"},
		"internal-comms":    {"skills/internal-comms/SKILL.md", "067b7587a344a928fc6534ef66b1bcd591fc7c26d207ea7ca3334aeb678d6475", "bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362", "3681c3a3c4ff95deb57ea03dad5b0a568e851a358077970d5a757c743047ef83"},
	}
	listed := DefaultCatalog().List()
	if !reflect.DeepEqual(listed, List()) || len(listed) != len(want) {
		t.Fatalf("catalog mismatch or unexpected size: %d", len(listed))
	}
	for _, skill := range listed {
		pinned, ok := want[skill.ID]
		if !ok || skill.SourcePath != pinned.path || skill.SourceSHA256 != pinned.sourceHash ||
			skill.LicenseSHA256 != pinned.licenseHash || skill.GuidanceSHA256 != pinned.guidanceHash {
			t.Fatalf("unexpected source pin for %q", skill.ID)
		}
		if skill.Repository != RepositoryURL || skill.Commit != SourceCommit || skill.SourceURL != RepositoryURL+"/blob/"+SourceCommit+"/"+pinned.path {
			t.Fatalf("invalid provenance for %q", skill.ID)
		}
		licensePath := "skills/" + skill.ID + "/LICENSE.txt"
		if skill.LicensePath != licensePath || skill.LicenseURL != RepositoryURL+"/blob/"+SourceCommit+"/"+licensePath {
			t.Fatalf("invalid pinned license provenance for %q: path=%q URL=%q", skill.ID, skill.LicensePath, skill.LicenseURL)
		}
		if skill.License != "Apache-2.0" || skill.Status != "adapted" || skill.Scope != "prompt-guidance-only" || skill.Bundled {
			t.Fatalf("unreviewed license, scope, or bundled source for %q", skill.ID)
		}
		guidance, ok := GuidanceFor(skill.ID)
		if !ok || skill.Boundary == "" || len(guidance) == 0 || len(guidance) > MaxGuidanceSummaryBytes {
			t.Fatalf("missing boundary or oversized guidance for %q", skill.ID)
		}
		digest := sha256.Sum256([]byte(guidance))
		if skill.GuidanceSHA256 != hex.EncodeToString(digest[:]) || skill.GuidanceSHA256 == skill.SourceSHA256 {
			t.Fatalf("guidance digest not distinguished from source for %q", skill.ID)
		}
		encoded, err := json.Marshal(skill)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{`"sourceSHA256"`, `"guidanceSHA256"`, `"license"`, `"licensePath"`, `"licenseURL"`, `"licenseSHA256"`, `"status"`, `"scope"`} {
			if !strings.Contains(string(encoded), key) {
				t.Fatalf("missing API field %s for %q", key, skill.ID)
			}
		}
		if strings.Contains(string(encoded), guidance) || strings.Contains(string(encoded), `"selectedCount"`) {
			t.Fatalf("metadata includes guidance body or a fictive selection count for %q", skill.ID)
		}
		delete(want, skill.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing entries: %v", want)
	}
	for _, excludedID := range []string{"doc-coauthoring", "docx", "pdf", "pptx", "xlsx", "academy-guide", "claude-api"} {
		for _, skill := range listed {
			if skill.ID == excludedID {
				t.Fatalf("unreviewed or excluded skill %q entered the catalog", excludedID)
			}
		}
	}
}

func TestCatalogFingerprintIsStableAndTracksSourcePinsWithUnchangedGuidance(t *testing.T) {
	catalog := DefaultCatalog()
	want := catalog.Fingerprint()
	if want != catalog.Fingerprint() || !isSHA256Hash(want) {
		t.Fatalf("catalog fingerprint is unstable or malformed: %q", want)
	}

	reordered := append([]catalogEntry(nil), catalog.entries...)
	for left, right := 0, len(reordered)-1; left < right; left, right = left+1, right-1 {
		reordered[left], reordered[right] = reordered[right], reordered[left]
	}
	if got := (Catalog{entries: reordered}).Fingerprint(); got != want {
		t.Fatalf("catalog ordering changed the canonical fingerprint: got %q want %q", got, want)
	}

	changedPin := append([]catalogEntry(nil), catalog.entries...)
	changedPin[0].skill.SourceSHA256 = strings.Repeat("f", 64)
	changed := Catalog{entries: changedPin}
	oldSkill := catalog.List()[0]
	newSkill := changed.List()[0]
	oldGuidance, _ := catalog.GuidanceFor(oldSkill.ID)
	newGuidance, _ := changed.GuidanceFor(newSkill.ID)
	if oldGuidance != newGuidance || oldSkill.GuidanceSHA256 != newSkill.GuidanceSHA256 {
		t.Fatal("test must retain identical HAI guidance while changing only source identity")
	}
	if got := changed.Fingerprint(); got == want {
		t.Fatalf("source pin change with unchanged guidance retained fingerprint %q", got)
	}

	changedLicense := append([]catalogEntry(nil), catalog.entries...)
	changedLicense[0].skill.LicenseSHA256 = strings.Repeat("e", 64)
	licenseOnlyChange := Catalog{entries: changedLicense}
	if got := licenseOnlyChange.Fingerprint(); got == want {
		t.Fatalf("license pin change with unchanged source and guidance retained fingerprint %q", got)
	}
}

func TestPackageContainsNoUpstreamAssetsOrExecutionPath(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") {
			t.Fatalf("non-Go asset in metadata-only catalog: %s", file.Name())
		}
		if strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"os/exec"`) || strings.Contains(string(body), "//go:embed") {
			t.Fatalf("execution or embedded asset in %s", file.Name())
		}
	}
}

func TestMatchIsDeterministicAndScoped(t *testing.T) {
	cases := []struct {
		taskType, request string
		ids               []string
	}{
		{"frontend", "Redesign the dashboard", []string{"frontend-design"}},
		{"integration", "Build an MCP connector and verify it with Playwright", []string{"mcp-builder", "webapp-testing"}},
		{"frontend", "Maak het dashboard overzichtelijk en mobiel bruikbaar", []string{"frontend-design"}},
		{"integration", "Bouw een MCP-koppeling en test de webapp", []string{"mcp-builder", "webapp-testing"}},
		{"skill-authoring", "Create a bounded HAI skill and test it against edge cases", []string{"skill-creator"}},
		{"capability design", "Improve the skill catalog and evaluate triggering", []string{"skill-creator"}},
		{"skill-authoring", "Do not create a skill; implement this code instead", nil},
		{"skill-authoring", "Do not create a skill, but evaluate the HAI skill catalog", []string{"skill-creator"}},
		{"email", "Draft a concise reply to my lawyer", []string{"internal-comms"}},
		{"communication", "Stel een statusrapport op voor het projectteam", []string{"internal-comms"}},
		{"email", "Do not draft a reply; summarize the existing message only", nil},
		{"communication", "Do not draft an email, but write a text message", []string{"internal-comms"}},
		{"recommendation", "What do you recommend before I accept this settlement offer?", []string{"discernment-nudge"}},
		{"planning", "Give me a realistic estimate and plan to reduce debt without losing my emergency savings", []string{"discernment-nudge"}},
		{"decision-support", "Welke keuze is verstandig voor mijn loopbaan en financiële situatie?", []string{"discernment-nudge"}},
		{"", "Wat past bij mijn financiële situatie?", []string{"discernment-nudge"}},
		{"decision-support", "Implement a code change to estimate retry costs", nil},
		{"decision-support", "Explain how financial planning works", nil},
		{"decision-support", "Review this recommendation and verify the claims", nil},
		{"", "Implement code; actually, should I keep the emergency fund?", []string{"discernment-nudge"}},
		{"code", "Implement a code change to estimate retry costs", nil},
		{"review", "Review this recommendation and verify the claims", nil},
		{"answer", "Quick lookup: what is a mortgage?", nil},
		{"education", "Explain the history of financial planning", nil},
		{"summary", "Summarize this legal letter", nil},
		{"decision-support", "No advice; summarize the legal letter only", nil},
		{"", "I have no plan; what would you recommend for my career?", []string{"discernment-nudge"}},
		{"", "I might recommend a plan, but no advice; what legal risks exist?", nil},
		{"decision-support", "No advice, but what would you recommend?", []string{"discernment-nudge"}},
		{"decision-support", "No advice, but what legal risks exist?", nil},
		{"mcp", "Do not use MCP for this integration", nil},
		{"mcp", "Don't use MCP for this integration", nil},
		{"mcp", "MCP is not needed for this task", nil},
		{"integration", "MCP is not only relevant but central to this integration", []string{"mcp-builder"}},
		{"mcp", "Zonder MCP, bouw een andere koppeling", nil},
		{"testing", "Do not use Playwright; test the website manually", []string{"webapp-testing"}},
		{"frontend", "Do not redesign the dashboard; focus on backend only", nil},
		{"frontend", "Do not redesign the UI; only update the dashboard data endpoint", nil},
		{"frontend", "Do not redesign the UI, but do update the dashboard layout", []string{"frontend-design"}},
		{"mcp", "No MCP, but review MCP documentation", []string{"mcp-builder"}},
		{"mcp", "No MCP; review MCP documentation", nil},
		{"testing", "Do not test the web app; only update its backend endpoint", nil},
		{"insurance", "Prepare an evidence bundle", nil},
		{"document", "Review a web document", nil},
		{"", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.taskType+"/"+tc.request, func(t *testing.T) {
			got := Match(tc.taskType, tc.request)
			ids := make([]string, 0, len(got))
			for _, skill := range got {
				ids = append(ids, skill.ID)
			}
			if len(ids) == 0 {
				ids = nil
			}
			if !reflect.DeepEqual(ids, tc.ids) || !reflect.DeepEqual(got, Match(tc.taskType, tc.request)) || !reflect.DeepEqual(got, DefaultCatalog().Match(tc.taskType, tc.request)) {
				t.Fatalf("Match(%q, %q) = %v, want %v", tc.taskType, tc.request, ids, tc.ids)
			}
		})
	}
}

func TestMatchRecognizesExpandedModelContextProtocolName(t *testing.T) {
	tests := []struct {
		name, taskType, request string
		want                    []string
	}{
		{name: "expanded name", request: "Build a Model Context Protocol integration", want: []string{"mcp-builder"}},
		{name: "hyphenated expanded name", request: "Build a model-context-protocol server", want: []string{"mcp-builder"}},
		{name: "existing abbreviation", request: "Build an MCP server", want: []string{"mcp-builder"}},
		{name: "explicit exclusion", request: "Do not use the Model Context Protocol for this integration"},
		{name: "later correction", request: "Skip the Model Context Protocol; actually, build a Model Context Protocol server", want: []string{"mcp-builder"}},
		{name: "request exclusion overrides task type", taskType: "Model Context Protocol integration", request: "Do not use the Model Context Protocol"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Match(test.taskType, test.request)
			ids := make([]string, 0, len(got))
			for _, skill := range got {
				ids = append(ids, skill.ID)
			}
			if len(ids) == 0 {
				ids = nil
			}
			if !reflect.DeepEqual(ids, test.want) {
				t.Fatalf("Match(%q, %q) = %v, want %v", test.taskType, test.request, ids, test.want)
			}
		})
	}
}

func TestDecisionCalibrationGuidanceIsHAIAuthoredAndProportionate(t *testing.T) {
	guidance, ok := GuidanceFor("discernment-nudge")
	if !ok || guidance == "" {
		t.Fatal("decision-calibration guidance is missing")
	}
	for _, required := range []string{"verified facts", "assumptions", "one way to check it", "avoid generic questions", "explicit review, citation, or verification requests", "approval gates"} {
		if !strings.Contains(guidance, required) {
			t.Errorf("decision-calibration guidance is missing boundary %q", required)
		}
	}
	for _, forbidden := range []string{"append 2-3", "invoke this skill", "run a script", "allowed-tools", "call a tool"} {
		if strings.Contains(strings.ToLower(guidance), forbidden) {
			t.Errorf("guidance contains upstream/execution behavior %q", forbidden)
		}
	}
}

func TestAddedGuidanceRemainsBoundedAndAdvisory(t *testing.T) {
	tests := []struct {
		id        string
		required  []string
		forbidden []string
	}{
		{
			id:        "skill-creator",
			required:  []string{"proposal, not permission", "in-scope, out-of-scope", "owner review", "Never execute upstream scripts", "grant tools"},
			forbidden: []string{"eval-viewer/", "subagent", "skill-workspace/", "allowed-tools"},
		},
		{
			id:        "internal-comms",
			required:  []string{"source-backed facts", "reviewable draft only", "do not send", "authorization"},
			forbidden: []string{"examples/", "company-wide", "send automatically", "allowed-tools"},
		},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			guidance, ok := GuidanceFor(test.id)
			if !ok || guidance == "" || len(guidance) > MaxGuidanceSummaryBytes {
				t.Fatalf("guidance missing or exceeds %d bytes", MaxGuidanceSummaryBytes)
			}
			for _, phrase := range test.required {
				if !strings.Contains(guidance, phrase) {
					t.Errorf("guidance is missing safety boundary %q", phrase)
				}
			}
			for _, phrase := range test.forbidden {
				if strings.Contains(strings.ToLower(guidance), strings.ToLower(phrase)) {
					t.Errorf("guidance includes upstream procedure or authority %q", phrase)
				}
			}
		})
	}
}

func TestMatchRejectsOversizedAndInvalidUTF8Input(t *testing.T) {
	tests := []struct {
		name, taskType, request string
	}{
		{name: "task type too large", taskType: strings.Repeat("x", MaxMatchTaskTypeBytes+1), request: "frontend"},
		{name: "request too large", taskType: "frontend", request: strings.Repeat("x", MaxMatchRequestBytes+1)},
		{name: "invalid request utf8", taskType: "frontend", request: string([]byte{0xff})},
		{name: "invalid task type utf8", taskType: string([]byte{0xff}), request: "dashboard"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateMatchInput(test.taskType, test.request); !errors.Is(err, ErrInvalidMatchInput) {
				t.Fatalf("ValidateMatchInput error = %v", err)
			}
			if got := DefaultCatalog().Match(test.taskType, test.request); len(got) != 0 {
				t.Fatalf("invalid input selected skills: %#v", got)
			}
		})
	}
	if err := ValidateMatchInput("frontend", "Redesign dashboard"); err != nil {
		t.Fatalf("normal task match input was rejected: %v", err)
	}
}

func TestMatchBoundsContrastSuffixWork(t *testing.T) {
	withinLimit := strings.TrimSpace(strings.Repeat("but ", MaxMatchContrastBoundaries) + "MCP")
	if err := ValidateMatchInput("", withinLimit); err != nil {
		t.Fatalf("request at the contrast limit was rejected: %v", err)
	}
	if got := Match("", withinLimit); len(got) != 1 || got[0].ID != "mcp-builder" {
		t.Fatalf("request at the contrast limit matched %#v, want MCP guidance", got)
	}

	overLimit := strings.TrimSpace(strings.Repeat("but ", MaxMatchContrastBoundaries+1) + "MCP")
	if err := ValidateMatchInput("", overLimit); !errors.Is(err, ErrInvalidMatchInput) {
		t.Fatalf("request over the contrast limit error = %v, want %v", err, ErrInvalidMatchInput)
	}
	if got := Match("", overLimit); len(got) != 0 {
		t.Fatalf("request over the contrast limit selected skills: %#v", got)
	}
}

func TestGuidanceForExactIDAndListIsolation(t *testing.T) {
	if _, ok := GuidanceFor("../skills/frontend-design"); ok {
		t.Fatal("accepted non-catalog ID")
	}
	guidance, ok := DefaultCatalog().GuidanceFor("frontend-design")
	if !ok || guidance == "" {
		t.Fatal("missing HAI-adapted guidance")
	}
	listed := List()
	listed[0].Name = "changed"
	if DefaultCatalog().List()[0].Name == "changed" {
		t.Fatal("caller mutated catalog")
	}
}
