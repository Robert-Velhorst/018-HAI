package migrations

import (
	"strings"
	"testing"
)

func TestControlledLearningApplicationMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0017_controlled_learning_application.up.sql")
	if err != nil {
		t.Fatalf("read controlled learning application up migration: %v", err)
	}
	downBytes, err := Files.ReadFile("pre/0017_controlled_learning_application.down.sql")
	if err != nil {
		t.Fatalf("read controlled learning application down migration: %v", err)
	}

	up := normalizeMigrationLineEndings(string(upBytes))
	for _, fragment := range []string{
		"CREATE TABLE public.controlled_learning_applications",
		"CREATE TABLE public.controlled_learning_application_events",
		"UNIQUE (owner_identity, idempotency_key)",
		"UNIQUE (\n            owner_identity,\n            proposal_id,\n            proposal_revision,\n            application_mode",
		"application_status IN (",
		"'rollback_applying'",
		"'rolled_back'",
		"'rollback_failed'",
		"ADD COLUMN application_id uuid",
		"decision_kind IN ('approve', 'escalate_governance')",
		"application.application_status = 'applied'",
		"application.application_status = 'handoff_ready'",
		"DEFERRABLE INITIALLY DEFERRED",
		"trg_controlled_learning_proposals_require_application",
		"trg_controlled_learning_application_events_immutable",
		"trg_controlled_learning_applications_guard_update",
		"controlled learning has legacy applied labels without application evidence",
		"controlled learning approval requires a completed application or protected handoff",
		"NEW.payload #>> '{rollbackPlan}'",
		"NEW.payload #>> '{governanceReference}'",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("up migration is missing contract fragment %q", fragment)
		}
	}
	requireMigrationOrder(t, up,
		"controlled learning has legacy applied labels without application evidence",
		"CREATE TABLE public.controlled_learning_applications",
		"CREATE TABLE public.controlled_learning_application_events",
		"ADD COLUMN application_id uuid",
		"CREATE CONSTRAINT TRIGGER trg_controlled_learning_proposals_require_application",
		"CREATE TRIGGER trg_controlled_learning_application_events_immutable",
		"CREATE TRIGGER trg_controlled_learning_applications_guard_update",
	)

	down := normalizeMigrationLineEndings(string(downBytes))
	for _, fragment := range []string{
		"DROP TRIGGER IF EXISTS trg_controlled_learning_applications_no_truncate",
		"DROP TRIGGER IF EXISTS trg_controlled_learning_application_events_immutable",
		"DROP TRIGGER IF EXISTS trg_controlled_learning_proposals_require_application",
		"DROP FUNCTION IF EXISTS public.hai_guard_controlled_learning_application_definition()",
		"DROP FUNCTION IF EXISTS public.hai_require_controlled_learning_application()",
		"DROP COLUMN IF EXISTS application_id",
		"DROP TABLE IF EXISTS public.controlled_learning_application_events",
		"DROP TABLE IF EXISTS public.controlled_learning_applications",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("down migration is missing contract fragment %q", fragment)
		}
	}
	requireMigrationOrder(t, down,
		"DROP TRIGGER IF EXISTS trg_controlled_learning_applications_no_truncate",
		"DROP TRIGGER IF EXISTS trg_controlled_learning_proposals_require_application",
		"DROP FUNCTION IF EXISTS public.hai_guard_controlled_learning_application_definition()",
		"DROP FUNCTION IF EXISTS public.hai_require_controlled_learning_application()",
		"DROP CONSTRAINT IF EXISTS chk_controlled_learning_review_decision_application",
		"DROP CONSTRAINT IF EXISTS fk_controlled_learning_review_decision_application",
		"DROP COLUMN IF EXISTS application_id",
		"DROP TABLE IF EXISTS public.controlled_learning_application_events",
		"DROP TABLE IF EXISTS public.controlled_learning_applications",
	)
}
