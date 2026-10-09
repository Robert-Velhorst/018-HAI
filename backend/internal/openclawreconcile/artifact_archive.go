package openclawreconcile

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrArtifactRetentionConflict = errors.New("retained artifact differs from current content")
var ErrArtifactRetentionQuota = errors.New("artifact retention quota reached")
var ErrArtifactRetentionDisabled = errors.New("artifact retention is not configured")
var ErrArtifactRetentionWeakKey = errors.New("artifact retention requires a sufficiently varied key and explicit operator confirmation that it was generated with a cryptographically secure random source")
var ErrArtifactRetentionExistingContentUnreadable = errors.New("existing retained artifacts cannot be read with the configured key")
var ErrArtifactRetentionLegacyProvenance = errors.New("legacy retained artifact source provenance cannot be cryptographically verified; re-retain it from the source before downloading")
var ErrArtifactNotRetained = errors.New("artifact has no retained HAI copy")

const artifactOwnerStorageLimit = 64 << 20
const artifactOwnerRecordLimit = 128
const artifactRetentionMinKeyBytes = 32

type ArtifactRetentionInfo struct {
	ArtifactDigest string    `json:"artifactDigest"`
	ContentSHA256  string    `json:"contentSHA256"`
	SizeBytes      int64     `json:"sizeBytes"`
	RetainedAt     time.Time `json:"retainedAt"`
	Verification   string    `gorm:"-" json:"verification"`
}

type retainedArtifact struct {
	ExecutionReference string
	ArtifactDigest     string
	OwnerIdentity      string
	SourceEventID      uuid.UUID
	ContentSHA256      string
	SizeBytes          int64
	EncryptedContent   []byte `json:"-"`
	RetainedAt         time.Time
}

func (retainedArtifact) TableName() string { return "openclaw_retained_artifacts" }

