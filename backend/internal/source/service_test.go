package source

import (
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/semantic"
	"automation-hub-backend/internal/workflow"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSyncLocalFolderExtractsReadableFilesWithProvenance(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root+"/project-note.md", "Decision: local folder ingestion should extract useful project context. Follow up: verify provenance before task planning.")
	writeTestFile(t, root+"/binary.bin", "\x00\x01ignored")
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:              sourceID,
		ConnectorKey:    "local-folder",
		Name:            "Local project folder",
		Category:        "local_folder",
		Enabled:         true,
		LocalOnly:       true,
		Status:          "active",
		ExcludePatterns: "ignored",
	})
	mem := &fakeSourceMemoryService{}
	service := NewService(repo, mem)

	result, err := service.Sync(sourceID, ImportRequest{
		Mode:       ModeHistoricalBackfill,
		FolderPath: ".",
		ProjectKey: "018-HAI",
		Limit:      10,
		MaxBytes:   4096,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if result.Job.ItemsSeen != 1 {
		t.Fatalf("ItemsSeen = %d, want 1", result.Job.ItemsSeen)
	}
	if result.Job.ItemsAdded != 1 {
		t.Fatalf("ItemsAdded = %d, want 1", result.Job.ItemsAdded)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	extraction := result.Extractions[0]
	if extraction.ProjectKey != "018-HAI" {
		t.Fatalf("ProjectKey = %q, want 018-HAI", extraction.ProjectKey)
	}
	if extraction.SourceLabel != "project-note.md" {
		t.Fatalf("SourceLabel = %q, want project-note.md", extraction.SourceLabel)
	}
	if !strings.HasPrefix(extraction.SourceURI, "file://") {
		t.Fatalf("SourceURI = %q, want file URI", extraction.SourceURI)
	}
	if !strings.Contains(extraction.Tasks, "Follow up") {
		t.Fatalf("Tasks = %q, want extracted follow up/task", extraction.Tasks)
	}
	if len(mem.created) != 1 {
		t.Fatalf("created memories = %d, want 1", len(mem.created))
	}
	if !repo.hasAudit("source.local_folder_scanned") || !repo.hasAudit("source.synced") {
		t.Fatalf("expected scan and sync audit records")
	}
}

func TestSyncWhisperAudioRequiresControlledTranscriptionRoute(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "whisper-audio",
		Name:         "Owner voice notes",
		Category:     "audio",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
		SyncTarget:   "voice-notes",
	})

	_, err := NewService(repo, nil).Sync(sourceID, ImportRequest{Mode: ModeManualImport, Items: []ImportItem{{ExternalID: "forged", Title: "Forged transcript", Content: "This must not be accepted."}}})
	if err == nil || !strings.Contains(err.Error(), "controlled transcription route") {
		t.Fatalf("Sync error = %v, want controlled transcription route error", err)
	}
	if due, reason := scheduledSourceDue(*repo.sources[sourceID], time.Now().UTC()); due || !strings.Contains(reason, "operator-triggered") {
		t.Fatalf("scheduledSourceDue = %v, %q; whisper audio must never be scheduled", due, reason)
	}
}

func TestSyncCloudQuerySummaryReadsOnlyBoundedIncrementalSummaries(t *testing.T) {
	root := t.TempDir()
	summaryPath := root + "/summary.jsonl"
	writeTestFile(t, summaryPath, `{"sync_id":"sync-1","sync_time":"2026-07-20T10:00:00Z","sync_duration_ms":123,"resources":4,"sources":[{"name":"aws","errors":[]}],"destinations":[{"name":"postgres","tables":[{"name":"aws_ec2_instances","resources":4}]}],"api_key":"must-not-be-ingested"}`+"\n")
	t.Setenv("HAI_CLOUDQUERY_SUMMARY_ENABLED", "true")
	t.Setenv("HAI_CLOUDQUERY_ALLOWED_ROOT", root)
	t.Setenv("HAI_CLOUDQUERY_SUMMARY_PATH", summaryPath)
	t.Setenv("HAI_CLOUDQUERY_MAX_ENTRIES", "10")

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:                sourceID,
		ConnectorKey:      cloudQuerySummaryConnectorKey,
		Name:              "CloudQuery local summary",
		Category:          "cloud_inventory",
		Enabled:           true,
		LocalOnly:         true,
		Status:            "active",
		DefaultProjectKey: "018-HAI",
	})
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeManualImport})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 1 || result.Job.ItemsAdded != 1 || !strings.HasPrefix(result.Job.CursorAfter, cloudQuerySummaryCursorPrefix) {
		t.Fatalf("unexpected CloudQuery sync result: %#v", result.Job)
	}
	if len(result.Extractions) != 1 || result.Extractions[0].ContentType != "cloudquery_sync_summary" {
		t.Fatalf("unexpected CloudQuery extraction: %#v", result.Extractions)
	}
	if strings.Contains(result.Extractions[0].Text, "must-not-be-ingested") {
		t.Fatalf("unexpected unknown CloudQuery JSON field leaked into extraction: %q", result.Extractions[0].Text)
	}
	if !repo.hasAudit("source.cloudquery_summary_read") || !repo.hasAudit("source.synced") {
		t.Fatalf("expected CloudQuery summary and completed sync audits")
	}

	file, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := file.WriteString(`{"sync_id":"sync-2","resources":2,"sources":[{"name":"github"}],"destinations":[{"name":"postgres"}]}` + "\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	_ = file.Close()
	result, err = NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("incremental Sync: %v", err)
	}
	if result.Job.ItemsSeen != 1 || result.Job.ItemsAdded != 1 {
		t.Fatalf("incremental sync reread prior records: %#v", result.Job)
	}
}

func TestCloudQuerySummaryConfigurationStaysInsideConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() + "/summary.jsonl"
	writeTestFile(t, outside, "{}\n")
	t.Setenv("HAI_CLOUDQUERY_SUMMARY_ENABLED", "true")
	t.Setenv("HAI_CLOUDQUERY_ALLOWED_ROOT", root)
	t.Setenv("HAI_CLOUDQUERY_SUMMARY_PATH", outside)
	if _, err := cloudQuerySummaryConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "remain inside") {
		t.Fatalf("cloudQuerySummaryConfigFromEnv error = %v, want allowed-root rejection", err)
	}
}

func TestSyncCloudQuerySummaryRejectsLegacyOrManualBypasses(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: cloudQuerySummaryConnectorKey,
		Name:         "Invalid legacy CloudQuery source",
		Category:     "cloud_inventory",
		Enabled:      true,
		LocalOnly:    false,
		Status:       "active",
	})
	if _, err := NewService(repo, nil).Sync(sourceID, ImportRequest{}); err == nil || !strings.Contains(err.Error(), "must remain local-only") {
		t.Fatalf("legacy CloudQuery source error = %v, want local-only rejection", err)
	}
	repo.sources[sourceID].LocalOnly = true
	if _, err := NewService(repo, nil).Sync(sourceID, ImportRequest{Items: []ImportItem{{ExternalID: "forged", Title: "Forged", Content: "must not enter CloudQuery source"}}}); err == nil || !strings.Contains(err.Error(), "manual items") {
		t.Fatalf("manual CloudQuery item error = %v, want manual-item rejection", err)
	}
}

func TestCloudQuerySummaryRejectsIncompleteAndMalformedRecords(t *testing.T) {
	root := t.TempDir()
	summaryPath := root + "/summary.jsonl"
	writeTestFile(t, summaryPath, `{"sync_id":"complete"}`+"\n"+`{"sync_id":"partial"`)
	t.Setenv("HAI_CLOUDQUERY_SUMMARY_ENABLED", "true")
	t.Setenv("HAI_CLOUDQUERY_ALLOWED_ROOT", root)
	t.Setenv("HAI_CLOUDQUERY_SUMMARY_PATH", summaryPath)
	items, cursor, err := fetchCloudQuerySummary(&models.ConnectedSource{})
	if err != nil || len(items) != 1 || !strings.HasPrefix(cursor, cloudQuerySummaryCursorPrefix) {
		t.Fatalf("incomplete final record must be deferred: items=%#v cursor=%q err=%v", items, cursor, err)
	}
	writeTestFile(t, summaryPath, "not-json\n")
	if _, _, err := fetchCloudQuerySummary(&models.ConnectedSource{}); err == nil || !strings.Contains(err.Error(), "invalid JSONL") {
		t.Fatalf("malformed complete record error = %v, want invalid JSONL", err)
	}
	writeTestFile(t, summaryPath, strings.Repeat("x", cloudQuerySummaryMaxLineBytes+1)+"\n")
	if _, _, err := fetchCloudQuerySummary(&models.ConnectedSource{}); err == nil || !strings.Contains(err.Error(), "16 KiB") {
		t.Fatalf("oversized summary record error = %v, want line-size rejection", err)
	}
}

func TestSyncOpenSpecArtifactsReadsOnlyChangePlanningFiles(t *testing.T) {
	root := t.TempDir()
	project := root + "/project"
	change := project + "/openspec/changes/add-local-routing"
	if err := os.MkdirAll(change+"/specs", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeTestFile(t, change+"/proposal.md", "# Proposal\nThe router must select local models first.\n")
	writeTestFile(t, change+"/design.md", "# Design\nUse an allowlisted local endpoint.\n")
	writeTestFile(t, change+"/tasks.md", "# Tasks\n- [ ] Add the local routing policy.\n")
	writeTestFile(t, change+"/specs/routing.md", "## ADDED Requirements\n### Requirement: Local routing\nThe system SHALL prefer local models.\n")
	writeTestFile(t, project+"/main.go", "package ignored\n// code outside OpenSpec must not be read\n")
	if err := os.MkdirAll(project+"/openspec/changes/archive/old-change", 0755); err != nil {
		t.Fatalf("MkdirAll archive: %v", err)
	}
	writeTestFile(t, project+"/openspec/changes/archive/old-change/proposal.md", "Archived change must not be imported.")
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:                sourceID,
		ConnectorKey:      openSpecArtifactConnectorKey,
		Name:              "Project OpenSpec",
		Category:          "code_spec",
		Enabled:           true,
		LocalOnly:         true,
		Status:            "active",
		SyncTarget:        "project",
		DefaultProjectKey: "018-HAI",
	})
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeManualImport})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 1 || result.Job.ItemsAdded != 1 || len(result.Extractions) != 1 {
		t.Fatalf("unexpected OpenSpec sync result: %#v", result)
	}
	extraction := result.Extractions[0]
	if extraction.ContentType != "openspec_change" || extraction.ProjectKey != "018-HAI" || !strings.Contains(extraction.Text, "ADDED Requirements") {
		t.Fatalf("OpenSpec extraction = %#v", extraction)
	}
	if strings.Contains(extraction.Text, "code outside OpenSpec") || strings.Contains(extraction.Text, "Archived change") {
		t.Fatalf("OpenSpec connector read out-of-scope files: %q", extraction.Text)
	}
	if !repo.hasAudit("source.openspec_artifacts_read") || !repo.hasAudit("source.synced") {
		t.Fatalf("expected OpenSpec source audits")
	}
}

func TestSyncOpenSpecArtifactsRejectsManualAndFolderOverride(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: openSpecArtifactConnectorKey,
		Name:         "OpenSpec",
		Category:     "code_spec",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
		SyncTarget:   "approved-project",
	})
	service := NewService(repo, nil)
	if _, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{ExternalID: "forged", Content: "forged planning artifact"}}}); err == nil || !strings.Contains(err.Error(), "manual items") {
		t.Fatalf("manual OpenSpec import error = %v, want rejection", err)
	}
	if _, err := service.Sync(sourceID, ImportRequest{FolderPath: "another-project"}); err == nil || !strings.Contains(err.Error(), "registered project folder") {
		t.Fatalf("OpenSpec folder override error = %v, want rejection", err)
	}
}

