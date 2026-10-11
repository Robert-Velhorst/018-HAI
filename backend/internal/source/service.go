package source

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pursuit"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/semantic"
	"automation-hub-backend/internal/workflow"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ModeManualImport       = "manual_import"
	ModeScheduledSync      = "scheduled_sync"
	ModeWebhookSync        = "webhook_sync"
	ModeFolderWatcher      = "folder_watcher"
	ModeHistoricalBackfill = "historical_backfill"
	ModeIncrementalSync    = "incremental_sync"
)

type CreateSourceRequest struct {
	OwnerIdentity     string   `json:"-"`
	ConnectorKey      string   `json:"connectorKey"`
	Name              string   `json:"name"`
	Category          string   `json:"category,omitempty"`
	Enabled           bool     `json:"enabled"`
	LocalOnly         bool     `json:"localOnly"`
	SyncFrequency     string   `json:"syncFrequency,omitempty"`
	SyncTarget        string   `json:"syncTarget,omitempty"`
	DefaultProjectKey string   `json:"defaultProjectKey,omitempty"`
	IngestionModes    []string `json:"ingestionModes,omitempty"`
	Permissions       []string `json:"permissions,omitempty"`
	ExcludePatterns   []string `json:"excludePatterns,omitempty"`
}

type UpdateSourceRequest struct {
	Name              string   `json:"name,omitempty"`
	Enabled           *bool    `json:"enabled,omitempty"`
	LocalOnly         *bool    `json:"localOnly,omitempty"`
	SyncFrequency     string   `json:"syncFrequency,omitempty"`
	SyncTarget        *string  `json:"syncTarget,omitempty"`
	DefaultProjectKey *string  `json:"defaultProjectKey,omitempty"`
	Permissions       []string `json:"permissions,omitempty"`
	ExcludePatterns   []string `json:"excludePatterns,omitempty"`
}

type ImportItem struct {
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	SourceURI  string `json:"sourceUri,omitempty"`
	ItemType   string `json:"itemType,omitempty"`
	ProjectKey string `json:"projectKey,omitempty"`
	Metadata   string `json:"metadata,omitempty"`
}

type ImportRequest struct {
	Mode       string       `json:"mode,omitempty"`
	Items      []ImportItem `json:"items"`
	FolderPath string       `json:"folderPath,omitempty"`
	// controlledTranscription is deliberately package-private. Only the
	// server-side whisper handler can mark runner output as audio-derived.
	controlledTranscription bool
	ProjectKey              string `json:"projectKey,omitempty"`
	Limit                   int    `json:"limit,omitempty"`
	MaxBytes                int64  `json:"maxBytes,omitempty"`
}

type SyncResult struct {
	Job                  models.SourceSyncJob         `json:"job"`
	Extractions          []models.SourceExtraction    `json:"extractions"`
	PursuitOutcomes      []PursuitRoutingOutcome      `json:"pursuitOutcomes,omitempty"`
	LifeGraphProjections []LifeGraphProjectionOutcome `json:"lifeGraphProjections,omitempty"`
	Message              string                       `json:"message"`
	Errors               []string                     `json:"errors,omitempty"`
	Warnings             []string                     `json:"warnings,omitempty"`
}

