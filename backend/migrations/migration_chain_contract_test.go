package migrations

import (
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var migrationFilePattern = regexp.MustCompile(
	`^(\d{4})_([a-z0-9]+(?:_[a-z0-9]+)*)\.(up|down)\.sql$`,
)

func TestMigrationChainHasUniqueOrderedPairedVersions(t *testing.T) {
	for _, phase := range []string{"pre", "post"} {
		t.Run(phase, func(t *testing.T) {
			entries, err := fs.ReadDir(Files, phase)
			if err != nil {
				t.Fatalf("read %s migrations: %v", phase, err)
			}

			type pair struct {
				name string
				up   bool
				down bool
			}
			byVersion := make(map[int]*pair)
			for _, entry := range entries {
				if entry.IsDir() {
					t.Fatalf("migration directory %s contains nested directory %q", phase, entry.Name())
				}
				matches := migrationFilePattern.FindStringSubmatch(entry.Name())
				if matches == nil {
					t.Fatalf("migration %s/%s does not follow NNNN_name.(up|down).sql", phase, entry.Name())
				}
				version, err := strconv.Atoi(matches[1])
				if err != nil {
					t.Fatalf("parse migration version %q: %v", matches[1], err)
				}
				current := byVersion[version]
				if current == nil {
					current = &pair{name: matches[2]}
					byVersion[version] = current
				} else if current.name != matches[2] {
					t.Fatalf(
						"migration version %04d collides between %q and %q",
						version,
						current.name,
						matches[2],
					)
				}
				if matches[3] == "up" {
					if current.up {
						t.Fatalf("migration %04d_%s has duplicate up files", version, current.name)
					}
					current.up = true
				} else {
					if current.down {
						t.Fatalf("migration %04d_%s has duplicate down files", version, current.name)
					}
					current.down = true
				}
			}

			versions := make([]int, 0, len(byVersion))
			for version, migration := range byVersion {
				if !migration.up || !migration.down {
					t.Errorf(
						"migration %s/%04d_%s must have both up and down files",
						phase,
						version,
						migration.name,
					)
				}
				versions = append(versions, version)
			}
			sort.Ints(versions)
			for index, version := range versions {
				want := index + 1
				if version != want {
					t.Errorf("%s migration sequence has version %04d at position %d, want %04d", phase, version, index, want)
				}
			}
		})
	}
}

func TestGovernanceMigrationTailPreservesSemanticUpgradeOrder(t *testing.T) {
	want := []string{
		"0014_unified_execution_authorization",
		"0015_controlled_learning",
		"0016_evidence_packs",
		"0017_controlled_learning_application",
		"0018_agent_team_lifecycle",
		"0019_life_ontology",
		"0020_proactivity_advisory",
		"0021_operational_life_graph",
		"0022_life_ontology_timestamp_precision",
		"0023_outcome_resilience_ledgers",
		"0024_execution_authorization_schema_compatibility",
		"0025_execution_authorization_life_domain",
		"0026_life_commitment_cost_ledgers",
		"0027_contact_review_decisions",
		"0028_automation_approval_proof_consumptions",
		"0029_framework_selector_v5_digest",
		"0030_framework_evidence_preflights",
		"0031_model_outcome_calibration",
		"0032_pursuit_workflow_standing_mandate_binding",
		"0033_pursuit_goal_contract",
		"0034_pursuit_resource_ledger",
		"0035_pursuit_resource_reservations",
		"0036_pursuit_resource_reservation_reconciliation",
		"0037_task_operation_identity",
		"0038_pursuit_portfolio_allocations",
		"0039_pursuit_portfolio_execution_proposals",
		"0040_pursuit_portfolio_execution_proposal_decisions",
		"0041_execution_authorization_portfolio_approval",
		"0042_workflow_active_source_idempotency",
		"0043_pursuit_activity_idempotency",
		"0044_workflow_completion_settlement_proofs",
		"0045_pursuit_portfolio_dispatch_coordination",
		"0046_workflow_reminder_activation_ledger",
		"0047_workflow_reminder_activation_decision_order",
		"0048_proactivity_attention_feedback",
		"0049_outcome_attention_monitor",
		"0050_outcome_monitor_composition_delivery",
		"0051_outcome_monitor_composition_snapshot",
		"0052_plan_graph_contract",
		"0053_workflow_coordination_plan_binding",
		"0054_pursuit_portfolio_coordination_plan_binding",
		"0055_workflow_reminder_delivery_ledger",
		"0056_workflow_reminder_delivery_dead_letter",
		"0057_workflow_reminder_single_delivery_authorization",
		"0058_workflow_coordination_draft_binding",
		"0059_agent_team_message_acknowledgments",
		"0060_connected_source_scheduler_index",
		"0061_connected_source_owner_activity_indexes",
		"0062_context_memory_owner_query_indexes",
		"0063_verification_owner_run_indexes",
		"0064_connected_source_history_query_indexes",
		"0065_host_runtime_jobs",
		"0066_automation_launch_execution_reference",
		"0067_automation_launch_event_idempotency",
		"0068_execution_authorization_owner_control_approval",
		"0069_openclaw_gateway_session_receipts",
		"0070_openclaw_gateway_session_review",
		"0071_openclaw_gateway_artifact_receipts",
		"0072_openclaw_maintenance",
		"0073_automation_runtime_model",
		"0074_openclaw_session_instance",
		"0075_openclaw_receipt_table_alignment",
		"0076_openclaw_usage_recovery",
		"0077_openclaw_usage_snapshot",
		"0078_openclaw_artifact_alignment",
		"0079_openclaw_artifact_recovery",
		"0080_openclaw_artifact_retention",
		"0081_openclaw_gateway_admission_intents",
		"0082_openclaw_gateway_cancellation_intents",
		"0083_openclaw_maintenance_retry_budget",
		"0084_brain_skill_selection_events",
		"0085_automation_launch_outcome_approval",
		"0086_host_runtime_job_expiry_review",
		"0087_source_manual_sync_jobs",
		"0088_source_extraction_correction_outbox",
		"0089_openclaw_reconcile_fairness_and_cancellation_retry",
		"0090_context_memory_source_extraction_identity",
		"0091_openclaw_artifact_retention_key_check",
		"0092_host_runtime_execution_confirmation_fence",
		"0093_openclaw_artifact_owner_binding",
		"0094_host_runtime_cancellation_request",
		"0095_trello_resumable_sync",
		"0096_llm_model_maintenance_statuses",
		"0097_execution_authorization_task_review_claim",
		"0098_openclaw_artifact_claim_leases",
		"0099_execution_authorization_workflow_decision_claim",
		"0100_host_runtime_stop_revision",
		"0101_llm_model_maintenance_admission_claims",
		"0102_model_telemetry_usage_source",
		"0103_trello_signed_webhook_intake",
		"0104_source_sync_cursor_storage",
		"0105_source_sync_idempotency_nonempty",
		"0106_brain_skill_selection_catalog_fingerprint",
		"0107_workflow_reminder_delivery_expired_status",
		"0108_host_runtime_start_intent",
		"0109_trello_webhook_reconciliation_generations",
		"0110_account_feed_registry",
		"0111_operation_source_identity",
		"0112_operation_source_observation",
		"0113_operation_source_heads",
		"0114_operation_source_configuration",
		"0115_durable_job_replay_policy",
		"0116_workflow_success_criteria",
		"0117_framework_preference_change_history",
		"0118_operation_source_evidence_raw_digest",
	}
	entries, err := fs.ReadDir(Files, "pre")
	if err != nil {
		t.Fatalf("read pre migrations: %v", err)
	}

	actual := make([]string, 0, len(want))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".up.sql")
		if base >= want[0] {
			actual = append(actual, base)
		}
	}
	sort.Strings(actual)
	if strings.Join(actual, "\n") != strings.Join(want, "\n") {
		t.Fatalf("governance migration tail = %v, want %v", actual, want)
	}

	for _, base := range want {
		downBytes, err := Files.ReadFile("pre/" + base + ".down.sql")
		if err != nil {
			t.Fatalf("read rollback for %s: %v", base, err)
		}
		down := migrationSQLWithoutLineComments(string(downBytes))
		if down == "" {
			t.Errorf("rollback for %s is empty", base)
		}
		if strings.Contains(strings.ToUpper(down), " CASCADE") {
			t.Errorf("rollback for %s must not use CASCADE", base)
		}
	}
}