func TestSyncWhatsAppExportParsesChatWindowsAndGatesReview(t *testing.T) {
	root := t.TempDir()
	export := strings.Join([]string{
		"31/05/2026, 09:10 - Robert Velhorst: Kun jij morgen de offerte opvolgen?",
		"31/05/2026, 09:11 - Joyce: Ja, ik moet eerst de documenten controleren.",
		"31/05/2026, 09:12 - Robert Velhorst: Afgesproken, wacht op bevestiging en herinner mij vrijdag.",
	}, "\n")
	writeTestFile(t, root+"/WhatsApp Chat with Joyce.txt", export)
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:                sourceID,
		ConnectorKey:      "whatsapp-export",
		Name:              "WhatsApp Joyce export",
		Category:          "chat",
		Enabled:           true,
		LocalOnly:         true,
		Status:            "active",
		DefaultProjectKey: "Robert-life-os",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)

	result, err := service.Sync(sourceID, ImportRequest{
		Mode:       ModeHistoricalBackfill,
		FolderPath: ".",
		Limit:      20,
		MaxBytes:   4096,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 1 {
		t.Fatalf("ItemsSeen = %d, want 1 parsed chat window", result.Job.ItemsSeen)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	extraction := result.Extractions[0]
	if extraction.ContentType != "whatsapp_chat_window" {
		t.Fatalf("ContentType = %q, want whatsapp_chat_window", extraction.ContentType)
	}
	if !extraction.Sensitive {
		t.Fatalf("WhatsApp extraction was not marked sensitive")
	}
	if !strings.Contains(extraction.Tasks, "moet") || !strings.Contains(extraction.Decisions, "Afgesproken") || !strings.Contains(extraction.FollowUps, "wacht op") {
		t.Fatalf("extraction missed Dutch operational signals: tasks=%q decisions=%q followUps=%q", extraction.Tasks, extraction.Decisions, extraction.FollowUps)
	}
	if len(workflowSpy.requests) != 1 {
		t.Fatalf("workflow requests = %d, want 1", len(workflowSpy.requests))
	}
	if !workflowSpy.requests[0].RequiresReview || !strings.Contains(workflowSpy.requests[0].ReviewReason, "sensitive") {
		t.Fatalf("workflow was not review gated: %#v", workflowSpy.requests[0])
	}
	if !repo.hasAudit("source.whatsapp_export_scanned") || !repo.hasAudit("workflow.intake_created") {
		t.Fatalf("expected WhatsApp scan and workflow intake audit records")
	}
}

func TestSyncWhatsAppManualPasteCreatesBoundedWindows(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "whatsapp-export",
		Name:         "WhatsApp manual paste",
		Category:     "chat",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	service := NewService(repo, &fakeSourceMemoryService{})

	result, err := service.Sync(sourceID, ImportRequest{
		Mode:       ModeManualImport,
		ProjectKey: "018-HAI",
		Limit:      2,
		Items: []ImportItem{{
			ExternalID: "chat-robert-test",
			Title:      "WhatsApp test chat",
			SourceURI:  "whatsapp-export://manual/test",
			Content: strings.Join([]string{
				"01/06/2026, 10:00 - Robert: Eerste bericht.",
				"01/06/2026, 10:01 - Contact: Tweede bericht moet worden opgevolgd.",
				"01/06/2026, 10:02 - Robert: Derde bericht.",
			}, "\n"),
		}},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 2 {
		t.Fatalf("ItemsSeen = %d, want 2 bounded windows", result.Job.ItemsSeen)
	}
	if result.Extractions[0].SourceLabel == result.Extractions[1].SourceLabel {
		t.Fatalf("expected distinct source labels per window")
	}
}

func TestSyncLocalFolderBlocksTraversalOutsideAllowlistedRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "local-folder",
		Name:         "Local project folder",
		Category:     "local_folder",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	service := NewService(repo, &fakeSourceMemoryService{})

	result, err := service.Sync(sourceID, ImportRequest{
		Mode:       ModeIncrementalSync,
		FolderPath: "..",
	})
	if err == nil {
		t.Fatalf("expected traversal error")
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if len(repo.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1 failed job", len(repo.jobs))
	}
	if repo.jobs[0].Status != "failed" {
		t.Fatalf("job status = %q, want failed", repo.jobs[0].Status)
	}
	if !repo.hasAudit("source.sync_failed") {
		t.Fatalf("expected failed sync audit record")
	}
}

func TestSyncLocalFolderSkipsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outsideRoot := t.TempDir()
	writeTestFile(t, outsideRoot+"/secret.md", "Decision: this outside file must not be ingested.")
	if err := os.Symlink(outsideRoot+"/secret.md", root+"/secret-link.md"); err != nil {
		t.Skipf("symlink not available on this platform: %v", err)
	}
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "local-folder",
		Name:         "Local project folder",
		Category:     "local_folder",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	service := NewService(repo, &fakeSourceMemoryService{})

	result, err := service.Sync(sourceID, ImportRequest{
		Mode:       ModeHistoricalBackfill,
		FolderPath: ".",
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 0 {
		t.Fatalf("ItemsSeen = %d, want symlink skipped", result.Job.ItemsSeen)
	}
	if !repo.hasAudit("source.local_folder_symlink_skipped") {
		t.Fatalf("expected symlink skip audit record")
	}
}

func TestRunDueScheduledSyncsRunsDueLocalFolderSource(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root+"/scheduled.md", "Decision: scheduled source sync should run without a dashboard click. Follow up: keep the folder allowlist enforced.")
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:                sourceID,
		ConnectorKey:      "local-folder",
		Name:              "Scheduled local folder",
		Category:          "local_folder",
		Enabled:           true,
		LocalOnly:         true,
		Status:            "active",
		SyncFrequency:     "1m",
		SyncTarget:        ".",
		DefaultProjectKey: "018-HAI",
	})
	service := NewService(repo, &fakeSourceMemoryService{})

	run, err := service.RunDueScheduledSyncs(time.Now().UTC())
	if err != nil {
		t.Fatalf("RunDueScheduledSyncs: %v", err)
	}
	if run.Checked != 1 || run.Due != 1 || run.Completed != 1 || run.Failed != 0 {
		t.Fatalf("run = %#v, want one completed due sync", run)
	}
	if len(repo.extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(repo.extractions))
	}
	updated, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if updated.LastSyncedAt == nil {
		t.Fatalf("expected LastSyncedAt to be updated")
	}
	if !repo.hasAudit("source.synced") {
		t.Fatalf("expected scheduled sync audit record")
	}
}

func TestRunDueScheduledSyncsForOwnerDoesNotTouchAnotherOwnersSources(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/alice", 0o755); err != nil {
		t.Fatalf("create Alice fixture directory: %v", err)
	}
	if err := os.MkdirAll(root+"/bob", 0o755); err != nil {
		t.Fatalf("create Bob fixture directory: %v", err)
	}
	writeTestFile(t, root+"/alice/brief.md", "Follow up: Alice source should be refreshed only for Alice.")
	writeTestFile(t, root+"/bob/brief.md", "Follow up: Bob source must not be refreshed by Alice's task.")
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	aliceID := uuid.New()
	bobID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{
			ID: aliceID, OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "Alice folder", Category: "local_folder",
			Enabled: true, LocalOnly: true, Status: "active", SyncFrequency: "1m", SyncTarget: "alice",
		},
		&models.ConnectedSource{
			ID: bobID, OwnerIdentity: "bob", ConnectorKey: "local-folder", Name: "Bob folder", Category: "local_folder",
			Enabled: true, LocalOnly: true, Status: "active", SyncFrequency: "1m", SyncTarget: "bob",
		},
	)
	service := NewService(repo, &fakeSourceMemoryService{})

	run, err := service.RunDueScheduledSyncsForOwner(time.Now().UTC(), "alice")
	if err != nil {
		t.Fatalf("RunDueScheduledSyncsForOwner: %v", err)
	}
	if run.Checked != 1 || run.Due != 1 || run.Completed != 1 || run.Failed != 0 {
		t.Fatalf("owner run = %#v, want Alice-only successful sync", run)
	}
	alice, _ := repo.FindSource(aliceID)
	bob, _ := repo.FindSource(bobID)
	if alice.LastSyncedAt == nil {
		t.Fatal("Alice source was not refreshed")
	}
	if bob.LastSyncedAt != nil {
		t.Fatal("Alice task refreshed Bob's source")
	}
	if repo.globalSourceReads != 0 || repo.ownerSourceReads != 1 || repo.lastSourceOwner != "alice" {
		t.Fatalf("owner sync source queries = global %d / owner %d (%q), want one database-scoped Alice query", repo.globalSourceReads, repo.ownerSourceReads, repo.lastSourceOwner)
	}
	if _, err := service.RunDueScheduledSyncsForOwner(time.Now().UTC(), " "); err == nil {
		t.Fatal("owner-scoped scheduled sync accepted an empty owner")
	}
}

func TestRunDueScheduledSyncsForOwnerContextStopsBeforeRepositoryWorkWhenCanceled(t *testing.T) {
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "Alice folder",
		Enabled: true, LocalOnly: true, Status: "active", SyncFrequency: "1m", SyncTarget: ".",
	})
	service := NewService(repo, &fakeSourceMemoryService{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run, err := RunDueScheduledSyncsForOwnerWithContext(service, ctx, time.Now().UTC(), "alice")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDueScheduledSyncsForOwnerContext error = %v, want context canceled", err)
	}
	if run != nil {
		t.Fatalf("run = %#v, want no batch after pre-cancellation", run)
	}
	if repo.ownerSourceReads != 0 || len(repo.jobs) != 0 {
		t.Fatalf("pre-canceled batch performed repository work: owner reads=%d jobs=%d", repo.ownerSourceReads, len(repo.jobs))
	}
}

func TestRunDueScheduledSyncsSkipsManualAndNotDueSources(t *testing.T) {
	lastSync := time.Now().UTC()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{
			ID:            uuid.New(),
			ConnectorKey:  "local-folder",
			Name:          "Manual folder",
			Category:      "local_folder",
			Enabled:       true,
			LocalOnly:     true,
			Status:        "active",
			SyncFrequency: "manual",
		},
		&models.ConnectedSource{
			ID:            uuid.New(),
			ConnectorKey:  "local-folder",
			Name:          "Fresh folder",
			Category:      "local_folder",
			Enabled:       true,
			LocalOnly:     true,
			Status:        "active",
			SyncFrequency: "1h",
			LastSyncedAt:  &lastSync,
		},
	)
	service := NewService(repo, &fakeSourceMemoryService{})

	run, err := service.RunDueScheduledSyncs(lastSync.Add(10 * time.Minute))
	if err != nil {
		t.Fatalf("RunDueScheduledSyncs: %v", err)
	}
	if run.Checked != 2 || run.Due != 0 || run.Completed != 0 || run.Skipped != 2 {
		t.Fatalf("run = %#v, want two skipped sources", run)
	}
}

func TestConnectorsExposeOperationalLocalAdapters(t *testing.T) {
	service := NewService(newFakeSourceRepo(), &fakeSourceMemoryService{})
	connectors, err := service.Connectors()
	if err != nil {
		t.Fatalf("Connectors: %v", err)
	}
	// The catalog must be honest about what each connector actually does: only
	// the two live remote adapters are "operational"; the local-file readers are
	// "local_only"; odoo-herp is "modeled". Every one is still enabled and usable
	// — honesty about kind is not the same as disabling anything.
	wantStatus := map[string]string{
		"github":          AdapterOperational,
		"json-feed":       AdapterOperational,
		"email":           AdapterLocalOnly,
		"calendar":        AdapterLocalOnly,
		"cloud-documents": AdapterLocalOnly,
		"project-board":   AdapterLocalOnly,
		"local-folder":    AdapterLocalOnly,
		"whatsapp-export": AdapterLocalOnly,
		"whisper-audio":   AdapterLocalOnly,
		"odoo-herp":       AdapterModeled,
	}
	seen := map[string]bool{}
	for _, connector := range connectors {
		want, tracked := wantStatus[connector.ConnectorKey]
		if !tracked {
			continue
		}
		seen[connector.ConnectorKey] = true
		if !connector.Enabled {
			t.Fatalf("%s connector should stay enabled, got %#v", connector.ConnectorKey, connector)
		}
		if connector.AdapterStatus != want {
			t.Fatalf("%s AdapterStatus = %q, want %q", connector.ConnectorKey, connector.AdapterStatus, want)
		}
		if !adapterIsUsable(connector.AdapterStatus) {
			t.Fatalf("%s should remain usable despite honest status %q", connector.ConnectorKey, connector.AdapterStatus)
		}
	}
	for key := range wantStatus {
		if !seen[key] {
			t.Fatalf("connector %s missing from catalog", key)
		}
	}
}

func TestCreateSourceAllowsOperationalEmailExportConnector(t *testing.T) {
	service := NewService(newFakeSourceRepo(), &fakeSourceMemoryService{})
	source, err := service.CreateSource(CreateSourceRequest{
		OwnerIdentity: "alice",
		ConnectorKey:  "email",
		Name:          "Robert email export",
		Enabled:       true,
		LocalOnly:     true,
		SyncFrequency: "manual",
		SyncTarget:    ".",
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if source.ConnectorKey != "email" || !source.Enabled || source.Category != "email" {
		t.Fatalf("source = %#v, want enabled email export source", source)
	}
	if source.OwnerIdentity != "alice" {
		t.Fatalf("OwnerIdentity = %q, want alice", source.OwnerIdentity)
	}
}

func TestSyncEmailExportUsesAllowlistedFolderAndEmailFilesOnly(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root+"/inbox.mbox", "From sender@example.com Sun Jun  1 10:00:00 2026\nSubject: Evidence request\n\nFollow up: prepare the requested evidence bundle.")
	writeTestFile(t, root+"/ignore.txt", "This is not an email export.")
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{ID: sourceID, ConnectorKey: "email", Name: "Mailbox", Category: "email", Enabled: true, LocalOnly: true, Status: "active", SyncTarget: "."})
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeHistoricalBackfill})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 1 || len(result.Extractions) != 1 || result.Extractions[0].ContentType != "email_export" {
		t.Fatalf("email export result = %#v", result)
	}
}