// PursuitRoutingOutcome makes source-created pursuit candidates and deferred
// routing visible without exposing a path that can execute or accept work.
type PursuitRoutingOutcome struct {
	ExtractionID string `json:"extractionId,omitempty"`
	WorkflowID   string `json:"workflowId,omitempty"`
	PursuitID    string `json:"pursuitId,omitempty"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}

type SearchRequest struct {
	OwnerIdentity        string   `json:"-"`
	Query                string   `json:"query"`
	ProjectKey           string   `json:"projectKey,omitempty"`
	Limit                int      `json:"limit,omitempty"`
	IncludeSensitive     bool     `json:"includeSensitive,omitempty"`
	ExcludeConnectorKeys []string `json:"-"`
}

type RankedExtraction struct {
	Extraction  models.SourceExtraction `json:"extraction"`
	Score       float64                 `json:"score"`
	Explanation string                  `json:"explanation"`
}

type SearchResult struct {
	Query       string             `json:"query"`
	ProjectKey  string             `json:"projectKey,omitempty"`
	UsedContext []RankedExtraction `json:"usedContext"`
	Explanation string             `json:"explanation"`
}

type ScheduledSyncRun struct {
	Checked   int      `json:"checked"`
	Due       int      `json:"due"`
	Completed int      `json:"completed"`
	Failed    int      `json:"failed"`
	Skipped   int      `json:"skipped"`
	Messages  []string `json:"messages"`
}

type ConnectionHealth struct {
	SourceID          uuid.UUID  `json:"sourceId"`
	ConnectorKey      string     `json:"connectorKey"`
	Status            string     `json:"status"`
	Reason            string     `json:"reason"`
	Configured        bool       `json:"configured"`
	Authorized        bool       `json:"authorized"`
	RequiresReconnect bool       `json:"requiresReconnect"`
	CursorPhase       string     `json:"cursorPhase,omitempty"`
	TokenExpiry       *time.Time `json:"tokenExpiry,omitempty"`
	LastSyncedAt      *time.Time `json:"lastSyncedAt,omitempty"`
}

type ConnectionHealthService interface {
	ConnectionHealth(sourceID uuid.UUID) (*ConnectionHealth, error)
}

type OverviewService interface {
	OverviewForOwner(ownerIdentity, projectKey string, includeArchived bool) (*SourceOverview, error)
}

type Service interface {
	Connectors() ([]models.SourceConnector, error)
	CreateSource(request CreateSourceRequest) (*models.ConnectedSource, error)
	UpdateSource(id uuid.UUID, request UpdateSourceRequest) (*models.ConnectedSource, error)
	Sources(includeDisabled bool) ([]models.ConnectedSource, error)
	SourcesForOwner(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error)
	SyncJobs(sourceID *uuid.UUID, limit int) ([]models.SourceSyncJob, error)
	SyncJobsForOwner(ownerIdentity string, limit int) ([]models.SourceSyncJob, error)
	Sync(sourceID uuid.UUID, request ImportRequest) (*SyncResult, error)
	SyncContext(ctx context.Context, sourceID uuid.UUID, request ImportRequest) (*SyncResult, error)
	// DueSources lists enabled sources whose schedule is due at now. The durable
	// scheduler uses it to enqueue one retryable job per source.
	DueSources(now time.Time) ([]models.ConnectedSource, error)
	RunDueScheduledSyncs(now time.Time) (*ScheduledSyncRun, error)
	RunDueScheduledSyncsForOwner(now time.Time, ownerIdentity string) (*ScheduledSyncRun, error)
	Reindex(sourceID uuid.UUID) (*SyncResult, error)
	Pause(sourceID uuid.UUID, paused bool) (*models.ConnectedSource, error)
	Revoke(sourceID uuid.UUID) (*models.ConnectedSource, error)
	Search(request SearchRequest) (*SearchResult, error)
	Extractions(projectKey string, includeArchived bool) ([]models.SourceExtraction, error)
	ExtractionsForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.SourceExtraction, error)
	ExtractionsForOwnerLimit(ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, error)
	ExtractionForOwner(ownerIdentity string, id uuid.UUID) (*models.SourceExtraction, error)
	UpdateExtraction(id uuid.UUID, request models.SourceExtraction) (*models.SourceExtraction, error)
	ArchiveExtraction(id uuid.UUID, archived bool) (*models.SourceExtraction, error)
	DeleteExtraction(id uuid.UUID) error
	AuditLogs(sourceID *uuid.UUID, limit int) ([]models.SourceAuditLog, error)
	AuditLogsForOwner(ownerIdentity string, limit int) ([]models.SourceAuditLog, error)
	StartGoogleOAuth(sourceID uuid.UUID) (string, error)
	CompleteGoogleOAuth(ctx context.Context, code, state, ownerIdentity string) (uuid.UUID, error)
}

// ContextSearchService binds an interactive source lookup to its caller while
// preserving the legacy Service contract used by narrow test doubles and
// trusted in-process workers.
type ContextSearchService interface {
	SearchContext(context.Context, SearchRequest) (*SearchResult, error)
}

// ContextScheduledSyncService lets interactive owner-scoped batches stop when
// their HTTP caller disconnects. Trusted background schedulers keep using the
// durable, background-context Service methods.
type ContextScheduledSyncService interface {
	RunDueScheduledSyncsForOwnerContext(context.Context, time.Time, string) (*ScheduledSyncRun, error)
}

// RunDueScheduledSyncsForOwnerWithContext prefers the cancellable production
// path while retaining compatibility with narrow in-process Service doubles.
func RunDueScheduledSyncsForOwnerWithContext(service Service, ctx context.Context, now time.Time, ownerIdentity string) (*ScheduledSyncRun, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cancellable, ok := service.(ContextScheduledSyncService); ok {
		return cancellable.RunDueScheduledSyncsForOwnerContext(ctx, now, ownerIdentity)
	}
	return service.RunDueScheduledSyncsForOwner(now, ownerIdentity)
}

// SearchWithContext uses the cancellable search path when the service supports
// it and fails promptly when the caller is already gone.
func SearchWithContext(service Service, ctx context.Context, request SearchRequest) (*SearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if contextual, ok := service.(ContextSearchService); ok {
		return contextual.SearchContext(ctx, request)
	}
	return service.Search(request)
}

type service struct {
	repo                  Repository
	memoryService         memory.Service
	workflowService       workflow.Service
	pursuitLinker         pursuitAutoLinker
	semanticService       semantic.Service
	lifeOntologyProjector LifeOntologyProjector
	finalEffectAuthorizer FinalEffectAuthorizer
	emergencyStop         func() safety.EmergencyStopDecision
	syncMu                sync.Mutex
	activeSyncs           map[uuid.UUID]bool
}

type pursuitAutoLinker interface {
	AutoLinkWorkflow(request pursuit.AutoLinkWorkflowRequest) (*pursuit.AutoLinkResult, error)
	AutoLinkMemory(request pursuit.AutoLinkMemoryRequest) (*pursuit.AutoLinkResult, error)
}

// pursuitWorkflowIntakeRouter routes producer-created work through the pursuit
// lifecycle gate before a workflow exists. It is intentionally optional so the
// source service remains usable without the pursuit module in focused tests.
type pursuitWorkflowIntakeRouter interface {
	RouteWorkflowIntake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error)
}

var errLocalFolderLimitReached = fmt.Errorf("local folder scan limit reached")
var (
	ErrSyncInProgress    = errors.New("source sync is already in progress")
	ErrSourceRevoked     = errors.New("source access is revoked")
	ErrSourceChanged     = errors.New("source changed concurrently; refresh and retry")
	ErrOAuthStateInvalid = errors.New("oauth state is invalid, expired, already used, or belongs to another operator")
)

func rejectRevokedSource(source *models.ConnectedSource) error {
	if source == nil {
		return gorm.ErrRecordNotFound
	}
	if source.RevokedAt != nil || strings.EqualFold(strings.TrimSpace(source.Status), "revoked") {
		return ErrSourceRevoked
	}
	return nil
}

const maxSyncErrorDetails = 20
const maxSyncPursuitOutcomes = 20
const defaultHTTPFeedAllowedHosts = "localhost,127.0.0.1,::1,host.docker.internal,api.github.com,api.trello.com"
const defaultWhatsAppChunkMessages = 40

var whatsAppMessageLine = regexp.MustCompile(`^\[?(\d{1,2}[/-]\d{1,2}[/-]\d{2,4}),?\s+(\d{1,2}:\d{2}(?::\d{2})?(?:\s?[APap]\.?[Mm]\.?)?)\]?\s*[-\x{2013}]\s*([^:]{1,120}):\s*(.*)$`)

func NewService(repo Repository, memoryService memory.Service) Service {
	return &service{repo: repo, memoryService: memoryService, activeSyncs: map[uuid.UUID]bool{}}
}

func NewServiceWithWorkflow(repo Repository, memoryService memory.Service, workflowService workflow.Service) Service {
	return NewServiceWithWorkflowAndPursuitLinker(repo, memoryService, workflowService, nil)
}

func NewServiceWithWorkflowAndPursuitLinker(repo Repository, memoryService memory.Service, workflowService workflow.Service, pursuitLinker pursuitAutoLinker) Service {
	return NewServiceWithWorkflowPursuitAndSemantic(repo, memoryService, workflowService, pursuitLinker, nil)
}

// NewServiceWithWorkflowPursuitAndSemantic keeps semantic search optional. A
// missing or unhealthy local embedding server never removes source ingestion or
// the provenance-preserving keyword search path.
func NewServiceWithWorkflowPursuitAndSemantic(repo Repository, memoryService memory.Service, workflowService workflow.Service, pursuitLinker pursuitAutoLinker, semanticService semantic.Service) Service {
	return &service{repo: repo, memoryService: memoryService, workflowService: workflowService, pursuitLinker: pursuitLinker, semanticService: semanticService, activeSyncs: map[uuid.UUID]bool{}}
}

func DefaultService() Service {
	return NewServiceWithWorkflow(DefaultRepository(), memory.DefaultService(), workflow.DefaultService())
}

func (s *service) intakeWorkflow(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	if router, ok := s.pursuitLinker.(pursuitWorkflowIntakeRouter); ok {
		return router.RouteWorkflowIntake(request)
	}
	if s.pursuitLinker != nil {
		return nil, pursuit.ErrLifecycleRouterRequired
	}
	if s.workflowService == nil {
		return nil, fmt.Errorf("workflow service is not configured")
	}
	return s.workflowService.Intake(request)
}

func (s *service) routesWorkflowThroughPursuits() bool {
	_, ok := s.pursuitLinker.(pursuitWorkflowIntakeRouter)
	return ok
}

func (s *service) Connectors() ([]models.SourceConnector, error) {
	if err := s.ensureConnectors(); err != nil {
		return nil, err
	}
	connectors, err := s.repo.FindConnectors()
	if err != nil {
		return nil, err
	}
	// Be honest about Google sources: the adapters are real, but cannot connect
	// until OAuth plus the dedicated token/state keys are configured.
	if !googleOAuthReady() {
		for i := range connectors {
			if isGoogleOAuthConnector(connectors[i].ConnectorKey) {
				connectors[i].AdapterStatus = AdapterNotImplemented
				connectors[i].StatusReason = "real read-only Google OAuth adapter is implemented but GOOGLE_OAUTH_* or the dedicated HAI OAuth encryption/signing keys are not set"
			}
		}
	}
	// Same honesty for Trello: the live read-only REST adapter is implemented but
	// cannot connect until least-privilege credentials are configured.
	if !trelloConfigured() {
		for i := range connectors {
			if connectors[i].ConnectorKey == trelloConnectorKey {
				connectors[i].AdapterStatus = AdapterNotImplemented
				connectors[i].StatusReason = "real read-only Trello REST adapter is implemented but TRELLO_API_KEY/TRELLO_READ_TOKEN are not set, so it cannot connect yet"
			}
		}
	}
	if _, err := odooJSON2ConfigFromEnv(); err != nil {
		for i := range connectors {
			if connectors[i].ConnectorKey == odooJSON2ConnectorKey {
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "live read-only Odoo JSON-2 adapter is implemented but configuration is incomplete: " + err.Error()
			}
		}
	}
	if _, err := shareTConfigFromEnv(); err != nil {
		for i := range connectors {
			if connectors[i].ConnectorKey == shareTConnectorKey {
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "live read-only ShareT adapter is implemented but configuration is incomplete: " + err.Error()
			}
		}
	}
	if _, err := cloudQuerySummaryConfigFromEnv(); err != nil {
		for i := range connectors {
			if connectors[i].ConnectorKey == cloudQuerySummaryConnectorKey {
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "local CloudQuery sync-summary adapter is implemented but configuration is incomplete: " + err.Error()
			}
		}
	}
	if _, err := airbyteInventoryConfigFromEnv(); err != nil {
		for i := range connectors {
			if connectors[i].ConnectorKey == airbyteInventoryConnectorKey {
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "live read-only Airbyte inventory adapter is implemented but configuration is incomplete: " + err.Error()
			}
		}
	}
	if _, err := laroConfigFromEnv(); err != nil {
		for i := range connectors {
			if connectors[i].ConnectorKey == laroConnectorKey {
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "live read-only LARO adapter is implemented but configuration is incomplete: " + err.Error()
			}
		}
	}
	return connectors, nil
}

func (s *service) CreateSource(request CreateSourceRequest) (*models.ConnectedSource, error) {
	if err := s.ensureConnectors(); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	connectorKey := strings.TrimSpace(request.ConnectorKey)
	if connectorKey == "" {
		return nil, fmt.Errorf("connectorKey is required")
	}
	connector, err := s.connectorByKey(connectorKey)
	if err != nil {
		return nil, err
	}
	if connectorKey == odooJSON2ConnectorKey {
		if _, err := odooJSON2ConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("Odoo JSON-2 connector requires explicit configuration: %w", err)
		}
		if request.LocalOnly {
			return nil, fmt.Errorf("Odoo JSON-2 is a remote account bridge; localOnly must be false")
		}
		if strings.TrimSpace(request.SyncTarget) != "" {
			return nil, fmt.Errorf("Odoo JSON-2 endpoint is configured only through HAI_ODOO_BASE_URL; syncTarget must be empty")
		}
	}
	if connectorKey == shareTConnectorKey {
		if _, err := shareTConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("ShareT connector requires explicit configuration: %w", err)
		}
		if request.LocalOnly {
			return nil, fmt.Errorf("ShareT is a remote read-only source; localOnly must be false")
		}
		if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
			return nil, fmt.Errorf("ShareT must use the project_board category")
		}
		if strings.TrimSpace(request.SyncTarget) != "" {
			return nil, fmt.Errorf("ShareT endpoint is configured only through HAI_SHARET_BASE_URL; syncTarget must be empty")
		}
	}
	if connectorKey == cloudQuerySummaryConnectorKey {
		if _, err := cloudQuerySummaryConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("CloudQuery sync-summary connector requires explicit configuration: %w", err)
		}
		if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
			return nil, fmt.Errorf("CloudQuery sync summaries must use the cloud_inventory category")
		}
		if !request.LocalOnly {
			return nil, fmt.Errorf("CloudQuery sync summaries are local-only; localOnly must be true")
		}
		if strings.TrimSpace(request.SyncTarget) != "" {
			return nil, fmt.Errorf("CloudQuery summary path is configured only through HAI_CLOUDQUERY_SUMMARY_PATH; syncTarget must be empty")
		}
	}
	if connectorKey == airbyteInventoryConnectorKey {
		if _, err := airbyteInventoryConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("Airbyte inventory connector requires explicit configuration: %w", err)
		}
		if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
			return nil, fmt.Errorf("Airbyte inventory must use the connector_inventory category")
		}
		if !request.LocalOnly {
			return nil, fmt.Errorf("Airbyte inventory is local-only; localOnly must be true")
		}
		if strings.TrimSpace(request.SyncTarget) != "" {
			return nil, fmt.Errorf("Airbyte inventory endpoint is configured only through HAI_AIRBYTE_BASE_URL; syncTarget must be empty")
		}
	}
	if connectorKey == laroConnectorKey {
		if _, err := laroConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("LARO connector requires explicit configuration: %w", err)
		}
		if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
			return nil, fmt.Errorf("LARO must use the legal_case category")
		}
		if request.LocalOnly {
			return nil, fmt.Errorf("LARO is an authenticated account bridge; localOnly must be false")
		}
		if strings.TrimSpace(request.SyncTarget) != "" {
			return nil, fmt.Errorf("LARO endpoint is configured only through HAI_LARO_BASE_URL; syncTarget must be empty")
		}
	}
	if connectorKey == openSpecArtifactConnectorKey {
		if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
			return nil, fmt.Errorf("OpenSpec artifacts must use the code_spec category")
		}
		if !request.LocalOnly {
			return nil, fmt.Errorf("OpenSpec artifacts are local-only; localOnly must be true")
		}
		if strings.TrimSpace(request.SyncTarget) == "" {
			return nil, fmt.Errorf("OpenSpec artifacts require a selected project folder under CONNECTED_SOURCE_LOCAL_ROOT")
		}
		if _, err := resolveAllowedFolder(firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources"), request.SyncTarget); err != nil {
			return nil, fmt.Errorf("OpenSpec project folder is not allowed: %w", err)
		}
	}
	if connectorKey == projectInstructionsConnectorKey || connectorKey == fabricPatternsConnectorKey {
		label := "project instructions"
		if connectorKey == fabricPatternsConnectorKey {
			label = "Fabric patterns"
		}
		if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
			return nil, fmt.Errorf("%s must use the code_spec category", label)
		}
		if !request.LocalOnly {
			return nil, fmt.Errorf("%s are local-only; localOnly must be true", label)
		}
		if strings.TrimSpace(request.SyncTarget) == "" {
			return nil, fmt.Errorf("%s require a selected folder under CONNECTED_SOURCE_LOCAL_ROOT", label)
		}
		if _, err := resolveAllowedFolder(firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources"), request.SyncTarget); err != nil {
			return nil, fmt.Errorf("%s folder is not allowed: %w", label, err)
		}
	}
	if !connector.Enabled || !adapterIsUsable(connector.AdapterStatus) {
		return nil, fmt.Errorf("connector %s is registered but its real adapter is not implemented yet", connectorKey)
	}
	category := firstNonEmpty(request.Category, connector.Category)
	if category == "" {
		return nil, fmt.Errorf("category is required")
	}
	source := &models.ConnectedSource{
		OwnerIdentity:     strings.TrimSpace(request.OwnerIdentity),
		ConnectorKey:      connectorKey,
		Name:              name,
		Category:          category,
		Enabled:           request.Enabled,
		LocalOnly:         request.LocalOnly,
		SyncFrequency:     firstNonEmpty(request.SyncFrequency, "manual"),
		SyncTarget:        strings.TrimSpace(request.SyncTarget),
		DefaultProjectKey: strings.TrimSpace(request.DefaultProjectKey),
		IngestionModes:    joinValues(defaultModes(request.IngestionModes)),
		Permissions:       joinValues(minimalPermissions(category, request.Permissions)),
		ExcludePatterns:   joinValues(request.ExcludePatterns),
		Status:            "active",
	}
	if sourceUsesLocalFolder(connectorKey) && source.SyncTarget == "" {
		source.SyncTarget = "."
	}
	if !request.Enabled {
		source.Status = "paused"
	}
	created, err := s.repo.CreateSource(source)
	if err == nil {
		s.audit(created.ID, "source.connected", "connected source registered with minimal permissions")
	}
	return created, err
}

func (s *service) UpdateSource(id uuid.UUID, request UpdateSourceRequest) (*models.ConnectedSource, error) {
	source, err := s.repo.FindSource(id)
	if err != nil {
		return nil, err
	}
	if err := rejectRevokedSource(source); err != nil {
		return nil, err
	}
	if source.ConnectorKey == cloudQuerySummaryConnectorKey {
		if _, err := cloudQuerySummaryConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("CloudQuery sync-summary connector requires explicit configuration: %w", err)
		}
		if request.LocalOnly != nil && !*request.LocalOnly {
			return nil, fmt.Errorf("CloudQuery sync summaries are local-only; localOnly must remain true")
		}
		if request.SyncTarget != nil && strings.TrimSpace(*request.SyncTarget) != "" {
			return nil, fmt.Errorf("CloudQuery summary path is configured only through HAI_CLOUDQUERY_SUMMARY_PATH; syncTarget must remain empty")
		}
	}
	if source.ConnectorKey == shareTConnectorKey {
		if _, err := shareTConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("ShareT connector requires explicit configuration: %w", err)
		}
		if request.LocalOnly != nil && *request.LocalOnly {
			return nil, fmt.Errorf("ShareT is a remote read-only source; localOnly must remain false")
		}
		if request.SyncTarget != nil && strings.TrimSpace(*request.SyncTarget) != "" {
			return nil, fmt.Errorf("ShareT endpoint is configured only through HAI_SHARET_BASE_URL; syncTarget must remain empty")
		}
	}
	if source.ConnectorKey == airbyteInventoryConnectorKey {
		if _, err := airbyteInventoryConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("Airbyte inventory connector requires explicit configuration: %w", err)
		}
		if request.LocalOnly != nil && !*request.LocalOnly {
			return nil, fmt.Errorf("Airbyte inventory is local-only; localOnly must remain true")
		}
		if request.SyncTarget != nil && strings.TrimSpace(*request.SyncTarget) != "" {
			return nil, fmt.Errorf("Airbyte inventory endpoint is configured only through HAI_AIRBYTE_BASE_URL; syncTarget must remain empty")
		}
	}
	if source.ConnectorKey == laroConnectorKey {
		if _, err := laroConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("LARO connector requires explicit configuration: %w", err)
		}
		if request.LocalOnly != nil && *request.LocalOnly {
			return nil, fmt.Errorf("LARO is an authenticated account bridge; localOnly must remain false")
		}
		if request.SyncTarget != nil && strings.TrimSpace(*request.SyncTarget) != "" {
			return nil, fmt.Errorf("LARO endpoint is configured only through HAI_LARO_BASE_URL; syncTarget must remain empty")
		}
	}
	if source.ConnectorKey == openSpecArtifactConnectorKey {
		if request.LocalOnly != nil && !*request.LocalOnly {
			return nil, fmt.Errorf("OpenSpec artifacts are local-only; localOnly must remain true")
		}
		if request.SyncTarget != nil {
			if strings.TrimSpace(*request.SyncTarget) == "" {
				return nil, fmt.Errorf("OpenSpec artifacts require a selected project folder")
			}
			if _, err := resolveAllowedFolder(firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources"), *request.SyncTarget); err != nil {
				return nil, fmt.Errorf("OpenSpec project folder is not allowed: %w", err)
			}
		}
	}
	if source.ConnectorKey == projectInstructionsConnectorKey || source.ConnectorKey == fabricPatternsConnectorKey {
		label := "project instructions"
		if source.ConnectorKey == fabricPatternsConnectorKey {
			label = "Fabric patterns"
		}
		if request.LocalOnly != nil && !*request.LocalOnly {
			return nil, fmt.Errorf("%s are local-only; localOnly must remain true", label)
		}
		if request.SyncTarget != nil {
			if strings.TrimSpace(*request.SyncTarget) == "" {
				return nil, fmt.Errorf("%s require a selected folder", label)
			}
			if _, err := resolveAllowedFolder(firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources"), *request.SyncTarget); err != nil {
				return nil, fmt.Errorf("%s folder is not allowed: %w", label, err)
			}
		}
	}
	if strings.TrimSpace(request.Name) != "" {
		source.Name = strings.TrimSpace(request.Name)
	}
	if request.Enabled != nil {
		source.Enabled = *request.Enabled
		if source.Enabled {
			source.Status = "active"
		} else {
			source.Status = "paused"
		}
	}
	if request.LocalOnly != nil {
		source.LocalOnly = *request.LocalOnly
	}
	if request.SyncFrequency != "" {
		source.SyncFrequency = request.SyncFrequency
	}
	if request.SyncTarget != nil {
		source.SyncTarget = strings.TrimSpace(*request.SyncTarget)
	}
	if request.DefaultProjectKey != nil {
		source.DefaultProjectKey = strings.TrimSpace(*request.DefaultProjectKey)
	}
	if request.Permissions != nil {
		source.Permissions = joinValues(minimalPermissions(source.Category, request.Permissions))
	}
	if request.ExcludePatterns != nil {
		source.ExcludePatterns = joinValues(request.ExcludePatterns)
	}
	updated, err := s.repo.UpdateSource(source)
	if err == nil {
		s.audit(id, "source.updated", "source controls updated")
	}
	return updated, err
}

func (s *service) Sources(includeDisabled bool) ([]models.ConnectedSource, error) {
	return s.repo.FindSources(includeDisabled)
}

func (s *service) SourcesForOwner(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return []models.ConnectedSource{}, nil
	}
	return s.repo.FindSourcesForOwner(ownerIdentity, includeDisabled, identity.CanReadLegacyOwnerlessData(ownerIdentity))
}

func (s *service) OverviewForOwner(ownerIdentity, projectKey string, includeArchived bool) (*SourceOverview, error) {
	visibleSourceIDs, err := s.visibleSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return s.repo.FindOverviewForSources(sourceIDsFromSet(visibleSourceIDs), projectKey, includeArchived)
}

func (s *service) SyncJobs(sourceID *uuid.UUID, limit int) ([]models.SourceSyncJob, error) {
	return s.repo.FindSyncJobs(sourceID, limit)
}

func (s *service) SyncJobsForOwner(ownerIdentity string, limit int) ([]models.SourceSyncJob, error) {
	visibleSourceIDs, err := s.visibleSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return s.repo.FindSyncJobsForSources(sourceIDsFromSet(visibleSourceIDs), limit)
}

func (s *service) ConnectionHealth(sourceID uuid.UUID) (*ConnectionHealth, error) {
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return nil, err
	}
	health := &ConnectionHealth{
		SourceID: source.ID, ConnectorKey: source.ConnectorKey,
		Status: source.Status, Configured: true, LastSyncedAt: source.LastSyncedAt,
	}
	if source.Status == "revoked" || source.RevokedAt != nil {
		health.Status = "revoked"
		health.Reason = "source access was revoked"
		return health, nil
	}
	if !isGoogleOAuthConnector(source.ConnectorKey) {
		health.Authorized = source.Enabled
		health.Reason = "connector health is derived from its enabled and synchronization state"
		return health, nil
	}
	health.Configured = googleOAuthReady()
	if !health.Configured {
		health.Status = "configuration_required"
		health.Reason = "Google OAuth client, token encryption key, or state signing key is not configured"
		return health, nil
	}
	token, err := s.repo.FindOAuthToken(sourceID)
	if err != nil {
		health.Status = "disconnected"
		health.Reason = "no Google account grant is stored for this source"
		return health, nil
	}
	if !token.Expiry.IsZero() {
		health.TokenExpiry = &token.Expiry
	}
	requiredScope, err := requiredGoogleScope(source.ConnectorKey)
	if err != nil {
		return nil, err
	}
	if !hasOAuthScope(token.Scope, requiredScope) || len(token.RefreshToken) == 0 {
		health.Status = "reconnect_required"
		health.RequiresReconnect = true
		health.Reason = "stored Google grant is missing the required read scope or refresh credential"
		return health, nil
	}
	health.Authorized = true
	health.Status = "ready"
	health.Reason = "encrypted read-only Google grant is available"
	if source.ConnectorKey == gmailConnectorKey {
		if cursor, err := decodeGmailCursor(source.Cursor); err == nil {
			health.CursorPhase = cursor.Phase
		}
	} else if source.ConnectorKey == driveConnectorKey {
		if cursor, err := decodeDriveCursor(source.Cursor); err == nil {
			health.CursorPhase = cursor.Phase
		}
	} else if source.ConnectorKey == contactsConnectorKey {
		if cursor, err := decodeContactsCursor(source.Cursor); err == nil {
			health.CursorPhase = cursor.Phase
		}
	} else if cursor, err := decodeCalendarCursor(source.Cursor); err == nil {
		health.CursorPhase = cursor.Phase
	}
	return health, nil
}

func (s *service) Sync(sourceID uuid.UUID, request ImportRequest) (*SyncResult, error) {
	return s.SyncContext(context.Background(), sourceID, request)
}

func (s *service) SyncContext(ctx context.Context, sourceID uuid.UUID, request ImportRequest) (*SyncResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.beginSync(sourceID) {
		return nil, ErrSyncInProgress
	}
	defer s.endSync(sourceID)

	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return nil, err
	}
	if err := rejectRevokedSource(source); err != nil {
		return nil, err
	}
	if !source.Enabled || source.Status == "paused" {
		return nil, fmt.Errorf("source is not enabled for sync")
	}
	if source.ConnectorKey == "whisper-audio" && !request.controlledTranscription {
		return nil, fmt.Errorf("whisper-audio sources must use the controlled transcription route")
	}
	if source.ConnectorKey == cloudQuerySummaryConnectorKey {
		if !source.LocalOnly || strings.TrimSpace(source.SyncTarget) != "" {
			return nil, fmt.Errorf("CloudQuery sync summaries must remain local-only with the environment-configured summary path")
		}
		if len(request.Items) != 0 {
			return nil, fmt.Errorf("CloudQuery sync summaries must be read from the configured local summary file; manual items are not accepted")
		}
	}
	if source.ConnectorKey == shareTConnectorKey {
		if source.LocalOnly || strings.TrimSpace(source.SyncTarget) != "" {
			return nil, fmt.Errorf("ShareT must remain remote and use only the environment-configured endpoint")
		}
		if len(request.Items) != 0 {
			return nil, fmt.Errorf("ShareT links must be read from the configured API; manual items are not accepted")
		}
	}
	if source.ConnectorKey == airbyteInventoryConnectorKey {
		if !source.LocalOnly || strings.TrimSpace(source.SyncTarget) != "" {
			return nil, fmt.Errorf("Airbyte inventory must remain local-only with the environment-configured endpoint")
		}
		if len(request.Items) != 0 {
			return nil, fmt.Errorf("Airbyte inventory must be read from the configured local API; manual items are not accepted")
		}
	}
	if source.ConnectorKey == openSpecArtifactConnectorKey {
		if !source.LocalOnly || strings.TrimSpace(source.SyncTarget) == "" {
			return nil, fmt.Errorf("OpenSpec artifacts must remain local-only with a selected project folder")
		}
		if len(request.Items) != 0 {
			return nil, fmt.Errorf("OpenSpec artifacts must be read from the selected project folder; manual items are not accepted")
		}
		if requested := strings.TrimSpace(request.FolderPath); requested != "" && requested != strings.TrimSpace(source.SyncTarget) {
			return nil, fmt.Errorf("OpenSpec sync must use its registered project folder")
		}
		request.FolderPath = source.SyncTarget
		request.ProjectKey = firstNonEmpty(request.ProjectKey, source.DefaultProjectKey)
	}
	if isManualPlanningContextOnlyConnector(source.ConnectorKey) {
		if !source.LocalOnly || strings.TrimSpace(source.SyncTarget) == "" {
			return nil, fmt.Errorf("%s must remain local-only with a selected folder", source.ConnectorKey)
		}
		if len(request.Items) != 0 {
			return nil, fmt.Errorf("%s must be read from the selected folder; manual items are not accepted", source.ConnectorKey)
		}
		if requested := strings.TrimSpace(request.FolderPath); requested != "" && requested != strings.TrimSpace(source.SyncTarget) {
			return nil, fmt.Errorf("%s sync must use its registered folder", source.ConnectorKey)
		}
		request.FolderPath = source.SyncTarget
		request.ProjectKey = firstNonEmpty(request.ProjectKey, source.DefaultProjectKey)
	}
	if !sourceHasNativeAdapter(source.ConnectorKey) && len(request.Items) == 0 {
		return nil, fmt.Errorf("connector %s has no real sync adapter yet; provide explicit manual import items or use local-folder", source.ConnectorKey)
	}
	if sourceUsesLocalFolder(source.ConnectorKey) {
		request.FolderPath = firstNonEmpty(request.FolderPath, source.SyncTarget, ".")
		request.ProjectKey = firstNonEmpty(request.ProjectKey, source.DefaultProjectKey)
		source.SyncTarget = request.FolderPath
		source.DefaultProjectKey = request.ProjectKey
	}
	mode := firstNonEmpty(request.Mode, ModeManualImport)
	started := time.Now().UTC()
	job, err := s.repo.CreateSyncJob(&models.SourceSyncJob{
		SourceID:     sourceID,
		Mode:         mode,
		Status:       "running",
		CursorBefore: source.Cursor,
		CursorAfter:  source.Cursor,
		StartedAt:    started,
	})
	if err != nil {
		return nil, err
	}

	extractions := []models.SourceExtraction{}
	pursuitOutcomes := []PursuitRoutingOutcome{}
	lifeGraphProjections := []LifeGraphProjectionOutcome{}
	added := 0
	updated := 0
	failed := 0
	itemErrors := []string{}
	warnings := []string{}
	items := request.Items
	recordFailure := func(item ImportItem, stage string, err error) {
		failed++
		if len(itemErrors) < maxSyncErrorDetails {
			itemErrors = append(itemErrors, itemFailure(item, stage, err))
		}
	}
	abortCanceledSync := func() error {
		errContext := ctx.Err()
		if errContext == nil {
			return nil
		}
		now := time.Now().UTC()
		job.Status = "failed"
		job.Message = "sync canceled before all source items were processed; cursor retained for retry"
		job.ItemsSeen = len(items)
		job.ItemsAdded = added
		job.ItemsUpdated = updated
		job.ItemsFailed = failed
		job.CursorAfter = job.CursorBefore
		job.CompletedAt = &now
		_, _ = s.repo.UpdateSyncJob(job)
		s.audit(sourceID, "source.sync_canceled", job.Message)
		return errContext
	}
	adapterCursor := ""
	adapterCursorAuthoritative := false
	if len(items) == 0 && sourceUsesLocalFolder(source.ConnectorKey) {
		items, err = s.localFolderItems(source, request)
		items = filterConnectorLocalItems(items, source.ConnectorKey)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == "json-feed" {
		items, adapterCursor, err = fetchJSONFeed(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == "github" {
		items, adapterCursor, err = fetchGitHubSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == gmailConnectorKey {
		items, adapterCursor, err = s.fetchGmailSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == driveConnectorKey {
		items, adapterCursor, err = s.fetchDriveSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == contactsConnectorKey {
		items, adapterCursor, err = s.fetchContactsSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == calendarConnectorKey {
		items, adapterCursor, err = s.fetchCalendarSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == trelloConnectorKey {
		items, adapterCursor, err = fetchTrelloSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == odooJSON2ConnectorKey {
		items, adapterCursor, err = fetchOdooJSON2Source(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
		s.audit(sourceID, "source.odoo_json2_read", fmt.Sprintf("read %d bounded Odoo JSON-2 record(s) through the configured model allowlist", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == shareTConnectorKey {
		items, adapterCursor, err = fetchShareTSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
		s.audit(sourceID, "source.sharet_read", fmt.Sprintf("read %d bounded ShareT link record(s) through a read-only connector credential", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == cloudQuerySummaryConnectorKey {
		items, adapterCursor, err = fetchCloudQuerySummary(source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
		s.audit(sourceID, "source.cloudquery_summary_read", fmt.Sprintf("read %d bounded CloudQuery sync summary record(s) from the configured local summary file", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == airbyteInventoryConnectorKey {
		items, adapterCursor, err = fetchAirbyteInventory(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
		s.audit(sourceID, "source.airbyte_inventory_read", fmt.Sprintf("read %d bounded Airbyte source and connection inventory record(s) from approved workspaces", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == laroConnectorKey {
		var laroResult laroFetchResult
		laroResult, err = fetchLAROSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
		items = laroResult.items
		adapterCursor = laroResult.nextCursor
		adapterCursorAuthoritative = true
		if laroResult.cursorReset {
			s.audit(sourceID, "source.laro_cursor_reset", "LARO rejected the stored incremental cursor; HAI completed one bounded read without it and cleared the stale cursor")
		}
		s.audit(sourceID, "source.laro_read", fmt.Sprintf("read %d bounded, owner-scoped LARO legal record(s)", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == openSpecArtifactConnectorKey {
		items, err = s.openSpecArtifactItems(source, request)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
		s.audit(sourceID, "source.openspec_artifacts_read", fmt.Sprintf("read %d bounded OpenSpec change artifact bundle(s) from the selected local project", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == projectInstructionsConnectorKey {
		items, err = s.projectInstructionItems(source, request)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
		s.audit(sourceID, "source.project_instructions_read", fmt.Sprintf("read %d untrusted project instruction file(s) from the selected local project", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == fabricPatternsConnectorKey {
		items, err = s.fabricPatternItems(source, request)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
		s.audit(sourceID, "source.fabric_patterns_read", fmt.Sprintf("read %d bounded untrusted Fabric prompt pattern(s) from the selected local folder", len(items)))
	}
	if source.ConnectorKey == "whatsapp-export" {
		items, err = s.whatsAppExportItems(source, request)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = err.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", err.Error())
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == "odoo-herp" {
		items = odooHERPItems(source, request)
		s.audit(sourceID, "source.odoo_herp_modeled", fmt.Sprintf("modeled %d Odoo/HERP app domain(s) into governed source records", len(items)))
	}
	if source.ConnectorKey == calendarConnectorKey {
		existingItems, errExisting := s.repo.FindRawItems(source.ID)
		if errExisting != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = "calendar conflict planning could not read cached event records: " + errExisting.Error()
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", job.Message)
			return nil, errExisting
		}
		items = appendCalendarConflictItems(existingItems, items, time.Now().UTC())
		for index := range items {
			if strings.HasPrefix(items[index].ItemType, "google_calendar_conflict") && strings.TrimSpace(items[index].ProjectKey) == "" {
				items[index].ProjectKey = firstNonEmpty(source.DefaultProjectKey, "Robert-life-os")
			}
		}
	}
	for index, item := range items {
		if errContext := abortCanceledSync(); errContext != nil {
			return nil, errContext
		}
		if shouldExclude(source.ExcludePatterns, item.Title+" "+item.SourceURI) {
			continue
		}
		raw, wasAdded, errItem := s.upsertRawItem(source, item, index)
		if errItem != nil {
			recordFailure(item, "raw item persistence failed", errItem)
			continue
		}
		if wasAdded {
			added++
		} else {
			updated++
		}
		extraction, errExtract := s.extractAndStore(source, raw, item.Content)
		if errExtract != nil {
			recordFailure(item, "extraction failed", errExtract)
			continue
		}
		if errIndex := s.indexExtraction(ctx, extraction); errIndex != nil {
			recordFailure(item, "index update failed", errIndex)
			continue
		}
		projection, errProjection := s.projectExtractionToLifeGraph(ctx, source, extraction)
		if errProjection != nil {
			warning := itemFailure(item, "life graph projection failed", errProjection)
			if len(warnings) < maxSyncErrorDetails {
				warnings = append(warnings, warning)
			}
			s.audit(sourceID, "life_graph.projection_failed", compact(safety.RedactSecrets(warning), 300))
		} else if projection != nil {
			if len(lifeGraphProjections) < maxSyncPursuitOutcomes {
				lifeGraphProjections = append(lifeGraphProjections, *projection)
			}
			s.audit(sourceID, "life_graph.projected", "projected extraction "+extraction.ID.String()+" as an advisory source-backed document")
		}
		if extraction.ContentType == "google_calendar_event_cancelled" || extraction.ContentType == "google_calendar_conflict_resolved" {
			reason := "Google Calendar reports that this source event was cancelled; stop source-derived work pending owner review"
			stage := "calendar cancellation could not stop prior source-derived work"
			auditMessage := "cancelled calendar event stopped prior source-derived work before review"
			if extraction.ContentType == "google_calendar_conflict_resolved" {
				reason = "the source-backed Calendar overlap is no longer present; stop the stale conflict workflow while preserving audit history"
				stage = "resolved calendar conflict could not stop stale source-derived work"
				auditMessage = "resolved calendar conflict stopped its stale source-derived workflow"
			}
			if errRetract := s.retractWorkflowForExtraction(extraction, reason); errRetract != nil {
				recordFailure(item, stage, errRetract)
				continue
			}
			s.audit(sourceID, "workflow.calendar_event_retracted", auditMessage)
		}
		if extraction.ContentType == "google_calendar_conflict_resolved" {
			extractions = append(extractions, *extraction)
			continue
		}
		supplementalSignal := firstNonEmpty(calendarPreparationSignal(raw, time.Now().UTC()), calendarConflictWorkflowSignal(raw))
		pursuitOutcome, errWorkflow := s.createWorkflowFromExtractionWithSignal(source, extraction, supplementalSignal)
		if pursuitOutcome != nil && len(pursuitOutcomes) < maxSyncPursuitOutcomes {
			pursuitOutcomes = append(pursuitOutcomes, *pursuitOutcome)
		}
		if errWorkflow != nil {
			recordFailure(item, "workflow intake failed", errWorkflow)
			continue
		}
		extractions = append(extractions, *extraction)
		s.storeUsefulMemory(source, extraction)
	}
	if errContext := abortCanceledSync(); errContext != nil {
		return nil, errContext
	}

	now := time.Now().UTC()
	job.ItemsSeen = len(items)
	job.ItemsAdded = added
	job.ItemsUpdated = updated
	job.ItemsFailed = failed
	job.CursorAfter = source.Cursor
	if failed == 0 {
		source.LastSyncedAt = &now
		if adapterCursorAuthoritative {
			source.Cursor = adapterCursor
		} else {
			source.Cursor = firstNonEmpty(adapterCursor, fmt.Sprintf("%s:%d", now.Format(time.RFC3339), len(items)))
		}
		job.Status = "completed"
		job.CursorAfter = source.Cursor
		job.Message = "sync completed with cached extraction and provenance links"
	} else if len(extractions) > 0 {
		job.Status = "partial_failure"
		job.Message = fmt.Sprintf("sync partially completed; %d item(s) failed and the cursor was retained for retry", failed)
	} else {
		job.Status = "failed"
		job.Message = fmt.Sprintf("sync failed; %d item(s) failed and the cursor was retained for retry", failed)
	}
	if _, errSource := s.repo.UpdateSource(source); errSource != nil {
		failed++
		job.ItemsFailed = failed
		job.Status = "failed"
		job.CursorAfter = job.CursorBefore
		job.Message = "sync result could not update source state; cursor was not confirmed"
		if len(itemErrors) < maxSyncErrorDetails {
			itemErrors = append(itemErrors, "source state update failed: "+compact(errSource.Error(), 220))
		}
	}
	job.CompletedAt = &now
	job, err = s.repo.UpdateSyncJob(job)
	if err == nil {
		action := "source.synced"
		if job.Status != "completed" {
			action = "source.sync_partial_failure"
		}
		s.audit(sourceID, action, job.Message)
	}
	return &SyncResult{Job: *job, Extractions: extractions, PursuitOutcomes: pursuitOutcomes, LifeGraphProjections: lifeGraphProjections, Message: job.Message, Errors: itemErrors, Warnings: warnings}, err
}

// DueSources returns the enabled sources whose schedule has come due at now.
func (s *service) DueSources(now time.Time) ([]models.ConnectedSource, error) {
	sources, err := s.repo.FindSources(false)
	if err != nil {
		return nil, err
	}
	due := make([]models.ConnectedSource, 0, len(sources))
	for _, item := range sources {
		if ok, _ := scheduledSourceDue(item, now); ok {
			due = append(due, item)
		}
	}
	return due, nil
}

func (s *service) RunDueScheduledSyncs(now time.Time) (*ScheduledSyncRun, error) {
	sources, err := s.repo.FindSources(false)
	if err != nil {
		return nil, err
	}
	return s.runDueScheduledSyncs(context.Background(), now, sources)
}

// RunDueScheduledSyncsForOwner refreshes only sources explicitly owned by the
// authenticated user. Ownerless legacy sources remain readable for local
// compatibility but are never modified by an owner-scoped task request.
func (s *service) RunDueScheduledSyncsForOwner(now time.Time, ownerIdentity string) (*ScheduledSyncRun, error) {
	return s.RunDueScheduledSyncsForOwnerContext(context.Background(), now, ownerIdentity)
}

func (s *service) RunDueScheduledSyncsForOwnerContext(ctx context.Context, now time.Time, ownerIdentity string) (*ScheduledSyncRun, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required for owner-scoped scheduled source sync")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sources, err := s.repo.FindSourcesForOwner(ownerIdentity, false, false)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.runDueScheduledSyncs(ctx, now, sources)
}

func (s *service) runDueScheduledSyncs(ctx context.Context, now time.Time, sources []models.ConnectedSource) (*ScheduledSyncRun, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	run := &ScheduledSyncRun{Checked: len(sources)}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return run, err
		}
		due, reason := scheduledSourceDue(source, now)
		if !due {
			run.Skipped++
			if reason != "" {
				run.Messages = append(run.Messages, fmt.Sprintf("%s skipped: %s", source.Name, reason))
			}
			continue
		}
		run.Due++
		result, errSync := s.SyncContext(ctx, source.ID, ImportRequest{
			Mode:       ModeScheduledSync,
			FolderPath: source.SyncTarget,
			ProjectKey: source.DefaultProjectKey,
		})
		if errSync != nil {
			if errors.Is(errSync, context.Canceled) || errors.Is(errSync, context.DeadlineExceeded) {
				return run, errSync
			}
			if errors.Is(errSync, ErrSyncInProgress) {
				run.Skipped++
				run.Messages = append(run.Messages, fmt.Sprintf("%s skipped: sync already in progress", source.Name))
				continue
			}
			run.Failed++
			run.Messages = append(run.Messages, fmt.Sprintf("%s failed: %s", source.Name, errSync.Error()))
			s.createSyncFailureWorkflow(source, errSync.Error())
			continue
		}
		if result.Job.Status != "completed" {
			run.Failed++
			run.Messages = append(run.Messages, fmt.Sprintf("%s %s: %d seen, %d failed", source.Name, result.Job.Status, result.Job.ItemsSeen, result.Job.ItemsFailed))
			s.createSyncFailureWorkflow(source, result.Job.Message)
			continue
		}
		run.Completed++
		run.Messages = append(run.Messages, fmt.Sprintf("%s synced: %d seen, %d added, %d updated", source.Name, result.Job.ItemsSeen, result.Job.ItemsAdded, result.Job.ItemsUpdated))
	}
	return run, nil
}

func (s *service) createSyncFailureWorkflow(source models.ConnectedSource, reason string) {
	if s.workflowService == nil {
		return
	}
	record, err := s.intakeWorkflow(workflow.IntakeRequest{
		OwnerIdentity: source.OwnerIdentity,
		Input: strings.Join([]string{
			"Connected source sync failed for " + source.Name + ".",
			"Connector: " + source.ConnectorKey + ".",
			"Failure: " + compact(safety.RedactSecrets(reason), 320),
			"Required action: inspect connector health, permissions, target availability, and retry policy before resuming autonomous ingestion.",
		}, "\n"),
		ProjectKey:     source.DefaultProjectKey,
		SourceType:     "source_sync",
		SourceID:       source.ID.String(),
		SourceURI:      safety.RedactURL(source.SyncTarget),
		SourceLabel:    source.Name,
		ContentType:    "operational_failure",
		Trigger:        "scheduled_source_sync_failed",
		Actor:          "source-scheduler",
		RequiresReview: true,
		ReviewReason:   "background source ingestion failed and requires operator review",
	})
	if err != nil {
		if _, pending := pursuit.IsCandidatePending(err); pending {
			s.audit(source.ID, "pursuit.intake_deferred", "source sync failure created or matched a pursuit candidate; workflow creation awaits explicit acceptance")
			return
		}
		if errors.Is(err, pursuit.ErrLifecycleRouterRequired) {
			s.audit(source.ID, "pursuit.intake_deferred", "source sync failure retained; configured pursuit linker is missing the lifecycle router")
			return
		}
		s.audit(source.ID, "source.failure_workflow_failed", compact(err.Error(), 260))
		return
	}
	if record != nil && !s.routesWorkflowThroughPursuits() {
		s.autoLinkPursuitWorkflow(&source, nil, record, "Connected source sync failed for "+source.Name+". "+reason)
	}
	s.audit(source.ID, "source.failure_workflow_created", "scheduled sync failure routed to workflow review")
}

func (s *service) Reindex(sourceID uuid.UUID) (*SyncResult, error) {
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return nil, err
	}
	if err := rejectRevokedSource(source); err != nil {
		return nil, err
	}
	items, err := s.repo.FindRawItems(sourceID)
	if err != nil {
		return nil, err
	}
	request := ImportRequest{Mode: ModeIncrementalSync}
	for _, item := range items {
		request.Items = append(request.Items, ImportItem{
			ExternalID: item.ExternalID,
			Title:      item.Title,
			Content:    firstNonEmpty(item.Content, item.Metadata),
			SourceURI:  item.SourceURI,
			ItemType:   item.ItemType,
			ProjectKey: item.ProjectKey,
			Metadata:   item.Metadata,
		})
	}
	result, err := s.Sync(source.ID, request)
	if err == nil {
		s.audit(sourceID, "source.reindexed", "source re-indexed from cached raw content")
	}
	return result, err
}

func (s *service) Pause(sourceID uuid.UUID, paused bool) (*models.ConnectedSource, error) {
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return nil, err
	}
	if err := rejectRevokedSource(source); err != nil {
		return nil, err
	}
	source.Enabled = !paused
	if paused {
		source.Status = "paused"
	} else {
		source.Status = "active"
	}
	updated, err := s.repo.UpdateSource(source)
	if err == nil {
		s.audit(sourceID, "source.pause", fmt.Sprintf("source paused=%v", paused))
	}
	return updated, err
}

func (s *service) Revoke(uuid.UUID) (*models.ConnectedSource, error) {
	return nil, ErrDestructiveAuthorizationRequired
}

func (s *service) RevokeAuthorized(
	ctx context.Context,
	sourceID uuid.UUID,
	auth DestructiveEffectAuthorization,
) (*models.ConnectedSource, error) {
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return nil, err
	}
	if err := rejectRevokedSource(source); err != nil {
		return nil, err
	}
	if strings.TrimSpace(source.OwnerIdentity) == "" ||
		strings.TrimSpace(source.OwnerIdentity) != strings.TrimSpace(auth.OwnerIdentity) {
		return nil, ErrDestructiveOwnerMismatch
	}
	if err := s.authorizeDestructiveEffect(ctx, auth, *source, nil); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	updated, err := s.repo.RevokeSource(source, auth.OwnerIdentity, now)
	if err == nil {
		s.audit(sourceID, "source.revoked", "source access revoked")
	}
	return updated, err
}

func (s *service) Search(request SearchRequest) (*SearchResult, error) {
	return s.SearchContext(context.Background(), request)
}

func (s *service) SearchContext(ctx context.Context, request SearchRequest) (*SearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	visibleSourceIDs, err := s.visibleSourceIDsExcluding(request.OwnerIdentity, request.ExcludeConnectorKeys)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(request.ExcludeConnectorKeys) == 0 && s.semanticService != nil && s.semanticService.Enabled() {
		matches, semanticErr := s.semanticService.Search(ctx, semantic.SearchRequest{
			OwnerIdentity: request.OwnerIdentity, Query: request.Query, ProjectKey: request.ProjectKey,
			Limit: limit, IncludeSensitive: request.IncludeSensitive,
		})
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if semanticErr == nil && len(matches) > 0 {
			ranked := make([]RankedExtraction, 0, len(matches))
			for _, match := range matches {
				// The semantic index is a cache, not an authority boundary. Re-check
				// the parent source on every retrieval so a revocation takes effect
				// immediately even if an old embedding still exists.
				if !visibleSourceIDs[match.Extraction.SourceID] {
					continue
				}
				ranked = append(ranked, RankedExtraction{
					Extraction: match.Extraction, Score: match.Similarity,
					Explanation: "local pgvector cosine similarity; source ownership and sensitivity filters applied",
				})
			}
			if len(ranked) > 0 {
				return &SearchResult{Query: request.Query, ProjectKey: request.ProjectKey, UsedContext: ranked,
					Explanation: fmt.Sprintf("Retrieved %d source-backed records through local pgvector semantic retrieval; owner, source-revocation, project, archive, and sensitivity filters were enforced.", len(ranked))}, nil
			}
		}
		if semanticErr != nil && (errors.Is(semanticErr, context.Canceled) || errors.Is(semanticErr, context.DeadlineExceeded) || ctx.Err() != nil) {
			return nil, firstContextError(ctx, semanticErr)
		}
		// A semantic failure falls back to the existing bounded keyword search.
		// It is deliberately not attached to an arbitrary source audit record.
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	extractions, err := s.repo.FindExtractionsForSources(sourceIDsFromSet(visibleSourceIDs), strings.TrimSpace(request.ProjectKey), false)
	if err != nil {
		return nil, err
	}
	ranked := []RankedExtraction{}
	for _, extraction := range extractions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !visibleSourceIDs[extraction.SourceID] {
			continue
		}
		if extraction.Sensitive && !request.IncludeSensitive {
			continue
		}
		score, explanation := scoreExtraction(extraction, request)
		if score <= 0.12 {
			continue
		}
		ranked = append(ranked, RankedExtraction{
			Extraction:  extraction,
			Score:       math.Round(score*1000) / 1000,
			Explanation: explanation,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return &SearchResult{
		Query:       request.Query,
		ProjectKey:  request.ProjectKey,
		UsedContext: ranked,
		Explanation: fmt.Sprintf("Retrieved %d relevant connected-source records from %d visible cached extractions; unrelated, other-user, and sensitive records were not loaded.", len(ranked), len(extractions)),
	}, nil
}

func firstContextError(ctx context.Context, fallback error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return fallback
}

func (s *service) visibleSourceIDs(ownerIdentity string) (map[uuid.UUID]bool, error) {
	return s.visibleSourceIDsExcluding(ownerIdentity, nil)
}

func (s *service) visibleSourceIDsExcluding(ownerIdentity string, excludedConnectorKeys []string) (map[uuid.UUID]bool, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return map[uuid.UUID]bool{}, nil
	}
	sources, err := s.repo.FindSourcesForOwner(ownerIdentity, true, identity.CanReadLegacyOwnerlessData(ownerIdentity))
	if err != nil {
		return nil, err
	}
	excluded := make(map[string]bool, len(excludedConnectorKeys))
	for _, connectorKey := range excludedConnectorKeys {
		if connectorKey = strings.TrimSpace(connectorKey); connectorKey != "" {
			excluded[connectorKey] = true
		}
	}
	visible := make(map[uuid.UUID]bool, len(sources))
	for _, source := range sources {
		if excluded[strings.TrimSpace(source.ConnectorKey)] {
			continue
		}
		if source.RevokedAt != nil || strings.EqualFold(strings.TrimSpace(source.Status), "revoked") {
			continue
		}
		visible[source.ID] = true
	}
	return visible, nil
}

func sourceIDsFromSet(sourceIDs map[uuid.UUID]bool) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(sourceIDs))
	for id := range sourceIDs {
		result = append(result, id)
	}
	return result
}

func (s *service) Extractions(projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	return s.repo.FindExtractions(projectKey, includeArchived)
}

func (s *service) ExtractionsForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	visibleSourceIDs, err := s.visibleSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return s.repo.FindExtractionsForSources(sourceIDsFromSet(visibleSourceIDs), projectKey, includeArchived)
}

func (s *service) ExtractionsForOwnerLimit(ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, error) {
	visibleSourceIDs, err := s.visibleSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return s.repo.FindRecentExtractionsForSources(
		sourceIDsFromSet(visibleSourceIDs),
		strings.TrimSpace(projectKey),
		includeArchived,
		limit,
	)
}

func (s *service) ExtractionForOwner(ownerIdentity string, id uuid.UUID) (*models.SourceExtraction, error) {
	return s.repo.FindExtractionForOwner(id, ownerIdentity)
}

func (s *service) UpdateExtraction(id uuid.UUID, request models.SourceExtraction) (*models.SourceExtraction, error) {
	extraction, err := s.repo.FindExtraction(id)
	if err != nil {
		return nil, err
	}
	before := *extraction
	if request.Text != "" {
		extraction.Text = request.Text
	}
	if request.Summary != "" {
		extraction.Summary = request.Summary
	}
	if request.ProjectKey != "" {
		extraction.ProjectKey = request.ProjectKey
	}
	extraction.Entities = request.Entities
	extraction.Dates = request.Dates
	extraction.Tasks = request.Tasks
	extraction.Decisions = request.Decisions
	extraction.FollowUps = request.FollowUps
	extraction.Sensitive = request.Sensitive
	extraction.Uncertain = request.Uncertain
	if firstNonEmpty(extraction.Tasks, extraction.FollowUps) == "" {
		if err := s.retractWorkflowForExtraction(extraction, "corrected extraction no longer contains an actionable task or follow-up"); err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.SaveExtraction(extraction)
	if err == nil && updated != nil {
		s.audit(extraction.SourceID, "extraction.corrected", "operator corrected extraction")
		if errIndex := s.indexExtraction(context.Background(), updated); errIndex != nil {
			return updated, fmt.Errorf("extraction corrected but index update failed: %w", errIndex)
		}
		if errWorkflow := s.reconcileWorkflowFromExtraction(updated); errWorkflow != nil {
			return updated, fmt.Errorf("extraction corrected but workflow reconciliation failed: %w", errWorkflow)
		}
		if source, errSource := s.repo.FindSource(updated.SourceID); errSource == nil {
			if _, errProjection := s.projectExtractionToLifeGraph(context.Background(), source, updated); errProjection != nil {
				s.audit(updated.SourceID, "life_graph.projection_failed", compact(safety.RedactSecrets(errProjection.Error()), 300))
			} else if s.lifeOntologyProjector != nil && strings.TrimSpace(source.OwnerIdentity) != "" {
				s.audit(updated.SourceID, "life_graph.projected", "projected corrected extraction "+updated.ID.String()+" as a new immutable graph observation")
			}
		}
		s.rememberExtractionCorrection(&before, updated)
	}
	return updated, err
}

func (s *service) ArchiveExtraction(id uuid.UUID, archived bool) (*models.SourceExtraction, error) {
	extraction, err := s.repo.FindExtraction(id)
	if err != nil {
		return nil, err
	}
	if archived {
		if err := s.retractWorkflowForExtraction(extraction, "source extraction was archived by the operator"); err != nil {
			return nil, err
		}
	}
	extraction.Archived = archived
	updated, err := s.repo.SaveExtraction(extraction)
	if err == nil {
		s.audit(extraction.SourceID, "extraction.archived", fmt.Sprintf("archived=%v", archived))
		if source, errSource := s.repo.FindSource(updated.SourceID); errSource == nil {
			if _, errProjection := s.projectExtractionToLifeGraph(context.Background(), source, updated); errProjection != nil {
				s.audit(updated.SourceID, "life_graph.projection_failed", compact(safety.RedactSecrets(errProjection.Error()), 300))
			} else if s.lifeOntologyProjector != nil && strings.TrimSpace(source.OwnerIdentity) != "" {
				s.audit(updated.SourceID, "life_graph.projected", "projected extraction archive state "+updated.ID.String()+" as a new immutable graph observation")
			}
		}
	}
	return updated, err
}

func (s *service) DeleteExtraction(uuid.UUID) error {
	return ErrDestructiveAuthorizationRequired
}

func (s *service) DeleteExtractionAuthorized(
	ctx context.Context,
	id uuid.UUID,
	auth DestructiveEffectAuthorization,
) error {
	extraction, err := s.repo.FindExtraction(id)
	if err != nil {
		return err
	}
	source, err := s.repo.FindSource(extraction.SourceID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(source.OwnerIdentity) == "" ||
		strings.TrimSpace(source.OwnerIdentity) != strings.TrimSpace(auth.OwnerIdentity) {
		return ErrDestructiveOwnerMismatch
	}
	if err := s.authorizeDestructiveEffect(ctx, auth, *source, extraction); err != nil {
		return err
	}
	if err := s.retractWorkflowForExtraction(extraction, "source extraction was deleted by the operator"); err != nil {
		return err
	}
	if decision := s.evaluateSourceEmergencyStop(); decision.Active {
		return fmt.Errorf(
			"%w: %s",
			ErrSourceEmergencyStopActive,
			safety.RedactSecrets(strings.TrimSpace(decision.Reason)),
		)
	}
	if err := s.repo.DeleteExtractionForOwner(
		extraction,
		source,
		auth.OwnerIdentity,
	); err != nil {
		return err
	}
	s.audit(extraction.SourceID, "extraction.deleted", "operator deleted extraction")
	return nil
}

func (s *service) AuditLogs(sourceID *uuid.UUID, limit int) ([]models.SourceAuditLog, error) {
	return s.repo.FindAuditLogs(sourceID, limit)
}

func (s *service) AuditLogsForOwner(ownerIdentity string, limit int) ([]models.SourceAuditLog, error) {
	visibleSourceIDs, err := s.visibleSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return s.repo.FindAuditLogsForSources(sourceIDsFromSet(visibleSourceIDs), limit)
}

func (s *service) localFolderItems(source *models.ConnectedSource, request ImportRequest) ([]ImportItem, error) {
	root := firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources")
	folder, err := resolveAllowedFolder(root, request.FolderPath)
	if err != nil {
		return nil, err
	}
	limit := firstPositiveInt(request.Limit, envInt("CONNECTED_SOURCE_FILE_LIMIT", 100))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	maxBytes := firstPositiveInt64(request.MaxBytes, envInt64("CONNECTED_SOURCE_MAX_BYTES", 1024*1024))
	if maxBytes <= 0 || maxBytes > 10*1024*1024 {
		maxBytes = 1024 * 1024
	}
	includeAfter := time.Time{}
	if request.Mode != ModeHistoricalBackfill && source.LastSyncedAt != nil {
		includeAfter = *source.LastSyncedAt
	}
	items := []ImportItem{}
	err = filepath.WalkDir(folder, func(path string, entry os.DirEntry, errWalk error) error {
		if errWalk != nil {
			return nil
		}
		if len(items) >= limit {
			return errLocalFolderLimitReached
		}
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 {
			s.audit(source.ID, "source.local_folder_symlink_skipped", fmt.Sprintf("skipped symlink %s", path))
			return nil
		}
		if entry.IsDir() {
			if path != folder && shouldExclude(source.ExcludePatterns, name) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldExclude(source.ExcludePatterns, path) || !isReadableLocalFile(path) {
			return nil
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			return nil
		}
		if !includeAfter.IsZero() && !info.ModTime().After(includeAfter) {
			return nil
		}
		content, errRead := readLocalTextFile(path, maxBytes)
		if errRead != nil || strings.TrimSpace(content) == "" {
			return nil
		}
		rel, _ := filepath.Rel(folder, path)
		items = append(items, ImportItem{
			ExternalID: "file:" + filepath.ToSlash(rel),
			Title:      filepath.ToSlash(rel),
			Content:    content,
			SourceURI:  "file://" + filepath.ToSlash(path),
			ItemType:   localFileContentType(path),
			ProjectKey: request.ProjectKey,
			Metadata:   fmt.Sprintf("size=%d;modified=%s", info.Size(), info.ModTime().UTC().Format(time.RFC3339)),
		})
		return nil
	})
	if err != nil && err != errLocalFolderLimitReached {
		return nil, err
	}
	s.audit(source.ID, "source.local_folder_scanned", fmt.Sprintf("scanned %d files from selected local-folder source", len(items)))
	return items, nil
}

func (s *service) whatsAppExportItems(source *models.ConnectedSource, request ImportRequest) ([]ImportItem, error) {
	projectKey := firstNonEmpty(request.ProjectKey, source.DefaultProjectKey)
	if len(request.Items) > 0 {
		return expandWhatsAppImportItems(request.Items, projectKey, source.Name, firstPositiveInt(request.Limit, envInt("WHATSAPP_EXPORT_CHUNK_MESSAGES", defaultWhatsAppChunkMessages))), nil
	}

	root := firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources")
	folder, err := resolveAllowedFolder(root, firstNonEmpty(request.FolderPath, source.SyncTarget, "."))
	if err != nil {
		return nil, err
	}
	limit := firstPositiveInt(request.Limit, envInt("CONNECTED_SOURCE_FILE_LIMIT", 100))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	maxBytes := firstPositiveInt64(request.MaxBytes, envInt64("CONNECTED_SOURCE_MAX_BYTES", 2*1024*1024))
	if maxBytes <= 0 || maxBytes > 10*1024*1024 {
		maxBytes = 2 * 1024 * 1024
	}

	baseItems := []ImportItem{}
	err = filepath.WalkDir(folder, func(path string, entry os.DirEntry, errWalk error) error {
		if errWalk != nil {
			return nil
		}
		if len(baseItems) >= limit {
			return errLocalFolderLimitReached
		}
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 {
			s.audit(source.ID, "source.whatsapp_symlink_skipped", fmt.Sprintf("skipped symlink %s", path))
			return nil
		}
		if entry.IsDir() {
			if path != folder && shouldExclude(source.ExcludePatterns, name) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldExclude(source.ExcludePatterns, path) || strings.ToLower(filepath.Ext(path)) != ".txt" {
			return nil
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			return nil
		}
		if request.Mode != ModeHistoricalBackfill && source.LastSyncedAt != nil && !info.ModTime().After(*source.LastSyncedAt) {
			return nil
		}
		content, errRead := readLocalTextFile(path, maxBytes)
		if errRead != nil || strings.TrimSpace(content) == "" {
			return nil
		}
		rel, _ := filepath.Rel(folder, path)
		baseItems = append(baseItems, ImportItem{
			ExternalID: "whatsapp-file:" + filepath.ToSlash(rel),
			Title:      strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel)),
			Content:    content,
			SourceURI:  "file://" + filepath.ToSlash(path),
			ItemType:   "whatsapp_export",
			ProjectKey: projectKey,
			Metadata:   fmt.Sprintf("source=whatsapp-export;size=%d;modified=%s", info.Size(), info.ModTime().UTC().Format(time.RFC3339)),
		})
		return nil
	})
	if err != nil && err != errLocalFolderLimitReached {
		return nil, err
	}
	items := expandWhatsAppImportItems(baseItems, projectKey, source.Name, envInt("WHATSAPP_EXPORT_CHUNK_MESSAGES", defaultWhatsAppChunkMessages))
	s.audit(source.ID, "source.whatsapp_export_scanned", fmt.Sprintf("parsed %d source file(s) into %d bounded WhatsApp conversation windows", len(baseItems), len(items)))
	return items, nil
}

type odooHERPApp struct {
	Name      string
	Domain    string
	Role      string
	Signals   []string
	Workflows []string
	Risk      string
	Autonomy  string
}

func odooHERPItems(source *models.ConnectedSource, request ImportRequest) []ImportItem {
	projectKey := firstNonEmpty(request.ProjectKey, source.DefaultProjectKey, "Robert-life-os")
	apps := selectedOdooHERPApps(firstNonEmpty(request.FolderPath, source.SyncTarget))
	items := make([]ImportItem, 0, len(apps))
	for _, app := range apps {
		slug := slugText(app.Name)
		items = append(items, ImportItem{
			ExternalID: "odoo-herp-app:" + slug,
			Title:      "Odoo " + app.Name + " app",
			Content:    renderOdooHERPApp(app),
			SourceURI:  sourceURIForOdooApp(source.SyncTarget, slug),
			ItemType:   "odoo_herp_app",
			ProjectKey: projectKey,
			Metadata: fmt.Sprintf(
				"source=odoo-herp;domain=%s;risk=%s;autonomy=%s;read_only_default=true",
				app.Domain,
				app.Risk,
				app.Autonomy,
			),
		})
	}
	return items
}

func selectedOdooHERPApps(selector string) []odooHERPApp {
	apps := defaultOdooHERPApps()
	raw := strings.TrimSpace(selector)
	if raw == "" {
		return apps
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.RawQuery != "" {
		raw = firstNonEmpty(parsed.Query().Get("apps"), parsed.Query().Get("modules"), parsed.Query().Get("domains"))
	}
	if raw == "" || strings.Contains(strings.ToLower(raw), "odoo.com/odoo") {
		return apps
	}
	requested := map[string]bool{}
	for _, value := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	}) {
		key := slugText(value)
		if key != "" {
			requested[key] = true
		}
	}
	if len(requested) == 0 {
		return apps
	}
	selected := []odooHERPApp{}
	for _, app := range apps {
		if odooAppRequested(app, requested) {
			selected = append(selected, app)
		}
	}
	if len(selected) == 0 {
		return apps
	}
	return selected
}

func odooAppRequested(app odooHERPApp, requested map[string]bool) bool {
	appKey := slugText(app.Name)
	domainKey := slugText(app.Domain)
	for key := range requested {
		if key == appKey || key == domainKey || strings.Contains(appKey, key) || (len(key) >= 6 && strings.Contains(domainKey, key)) {
			return true
		}
	}
	return false
}

func renderOdooHERPApp(app odooHERPApp) string {
	return strings.Join([]string{
		"Odoo app: " + app.Name,
		"HERP domain: " + app.Domain,
		"HAI role: " + app.Role,
		"Source signals: " + strings.Join(app.Signals, "; "),
		"Workflow candidates: " + strings.Join(app.Workflows, "; "),
		"Task: map " + app.Name + " records into source-linked HAI workflows, next-best actions, and review queues.",
		"Follow up: review fresh " + app.Domain + " records during Odoo sync and create follow-up tasks when an owner, deadline, quote, invoice, stock movement, customer reply, or blocked item appears.",
		"Decision: keep Odoo write-back read-only by default; quotes, invoices, payments, public messages, inventory changes, account changes, and external sends require Robert approval.",
		"Risk: " + app.Risk,
		"Autonomy: " + app.Autonomy,
	}, "\n")
}

func sourceURIForOdooApp(syncTarget, slug string) string {
	target := strings.TrimSpace(syncTarget)
	if target == "" || strings.ContainsAny(target, ",;|") {
		return "odoo://app/" + slug
	}
	if strings.Contains(target, "#") {
		return target + "&app=" + slug
	}
	return strings.TrimRight(target, "/") + "#app=" + slug
}

func defaultOdooHERPApps() []odooHERPApp {
	return []odooHERPApp{
		{Name: "CRM", Domain: "relationships", Role: "turn leads, opportunities, promises, and stalled conversations into next actions.", Signals: []string{"lead stage", "expected revenue", "next activity", "lost reason"}, Workflows: []string{"follow up stale opportunities", "draft client replies", "surface high-value leads"}, Risk: "medium", Autonomy: "draft and schedule low-risk reminders"},
		{Name: "Sales", Domain: "commercial operations", Role: "track quotes, orders, customer commitments, and deal blockers.", Signals: []string{"quotation status", "expiration date", "customer acceptance", "delivery promise"}, Workflows: []string{"draft quotation follow-ups", "flag expiring offers", "prepare handoff checklists"}, Risk: "medium", Autonomy: "draft only for commercial commitments"},
		{Name: "Invoicing and Accounting", Domain: "finance", Role: "connect invoices, bills, overdue amounts, payments, and financial obligations to safe review queues.", Signals: []string{"invoice due date", "payment status", "amount", "vendor or customer"}, Workflows: []string{"detect overdue invoices", "prepare payment review", "summarize cash obligations"}, Risk: "high", Autonomy: "approval required for financial action"},
		{Name: "Website and eCommerce", Domain: "public presence", Role: "track orders, forms, public pages, and publishable content without making unsupported public changes.", Signals: []string{"order status", "form submission", "page change", "abandoned cart"}, Workflows: []string{"triage customer submissions", "draft content updates", "flag public publishing approvals"}, Risk: "high", Autonomy: "draft only for public publishing"},
		{Name: "Inventory", Domain: "stock and assets", Role: "monitor stock levels, reservations, receiving, delivery readiness, and missing materials.", Signals: []string{"on-hand quantity", "reserved quantity", "reorder rule", "picking status"}, Workflows: []string{"detect low stock", "prepare procurement tasks", "flag delivery blockers"}, Risk: "medium", Autonomy: "suggest and checklist, approval for stock moves"},
		{Name: "Purchase", Domain: "procurement", Role: "track supplier requests, purchase orders, approvals, expected receipts, and price changes.", Signals: []string{"RFQ status", "vendor", "expected arrival", "purchase amount"}, Workflows: []string{"chase supplier replies", "flag late receipts", "prepare approval packages"}, Risk: "high", Autonomy: "approval required for spend"},
		{Name: "Manufacturing", Domain: "production", Role: "map bills of materials, work orders, shortages, and completion blockers into operational steps.", Signals: []string{"work order status", "component shortage", "planned date", "quality check"}, Workflows: []string{"detect blocked production", "prepare material checklist", "flag overdue work orders"}, Risk: "medium", Autonomy: "execute only low-risk status checklists"},
		{Name: "Project", Domain: "project delivery", Role: "turn project tasks, blockers, assignees, milestones, and due dates into HAI work queues.", Signals: []string{"task stage", "assignee", "deadline", "blocked reason"}, Workflows: []string{"clear blockers", "generate next actions", "summarize project health"}, Risk: "medium", Autonomy: "safe admin updates only"},
		{Name: "Timesheets", Domain: "capacity and billing", Role: "connect time entries, missing logs, billability, and workload imbalance to review.", Signals: []string{"timesheet entry", "billable flag", "employee", "project"}, Workflows: []string{"detect missing time", "prepare billing summary", "flag overloaded weeks"}, Risk: "medium", Autonomy: "suggest corrections, approval for billed changes"},
		{Name: "Helpdesk", Domain: "support", Role: "route tickets, customer pain, SLA risk, and repeated issues into workflows and knowledge.", Signals: []string{"ticket priority", "SLA deadline", "customer", "status"}, Workflows: []string{"prioritize urgent tickets", "draft customer updates", "detect repeat incidents"}, Risk: "medium", Autonomy: "draft customer replies, send only with approval when sensitive"},
		{Name: "Field Service", Domain: "field operations", Role: "coordinate appointments, locations, materials, travel dependencies, and completion evidence.", Signals: []string{"appointment time", "address", "task status", "materials needed"}, Workflows: []string{"prepare visit checklist", "detect impossible schedules", "draft after-job summary"}, Risk: "medium", Autonomy: "admin reminders and checklists"},
		{Name: "Planning", Domain: "resource planning", Role: "monitor shifts, workload, availability, and conflicts before they become missed commitments.", Signals: []string{"resource allocation", "shift", "conflict", "capacity"}, Workflows: []string{"flag schedule conflicts", "suggest workload moves", "prepare staffing reminders"}, Risk: "medium", Autonomy: "suggest only for people assignments"},
		{Name: "Employees and HR", Domain: "people operations", Role: "surface HR tasks, documents, onboarding, absence, and sensitive people records with strict privacy.", Signals: []string{"employee record", "contract date", "leave request", "document"}, Workflows: []string{"prepare onboarding checklist", "flag expiring documents", "route HR approvals"}, Risk: "high", Autonomy: "review-gated only"},
		{Name: "Expenses", Domain: "finance", Role: "track receipts, reimbursements, policy gaps, and missing evidence.", Signals: []string{"receipt", "amount", "approval status", "employee"}, Workflows: []string{"collect missing receipts", "prepare reimbursement review", "flag policy exceptions"}, Risk: "high", Autonomy: "approval required for financial action"},
		{Name: "Documents and Sign", Domain: "documents", Role: "connect contracts, signed files, evidence, folders, and document requests to provenance-backed workflows.", Signals: []string{"document owner", "signature status", "folder", "version"}, Workflows: []string{"detect unsigned documents", "prepare evidence bundles", "link source files to cases"}, Risk: "high", Autonomy: "never delete or sign automatically"},
		{Name: "Calendar and Appointments", Domain: "time", Role: "turn appointments, meetings, deadlines, and scheduling conflicts into preparation and follow-up work.", Signals: []string{"event date", "attendee", "location", "booking status"}, Workflows: []string{"create preparation tasks", "flag conflicts", "draft reschedule options"}, Risk: "medium", Autonomy: "low-risk reminders only"},
		{Name: "Discuss and Contacts", Domain: "communication", Role: "link people, threads, commitments, and unanswered messages to projects and follow-ups.", Signals: []string{"message thread", "contact role", "company", "last reply"}, Workflows: []string{"detect unanswered threads", "summarize commitments", "draft next reply"}, Risk: "high", Autonomy: "draft only for external communication"},
		{Name: "Marketing and Social", Domain: "public communication", Role: "plan campaigns, audience messages, and publishing work with source-grounded approval controls.", Signals: []string{"campaign status", "mailing result", "social post", "audience segment"}, Workflows: []string{"draft campaign updates", "flag public posting approvals", "review unsupported claims"}, Risk: "high", Autonomy: "draft only for public content"},
		{Name: "Knowledge", Domain: "organizational memory", Role: "turn internal notes, procedures, and repeated answers into verified HAI memory and source-grounded responses.", Signals: []string{"article update", "procedure", "owner", "tag"}, Workflows: []string{"refresh procedural memory", "detect stale guidance", "propose knowledge updates"}, Risk: "medium", Autonomy: "propose memory updates with source links"},
		{Name: "Point of Sale", Domain: "front-office sales", Role: "connect POS orders, sessions, cash movements, returns, and stock effects to reviewable operations.", Signals: []string{"session status", "order", "refund", "cash difference"}, Workflows: []string{"flag cash differences", "summarize daily sales", "detect refund review needs"}, Risk: "high", Autonomy: "approval required for cash or refund action"},
		{Name: "Subscriptions", Domain: "recurring revenue", Role: "track renewal dates, failed payments, churn risk, and customer commitments.", Signals: []string{"renewal date", "subscription status", "MRR", "payment failure"}, Workflows: []string{"draft renewal follow-ups", "flag failed payments", "surface churn risk"}, Risk: "medium", Autonomy: "draft only for customer commitments"},
	}
}

func (s *service) ensureConnectors() error {
	for _, connector := range defaultConnectors() {
		c := connector
		if _, err := s.repo.SaveConnector(&c); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) connectorByKey(connectorKey string) (models.SourceConnector, error) {
	connectors, err := s.repo.FindConnectors()
	if err != nil {
		return models.SourceConnector{}, err
	}
	for _, connector := range connectors {
		if connector.ConnectorKey == connectorKey {
			return connector, nil
		}
	}
	return models.SourceConnector{}, fmt.Errorf("connector %s is not registered", connectorKey)
}

func (s *service) upsertRawItem(source *models.ConnectedSource, item ImportItem, index int) (*models.SourceRawItem, bool, error) {
	externalID := firstNonEmpty(item.ExternalID, fmt.Sprintf("manual-%d-%s", index, hashText(item.Title+item.Content)))
	existing, err := s.repo.FindRawItem(source.ID, externalID)
	added := false
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, false, err
	}
	if err == gorm.ErrRecordNotFound {
		existing = &models.SourceRawItem{
			SourceID:   source.ID,
			ExternalID: externalID,
			FetchedAt:  time.Now().UTC(),
		}
		added = true
	}
	existing.ProjectKey = item.ProjectKey
	existing.ItemType = firstNonEmpty(item.ItemType, source.Category)
	existing.Title = item.Title
	existing.SourceURI = item.SourceURI
	existing.Content = item.Content
	existing.Metadata = item.Metadata
	existing.ContentHash = hashText(item.Title + "|" + item.Content)
	raw, err := s.repo.SaveRawItem(existing)
	return raw, added, err
}

func (s *service) extractAndStore(source *models.ConnectedSource, raw *models.SourceRawItem, text string) (*models.SourceExtraction, error) {
	existing, err := s.repo.FindExtractionByRawItem(raw.ID)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if err == gorm.ErrRecordNotFound {
		existing = &models.SourceExtraction{SourceID: source.ID, RawItemID: raw.ID}
	}
	now := time.Now().UTC()
	clean := normalizeSpaces(text)
	existing.ProjectKey = raw.ProjectKey
	existing.ContentType = raw.ItemType
	existing.Text = clean
	existing.Summary = compact(clean, 420)
	existing.Entities = joinValues(extractEntities(clean))
	existing.Dates = joinValues(extractDates(clean))
	existing.Tasks = joinValues(extractTasks(clean))
	existing.Decisions = joinValues(extractDecisions(clean))
	existing.FollowUps = joinValues(extractFollowUps(clean))
	existing.SourceURI = raw.SourceURI
	existing.SourceLabel = raw.Title
	existing.ContentHash = raw.ContentHash
	existing.Sensitive = source.ConnectorKey == "whatsapp-export" || source.ConnectorKey == laroConnectorKey || containsAny(strings.ToLower(clean), "password", "secret", "token", "bank", "invoice", "contract", "legal", "medical", "juridisch", "medisch", "rekening", "factuur")
	existing.Uncertain = isManualPlanningContextOnlyConnector(source.ConnectorKey) || sourceRawItemRequiresReview(raw) || len(clean) < 40 || containsAny(strings.ToLower(clean), "maybe", "unclear", "unknown")
	existing.LastIndexedAt = &now
	return s.repo.SaveExtraction(existing)
}

func (s *service) storeUsefulMemory(source *models.ConnectedSource, extraction *models.SourceExtraction) {
	if s.memoryService == nil || source == nil || extraction == nil {
		return
	}
	if isManualPlanningContextOnlyConnector(source.ConnectorKey) {
		return
	}
	if extraction.Sensitive || extraction.Uncertain || extraction.Summary == "" {
		return
	}
	created, err := memory.CreateForOwner(s.memoryService, source.OwnerIdentity, memory.CreateRequest{
		ProjectKey:  extraction.ProjectKey,
		Kind:        "source",
		Content:     extraction.Summary,
		Summary:     extraction.Summary,
		Tags:        []string{"connected-source", source.Category, source.ConnectorKey},
		Confidence:  0.68,
		SourceURI:   extraction.SourceURI,
		SourceLabel: extraction.SourceLabel,
	})
	if err != nil {
		s.audit(source.ID, "memory.source_create_failed", compact(safety.RedactSecrets(err.Error()), 240))
		return
	}
	if created != nil {
		s.autoLinkPursuitMemory(source, extraction, created)
	}
}

func (s *service) rememberExtractionCorrection(before, after *models.SourceExtraction) {
	if s.memoryService == nil || before == nil || after == nil || !extractionCorrectionUseful(before, after) {
		return
	}
	source, _ := s.repo.FindSource(after.SourceID)
	if source != nil && isManualPlanningContextOnlyConnector(source.ConnectorKey) {
		return
	}
	request := extractionCorrectionMemoryRequest(source, before, after)
	ownerIdentity := ""
	if source != nil {
		ownerIdentity = source.OwnerIdentity
	}
	created, err := memory.CreateForOwner(s.memoryService, ownerIdentity, request)
	if err != nil {
		s.audit(after.SourceID, "extraction.correction_memory_failed", compact(safety.RedactSecrets(err.Error()), 240))
		return
	}
	if created != nil {
		s.autoLinkPursuitMemory(source, after, created)
	}
	s.audit(after.SourceID, "extraction.correction_memory_created", "stored reviewable source correction lesson")
}

func extractionCorrectionUseful(before, after *models.SourceExtraction) bool {
	if before.ID == uuid.Nil || after.ID == uuid.Nil || before.ID != after.ID {
		return false
	}
	fields := []struct {
		left  string
		right string
	}{
		{before.ProjectKey, after.ProjectKey},
		{before.Summary, after.Summary},
		{before.Entities, after.Entities},
		{before.Dates, after.Dates},
		{before.Tasks, after.Tasks},
		{before.Decisions, after.Decisions},
		{before.FollowUps, after.FollowUps},
	}
	for _, field := range fields {
		if normalizeSpaces(field.left) != normalizeSpaces(field.right) {
			return true
		}
	}
	return before.Sensitive != after.Sensitive || before.Uncertain != after.Uncertain
}

func extractionCorrectionMemoryRequest(source *models.ConnectedSource, before, after *models.SourceExtraction) memory.CreateRequest {
	sourceLabel := firstNonEmpty(after.SourceLabel, "Corrected connected-source extraction")
	sourceURI := firstNonEmpty(after.SourceURI, "source-extraction://"+after.ID.String())
	tags := []string{"connected-source", "source-correction", "correction"}
	if source != nil {
		tags = append(tags, source.Category, source.ConnectorKey)
	}
	tags = append(tags, after.ContentType, after.ProjectKey)

	if after.Sensitive {
		content := strings.Join([]string{
			"Robert corrected a sensitive connected-source extraction.",
			"Future behavior: keep similar records review-gated, avoid storing raw sensitive content as memory, and ask for confirmation before workflow, task, or memory use.",
		}, " ")
		return memory.CreateRequest{
			ProjectKey:  after.ProjectKey,
			Kind:        "lesson",
			Content:     content,
			Summary:     "Sensitive source correction requires review before future use.",
			Tags:        append(tags, "sensitive", "review-required"),
			Confidence:  0.62,
			SourceURI:   "source-extraction://" + after.ID.String(),
			SourceLabel: "Sensitive connected-source correction",
		}
	}

	changedFields := extractionCorrectionChangedFields(before, after)
	content := strings.Join([]string{
		"Robert corrected connected-source extraction behavior.",
		"Changed fields: " + strings.Join(changedFields, ", ") + ".",
		extractionCorrectionValue("Previous summary", before.Summary),
		extractionCorrectionValue("Revised summary", after.Summary),
		extractionCorrectionValue("Previous tasks", before.Tasks),
		extractionCorrectionValue("Revised tasks", after.Tasks),
		extractionCorrectionValue("Previous follow-ups", before.FollowUps),
		extractionCorrectionValue("Revised follow-ups", after.FollowUps),
		"Future behavior: prefer Robert-corrected project matching, task extraction, follow-up detection, and review gating for similar connected-source records; if source evidence conflicts, mark needs_review.",
	}, " ")

	confidence := 0.78
	if after.Uncertain {
		confidence = 0.66
		tags = append(tags, "uncertain", "review-required")
	}
	return memory.CreateRequest{
		ProjectKey:  after.ProjectKey,
		Kind:        "lesson",
		Content:     compact(content, 1300),
		Summary:     compact("Learn from source correction for "+sourceLabel+": "+strings.Join(changedFields, ", "), 240),
		Tags:        tags,
		Confidence:  confidence,
		SourceURI:   safety.RedactURL(sourceURI),
		SourceLabel: sourceLabel,
	}
}

func extractionCorrectionChangedFields(before, after *models.SourceExtraction) []string {
	fields := []struct {
		name  string
		left  string
		right string
	}{
		{"project", before.ProjectKey, after.ProjectKey},
		{"summary", before.Summary, after.Summary},
		{"entities", before.Entities, after.Entities},
		{"dates", before.Dates, after.Dates},
		{"tasks", before.Tasks, after.Tasks},
		{"decisions", before.Decisions, after.Decisions},
		{"follow-ups", before.FollowUps, after.FollowUps},
	}
	changed := []string{}
	for _, field := range fields {
		if normalizeSpaces(field.left) != normalizeSpaces(field.right) {
			changed = append(changed, field.name)
		}
	}
	if before.Sensitive != after.Sensitive {
		changed = append(changed, "sensitivity")
	}
	if before.Uncertain != after.Uncertain {
		changed = append(changed, "uncertainty")
	}
	if len(changed) == 0 {
		return []string{"operator correction"}
	}
	return changed
}

func extractionCorrectionValue(label, value string) string {
	value = strings.TrimSpace(safety.RedactSecrets(value))
	if value == "" {
		return ""
	}
	return fmt.Sprintf("%s: %s.", label, compact(value, 260))
}

func (s *service) createWorkflowFromExtraction(source *models.ConnectedSource, extraction *models.SourceExtraction) (*PursuitRoutingOutcome, error) {
	return s.createWorkflowFromExtractionWithSignal(source, extraction, "")
}

func (s *service) createWorkflowFromExtractionWithSignal(source *models.ConnectedSource, extraction *models.SourceExtraction, supplementalSignal string) (*PursuitRoutingOutcome, error) {
	if s.workflowService == nil {
		return nil, nil
	}
	if source == nil || isManualPlanningContextOnlyConnector(source.ConnectorKey) {
		return nil, nil
	}
	taskSignal := firstNonEmpty(extraction.Tasks, extraction.FollowUps, supplementalSignal)
	if taskSignal == "" {
		return nil, nil
	}
	input := strings.Join([]string{
		firstNonEmpty(extraction.Summary, extraction.SourceLabel),
		"Tasks: " + extraction.Tasks,
		"Follow-ups: " + extraction.FollowUps,
		strings.TrimSpace(supplementalSignal),
		"Dates: " + extraction.Dates,
	}, "\n")
	requiresReview := extraction.Uncertain || extraction.Sensitive
	reviewReason := extractionReviewReason(extraction)
	record, err := s.intakeWorkflow(workflow.IntakeRequest{
		OwnerIdentity:  source.OwnerIdentity,
		Input:          input,
		ProjectKey:     extraction.ProjectKey,
		SourceType:     source.Category,
		SourceID:       extraction.ID.String(),
		RawItemID:      sourceRawItemID(extraction),
		ExtractionID:   extraction.ID.String(),
		SourceURI:      firstNonEmpty(extraction.SourceURI, "source-extraction://"+extraction.ID.String()),
		SourceLabel:    extraction.SourceLabel,
		Trigger:        "source.extraction",
		Actor:          "source-worker",
		RequiresReview: requiresReview,
		ReviewReason:   reviewReason,
	})
	if err != nil {
		if routed, pending := pursuit.IsCandidatePending(err); pending {
			message := "actionable extraction created or matched a pursuit candidate; workflow creation awaits explicit acceptance"
			outcome := &PursuitRoutingOutcome{ExtractionID: extraction.ID.String(), Status: "candidate_pending", Message: message}
			if routed != nil && strings.TrimSpace(routed.Message) != "" {
				message = routed.Message
				outcome.Message = message
			}
			if routed != nil && routed.PursuitID != uuid.Nil {
				outcome.PursuitID = routed.PursuitID.String()
			}
			s.audit(source.ID, "pursuit.intake_deferred", compact(message, 260))
			return outcome, nil
		}
		if errors.Is(err, pursuit.ErrLifecycleRouterRequired) {
			message := "actionable extraction retained; configured pursuit linker is missing the lifecycle router"
			s.audit(source.ID, "pursuit.intake_deferred", message)
			return &PursuitRoutingOutcome{ExtractionID: extraction.ID.String(), Status: "routing_deferred", Message: message}, nil
		}
		s.audit(source.ID, "workflow.intake_failed", err.Error())
		return nil, err
	}
	outcome := &PursuitRoutingOutcome{ExtractionID: extraction.ID.String(), Status: "workflow_created", Message: "actionable extraction created a governed workflow"}
	if record != nil && record.Item.ID != uuid.Nil {
		outcome.WorkflowID = record.Item.ID.String()
	}
	if record != nil && !s.routesWorkflowThroughPursuits() {
		if linked := s.autoLinkPursuitWorkflow(source, extraction, record, input); linked != nil && linked.PursuitID != uuid.Nil {
			outcome.PursuitID = linked.PursuitID.String()
			if linked.Linked {
				outcome.Status = "pursuit_linked"
				outcome.Message = "actionable extraction linked to a matching pursuit"
			}
		}
	} else if record != nil && s.routesWorkflowThroughPursuits() {
		outcome.Status = "pursuit_routed"
		outcome.Message = "actionable extraction routed through the pursuit lifecycle"
		if len(record.Pursuits) > 0 && record.Pursuits[0].ID != uuid.Nil {
			outcome.PursuitID = record.Pursuits[0].ID.String()
		}
	}
	s.audit(source.ID, "workflow.intake_created", "actionable extraction sent to workflow engine")
	return outcome, nil
}

func sourceRawItemID(extraction *models.SourceExtraction) string {
	if extraction == nil || extraction.RawItemID == uuid.Nil {
		return ""
	}
	return extraction.RawItemID.String()
}

func (s *service) autoLinkPursuitWorkflow(source *models.ConnectedSource, extraction *models.SourceExtraction, record *workflow.WorkflowRecord, input string) *pursuit.AutoLinkResult {
	if s.pursuitLinker == nil || source == nil || record == nil || record.Item.ID == uuid.Nil {
		return nil
	}
	request := pursuit.AutoLinkWorkflowRequest{
		OwnerIdentity:        source.OwnerIdentity,
		WorkflowID:           record.Item.ID,
		Input:                input,
		ProjectKey:           source.DefaultProjectKey,
		SourceType:           source.Category,
		SourceID:             source.ID.String(),
		SourceURI:            safety.RedactURL(source.SyncTarget),
		SourceLabel:          source.Name,
		Actor:                "source-worker",
		AllowCreateCandidate: true,
	}
	if extraction != nil {
		request.ProjectKey = firstNonEmpty(extraction.ProjectKey, source.DefaultProjectKey)
		request.SourceType = source.Category
		request.SourceID = extraction.ID.String()
		request.SourceURI = firstNonEmpty(extraction.SourceURI, "source-extraction://"+extraction.ID.String())
		request.SourceLabel = firstNonEmpty(extraction.SourceLabel, source.Name)
		request.ExtractionID = extraction.ID.String()
		if extraction.RawItemID != uuid.Nil {
			request.RawItemID = extraction.RawItemID.String()
		}
	}
	result, err := s.pursuitLinker.AutoLinkWorkflow(request)
	if err != nil {
		s.audit(source.ID, "pursuit.auto_link_failed", compact(err.Error(), 260))
		return nil
	}
	if result != nil && result.Linked {
		s.audit(source.ID, "pursuit.auto_linked", fmt.Sprintf("workflow %s linked to pursuit %s with %.2f confidence", record.Item.ID, result.PursuitID, result.Score))
		return result
	}
	if result != nil && result.Message != "" {
		s.audit(source.ID, "pursuit.auto_link_skipped", result.Message)
	}
	return result
}

func (s *service) autoLinkPursuitMemory(source *models.ConnectedSource, extraction *models.SourceExtraction, memoryRecord *models.ContextMemory) {
	if s.pursuitLinker == nil || source == nil || extraction == nil || memoryRecord == nil || memoryRecord.ID == uuid.Nil {
		return
	}
	request := pursuit.AutoLinkMemoryRequest{
		OwnerIdentity:        source.OwnerIdentity,
		MemoryID:             memoryRecord.ID,
		Input:                firstNonEmpty(extraction.Summary, memoryRecord.Summary, memoryRecord.Content),
		ProjectKey:           firstNonEmpty(extraction.ProjectKey, source.DefaultProjectKey, memoryRecord.ProjectKey),
		SourceURI:            firstNonEmpty(memoryRecord.SourceURI, extraction.SourceURI, "source-extraction://"+extraction.ID.String()),
		SourceLabel:          firstNonEmpty(memoryRecord.SourceLabel, extraction.SourceLabel, source.Name),
		Actor:                "source-worker",
		AllowCreateCandidate: false,
	}
	result, err := s.pursuitLinker.AutoLinkMemory(request)
	if err != nil {
		s.audit(source.ID, "pursuit.memory_auto_link_failed", compact(err.Error(), 260))
		return
	}
	if result != nil && result.Linked {
		s.audit(source.ID, "pursuit.memory_auto_linked", fmt.Sprintf("memory %s linked to pursuit %s with %.2f confidence", memoryRecord.ID, result.PursuitID, result.Score))
		return
	}
	if result != nil && result.Message != "" {
		s.audit(source.ID, "pursuit.memory_auto_link_skipped", result.Message)
	}
}

func extractionReviewReason(extraction *models.SourceExtraction) string {
	reasons := []string{}
	if extraction.ContentType == "google_calendar_event_cancelled" {
		reasons = append(reasons, "calendar cancellation requires owner review before prior obligations change")
	}
	if extraction.ContentType == "google_calendar_conflict" {
		reasons = append(reasons, "calendar conflict requires owner prioritization before any schedule change")
	}
	if extraction.Uncertain {
		reasons = append(reasons, "extraction is uncertain")
	}
	if extraction.Sensitive {
		reasons = append(reasons, "extraction contains sensitive content")
	}
	return strings.Join(reasons, "; ")
}

func sourceRawItemRequiresReview(raw *models.SourceRawItem) bool {
	if raw == nil || strings.TrimSpace(raw.Metadata) == "" {
		return false
	}
	var metadata struct {
		ReviewRequired bool `json:"reviewRequired"`
	}
	return json.Unmarshal([]byte(raw.Metadata), &metadata) == nil && metadata.ReviewRequired
}

func (s *service) reconcileWorkflowFromExtraction(extraction *models.SourceExtraction) error {
	if firstNonEmpty(extraction.Tasks, extraction.FollowUps) == "" {
		return nil
	}
	source, err := s.repo.FindSource(extraction.SourceID)
	if err != nil {
		s.audit(extraction.SourceID, "workflow.reconcile_failed", err.Error())
		return err
	}
	if _, err := s.createWorkflowFromExtraction(source, extraction); err != nil {
		s.audit(extraction.SourceID, "workflow.reconcile_failed", err.Error())
		return err
	}
	return nil
}

func (s *service) retractWorkflowForExtraction(extraction *models.SourceExtraction, reason string) error {
	if s.workflowService == nil {
		return nil
	}
	source, err := s.repo.FindSource(extraction.SourceID)
	if err != nil {
		return err
	}
	return s.workflowService.RetractSource(source.Category, extraction.ID.String(), reason)
}

func (s *service) indexExtraction(ctx context.Context, extraction *models.SourceExtraction) error {
	keywords := strings.Join(mapKeys(tokenSet(extraction.Text+" "+extraction.Summary+" "+extraction.Entities+" "+extraction.Tasks)), ",")
	if _, err := s.repo.SaveIndexEntry(&models.SourceIndexEntry{
		SourceID:     extraction.SourceID,
		ExtractionID: extraction.ID,
		ProjectKey:   extraction.ProjectKey,
		IndexType:    "keyword",
		Keywords:     keywords,
	}); err != nil {
		return err
	}
	if err := s.repo.DeletePendingVectorIndex(extraction.ID); err != nil {
		return err
	}
	if s.semanticService == nil || !s.semanticService.Enabled() {
		return nil
	}
	if err := s.semanticService.Index(ctx, extraction); err != nil {
		// Semantic indexing is optional enrichment. Preserve the extracted record
		// and keyword index, then expose the degraded state in the source audit.
		s.audit(extraction.SourceID, "semantic.index_failed", "local semantic index was not updated: "+compact(err.Error(), 240))
		return nil
	}
	_, err := s.repo.SaveIndexEntry(&models.SourceIndexEntry{
		SourceID: extraction.SourceID, ExtractionID: extraction.ID, ProjectKey: extraction.ProjectKey,
		IndexType: "pgvector", VectorRef: "pgvector:local-embedding",
	})
	return err
}

func (s *service) beginSync(sourceID uuid.UUID) bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.activeSyncs == nil {
		s.activeSyncs = map[uuid.UUID]bool{}
	}
	if s.activeSyncs[sourceID] {
		return false
	}
	s.activeSyncs[sourceID] = true
	return true
}

func (s *service) endSync(sourceID uuid.UUID) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	delete(s.activeSyncs, sourceID)
}

func itemFailure(item ImportItem, stage string, err error) string {
	label := firstNonEmpty(item.ExternalID, item.Title, "unknown item")
	return compact(fmt.Sprintf("%s: %s: %v", label, stage, err), 320)
}

func (s *service) audit(sourceID uuid.UUID, action, message string) {
	_, _ = s.repo.SaveAuditLog(&models.SourceAuditLog{
		SourceID: sourceID,
		Action:   action,
		Message:  message,
	})
}

// Adapter status values. These describe honestly what a connector actually does,
// which is not the same question as whether it can be used:
//
//	AdapterOperational — connects to and fetches from the live external service.
//	AdapterLocalOnly   — functional, but ingests from local files/folders/exports
//	                     rather than the live cloud service its name suggests.
//	AdapterModeled     — functional, but produces built-in domain models rather
//	                     than reading a real source.
//	AdapterNotImplemented — registered as a contract only; no working adapter.
//
// Only operational, local-only, and modeled adapters are usable (see
// adapterIsUsable). The distinction lets the UI avoid reporting a local-folder
// reader as a live Gmail/Trello/Drive connector or a disabled remote adapter as
// ready to connect.
const (
	AdapterOperational           = "operational"
	AdapterLocalOnly             = "local_only"
	AdapterModeled               = "modeled"
	AdapterConfigurationRequired = "configuration_required"
	AdapterNotImplemented        = "not_implemented"
)

// adapterIsUsable reports whether a connector with the given status can back a
// created source. Everything except an unimplemented (or unset) adapter can.
func adapterIsUsable(status string) bool {
	switch strings.TrimSpace(status) {
	case AdapterOperational, AdapterLocalOnly, AdapterModeled:
		return true
	default:
		return false
	}
}

func defaultConnectors() []models.SourceConnector {
	modes := joinValues([]string{ModeManualImport, ModeScheduledSync, ModeWebhookSync, ModeHistoricalBackfill, ModeIncrementalSync})
	return []models.SourceConnector{
		{ConnectorKey: "email", Name: "Email exports (MBOX/EML)", Category: "email", SupportedModes: modes, RequiredScopes: "metadata,read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads MBOX/EML export files from an allowlisted local folder; does not connect to Gmail/IMAP"},
		{ConnectorKey: "calendar", Name: "Calendar exports (ICS)", Category: "calendar", SupportedModes: modes, RequiredScopes: "metadata,read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads ICS export files from an allowlisted local folder; does not connect to Google/Outlook Calendar"},
		{ConnectorKey: "cloud-documents", Name: "Synced cloud document folders", Category: "cloud_document", SupportedModes: modes, RequiredScopes: "metadata,read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads a locally-synced folder (bounded by the folder allowlist); does not connect to a Drive/Dropbox API"},
		{ConnectorKey: "project-board", Name: "Trello project-board exports", Category: "project_board", SupportedModes: modes, RequiredScopes: "metadata,read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads Trello JSON export files from an allowlisted local folder; does not connect to the Trello API"},
		{ConnectorKey: trelloConnectorKey, Name: "Trello board (read-only API)", Category: "project_board", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Trello REST sync of card descriptions, comments, checklists, attachment metadata, lists, due dates, and labels; set a board id in syncTarget and least-privilege TRELLO_API_KEY/TRELLO_READ_TOKEN. Attachment bodies are not downloaded and HAI never writes to Trello."},
		{ConnectorKey: "github", Name: "GitHub repositories and work", Category: "github", SupportedModes: modes, RequiredScopes: "metadata,read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only GitHub REST sync of repositories, issues, pull requests, commits, and workflow runs; optional token in GITHUB_SOURCE_TOKEN"},
		{ConnectorKey: gmailConnectorKey, Name: "Gmail (Google OAuth)", Category: "email", SupportedModes: joinValues([]string{ModeHistoricalBackfill, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "gmail.readonly", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Gmail sync over Google OAuth with provider-native history cursors, bounded message text, attachment metadata, and source provenance"},
		{ConnectorKey: driveConnectorKey, Name: "Google Drive (Google OAuth)", Category: "cloud_document", SupportedModes: joinValues([]string{ModeHistoricalBackfill, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "drive.readonly", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Drive inventory and changes sync over Google OAuth; text-native files are extracted within limits and binary documents remain provenance-linked for governed extraction"},
		{ConnectorKey: contactsConnectorKey, Name: "Google Contacts (Google OAuth)", Category: "contact", SupportedModes: joinValues([]string{ModeHistoricalBackfill, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "contacts.readonly", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Google Contacts sync with provider-native sync tokens; imported people remain review candidates and HAI never writes back to the address book"},
		{ConnectorKey: calendarConnectorKey, Name: "Google Calendar (Google OAuth)", Category: "calendar", SupportedModes: joinValues([]string{ModeHistoricalBackfill, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "calendar.readonly", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only primary Google Calendar sync with a bounded initial backfill and provider-native sync tokens; cancellations remain reviewable tombstones and HAI never writes back"},
		{ConnectorKey: "local-folder", Name: "Selected local folders", Category: "local_folder", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeFolderWatcher, ModeIncrementalSync}), RequiredScopes: "selected-folder-read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "manual and scheduled ingestion of an allowlisted local folder"},
		{ConnectorKey: "json-feed", Name: "Allowlisted JSON feed", Category: "generic_feed", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "metadata,read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live scheduled and incremental fetch of a normalized JSON feed over HTTP, with host allowlisting and bounded responses"},
		{ConnectorKey: "whatsapp-export", Name: "WhatsApp exported chats", Category: "chat", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "selected-chat-export-read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "parses local WhatsApp .txt export files into bounded, sensitive, review-gated records; does not connect to WhatsApp"},
		{ConnectorKey: "whisper-audio", Name: "Selected audio folders (whisper.cpp)", Category: "audio", SupportedModes: joinValues([]string{ModeManualImport}), RequiredScopes: "selected-audio-folder-read,explicit-consent", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "operator-triggered local transcription from an explicit selected folder through whisper.cpp; no microphone capture, cloud upload, scheduled scan, or raw-audio retention"},
		{ConnectorKey: "odoo-herp", Name: "Odoo / HERP operations", Category: "herp", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "metadata,read,herp:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterModeled, StatusReason: "generates built-in Odoo app-domain models from manual selection; no live Odoo connection; write-back disabled by default"},
		{ConnectorKey: odooJSON2ConnectorKey, Name: "Odoo JSON-2 (read only)", Category: "herp", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "odoo-api-key,read-only-model-access", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Odoo JSON-2 sync for a fixed model and field allowlist; requires HAI_ODOO_* configuration and never writes back"},
		{ConnectorKey: shareTConnectorKey, Name: "ShareT links (read only)", Category: "project_board", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "connector:read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only ShareT link inventory through a scoped connector token; fetches every page within an explicit completeness limit, excludes participant email addresses, and never creates, changes, comments on, or revokes links"},
		{ConnectorKey: cloudQuerySummaryConnectorKey, Name: "CloudQuery sync summaries (local read only)", Category: "cloud_inventory", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "selected-folder-read,cloud_inventory:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads only a fixed, operator-produced local CloudQuery JSONL sync summary; never starts CloudQuery, reads its configuration or credentials, or accesses source/destination data"},
		{ConnectorKey: airbyteInventoryConnectorKey, Name: "Airbyte source and connection inventory (local read only)", Category: "connector_inventory", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "airbyte-api-key,approved-workspace:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads only bounded source and connection metadata from a configured local Airbyte API and fixed workspace allowlist; never reads credentials/configuration/records or creates, changes, starts, stops, or deletes a sync"},
		{ConnectorKey: laroConnectorKey, Name: "LARO legal case intelligence (read only)", Category: "legal_case", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "laro:hai:read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "reads bounded, owner-scoped case summaries and source-linked legal analyses from LARO with a revocable connector credential; source bytes and client contact details are excluded and HAI never writes back"},
		{ConnectorKey: openSpecArtifactConnectorKey, Name: "OpenSpec change artifacts (local read only)", Category: "code_spec", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "selected-folder-read,code_spec:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads only proposal.md, design.md, tasks.md, and specs Markdown below a selected local openspec/changes folder; never installs or runs OpenSpec, edits a repository, or authorizes code changes"},
		{ConnectorKey: projectInstructionsConnectorKey, Name: "Project instructions (manual context only)", Category: "code_spec", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "selected-folder-read,code_spec:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads only root AGENTS.md and CLAUDE.md from a selected local project; stores untrusted review context and never authorizes execution"},
		{ConnectorKey: fabricPatternsConnectorKey, Name: "Fabric patterns (manual context only)", Category: "code_spec", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "selected-folder-read,code_spec:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads only bounded immediate-child system.md pattern files from a selected local folder; never runs or auto-attaches patterns"},
	}
}

func categoryForConnector(connectorKey string) string {
	for _, connector := range defaultConnectors() {
		if connector.ConnectorKey == connectorKey {
			return connector.Category
		}
	}
	return ""
}

func defaultModes(values []string) []string {
	if len(values) > 0 {
		return values
	}
	return []string{ModeManualImport, ModeIncrementalSync}
}

func sourceUsesLocalFolder(connectorKey string) bool {
	switch strings.TrimSpace(connectorKey) {
	case "local-folder", "email", "calendar", "cloud-documents", "project-board":
		return true
	default:
		return false
	}
}

func sourceHasNativeAdapter(connectorKey string) bool {
	return sourceUsesLocalFolder(connectorKey) || connectorKey == "json-feed" || connectorKey == "github" || isGoogleOAuthConnector(connectorKey) || connectorKey == trelloConnectorKey || connectorKey == "whatsapp-export" || connectorKey == "whisper-audio" || connectorKey == "odoo-herp" || connectorKey == odooJSON2ConnectorKey || connectorKey == shareTConnectorKey || connectorKey == cloudQuerySummaryConnectorKey || connectorKey == airbyteInventoryConnectorKey || connectorKey == laroConnectorKey || connectorKey == openSpecArtifactConnectorKey || isManualPlanningContextOnlyConnector(connectorKey)
}

func filterConnectorLocalItems(items []ImportItem, connectorKey string) []ImportItem {
	allowed := map[string]bool{}
	itemType := ""
	switch connectorKey {
	case "email":
		allowed = map[string]bool{".mbox": true, ".eml": true}
		itemType = "email_export"
	case "calendar":
		allowed = map[string]bool{".ics": true}
		itemType = "calendar_export"
	case "project-board":
		allowed = map[string]bool{".json": true}
		itemType = "project_board_export"
	default:
		return items
	}
	filtered := make([]ImportItem, 0, len(items))
	for _, item := range items {
		ext := strings.ToLower(filepath.Ext(item.Title))
		if !allowed[ext] {
			continue
		}
		item.ItemType = itemType
		filtered = append(filtered, item)
	}
	return filtered
}

func minimalPermissions(category string, requested []string) []string {
	if len(requested) == 0 {
		return []string{"metadata:read", category + ":read"}
	}
	allowed := map[string]bool{"metadata:read": true, category + ":read": true, "selected-folder-read": true, "selected-chat-export-read": true, "selected-audio-folder-read": true, "explicit-consent": true, "herp:read": true, "odoo:read": true}
	result := []string{}
	for _, value := range requested {
		value = strings.TrimSpace(value)
		if allowed[value] {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return []string{"metadata:read"}
	}
	return result
}

func scoreExtraction(extraction models.SourceExtraction, request SearchRequest) (float64, string) {
	queryTokens := tokenSet(request.Query)
	textTokens := tokenSet(extraction.Text + " " + extraction.Summary + " " + extraction.Entities + " " + extraction.Tasks + " " + extraction.Decisions)
	relevance := overlapScore(queryTokens, textTokens)
	projectMatch := 0.0
	if request.ProjectKey != "" && request.ProjectKey == extraction.ProjectKey {
		projectMatch = 0.2
	}
	recency := recencyScore(extraction.UpdatedAt)
	provenance := 0.1
	if extraction.SourceURI == "" {
		provenance = 0
	}
	score := relevance*0.55 + projectMatch + recency*0.15 + provenance
	parts := []string{fmt.Sprintf("relevance %.2f", relevance), fmt.Sprintf("recency %.2f", recency)}
	if projectMatch > 0 {
		parts = append(parts, "same project")
	}
	if provenance > 0 {
		parts = append(parts, "source linked")
	}
	return score, strings.Join(parts, ", ")
}

func scheduledSourceDue(source models.ConnectedSource, now time.Time) (bool, string) {
	if source.ConnectorKey == "whisper-audio" {
		return false, "whisper-audio transcription is operator-triggered only"
	}
	if !sourceHasNativeAdapter(source.ConnectorKey) {
		return false, "scheduled adapter is not implemented for connector " + source.ConnectorKey
	}
	interval, ok := parseSyncFrequency(source.SyncFrequency)
	if !ok {
		return false, "sync frequency is manual or unsupported"
	}
	if source.LastSyncedAt == nil {
		return true, ""
	}
	if now.Sub(*source.LastSyncedAt) >= interval {
		return true, ""
	}
	return false, "not due yet"
}

type jsonFeedEnvelope struct {
	Items      []ImportItem `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

func fetchJSONFeed(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	if source == nil {
		return nil, "", fmt.Errorf("source is required")
	}
	target, err := url.Parse(strings.TrimSpace(source.SyncTarget))
	if err != nil || target.Scheme == "" || target.Hostname() == "" {
		return nil, "", fmt.Errorf("json-feed sync target must be an absolute HTTP(S) URL")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, "", fmt.Errorf("json-feed sync target must use HTTP or HTTPS")
	}
	if target.User != nil {
		return nil, "", fmt.Errorf("json-feed credentials must not be embedded in syncTarget")
	}
	if !sourceHTTPHostAllowed(target.Hostname()) {
		return nil, "", fmt.Errorf("json-feed host %s is not allowlisted; set CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS deliberately", target.Hostname())
	}
	if sourceHTTPAddressBlocked(target.Hostname()) {
		return nil, "", fmt.Errorf("json-feed target uses link-local, metadata, or unspecified address space")
	}
	if source.Cursor != "" {
		query := target.Query()
		query.Set("cursor", source.Cursor)
		target.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create json-feed request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	client := &http.Client{
		Timeout:   sourceHTTPTimeout(),
		Transport: sourceHTTPTransport(),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetch json-feed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("json-feed returned HTTP %d", response.StatusCode)
	}
	maxBytes := sourceHTTPMaxBytes()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read json-feed: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("json-feed response exceeds %d bytes", maxBytes)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return []ImportItem{}, source.Cursor, nil
	}
	if body[0] == '[' {
		var items []ImportItem
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, "", fmt.Errorf("decode json-feed items: %w", err)
		}
		return normalizeFeedItems(items, source), source.Cursor, nil
	}
	var envelope jsonFeedEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, "", fmt.Errorf("decode json-feed envelope: %w", err)
	}
	return normalizeFeedItems(envelope.Items, source), firstNonEmpty(strings.TrimSpace(envelope.NextCursor), source.Cursor), nil
}

