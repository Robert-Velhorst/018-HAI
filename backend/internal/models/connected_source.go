package models

import (
	"time"

	"github.com/google/uuid"
)

type SourceConnector struct {
	ID               uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ConnectorKey     string    `gorm:"type:varchar(80);uniqueIndex;not null" json:"connectorKey"`
	Name             string    `gorm:"type:varchar(255);not null" json:"name"`
	Category         string    `gorm:"type:varchar(80);index;not null" json:"category"`
	SupportedModes   string    `gorm:"type:varchar(512)" json:"supportedModes"`
	RequiredScopes   string    `gorm:"type:varchar(512)" json:"requiredScopes"`
	LocalOnlyCapable bool      `gorm:"default:true" json:"localOnlyCapable"`
	Enabled          bool      `gorm:"default:false;index" json:"enabled"`
	AdapterStatus    string    `gorm:"type:varchar(80);default:'not_implemented';index" json:"adapterStatus,omitempty"`
	StatusReason     string    `gorm:"type:text" json:"statusReason,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type ConnectedSource struct {
	ID uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	// OwnerIdentity is derived from the verified IDP subject. It is intentionally
	// omitted from API responses so callers cannot supply or inspect ownership.
	OwnerIdentity     string     `gorm:"type:varchar(255);index" json:"-"`
	ConnectorKey      string     `gorm:"type:varchar(80);index;not null" json:"connectorKey"`
	Name              string     `gorm:"type:varchar(255);not null" json:"name"`
	Category          string     `gorm:"type:varchar(80);index;not null" json:"category"`
	Enabled           bool       `gorm:"default:true;index" json:"enabled"`
	LocalOnly         bool       `gorm:"default:true;index" json:"localOnly"`
	SyncFrequency     string     `gorm:"type:varchar(50);default:'manual'" json:"syncFrequency"`
	SyncTarget        string     `gorm:"type:text" json:"syncTarget,omitempty"`
	DefaultProjectKey string     `gorm:"type:varchar(255);index" json:"defaultProjectKey,omitempty"`
	IngestionModes    string     `gorm:"type:varchar(512)" json:"ingestionModes"`
	Permissions       string     `gorm:"type:varchar(1024)" json:"permissions"`
	ExcludePatterns   string     `gorm:"type:text" json:"excludePatterns"`
	Cursor            string     `gorm:"type:varchar(512)" json:"cursor,omitempty"`
	Status            string     `gorm:"type:varchar(50);default:'active';index" json:"status"`
	LastSyncedAt      *time.Time `json:"lastSyncedAt,omitempty"`
	RevokedAt         *time.Time `json:"revokedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type SourceSyncJob struct {
	ID           uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	SourceID     uuid.UUID  `gorm:"type:uuid;index;not null" json:"sourceId"`
	Mode         string     `gorm:"type:varchar(50);index;not null" json:"mode"`
	Status       string     `gorm:"type:varchar(50);index;not null" json:"status"`
	CursorBefore string     `gorm:"type:varchar(512)" json:"cursorBefore,omitempty"`
	CursorAfter  string     `gorm:"type:varchar(512)" json:"cursorAfter,omitempty"`
	ItemsSeen    int        `json:"itemsSeen"`
	ItemsAdded   int        `json:"itemsAdded"`
	ItemsUpdated int        `json:"itemsUpdated"`
	ItemsFailed  int        `json:"itemsFailed"`
	Message      string     `gorm:"type:text" json:"message,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type SourceRawItem struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	SourceID    uuid.UUID `gorm:"type:uuid;index;not null" json:"sourceId"`
	ExternalID  string    `gorm:"type:varchar(255);index;not null" json:"externalId"`
	ProjectKey  string    `gorm:"type:varchar(255);index" json:"projectKey,omitempty"`
	ItemType    string    `gorm:"type:varchar(80);index" json:"itemType"`
	Title       string    `gorm:"type:varchar(512)" json:"title"`
	SourceURI   string    `gorm:"type:varchar(1024)" json:"sourceUri,omitempty"`
	Content     string    `gorm:"type:text" json:"content,omitempty"`
	Metadata    string    `gorm:"type:text" json:"metadata,omitempty"`
	ContentHash string    `gorm:"type:varchar(64);index" json:"contentHash,omitempty"`
	FetchedAt   time.Time `json:"fetchedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SourceExtraction struct {
	ID            uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	SourceID      uuid.UUID  `gorm:"type:uuid;index;not null" json:"sourceId"`
	RawItemID     uuid.UUID  `gorm:"type:uuid;index;not null" json:"rawItemId"`
	ProjectKey    string     `gorm:"type:varchar(255);index" json:"projectKey,omitempty"`
	ContentType   string     `gorm:"type:varchar(80);index" json:"contentType"`
	Text          string     `gorm:"type:text" json:"text"`
	Summary       string     `gorm:"type:text" json:"summary,omitempty"`
	Entities      string     `gorm:"type:text" json:"entities,omitempty"`
	Dates         string     `gorm:"type:text" json:"dates,omitempty"`
	Tasks         string     `gorm:"type:text" json:"tasks,omitempty"`
	Decisions     string     `gorm:"type:text" json:"decisions,omitempty"`
	FollowUps     string     `gorm:"type:text" json:"followUps,omitempty"`
	SourceURI     string     `gorm:"type:varchar(1024)" json:"sourceUri,omitempty"`
	SourceLabel   string     `gorm:"type:varchar(512)" json:"sourceLabel,omitempty"`
	ContentHash   string     `gorm:"type:varchar(64);index" json:"contentHash,omitempty"`
	Sensitive     bool       `gorm:"default:false;index" json:"sensitive"`
	Uncertain     bool       `gorm:"default:false;index" json:"uncertain"`
	Archived      bool       `gorm:"default:false;index" json:"archived"`
	LastIndexedAt *time.Time `json:"lastIndexedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type SourceIndexEntry struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	SourceID     uuid.UUID `gorm:"type:uuid;index;not null" json:"sourceId"`
	ExtractionID uuid.UUID `gorm:"type:uuid;index;not null" json:"extractionId"`
	ProjectKey   string    `gorm:"type:varchar(255);index" json:"projectKey,omitempty"`
	IndexType    string    `gorm:"type:varchar(50);index;not null" json:"indexType"`
	Keywords     string    `gorm:"type:text" json:"keywords,omitempty"`
	VectorRef    string    `gorm:"type:varchar(512)" json:"vectorRef,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type SourceAuditLog struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	SourceID  uuid.UUID `gorm:"type:uuid;index" json:"sourceId,omitempty"`
	Action    string    `gorm:"type:varchar(80);index;not null" json:"action"`
	Message   string    `gorm:"type:text" json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// SourceOAuthToken stores the OAuth tokens for one connected source. The access
// and refresh tokens are AES-256-GCM ciphertext, never plaintext — a refresh
// token is a long-lived credential to the user's account. The tokens are
// deliberately not exposed in JSON: they must never leave the backend.
type SourceOAuthToken struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	SourceID     uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"sourceId"`
	Provider     string    `gorm:"type:varchar(50);index;not null" json:"provider"`
	AccessToken  []byte    `gorm:"type:bytea" json:"-"`
	RefreshToken []byte    `gorm:"type:bytea" json:"-"`
	Scope        string    `gorm:"type:varchar(1024)" json:"scope,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// SourceOAuthState is the durable, single-use authorization attempt for one
// connected source. Only a SHA-256 digest is stored: the browser state value is
// a short-lived bearer secret and must not appear in the database or API.
type SourceOAuthState struct {
	ID            uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"-"`
	SourceID      uuid.UUID  `gorm:"type:uuid;uniqueIndex:ux_source_oauth_states_source;index:idx_source_oauth_states_source_created,priority:1;not null" json:"-"`
	OwnerIdentity string     `gorm:"type:varchar(255);index:idx_source_oauth_states_owner_expiry,priority:1;not null" json:"-"`
	StateDigest   string     `gorm:"type:char(64);uniqueIndex:ux_source_oauth_states_digest;not null" json:"-"`
	ExpiresAt     time.Time  `gorm:"index:idx_source_oauth_states_owner_expiry,priority:2;not null" json:"-"`
	ConsumedAt    *time.Time `json:"-"`
	CreatedAt     time.Time  `gorm:"index:idx_source_oauth_states_source_created,priority:2" json:"-"`
}
