//go:build integration

package frameworkregistry

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func frameworkRegistryIntegrationRepository(t *testing.T) (*GormRepository, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres integration test")
	}
	destructiveFlag := os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")
	if !strings.EqualFold(strings.TrimSpace(destructiveFlag), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required for destructive Postgres integration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if err := validateDestructiveIntegrationTarget(destructiveFlag, databaseName); err != nil {
		t.Fatalf("refusing destructive integration setup: %v", err)
	}
	if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;").Error; err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	for _, path := range []string{
		"pre/0001_extensions.up.sql",
		"pre/0003_framework_registry.up.sql",
		"pre/0005_framework_operating_contract.up.sql",
		"pre/0029_framework_selector_v5_digest.up.sql",
	} {
		sql, err := migrations.Files.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatalf("apply migration %s: %v", path, err)
		}
	}
	return NewGormRepository(db), db
}

func TestFrameworkRegistryPostgresConstraintsAndImmutability(t *testing.T) {
	repo, db := frameworkRegistryIntegrationRepository(t)

	t.Run("service selection persists reproducibility metadata", func(t *testing.T) {
		service, err := NewService(repo)
		if err != nil {
			t.Fatalf("NewService: %v", err)
		}
		const secret = "integration-secret-must-not-persist"
		decision, err := service.Select(SelectionRequest{
			OwnerIdentity:  "service-owner",
			Request:        "Review source evidence with api_key=" + secret + " before drafting.",
			RiskLevel:      "high",
			NeedsDocuments: true,
			NeedsApproval:  true,
		})
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if decision.CatalogVersion != frameworkCatalogVersion ||
			decision.SelectorAlgorithmVersion != frameworkSelectorAlgorithmVersion ||
			len(decision.CatalogDigest) != 64 ||
			len(decision.EffectivePreferenceDigest) != 64 ||
			len(decision.ConstitutionDigest) != 64 ||
			decision.ConstitutionSource != "builtin-robert-constitution-v1:v1" {
			t.Fatalf("selection reproducibility metadata = %#v", decision)
		}
		var audit models.FrameworkSelectionRecord
		if err := db.Where("id = ?", decision.ID).First(&audit).Error; err != nil {
			t.Fatalf("read service selection audit: %v", err)
		}
		if strings.Contains(audit.RequestSummary, secret) ||
			strings.Contains(audit.NeedOrCommitment, secret) ||
			audit.CatalogDigest != decision.CatalogDigest ||
			audit.EffectivePreferenceDigest != decision.EffectivePreferenceDigest ||
			audit.ConstitutionDigest != decision.ConstitutionDigest {
			t.Fatalf("unsafe or incomplete service selection audit: %#v", audit)
		}
	})

	if _, err := repo.UpsertPreference("alice", Preference{
		FrameworkID: "truth-evidence",
		State:       PreferenceEnabled,
		Adaptations: []string{`Keep "direct" evidence`, "Preserve\nprovenance"},
	}); err != nil {
		t.Fatalf("UpsertPreference create: %v", err)
	}
	if _, err := repo.UpsertPreference("alice", Preference{
		FrameworkID: "truth-evidence",
		State:       PreferenceDisabled,
		Adaptations: []string{"Require review"},
	}); err != nil {
		t.Fatalf("UpsertPreference update: %v", err)
	}
	preferences, err := repo.ListPreferences("alice")
	if err != nil || len(preferences) != 1 || preferences[0].State != PreferenceDisabled {
		t.Fatalf("upserted preferences = %#v, %v", preferences, err)
	}

	hash := sha256.Sum256([]byte("integration request"))
	decision := SelectionDecision{
		ID:                        uuid.NewString(),
		CreatedAt:                 time.Now().UTC(),
		CatalogVersion:            "1.0.0",
		CatalogDigest:             repositoryTestDigest("catalog"),
		SelectorAlgorithmVersion:  "framework-selector-v1",
		EffectivePreferenceDigest: repositoryTestDigest("preferences"),
		ConstitutionDigest:        repositoryTestDigest("constitution"),
		OperatingContractDigest:   repositoryTestDigest("operating-contract"),
		LifeDomain:                "work",
		NeedOrCommitment:          "Verify evidence",
		Selected:                  []SelectedFramework{{ID: "truth-evidence", Version: "1.0.0"}},
		RequiredAgents:            []string{"evidence_reviewer"},
		MaximumAutonomyLevel:      2,
		AuthoritySummary:          "Recommend only",
		RequiresApproval:          true,
		ApprovalReasons:           []string{"consequential output"},
		EvidenceRequirements:      []string{"source record"},
		CompletionCriteria:        []string{"claims verified"},
		LearningPlan:              []string{"record confirmed correction"},
		ContextRequirements:       []string{"source context"},
		SelectionReason:           "Evidence discipline applies.",
		ConstitutionVersion:       1,
		ConstitutionSource:        "builtin-robert-constitution-v1:v1",
	}
	if err := repo.CreateSelection(
		"alice",
		decision,
		fmt.Sprintf("%x", hash),
		"Review password=do-not-store with evidence.",
	); err != nil {
		t.Fatalf("CreateSelection: %v", err)
	}
	var audit models.FrameworkSelectionRecord
	if err := db.Where("id = ?", decision.ID).First(&audit).Error; err != nil {
		t.Fatalf("read selection audit: %v", err)
	}
	if strings.Contains(audit.RequestSummary, "do-not-store") {
		t.Fatalf("selection audit retained a secret: %q", audit.RequestSummary)
	}
	if audit.CatalogDigest != decision.CatalogDigest ||
		audit.EffectivePreferenceDigest != decision.EffectivePreferenceDigest ||
		audit.ConstitutionDigest != decision.ConstitutionDigest ||
		audit.ConstitutionSource != decision.ConstitutionSource {
		t.Fatalf("selection reproducibility metadata was not persisted: %#v", audit)
	}
	if err := db.Model(&models.FrameworkSelectionRecord{}).
		Where("id = ?", decision.ID).
		Update("selection_reason", "tampered").Error; err == nil {
		t.Fatal("Postgres allowed selection audit update")
	}
	if err := db.Delete(&models.FrameworkSelectionRecord{}, "id = ?", decision.ID).Error; err == nil {
		t.Fatal("Postgres allowed selection audit delete")
	}

	first, err := repo.CreateConstitution("alice", Constitution{
		ID:             uuid.NewString(),
		Version:        1,
		Status:         ConstitutionDraft,
		Values:         []string{"Keep Robert in control"},
		ProtectedRules: []string{"Never self-approve"},
		ChangeSummary:  "First owner version",
	})
	if err != nil {
		t.Fatalf("CreateConstitution first: %v", err)
	}
	if _, err := repo.ActivateConstitution(
		"alice",
		first.ID,
		"alice",
		"Reviewed and approved.",
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("ActivateConstitution first: %v", err)
	}
	activeMutationTests := []struct {
		name   string
		column string
		value  interface{}
	}{
		{name: "content", column: "values_json", value: `["tampered"]`},
		{name: "version", column: "version", value: 99},
		{name: "owner", column: "owner_identity", value: "bob"},
		{name: "created timestamp", column: "created_at", value: time.Now().UTC().Add(-time.Hour)},
		{name: "approval metadata", column: "approval_note", value: "rewritten approval"},
		{name: "reverse lifecycle", column: "status", value: ConstitutionDraft},
	}
	for _, mutation := range activeMutationTests {
		t.Run("reject active "+mutation.name+" mutation", func(t *testing.T) {
			if err := db.Model(&models.RobertConstitutionVersion{}).
				Where("id = ?", first.ID).
				Update(mutation.column, mutation.value).Error; err == nil {
				t.Fatalf("Postgres allowed active constitution %s mutation", mutation.name)
			}
		})
	}
	if err := db.Delete(&models.RobertConstitutionVersion{}, "id = ?", first.ID).Error; err == nil {
		t.Fatal("Postgres allowed active constitution deletion")
	}
	second, err := repo.CreateConstitution("alice", Constitution{
		ID:             uuid.NewString(),
		Version:        2,
		BaseVersion:    first.Version,
		Status:         ConstitutionDraft,
		Values:         []string{"Keep Robert in control", "Prefer verified completion"},
		ProtectedRules: []string{"Never self-approve"},
		ChangeSummary:  "Second owner version",
	})
	if err != nil {
		t.Fatalf("CreateConstitution second: %v", err)
	}
	if err := db.Model(&models.RobertConstitutionVersion{}).
		Where("id = ?", second.ID).
		Update("values_json", `["draft tamper"]`).Error; err == nil {
		t.Fatal("Postgres allowed draft content mutation outside a new version")
	}
	if _, err := repo.ActivateConstitution(
		"alice",
		second.ID,
		"alice",
		"Reviewed and approved.",
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("ActivateConstitution second: %v", err)
	}
	constitutions, err := repo.ListConstitutions("alice")
	if err != nil {
		t.Fatalf("ListConstitutions: %v", err)
	}
	if len(constitutions) != 2 ||
		constitutions[0].Status != ConstitutionActive ||
		constitutions[1].Status != ConstitutionSuperseded {
		t.Fatalf("constitution lifecycle = %#v", constitutions)
	}
	if err := db.Model(&models.RobertConstitutionVersion{}).
		Where("id = ?", first.ID).
		Update("status", ConstitutionActive).Error; err == nil {
		t.Fatal("Postgres allowed superseded constitution reactivation")
	}
	if _, err := repo.CreateConstitution("alice", Constitution{
		ID:            uuid.NewString(),
		Version:       2,
		BaseVersion:   first.Version,
		Status:        ConstitutionDraft,
		ChangeSummary: "Duplicate version",
	}); err == nil {
		t.Fatal("Postgres allowed duplicate owner/version")
	}
}