func fetchGitHubSource(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	if source == nil {
		return nil, "", fmt.Errorf("source is required")
	}
	repository := strings.Trim(strings.TrimSpace(source.SyncTarget), "/")
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, "", fmt.Errorf("github syncTarget must be an owner/repository slug")
	}
	base := strings.TrimRight(firstNonEmpty(os.Getenv("GITHUB_SOURCE_API_BASE_URL"), "https://api.github.com"), "/")
	parsedBase, err := url.Parse(base)
	if err != nil || (parsedBase.Scheme != "http" && parsedBase.Scheme != "https") || parsedBase.Hostname() == "" {
		return nil, "", fmt.Errorf("GITHUB_SOURCE_API_BASE_URL must be an absolute HTTP(S) URL")
	}
	if !sourceHTTPHostAllowed(parsedBase.Hostname()) || sourceHTTPAddressBlocked(parsedBase.Hostname()) {
		return nil, "", fmt.Errorf("github API host %s is not allowlisted", parsedBase.Hostname())
	}
	endpoints := []struct {
		path string
		kind string
	}{
		{"/repos/" + repository, "repository"},
		{"/repos/" + repository + "/issues", "issue"},
		{"/repos/" + repository + "/pulls", "pull_request"},
		{"/repos/" + repository + "/commits", "commit"},
		{"/repos/" + repository + "/actions/runs", "workflow_run"},
	}
	items := []ImportItem{}
	latest := strings.TrimSpace(source.Cursor)
	for _, endpoint := range endpoints {
		value, err := fetchGitHubJSON(ctx, parsedBase, endpoint.path, source.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("fetch github %s: %w", endpoint.kind, err)
		}
		records := githubRecords(value, endpoint.kind)
		for _, record := range records {
			item, updated := githubImportItem(record, endpoint.kind, source.DefaultProjectKey, repository)
			if item.ExternalID == "" || item.Content == "" {
				continue
			}
			items = append(items, item)
			if updated > latest {
				latest = updated
			}
		}
	}
	return items, latest, nil
}

