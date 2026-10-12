package openclawreconcile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRetainedArtifactEventScopeBindsExactSourceEvent(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=127.0.0.1 port=1 user=unused dbname=unused sslmode=disable"), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open dry-run query database: %v", err)
	}
	eventID := uuid.New()
	var rows []ArtifactRetentionInfo
	result := retainedArtifactEventScope(db.Table("openclaw_retained_artifacts"), "owner-1", "ocgw:v2:execution-1", eventID).
		Select("artifact_digest,content_sha256,size_bytes,retained_at").Find(&rows)
	if result.Error != nil {
		t.Fatalf("build retained-artifact query: %v", result.Error)
	}
	query := result.Statement.SQL.String()
	if !strings.Contains(query, "source_event_id") {
		t.Fatalf("retained-artifact query does not bind the source event: %s", query)
	}
	foundEvent := false
	for _, value := range result.Statement.Vars {
		if fmt.Sprint(value) == eventID.String() {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Fatalf("retained-artifact query omitted exact source event %s: %#v", eventID, result.Statement.Vars)
	}
}

func TestArtifactArchiveRequiresExplicitKeyConfirmationAndRejectsWeakMaterial(t *testing.T) {
	legacy := NewArtifactArchive(nil, "short key")
	if legacy.aead == nil || legacy.strongKey {
		t.Fatal("legacy key must remain usable for reads and must not enable writes")
	}

	placeholder := strings.Repeat("x", artifactRetentionMinKeyBytes)
	legacyLong := NewArtifactArchive(nil, placeholder)
	confirmedPlaceholder := NewArtifactArchiveWithWriteConfirmation(nil, placeholder, true)
	if legacyLong.aead == nil || legacyLong.strongKey || confirmedPlaceholder.strongKey {
		t.Fatal("repeated or unconfirmed key material unexpectedly permits new retention")
	}

	empty := NewArtifactArchiveWithWriteConfirmation(nil, " \t\n", true)
	if empty.aead != nil || empty.strongKey {
		t.Fatal("blank key unexpectedly enabled retention")
	}
}

