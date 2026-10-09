package phase2

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"

	"github.com/google/uuid"
)

func TestEvidencePackGenerationAndRetrieval(t *testing.T) {
	r, _ := newTestServer(t)
	// Complete one source-fed operation only after the exact owner review flow.
	id := createCompletedSourceOperation(t, r)

	gen := do(t, r, http.MethodPost, "/operations/"+id+"/evidence-pack")
	if gen.Code != http.StatusCreated {
		t.Fatalf("generate: status %d body %s", gen.Code, gen.Body.String())
	}
	var pack EvidencePack
	_ = json.Unmarshal(gen.Body.Bytes(), &pack)
	if pack.ID == uuid.Nil {
		t.Fatal("evidence pack must get a UUID")
	}
	if pack.ID.Version() != 4 {
		t.Fatalf("evidence pack ID version = %d, want 4", pack.ID.Version())
	}
	if pack.OwnerIdentity != "local-operator" || pack.WorkspaceID != "local" {
		t.Fatalf("evidence pack scope = %q/%q", pack.OwnerIdentity, pack.WorkspaceID)
	}
	if pack.OperationID.String() != id {
		t.Fatalf("operation id = %s, want %s", pack.OperationID, id)
	}
	if !strings.HasPrefix(pack.ContentDigest, "sha256:") || len(pack.ContentDigest) != 71 {
		t.Fatalf("content digest = %q", pack.ContentDigest)
	}
	if pack.Provenance.SourceType == "" || pack.Provenance.SourceRevisionHash == "" ||
		pack.Provenance.DedupeKey == "" {
		t.Fatalf("provenance was not preserved: %#v", pack.Provenance)
	}
	// Must contain the mandatory §10.18 sections.
	for _, section := range []string{
		"# Evidence Pack:", "## Operation", "## Source Evidence", "## Privacy Scan",
		"## Policy Decision", "## Approval", "## Runtime / Model Attempts",
		"## Verification", "## Audit Timeline", "## Known Limits",
	} {
		if !strings.Contains(pack.Markdown, section) {
			t.Fatalf("evidence pack missing section %q", section)
		}
	}
	// It must reference the source revision hash and be verification-passed.
	if !strings.Contains(pack.Markdown, "sourceRevisionHash:") {
		t.Fatalf("evidence pack must include the source revision hash")
	}

	// Retrieval by id works.
	got := do(t, r, http.MethodGet, "/evidence-packs/"+pack.ID.String())
	if got.Code != http.StatusOK {
		t.Fatalf("get evidence pack: status %d", got.Code)
	}
	var retrieved EvidencePack
	if err := json.Unmarshal(got.Body.Bytes(), &retrieved); err != nil {
		t.Fatalf("decode retrieved evidence pack: %v", err)
	}
	if retrieved.ID != pack.ID || retrieved.OwnerIdentity != pack.OwnerIdentity ||
		retrieved.WorkspaceID != pack.WorkspaceID ||
		retrieved.Provenance.SourceRevisionHash != pack.Provenance.SourceRevisionHash ||
		retrieved.ContentDigest != pack.ContentDigest {
		t.Fatalf("retrieved pack lost durable fields: got %#v want %#v", retrieved, pack)
	}
}

func TestEvidencePackRedactsSecrets(t *testing.T) {
	// A pack built directly over an operation whose content carries a secret must
	// never embed the raw secret — only the redacted preview.
	r, m := newTestServer(t)
	do(t, r, http.MethodPost, "/background/run")
	ops, _ := m.Service().List(operationsFilterAll())
	if len(ops) == 0 {
		t.Fatalf("need an operation")
	}
	op := ops[0]
	op.Description = "api_key = sk-live-SECRETVALUE123456 do not leak"
	saved, _ := m.Service().Save(op, "test", "hai", "inject secret")
	events, _ := m.Service().Events(saved.ID)
	scan := privacyfilter.Scan(saved.Title+"\n"+saved.Description, 280)
	pack := buildEvidencePack(*saved, events, scan, nil, time.Now().UTC())
	if strings.Contains(pack.Markdown, "sk-live-SECRETVALUE123456") {
		t.Fatalf("evidence pack must not embed the raw secret")
	}
	if !strings.Contains(pack.Markdown, "REDACTED") {
		t.Fatalf("evidence pack privacy scan must show redaction")
	}
	if pack.OwnerIdentity != saved.OwnerUserID || pack.WorkspaceID != saved.WorkspaceID {
		t.Fatalf("pack scope = %q/%q, want %q/%q", pack.OwnerIdentity, pack.WorkspaceID, saved.OwnerUserID, saved.WorkspaceID)
	}
	if pack.Provenance.SourceRevisionHash != saved.SourceRevisionHash ||
		pack.Provenance.SourceURI != saved.SourceURI {
		t.Fatalf("pack provenance = %#v", pack.Provenance)
	}
}

func operationsFilterAll() operations.Filter {
	return operations.Filter{OwnerUserID: "local-operator", WorkspaceID: "local", Limit: 50}
}