func fetchGitHubJSON(ctx context.Context, base *url.URL, resourcePath, cursor string) (any, error) {
	target := *base
	target.Path = strings.TrimRight(base.Path, "/") + resourcePath
	query := target.Query()
	query.Set("per_page", "100")
	query.Set("state", "all")
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	if cursor != "" && resourcePath != "" && resourcePath != "/repos/" {
		if _, err := time.Parse(time.RFC3339, cursor); err == nil && (strings.HasSuffix(resourcePath, "/issues") || strings.HasSuffix(resourcePath, "/pulls")) {
			query.Set("since", cursor)
		}
	}
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "HAI-connected-source")
	if token := strings.TrimSpace(os.Getenv("GITHUB_SOURCE_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: sourceHTTPTimeout(), Transport: sourceHTTPTransport(), CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, sourceHTTPMaxBytes()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > sourceHTTPMaxBytes() {
		return nil, fmt.Errorf("GitHub response exceeds %d bytes", sourceHTTPMaxBytes())
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, fmt.Errorf("decode GitHub response: %w", err)
	}
	return value, nil
}

func githubRecords(value any, kind string) []map[string]any {
	if object, ok := value.(map[string]any); ok {
		if kind == "workflow_run" {
			if runs, ok := object["workflow_runs"].([]any); ok {
				return githubRecordSlice(runs)
			}
		}
		return []map[string]any{object}
	}
	if list, ok := value.([]any); ok {
		return githubRecordSlice(list)
	}
	return nil
}