func TestSyncGitHubImportsReadOnlyRepositoryRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/demo":
			_, _ = w.Write([]byte(`{"id":1,"full_name":"acme/demo","html_url":"https://github.com/acme/demo","updated_at":"2026-07-09T10:00:00Z"}`))
		case "/repos/acme/demo/issues":
			_, _ = w.Write([]byte(`[{"id":2,"number":7,"title":"Fix source ingest","body":"Follow up: add safe connector tests.","html_url":"https://github.com/acme/demo/issues/7","updated_at":"2026-07-09T10:01:00Z","state":"open"}]`))
		case "/repos/acme/demo/pulls", "/repos/acme/demo/commits":
			_, _ = w.Write([]byte(`[]`))
		case "/repos/acme/demo/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("GITHUB_SOURCE_API_BASE_URL", server.URL)
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL", "true")
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{ID: sourceID, ConnectorKey: "github", Name: "Demo repo", Category: "github", Enabled: true, Status: "active", SyncTarget: "acme/demo"})
	result, err := NewService(repo, &fakeSourceMemoryService{}).Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsSeen != 2 || result.Job.CursorAfter != "2026-07-09T10:01:00Z" {
		t.Fatalf("GitHub sync result = %#v", result.Job)
	}
	if !repo.hasAudit("source.synced") {
		t.Fatalf("expected GitHub sync audit record")
	}
}

