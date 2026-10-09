package migrations_test

import (
	"testing"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/verification"
)

func TestVerificationCompletionAuditIsAtomicWithFinalizationPostgres(t *testing.T) {
	t.Setenv("DB_AUTOMIGRATE", "false")
	t.Setenv("HAI_SEMANTIC_RETRIEVAL_ENABLED", "false")
	db := openIsolatedMigrationDatabase(t)
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("apply the production migration sequence: %v", err)
	}

	service := verification.NewService(verification.NewGormRepository(db), nil, nil)
	request := verification.AnswerRequest{
		OwnerIdentity: "postgres-audit-owner",
		Question:      "What does the record establish?",
		DraftAnswer:   "The record establishes that the work passed.",
		Mode:          verification.ModeGrounded,
		ExternalEvidence: []verification.EvidenceInput{{
			SourceType: "project_record",
			SourceID:   "record-success",
			SourceURI:  "local://verification/record-success",
			Snippet:    "The record establishes that the work passed.",
		}},
	}
	success, err := service.Answer(request)
	if err != nil {
		t.Fatalf("persist successful verification: %v", err)
	}
	var completedCount int64
	if err := db.Model(&models.VerificationAuditLog{}).
		Where("run_id = ? AND action = ?", success.Run.ID, "verification.completed").Count(&completedCount).Error; err != nil {
		t.Fatalf("count completion audit: %v", err)
	}
	if completedCount != 1 {
		t.Fatalf("completion audit count = %d, want 1", completedCount)
	}
	if _, err := service.RunDetailsForOwner("different-owner", success.Run.ID); err == nil {
		t.Fatal("the production repository returned a verification run to a different owner")
	}
	if _, err := service.RunDetailsForOwner(request.OwnerIdentity, success.Run.ID); err != nil {
		t.Fatalf("the production repository could not load the owner's run: %v", err)
	}

	if err := db.Exec(`CREATE FUNCTION public.reject_verification_completion_audit() RETURNS trigger AS $$
BEGIN
	IF NEW.action = 'verification.completed' THEN
		RAISE EXCEPTION 'injected completion audit failure';
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql`).Error; err != nil {
		t.Fatalf("create test-only audit failure function: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_verification_completion_audit
BEFORE INSERT ON public.verification_audit_logs
FOR EACH ROW EXECUTE FUNCTION public.reject_verification_completion_audit()`).Error; err != nil {
		t.Fatalf("create test-only audit failure trigger: %v", err)
	}

	request.Question = "What does the second record establish?"
	request.ExternalEvidence[0].SourceID = "record-failure"
	request.ExternalEvidence[0].SourceURI = "local://verification/record-failure"
	failure, err := service.Answer(request)
	if err == nil || failure != nil {
		t.Fatalf("completion audit failure returned a successful result: result=%#v err=%v", failure, err)
	}
	var uncertain models.VerificationRun
	if err := db.Where("owner_identity = ? AND question = ?", request.OwnerIdentity, request.Question).First(&uncertain).Error; err != nil {
		t.Fatalf("load run after failed finalization: %v", err)
	}
	if uncertain.Status != verification.StatusUncertain || uncertain.Answer != "" {
		t.Fatalf("failed run was finalized: %#v", uncertain)
	}
	for name, model := range map[string]any{
		"evidence": &models.VerificationEvidence{},
		"claims":   &models.VerificationClaim{},
	} {
		var count int64
		if err := db.Model(model).Where("run_id = ?", uncertain.ID).Count(&count).Error; err != nil {
			t.Fatalf("count rolled-back %s: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("failed finalization retained %d %s rows", count, name)
		}
	}
	for action, want := range map[string]int64{
		"verification.completed":          0,
		"verification.persistence_failed": 1,
	} {
		var count int64
		if err := db.Model(&models.VerificationAuditLog{}).
			Where("run_id = ? AND action = ?", uncertain.ID, action).Count(&count).Error; err != nil {
			t.Fatalf("count %s audit: %v", action, err)
		}
		if count != want {
			t.Fatalf("%s audit count = %d, want %d", action, count, want)
		}
	}
}