func githubRecordSlice(items []any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record, ok := item.(map[string]any); ok {
			result = append(result, record)
		}
	}
	return result
}

func githubImportItem(record map[string]any, kind, projectKey, repository string) (ImportItem, string) {
	identifier := githubString(record, "node_id", "id", "sha", "number", "name")
	if identifier == "" {
		return ImportItem{}, ""
	}
	title := githubString(record, "title", "full_name", "name", "display_title", "sha")
	if title == "" {
		title = kind + " from " + repository
	}
	body := githubString(record, "body", "message", "description", "name", "status")
	if nested, ok := record["commit"].(map[string]any); ok {
		body = firstNonEmpty(body, githubString(nested, "message"))
	}
	content := strings.TrimSpace(strings.Join([]string{
		"GitHub " + strings.ReplaceAll(kind, "_", " ") + " from " + repository,
		"Title: " + title,
		"Status: " + githubString(record, "state", "status", "conclusion"),
		body,
	}, "\n"))
	updated := githubString(record, "updated_at", "created_at", "run_started_at", "timestamp")
	return ImportItem{
		ExternalID: "github:" + kind + ":" + identifier,
		Title:      compact(title, 500),
		Content:    compact(content, 12000),
		SourceURI:  githubString(record, "html_url", "url"),
		ItemType:   "github_" + kind,
		ProjectKey: projectKey,
		Metadata:   fmt.Sprintf("source=github;repository=%s;kind=%s;updated=%s", repository, kind, updated),
	}, updated
}

