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
		"0060_opscontrol_approval_provenance",
		"0061_context_memory_owner_query_indexes",
		"0062_ambient_need_defaults",
		"0063_life_ontology_bounded_query_indexes",
		"0064_task_completion_history_projection",
		"0065_source_oauth_state",
		"0066_workflow_rule_defaults",
		"0067_task_operation_cancellation",
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
		down := strings.TrimSpace(string(downBytes))
		if down == "" {
			t.Errorf("rollback for %s is empty", base)
		}
		if strings.Contains(strings.ToUpper(down), " CASCADE") {
			t.Errorf("rollback for %s must not use CASCADE", base)
		}
	}
}