func TestArtifactArchiveConfirmedRandomKeyKeepsExactLegacyKeyMaterial(t *testing.T) {
	randomMaterial := make([]byte, artifactRetentionMinKeyBytes)
	if _, err := rand.Read(randomMaterial); err != nil {
		t.Fatal(err)
	}
	secret := base64.RawURLEncoding.EncodeToString(randomMaterial)
	legacy := NewArtifactArchive(nil, secret)
	confirmed := NewArtifactArchiveWithWriteConfirmation(nil, secret, true)
	if legacy.aead == nil || legacy.strongKey || confirmed.aead == nil || !confirmed.strongKey {
		t.Fatal("legacy/confirmed key readiness did not match the retention contract")
	}
	nonce := make([]byte, confirmed.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("existing retained artifact")
	ciphertext := confirmed.aead.Seal(nil, nonce, plaintext, []byte("retention compatibility"))
	opened, err := legacy.aead.Open(nil, nonce, ciphertext, []byte("retention compatibility"))
	if err != nil || string(opened) != string(plaintext) {
		t.Fatalf("separate write confirmation changed the encryption key: %v", err)
	}
}

func TestArtifactArchiveKeepsPrefixedKeyLiteralAndReadsLegacyPrefixedCiphertext(t *testing.T) {
	randomMaterial := make([]byte, artifactRetentionMinKeyBytes)
	if _, err := rand.Read(randomMaterial); err != nil {
		t.Fatal(err)
	}
	legacyMaterial := base64.RawURLEncoding.EncodeToString(randomMaterial)
	secret := "random-v1:" + legacyMaterial
	unconfirmed := NewArtifactArchive(nil, secret)
	configured := NewArtifactArchiveWithWriteConfirmation(nil, secret, true)
	if unconfirmed.aead == nil || unconfirmed.strongKey {
		t.Fatal("legacy prefix alone must not enable writes")
	}
	if !configured.strongKey || configured.aead == nil || configured.legacyReadAEAD == nil {
		t.Fatal("valid operator-confirmed key was not accepted")
	}
	// New writes treat every configured byte, including the historical prefix,
	// literally; only a failed primary decrypt may use the compatibility key.
	nonce := make([]byte, configured.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("legacy key prefix remains literal")
	aad := []byte("retention compatibility")
	literalCiphertext := configured.aead.Seal(nil, nonce, plaintext, aad)
	opened, err := openRetainedContent(configured.aead, configured.legacyReadAEAD, append(nonce, literalCiphertext...), aad)
	if err != nil || string(opened) != string(plaintext) {
		t.Fatalf("literal prefixed key could not decrypt: %v", err)
	}

	// Before confirmation became a separate setting, random-v1:<key> was
	// interpreted as the key material <key>. Reproduce that old ciphertext and
	// exercise the same primary-then-compatibility path used by ArtifactArchive.Read.
	for _, oldMaterial := range []string{legacyMaterial, " " + legacyMaterial + " "} {
		oldKey := NewArtifactArchive(nil, oldMaterial)
		oldNonce := make([]byte, oldKey.aead.NonceSize())
		if _, err := rand.Read(oldNonce); err != nil {
			t.Fatal(err)
		}
		oldCiphertext := oldKey.aead.Seal(nil, oldNonce, plaintext, aad)
		oldConfiguration := NewArtifactArchive(nil, "random-v1:"+oldMaterial)
		opened, err = openRetainedContent(oldConfiguration.aead, oldConfiguration.legacyReadAEAD, append(oldNonce, oldCiphertext...), aad)
		if err != nil || string(opened) != string(plaintext) {
			t.Fatalf("legacy prefixed ciphertext for exact key material %q could not be read: %v", oldMaterial, err)
		}
	}
}

func TestOpenRetainedContentRejectsValidLengthNonceAndTagCorruption(t *testing.T) {
	keyMaterial := make([]byte, artifactRetentionMinKeyBytes)
	if _, err := rand.Read(keyMaterial); err != nil {
		t.Fatal(err)
	}
	archive := NewArtifactArchive(nil, base64.RawURLEncoding.EncodeToString(keyMaterial))
	if archive.aead == nil {
		t.Fatal("test archive did not initialize")
	}
	nonce := make([]byte, archive.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	aad := []byte("retained artifact metadata")
	ciphertext := append(append([]byte(nil), nonce...), archive.aead.Seal(nil, nonce, []byte("retained content"), aad)...)

	for _, test := range []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "nonce", mutate: func(data []byte) { data[0] ^= 0x01 }},
		{name: "authentication tag", mutate: func(data []byte) { data[len(data)-1] ^= 0x01 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			corrupted := append([]byte(nil), ciphertext...)
			test.mutate(corrupted)
			if len(corrupted) != len(ciphertext) {
				t.Fatalf("corruption changed ciphertext length: %d != %d", len(corrupted), len(ciphertext))
			}
			if plaintext, err := openRetainedContent(archive.aead, nil, corrupted, aad); err == nil || plaintext != nil {
				t.Fatalf("corrupted ciphertext accepted: plaintext=%q err=%v", plaintext, err)
			}
		})
	}
}

func TestArtifactArchiveRetainedCiphertextBindsEventIdentityAndReadsV1(t *testing.T) {
	keyMaterial := make([]byte, artifactRetentionMinKeyBytes)
	if _, err := rand.Read(keyMaterial); err != nil {
		t.Fatal(err)
	}
	archive := NewArtifactArchive(nil, base64.RawURLEncoding.EncodeToString(keyMaterial))
	if archive.aead == nil {
		t.Fatal("test archive did not initialize")
	}
	content := []byte("event-bound retained artifact")
	hash := sha256.Sum256(content)
	row := retainedArtifact{
		ExecutionReference: "ocgw:v2:event-bound",
		ArtifactDigest:     strings.Repeat("a", 64),
		OwnerIdentity:      "retention-owner",
		SourceEventID:      uuid.New(),
		ContentSHA256:      hex.EncodeToString(hash[:]),
		SizeBytes:          int64(len(content)),
	}

	nonce := make([]byte, archive.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	row.EncryptedContent = append(append([]byte(nil), nonce...), archive.aead.Seal(nil, nonce, content, retainedAAD(row))...)
	opened, err := archive.decryptRetainedArtifact(row)
	if err != nil || string(opened) != string(content) {
		t.Fatalf("v2 retained ciphertext did not decrypt: %v", err)
	}

	tampered := row
	tampered.SourceEventID = uuid.New()
	if opened, err := archive.decryptRetainedArtifact(tampered); err == nil || opened != nil {
		t.Fatalf("v2 ciphertext accepted a changed source event: plaintext=%q err=%v", opened, err)
	}

	legacy := row
	legacy.SourceEventID = uuid.New()
	legacyNonce := make([]byte, archive.aead.NonceSize())
	if _, err := rand.Read(legacyNonce); err != nil {
		t.Fatal(err)
	}
	legacy.EncryptedContent = append(append([]byte(nil), legacyNonce...), archive.aead.Seal(nil, legacyNonce, content, retainedAADV1(legacy))...)
	opened, err = archive.decryptRetainedArtifact(legacy)
	if err != nil || string(opened) != string(content) {
		t.Fatalf("backward-compatible v1 retained ciphertext did not decrypt: %v", err)
	}
}