func githubString(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := record[key]; ok {
			switch typed := value.(type) {
			case string:
				if strings.TrimSpace(typed) != "" {
					return strings.TrimSpace(typed)
				}
			case float64:
				return strconv.FormatInt(int64(typed), 10)
			}
		}
	}
	return ""
}

func normalizeFeedItems(items []ImportItem, source *models.ConnectedSource) []ImportItem {
	result := make([]ImportItem, 0, len(items))
	for _, item := range items {
		item.ExternalID = strings.TrimSpace(item.ExternalID)
		item.Title = strings.TrimSpace(item.Title)
		item.Content = strings.TrimSpace(item.Content)
		item.ProjectKey = firstNonEmpty(strings.TrimSpace(item.ProjectKey), source.DefaultProjectKey)
		if item.ExternalID == "" || item.Content == "" {
			continue
		}
		result = append(result, item)
	}
	return result
}

func sourceHTTPHostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, allowed := range strings.Split(firstNonEmpty(os.Getenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS"), defaultHTTPFeedAllowedHosts), ",") {
		if host == strings.ToLower(strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}

func sourceHTTPAddressBlocked(host string) bool {
	if envBool("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL") {
		return false
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.String() == "169.254.169.254"
}

func sourceHTTPTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: sourceHTTPTimeout()}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("invalid json-feed network address: %w", err)
			}
			resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve json-feed host: %w", err)
			}
			for _, candidate := range resolved {
				if sourceHTTPAddressBlocked(candidate.IP.String()) {
					return nil, fmt.Errorf("json-feed host resolved to blocked address space")
				}
			}
			if len(resolved) == 0 {
				return nil, fmt.Errorf("json-feed host resolved to no addresses")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(resolved[0].IP.String(), port))
		},
	}
}