// Source lint only, not a PostgreSQL parser or execution proof. Full-line
// documentation must not be mistaken for executable rollback clauses.
func migrationSQLWithoutLineComments(sql string) string {
	var lines []string
	for _, line := range strings.Split(sql, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func TestMigrationTailRollbackLintIgnoresDocumentationButRetainsClauses(t *testing.T) {
	for _, test := range []struct {
		name, sql string
		cascade   bool
	}{
		{"warning only", "-- Do not use CASCADE.\r\nDROP TABLE example;", false},
		{"indented warning", "  -- Do not use CASCADE.\nDROP TABLE example;", false},
		{"clause after warning", "-- Do not use CASCADE.\nDROP TABLE example CASCADE;", true},
		{"lowercase clause", "drop table example cascade;", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := strings.Contains(strings.ToUpper(migrationSQLWithoutLineComments(test.sql)), " CASCADE"); got != test.cascade {
				t.Fatalf("cascade lint=%v, want %v", got, test.cascade)
			}
		})
	}
	if migrationSQLWithoutLineComments("-- documentation only\n") != "" {
		t.Fatal("documentation alone must not make a rollback nonempty")
	}
}

func TestTaskReviewApprovalClaimMigrationPreservesConsumedHistory(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0097_execution_authorization_task_review_claim.up.sql")
	if err != nil {
		t.Fatalf("read task-review claim migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0097_execution_authorization_task_review_claim.down.sql")
	if err != nil {
		t.Fatalf("read task-review claim rollback: %v", err)
	}
	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))
	for _, fragment := range []string{
		"create table public.execution_authorization_task_review_claims",
		"insert into public.execution_authorization_task_review_claims",
		"join public.execution_authorization_receipts as receipts",
		"having count(*) > 1",
		"raise exception",
		"references public.task_review_decisions (owner_identity, id)",
		"references public.execution_authorization_consumptions (owner_identity, receipt_id)",
		"unique (owner_identity, task_review_decision_id)",
		"before update or delete",
		"create trigger trg_execution_authorization_consumptions_claim_task_review",
		"after insert",
		"hai_claim_execution_authorization_task_review_consumption",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("upgrade migration is missing %q", fragment)
		}
	}
	if strings.Contains(up, "update public.execution_authorization_consumptions") {
		t.Error("upgrade migration must preserve the append-only consumption ledger")
	}
	for _, fragment := range []string{
		"from public.execution_authorization_task_review_claims",
		"raise exception",
		"drop trigger if exists trg_execution_authorization_consumptions_claim_task_review",
		"drop function if exists public.hai_claim_execution_authorization_task_review_consumption()",
		"drop table public.execution_authorization_task_review_claims",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("rollback migration is missing %q", fragment)
		}
	}
}