func TestCreateSourceAllowsOperationalLocalFolder(t *testing.T) {
	service := NewService(newFakeSourceRepo(), &fakeSourceMemoryService{})
	source, err := service.CreateSource(CreateSourceRequest{
		ConnectorKey:  "local-folder",
		Name:          "Local folder",
		Enabled:       true,
		LocalOnly:     true,
		SyncFrequency: "manual",
		SyncTarget:    ".",
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if source.ConnectorKey != "local-folder" || !source.Enabled {
		t.Fatalf("source = %#v, want enabled local-folder", source)
	}
}

func TestSyncJSONFeedImportsItemsAndAdvancesCursor(t *testing.T) {
	var receivedCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCursor = r.URL.Query().Get("cursor")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"nextCursor":"cursor-2",
			"items":[{
				"externalId":"message-2",
				"title":"Follow-up request",
				"content":"Follow up: prepare the evidence checklist by Friday.",
				"sourceUri":"local-bridge://email/message-2",
				"itemType":"email"
			}]
		}`))
	}))
	defer server.Close()

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:                sourceID,
		ConnectorKey:      "json-feed",
		Name:              "Local account bridge",
		Category:          "generic_feed",
		Enabled:           true,
		LocalOnly:         true,
		Status:            "active",
		SyncFrequency:     "1m",
		SyncTarget:        server.URL,
		DefaultProjectKey: "018-HAI",
		Cursor:            "cursor-1",
	})
	service := NewService(repo, &fakeSourceMemoryService{})

	result, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if receivedCursor != "cursor-1" {
		t.Fatalf("received cursor = %q, want cursor-1", receivedCursor)
	}
	if result.Job.ItemsSeen != 1 || len(result.Extractions) != 1 {
		t.Fatalf("result = %#v, want one imported extraction", result)
	}
	if result.Extractions[0].ProjectKey != "018-HAI" {
		t.Fatalf("project key = %q, want default project", result.Extractions[0].ProjectKey)
	}
	updated, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if updated.Cursor != "cursor-2" {
		t.Fatalf("cursor = %q, want cursor-2", updated.Cursor)
	}
}

func TestSyncContextCancelsRemoteFeedAndRetainsCursor(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: "json-feed", Name: "Cancelable local bridge",
		Category: "generic_feed", Enabled: true, LocalOnly: true, Status: "active",
		SyncTarget: server.URL, Cursor: "cursor-before",
	})
	service := NewService(repo, &fakeSourceMemoryService{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.SyncContext(ctx, sourceID, ImportRequest{Mode: ModeIncrementalSync})
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("source sync did not reach the remote feed")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SyncContext error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("source sync ignored caller cancellation")
	}
	if len(repo.jobs) != 1 || repo.jobs[0].Status != "failed" || repo.jobs[0].CursorAfter != "cursor-before" {
		t.Fatalf("canceled sync job = %#v", repo.jobs)
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Cursor != "cursor-before" {
		t.Fatalf("source cursor = %q, want retained cursor", stored.Cursor)
	}
}

func TestSyncJSONFeedRejectsUnallowlistedHost(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "json-feed",
		Name:         "Blocked external feed",
		Category:     "generic_feed",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
		SyncTarget:   "https://example.com/feed",
	})
	service := NewService(repo, &fakeSourceMemoryService{})

	_, err := service.Sync(sourceID, ImportRequest{Mode: ModeIncrementalSync})
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("error = %v, want allowlist rejection", err)
	}
}

func TestSyncCreatesWorkflowForActionableExtraction(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Legal mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)

	result, err := service.Sync(sourceID, ImportRequest{
		Mode: ModeManualImport,
		Items: []ImportItem{
			{
				ExternalID: "email-1",
				Title:      "Lawyer follow-up",
				Content:    "Follow up: draft a formal reply for the legal case before tomorrow.",
				SourceURI:  "mailto:lawyer@example.test",
				ItemType:   "email",
				ProjectKey: "Vivare dispute",
			},
		},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	if len(workflowSpy.requests) != 1 {
		t.Fatalf("workflow requests = %d, want 1", len(workflowSpy.requests))
	}
	request := workflowSpy.requests[0]
	if request.ProjectKey != "Vivare dispute" {
		t.Fatalf("ProjectKey = %q, want Vivare dispute", request.ProjectKey)
	}
	if request.Trigger != "source.extraction" {
		t.Fatalf("Trigger = %q, want source.extraction", request.Trigger)
	}
	if request.SourceID != result.Extractions[0].ID.String() {
		t.Fatalf("SourceID = %q, want stable extraction identity", request.SourceID)
	}
	if !repo.hasAudit("workflow.intake_created") {
		t.Fatalf("expected workflow intake audit record")
	}
}

func TestSyncDefersActionableExtractionWhenPursuitLinkerLacksLifecycleRouter(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:                sourceID,
		OwnerIdentity:     "alice",
		ConnectorKey:      "email",
		Name:              "Legal mailbox",
		Category:          "email",
		Enabled:           true,
		LocalOnly:         true,
		Status:            "active",
		DefaultProjectKey: "Vivare dispute",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	pursuitSpy := &fakeSourcePursuitLinker{result: &pursuit.AutoLinkResult{
		Linked:    true,
		PursuitID: uuid.New(),
		Score:     0.72,
	}}
	service := NewServiceWithWorkflowAndPursuitLinker(repo, &fakeSourceMemoryService{}, workflowSpy, pursuitSpy)

	result, err := service.Sync(sourceID, ImportRequest{
		Mode: ModeManualImport,
		Items: []ImportItem{{
			ExternalID: "email-1",
			Title:      "Lawyer follow-up",
			Content:    "Follow up: draft a formal reply for the legal case before tomorrow.",
			SourceURI:  "mailto:lawyer@example.test",
			ItemType:   "email",
			ProjectKey: "Vivare dispute",
		}},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	if len(workflowSpy.requests) != 0 || len(pursuitSpy.requests) != 0 {
		t.Fatalf("partial pursuit integration created workflow work: workflows=%#v links=%#v", workflowSpy.requests, pursuitSpy.requests)
	}
	if !repo.hasAudit("pursuit.intake_deferred") || repo.hasAudit("workflow.intake_created") || repo.hasAudit("workflow.intake_failed") {
		t.Fatalf("source intake was not retained as a clean deferred state")
	}
}

func TestSyncRoutesActionableExtractionThroughPursuitGateway(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:            sourceID,
		OwnerIdentity: "alice",
		ConnectorKey:  "email",
		Name:          "Legal mailbox",
		Category:      "email",
		Enabled:       true,
		LocalOnly:     true,
		Status:        "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	pursuitGateway := &fakeSourcePursuitGateway{}
	service := NewServiceWithWorkflowAndPursuitLinker(repo, &fakeSourceMemoryService{}, workflowSpy, pursuitGateway)

	result, err := service.Sync(sourceID, ImportRequest{
		Mode: ModeManualImport,
		Items: []ImportItem{{
			ExternalID: "email-pursuit-gateway",
			Title:      "Lawyer follow-up",
			Content:    "Follow up: draft a formal reply for the legal case before tomorrow.",
			SourceURI:  "mailto:lawyer@example.test",
			ItemType:   "email",
			ProjectKey: "Vivare dispute",
		}},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(workflowSpy.requests) != 0 {
		t.Fatalf("direct workflow intake bypassed pursuit gateway: %#v", workflowSpy.requests)
	}
	if len(pursuitGateway.routed) != 1 || pursuitGateway.routed[0].Trigger != "source.extraction" {
		t.Fatalf("pursuit gateway requests = %#v", pursuitGateway.routed)
	}
	if pursuitGateway.routed[0].OwnerIdentity != "alice" {
		t.Fatalf("routed workflow owner = %q, want alice", pursuitGateway.routed[0].OwnerIdentity)
	}
	if len(pursuitGateway.requests) != 0 {
		t.Fatalf("workflow was linked twice after pursuit routing: %#v", pursuitGateway.requests)
	}
	if len(result.PursuitOutcomes) != 1 || result.PursuitOutcomes[0].Status != "pursuit_routed" || result.PursuitOutcomes[0].WorkflowID == "" || result.PursuitOutcomes[0].PursuitID != pursuitGateway.pursuitID.String() {
		t.Fatalf("pursuit routing outcome = %#v, want routed workflow context", result.PursuitOutcomes)
	}
}

func TestSyncDefersCandidatePendingPursuitGatewayWithoutWorkflowFailure(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:            sourceID,
		OwnerIdentity: "alice",
		ConnectorKey:  "email",
		Name:          "Legal mailbox",
		Category:      "email",
		Enabled:       true,
		LocalOnly:     true,
		Status:        "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	pursuitGateway := &fakeSourcePursuitGateway{err: &pursuit.CandidatePendingError{Result: &pursuit.RoutedIntakeResult{
		Mode:             "candidate_created",
		CreatedCandidate: true,
		PursuitID:        uuid.New(),
		Message:          "source candidate awaits approval",
	}}}
	service := NewServiceWithWorkflowAndPursuitLinker(repo, &fakeSourceMemoryService{}, workflowSpy, pursuitGateway)

	result, err := service.Sync(sourceID, ImportRequest{Mode: ModeManualImport, Items: []ImportItem{{
		ExternalID: "email-candidate-pending",
		Title:      "Lawyer follow-up",
		Content:    "Follow up: draft a formal reply for the legal case before tomorrow.",
		SourceURI:  "mailto:lawyer@example.test",
		ItemType:   "email",
		ProjectKey: "Vivare dispute",
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(workflowSpy.requests) != 0 || len(pursuitGateway.routed) != 1 {
		t.Fatalf("candidate pending path bypassed gateway or created workflow: workflows=%#v routed=%#v", workflowSpy.requests, pursuitGateway.routed)
	}
	if !repo.hasAudit("pursuit.intake_deferred") || repo.hasAudit("workflow.intake_failed") {
		t.Fatalf("candidate pending source audit was not a successful deferral")
	}
	if len(result.PursuitOutcomes) != 1 || result.PursuitOutcomes[0].Status != "candidate_pending" || result.PursuitOutcomes[0].PursuitID != pursuitGateway.err.(*pursuit.CandidatePendingError).Result.PursuitID.String() {
		t.Fatalf("candidate pursuit outcome = %#v, want the reviewable candidate", result.PursuitOutcomes)
	}
}

func TestSearchExcludesOtherOwnersSourceExtractions(t *testing.T) {
	aliceID := uuid.New()
	bobID := uuid.New()
	legacyID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice mailbox", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: bobID, OwnerIdentity: "bob", Name: "Bob mailbox", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: legacyID, Name: "Legacy local source", Enabled: true, Status: "active"},
	)
	for _, extraction := range []models.SourceExtraction{
		{ID: uuid.New(), SourceID: aliceID, Text: "Alice legal evidence deadline", Summary: "Alice legal evidence deadline"},
		{ID: uuid.New(), SourceID: bobID, Text: "Bob legal evidence deadline", Summary: "Bob legal evidence deadline"},
		{ID: uuid.New(), SourceID: legacyID, Text: "Legacy legal evidence deadline", Summary: "Legacy legal evidence deadline"},
	} {
		copyExtraction := extraction
		if _, err := repo.SaveExtraction(&copyExtraction); err != nil {
			t.Fatalf("SaveExtraction: %v", err)
		}
	}

	result, err := NewService(repo, nil).Search(SearchRequest{OwnerIdentity: "alice", Query: "legal evidence deadline", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.UsedContext) != 1 {
		t.Fatalf("visible search results = %#v, want Alice only", result.UsedContext)
	}
	for _, ranked := range result.UsedContext {
		if ranked.Extraction.SourceID == bobID || ranked.Extraction.SourceID == legacyID {
			t.Fatalf("search returned data outside Alice's exact owner scope")
		}
	}
	if len(repo.lastExtractionSourceIDs) != 1 {
		t.Fatalf("search loaded source ids %#v, want only Alice's source", repo.lastExtractionSourceIDs)
	}
	for _, sourceID := range repo.lastExtractionSourceIDs {
		if sourceID == bobID || sourceID == legacyID {
			t.Fatalf("search repository query included a source outside Alice's exact owner scope")
		}
	}
}

func TestSearchAllowsOwnerlessSourcesOnlyForConfiguredLegacyOwner(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "alice")
	aliceID := uuid.New()
	legacyID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: aliceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: legacyID, Name: "Legacy source", Enabled: true, Status: "active"},
	)
	for _, sourceID := range []uuid.UUID{aliceID, legacyID} {
		_, _ = repo.SaveExtraction(&models.SourceExtraction{ID: uuid.New(), SourceID: sourceID, Text: "migration evidence", Summary: "migration evidence"})
	}

	result, err := NewService(repo, nil).Search(SearchRequest{OwnerIdentity: "alice", Query: "migration evidence", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.UsedContext) != 2 {
		t.Fatalf("configured migration owner results = %#v, want owned and ownerless", result.UsedContext)
	}
}

func TestSearchExcludesRevokedSourceExtractions(t *testing.T) {
	activeID := uuid.New()
	revokedID := uuid.New()
	revokedAt := time.Now().UTC().Add(-time.Minute)
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: activeID, OwnerIdentity: "alice", Name: "Active mailbox", Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: revokedID, OwnerIdentity: "alice", Name: "Revoked mailbox", Enabled: false, Status: "revoked", RevokedAt: &revokedAt},
	)
	for _, extraction := range []models.SourceExtraction{
		{ID: uuid.New(), SourceID: activeID, Text: "Active legal evidence deadline", Summary: "Active legal evidence deadline"},
		{ID: uuid.New(), SourceID: revokedID, Text: "Revoked legal evidence deadline", Summary: "Revoked legal evidence deadline"},
	} {
		copyExtraction := extraction
		if _, err := repo.SaveExtraction(&copyExtraction); err != nil {
			t.Fatalf("SaveExtraction: %v", err)
		}
	}

	result, err := NewService(repo, nil).Search(SearchRequest{OwnerIdentity: "alice", Query: "legal evidence deadline", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.UsedContext) != 1 || result.UsedContext[0].Extraction.SourceID != activeID {
		t.Fatalf("visible search results = %#v, want active source only", result.UsedContext)
	}
	if len(repo.lastExtractionSourceIDs) != 1 || repo.lastExtractionSourceIDs[0] != activeID {
		t.Fatalf("repository source filter = %#v, want active source only", repo.lastExtractionSourceIDs)
	}

	extractions, err := NewService(repo, nil).ExtractionsForOwner("alice", "", true)
	if err != nil {
		t.Fatalf("ExtractionsForOwner: %v", err)
	}
	if len(extractions) != 1 || extractions[0].SourceID != activeID {
		t.Fatalf("visible extractions = %#v, want active source only", extractions)
	}
}

func TestRevokedSourceCannotBeReactivatedOrProcessed(t *testing.T) {
	sourceID := uuid.New()
	revokedAt := time.Now().UTC().Add(-time.Minute)
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: "Revoked files",
		Enabled: false, Status: "revoked", RevokedAt: &revokedAt, SyncTarget: "notes",
	})
	service := NewService(repo, nil)
	enabled := true

	operations := []struct {
		name string
		run  func() error
	}{
		{name: "update", run: func() error {
			_, err := service.UpdateSource(sourceID, UpdateSourceRequest{Enabled: &enabled})
			return err
		}},
		{name: "sync", run: func() error {
			_, err := service.Sync(sourceID, ImportRequest{Mode: ModeManualImport, Items: []ImportItem{{ExternalID: "new", Content: "must not import"}}})
			return err
		}},
		{name: "reindex", run: func() error {
			_, err := service.Reindex(sourceID)
			return err
		}},
		{name: "pause", run: func() error {
			_, err := service.Pause(sourceID, true)
			return err
		}},
		{name: "resume", run: func() error {
			_, err := service.Pause(sourceID, false)
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); !errors.Is(err, ErrSourceRevoked) {
				t.Fatalf("error = %v, want ErrSourceRevoked", err)
			}
		})
	}
	stored, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if stored.Enabled || stored.Status != "revoked" || stored.RevokedAt == nil {
		t.Fatalf("revoked source changed: %#v", stored)
	}
	if len(repo.rawItems) != 0 || len(repo.extractions) != 0 {
		t.Fatalf("revoked source produced data: raw=%#v extractions=%#v", repo.rawItems, repo.extractions)
	}
}

func TestSemanticSearchCannotReturnRevokedSourceExtraction(t *testing.T) {
	revokedID := uuid.New()
	revokedAt := time.Now().UTC().Add(-time.Minute)
	extraction := models.SourceExtraction{ID: uuid.New(), SourceID: revokedID, Text: "Revoked private source content"}
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: revokedID, OwnerIdentity: "alice", Name: "Revoked source", Enabled: false, Status: "revoked", RevokedAt: &revokedAt,
	})
	if _, err := repo.SaveExtraction(&extraction); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	semanticService := &fakeSemanticService{matches: []semantic.Match{{Extraction: extraction, Similarity: 0.99}}}
	service := NewServiceWithWorkflowPursuitAndSemantic(repo, nil, nil, nil, semanticService)

	result, err := service.Search(SearchRequest{OwnerIdentity: "alice", Query: "private source", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.UsedContext) != 0 {
		t.Fatalf("semantic search returned revoked source context: %#v", result.UsedContext)
	}
}

func TestSearchUsesSemanticResultsWithoutLoadingEveryExtraction(t *testing.T) {
	sourceID := uuid.New()
	extraction := models.SourceExtraction{ID: uuid.New(), SourceID: sourceID, Text: "Semantic evidence from a local source"}
	repo := newFakeSourceRepo(&models.ConnectedSource{ID: sourceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active"})
	if _, err := repo.SaveExtraction(&extraction); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	semanticService := &fakeSemanticService{matches: []semantic.Match{{Extraction: extraction, Similarity: 0.92}}}
	service := NewServiceWithWorkflowPursuitAndSemantic(repo, nil, nil, nil, semanticService)

	result, err := service.Search(SearchRequest{OwnerIdentity: "alice", Query: "local source", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.UsedContext) != 1 || result.UsedContext[0].Score != 0.92 {
		t.Fatalf("semantic search result = %#v", result)
	}
	if !strings.Contains(result.Explanation, "pgvector") {
		t.Fatalf("semantic retrieval explanation missing: %q", result.Explanation)
	}
	if len(repo.lastExtractionSourceIDs) != 0 {
		t.Fatalf("semantic search should not preload all extractions: %#v", repo.lastExtractionSourceIDs)
	}
	if semanticService.request.OwnerIdentity != "alice" || semanticService.request.Query != "local source" {
		t.Fatalf("semantic search request = %#v", semanticService.request)
	}
}

func TestOwnerScopedSourceWritesOwnerScopedMemory(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:            sourceID,
		OwnerIdentity: "alice",
		ConnectorKey:  "email",
		Name:          "Alice mailbox",
		Category:      "email",
		Enabled:       true,
		LocalOnly:     true,
		Status:        "active",
	})
	memorySpy := &fakeSourceMemoryService{}
	service := NewService(repo, memorySpy)
	if _, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "alice-context",
		Title:      "Alice context",
		Content:    "Alice prefers concise evidence summaries for this project.",
		ItemType:   "email",
	}}}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(memorySpy.ownerCreated) != 1 || memorySpy.ownerCreated[0].ownerIdentity != "alice" {
		t.Fatalf("owner-scoped memory writes = %#v, want one Alice memory", memorySpy.ownerCreated)
	}
	if len(memorySpy.created) != 0 {
		t.Fatalf("global memory writes = %#v, want none for owner-scoped source", memorySpy.created)
	}
}

func TestSyncAutoLinksStableSourceMemoryToPursuit(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:                sourceID,
		ConnectorKey:      "email",
		Name:              "Legal mailbox",
		Category:          "email",
		Enabled:           true,
		LocalOnly:         true,
		Status:            "active",
		DefaultProjectKey: "Vivare dispute",
	})
	memorySpy := &fakeSourceMemoryService{}
	workflowSpy := &fakeSourceWorkflowService{}
	pursuitSpy := &fakeSourcePursuitLinker{memoryResult: &pursuit.AutoLinkResult{
		Linked:    true,
		PursuitID: uuid.New(),
		Score:     0.78,
	}}
	service := NewServiceWithWorkflowAndPursuitLinker(repo, memorySpy, workflowSpy, pursuitSpy)

	result, err := service.Sync(sourceID, ImportRequest{
		Mode: ModeManualImport,
		Items: []ImportItem{{
			ExternalID: "email-context-1",
			Title:      "Vivare context note",
			Content:    "Robert prefers formal Dutch summaries for Vivare correspondence and evidence bundles attached to lawyer messages.",
			SourceURI:  "mailto:vivare-context@example.test",
			ItemType:   "email",
			ProjectKey: "Vivare dispute",
		}},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Extractions) != 1 {
		t.Fatalf("extractions = %d, want 1", len(result.Extractions))
	}
	if len(workflowSpy.requests) != 0 {
		t.Fatalf("workflow requests = %#v, want none for stable context memory", workflowSpy.requests)
	}
	if len(memorySpy.created) != 1 {
		t.Fatalf("created memories = %d, want one stable source memory", len(memorySpy.created))
	}
	if len(pursuitSpy.memoryRequests) != 1 {
		t.Fatalf("pursuit memory auto-link requests = %d, want 1", len(pursuitSpy.memoryRequests))
	}
	request := pursuitSpy.memoryRequests[0]
	if request.MemoryID == uuid.Nil || request.ProjectKey != "Vivare dispute" {
		t.Fatalf("memory auto-link request memory/project = %s/%q", request.MemoryID, request.ProjectKey)
	}
	if request.AllowCreateCandidate {
		t.Fatalf("stable source memory must not create noisy pursuit candidates")
	}
	if request.SourceURI != "mailto:vivare-context@example.test" || request.SourceLabel != "Vivare context note" {
		t.Fatalf("memory auto-link source reference = %q/%q", request.SourceURI, request.SourceLabel)
	}
	if !repo.hasAudit("pursuit.memory_auto_linked") {
		t.Fatalf("expected pursuit memory auto-link audit record")
	}
}

func TestSyncRetainsCursorWhenWorkflowIntakePartiallyFails(t *testing.T) {
	lastSync := time.Now().UTC().Add(-time.Hour)
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
		Cursor:       "cursor-before",
		LastSyncedAt: &lastSync,
	})
	workflowSpy := &fakeSourceWorkflowService{intakeErr: errors.New("workflow database unavailable")}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)

	result, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{
		{
			ExternalID: "context-only",
			Title:      "Background",
			Content:    "A sufficiently long informational record describing confirmed project context and background details.",
		},
		{
			ExternalID: "actionable",
			Title:      "Action",
			Content:    "Follow up: prepare a detailed project checklist and confirm the result with the project owner.",
		},
	}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.Status != "partial_failure" || result.Job.ItemsFailed != 1 {
		t.Fatalf("job = %#v, want one partial failure", result.Job)
	}
	if result.Job.CursorAfter != "cursor-before" || len(result.Errors) != 1 {
		t.Fatalf("cursor/errors = %q/%#v, want retained cursor and error detail", result.Job.CursorAfter, result.Errors)
	}
	updated, err := repo.FindSource(sourceID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if updated.Cursor != "cursor-before" || updated.LastSyncedAt == nil || !updated.LastSyncedAt.Equal(lastSync) {
		t.Fatalf("partial failure advanced source state: %#v", updated)
	}
	if !repo.hasAudit("source.sync_partial_failure") {
		t.Fatalf("expected partial failure audit record")
	}
}

func TestSyncCapsReturnedFailureDetails(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	workflowSpy := &fakeSourceWorkflowService{intakeErr: errors.New("workflow unavailable")}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	items := make([]ImportItem, maxSyncErrorDetails+5)
	for index := range items {
		items[index] = ImportItem{
			ExternalID: fmt.Sprintf("actionable-%d", index),
			Title:      "Action",
			Content:    "Follow up: prepare a detailed project checklist and confirm the result with the project owner.",
		}
	}

	result, err := service.Sync(sourceID, ImportRequest{Items: items})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Job.ItemsFailed != len(items) {
		t.Fatalf("failed count = %d, want %d", result.Job.ItemsFailed, len(items))
	}
	if len(result.Errors) != maxSyncErrorDetails {
		t.Fatalf("error details = %d, want cap %d", len(result.Errors), maxSyncErrorDetails)
	}
}

func TestScheduledSyncCountsPartialResultAsFailed(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root+"/action.md", "Follow up: prepare a detailed project checklist and confirm the result with the project owner.")
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:            sourceID,
		ConnectorKey:  "local-folder",
		Name:          "Scheduled folder",
		Category:      "local_folder",
		Enabled:       true,
		LocalOnly:     true,
		Status:        "active",
		SyncFrequency: "1m",
		SyncTarget:    ".",
	})
	workflowSpy := &fakeSourceWorkflowService{intakeErr: errors.New("workflow unavailable")}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)

	run, err := service.RunDueScheduledSyncs(time.Now().UTC())
	if err != nil {
		t.Fatalf("RunDueScheduledSyncs: %v", err)
	}
	if run.Completed != 0 || run.Failed != 1 {
		t.Fatalf("run = %#v, want failed scheduled sync", run)
	}
	if len(workflowSpy.requests) != 2 {
		t.Fatalf("workflow requests = %d, want extraction failure plus operational review", len(workflowSpy.requests))
	}
	failureWorkflow := workflowSpy.requests[len(workflowSpy.requests)-1]
	if failureWorkflow.SourceType != "source_sync" || !failureWorkflow.RequiresReview {
		t.Fatalf("failure workflow = %#v, want source_sync review workflow", failureWorkflow)
	}
	updated, _ := repo.FindSource(sourceID)
	if updated.LastSyncedAt != nil || updated.Cursor != "" {
		t.Fatalf("failed scheduled sync advanced source state: %#v", updated)
	}
}

func TestSyncJobsReturnsPersistentHistory(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Imported mailbox records",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	service := NewService(repo, &fakeSourceMemoryService{})
	if _, err := service.Sync(sourceID, ImportRequest{
		Items: []ImportItem{{
			ExternalID: "mail-1",
			Title:      "Follow-up",
			Content:    "Follow up: prepare the project status.",
		}},
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	jobs, err := service.SyncJobs(&sourceID, sourceHistoryDefaultLimit)
	if err != nil {
		t.Fatalf("SyncJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != "completed" {
		t.Fatalf("jobs = %#v, want one completed sync job", jobs)
	}
}

func TestSyncRejectsOverlappingRunForSameSource(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	started := make(chan struct{})
	release := make(chan struct{})
	workflowSpy := &fakeSourceWorkflowService{intakeStarted: started, intakeRelease: release}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	firstDone := make(chan error, 1)

	go func() {
		_, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
			ExternalID: "first",
			Title:      "First",
			Content:    "Follow up: prepare a detailed project checklist and confirm the result with the project owner.",
		}}})
		firstDone <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("first sync did not reach workflow intake")
	}

	_, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "second",
		Title:      "Second",
		Content:    "Follow up: prepare another detailed project checklist.",
	}}})
	if !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("overlapping sync error = %v, want ErrSyncInProgress", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Sync: %v", err)
	}
}

func TestSyncCreatesSeparateWorkflowCandidatesForSharedSourceURI(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)

	result, err := service.Sync(sourceID, ImportRequest{
		Mode: ModeManualImport,
		Items: []ImportItem{
			{ExternalID: "message-1", Title: "First", Content: "Follow up: prepare the first detailed project checklist for review.", SourceURI: "mailto:shared@example.test"},
			{ExternalID: "message-2", Title: "Second", Content: "Follow up: prepare the second detailed project checklist for review.", SourceURI: "mailto:shared@example.test"},
		},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(workflowSpy.requests) != 2 || len(result.Extractions) != 2 {
		t.Fatalf("workflow requests=%d extractions=%d, want 2", len(workflowSpy.requests), len(result.Extractions))
	}
	if workflowSpy.requests[0].SourceID == workflowSpy.requests[1].SourceID {
		t.Fatalf("separate source records received the same workflow identity")
	}
}

func TestSyncRoutesUncertainActionableExtractionToReview(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)

	_, err := service.Sync(sourceID, ImportRequest{
		Items: []ImportItem{{ExternalID: "short", Title: "Short", Content: "Todo: call.", SourceURI: "local://short"}},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(workflowSpy.requests) != 1 || !workflowSpy.requests[0].RequiresReview {
		t.Fatalf("uncertain extraction was not review gated: %#v", workflowSpy.requests)
	}
	if !strings.Contains(workflowSpy.requests[0].ReviewReason, "uncertain") {
		t.Fatalf("review reason = %q", workflowSpy.requests[0].ReviewReason)
	}
}

func TestSyncCalendarCancellationStopsPriorWorkAndRequiresReview(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: calendarConnectorKey, Name: "Robert Calendar",
		Category: "calendar", Enabled: true, Status: "active", DefaultProjectKey: "Robert-life-os",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)

	result, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "google-calendar:event-cancelled",
		Title:      "Hearing (cancelled in Google Calendar)",
		Content:    "Google Calendar reports that this event was cancelled. Preserve prior HAI context and obligations for owner review; do not delete tasks, commitments, or evidence automatically.",
		SourceURI:  "https://calendar.google.com/calendar/event?eid=cancelled",
		ItemType:   "google_calendar_event_cancelled",
		ProjectKey: "Robert-life-os",
		Metadata:   `{"source":"google-calendar","cancelled":true,"reviewRequired":true,"writebackAllowed":false}`,
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Extractions) != 1 || !result.Extractions[0].Uncertain {
		t.Fatalf("cancellation extraction was not review marked: %#v", result.Extractions)
	}
	if len(workflowSpy.retractions) != 1 || workflowSpy.retractions[0].sourceType != "calendar" {
		t.Fatalf("retractions = %#v, want one calendar retraction", workflowSpy.retractions)
	}
	if len(workflowSpy.requests) != 1 || !workflowSpy.requests[0].RequiresReview {
		t.Fatalf("workflow requests = %#v, want one review-gated cancellation", workflowSpy.requests)
	}
	if !strings.Contains(workflowSpy.requests[0].ReviewReason, "calendar cancellation") {
		t.Fatalf("review reason = %q", workflowSpy.requests[0].ReviewReason)
	}
	if !repo.hasAudit("workflow.calendar_event_retracted") {
		t.Fatal("missing calendar retraction audit record")
	}
}

func TestSyncCalendarUpcomingMeetingCreatesPreparationWorkflow(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: calendarConnectorKey, Name: "Robert Calendar",
		Category: "calendar", Enabled: true, Status: "active", DefaultProjectKey: "Robert-life-os",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	end := start.Add(time.Hour)
	metadata := fmt.Sprintf(`{"start":%q,"end":%q,"attendeeCount":1,"readonly":true,"reviewRequired":false}`, start.Format(time.RFC3339), end.Format(time.RFC3339))

	_, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "google-calendar:event-upcoming",
		Title:      "Project review",
		Content:    "Google Calendar event: Project review\nStart: " + start.Format(time.RFC3339) + "\nEnd: " + end.Format(time.RFC3339) + "\nAttendees: owner@example.test",
		SourceURI:  "https://calendar.google.com/calendar/event?eid=upcoming",
		ItemType:   "google_calendar_event",
		ProjectKey: "Robert-life-os",
		Metadata:   metadata,
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(workflowSpy.requests) != 1 || !strings.Contains(workflowSpy.requests[0].Input, "HAI proposal: review preparation") {
		t.Fatalf("workflow requests = %#v, want one preparation proposal", workflowSpy.requests)
	}
	if workflowSpy.requests[0].RequiresReview {
		t.Fatalf("low-risk local preparation was unexpectedly approval gated: %#v", workflowSpy.requests[0])
	}
}

func TestSyncCalendarConflictWorkflowIsStableAndRetractedWhenResolved(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: calendarConnectorKey, Name: "Robert Calendar",
		Category: "calendar", Enabled: true, Status: "active", DefaultProjectKey: "Robert-life-os",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)

	event := func(id, title string, eventStart time.Time) ImportItem {
		end := eventStart.Add(time.Hour)
		return ImportItem{
			ExternalID: "google-calendar:" + id, Title: title,
			Content:   "Google Calendar event: " + title + "\nStart: " + eventStart.Format(time.RFC3339) + "\nEnd: " + end.Format(time.RFC3339),
			SourceURI: "https://calendar.google.com/calendar/event?eid=" + id,
			ItemType:  "google_calendar_event",
			Metadata:  fmt.Sprintf(`{"start":%q,"end":%q,"attendeeCount":0,"readonly":true}`, eventStart.Format(time.RFC3339), end.Format(time.RFC3339)),
		}
	}
	left := event("left", "Reserved A", start)
	right := event("right", "Reserved B", start.Add(30*time.Minute))
	first, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{left, right}})
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if len(first.Extractions) != 3 || len(workflowSpy.requests) != 1 {
		t.Fatalf("extractions=%d requests=%#v, want one conflict workflow", len(first.Extractions), workflowSpy.requests)
	}
	if !workflowSpy.requests[0].RequiresReview || workflowSpy.requests[0].ProjectKey != "Robert-life-os" || !strings.Contains(workflowSpy.requests[0].Input, "detected schedule conflict") {
		t.Fatalf("conflict workflow = %#v", workflowSpy.requests[0])
	}

	moved := event("right", "Reserved B", start.Add(3*time.Hour))
	second, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{moved}})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(second.Extractions) != 2 || len(workflowSpy.requests) != 1 {
		t.Fatalf("resolved extractions=%d requests=%d, want moved event plus resolution and no replacement workflow", len(second.Extractions), len(workflowSpy.requests))
	}
	if len(workflowSpy.retractions) != 1 || !strings.Contains(workflowSpy.retractions[0].reason, "overlap is no longer present") {
		t.Fatalf("retractions = %#v", workflowSpy.retractions)
	}
}

func TestReindexUsesCachedRawContentAndPreservesMetadata(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Imported records",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	service := NewService(repo, &fakeSourceMemoryService{})
	content := "Decision: preserve cached source content. Follow up: verify the reindex result."
	metadata := `{"threadId":"thread-1"}`

	if _, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "message-1",
		Title:      "Reindex record",
		Content:    content,
		Metadata:   metadata,
	}}}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	rawItems, err := repo.FindRawItems(sourceID)
	if err != nil || len(rawItems) != 1 {
		t.Fatalf("FindRawItems: items=%#v err=%v", rawItems, err)
	}
	if rawItems[0].Content != content || rawItems[0].Metadata != metadata {
		t.Fatalf("raw cache mixed content and metadata: %#v", rawItems[0])
	}
	extraction, err := repo.FindExtractionByRawItem(rawItems[0].ID)
	if err != nil {
		t.Fatalf("FindExtractionByRawItem: %v", err)
	}
	repo.index = append(repo.index, models.SourceIndexEntry{
		ID:           uuid.New(),
		SourceID:     sourceID,
		ExtractionID: extraction.ID,
		IndexType:    "vector_ref",
		VectorRef:    "local-vector-pending:" + extraction.ID.String(),
	})

	result, err := service.Reindex(sourceID)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if len(result.Extractions) != 1 || result.Extractions[0].Text != content {
		t.Fatalf("reindex did not use cached content: %#v", result.Extractions)
	}
	if len(repo.index) != 1 || repo.index[0].IndexType != "keyword" || repo.index[0].VectorRef != "" {
		t.Fatalf("reindex retained placeholder or duplicate index rows: %#v", repo.index)
	}
	rawItems, _ = repo.FindRawItems(sourceID)
	if rawItems[0].Metadata != metadata {
		t.Fatalf("reindex overwrote raw metadata: %#v", rawItems[0])
	}
}

func TestArchiveExtractionRetractsPendingWorkflowCandidate(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	result, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "message-archive",
		Title:      "Archive",
		Content:    "Follow up: prepare the detailed project checklist for review.",
		SourceURI:  "local://archive",
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	extractionID := result.Extractions[0].ID
	if _, err := service.ArchiveExtraction(extractionID, true); err != nil {
		t.Fatalf("ArchiveExtraction: %v", err)
	}
	if len(workflowSpy.retractions) != 1 || workflowSpy.retractions[0].sourceID != extractionID.String() {
		t.Fatalf("workflow retractions = %#v", workflowSpy.retractions)
	}
}

func TestDeleteExtractionRetractsWorkflowAndRemovesDerivedIndexMetadata(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:            sourceID,
		OwnerIdentity: "robert",
		ConnectorKey:  "email",
		Name:          "Project mailbox",
		Category:      "email",
		Enabled:       true,
		LocalOnly:     true,
		Status:        "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := authorizedSourceEffectService(
		NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy),
	)
	result, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "message-delete",
		Title:      "Delete",
		Content:    "Follow up: prepare the detailed project checklist for review.",
		SourceURI:  "local://delete",
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	extractionID := result.Extractions[0].ID
	repo.index = append(repo.index, models.SourceIndexEntry{
		ID:           uuid.New(),
		SourceID:     sourceID,
		ExtractionID: extractionID,
		IndexType:    "vector_ref",
		VectorRef:    "configured-local-vector:" + extractionID.String(),
	})
	if err := service.DeleteExtractionAuthorized(
		context.Background(),
		extractionID,
		testSourceAuthorization("robert"),
	); err != nil {
		t.Fatalf("DeleteExtraction: %v", err)
	}
	if _, err := repo.FindExtraction(extractionID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted extraction lookup error = %v, want not found", err)
	}
	if len(repo.index) != 0 {
		t.Fatalf("derived index metadata remained after deletion: %#v", repo.index)
	}
	if len(workflowSpy.retractions) != 1 || workflowSpy.retractions[0].sourceID != extractionID.String() {
		t.Fatalf("workflow retractions = %#v", workflowSpy.retractions)
	}
	if !repo.hasAudit("extraction.deleted") {
		t.Fatalf("expected deletion audit after successful deletion")
	}
}

func TestDeleteExtractionDoesNotAuditWhenRepositoryDeleteFails(t *testing.T) {
	sourceID := uuid.New()
	extractionID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{ID: sourceID, OwnerIdentity: "robert", ConnectorKey: "email", Name: "Project mailbox", Category: "email", Enabled: true, Status: "active"})
	if _, err := repo.SaveExtraction(&models.SourceExtraction{ID: extractionID, SourceID: sourceID, Summary: "Private source context"}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}
	repo.index = append(repo.index, models.SourceIndexEntry{ID: uuid.New(), SourceID: sourceID, ExtractionID: extractionID, IndexType: "keyword", Keywords: "private,source,context"})
	repo.deleteExtractionErr = errors.New("storage unavailable")
	if err := authorizedSourceEffectService(NewService(repo, nil)).DeleteExtractionAuthorized(
		context.Background(),
		extractionID,
		testSourceAuthorization("robert"),
	); err == nil {
		t.Fatal("expected repository delete failure")
	}
	if repo.hasAudit("extraction.deleted") {
		t.Fatalf("deletion audit was recorded before storage deletion succeeded: %#v", repo.auditLogs)
	}
	if _, err := repo.FindExtraction(extractionID); err != nil {
		t.Fatalf("failed deletion removed extraction: %v", err)
	}
	if len(repo.index) != 1 {
		t.Fatalf("failed deletion removed derived metadata: %#v", repo.index)
	}
}

func TestCorrectingAwayActionableFieldsRetractsWorkflowCandidate(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	result, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "message-correct",
		Title:      "Correction",
		Content:    "Follow up: prepare the detailed project checklist for review.",
		SourceURI:  "local://correction",
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	extraction := result.Extractions[0]
	extraction.Tasks = ""
	extraction.FollowUps = ""
	if _, err := service.UpdateExtraction(extraction.ID, extraction); err != nil {
		t.Fatalf("UpdateExtraction: %v", err)
	}
	if len(workflowSpy.retractions) != 1 || workflowSpy.retractions[0].sourceID != extraction.ID.String() {
		t.Fatalf("workflow retractions = %#v", workflowSpy.retractions)
	}
}

func TestCorrectingActionableExtractionReconcilesRevisedWorkflowInput(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	workflowSpy := &fakeSourceWorkflowService{}
	service := NewServiceWithWorkflow(repo, &fakeSourceMemoryService{}, workflowSpy)
	result, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "message-revised",
		Title:      "Correction",
		Content:    "Follow up: prepare the original project checklist.",
		SourceURI:  "local://revised-correction",
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	extraction := result.Extractions[0]
	extraction.Tasks = "prepare the revised evidence checklist"
	extraction.FollowUps = "ask Robert to review the revised checklist"
	if _, err := service.UpdateExtraction(extraction.ID, extraction); err != nil {
		t.Fatalf("UpdateExtraction: %v", err)
	}
	if len(workflowSpy.requests) != 2 {
		t.Fatalf("workflow intake requests = %d, want original and revised input", len(workflowSpy.requests))
	}
	original := workflowSpy.requests[0]
	revised := workflowSpy.requests[1]
	if revised.SourceID != original.SourceID || revised.SourceID != extraction.ID.String() {
		t.Fatalf("revised workflow lost stable source identity: %#v", workflowSpy.requests)
	}
	if revised.Input == original.Input || !strings.Contains(revised.Input, "revised evidence checklist") {
		t.Fatalf("revised workflow input was not reconciled: %q", revised.Input)
	}
}

func TestCorrectingExtractionStoresCorrectionLessonMemory(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "email",
		Name:         "Project mailbox",
		Category:     "email",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	mem := &fakeSourceMemoryService{}
	workflowSpy := &fakeSourceWorkflowService{}
	pursuitSpy := &fakeSourcePursuitLinker{memoryResult: &pursuit.AutoLinkResult{Linked: true, PursuitID: uuid.New(), Score: 0.81}}
	service := NewServiceWithWorkflowAndPursuitLinker(repo, mem, workflowSpy, pursuitSpy)
	result, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "message-memory-correction",
		Title:      "Correction",
		Content:    "Short note",
		SourceURI:  "local://memory-correction",
		ItemType:   "email",
		ProjectKey: "018-HAI",
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(mem.created) != 0 {
		t.Fatalf("initial uncertain extraction stored %d memories, want 0", len(mem.created))
	}

	extraction := result.Extractions[0]
	extraction.Uncertain = false
	extraction.Summary = "Robert corrected the source into a concrete evidence checklist request."
	extraction.Tasks = "prepare the revised evidence checklist"
	extraction.FollowUps = "ask Robert to review the revised checklist"
	if _, err := service.UpdateExtraction(extraction.ID, extraction); err != nil {
		t.Fatalf("UpdateExtraction: %v", err)
	}

	if len(mem.created) != 1 {
		t.Fatalf("stored %d correction memories, want 1", len(mem.created))
	}
	if len(pursuitSpy.memoryRequests) != 1 {
		t.Fatalf("pursuit memory link requests = %d, want correction lesson linked", len(pursuitSpy.memoryRequests))
	}
	linkRequest := pursuitSpy.memoryRequests[0]
	if linkRequest.AllowCreateCandidate {
		t.Fatalf("correction lesson memory must not create pursuit candidates")
	}
	if linkRequest.ProjectKey != "018-HAI" || linkRequest.SourceURI != "local://memory-correction" {
		t.Fatalf("correction link request = %#v, want project and source provenance", linkRequest)
	}
	created := mem.created[0]
	if created.Kind != "lesson" || created.ProjectKey != "018-HAI" {
		t.Fatalf("created memory = %#v, want project-scoped lesson", created)
	}
	if created.Confidence < 0.75 {
		t.Fatalf("confidence = %.2f, want strong source-correction lesson", created.Confidence)
	}
	if !strings.Contains(created.Content, "revised evidence checklist") || !strings.Contains(created.Content, "Future behavior") {
		t.Fatalf("created memory did not preserve corrected behavior: %q", created.Content)
	}
	if !hasString(created.Tags, "source-correction") || !hasString(created.Tags, "email") {
		t.Fatalf("tags = %#v, want source correction and connector context", created.Tags)
	}
	if created.SourceURI != "local://memory-correction" || created.SourceLabel != "Correction" {
		t.Fatalf("source reference = %q/%q, want original provenance", created.SourceURI, created.SourceLabel)
	}
	if !repo.hasAudit("extraction.correction_memory_created") {
		t.Fatalf("expected extraction.correction_memory_created audit log")
	}
	if !repo.hasAudit("pursuit.memory_auto_linked") {
		t.Fatalf("expected correction lesson to be linked into pursuit context")
	}
}

func TestSensitiveExtractionCorrectionStoresReviewOnlyLesson(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID:           sourceID,
		ConnectorKey: "whatsapp-export",
		Name:         "WhatsApp export",
		Category:     "chat",
		Enabled:      true,
		LocalOnly:    true,
		Status:       "active",
	})
	mem := &fakeSourceMemoryService{}
	pursuitSpy := &fakeSourcePursuitLinker{memoryResult: &pursuit.AutoLinkResult{Linked: true, PursuitID: uuid.New(), Score: 0.68}}
	service := NewServiceWithWorkflowAndPursuitLinker(repo, mem, &fakeSourceWorkflowService{}, pursuitSpy)
	result, err := service.Sync(sourceID, ImportRequest{Items: []ImportItem{{
		ExternalID: "message-sensitive-correction",
		Title:      "Sensitive chat",
		Content:    "Legal matter password=supersecret Follow up: review with lawyer.",
		SourceURI:  "file://sensitive-chat.txt",
		ItemType:   "whatsapp_export",
		ProjectKey: "legal-case",
	}}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(mem.created) != 0 {
		t.Fatalf("initial sensitive extraction stored %d memories, want 0", len(mem.created))
	}

	extraction := result.Extractions[0]
	extraction.Sensitive = true
	extraction.Summary = "Corrected sensitive legal chat password=supersecret"
	extraction.Tasks = "draft legal response with password=supersecret"
	if _, err := service.UpdateExtraction(extraction.ID, extraction); err != nil {
		t.Fatalf("UpdateExtraction: %v", err)
	}

	if len(mem.created) != 1 {
		t.Fatalf("stored %d correction memories, want 1", len(mem.created))
	}
	if len(pursuitSpy.memoryRequests) != 1 {
		t.Fatalf("pursuit memory link requests = %d, want sensitive correction lesson linked", len(pursuitSpy.memoryRequests))
	}
	linkRequest := pursuitSpy.memoryRequests[0]
	if linkRequest.AllowCreateCandidate {
		t.Fatalf("sensitive correction lesson memory must not create pursuit candidates")
	}
	if linkRequest.SourceURI != "source-extraction://"+extraction.ID.String() || linkRequest.SourceLabel != "Sensitive connected-source correction" {
		t.Fatalf("sensitive correction link source = %q/%q", linkRequest.SourceURI, linkRequest.SourceLabel)
	}
	created := mem.created[0]
	if created.Kind != "lesson" || created.ProjectKey != "legal-case" {
		t.Fatalf("created memory = %#v, want project-scoped lesson", created)
	}
	if !strings.Contains(created.Content, "review-gated") || !strings.Contains(created.Content, "avoid storing raw sensitive content") {
		t.Fatalf("sensitive correction lesson is not review-gated: %q", created.Content)
	}
	leaked := strings.ToLower(created.Content + " " + created.Summary + " " + created.SourceURI + " " + created.SourceLabel)
	for _, forbidden := range []string{"supersecret", "password=supersecret", "sensitive-chat.txt"} {
		if strings.Contains(leaked, forbidden) {
			t.Fatalf("sensitive correction memory leaked %q: %#v", forbidden, created)
		}
	}
	if !hasString(created.Tags, "sensitive") || !hasString(created.Tags, "review-required") {
		t.Fatalf("tags = %#v, want sensitive review tags", created.Tags)
	}
	if created.SourceURI != "source-extraction://"+extraction.ID.String() || created.SourceLabel != "Sensitive connected-source correction" {
		t.Fatalf("source reference = %q/%q, want sanitized extraction reference", created.SourceURI, created.SourceLabel)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

type fakeSourceRepo struct {
	connectors              map[string]models.SourceConnector
	sources                 map[uuid.UUID]*models.ConnectedSource
	jobs                    []models.SourceSyncJob
	rawItems                map[uuid.UUID]*models.SourceRawItem
	extractions             map[uuid.UUID]*models.SourceExtraction
	index                   []models.SourceIndexEntry
	lastExtractionSourceIDs []uuid.UUID
	auditLogs               []models.SourceAuditLog
	deleteExtractionErr     error
	oauthTokens             map[uuid.UUID]*models.SourceOAuthToken
	oauthStates             map[uuid.UUID]*models.SourceOAuthState
	globalSourceReads       int
	ownerSourceReads        int
	lastSourceOwner         string
}

type fakeSemanticService struct {
	matches []semantic.Match
	err     error
	request semantic.SearchRequest
	search  func(context.Context, semantic.SearchRequest) ([]semantic.Match, error)
}

func (s *fakeSemanticService) Enabled() bool  { return true }
func (s *fakeSemanticService) Reason() string { return "test semantic service" }
func (s *fakeSemanticService) Index(context.Context, *models.SourceExtraction) error {
	return nil
}
func (s *fakeSemanticService) Search(ctx context.Context, request semantic.SearchRequest) ([]semantic.Match, error) {
	s.request = request
	if s.search != nil {
		return s.search(ctx, request)
	}
	return s.matches, s.err
}
func (s *fakeSemanticService) IndexMemory(context.Context, *models.ContextMemory) error { return nil }
func (s *fakeSemanticService) DeleteMemory(context.Context, uuid.UUID) error            { return nil }
func (s *fakeSemanticService) SearchMemory(context.Context, semantic.MemorySearchRequest) ([]semantic.MemoryMatch, error) {
	return nil, nil
}

func newFakeSourceRepo(sources ...*models.ConnectedSource) *fakeSourceRepo {
	repo := &fakeSourceRepo{
		connectors:  map[string]models.SourceConnector{},
		sources:     map[uuid.UUID]*models.ConnectedSource{},
		rawItems:    map[uuid.UUID]*models.SourceRawItem{},
		extractions: map[uuid.UUID]*models.SourceExtraction{},
		oauthTokens: map[uuid.UUID]*models.SourceOAuthToken{},
		oauthStates: map[uuid.UUID]*models.SourceOAuthState{},
	}
	for _, source := range sources {
		repo.sources[source.ID] = source
	}
	return repo
}

func (r *fakeSourceRepo) SaveOAuthState(state *models.SourceOAuthState) error {
	if state == nil {
		return gorm.ErrRecordNotFound
	}
	if !validOAuthStateDigest(strings.TrimSpace(state.StateDigest)) || !state.ExpiresAt.After(time.Now().UTC()) {
		return ErrOAuthStateInvalid
	}
	source, ok := r.sources[state.SourceID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if err := rejectRevokedSource(source); err != nil {
		return err
	}
	if strings.TrimSpace(source.OwnerIdentity) != strings.TrimSpace(state.OwnerIdentity) {
		return gorm.ErrRecordNotFound
	}
	stored := *state
	if stored.ID == uuid.Nil {
		stored.ID = uuid.New()
	}
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}
	r.oauthStates[state.SourceID] = &stored
	return nil
}

func (r *fakeSourceRepo) ConsumeOAuthState(
	sourceID uuid.UUID,
	ownerIdentity, stateDigest string,
	consumedAt time.Time,
) error {
	source, ok := r.sources[sourceID]
	if !ok || rejectRevokedSource(source) != nil ||
		strings.TrimSpace(source.OwnerIdentity) != strings.TrimSpace(ownerIdentity) {
		return ErrOAuthStateInvalid
	}
	state, ok := r.oauthStates[sourceID]
	if !ok || state.ConsumedAt != nil ||
		state.StateDigest != strings.TrimSpace(stateDigest) ||
		state.ExpiresAt.Before(consumedAt) {
		return ErrOAuthStateInvalid
	}
	consumedAt = consumedAt.UTC()
	state.ConsumedAt = &consumedAt
	return nil
}

func (r *fakeSourceRepo) SaveOAuthToken(token *models.SourceOAuthToken) error {
	source, ok := r.sources[token.SourceID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if err := rejectRevokedSource(source); err != nil {
		return err
	}
	if r.oauthTokens == nil {
		r.oauthTokens = map[uuid.UUID]*models.SourceOAuthToken{}
	}
	stored := *token
	r.oauthTokens[token.SourceID] = &stored
	return nil
}

func (r *fakeSourceRepo) FindOAuthToken(sourceID uuid.UUID) (*models.SourceOAuthToken, error) {
	if token, ok := r.oauthTokens[sourceID]; ok {
		copy := *token
		return &copy, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeSourceRepo) SaveConnector(connector *models.SourceConnector) (*models.SourceConnector, error) {
	existing, exists := r.connectors[connector.ConnectorKey]
	if connector.ID == uuid.Nil {
		if exists {
			connector.ID = existing.ID
		} else {
			connector.ID = uuid.New()
		}
	}
	now := time.Now().UTC()
	if connector.CreatedAt.IsZero() {
		if exists {
			connector.CreatedAt = existing.CreatedAt
		} else {
			connector.CreatedAt = now
		}
	}
	connector.UpdatedAt = now
	r.connectors[connector.ConnectorKey] = *connector
	return connector, nil
}

func (r *fakeSourceRepo) FindConnectors() ([]models.SourceConnector, error) {
	result := []models.SourceConnector{}
	for _, connector := range r.connectors {
		result = append(result, connector)
	}
	return result, nil
}

func (r *fakeSourceRepo) CreateSource(source *models.ConnectedSource) (*models.ConnectedSource, error) {
	if source.ID == uuid.Nil {
		source.ID = uuid.New()
	}
	now := time.Now().UTC()
	source.CreatedAt = now
	source.UpdatedAt = now
	r.sources[source.ID] = source
	return source, nil
}

func (r *fakeSourceRepo) UpdateSource(source *models.ConnectedSource) (*models.ConnectedSource, error) {
	current, ok := r.sources[source.ID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	if err := rejectRevokedSource(current); err != nil {
		return nil, err
	}
	if !current.UpdatedAt.Equal(source.UpdatedAt) {
		return nil, ErrSourceChanged
	}
	source.OwnerIdentity = current.OwnerIdentity
	source.ConnectorKey = current.ConnectorKey
	source.CreatedAt = current.CreatedAt
	source.RevokedAt = current.RevokedAt
	source.UpdatedAt = time.Now().UTC()
	r.sources[source.ID] = source
	return source, nil
}

func (r *fakeSourceRepo) RevokeSource(
	expected *models.ConnectedSource,
	ownerIdentity string,
	revokedAt time.Time,
) (*models.ConnectedSource, error) {
	if expected == nil {
		return nil, gorm.ErrRecordNotFound
	}
	source, ok := r.sources[expected.ID]
	if !ok ||
		source.OwnerIdentity != ownerIdentity ||
		source.ConnectorKey != expected.ConnectorKey ||
		source.DefaultProjectKey != expected.DefaultProjectKey ||
		!source.UpdatedAt.Equal(expected.UpdatedAt) {
		return nil, gorm.ErrRecordNotFound
	}
	source.Enabled = false
	source.Status = "revoked"
	source.RevokedAt = &revokedAt
	source.UpdatedAt = time.Now().UTC()
	delete(r.oauthTokens, expected.ID)
	delete(r.oauthStates, expected.ID)
	copied := *source
	return &copied, nil
}

func (r *fakeSourceRepo) FindSources(includeDisabled bool) ([]models.ConnectedSource, error) {
	r.globalSourceReads++
	result := []models.ConnectedSource{}
	for _, source := range r.sources {
		if includeDisabled || (source.Enabled && source.Status != "revoked") {
			result = append(result, *source)
		}
	}
	return result, nil
}

func (r *fakeSourceRepo) FindSourcesForOwner(ownerIdentity string, includeDisabled, includeLegacyOwnerless bool) ([]models.ConnectedSource, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	r.ownerSourceReads++
	r.lastSourceOwner = ownerIdentity
	if ownerIdentity == "" {
		return []models.ConnectedSource{}, nil
	}
	result := []models.ConnectedSource{}
	for _, source := range r.sources {
		sourceOwner := strings.TrimSpace(source.OwnerIdentity)
		if sourceOwner != ownerIdentity && !(includeLegacyOwnerless && sourceOwner == "") {
			continue
		}
		if includeDisabled || (source.Enabled && source.Status != "revoked") {
			result = append(result, *source)
		}
	}
	return result, nil
}

func (r *fakeSourceRepo) FindSource(id uuid.UUID) (*models.ConnectedSource, error) {
	source, ok := r.sources[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	copied := *source
	return &copied, nil
}

func (r *fakeSourceRepo) CreateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error) {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	now := time.Now().UTC()
	job.CreatedAt = now
	job.UpdatedAt = now
	r.jobs = append(r.jobs, *job)
	return job, nil
}

func (r *fakeSourceRepo) UpdateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error) {
	job.UpdatedAt = time.Now().UTC()
	for index := range r.jobs {
		if r.jobs[index].ID == job.ID {
			r.jobs[index] = *job
			return job, nil
		}
	}
	r.jobs = append(r.jobs, *job)
	return job, nil
}

func (r *fakeSourceRepo) FindSyncJobs(sourceID *uuid.UUID, limit int) ([]models.SourceSyncJob, error) {
	result := []models.SourceSyncJob{}
	for _, job := range r.jobs {
		if sourceID == nil || job.SourceID == *sourceID {
			result = append(result, job)
			if limit > 0 && len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (r *fakeSourceRepo) FindSyncJobsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceSyncJob, error) {
	allowed := make(map[uuid.UUID]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		allowed[id] = true
	}
	result := []models.SourceSyncJob{}
	for _, job := range r.jobs {
		if allowed[job.SourceID] {
			result = append(result, job)
			if limit > 0 && len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (r *fakeSourceRepo) FindRawItem(sourceID uuid.UUID, externalID string) (*models.SourceRawItem, error) {
	for _, item := range r.rawItems {
		if item.SourceID == sourceID && item.ExternalID == externalID {
			copied := *item
			return &copied, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeSourceRepo) SaveRawItem(item *models.SourceRawItem) (*models.SourceRawItem, error) {
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	r.rawItems[item.ID] = item
	return item, nil
}

func (r *fakeSourceRepo) FindRawItems(sourceID uuid.UUID) ([]models.SourceRawItem, error) {
	result := []models.SourceRawItem{}
	for _, item := range r.rawItems {
		if item.SourceID == sourceID {
			result = append(result, *item)
		}
	}
	return result, nil
}

func (r *fakeSourceRepo) FindExtractionByRawItem(rawItemID uuid.UUID) (*models.SourceExtraction, error) {
	for _, extraction := range r.extractions {
		if extraction.RawItemID == rawItemID {
			copied := *extraction
			return &copied, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeSourceRepo) SaveExtraction(extraction *models.SourceExtraction) (*models.SourceExtraction, error) {
	if extraction.ID == uuid.Nil {
		extraction.ID = uuid.New()
	}
	now := time.Now().UTC()
	if extraction.CreatedAt.IsZero() {
		extraction.CreatedAt = now
	}
	extraction.UpdatedAt = now
	r.extractions[extraction.ID] = extraction
	return extraction, nil
}

func (r *fakeSourceRepo) FindExtractions(projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	return r.findExtractions(nil, projectKey, includeArchived, 0)
}

func (r *fakeSourceRepo) FindExtractionsForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	r.lastExtractionSourceIDs = append([]uuid.UUID{}, sourceIDs...)
	if len(sourceIDs) == 0 {
		return []models.SourceExtraction{}, nil
	}
	allowed := make(map[uuid.UUID]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		allowed[id] = true
	}
	return r.findExtractions(allowed, projectKey, includeArchived, 0)
}

func (r *fakeSourceRepo) FindRecentExtractionsForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, error) {
	r.lastExtractionSourceIDs = append([]uuid.UUID{}, sourceIDs...)
	if len(sourceIDs) == 0 {
		return []models.SourceExtraction{}, nil
	}
	allowed := make(map[uuid.UUID]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		allowed[id] = true
	}
	return r.findExtractions(allowed, projectKey, includeArchived, limit)
}

func (r *fakeSourceRepo) findExtractions(sourceIDs map[uuid.UUID]bool, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, error) {
	result := []models.SourceExtraction{}
	for _, extraction := range r.extractions {
		if sourceIDs != nil && !sourceIDs[extraction.SourceID] {
			continue
		}
		if projectKey != "" && extraction.ProjectKey != projectKey {
			continue
		}
		if !includeArchived && extraction.Archived {
			continue
		}
		result = append(result, *extraction)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (r *fakeSourceRepo) FindExtraction(id uuid.UUID) (*models.SourceExtraction, error) {
	extraction, ok := r.extractions[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	copied := *extraction
	return &copied, nil
}

func (r *fakeSourceRepo) FindExtractionForOwner(id uuid.UUID, ownerIdentity string) (*models.SourceExtraction, error) {
	extraction, err := r.FindExtraction(id)
	if err != nil {
		return nil, err
	}
	source, err := r.FindSource(extraction.SourceID)
	if err != nil || strings.TrimSpace(source.OwnerIdentity) != strings.TrimSpace(ownerIdentity) || strings.TrimSpace(ownerIdentity) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	return extraction, nil
}

func (r *fakeSourceRepo) DeleteExtractionForOwner(
	expected *models.SourceExtraction,
	expectedSource *models.ConnectedSource,
	ownerIdentity string,
) error {
	if r.deleteExtractionErr != nil {
		return r.deleteExtractionErr
	}
	if expected == nil || expectedSource == nil ||
		expected.SourceID != expectedSource.ID {
		return gorm.ErrRecordNotFound
	}
	source, ok := r.sources[expectedSource.ID]
	if !ok ||
		source.OwnerIdentity != ownerIdentity ||
		source.ConnectorKey != expectedSource.ConnectorKey ||
		!source.UpdatedAt.Equal(expectedSource.UpdatedAt) {
		return gorm.ErrRecordNotFound
	}
	extraction, ok := r.extractions[expected.ID]
	if !ok ||
		extraction.SourceID != expected.SourceID ||
		extraction.ProjectKey != expected.ProjectKey ||
		extraction.RawItemID != expected.RawItemID ||
		extraction.ContentHash != expected.ContentHash ||
		extraction.SourceURI != expected.SourceURI ||
		!extraction.UpdatedAt.Equal(expected.UpdatedAt) {
		return gorm.ErrRecordNotFound
	}
	delete(r.extractions, expected.ID)
	filtered := r.index[:0]
	for _, entry := range r.index {
		if entry.ExtractionID != expected.ID {
			filtered = append(filtered, entry)
		}
	}
	r.index = filtered
	return nil
}

func (r *fakeSourceRepo) SaveIndexEntry(entry *models.SourceIndexEntry) (*models.SourceIndexEntry, error) {
	for index := range r.index {
		if r.index[index].ExtractionID == entry.ExtractionID && r.index[index].IndexType == entry.IndexType {
			entry.ID = r.index[index].ID
			entry.CreatedAt = r.index[index].CreatedAt
			entry.UpdatedAt = time.Now().UTC()
			r.index[index] = *entry
			return entry, nil
		}
	}
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	r.index = append(r.index, *entry)
	return entry, nil
}

func (r *fakeSourceRepo) DeletePendingVectorIndex(extractionID uuid.UUID) error {
	filtered := r.index[:0]
	for _, entry := range r.index {
		if entry.ExtractionID == extractionID && entry.IndexType == "vector_ref" && strings.HasPrefix(entry.VectorRef, "local-vector-pending:") {
			continue
		}
		filtered = append(filtered, entry)
	}
	r.index = filtered
	return nil
}

func (r *fakeSourceRepo) SaveAuditLog(log *models.SourceAuditLog) (*models.SourceAuditLog, error) {
	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}
	log.CreatedAt = time.Now().UTC()
	r.auditLogs = append(r.auditLogs, *log)
	return log, nil
}

func (r *fakeSourceRepo) FindAuditLogs(sourceID *uuid.UUID, limit int) ([]models.SourceAuditLog, error) {
	result := []models.SourceAuditLog{}
	for _, log := range r.auditLogs {
		if sourceID == nil || log.SourceID == *sourceID {
			result = append(result, log)
			if limit > 0 && len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (r *fakeSourceRepo) FindAuditLogsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceAuditLog, error) {
	allowed := make(map[uuid.UUID]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		allowed[id] = true
	}
	result := []models.SourceAuditLog{}
	for _, log := range r.auditLogs {
		if allowed[log.SourceID] {
			result = append(result, log)
			if limit > 0 && len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (r *fakeSourceRepo) FindOverviewForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) (*SourceOverview, error) {
	overview := emptySourceOverview()
	allowed := make(map[uuid.UUID]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		allowed[id] = true
	}
	for _, extraction := range r.extractions {
		if !allowed[extraction.SourceID] || (projectKey != "" && extraction.ProjectKey != projectKey) || (!includeArchived && extraction.Archived) {
			continue
		}
		overview.ExtractionCount++
		overview.ExtractionCountsBySource[extraction.SourceID.String()]++
		if extraction.Sensitive {
			overview.SensitiveExtractionCount++
		}
		if extraction.Uncertain {
			overview.UncertainExtractionCount++
		}
	}
	latestCreatedAt := map[uuid.UUID]time.Time{}
	for _, job := range r.jobs {
		if !allowed[job.SourceID] {
			continue
		}
		switch job.Status {
		case "failed", "partial_failure":
			overview.FailedJobs++
		case "pending", "running":
			overview.PendingJobs++
		}
		if current, exists := latestCreatedAt[job.SourceID]; !exists || job.CreatedAt.After(current) {
			latestCreatedAt[job.SourceID] = job.CreatedAt
			overview.LatestJobStatusBySource[job.SourceID.String()] = job.Status
		}
	}
	return overview, nil
}

func (r *fakeSourceRepo) hasAudit(action string) bool {
	for _, log := range r.auditLogs {
		if log.Action == action {
			return true
		}
	}
	return false
}

type fakeSourceMemoryService struct {
	created      []memory.CreateRequest
	ownerCreated []ownerMemoryCreate
}

var _ memory.OwnerScopedService = (*fakeSourceMemoryService)(nil)

type ownerMemoryCreate struct {
	ownerIdentity string
	request       memory.CreateRequest
}

func (s *fakeSourceMemoryService) Create(request memory.CreateRequest) (*models.ContextMemory, error) {
	s.created = append(s.created, request)
	return &models.ContextMemory{
		ID:          uuid.New(),
		ProjectKey:  request.ProjectKey,
		Kind:        request.Kind,
		Content:     request.Content,
		Summary:     request.Summary,
		Confidence:  request.Confidence,
		SourceURI:   request.SourceURI,
		SourceLabel: request.SourceLabel,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}, nil
}

func (s *fakeSourceMemoryService) CreateForOwner(ownerIdentity string, request memory.CreateRequest) (*models.ContextMemory, error) {
	s.ownerCreated = append(s.ownerCreated, ownerMemoryCreate{ownerIdentity: ownerIdentity, request: request})
	return &models.ContextMemory{
		ID:            uuid.New(),
		OwnerIdentity: ownerIdentity,
		ProjectKey:    request.ProjectKey,
		Kind:          request.Kind,
		Content:       request.Content,
		Summary:       request.Summary,
		Confidence:    request.Confidence,
		SourceURI:     request.SourceURI,
		SourceLabel:   request.SourceLabel,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}, nil
}

func (s *fakeSourceMemoryService) Update(id uuid.UUID, request memory.UpdateRequest) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeSourceMemoryService) UpdateForOwner(ownerIdentity string, id uuid.UUID, request memory.UpdateRequest) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeSourceMemoryService) FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeSourceMemoryService) FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeSourceMemoryService) FindByID(id uuid.UUID) (*models.ContextMemory, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *fakeSourceMemoryService) FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *fakeSourceMemoryService) Archive(id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeSourceMemoryService) ArchiveForOwner(ownerIdentity string, id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeSourceMemoryService) Delete(id uuid.UUID) error {
	return nil
}

func (s *fakeSourceMemoryService) DeleteForOwner(ownerIdentity string, id uuid.UUID) error {
	return nil
}

func (s *fakeSourceMemoryService) Retrieve(request memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	return &memory.RetrieveResult{Query: request.Query}, nil
}

func (s *fakeSourceMemoryService) RetrieveForOwner(ownerIdentity string, request memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	return &memory.RetrieveResult{}, nil
}

type fakeSourceWorkflowService struct {
	requests      []workflow.IntakeRequest
	retractions   []sourceWorkflowRetraction
	intakeErr     error
	intakeStarted chan struct{}
	intakeRelease chan struct{}
}

type sourceWorkflowRetraction struct {
	sourceType string
	sourceID   string
	reason     string
}

func (s *fakeSourceWorkflowService) Intake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	s.requests = append(s.requests, request)
	if s.intakeStarted != nil {
		close(s.intakeStarted)
		s.intakeStarted = nil
	}
	if s.intakeRelease != nil {
		<-s.intakeRelease
	}
	if s.intakeErr != nil {
		return nil, s.intakeErr
	}
	return &workflow.WorkflowRecord{
		Item: models.WorkflowItem{ID: uuid.New(), Title: request.Input, ProjectKey: request.ProjectKey},
	}, nil
}

func (s *fakeSourceWorkflowService) Items(includeArchived bool) ([]models.WorkflowItem, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) ItemsForOwner(ownerIdentity string, includeArchived bool) ([]models.WorkflowItem, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) ApprovalItems() ([]models.WorkflowItem, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) ApprovalItemsForOwner(ownerIdentity string) ([]models.WorkflowItem, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) Dashboard() (*workflow.WorkflowDashboard, error) {
	return &workflow.WorkflowDashboard{}, nil
}

func (s *fakeSourceWorkflowService) DashboardForOwner(ownerIdentity string) (*workflow.WorkflowDashboard, error) {
	return &workflow.WorkflowDashboard{}, nil
}

func (s *fakeSourceWorkflowService) Get(id uuid.UUID) (*workflow.WorkflowRecord, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *fakeSourceWorkflowService) GetForOwner(ownerIdentity string, id uuid.UUID) (*workflow.WorkflowRecord, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *fakeSourceWorkflowService) Transition(id uuid.UUID, request workflow.TransitionRequest) (*workflow.WorkflowRecord, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) ResolveApproval(id uuid.UUID, request workflow.ApprovalResolutionRequest) (*workflow.WorkflowRecord, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) ResolveInterruptedExecution(id uuid.UUID, request workflow.InterruptedExecutionResolutionRequest) (*workflow.WorkflowRecord, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) ResolveProposal(id uuid.UUID, proposalID uuid.UUID, request workflow.ProposalResolutionRequest) (*workflow.WorkflowRecord, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) UpdateChecklistItem(id uuid.UUID, itemID uuid.UUID, request workflow.ChecklistUpdateRequest) (*workflow.WorkflowRecord, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) RetractSource(sourceType, sourceID, reason string) error {
	s.retractions = append(s.retractions, sourceWorkflowRetraction{sourceType: sourceType, sourceID: sourceID, reason: reason})
	return nil
}

type fakeSourcePursuitLinker struct {
	requests       []pursuit.AutoLinkWorkflowRequest
	memoryRequests []pursuit.AutoLinkMemoryRequest
	result         *pursuit.AutoLinkResult
	memoryResult   *pursuit.AutoLinkResult
	err            error
}

type fakeSourcePursuitGateway struct {
	fakeSourcePursuitLinker
	routed    []workflow.IntakeRequest
	err       error
	pursuitID uuid.UUID
}

func (f *fakeSourcePursuitGateway) RouteWorkflowIntake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	f.routed = append(f.routed, request)
	if f.err != nil {
		return nil, f.err
	}
	if f.pursuitID == uuid.Nil {
		f.pursuitID = uuid.New()
	}
	return &workflow.WorkflowRecord{
		Item: models.WorkflowItem{ID: uuid.New(), Title: request.Input, ProjectKey: request.ProjectKey},
		Pursuits: []workflow.WorkflowPursuitContext{{
			ID: f.pursuitID,
		}},
	}, nil
}

func (f *fakeSourcePursuitLinker) AutoLinkWorkflow(request pursuit.AutoLinkWorkflowRequest) (*pursuit.AutoLinkResult, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &pursuit.AutoLinkResult{Linked: true, PursuitID: uuid.New(), Score: 0.8}, nil
}

func (f *fakeSourcePursuitLinker) AutoLinkMemory(request pursuit.AutoLinkMemoryRequest) (*pursuit.AutoLinkResult, error) {
	f.memoryRequests = append(f.memoryRequests, request)
	if f.err != nil {
		return nil, f.err
	}
	if f.memoryResult != nil {
		return f.memoryResult, nil
	}
	return &pursuit.AutoLinkResult{Linked: true, PursuitID: uuid.New(), Score: 0.8}, nil
}

func hasString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func (s *fakeSourceWorkflowService) RecoverStaleClaims(request workflow.RunDueRequest) (*workflow.ClaimRecoverySummary, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) RecoverStaleClaimsForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.ClaimRecoverySummary, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) RunDue(request workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) RunDueForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.WorkflowRunSummary, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) RunOneForOwner(ownerIdentity string, id uuid.UUID) (*workflow.WorkflowRunResult, error) {
	return &workflow.WorkflowRunResult{WorkflowID: id, Status: "skipped"}, nil
}

func (s *fakeSourceWorkflowService) RunDueOpenLoops(request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) RunDueOpenLoopsForOwner(ownerIdentity string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	return nil, nil
}

func (s *fakeSourceWorkflowService) Overview() workflow.Overview {
	return workflow.Overview{}
}
