package sourceevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ValidatorPrimarySource = "primary_source"
	ValidatorFreshSource   = "fresh_source"
	ValidatorSourceContext = "source_context"
)

var (
	ErrNotFound              = errors.New("source evidence not found")
	ErrInvalidClaim          = errors.New("invalid source evidence claim")
	ErrSnapshotMismatch      = errors.New("source evidence snapshot changed")
	ErrRepositoryUnavailable = errors.New("source evidence repository unavailable")
	lowerSHA256              = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sourceContentHashPattern = regexp.MustCompile(`^[0-9a-f]{1,64}$`)
)

type Claim struct {
	RequirementID  string `json:"requirementId"`
	Validator      string `json:"validator"`
	ExtractionID   string `json:"extractionId"`
	SourceID       string `json:"sourceId"`
	RawItemID      string `json:"rawItemId"`
	SnapshotDigest string `json:"snapshotDigest"`
	MaxAgeSeconds  int64  `json:"maxAgeSeconds,omitempty"`
}

type Snapshot struct {
	OwnerIdentity           string    `json:"-"`
	ExtractionID            string    `json:"extractionId"`
	SourceID                string    `json:"sourceId"`
	RawItemID               string    `json:"rawItemId"`
	ProjectKey              string    `json:"projectKey,omitempty"`
	RawProjectKey           string    `json:"-"`
	ExtractionURI           string    `json:"-"`
	RawItemURI              string    `json:"-"`
	ExtractionHash          string    `json:"-"`
	RawItemHash             string    `json:"-"`
	RawItemPayloadDigest    string    `json:"rawItemPayloadDigest"`
	ExtractionPayloadDigest string    `json:"extractionPayloadDigest"`
	SnapshotDigest          string    `json:"snapshotDigest"`
	FetchedAt               time.Time `json:"fetchedAt"`
	ExtractionAt            time.Time `json:"extractionAt"`
	Sensitive               bool      `json:"sensitive"`
	LocalOnly               bool      `json:"localOnly"`
	ConnectorKey            string    `json:"connectorKey"`
}

type Repository interface {
	Resolve(context.Context, string, string) (Snapshot, error)
}

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("open source evidence database: %w", err)
	}
	return NewGormRepository(db), nil
}

type joinedSnapshot struct {
	OwnerIdentity         string    `gorm:"column:owner_identity"`
	ExtractionID          string    `gorm:"column:extraction_id"`
	SourceID              string    `gorm:"column:source_id"`
	RawItemID             string    `gorm:"column:raw_item_id"`
	ProjectKey            string    `gorm:"column:project_key"`
	RawProjectKey         string    `gorm:"column:raw_project_key"`
	RawItemPayloadURI     string    `gorm:"column:raw_item_payload_uri"`
	ExtractionURI         string    `gorm:"column:extraction_uri"`
	RawItemURI            string    `gorm:"column:raw_item_uri"`
	ExtractionHash        string    `gorm:"column:extraction_hash"`
	RawItemHash           string    `gorm:"column:raw_item_hash"`
	RawItemExternalID     string    `gorm:"column:raw_item_external_id"`
	RawItemType           string    `gorm:"column:raw_item_type"`
	RawItemTitle          string    `gorm:"column:raw_item_title"`
	RawItemContent        string    `gorm:"column:raw_item_content"`
	RawItemMetadata       string    `gorm:"column:raw_item_metadata"`
	RawItemUpdatedAt      time.Time `gorm:"column:raw_item_updated_at"`
	ExtractionText        string    `gorm:"column:extraction_text"`
	ExtractionSummary     string    `gorm:"column:extraction_summary"`
	ExtractionEntities    string    `gorm:"column:extraction_entities"`
	ExtractionDates       string    `gorm:"column:extraction_dates"`
	ExtractionTasks       string    `gorm:"column:extraction_tasks"`
	ExtractionDecisions   string    `gorm:"column:extraction_decisions"`
	ExtractionFollowUps   string    `gorm:"column:extraction_follow_ups"`
	ExtractionContentType string    `gorm:"column:extraction_content_type"`
	ExtractionSourceLabel string    `gorm:"column:extraction_source_label"`
	FetchedAt             time.Time `gorm:"column:fetched_at"`
	ExtractionAt          time.Time `gorm:"column:extraction_at"`
	Sensitive             bool      `gorm:"column:sensitive"`
	LocalOnly             bool      `gorm:"column:local_only"`
	ConnectorKey          string    `gorm:"column:connector_key"`
}