func sourceHTTPTimeout() time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CONNECTED_SOURCE_HTTP_TIMEOUT_SECONDS")))
	if err != nil || seconds < 1 || seconds > 120 {
		seconds = 20
	}
	return time.Duration(seconds) * time.Second
}

func sourceHTTPMaxBytes() int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("CONNECTED_SOURCE_HTTP_MAX_BYTES")), 10, 64)
	if err != nil || value < 1024 || value > 20*1024*1024 {
		return 2 * 1024 * 1024
	}
	return value
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func parseSyncFrequency(value string) (time.Duration, bool) {
	clean := strings.TrimSpace(strings.ToLower(value))
	switch clean {
	case "", "manual", "off", "disabled", "none":
		return 0, false
	case "hourly":
		return time.Hour, true
	case "daily":
		return 24 * time.Hour, true
	case "weekly":
		return 7 * 24 * time.Hour, true
	}
	duration, err := time.ParseDuration(clean)
	if err != nil || duration < time.Minute {
		return 0, false
	}
	return duration, true
}

type whatsAppMessage struct {
	DateTime string
	Sender   string
	Body     string
}

func expandWhatsAppImportItems(items []ImportItem, projectKey, sourceName string, chunkMessages int) []ImportItem {
	if chunkMessages <= 0 || chunkMessages > 200 {
		chunkMessages = defaultWhatsAppChunkMessages
	}
	result := []ImportItem{}
	for _, item := range items {
		messages := parseWhatsAppMessages(item.Content)
		if len(messages) == 0 {
			normalized := item
			normalized.ItemType = firstNonEmpty(normalized.ItemType, "whatsapp_export")
			normalized.ProjectKey = firstNonEmpty(normalized.ProjectKey, projectKey)
			normalized.Metadata = firstNonEmpty(normalized.Metadata, "source=whatsapp-export;format=unparsed")
			result = append(result, normalized)
			continue
		}
		for start := 0; start < len(messages); start += chunkMessages {
			end := start + chunkMessages
			if end > len(messages) {
				end = len(messages)
			}
			window := messages[start:end]
			title := whatsAppWindowTitle(item, sourceName, window, start, end)
			result = append(result, ImportItem{
				ExternalID: firstNonEmpty(item.ExternalID, hashText(item.Title+item.SourceURI)) + fmt.Sprintf(":messages:%d-%d", start+1, end),
				Title:      title,
				Content:    renderWhatsAppWindow(window),
				SourceURI:  firstNonEmpty(item.SourceURI, "whatsapp-export://"+hashText(item.Title)),
				ItemType:   "whatsapp_chat_window",
				ProjectKey: firstNonEmpty(item.ProjectKey, projectKey),
				Metadata: fmt.Sprintf(
					"source=whatsapp-export;messages=%d;window_start=%d;window_end=%d;chat=%s",
					len(window),
					start+1,
					end,
					firstNonEmpty(item.Title, sourceName, "WhatsApp export"),
				),
			})
		}
	}
	return result
}