type artifactRetentionKeyCheck struct {
	ID             int       `gorm:"primaryKey"`
	EncryptedCheck []byte    `gorm:"column:encrypted_check"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (artifactRetentionKeyCheck) TableName() string { return "openclaw_artifact_retention_key_check" }

const artifactRetentionKeyCheckID = 1
const artifactRetentionKeyCheckPlaintext = "hai-openclaw-artifact-retention-key-check-v1"

type ArtifactArchive struct {
	repo                     *Repository
	aead                     cipher.AEAD
	legacyReadAEAD           cipher.AEAD
	strongKey                bool
	validationMu             sync.Mutex
	existingContentValidated bool
}

func NewArtifactArchive(repo *Repository, secret string) *ArtifactArchive {
	return newArtifactArchive(repo, secret, false)
}

// NewArtifactArchiveWithWriteConfirmation keeps the configured key bytes
// unchanged for decryption and enables writes only when the operator separately
// confirms that the key came from a cryptographically secure random source.
func NewArtifactArchiveWithWriteConfirmation(repo *Repository, secret string, confirmed bool) *ArtifactArchive {
	return newArtifactArchive(repo, secret, confirmed)
}

func newArtifactArchive(repo *Repository, secret string, confirmed bool) *ArtifactArchive {
	a := &ArtifactArchive{repo: repo}
	if strings.TrimSpace(secret) == "" {
		return a
	}
	key := sha256.Sum256([]byte("hai-openclaw-retention-v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err == nil {
		a.aead, _ = cipher.NewGCM(block)
		a.strongKey = confirmed && sufficientlyVariedArtifactKey(secret)
	}
	if strings.HasPrefix(secret, "random-v1:") {
		legacyMaterial := strings.TrimPrefix(secret, "random-v1:")
		// Historical builds stripped this marker before deriving the encryption
		// key. Preserve the exact remainder for reads, including any whitespace.
		legacyKey := sha256.Sum256([]byte("hai-openclaw-retention-v1\x00" + legacyMaterial))
		if legacyBlock, legacyErr := aes.NewCipher(legacyKey[:]); legacyErr == nil {
			a.legacyReadAEAD, _ = cipher.NewGCM(legacyBlock)
		}
	}
	return a
}

func sufficientlyVariedArtifactKey(secret string) bool {
	key := []byte(secret)
	if len(key) < artifactRetentionMinKeyBytes {
		return false
	}
	var frequencies [256]int
	unique, maxRun, currentRun := 0, 0, 0
	var previous byte
	for index, value := range key {
		if frequencies[value] == 0 {
			unique++
		}
		frequencies[value]++
		if index == 0 || value != previous {
			currentRun = 1
		} else {
			currentRun++
		}
		if currentRun > maxRun {
			maxRun = currentRun
		}
		previous = value
	}
	entropy := 0.0
	for _, count := range frequencies {
		if count == 0 {
			continue
		}
		probability := float64(count) / float64(len(key))
		entropy -= probability * math.Log2(probability)
	}
	// This rejects obvious placeholders and accidental repetition. It is not
	// proof of entropy; the separate operator confirmation remains necessary.
	return unique >= 12 && maxRun <= 12 && entropy >= 3.0
}
func (a *ArtifactArchive) Ready() bool {
	return a != nil && a.repo != nil && a.repo.db != nil && a.aead != nil
}

func (a *ArtifactArchive) WriteReady() bool {
	return a.Ready() && a.strongKey
}

func (a *ArtifactArchive) MetadataReady() bool {
	return a != nil && a.repo != nil && a.repo.db != nil
}

func retainedAAD(row retainedArtifact) []byte {
	// The ciphertext column has a fixed nonce+tag length constraint, so format
	// versioning lives in authenticated associated data rather than a prefix.
	data, _ := json.Marshal([]string{"hai-openclaw-retention-v2", row.OwnerIdentity, row.ExecutionReference, row.ArtifactDigest, row.ContentSHA256, row.SourceEventID.String(), fmt.Sprint(row.SizeBytes)})
	return data
}

func retainedAADV1(row retainedArtifact) []byte {
	data, _ := json.Marshal([]string{"hai-openclaw-retention-v1", row.OwnerIdentity, row.ExecutionReference, row.ArtifactDigest, row.ContentSHA256})
	return data
}

func retainedArtifactEventScope(query *gorm.DB, owner, executionReference string, eventID uuid.UUID) *gorm.DB {
	return query.Where("owner_identity = ? AND execution_reference = ? AND source_event_id = ?", owner, executionReference, eventID)
}

func openRetainedContent(primary, legacy cipher.AEAD, encrypted, aad []byte) ([]byte, error) {
	if primary == nil || len(encrypted) < primary.NonceSize() {
		return nil, fmt.Errorf("invalid retained ciphertext")
	}
	data, err := primary.Open(nil, encrypted[:primary.NonceSize()], encrypted[primary.NonceSize():], aad)
	if err == nil || legacy == nil {
		return data, err
	}
	if len(encrypted) < legacy.NonceSize() {
		return nil, err
	}
	return legacy.Open(nil, encrypted[:legacy.NonceSize()], encrypted[legacy.NonceSize():], aad)
}

func (a *ArtifactArchive) decryptRetainedArtifact(row retainedArtifact) ([]byte, error) {
	data, _, err := a.decryptRetainedArtifactWithFormat(row)
	return data, err
}

func (a *ArtifactArchive) decryptRetainedArtifactWithFormat(row retainedArtifact) ([]byte, bool, error) {
	if row.SizeBytes < 0 || row.SizeBytes > agentruntime.OpenClawArtifactContentLimit || int64(len(row.EncryptedContent)) != row.SizeBytes+28 {
		return nil, false, fmt.Errorf("invalid retained content")
	}
	data, err := openRetainedContent(a.aead, nil, row.EncryptedContent, retainedAAD(row))
	legacy := false
	if err != nil {
		// v1 did not authenticate SourceEventID or SizeBytes. Keep it readable
		// only for controlled integrity/key validation. Download paths reject
		// v1 because its source-event provenance cannot be proven.
		data, err = openRetainedContent(a.aead, a.legacyReadAEAD, row.EncryptedContent, retainedAADV1(row))
		legacy = err == nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("retained content cannot be decrypted")
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != row.ContentSHA256 || int64(len(data)) != row.SizeBytes {
		return nil, legacy, fmt.Errorf("retained content integrity failed")
	}
	return data, legacy, nil
}

func artifactRetentionKeyCheckAAD() []byte {
	return []byte("hai-openclaw-retention-key-check-v1")
}

func (a *ArtifactArchive) validateOrCreateKeyCheck(tx *gorm.DB) error {
	var check artifactRetentionKeyCheck
	err := tx.Where("id = ?", artifactRetentionKeyCheckID).Take(&check).Error
	if err == nil {
		if len(check.EncryptedCheck) < a.aead.NonceSize()+a.aead.Overhead() {
			return ErrArtifactRetentionExistingContentUnreadable
		}
		nonce := check.EncryptedCheck[:a.aead.NonceSize()]
		plaintext, openErr := a.aead.Open(nil, nonce, check.EncryptedCheck[a.aead.NonceSize():], artifactRetentionKeyCheckAAD())
		if openErr != nil || string(plaintext) != artifactRetentionKeyCheckPlaintext {
			return ErrArtifactRetentionExistingContentUnreadable
		}
		return a.verifyExistingRetainedContent(tx)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("read artifact-retention key check: %w", err)
	}
	if err := a.verifyExistingRetainedContent(tx); err != nil {
		if errors.Is(err, ErrArtifactRetentionExistingContentUnreadable) {
			return err
		}
		return fmt.Errorf("verify existing retained artifacts: %w", err)
	}
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("create artifact-retention key check nonce: %w", err)
	}
	ciphertext := a.aead.Seal(nil, nonce, []byte(artifactRetentionKeyCheckPlaintext), artifactRetentionKeyCheckAAD())
	check = artifactRetentionKeyCheck{ID: artifactRetentionKeyCheckID, EncryptedCheck: append(nonce, ciphertext...), CreatedAt: time.Now().UTC()}
	if err := tx.Create(&check).Error; err != nil {
		return fmt.Errorf("persist artifact-retention key check: %w", err)
	}
	return nil
}

func (a *ArtifactArchive) verifyExistingRetainedContent(tx *gorm.DB) error {
	rows, err := tx.Model(&retainedArtifact{}).
		Select("owner_identity, execution_reference, artifact_digest, content_sha256, size_bytes, source_event_id, encrypted_content").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row retainedArtifact
		if err := tx.ScanRows(rows, &row); err != nil {
			return err
		}
		if _, err := a.decryptRetainedArtifact(row); err != nil {
			return ErrArtifactRetentionExistingContentUnreadable
		}
	}
	return rows.Err()
}

func (a *ArtifactArchive) List(ctx context.Context, owner string, eventID uuid.UUID) ([]ArtifactRetentionInfo, error) {
	if !a.MetadataReady() {
		return nil, ErrArtifactRetentionDisabled
	}
	receipt, _, err := a.repo.artifactReceiptForEvent(ctx, owner, eventID)
	if err != nil {
		return nil, err
	}
	rows := []ArtifactRetentionInfo{}
	query := retainedArtifactEventScope(a.repo.db.WithContext(ctx).Table("openclaw_retained_artifacts"), owner, receipt.ExecutionReference, eventID)
	err = query.Select("artifact_digest,content_sha256,size_bytes,retained_at").Order("retained_at ASC, artifact_digest ASC").Limit(20).Find(&rows).Error
	for i := range rows {
		rows[i].Verification = "unverified"
	}
	return rows, err
}

func (a *ArtifactArchive) Read(ctx context.Context, binding ArtifactDownloadBinding) (*agentruntime.GatewayArtifactContent, error) {
	if !a.MetadataReady() {
		return nil, ErrArtifactRetentionDisabled
	}
	current, err := a.repo.ArtifactForEvent(ctx, binding.Receipt.OwnerIdentity, binding.EventID, binding.Descriptor.Digest)
	if err != nil {
		return nil, err
	}
	if current.Receipt.ExecutionReference != binding.Receipt.ExecutionReference || current.Receipt.RuntimeTaskID != binding.Receipt.RuntimeTaskID || current.Receipt.RunID != binding.Receipt.RunID {
		return nil, gorm.ErrRecordNotFound
	}
	var row retainedArtifact
	query := retainedArtifactEventScope(a.repo.db.WithContext(ctx).Model(&retainedArtifact{}), binding.Receipt.OwnerIdentity, binding.Receipt.ExecutionReference, binding.EventID)
	err = query.Where("artifact_digest = ?", binding.Descriptor.Digest).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrArtifactNotRetained
	}
	if err != nil {
		return nil, err
	}
	if !a.Ready() {
		return nil, ErrArtifactRetentionDisabled
	}
	data, legacy, err := a.decryptRetainedArtifactWithFormat(row)
	if err != nil {
		return nil, err
	}
	if legacy {
		return nil, ErrArtifactRetentionLegacyProvenance
	}
	if current.Descriptor.SizeBytes != nil && *current.Descriptor.SizeBytes != row.SizeBytes {
		return nil, fmt.Errorf("retained content integrity failed")
	}
	return &agentruntime.GatewayArtifactContent{Data: data, ContentSHA256: row.ContentSHA256}, nil
}

// verifyRetainedContentMatchesLive checks a legacy retained copy against the
// currently bound source without changing the stored ciphertext or audit log.
func (a *ArtifactArchive) verifyRetainedContentMatchesLive(ctx context.Context, binding ArtifactDownloadBinding, content *agentruntime.GatewayArtifactContent) error {
	if !a.Ready() {
		return ErrArtifactRetentionDisabled
	}
	if content == nil || len(content.Data) > agentruntime.OpenClawArtifactContentLimit {
		return agentruntime.ErrOpenClawArtifactUnsupported
	}
	hash := sha256.Sum256(content.Data)
	if hex.EncodeToString(hash[:]) != content.ContentSHA256 {
		return fmt.Errorf("content integrity failed")
	}
	current, err := a.repo.ArtifactForEvent(ctx, binding.Receipt.OwnerIdentity, binding.EventID, binding.Descriptor.Digest)
	if err != nil {
		return err
	}
	if !sameArtifactBinding(current, binding) || (current.Descriptor.SizeBytes != nil && *current.Descriptor.SizeBytes != int64(len(content.Data))) {
		return ErrArtifactRetentionConflict
	}
	var row retainedArtifact
	query := retainedArtifactEventScope(a.repo.db.WithContext(ctx).Model(&retainedArtifact{}), binding.Receipt.OwnerIdentity, binding.Receipt.ExecutionReference, binding.EventID)
	err = query.Where("artifact_digest = ?", binding.Descriptor.Digest).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrArtifactNotRetained
	}
	if err != nil {
		return err
	}
	stored, _, err := a.decryptRetainedArtifactWithFormat(row)
	if err != nil {
		return ErrArtifactRetentionExistingContentUnreadable
	}
	if row.ContentSHA256 != content.ContentSHA256 || !bytes.Equal(stored, content.Data) {
		return ErrArtifactRetentionConflict
	}
	return nil
}

func sameArtifactBinding(current *ArtifactDownloadBinding, expected ArtifactDownloadBinding) bool {
	if current == nil || current.EventID != expected.EventID ||
		current.Receipt.OwnerIdentity != expected.Receipt.OwnerIdentity ||
		current.Receipt.ExecutionReference != expected.Receipt.ExecutionReference ||
		current.Receipt.RuntimeTaskID != expected.Receipt.RuntimeTaskID ||
		current.Receipt.SessionKey != expected.Receipt.SessionKey ||
		current.Receipt.SessionID != expected.Receipt.SessionID ||
		current.Receipt.RunID != expected.Receipt.RunID ||
		current.Receipt.Status != expected.Receipt.Status ||
		current.Receipt.TerminalStatus != expected.Receipt.TerminalStatus ||
		!current.Receipt.TerminalAt.Equal(expected.Receipt.TerminalAt) ||
		current.Descriptor.Digest != expected.Descriptor.Digest ||
		current.Descriptor.Type != expected.Descriptor.Type ||
		current.Descriptor.MIMEType != expected.Descriptor.MIMEType {
		return false
	}
	if current.Descriptor.SizeBytes == nil || expected.Descriptor.SizeBytes == nil {
		return current.Descriptor.SizeBytes == nil && expected.Descriptor.SizeBytes == nil
	}
	return *current.Descriptor.SizeBytes == *expected.Descriptor.SizeBytes
}

func artifactSourceEvent(tx *gorm.DB, binding ArtifactDownloadBinding) (models.AutomationLaunchEvent, error) {
	var source models.AutomationLaunchEvent
	err := tx.Where("id = ? AND owner_identity = ? AND runtime_type = 'openclaw' AND execution_reference = ? AND runtime_task_id = ?",
		binding.EventID, binding.Receipt.OwnerIdentity, binding.Receipt.ExecutionReference, binding.Receipt.RuntimeTaskID).Take(&source).Error
	return source, err
}

func (a *ArtifactArchive) Retain(ctx context.Context, binding ArtifactDownloadBinding, content *agentruntime.GatewayArtifactContent) (*ArtifactRetentionInfo, error) {
	if !a.Ready() {
		return nil, ErrArtifactRetentionDisabled
	}
	if !a.strongKey {
		return nil, ErrArtifactRetentionWeakKey
	}
	if content == nil || len(content.Data) > agentruntime.OpenClawArtifactContentLimit {
		return nil, agentruntime.ErrOpenClawArtifactUnsupported
	}
	hash := sha256.Sum256(content.Data)
	if hex.EncodeToString(hash[:]) != content.ContentSHA256 {
		return nil, fmt.Errorf("content integrity failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.validationMu.Lock()
	needsKeyValidation := !a.existingContentValidated
	if needsKeyValidation {
		defer a.validationMu.Unlock()
	} else {
		a.validationMu.Unlock()
	}
	validatedInTransaction := false
	var info ArtifactRetentionInfo
	err := a.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if needsKeyValidation {
			// Only cold archives need the global lock. Once this instance has
			// authenticated the persisted key check, owner-scoped writes should not
			// serialize unrelated owners behind an archive-wide lock.
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "openclaw-artifact-retention:key-validation").Error; err != nil {
				return err
			}
			if err := a.validateOrCreateKeyCheck(tx); err != nil {
				return err
			}
			validatedInTransaction = true
		}
		// Serialize each owner's quota read and insert across archive instances.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "openclaw-artifact-retention:"+binding.Receipt.OwnerIdentity).Error; err != nil {
			return err
		}
		current, err := NewRepository(tx).ArtifactForEvent(ctx, binding.Receipt.OwnerIdentity, binding.EventID, binding.Descriptor.Digest)
		if err != nil {
			return err
		}
		if !sameArtifactBinding(current, binding) || (current.Descriptor.SizeBytes != nil && *current.Descriptor.SizeBytes != int64(len(content.Data))) {
			return fmt.Errorf("retention binding changed")
		}
		var existing retainedArtifact
		err = tx.Where("execution_reference = ? AND artifact_digest = ?", binding.Receipt.ExecutionReference, binding.Descriptor.Digest).Take(&existing).Error
		if err == nil {
			if existing.OwnerIdentity != binding.Receipt.OwnerIdentity || existing.SourceEventID != binding.EventID || existing.ContentSHA256 != content.ContentSHA256 {
				return ErrArtifactRetentionConflict
			}
			existingData, legacy, err := a.decryptRetainedArtifactWithFormat(existing)
			if err != nil {
				return ErrArtifactRetentionExistingContentUnreadable
			}
			if !bytes.Equal(existingData, content.Data) {
				return ErrArtifactRetentionConflict
			}
			if legacy {
				source, err := artifactSourceEvent(tx, binding)
				if err != nil {
					return fmt.Errorf("confirm legacy artifact source event: %w", err)
				}
				nonce := make([]byte, a.aead.NonceSize())
				if _, err := rand.Read(nonce); err != nil {
					return err
				}
				existing.EncryptedContent = append(nonce, a.aead.Seal(nil, nonce, existingData, retainedAAD(existing))...)
				updated := tx.Model(&retainedArtifact{}).
					Where("owner_identity = ? AND execution_reference = ? AND artifact_digest = ?", existing.OwnerIdentity, existing.ExecutionReference, existing.ArtifactDigest).
					Update("encrypted_content", existing.EncryptedContent)
				if updated.Error != nil {
					return updated.Error
				}
				if updated.RowsAffected != 1 {
					return ErrArtifactRetentionConflict
				}
				now := time.Now().UTC()
				event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: source.AutomationID, OwnerIdentity: source.OwnerIdentity, RuntimeType: "openclaw", RuntimeTaskID: source.RuntimeTaskID, ExecutionReference: source.ExecutionReference, LaunchType: "agent_runtime_openclaw_artifact_provenance_upgrade", EventKey: "openclaw-artifact-provenance-upgrade:" + uuid.NewString(), Target: "archive://openclaw/artifact", Status: "observed", Message: "Legacy bytes were matched to the exact live artifact and rebound to the current source event; deliverable correctness remains unverified", AuditEvents: []string{"artifact_reference_sha256=" + existing.ArtifactDigest, "content_sha256=" + existing.ContentSHA256, "legacy_source_event_id=" + existing.SourceEventID.String(), "source_event_id=" + binding.EventID.String(), "retention_format=v2"}, StartedAt: now, CompletedAt: now}
				if err := tx.Create(&event).Error; err != nil {
					return err
				}
			}
			info = ArtifactRetentionInfo{ArtifactDigest: existing.ArtifactDigest, ContentSHA256: existing.ContentSHA256, SizeBytes: existing.SizeBytes, RetainedAt: existing.RetainedAt, Verification: "unverified"}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var quota struct {
			Bytes   int64
			Records int64
		}
		if err := tx.Model(&retainedArtifact{}).Select("COALESCE(SUM(size_bytes),0) AS bytes, COUNT(*) AS records").Where("owner_identity = ?", binding.Receipt.OwnerIdentity).Scan(&quota).Error; err != nil {
			return err
		}
		if quota.Bytes+int64(len(content.Data)) > artifactOwnerStorageLimit || quota.Records >= artifactOwnerRecordLimit {
			return ErrArtifactRetentionQuota
		}
		row := retainedArtifact{ExecutionReference: binding.Receipt.ExecutionReference, ArtifactDigest: binding.Descriptor.Digest, OwnerIdentity: binding.Receipt.OwnerIdentity, SourceEventID: binding.EventID, ContentSHA256: content.ContentSHA256, SizeBytes: int64(len(content.Data)), RetainedAt: time.Now().UTC()}
		nonce := make([]byte, a.aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		row.EncryptedContent = a.aead.Seal(nonce, nonce, content.Data, retainedAAD(row))
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		source, err := artifactSourceEvent(tx, binding)
		if err != nil {
			return err
		}
		event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: source.AutomationID, OwnerIdentity: source.OwnerIdentity, RuntimeType: "openclaw", RuntimeTaskID: source.RuntimeTaskID, ExecutionReference: source.ExecutionReference, LaunchType: "agent_runtime_openclaw_artifact_retain", EventKey: "openclaw-artifact-retain:" + uuid.NewString(), Target: "archive://openclaw/artifact", Status: "observed", Message: "Artifact retained; deliverable correctness is not verified", AuditEvents: []string{"artifact_reference_sha256=" + row.ArtifactDigest, "content_sha256=" + row.ContentSHA256}, StartedAt: row.RetainedAt, CompletedAt: row.RetainedAt}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		info = ArtifactRetentionInfo{ArtifactDigest: row.ArtifactDigest, ContentSHA256: row.ContentSHA256, SizeBytes: row.SizeBytes, RetainedAt: row.RetainedAt, Verification: "unverified"}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	if validatedInTransaction {
		a.existingContentValidated = true
	}
	return &info, nil
}