func (repository *GormRepository) Resolve(ctx context.Context, ownerIdentity, extractionID string) (Snapshot, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	extractionID = strings.TrimSpace(extractionID)
	if repository == nil || repository.db == nil {
		return Snapshot{}, ErrRepositoryUnavailable
	}
	if ctx == nil || ownerIdentity == "" || extractionID == "" || strings.ContainsAny(ownerIdentity+extractionID, "\r\n\x00") {
		return Snapshot{}, ErrInvalidClaim
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	db, cleanup, err := infra.PostgresExecutionDB(ctx, repository.db)
	if err != nil {
		return Snapshot{}, err
	}
	defer cleanup()

	var row joinedSnapshot
	query := db.Raw(`
		SELECT cs.owner_identity, se.id::text AS extraction_id,
			se.source_id::text AS source_id, se.raw_item_id::text AS raw_item_id,
			se.project_key, sr.project_key AS raw_project_key,
			se.source_uri AS extraction_uri,
			sr.source_uri AS raw_item_uri, se.content_hash AS extraction_hash,
			sr.content_hash AS raw_item_hash, sr.external_id AS raw_item_external_id,
			sr.source_uri AS raw_item_payload_uri,
			COALESCE(sr.item_type, '') AS raw_item_type,
			COALESCE(sr.title, '') AS raw_item_title,
			COALESCE(sr.content, '') AS raw_item_content,
			COALESCE(sr.metadata, '') AS raw_item_metadata,
			sr.updated_at AS raw_item_updated_at, se.text AS extraction_text,
			se.summary AS extraction_summary, se.entities AS extraction_entities,
			se.dates AS extraction_dates, se.tasks AS extraction_tasks,
			se.decisions AS extraction_decisions, se.follow_ups AS extraction_follow_ups,
			se.content_type AS extraction_content_type, se.source_label AS extraction_source_label,
			sr.fetched_at,
			se.updated_at AS extraction_at, se.sensitive, cs.local_only,
			cs.connector_key
		FROM public.source_extractions AS se
		JOIN public.source_raw_items AS sr
		  ON sr.id = se.raw_item_id AND sr.source_id = se.source_id
		JOIN public.connected_sources AS cs
		  ON cs.id = se.source_id
		WHERE cs.owner_identity = ? AND se.id = ?::uuid
		  AND cs.enabled = TRUE AND cs.status = 'active' AND cs.revoked_at IS NULL
		  AND se.archived = FALSE AND se.uncertain = FALSE
		  AND COALESCE(se.project_key, '') = COALESCE(sr.project_key, '')
		  AND btrim(se.source_uri) <> '' AND se.source_uri = sr.source_uri
		  AND btrim(se.content_hash) <> '' AND se.content_hash = sr.content_hash`,
		ownerIdentity, extractionID,
	).Scan(&row)
	if query.Error != nil {
		return Snapshot{}, fmt.Errorf("resolve source evidence: %w", query.Error)
	}
	if query.RowsAffected != 1 {
		return Snapshot{}, ErrNotFound
	}
	snapshot := Snapshot{
		OwnerIdentity: strings.TrimSpace(row.OwnerIdentity), ExtractionID: strings.TrimSpace(row.ExtractionID),
		SourceID: strings.TrimSpace(row.SourceID), RawItemID: strings.TrimSpace(row.RawItemID),
		ProjectKey: strings.TrimSpace(row.ProjectKey), RawProjectKey: strings.TrimSpace(row.RawProjectKey),
		ExtractionURI: strings.TrimSpace(row.ExtractionURI),
		RawItemURI:    strings.TrimSpace(row.RawItemURI), ExtractionHash: strings.TrimSpace(row.ExtractionHash),
		RawItemHash: strings.TrimSpace(row.RawItemHash), FetchedAt: row.FetchedAt.UTC(),
		ExtractionAt: row.ExtractionAt.UTC(), Sensitive: row.Sensitive, LocalOnly: row.LocalOnly,
		ConnectorKey: strings.TrimSpace(row.ConnectorKey),
	}
	snapshot.RawItemPayloadDigest = rawItemPayloadDigest(models.SourceRawItem{
		ID: mustUUID(snapshot.RawItemID), SourceID: mustUUID(snapshot.SourceID),
		ExternalID: row.RawItemExternalID, ProjectKey: row.RawProjectKey, ItemType: row.RawItemType,
		Title: row.RawItemTitle, SourceURI: row.RawItemPayloadURI, Content: row.RawItemContent,
		Metadata: row.RawItemMetadata, ContentHash: row.RawItemHash,
		FetchedAt: snapshot.FetchedAt, UpdatedAt: row.RawItemUpdatedAt.UTC(),
	})
	snapshot.ExtractionPayloadDigest = ExtractionPayloadDigest(models.SourceExtraction{
		ID: mustUUID(snapshot.ExtractionID), SourceID: mustUUID(snapshot.SourceID), RawItemID: mustUUID(snapshot.RawItemID),
		ProjectKey: snapshot.ProjectKey, ContentType: row.ExtractionContentType,
		Text: row.ExtractionText, Summary: row.ExtractionSummary, Entities: row.ExtractionEntities,
		Dates: row.ExtractionDates, Tasks: row.ExtractionTasks, Decisions: row.ExtractionDecisions,
		FollowUps: row.ExtractionFollowUps, SourceURI: snapshot.ExtractionURI,
		SourceLabel: row.ExtractionSourceLabel, ContentHash: snapshot.ExtractionHash,
		Sensitive: snapshot.Sensitive, UpdatedAt: snapshot.ExtractionAt,
	})
	if snapshot.OwnerIdentity != ownerIdentity || snapshot.ExtractionID != extractionID ||
		snapshot.SourceID == "" || snapshot.RawItemID == "" || snapshot.ProjectKey != snapshot.RawProjectKey ||
		snapshot.ExtractionURI == "" ||
		snapshot.ExtractionURI != snapshot.RawItemURI || snapshot.ExtractionHash == "" ||
		snapshot.ExtractionHash != snapshot.RawItemHash || !lowerSHA256.MatchString(snapshot.RawItemPayloadDigest) ||
		snapshot.FetchedAt.IsZero() || snapshot.ExtractionAt.IsZero() {
		return Snapshot{}, ErrSnapshotMismatch
	}
	snapshot.SnapshotDigest = SnapshotDigest(snapshot)
	return snapshot, nil
}

func SnapshotDigest(snapshot Snapshot) string {
	payload := struct {
		Version                 string    `json:"version"`
		OwnerIdentity           string    `json:"ownerIdentity"`
		ExtractionID            string    `json:"extractionId"`
		SourceID                string    `json:"sourceId"`
		RawItemID               string    `json:"rawItemId"`
		ProjectKey              string    `json:"projectKey"`
		RawProjectKey           string    `json:"rawProjectKey"`
		ExtractionURI           string    `json:"extractionUri"`
		RawItemURI              string    `json:"rawItemUri"`
		ExtractionHash          string    `json:"extractionHash"`
		RawItemHash             string    `json:"rawItemHash"`
		FetchedAt               time.Time `json:"fetchedAt"`
		ExtractionAt            time.Time `json:"extractionAt"`
		Sensitive               bool      `json:"sensitive"`
		LocalOnly               bool      `json:"localOnly"`
		ConnectorKey            string    `json:"connectorKey"`
		RawItemPayloadDigest    string    `json:"rawItemPayloadDigest"`
		ExtractionPayloadDigest string    `json:"extractionPayloadDigest"`
	}{
		Version: "source-evidence-snapshot-v2", OwnerIdentity: strings.TrimSpace(snapshot.OwnerIdentity),
		ExtractionID: strings.TrimSpace(snapshot.ExtractionID), SourceID: strings.TrimSpace(snapshot.SourceID),
		RawItemID: strings.TrimSpace(snapshot.RawItemID), ProjectKey: strings.TrimSpace(snapshot.ProjectKey),
		RawProjectKey: strings.TrimSpace(snapshot.RawProjectKey),
		ExtractionURI: strings.TrimSpace(snapshot.ExtractionURI), RawItemURI: strings.TrimSpace(snapshot.RawItemURI),
		ExtractionHash: strings.TrimSpace(snapshot.ExtractionHash), RawItemHash: strings.TrimSpace(snapshot.RawItemHash),
		FetchedAt: snapshot.FetchedAt.UTC(), ExtractionAt: snapshot.ExtractionAt.UTC(),
		Sensitive: snapshot.Sensitive, LocalOnly: snapshot.LocalOnly, ConnectorKey: strings.TrimSpace(snapshot.ConnectorKey),
		RawItemPayloadDigest:    snapshot.RawItemPayloadDigest,
		ExtractionPayloadDigest: snapshot.ExtractionPayloadDigest,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func ExtractionPayloadDigest(extraction models.SourceExtraction) string {
	payload := struct {
		Version     string    `json:"version"`
		ID          string    `json:"id"`
		SourceID    string    `json:"sourceId"`
		RawItemID   string    `json:"rawItemId"`
		ProjectKey  string    `json:"projectKey"`
		ContentType string    `json:"contentType"`
		Text        string    `json:"text"`
		Summary     string    `json:"summary"`
		Entities    string    `json:"entities"`
		Dates       string    `json:"dates"`
		Tasks       string    `json:"tasks"`
		Decisions   string    `json:"decisions"`
		FollowUps   string    `json:"followUps"`
		SourceURI   string    `json:"sourceUri"`
		SourceLabel string    `json:"sourceLabel"`
		ContentHash string    `json:"contentHash"`
		Sensitive   bool      `json:"sensitive"`
		Uncertain   bool      `json:"uncertain"`
		Archived    bool      `json:"archived"`
		UpdatedAt   time.Time `json:"updatedAt"`
	}{
		Version: "source-extraction-payload-v1", ID: extraction.ID.String(), SourceID: extraction.SourceID.String(),
		RawItemID: extraction.RawItemID.String(), ProjectKey: strings.TrimSpace(extraction.ProjectKey),
		ContentType: strings.TrimSpace(extraction.ContentType), Text: extraction.Text, Summary: extraction.Summary,
		Entities: extraction.Entities, Dates: extraction.Dates, Tasks: extraction.Tasks,
		Decisions: extraction.Decisions, FollowUps: extraction.FollowUps,
		SourceURI: strings.TrimSpace(extraction.SourceURI), SourceLabel: strings.TrimSpace(extraction.SourceLabel),
		ContentHash: strings.TrimSpace(extraction.ContentHash), Sensitive: extraction.Sensitive,
		Uncertain: extraction.Uncertain, Archived: extraction.Archived, UpdatedAt: extraction.UpdatedAt.UTC(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func mustUUID(value string) uuid.UUID {
	parsed, _ := uuid.Parse(value)
	return parsed
}

func rawItemPayloadDigest(item models.SourceRawItem) string {
	payload := struct {
		Version     string    `json:"version"`
		ID          string    `json:"id"`
		SourceID    string    `json:"sourceId"`
		ExternalID  string    `json:"externalId"`
		ProjectKey  string    `json:"projectKey"`
		ItemType    string    `json:"itemType"`
		Title       string    `json:"title"`
		SourceURI   string    `json:"sourceUri"`
		Content     string    `json:"content"`
		Metadata    string    `json:"metadata"`
		ContentHash string    `json:"contentHash"`
		FetchedAt   time.Time `json:"fetchedAt"`
		UpdatedAt   time.Time `json:"updatedAt"`
	}{
		Version: "source-raw-item-payload-v1", ID: item.ID.String(), SourceID: item.SourceID.String(),
		ExternalID: item.ExternalID, ProjectKey: item.ProjectKey, ItemType: item.ItemType,
		Title: item.Title, SourceURI: item.SourceURI, Content: item.Content, Metadata: item.Metadata,
		ContentHash: item.ContentHash, FetchedAt: item.FetchedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func VerifyClaim(snapshot Snapshot, claim Claim, ownerIdentity string, now time.Time) error {
	if strings.TrimSpace(ownerIdentity) == "" || snapshot.OwnerIdentity != strings.TrimSpace(ownerIdentity) ||
		claim.RequirementID == "" || claim.ExtractionID != snapshot.ExtractionID ||
		claim.SourceID != snapshot.SourceID || claim.RawItemID != snapshot.RawItemID ||
		!lowerSHA256.MatchString(claim.SnapshotDigest) || claim.SnapshotDigest != snapshot.SnapshotDigest ||
		snapshot.SnapshotDigest != SnapshotDigest(snapshot) ||
		strings.TrimSpace(snapshot.ProjectKey) != strings.TrimSpace(snapshot.RawProjectKey) ||
		strings.TrimSpace(snapshot.ExtractionURI) == "" || snapshot.ExtractionURI != snapshot.RawItemURI ||
		!sourceContentHashPattern.MatchString(snapshot.ExtractionHash) || snapshot.ExtractionHash != snapshot.RawItemHash ||
		!lowerSHA256.MatchString(snapshot.ExtractionPayloadDigest) || snapshot.FetchedAt.IsZero() ||
		snapshot.ExtractionAt.IsZero() || strings.TrimSpace(snapshot.ConnectorKey) == "" ||
		(snapshot.RawItemPayloadDigest != "" && !lowerSHA256.MatchString(snapshot.RawItemPayloadDigest)) {
		return ErrSnapshotMismatch
	}
	switch claim.Validator {
	case ValidatorPrimarySource, ValidatorSourceContext:
	case ValidatorFreshSource:
		if claim.MaxAgeSeconds <= 0 {
			return ErrInvalidClaim
		}
		if snapshot.FetchedAt.After(now.Add(5 * time.Minute)) {
			return ErrSnapshotMismatch
		}
		if now.After(snapshot.FetchedAt) {
			// Compare timestamp components directly because time.Duration saturates at about 292 years.
			ageSeconds := uint64(now.Unix()) - uint64(snapshot.FetchedAt.Unix())
			maxAgeSeconds := uint64(claim.MaxAgeSeconds)
			if ageSeconds > maxAgeSeconds ||
				(ageSeconds == maxAgeSeconds && now.Nanosecond() > snapshot.FetchedAt.Nanosecond()) {
				return ErrSnapshotMismatch
			}
		}
	default:
		return ErrInvalidClaim
	}
	return nil
}