func parseWhatsAppMessages(content string) []whatsAppMessage {
	messages := []whatsAppMessage{}
	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		matches := whatsAppMessageLine.FindStringSubmatch(line)
		if len(matches) == 5 {
			messages = append(messages, whatsAppMessage{
				DateTime: strings.TrimSpace(matches[1] + " " + matches[2]),
				Sender:   strings.TrimSpace(matches[3]),
				Body:     strings.TrimSpace(matches[4]),
			})
			continue
		}
		if len(messages) > 0 {
			last := &messages[len(messages)-1]
			last.Body = strings.TrimSpace(last.Body + "\n" + strings.TrimSpace(line))
		}
	}
	return messages
}

func renderWhatsAppWindow(messages []whatsAppMessage) string {
	lines := []string{
		"WhatsApp conversation export window.",
		"Treat this as private connected-source evidence. Do not send, publish, or store as stable memory without Robert's approval.",
	}
	for _, message := range messages {
		lines = append(lines, fmt.Sprintf("%s | %s: %s", message.DateTime, message.Sender, message.Body))
	}
	return strings.Join(lines, "\n")
}

func whatsAppWindowTitle(item ImportItem, sourceName string, messages []whatsAppMessage, start, end int) string {
	chat := firstNonEmpty(item.Title, sourceName, "WhatsApp export")
	if len(messages) == 0 {
		return chat
	}
	return fmt.Sprintf("%s messages %d-%d (%s to %s)", chat, start+1, end, messages[0].DateTime, messages[len(messages)-1].DateTime)
}

func extractEntities(text string) []string {
	result := []string{}
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, ".,;:()[]")
		if len(word) > 2 && word[:1] == strings.ToUpper(word[:1]) {
			result = append(result, word)
		}
	}
	return uniqueStrings(limitValues(result, 20))
}

func extractDates(text string) []string {
	result := []string{}
	for _, word := range strings.Fields(text) {
		clean := strings.Trim(word, ".,;:()[]")
		lower := strings.ToLower(clean)
		if strings.Contains(clean, "202") || strings.Contains(lower, "deadline") || strings.Contains(lower, "tomorrow") || strings.Contains(lower, "today") || strings.Contains(lower, "morgen") || strings.Contains(lower, "vandaag") || strings.Contains(lower, "maandag") || strings.Contains(lower, "dinsdag") || strings.Contains(lower, "woensdag") || strings.Contains(lower, "donderdag") || strings.Contains(lower, "vrijdag") {
			result = append(result, clean)
		}
	}
	return uniqueStrings(limitValues(result, 20))
}

func extractTasks(text string) []string {
	return extractSentences(text, "todo", "must", "should", "need to", "action", "task", "moet", "moeten", "nodig", "actie", "taak", "regelen", "uitzoeken", "oppakken")
}

func extractDecisions(text string) []string {
	return extractSentences(text, "decided", "decision", "approved", "rejected", "agreed", "besloten", "beslissing", "goedgekeurd", "afgewezen", "akkoord", "afgesproken")
}

func extractFollowUps(text string) []string {
	return extractSentences(text, "follow up", "waiting", "open loop", "remind", "next", "opvolgen", "wachten", "wacht op", "herinner", "reminder", "reageer", "antwoord", "volgende")
}

func extractSentences(text string, needles ...string) []string {
	result := []string{}
	for _, sentence := range strings.Split(strings.ReplaceAll(text, "\n", ". "), ".") {
		lower := strings.ToLower(sentence)
		if containsAny(lower, needles...) {
			result = append(result, compact(sentence, 220))
		}
	}
	return uniqueStrings(limitValues(result, 12))
}

func shouldExclude(patterns, value string) bool {
	value = strings.ToLower(value)
	for _, pattern := range strings.Split(patterns, ",") {
		pattern = strings.TrimSpace(strings.ToLower(pattern))
		if pattern != "" && strings.Contains(value, pattern) {
			return true
		}
	}
	return false
}

func hashText(value string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strings.ToLower(normalizeSpaces(value))))
	return fmt.Sprintf("%x", hash.Sum64())
}

func slugText(value string) string {
	clean := strings.ToLower(strings.TrimSpace(value))
	builder := strings.Builder{}
	lastDash := false
	for _, r := range clean {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func normalizeSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func compact(value string, limit int) string {
	value = normalizeSpaces(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func joinValues(values []string) string {
	return strings.Join(uniqueStrings(values), ",")
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func limitValues(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func tokenSet(value string) map[string]bool {
	set := map[string]bool{}
	replacer := strings.NewReplacer(",", " ", ".", " ", ";", " ", ":", " ", "/", " ", "\\", " ", "\n", " ", "\t", " ", "(", " ", ")", " ")
	for _, token := range strings.Fields(strings.ToLower(replacer.Replace(value))) {
		if len(token) >= 3 {
			set[token] = true
		}
	}
	return set
}

func mapKeys(values map[string]bool) []string {
	keys := []string{}
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 80 {
		return keys[:80]
	}
	return keys
}

func overlapScore(queryTokens, textTokens map[string]bool) float64 {
	if len(queryTokens) == 0 {
		return 0.2
	}
	matches := 0
	for token := range queryTokens {
		if textTokens[token] {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTokens))
}

func recencyScore(value time.Time) float64 {
	days := time.Since(value).Hours() / 24
	if days <= 1 {
		return 1
	}
	if days >= 90 {
		return 0.05
	}
	return 1 - (days / 100)
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int64
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func resolveAllowedFolder(root, requested string) (string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("connected-source root is not accessible: %w", err)
	}
	rootAbs, err = filepath.Abs(rootResolved)
	if err != nil {
		return "", err
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = "."
	}
	if strings.ContainsAny(requested, "\r\n\x00") {
		return "", fmt.Errorf("folder path contains invalid characters")
	}
	var folderAbs string
	if filepath.IsAbs(requested) {
		folderAbs, err = filepath.Abs(filepath.Clean(requested))
	} else {
		folderAbs, err = filepath.Abs(filepath.Join(rootAbs, filepath.Clean(requested)))
	}
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, folderAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("folder path must stay inside allowlisted root %s", rootAbs)
	}
	info, err := os.Stat(folderAbs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("folder path is not a directory")
	}
	folderResolved, err := filepath.EvalSymlinks(folderAbs)
	if err != nil {
		return "", fmt.Errorf("folder path is not accessible: %w", err)
	}
	folderAbs, err = filepath.Abs(folderResolved)
	if err != nil {
		return "", err
	}
	rel, err = filepath.Rel(rootAbs, folderAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("folder path must not resolve outside allowlisted root %s", rootAbs)
	}
	return folderAbs, nil
}

func readLocalTextFile(path string, maxBytes int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(content)) > maxBytes {
		content = content[:maxBytes]
	}
	if strings.Contains(string(content), "\x00") {
		return "", fmt.Errorf("binary file skipped")
	}
	return string(content), nil
}

func isReadableLocalFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".yaml", ".yml", ".log", ".mbox", ".eml", ".ics":
		return true
	default:
		return false
	}
}

func localFileContentType(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return "local_file"
	}
	return "local_file_" + ext
}
