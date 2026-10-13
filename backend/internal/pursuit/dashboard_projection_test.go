package pursuit

import (
	"automation-hub-backend/internal/models"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDashboardProjectionColumnsExcludeLargePayloads(t *testing.T) {
	tests := []struct {
		name      string
		columns   []string
		required  []string
		forbidden []string
	}{
		{name: "workflow evidence", columns: dashboardWorkflowEvidenceColumns, required: []string{"id", "workflow_id"}, forbidden: []string{"claim_text", "source_uri", "source_label"}},
		{name: "memory", columns: dashboardMemoryColumns, required: []string{"id"}, forbidden: []string{"content", "summary", "tags", "source_uri", "content_hash"}},
		{name: "source item", columns: pursuitSourceItemProjectionColumns, required: []string{"id", "source_id", "metadata", "updated_at"}, forbidden: []string{"content", "content_hash"}},
		{name: "source extraction", columns: dashboardSourceExtractionColumns, required: []string{"id", "summary", "archived", "updated_at"}, forbidden: []string{"text", "entities", "dates", "tasks", "decisions", "follow_ups", "content_hash"}},
		{name: "verification claim", columns: dashboardVerificationClaimColumns, required: []string{"id", "run_id", "status"}, forbidden: []string{"claim_text", "source_refs", "support_explanation"}},
		{name: "verification evidence", columns: dashboardVerificationEvidenceColumns, required: []string{"id", "run_id", "source_type"}, forbidden: []string{"snippet", "source_label", "authority", "freshness", "reject_reason"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected := map[string]bool{}
			for _, column := range test.columns {
				selected[strings.ToLower(strings.TrimSpace(column))] = true
			}
			for _, column := range test.required {
				if !selected[column] {
					t.Fatalf("required projection column %q is missing from %v", column, test.columns)
				}
			}
			for _, column := range test.forbidden {
				if selected[column] {
					t.Fatalf("large payload column %q is present in %v", column, test.columns)
				}
			}
		})
	}
}

func TestPartitionRecordsPreservesOrderAndDeduplicatesOwners(t *testing.T) {
	firstPursuitID, secondPursuitID := uuid.New(), uuid.New()
	firstRecordID, secondRecordID := uuid.New(), uuid.New()
	links := []models.PursuitLink{
		{PursuitID: firstPursuitID, LinkType: LinkWorkflow, LinkID: firstRecordID.String()},
		{PursuitID: firstPursuitID, LinkType: LinkWorkflow, LinkID: firstRecordID.String()},
		{PursuitID: secondPursuitID, LinkType: LinkWorkflow, LinkID: firstRecordID.String()},
		{PursuitID: firstPursuitID, LinkType: LinkWorkflow, LinkID: secondRecordID.String()},
		{PursuitID: firstPursuitID, LinkType: LinkMemory, LinkID: uuid.NewString()},
		{PursuitID: firstPursuitID, LinkType: LinkWorkflow, LinkID: "not-a-uuid"},
	}
	owners := ownersFromLinks(links, LinkWorkflow)
	if got := owners[firstRecordID]; len(got) != 2 || got[0] != firstPursuitID || got[1] != secondPursuitID {
		t.Fatalf("first record owners = %v, want both pursuits once in link order", got)
	}

	items := []models.WorkflowItem{{ID: secondRecordID, Title: "second"}, {ID: firstRecordID, Title: "first"}}
	partitioned := partitionRecords(items, owners, func(item models.WorkflowItem) uuid.UUID { return item.ID })
	first := recordsFor(partitioned, firstPursuitID)
	if len(first) != 2 || first[0].ID != secondRecordID || first[1].ID != firstRecordID {
		t.Fatalf("first pursuit records = %v, want global record order without duplicates", workflowRecordIDs(first))
	}
	second := recordsFor(partitioned, secondPursuitID)
	if len(second) != 1 || second[0].ID != firstRecordID {
		t.Fatalf("second pursuit records = %v, want shared first record", workflowRecordIDs(second))
	}
	if missing := recordsFor(partitioned, uuid.New()); missing == nil || len(missing) != 0 {
		t.Fatalf("missing partition = %#v, want non-nil empty slice", missing)
	}
}

func TestPartitionRuntimeAttemptsDeduplicatesDualOwnershipAndAppliesPerPursuitLimit(t *testing.T) {
	firstPursuitID, secondPursuitID := uuid.New(), uuid.New()
	automationID := uuid.New()
	items := make([]models.AutomationLaunchEvent, 25)
	for index := range items {
		items[index] = models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: automationID}
	}
	automationOwners := recordOwners{automationID: {firstPursuitID}}
	launchOwners := recordOwners{items[0].ID: {firstPursuitID, secondPursuitID}}

	partitioned := partitionRuntimeAttempts(items, automationOwners, launchOwners, 20)
	first := recordsFor(partitioned, firstPursuitID)
	if len(first) != 20 {
		t.Fatalf("first pursuit attempts = %d, want per-pursuit limit 20", len(first))
	}
	if first[0].ID != items[0].ID || first[19].ID != items[19].ID {
		t.Fatalf("first pursuit attempt order changed")
	}
	second := recordsFor(partitioned, secondPursuitID)
	if len(second) != 1 || second[0].ID != items[0].ID {
		t.Fatalf("second pursuit attempts = %#v, want explicitly linked launch once", second)
	}
}

func workflowRecordIDs(items []models.WorkflowItem) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		result = append(result, item.ID)
	}
	return result
}

var projectionBenchmarkSink map[uuid.UUID][]models.WorkflowItem

func BenchmarkDashboardPartitionIndexed(b *testing.B) {
	items, owners, pursuitIDs := projectionBenchmarkFixture(100, 500)
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		partitioned := partitionRecords(items, owners, func(item models.WorkflowItem) uuid.UUID { return item.ID })
		for _, pursuitID := range pursuitIDs {
			_ = recordsFor(partitioned, pursuitID)
		}
		projectionBenchmarkSink = partitioned
	}
}

func BenchmarkDashboardPartitionRepeatedScan(b *testing.B) {
	items, owners, pursuitIDs := projectionBenchmarkFixture(100, 500)
	recordIDsByPursuit := map[uuid.UUID][]uuid.UUID{}
	for _, item := range items {
		for _, pursuitID := range owners[item.ID] {
			recordIDsByPursuit[pursuitID] = append(recordIDsByPursuit[pursuitID], item.ID)
		}
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result := map[uuid.UUID][]models.WorkflowItem{}
		for _, pursuitID := range pursuitIDs {
			result[pursuitID] = benchmarkFilterWorkflowRecords(items, recordIDsByPursuit[pursuitID])
		}
		projectionBenchmarkSink = result
	}
}

func benchmarkFilterWorkflowRecords(items []models.WorkflowItem, ids []uuid.UUID) []models.WorkflowItem {
	wanted := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	result := []models.WorkflowItem{}
	for _, item := range items {
		if wanted[item.ID] {
			result = append(result, item)
		}
	}
	return result
}

func projectionBenchmarkFixture(pursuitCount, recordCount int) ([]models.WorkflowItem, recordOwners, []uuid.UUID) {
	pursuitIDs := make([]uuid.UUID, pursuitCount)
	for index := range pursuitIDs {
		pursuitIDs[index] = uuid.New()
	}
	items := make([]models.WorkflowItem, recordCount)
	owners := recordOwners{}
	for index := range items {
		items[index].ID = uuid.New()
		owners[items[index].ID] = []uuid.UUID{pursuitIDs[index%pursuitCount]}
	}
	return items, owners, pursuitIDs
}