func TestOpenClawArtifactClaimLeaseMigrationAddsPairedFence(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0098_openclaw_artifact_claim_leases.up.sql")
	if err != nil {
		t.Fatalf("read OpenClaw artifact claim migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0098_openclaw_artifact_claim_leases.down.sql")
	if err != nil {
		t.Fatalf("read OpenClaw artifact claim rollback: %v", err)
	}
	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))
	for _, fragment := range []string{
		"add column if not exists claimed_at timestamptz",
		"add column if not exists claim_token uuid",
		"check ((claimed_at is null) = (claim_token is null))",
		"create index if not exists idx_openclaw_artifact_collections_claimed",
		"create unique index if not exists idx_openclaw_artifact_collections_claim_token",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("artifact claim migration is missing %q", fragment)
		}
	}
	if strings.Contains(strings.ToUpper(up), "CASCADE") || strings.Contains(strings.ToUpper(down), "CASCADE") {
		t.Error("artifact claim migration must not cascade through related data")
	}
	for _, fragment := range []string{"drop constraint if exists chk_openclaw_artifact_claim_pair", "drop column if exists claim_token", "drop column if exists claimed_at"} {
		if !strings.Contains(down, fragment) {
			t.Errorf("artifact claim rollback is missing %q", fragment)
		}
	}
}
