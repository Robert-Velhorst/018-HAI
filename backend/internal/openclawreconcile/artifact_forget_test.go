package openclawreconcile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func confirmedArtifactTestKey(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func TestArtifactIfMatchSHA256RequiresOneStrongQuotedDigest(t *testing.T) {
	digest := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		headers []string
		want    string
		valid   bool
	}{
		{name: "strong quoted digest", headers: []string{`"` + digest + `"`}, want: digest, valid: true},
		{name: "optional HTTP whitespace", headers: []string{" \t\"" + digest + "\" "}, want: digest, valid: true},
		{name: "missing", valid: false},
		{name: "duplicate fields", headers: []string{`"` + digest + `"`, `"` + digest + `"`}, valid: false},
		{name: "wildcard", headers: []string{"*"}, valid: false},
		{name: "weak entity tag", headers: []string{`W/"` + digest + `"`}, valid: false},
		{name: "unquoted digest", headers: []string{digest}, valid: false},
		{name: "entity tag list", headers: []string{`"` + digest + `", "` + digest + `"`}, valid: false},
		{name: "uppercase digest", headers: []string{`"` + strings.Repeat("A", 64) + `"`}, valid: false},
		{name: "invalid hex", headers: []string{`"` + strings.Repeat("g", 64) + `"`}, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := artifactIfMatchSHA256(test.headers)
			if valid != test.valid || got != test.want {
				t.Fatalf("artifactIfMatchSHA256(%q) = (%q, %v), want (%q, %v)", test.headers, got, valid, test.want, test.valid)
			}
		})
	}
}

func TestArtifactForgetRowRequiresExactOwnerEventExecutionAndDigest(t *testing.T) {
	eventID := uuid.New()
	row := retainedArtifact{
		OwnerIdentity:      "owner-1",
		SourceEventID:      eventID,
		ExecutionReference: "ocgw:v2:execution-1",
		ArtifactDigest:     strings.Repeat("a", 64),
	}
	if !artifactForgetRowMatches(row, "owner-1", eventID, row.ExecutionReference, row.ArtifactDigest) {
		t.Fatal("exact retained-artifact binding was rejected")
	}
	tests := []struct {
		name string
		edit func(*retainedArtifact)
	}{
		{name: "different owner", edit: func(row *retainedArtifact) { row.OwnerIdentity = "owner-2" }},
		{name: "different source event", edit: func(row *retainedArtifact) { row.SourceEventID = uuid.New() }},
		{name: "different execution", edit: func(row *retainedArtifact) { row.ExecutionReference = "ocgw:v2:execution-2" }},
		{name: "different artifact digest", edit: func(row *retainedArtifact) { row.ArtifactDigest = strings.Repeat("b", 64) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := row
			test.edit(&changed)
			if artifactForgetRowMatches(changed, "owner-1", eventID, row.ExecutionReference, row.ArtifactDigest) {
				t.Fatal("mismatched retained-artifact binding was accepted")
			}
		})
	}
}

func TestArtifactForgetRequiresReadableAuthenticatedCiphertext(t *testing.T) {
	archive := NewArtifactArchive(nil, confirmedArtifactTestKey("correct retained-artifact key"))
	wrongKeyArchive := NewArtifactArchive(nil, confirmedArtifactTestKey("different retained-artifact key"))
	content := []byte("preserve this retained artifact")
	contentHash := sha256.Sum256(content)
	eventID := uuid.New()
	row := retainedArtifact{
		OwnerIdentity:      "owner-1",
		SourceEventID:      eventID,
		ExecutionReference: "ocgw:v2:execution-1",
		ArtifactDigest:     strings.Repeat("a", 64),
		ContentSHA256:      hex.EncodeToString(contentHash[:]),
		SizeBytes:          int64(len(content)),
	}
	nonce := bytes.Repeat([]byte{7}, archive.aead.NonceSize())
	row.EncryptedContent = append(nonce, archive.aead.Seal(nil, nonce, content, retainedAAD(row))...)
	originalCiphertext := bytes.Clone(row.EncryptedContent)
	args := []any{row, "owner-1", eventID, row.ExecutionReference, row.ArtifactDigest, row.ContentSHA256}
	remove := func(candidate *ArtifactArchive, values []any) error {
		return artifactForgetRowSafeToRemove(candidate, values[0].(retainedArtifact), values[1].(string), values[2].(uuid.UUID), values[3].(string), values[4].(string), values[5].(string))
	}

	if err := remove(archive, args); err != nil {
		t.Fatalf("correct key and exact fingerprint should authenticate the retained copy: %v", err)
	}
	if err := remove(wrongKeyArchive, args); !errors.Is(err, ErrArtifactRetentionExistingContentUnreadable) {
		t.Fatalf("wrong key must prevent removal of unreadable retained ciphertext: %v", err)
	}
	if !bytes.Equal(row.EncryptedContent, originalCiphertext) {
		t.Fatal("failed removal validation modified retained ciphertext")
	}
	corrupted := row
	corrupted.EncryptedContent = bytes.Clone(row.EncryptedContent)
	corrupted.EncryptedContent[len(corrupted.EncryptedContent)-1] ^= 1
	if err := artifactForgetRowSafeToRemove(archive, corrupted, "owner-1", eventID, row.ExecutionReference, row.ArtifactDigest, row.ContentSHA256); !errors.Is(err, ErrArtifactRetentionExistingContentUnreadable) {
		t.Fatalf("corrupted ciphertext must prevent removal: %v", err)
	}
	if err := artifactForgetRowSafeToRemove(archive, row, "owner-1", eventID, row.ExecutionReference, row.ArtifactDigest, strings.Repeat("b", 64)); !errors.Is(err, ErrArtifactRetentionPrecondition) {
		t.Fatalf("stale content fingerprint must remain a precondition failure: %v", err)
	}
}

