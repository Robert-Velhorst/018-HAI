package models

import (
	"time"

	"github.com/google/uuid"
)

// BrainCatalogUpstreamReview stores a redacted availability and ownership
// check for one fixed catalog repository. It deliberately stores no source
// archive, credential, issue data, or runtime configuration.
type BrainCatalogUpstreamReview struct {
	ID                  uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	CatalogEntryID      string    `gorm:"type:varchar(160);index;not null" json:"catalogEntryId"`
	Name                string    `gorm:"type:varchar(255);not null" json:"name"`
	UpstreamURL         string    `gorm:"type:varchar(1024);not null" json:"upstreamUrl"`
	ResolvedRepository  string    `gorm:"type:varchar(255)" json:"resolvedRepository,omitempty"`
	ResolvedUpstreamURL string    `gorm:"type:varchar(1024)" json:"resolvedUpstreamUrl,omitempty"`
	RepositoryMoved     bool      `gorm:"index" json:"repositoryMoved"`
	Available           bool      `gorm:"index" json:"available"`
	Archived            bool      `gorm:"index" json:"archived"`
	License             string    `gorm:"type:varchar(120)" json:"license,omitempty"`
	DefaultBranch       string    `gorm:"type:varchar(255)" json:"defaultBranch,omitempty"`
	PushedAt            string    `gorm:"type:varchar(80)" json:"pushedAt,omitempty"`
	Message             string    `gorm:"type:text" json:"message"`
	Disposition         string    `gorm:"type:varchar(80);index;not null" json:"disposition"`
	Readiness           string    `gorm:"type:varchar(80);index" json:"readiness"`
	ReadinessReason     string    `gorm:"type:text" json:"readinessReason"`
	RequiredGatesJSON   string    `gorm:"type:text" json:"-"`
	CheckedAt           time.Time `gorm:"index;not null" json:"checkedAt"`
	CreatedAt           time.Time `json:"createdAt"`
}

// BrainCatalogCollectionReview records a bounded public OSS Insight collection
// index check. It intentionally stores only collection names and counts, never
// repository source, credentials, package metadata, or runtime configuration.
type BrainCatalogCollectionReview struct {
	ID                  uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	SourceURL           string    `gorm:"type:varchar(1024);not null" json:"sourceUrl"`
	Available           bool      `gorm:"index" json:"available"`
	ExpectedTotal       int       `json:"expectedTotal"`
	CurrentTotal        int       `json:"currentTotal"`
	NewCollectionsJSON  string    `gorm:"type:text" json:"-"`
	MissingExpectedJSON string    `gorm:"type:text" json:"-"`
	Message             string    `gorm:"type:text" json:"message"`
	CheckedAt           time.Time `gorm:"index;not null" json:"checkedAt"`
	CreatedAt           time.Time `json:"createdAt"`
}

// BrainCatalogRepositoryDiscoveryReview records a bounded, read-only daily
// gap review. It keeps aggregate evidence plus a capped list of repository
// names that still need a separate review; it never stores code, package
// metadata, credentials, or an activation decision.
type BrainCatalogRepositoryDiscoveryReview struct {
	ID                         uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	SourceURL                  string    `gorm:"type:varchar(1024);not null" json:"sourceUrl"`
	Scope                      string    `gorm:"type:varchar(80);index;not null" json:"scope"`
	Available                  bool      `gorm:"index" json:"available"`
	CollectionsScreened        int       `json:"collectionsScreened"`
	EligibleCollections        int       `json:"eligibleCollections"`
	CollectionsChecked         int       `json:"collectionsChecked"`
	RepositoriesChecked        int       `json:"repositoriesChecked"`
	KnownProfileHits           int       `json:"knownProfileHits"`
	UnreviewedDiscoveries      int       `json:"unreviewedDiscoveries"`
	MissingCollectionsJSON     string    `gorm:"type:text" json:"-"`
	UnavailableCollectionsJSON string    `gorm:"type:text" json:"-"`
	CandidateRepositoriesJSON  string    `gorm:"type:text" json:"-"`
	CandidatesTruncated        bool      `json:"candidatesTruncated"`
	Message                    string    `gorm:"type:text" json:"message"`
	CheckedAt                  time.Time `gorm:"index;not null" json:"checkedAt"`
	CreatedAt                  time.Time `json:"createdAt"`
}