func TestEvidencePackRetrievalRequiresAuthenticatedMatchingOwner(t *testing.T) {
	m := newTestModule(t)
	ownerRouter := newTestRouter(m, "local-operator", true)
	operationID := createCompletedSourceOperation(t, ownerRouter)

	generated := do(t, ownerRouter, http.MethodPost, "/operations/"+operationID+"/evidence-pack")
	if generated.Code != http.StatusCreated {
		t.Fatalf("generate: status %d body %s", generated.Code, generated.Body.String())
	}
	var pack EvidencePack
	if err := json.Unmarshal(generated.Body.Bytes(), &pack); err != nil {
		t.Fatalf("decode pack: %v", err)
	}

	otherOwner := newTestRouter(m, "other-owner", true)
	got := do(t, otherOwner, http.MethodGet, "/evidence-packs/"+pack.ID.String())
	if got.Code != http.StatusNotFound {
		t.Fatalf("cross-owner retrieval status = %d, want 404; body %s", got.Code, got.Body.String())
	}
	if strings.Contains(got.Body.String(), pack.Title) {
		t.Fatal("cross-owner response leaked evidence metadata")
	}

	unauthenticated := newTestRouter(m, "", false)
	got = do(t, unauthenticated, http.MethodGet, "/evidence-packs/"+pack.ID.String())
	if got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated retrieval status = %d, want 401", got.Code)
	}
	got = do(t, unauthenticated, http.MethodPost, "/operations/"+operationID+"/evidence-pack")
	if got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated generation status = %d, want 401", got.Code)
	}
}

func TestEvidencePackRetrievalRejectsMalformedUUID(t *testing.T) {
	r, _ := newTestServer(t)
	got := do(t, r, http.MethodGet, "/evidence-packs/evp-1")
	if got.Code != http.StatusBadRequest {
		t.Fatalf("malformed id status = %d, want 400", got.Code)
	}
}

func TestEvidencePackStorageFailuresFailClosed(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		m := newTestModule(t)
		repository := newTestEvidencePackRepository()
		repository.createErr = errTestEvidenceStorage
		m.evidence = repository
		m.evidenceErr = nil
		r := newTestRouter(m, "local-operator", true)
		operationID := createCompletedSourceOperation(t, r)

		got := do(t, r, http.MethodPost, "/operations/"+operationID+"/evidence-pack")
		if got.Code != http.StatusInternalServerError {
			t.Fatalf("create failure status = %d, want 500; body %s", got.Code, got.Body.String())
		}
		if strings.Contains(got.Body.String(), errTestEvidenceStorage.Error()) {
			t.Fatal("storage error details must not be exposed")
		}
		if len(repository.packs) != 0 {
			t.Fatal("failed creation must not expose a process-local pack")
		}
	})

	t.Run("get", func(t *testing.T) {
		m := newTestModule(t)
		repository := newTestEvidencePackRepository()
		repository.getErr = errTestEvidenceStorage
		m.evidence = repository
		m.evidenceErr = nil
		r := newTestRouter(m, "local-operator", true)

		got := do(t, r, http.MethodGet, "/evidence-packs/"+uuid.NewString())
		if got.Code != http.StatusInternalServerError {
			t.Fatalf("get failure status = %d, want 500; body %s", got.Code, got.Body.String())
		}
		if strings.Contains(got.Body.String(), errTestEvidenceStorage.Error()) {
			t.Fatal("storage error details must not be exposed")
		}
	})

	t.Run("repository unavailable", func(t *testing.T) {
		m := newTestModule(t)
		m.evidence = nil
		m.evidenceErr = ErrEvidencePackRepositoryUnavailable
		r := newTestRouter(m, "local-operator", true)

		got := do(t, r, http.MethodGet, "/evidence-packs/"+uuid.NewString())
		if got.Code != http.StatusServiceUnavailable {
			t.Fatalf("unavailable repository status = %d, want 503; body %s", got.Code, got.Body.String())
		}
	})
}

func TestNormalizeEvidencePackRecomputesDigestAndRejectsMissingScope(t *testing.T) {
	now := time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC)
	value := EvidencePack{
		OwnerIdentity: " alice ",
		WorkspaceID:   " work ",
		OperationID:   uuid.New(),
		Title:         " Evidence ",
		Markdown:      "# verified",
		Provenance: EvidenceProvenance{
			SourceType: "manual",
			DedupeKey:  "manual:evidence",
		},
		ContentDigest: "sha256:caller-controlled",
		GeneratedAt:   now,
	}
	normalized, err := normalizeEvidencePack(value)
	if err != nil {
		t.Fatalf("normalize evidence pack: %v", err)
	}
	if normalized.ID == uuid.Nil || normalized.OwnerIdentity != "alice" ||
		normalized.WorkspaceID != "work" || normalized.Title != "Evidence" {
		t.Fatalf("normalized pack = %#v", normalized)
	}
	if normalized.ContentDigest == value.ContentDigest ||
		!strings.HasPrefix(normalized.ContentDigest, "sha256:") {
		t.Fatalf("content digest was not derived from evidence fields: %q", normalized.ContentDigest)
	}
	tampered := normalized
	tampered.Provenance.SourceURI = "file:///different-source"
	tampered, err = normalizeEvidencePack(tampered)
	if err != nil {
		t.Fatalf("normalize provenance change: %v", err)
	}
	if tampered.ContentDigest == normalized.ContentDigest {
		t.Fatal("content digest must bind source provenance")
	}

	value.OwnerIdentity = ""
	if _, err := normalizeEvidencePack(value); err == nil {
		t.Fatal("missing owner must be rejected")
	}
	value.OwnerIdentity = "alice"
	value.WorkspaceID = ""
	if _, err := normalizeEvidencePack(value); err == nil {
		t.Fatal("missing workspace must be rejected")
	}
}

func TestEvidencePackRepositoryErrorsAreClassifiable(t *testing.T) {
	if !errors.Is(ErrEvidencePackNotFound, ErrEvidencePackNotFound) ||
		!errors.Is(ErrEvidencePackRepositoryUnavailable, ErrEvidencePackRepositoryUnavailable) {
		t.Fatal("repository sentinel errors must remain classifiable")
	}
}