func TestArtifactForgetAuditEvidenceMustMatchExactRequest(t *testing.T) {
	eventID := uuid.New()
	digest := strings.Repeat("a", 64)
	contentSHA := strings.Repeat("b", 64)
	events := []string{
		"source_event_id=" + eventID.String(),
		"artifact_reference_sha256=" + digest,
		"content_sha256=" + contentSHA,
	}
	if !artifactForgetAuditEventsMatch(events, eventID, digest, contentSHA) {
		t.Fatal("exact prior removal evidence was not recognized")
	}
	tests := []struct {
		name    string
		eventID uuid.UUID
		digest  string
		content string
	}{
		{name: "different source event", eventID: uuid.New(), digest: digest, content: contentSHA},
		{name: "different artifact digest", eventID: eventID, digest: strings.Repeat("c", 64), content: contentSHA},
		{name: "different content fingerprint", eventID: eventID, digest: digest, content: strings.Repeat("d", 64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if artifactForgetAuditEventsMatch(events, test.eventID, test.digest, test.content) {
				t.Fatal("non-matching removal audit was accepted as an idempotent retry")
			}
		})
	}
	if artifactForgetAuditEventsMatch([]string{"source_event_id=" + eventID.String() + "-other", "artifact_reference_sha256=" + digest, "content_sha256=" + contentSHA}, eventID, digest, contentSHA) {
		t.Fatal("audit token prefix was accepted without an exact value match")
	}
}

func TestArtifactForgetMissingBindingIsIdempotentOnlyWithExactPriorAudit(t *testing.T) {
	eventID := uuid.New()
	digest := strings.Repeat("a", 64)
	contentSHA := strings.Repeat("b", 64)
	events := []string{
		"source_event_id=" + eventID.String(),
		"artifact_reference_sha256=" + digest,
		"content_sha256=" + contentSHA,
	}
	alreadyForgotten := artifactForgetAuditEventsMatch(events, eventID, digest, contentSHA)
	if err := artifactForgetMissingBindingResult(gorm.ErrRecordNotFound, alreadyForgotten); err != nil {
		t.Fatalf("exact prior removal should make a retry idempotent: %v", err)
	}

	wrongContentWasForgotten := artifactForgetAuditEventsMatch(events, eventID, digest, strings.Repeat("c", 64))
	if err := artifactForgetMissingBindingResult(gorm.ErrRecordNotFound, wrongContentWasForgotten); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing artifact without an exact content audit must remain not found: %v", err)
	}

	databaseErr := errors.New("audit store unavailable")
	if err := artifactForgetMissingBindingResult(databaseErr, alreadyForgotten); !errors.Is(err, databaseErr) {
		t.Fatalf("non-not-found errors must not be hidden by old audit evidence: %v", err)
	}
}

func TestArtifactForgetHandlerRejectsInvalidPreconditionsWithoutDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(postgres.Open("host=127.0.0.1 port=1 user=unused dbname=unused sslmode=disable"), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open lazy test DB handle: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get lazy test DB handle: %v", err)
	}
	defer sqlDB.Close()
	handler := NewArtifactHandler(nil, nil)
	handler.SetArchive(NewArtifactArchiveWithWriteConfirmation(NewRepository(db), confirmedArtifactTestKey(strings.Repeat("k", 32)), true))
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "authenticated-owner") })
	engine.DELETE("/events/:eventId/artifacts/:digest", handler.Forget)
	digest := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		headers []string
	}{
		{name: "missing"},
		{name: "wildcard", headers: []string{"*"}},
		{name: "weak tag", headers: []string{`W/"` + strings.Repeat("b", 64) + `"`}},
		{name: "unquoted", headers: []string{strings.Repeat("b", 64)}},
		{name: "multiple values", headers: []string{`"` + strings.Repeat("b", 64) + `"`, `"` + strings.Repeat("b", 64) + `"`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/events/"+uuid.NewString()+"/artifacts/"+digest, nil)
			for _, value := range test.headers {
				req.Header.Add("If-Match", value)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			if response.Code != http.StatusPreconditionRequired {
				t.Fatalf("status = %d, want 428: %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private") || sqlDB.Stats().OpenConnections != 0 {
				t.Fatal("invalid precondition leaked private data or reached the database")
			}
		})
	}
}
