package source

import (
	"automation-hub-backend/internal/docling"
	"automation-hub-backend/internal/lifecycle"
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
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ModeManualImport                       = "manual_import"
	ModeScheduledSync                      = "scheduled_sync"
	ModeWebhookSync                        = "webhook_sync"
	ModeFolderWatcher                      = "folder_watcher"
	ModeHistoricalBackfill                 = "historical_backfill"
	ModeIncrementalSync                    = "incremental_sync"
	trelloClosedCardOwnerArchivedItemType  = "trello_card_closed_owner_archived"
	trelloUnavailableOwnerArchivedItemType = "trello_card_unavailable_owner_archived"
	doclingDocumentsConnectorKey           = "docling-documents"
)

var sharedSourceHTTPTransport struct {
	sync.Mutex
	key       string
	transport *http.Transport
}

var sourceFailureWindowsPath = regexp.MustCompile(`(?i)(^|[\s"'=(])[a-z]:[\\/][^\s"']+`)

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
	// controlledDocumentExtraction is deliberately package-private. Only the
	// server-side Docling handler can mark runner output as document-derived.
	controlledDocumentExtraction bool
	// manualProjectKeyOverride is set only by the durable manual-sync worker
	// when the caller explicitly supplied a project key.
	manualProjectKeyOverride bool
	// trelloLogicalJobID is an internal worker continuation identity. It is not
	// accepted from JSON and never allows callers to supply Trello records.
	trelloLogicalJobID uuid.UUID
	// trelloWebhookReceiptID is set only by the verified durable webhook worker.
	trelloWebhookReceiptID uuid.UUID
	// trelloWebhookReconciliation requests an owner-bound, per-card authoritative
	// refresh. It is package-private and only set by the verified webhook worker.
	trelloWebhookReconciliation bool
	ProjectKey                  string `json:"projectKey,omitempty"`
	Limit                       int    `json:"limit,omitempty"`
	MaxBytes                    int64  `json:"maxBytes,omitempty"`
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
	Extraction     models.SourceExtraction `json:"extraction"`
	Score          float64                 `json:"score"`
	Explanation    string                  `json:"explanation"`
	RequiresReview bool                    `json:"requiresReview"`
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
	PollingStatus     string     `json:"pollingStatus,omitempty"`
	PollingReason     string     `json:"pollingReason,omitempty"`
	WebhookStatus     string     `json:"webhookStatus,omitempty"`
	WebhookReason     string     `json:"webhookReason,omitempty"`
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

// ConnectionHealthBatchService derives overview health from sources already
// loaded for the authenticated owner. It avoids one HTTP request per source.
type ConnectionHealthBatchService interface {
	ConnectionHealths(sources []models.ConnectedSource) ([]ConnectionHealth, error)
}

type ContextSyncService interface {
	SyncContext(ctx context.Context, sourceID uuid.UUID, request ImportRequest) (*SyncResult, error)
}

type ContextScheduledSyncService interface {
	RunDueScheduledSyncsContext(context.Context, time.Time) (*ScheduledSyncRun, error)
	RunDueScheduledSyncsForOwnerContext(context.Context, time.Time, string) (*ScheduledSyncRun, error)
}

// WithRuntimeContext is called during API composition before publishing the service.
// Legacy task/agent-cycle Sync callers retain their semantics, but not detached children.
func WithRuntimeContext(value Service, ctx context.Context) Service {
	if concrete, ok := value.(*service); ok {
		concrete.runtimeContext = ctx
	}
	return value
}

func (s *service) compatibilityContext() context.Context {
	if s.runtimeContext != nil {
		return s.runtimeContext
	}
	return context.Background()
}

func runScheduledSyncsWithContext(ctx context.Context, service Service, now time.Time) (*ScheduledSyncRun, error) {
	if contextual, ok := service.(ContextScheduledSyncService); ok {
		return contextual.RunDueScheduledSyncsContext(ctx, now)
	}
	return service.RunDueScheduledSyncs(now)
}

func runOwnerScheduledSyncsWithContext(ctx context.Context, service Service, now time.Time, owner string) (*ScheduledSyncRun, error) {
	if contextual, ok := service.(ContextScheduledSyncService); ok {
		return contextual.RunDueScheduledSyncsForOwnerContext(ctx, now, owner)
	}
	return service.RunDueScheduledSyncsForOwner(now, owner)
}

type ExtractionPage struct {
	Items      []models.SourceExtraction `json:"items"`
	TotalCount int64                     `json:"totalCount"`
	Limit      int                       `json:"limit"`
}

type ExtractionPageService interface {
	ExtractionPageForOwner(ownerIdentity, projectKey string, includeArchived bool, limit int) (*ExtractionPage, error)
}

func syncSourceWithContext(ctx context.Context, service Service, sourceID uuid.UUID, request ImportRequest) (*SyncResult, error) {
	if contextual, ok := service.(ContextSyncService); ok {
		return contextual.SyncContext(ctx, sourceID, request)
	}
	return service.Sync(sourceID, request)
}

type Service interface {
	Connectors() ([]models.SourceConnector, error)
	CreateSource(request CreateSourceRequest) (*models.ConnectedSource, error)
	UpdateSource(id uuid.UUID, request UpdateSourceRequest) (*models.ConnectedSource, error)
	Sources(includeDisabled bool) ([]models.ConnectedSource, error)
	SyncJobs(sourceID *uuid.UUID) ([]models.SourceSyncJob, error)
	Sync(sourceID uuid.UUID, request ImportRequest) (*SyncResult, error)
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
	UpdateExtraction(id uuid.UUID, request models.SourceExtraction) (*models.SourceExtraction, error)
	ArchiveExtraction(id uuid.UUID, archived bool) (*models.SourceExtraction, error)
	DeleteExtraction(id uuid.UUID) error
	AuditLogs(sourceID *uuid.UUID) ([]models.SourceAuditLog, error)
	StartGoogleOAuth(sourceID uuid.UUID) (string, error)
	CompleteGoogleOAuth(ctx context.Context, code, state string) (uuid.UUID, error)
}

type service struct {
	repo                      Repository
	memoryService             memory.Service
	workflowService           workflow.Service
	pursuitLinker             pursuitAutoLinker
	semanticService           semantic.Service
	lifeOntologyProjector     LifeOntologyProjector
	finalEffectAuthorizer     FinalEffectAuthorizer
	runtimeContext            context.Context
	emergencyStop             func() safety.EmergencyStopDecision
	syncMu                    sync.Mutex
	activeSyncs               map[uuid.UUID]bool
	manualSyncWorkerMu        sync.RWMutex
	manualSyncWorkerReady     atomic.Bool
	manualOnlySyncWorkerReady atomic.Bool
}

func (s *service) setManualSyncWorkerReady(ready bool) {
	s.manualSyncWorkerMu.Lock()
	defer s.manualSyncWorkerMu.Unlock()
	s.manualSyncWorkerReady.Store(ready)
}

func (s *service) manualSyncWorkerAvailable() bool {
	return s.manualSyncWorkerReady.Load()
}

// sourceSyncLeaseRepository serializes every source writer across backend
// processes. Production sync must fail closed when this capability is absent.
type sourceSyncLeaseRepository interface {
	AcquireSourceSyncLease(ctx context.Context, sourceID uuid.UUID) (release func(), acquired bool, err error)
}

// acquireSourceExtractionLocks always acquires the source lease before the
// owner/extraction fence. Releasing the returned function unwinds in reverse
// order so every source mutation follows the same lock hierarchy.
func (s *service) acquireSourceExtractionLocks(
	ctx context.Context,
	sourceID uuid.UUID,
	ownerIdentity string,
	extractionID uuid.UUID,
) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	leaseRepository, ok := s.repo.(sourceSyncLeaseRepository)
	if !ok {
		return nil, ErrSourceSyncLeaseUnavailable
	}
	releaseSource, acquired, err := leaseRepository.AcquireSourceSyncLease(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("acquire source sync lease: %w", err)
	}
	if !acquired {
		return nil, ErrSyncInProgress
	}
	if releaseSource == nil {
		return nil, errors.New("source sync lease has no release function")
	}

	fence, ok := s.repo.(extractionCorrectionWorkerFence)
	if !ok {
		releaseSource()
		return nil, ErrSourceExtractionFenceUnavailable
	}
	releaseExtraction, acquired, err := fence.AcquireExtractionCorrectionSessionLock(ctx, ownerIdentity, extractionID)
	if err != nil {
		releaseSource()
		return nil, fmt.Errorf("acquire source extraction fence: %w", err)
	}
	if !acquired {
		releaseSource()
		return nil, ErrSyncInProgress
	}
	if releaseExtraction == nil {
		releaseSource()
		return nil, errors.New("source extraction fence has no release function")
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			releaseExtraction()
			releaseSource()
		})
	}, nil
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
	ErrSyncInProgress                   = errors.New("source sync is already in progress")
	ErrSourceSyncLeaseUnavailable       = errors.New("source sync lease is unavailable")
	ErrSourceExtractionFenceUnavailable = errors.New("source extraction fence is unavailable")
	ErrInvalidSyncRequest               = errors.New("invalid source sync request")
	ErrSourceSyncUnavailable            = errors.New("source is not enabled for sync")
	ErrSourceOwnerRequired              = errors.New("authenticated source owner is required")
)

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
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "real read-only Google OAuth adapter is implemented but GOOGLE_OAUTH_* or the dedicated HAI OAuth encryption/signing keys are not set"
			}
		}
	}
	// Same honesty for Trello: the live read-only REST adapter is implemented but
	// cannot connect until least-privilege credentials are configured.
	if !trelloConfigured() {
		for i := range connectors {
			if connectors[i].ConnectorKey == trelloConnectorKey {
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "implemented real read-only Trello REST adapter requires TRELLO_API_KEY, TRELLO_READ_TOKEN, TRELLO_ACCOUNT_OWNER_IDENTITY, and TRELLO_ACCOUNT_MEMBER_ID; the shared token is restricted to one HAI identity and its configured Trello account"
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
	if _, err := workerControlConfigFromEnv(); err != nil {
		for i := range connectors {
			if connectors[i].ConnectorKey == workerControlConnectorKey {
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "read-only Worker Control adapter is implemented but configuration is incomplete: " + err.Error()
			}
		}
	}
	if status := docling.DefaultService().Status(); !status.Configured {
		for i := range connectors {
			if connectors[i].ConnectorKey == doclingDocumentsConnectorKey {
				connectors[i].AdapterStatus = AdapterConfigurationRequired
				connectors[i].StatusReason = "local Docling extraction is implemented but configuration is incomplete: " + status.ConfigError
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
	if connectorKey == trelloConnectorKey {
		if err := validateTrelloSourceRequest(request, connector); err != nil {
			return nil, err
		}
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
	if connectorKey == workerControlConnectorKey {
		if _, err := workerControlConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("Worker Control connector requires explicit configuration: %w", err)
		}
		if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
			return nil, fmt.Errorf("Worker Control must use the operations category")
		}
		if request.LocalOnly {
			return nil, fmt.Errorf("Worker Control is an authenticated account bridge; localOnly must be false")
		}
		if strings.TrimSpace(request.SyncTarget) != "" {
			return nil, fmt.Errorf("Worker Control endpoint is configured only through HAI_WORKER_CONTROL_BASE_URL; syncTarget must be empty")
		}
	}
	if connectorKey == doclingDocumentsConnectorKey {
		if status := docling.DefaultService().Status(); !status.Configured {
			return nil, fmt.Errorf("Docling document extraction requires an enabled, configured local runner")
		}
		if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
			return nil, fmt.Errorf("Docling documents must use the %s category", connector.Category)
		}
		if !request.LocalOnly {
			return nil, fmt.Errorf("Docling document extraction is local-only; localOnly must be true")
		}
		if strings.TrimSpace(request.SyncTarget) == "" {
			return nil, fmt.Errorf("Docling document extraction requires an explicit selected folder under CONNECTED_SOURCE_LOCAL_ROOT")
		}
		if _, err := resolveAllowedFolder(firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources"), request.SyncTarget); err != nil {
			return nil, fmt.Errorf("Docling document folder is not allowed: %w", err)
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
	if !connector.Enabled {
		return nil, fmt.Errorf("connector %s is disabled", connectorKey)
	}
	if connector.AdapterStatus == AdapterConfigurationRequired {
		return nil, fmt.Errorf("connector %s requires configuration before it can be connected", connectorKey)
	}
	if !adapterIsUsable(connector.AdapterStatus) {
		return nil, fmt.Errorf("connector %s is registered but its real adapter is not implemented yet", connectorKey)
	}
	category := firstNonEmpty(request.Category, connector.Category)
	if category == "" {
		return nil, fmt.Errorf("category is required")
	}
	syncFrequency, err := validatedSyncFrequency(connectorKey, request.SyncFrequency)
	if err != nil {
		return nil, err
	}
	syncTarget := strings.TrimSpace(request.SyncTarget)
	if connectorKey == trelloConnectorKey {
		syncTarget, err = trelloBoardID(syncTarget)
		if err != nil {
			return nil, err
		}
	}
	source := &models.ConnectedSource{
		OwnerIdentity:     strings.TrimSpace(request.OwnerIdentity),
		ConnectorKey:      connectorKey,
		Name:              name,
		Category:          category,
		Enabled:           request.Enabled,
		LocalOnly:         request.LocalOnly,
		SyncFrequency:     syncFrequency,
		SyncTarget:        syncTarget,
		DefaultProjectKey: strings.TrimSpace(request.DefaultProjectKey),
		IngestionModes:    joinValues(defaultModes(request.IngestionModes)),
		Permissions:       joinValues(minimalPermissions(category, request.Permissions)),
		ExcludePatterns:   joinValues(request.ExcludePatterns),
		Status:            "active",
	}
	if sourceUsesLocalFolder(connectorKey) && strings.TrimSpace(source.SyncTarget) == "" {
		return nil, fmt.Errorf("connector %s requires an explicit selected folder under CONNECTED_SOURCE_LOCAL_ROOT", connectorKey)
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
	if source.RevokedAt != nil || strings.EqualFold(strings.TrimSpace(source.Status), "revoked") {
		return nil, ErrSourceRevoked
	}
	if source.ConnectorKey == trelloConnectorKey {
		if request.LocalOnly != nil && *request.LocalOnly {
			return nil, fmt.Errorf("Trello is a remote read-only source; localOnly must remain false")
		}
		if request.SyncTarget != nil {
			requestedBoard, targetErr := trelloBoardID(*request.SyncTarget)
			if targetErr != nil {
				return nil, targetErr
			}
			currentBoard, currentErr := trelloBoardID(source.SyncTarget)
			if currentErr != nil {
				return nil, fmt.Errorf("stored Trello board target is invalid; reconnect this source")
			}
			sameBoard := requestedBoard == currentBoard
			requestedCanonicalID, requestedIsCanonicalID := normalizedTrelloMongoID(requestedBoard)
			currentCanonicalID, currentIsCanonicalID := normalizedTrelloMongoID(currentBoard)
			if requestedIsCanonicalID && currentIsCanonicalID {
				sameBoard = requestedCanonicalID == currentCanonicalID
			}
			if !sameBoard {
				return nil, fmt.Errorf("Trello board targets are immutable after connection; create a new source to connect another board")
			}
			// Preserve the existing representation so the durable sync state's board
			// binding remains stable across harmless ObjectID casing changes.
			request.SyncTarget = &currentBoard
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
	if source.ConnectorKey == workerControlConnectorKey {
		if _, err := workerControlConfigFromEnv(); err != nil {
			return nil, fmt.Errorf("Worker Control connector requires explicit configuration: %w", err)
		}
		if request.LocalOnly != nil && *request.LocalOnly {
			return nil, fmt.Errorf("Worker Control is an authenticated account bridge; localOnly must remain false")
		}
		if request.SyncTarget != nil && strings.TrimSpace(*request.SyncTarget) != "" {
			return nil, fmt.Errorf("Worker Control endpoint is configured only through HAI_WORKER_CONTROL_BASE_URL; syncTarget must remain empty")
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
		syncFrequency, err := validatedSyncFrequency(source.ConnectorKey, request.SyncFrequency)
		if err != nil {
			return nil, err
		}
		source.SyncFrequency = syncFrequency
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
	return s.repo.FindSourcesVisibleToOwner(ownerIdentity, includeDisabled)
}

func (s *service) MutableSourceForOwner(id uuid.UUID, ownerIdentity string) (*models.ConnectedSource, error) {
	return s.repo.FindMutableSourceForOwner(id, ownerIdentity)
}

func (s *service) MutableExtractionForOwner(id uuid.UUID, ownerIdentity string) (*models.SourceExtraction, error) {
	return s.repo.FindMutableExtractionForOwner(id, ownerIdentity)
}

func (s *service) SyncJobs(sourceID *uuid.UUID) ([]models.SourceSyncJob, error) {
	return s.repo.FindSyncJobs(sourceID)
}

func (s *service) SyncJobsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceSyncJob, error) {
	return s.repo.FindSyncJobsForSources(sourceIDs, limit)
}

func (s *service) ConnectionHealth(sourceID uuid.UUID) (*ConnectionHealth, error) {
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return nil, err
	}
	return s.connectionHealthForSource(*source)
}

func (s *service) ConnectionHealths(sources []models.ConnectedSource) ([]ConnectionHealth, error) {
	googleSourceIDs := make([]uuid.UUID, 0, len(sources))
	if googleOAuthReady() {
		for _, source := range sources {
			if isGoogleOAuthConnector(source.ConnectorKey) && source.Status != "revoked" && source.RevokedAt == nil {
				googleSourceIDs = append(googleSourceIDs, source.ID)
			}
		}
	}
	tokens, err := s.repo.FindOAuthTokensForSources(googleSourceIDs)
	if err != nil {
		return nil, err
	}
	tokensBySourceID := make(map[uuid.UUID]*models.SourceOAuthToken, len(tokens))
	for index := range tokens {
		tokensBySourceID[tokens[index].SourceID] = &tokens[index]
	}
	health := make([]ConnectionHealth, 0, len(sources))
	for _, source := range sources {
		item, err := s.connectionHealthForSourceWithToken(source, tokensBySourceID[source.ID])
		if err != nil {
			return nil, err
		}
		health = append(health, *item)
	}
	return health, nil
}

func (s *service) connectionHealthForSource(source models.ConnectedSource) (*ConnectionHealth, error) {
	var token *models.SourceOAuthToken
	if isGoogleOAuthConnector(source.ConnectorKey) && googleOAuthReady() && source.Status != "revoked" && source.RevokedAt == nil {
		stored, err := s.repo.FindOAuthToken(source.ID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("load Google OAuth token for connection health: %w", err)
		}
		if err == nil {
			token = stored
		}
	}
	return s.connectionHealthForSourceWithToken(source, token)
}

type trelloWebhookHealthRepository interface {
	FindLatestTrelloWebhookReceipt(sourceID uuid.UUID) (*models.TrelloWebhookReceipt, error)
}

func (s *service) trelloWebhookHealthStatus(sourceID uuid.UUID) (status, reason string, err error) {
	if _, _, err := trelloWebhookConfiguration(); err != nil {
		return "unconfigured", "Trello webhook callback URL and signing secret are not both configured correctly; API polling can operate independently", nil
	}
	repository, ok := s.repo.(trelloWebhookHealthRepository)
	if !ok {
		return "unverified", "callback configuration is present, but persisted signed-callback receipt evidence is unavailable; Trello-side registration and current provider health are not verified", nil
	}
	receipt, err := repository.FindLatestTrelloWebhookReceipt(sourceID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "unverified", "callback configuration is present, but no signed callback receipt documenting delivery has been persisted; Trello-side registration and current provider health are not verified", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("load latest Trello webhook receipt for connection health: %w", err)
	}
	if receipt == nil || receipt.ID == uuid.Nil || receipt.SourceID != sourceID || receipt.ReceivedAt.IsZero() {
		return "unverified", "callback configuration is present, but the latest persisted callback receipt is incomplete; Trello-side registration and current provider health are not verified", nil
	}
	receivedAt := receipt.ReceivedAt.UTC()
	age := time.Since(receivedAt)
	if age < 0 {
		return "unverified", "the latest persisted Trello callback receipt has a future timestamp and cannot be used as delivery evidence; registration and current provider health are not verified", nil
	}
	receiptTime := receivedAt.Format(time.RFC3339)
	if age > trelloWebhookReceiptFreshness {
		return "stale", "the latest signed Trello callback receipt was persisted at " + receiptTime + "; this evidence is stale and does not verify current delivery, Trello-side registration, or provider health", nil
	}
	return "delivery_observed", "a signed Trello callback was accepted and persisted at " + receiptTime + "; this is receipt evidence only and does not verify current delivery, Trello-side registration, or provider health", nil
}

func (s *service) connectionHealthForSourceWithToken(source models.ConnectedSource, token *models.SourceOAuthToken) (*ConnectionHealth, error) {
	health := &ConnectionHealth{
		SourceID: source.ID, ConnectorKey: source.ConnectorKey,
		Status: source.Status, Configured: true, LastSyncedAt: source.LastSyncedAt,
	}
	if source.ConnectorKey == trelloConnectorKey {
		health.PollingStatus = source.Status
		health.PollingReason = "Trello API polling health has not been verified by a successful read-only sync"
		webhookStatus, webhookReason, webhookErr := s.trelloWebhookHealthStatus(source.ID)
		if webhookErr != nil {
			return nil, webhookErr
		}
		health.WebhookStatus, health.WebhookReason = webhookStatus, webhookReason
	}
	if source.Status == "revoked" || source.RevokedAt != nil {
		health.Status = "revoked"
		health.Reason = "source access was revoked"
		if source.ConnectorKey == trelloConnectorKey {
			health.PollingStatus = health.Status
			health.PollingReason = health.Reason
		}
		return health, nil
	}
	if source.ConnectorKey == trelloConnectorKey {
		health.Configured = trelloConfigured()
		if !health.Configured {
			health.Status = "configuration_required"
			health.Reason = "Trello read-only credentials are not configured"
			health.PollingStatus, health.PollingReason = health.Status, health.Reason
			return health, nil
		}
		if !trelloCredentialsBelongToOwner(source.OwnerIdentity) {
			health.Configured = false
			health.Status = "configuration_required"
			health.Reason = "Trello read-only credentials are assigned to a different HAI owner"
			health.PollingStatus, health.PollingReason = health.Status, health.Reason
			return health, nil
		}
		if source.LocalOnly {
			health.Status = "configuration_required"
			health.Reason = "Trello board sources must remain remote read-only; reconnect with local-only disabled"
			health.PollingStatus, health.PollingReason = health.Status, health.Reason
			return health, nil
		}
		if _, err := trelloBoardID(source.SyncTarget); err != nil {
			health.Status = "configuration_required"
			health.Reason = "Trello board target needs review: " + err.Error()
			health.PollingStatus, health.PollingReason = health.Status, health.Reason
			return health, nil
		}
		if !source.Enabled || strings.EqualFold(strings.TrimSpace(source.Status), "paused") {
			health.Authorized = false
			health.Status = "paused"
			if source.LastSyncedAt != nil {
				health.Reason = "Trello source is paused; a successful read-only sync last verified board access at " + source.LastSyncedAt.UTC().Format(time.RFC3339)
			} else {
				health.Reason = "Trello source is paused"
			}
			health.PollingStatus, health.PollingReason = health.Status, health.Reason
			return health, nil
		}
		latestJobs, err := s.repo.FindSyncJobsForSources([]uuid.UUID{source.ID}, 1)
		if err != nil {
			return nil, fmt.Errorf("load latest Trello sync status: %w", err)
		}
		if len(latestJobs) != 0 {
			latestStatus := strings.ToLower(strings.TrimSpace(latestJobs[0].Status))
			switch latestStatus {
			case "completed":
				if source.LastSyncedAt != nil {
					age := time.Since(source.LastSyncedAt.UTC())
					if age >= 0 && age <= maxSchedulerInterval {
						health.Authorized = true
						health.Status = "polling_operational"
						health.Reason = "Trello read-only API polling is operational based on the latest complete sync at " + source.LastSyncedAt.UTC().Format(time.RFC3339) + "; this verifies polling only, not webhook registration or delivery"
						health.PollingStatus, health.PollingReason = health.Status, health.Reason
						return health, nil
					}
				}
			case "failed", "partial_failure":
				health.Authorized = false
				health.Status = "sync_failed"
				health.Reason = "the latest Trello read-only sync failed; review sync history before relying on this source"
				if latestStatus == "partial_failure" {
					health.Status = "sync_partial_failure"
					health.Reason = "the latest Trello read-only sync imported only part of the available data; review failed records before relying on this source"
				}
				if source.LastSyncedAt != nil {
					health.Reason += "; last fully successful access was " + source.LastSyncedAt.UTC().Format(time.RFC3339)
				}
				health.PollingStatus, health.PollingReason = health.Status, health.Reason
				return health, nil
			case "running":
				health.Status = "sync_running"
				health.Reason = "a Trello read-only sync is currently running; its result is not yet verified"
				if source.LastSyncedAt != nil {
					health.Reason += "; last fully successful access was " + source.LastSyncedAt.UTC().Format(time.RFC3339)
				}
				health.PollingStatus, health.PollingReason = health.Status, health.Reason
				return health, nil
			case "queued":
				health.Status = "sync_queued"
				health.Reason = "a Trello read-only sync is queued; its result is not yet verified"
				if source.LastSyncedAt != nil {
					health.Reason += "; last fully successful access was " + source.LastSyncedAt.UTC().Format(time.RFC3339)
				}
				health.PollingStatus, health.PollingReason = health.Status, health.Reason
				return health, nil
			case "cancelled":
				health.Authorized = false
				health.Status = "sync_cancelled"
				health.Reason = "the latest Trello read-only sync was cancelled; run a fresh sync to verify current access"
				if source.LastSyncedAt != nil {
					health.Reason += "; last fully successful access was " + source.LastSyncedAt.UTC().Format(time.RFC3339)
				}
				health.PollingStatus, health.PollingReason = health.Status, health.Reason
				return health, nil
			}
		}
		// Environment variables and a syntactically valid board id prove only
		// that the connector can be attempted. They do not prove that Trello
		// accepted the token or that it can read this board, so do not surface a
		// false "ready" state before an explicit sync provides evidence.
		health.Authorized = false
		if source.Enabled && source.LastSyncedAt != nil {
			health.Status = "previously_verified"
			health.Reason = "a successful read-only sync verified board access at " + source.LastSyncedAt.UTC().Format(time.RFC3339) + "; run a fresh sync to verify current access"
		} else if source.Enabled {
			health.Status = "configuration_ready"
			health.Reason = "least-privilege read-only Trello credentials and board target are configured; run a sync to verify live board access"
		}
		health.PollingStatus, health.PollingReason = health.Status, health.Reason
		return health, nil
	}
	if source.ConnectorKey == doclingDocumentsConnectorKey {
		status := docling.DefaultService().Status()
		health.Configured = status.Configured
		if !health.Configured {
			health.Status = "configuration_required"
			health.Reason = "local Docling runner is not configured"
			return health, nil
		}
		if !source.LocalOnly || strings.TrimSpace(source.SyncTarget) == "" {
			health.Status = "configuration_required"
			health.Reason = "Docling sources require one explicit local-only document folder"
			return health, nil
		}
		health.Authorized = source.Enabled
		health.Status = "configuration_ready"
		health.Reason = "local Docling runner and selected document folder are configured; run extraction to verify the local runner"
		return health, nil
	}
	if !isGoogleOAuthConnector(source.ConnectorKey) {
		health.Authorized = source.Enabled
		health.Reason = "connector health is derived from its enabled and synchronization state"
		return health, nil
	}
	if !source.Enabled || strings.EqualFold(strings.TrimSpace(source.Status), "paused") {
		health.Status = "paused"
		health.Reason = "Google source is paused; reconnect status and token expiry are not currently actionable"
		return health, nil
	}
	health.Configured = googleOAuthReady()
	if !health.Configured {
		health.Status = "configuration_required"
		health.Reason = "Google OAuth client, token encryption key, or state signing key is not configured"
		return health, nil
	}
	if strings.EqualFold(strings.TrimSpace(source.Status), "reconnect_required") {
		health.Status = "reconnect_required"
		health.RequiresReconnect = true
		health.Reason = "Google rejected the stored authorization; reconnect this account before syncing"
		return health, nil
	}
	if strings.TrimSpace(source.OwnerIdentity) == "" {
		health.Status = "configuration_required"
		health.Reason = "Google source must be bound to an authenticated owner before syncing"
		return health, nil
	}
	if token == nil {
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
	_, scopeErr := validateGoogleOAuthScope(token.Scope, requiredScope)
	if token.SourceID != source.ID || token.Provider != googleProvider ||
		len(token.AccessToken) == 0 || len(token.RefreshToken) == 0 ||
		strings.TrimSpace(token.Scope) == "" || scopeErr != nil {
		health.Status = "reconnect_required"
		health.RequiresReconnect = true
		health.Reason = "stored Google grant needs its source binding, provider, credentials, and exact read-only scope verified; reconnect this account"
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

func validateTrelloSourceRequest(request CreateSourceRequest, connector models.SourceConnector) error {
	if !trelloConfigured() {
		return fmt.Errorf("Trello connector requires %s, %s, %s, and a valid %s before a board can be connected", trelloAPIKeyEnv, trelloReadTokenEnv, trelloOwnerIdentityEnv, trelloAccountMemberIDEnv)
	}
	if !trelloCredentialsBelongToOwner(request.OwnerIdentity) {
		return fmt.Errorf("Trello credentials are restricted to the configured HAI account owner")
	}
	if request.LocalOnly {
		return fmt.Errorf("Trello is a remote read-only source; localOnly must be false")
	}
	if category := strings.TrimSpace(request.Category); category != "" && category != connector.Category {
		return fmt.Errorf("Trello must use the %s category", connector.Category)
	}
	if _, err := trelloBoardID(request.SyncTarget); err != nil {
		return err
	}
	if _, err := trelloBaseURL(); err != nil {
		return fmt.Errorf("Trello API configuration is invalid: %w", err)
	}
	return nil
}

func (s *service) Sync(sourceID uuid.UUID, request ImportRequest) (*SyncResult, error) {
	return s.SyncContext(s.compatibilityContext(), sourceID, request)
}

func (s *service) SyncContext(ctx context.Context, sourceID uuid.UUID, request ImportRequest) (result *SyncResult, resultErr error) {
	return s.syncContext(ctx, sourceID, request, uuid.Nil)
}

func (s *service) syncContext(ctx context.Context, sourceID uuid.UUID, request ImportRequest, manualSyncJobID uuid.UUID) (result *SyncResult, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = redactSourceError(resultErr)
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = lifecycle.WithOwnership(ctx, s.runtimeContext)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	finish, admitted := lifecycle.Enter(ctx, "source-sync")
	if !admitted {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, context.Canceled
	}
	defer finish()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Individual connector requests already have tight network timeouts, but a
	// full sync can involve several requests plus bounded extraction and
	// persistence work. Give every invocation an overall deadline as well so a
	// slow upstream or database cannot retain a durable worker indefinitely.
	// A caller-provided deadline remains authoritative when it is sooner.
	syncCtx, cancel := context.WithTimeout(ctx, sourceSyncTimeout())
	defer cancel()
	ctx = syncCtx
	if !s.beginSync(sourceID) {
		return nil, ErrSyncInProgress
	}
	defer s.endSync(sourceID)
	leaseRepo, ok := s.repo.(sourceSyncLeaseRepository)
	if !ok {
		return nil, ErrSourceSyncLeaseUnavailable
	}
	release, acquired, err := leaseRepo.AcquireSourceSyncLease(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("acquire source sync lease: %w", err)
	}
	if !acquired {
		return nil, ErrSyncInProgress
	}
	if release == nil {
		return nil, errors.New("source sync lease has no release function")
	}
	defer release()

	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return nil, err
	}
	var trelloCommentRefresh *trelloWebhookCommentRefresh
	if request.trelloWebhookReconciliation {
		if source.ConnectorKey != trelloConnectorKey || request.trelloWebhookReceiptID == uuid.Nil || len(request.Items) != 0 || manualSyncJobID != uuid.Nil {
			return nil, fmt.Errorf("%w: Trello webhook reconciliation must be an internal, item-free comment refresh", ErrInvalidSyncRequest)
		}
		repository, ok := s.repo.(trelloWebhookRepository)
		if !ok {
			return nil, errors.New("durable Trello webhook repository is unavailable")
		}
		receipt, receiptErr := repository.FindTrelloWebhookReceipt(source.ID, request.trelloWebhookReceiptID)
		if receiptErr != nil {
			return nil, fmt.Errorf("load Trello comment webhook receipt: %w", receiptErr)
		}
		if err := validateTrelloWebhookCommentRefresh(source, receipt, s.repo); err != nil {
			return nil, fmt.Errorf("validate Trello comment webhook reconciliation: %w", err)
		}
		trelloCommentRefresh = &trelloWebhookCommentRefresh{Receipt: receipt}
	}
	originalLastSyncedAt := source.LastSyncedAt
	if !source.Enabled || source.Status == "paused" || source.Status == "revoked" {
		return nil, ErrSourceSyncUnavailable
	}
	if source.ConnectorKey == trelloConnectorKey {
		if stateRepository, ok := s.repo.(trelloSyncStateRepository); ok {
			state, stateErr := stateRepository.FindTrelloSyncState(source.ID)
			if stateErr != nil && !errors.Is(stateErr, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("load Trello sync checkpoint: %w", stateErr)
			}
			if stateErr == nil && state.LogicalJobID != nil {
				if request.trelloWebhookReconciliation {
					return nil, ErrTrelloSyncAlreadyActive
				}
				if manualSyncJobID != uuid.Nil && *state.LogicalJobID != manualSyncJobID {
					return nil, ErrTrelloSyncAlreadyActive
				}
				if manualSyncJobID == uuid.Nil && request.trelloWebhookReceiptID == uuid.Nil {
					request.trelloLogicalJobID = *state.LogicalJobID
				}
			}
		}
	}
	if source.ConnectorKey == trelloConnectorKey && len(request.Items) != 0 && !isTrelloWebhookReceiptItem(source, request, s.repo) {
		return nil, fmt.Errorf("%w: Trello items are read only from the configured Trello API; caller-supplied items are not accepted", ErrInvalidSyncRequest)
	}
	if source.ConnectorKey == "github" && len(request.Items) != 0 {
		return nil, fmt.Errorf("%w: GitHub quality evidence must come from the configured GitHub API adapter; caller-supplied items are not accepted", ErrInvalidSyncRequest)
	}
	if source.ConnectorKey == "whisper-audio" && !request.controlledTranscription {
		return nil, fmt.Errorf("%w: whisper-audio sources must use the controlled transcription route", ErrInvalidSyncRequest)
	}
	if source.ConnectorKey == doclingDocumentsConnectorKey && !request.controlledDocumentExtraction {
		return nil, fmt.Errorf("%w: Docling document sources must use the controlled document extraction route", ErrInvalidSyncRequest)
	}
	if source.ConnectorKey == cloudQuerySummaryConnectorKey {
		if !source.LocalOnly || strings.TrimSpace(source.SyncTarget) != "" {
			return nil, fmt.Errorf("%w: CloudQuery sync summaries must remain local-only with the environment-configured summary path", ErrInvalidSyncRequest)
		}
		if len(request.Items) != 0 {
			return nil, fmt.Errorf("%w: CloudQuery sync summaries must be read from the configured local summary file; manual items are not accepted", ErrInvalidSyncRequest)
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
		if manualSyncJobID == uuid.Nil {
			source.DefaultProjectKey = request.ProjectKey
		}
	}
	mode := firstNonEmpty(request.Mode, ModeManualImport)
	started := time.Now().UTC()
	var job *models.SourceSyncJob
	if manualSyncJobID != uuid.Nil {
		repository, ok := s.repo.(manualSyncRepository)
		if !ok {
			return nil, errors.New("durable manual source sync repository is unavailable")
		}
		job, err = repository.StartManualSyncJob(manualSyncJobID, sourceID, started)
	} else if source.ConnectorKey == trelloConnectorKey && request.trelloLogicalJobID != uuid.Nil {
		stateRepository, ok := s.repo.(trelloSyncStateRepository)
		if !ok {
			return nil, errors.New("durable Trello sync repository is unavailable")
		}
		job, err = stateRepository.StartTrelloSyncJob(request.trelloLogicalJobID, sourceID, started)
	} else {
		job, err = s.repo.CreateSyncJob(&models.SourceSyncJob{
			SourceID:     sourceID,
			Mode:         mode,
			Status:       "running",
			CursorBefore: source.Cursor,
			CursorAfter:  source.Cursor,
			StartedAt:    started,
		})
	}
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
	recordFailure := func(item ImportItem, stage string, err error) {
		failed++
		if len(itemErrors) < maxSyncErrorDetails {
			itemErrors = append(itemErrors, itemFailure(item, stage, err))
		}
	}
	items := request.Items
	adapterCursor := ""
	var trelloSlice *trelloSyncSlice
	var trelloExistingRawItems []models.SourceRawItem
	trelloExistingRawItemsLoaded := false
	if trelloCommentRefresh != nil {
		*trelloCommentRefresh, err = s.fetchTrelloWebhookCommentRefresh(ctx, source, trelloCommentRefresh.Receipt)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", job.Message)
			return nil, err
		}
		items = trelloCommentRefresh.Items
	}
	if len(items) == 0 && sourceUsesLocalFolder(source.ConnectorKey) {
		items, err = s.localFolderItems(source, request)
		items = filterConnectorLocalItems(items, source.ConnectorKey)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == "json-feed" {
		items, adapterCursor, err = fetchJSONFeed(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == "github" {
		items, adapterCursor, err = fetchGitHubSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == gmailConnectorKey {
		items, adapterCursor, err = s.fetchGmailSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == driveConnectorKey {
		items, adapterCursor, err = s.fetchDriveSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == contactsConnectorKey {
		items, adapterCursor, err = s.fetchContactsSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == calendarConnectorKey {
		items, adapterCursor, err = s.fetchCalendarSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
	}
	if len(items) == 0 && source.ConnectorKey == trelloConnectorKey {
		if _, durable := s.repo.(trelloSyncStateRepository); durable {
			trelloSlice, err = s.prepareTrelloSyncSlice(ctx, source, job)
			if err != nil {
				now := time.Now().UTC()
				job.Status = "failed"
				job.Message = sourceOperationalFailureMessage(err)
				job.CompletedAt = &now
				_, _ = s.repo.UpdateSyncJob(job)
				s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
				return nil, err
			}
			items = trelloSlice.Items
			adapterCursor = trelloSlice.FinalCursor
		} else {
			// Narrow compatibility for simple non-durable repository adapters used
			// by connector unit tests. The production GormRepository never takes
			// this full-snapshot path.
			trelloExistingRawItems, err = s.repo.FindRawItems(source.ID)
			if err == nil {
				trelloExistingRawItemsLoaded = true
				items, adapterCursor, err = fetchTrelloSourceWithExistingItems(ctx, source, trelloExistingRawItems)
			}
			if err != nil {
				now := time.Now().UTC()
				job.Status = "failed"
				job.Message = sourceOperationalFailureMessage(err)
				job.CompletedAt = &now
				_, _ = s.repo.UpdateSyncJob(job)
				s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
				return nil, err
			}
		}
	}
	if len(items) == 0 && source.ConnectorKey == odooJSON2ConnectorKey {
		items, adapterCursor, err = fetchOdooJSON2Source(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.odoo_json2_read", fmt.Sprintf("read %d bounded Odoo JSON-2 record(s) through the configured model allowlist", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == shareTConnectorKey {
		items, adapterCursor, err = fetchShareTSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.sharet_read", fmt.Sprintf("read %d bounded ShareT link record(s) through a read-only connector credential", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == cloudQuerySummaryConnectorKey {
		items, adapterCursor, err = fetchCloudQuerySummary(source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.cloudquery_summary_read", fmt.Sprintf("read %d bounded CloudQuery sync summary record(s) from the configured local summary file", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == airbyteInventoryConnectorKey {
		items, adapterCursor, err = fetchAirbyteInventory(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.airbyte_inventory_read", fmt.Sprintf("read %d bounded Airbyte source and connection inventory record(s) from approved workspaces", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == laroConnectorKey {
		items, adapterCursor, err = fetchLAROSource(ctx, source)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.laro_read", fmt.Sprintf("read %d bounded, owner-scoped LARO legal record(s)", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == workerControlConnectorKey {
		items, adapterCursor, err = fetchWorkerControlSource(ctx, source.Cursor, source.DefaultProjectKey)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.worker_control_read", fmt.Sprintf("read %d bounded, owner-scoped Worker Control event(s)", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == openSpecArtifactConnectorKey {
		items, err = s.openSpecArtifactItems(source, request)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.openspec_artifacts_read", fmt.Sprintf("read %d bounded OpenSpec change artifact bundle(s) from the selected local project", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == projectInstructionsConnectorKey {
		items, err = s.projectInstructionItems(source, request)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.project_instructions_read", fmt.Sprintf("read %d untrusted project instruction file(s) from the selected local project", len(items)))
	}
	if len(items) == 0 && source.ConnectorKey == fabricPatternsConnectorKey {
		items, err = s.fabricPatternItems(source, request)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
			return nil, err
		}
		s.audit(sourceID, "source.fabric_patterns_read", fmt.Sprintf("read %d bounded untrusted Fabric prompt pattern(s) from the selected local folder", len(items)))
	}
	if source.ConnectorKey == "whatsapp-export" {
		items, err = s.whatsAppExportItems(source, request)
		if err != nil {
			now := time.Now().UTC()
			job.Status = "failed"
			job.Message = sourceOperationalFailureMessage(err)
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_failed", sourceOperationalFailureMessage(err))
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
			job.Message = sourceOperationalFailureMessage(errExisting)
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
	trelloRawByExternalID := map[string]*models.SourceRawItem{}
	var trelloClosureLookupErr error
	if source.ConnectorKey == trelloConnectorKey && hasTrelloStateItems(items) {
		if trelloExistingRawItemsLoaded {
			for index := range trelloExistingRawItems {
				raw := &trelloExistingRawItems[index]
				trelloRawByExternalID[raw.ExternalID] = raw
			}
		} else {
			for _, item := range items {
				if !isTrelloCardStateItem(item.ItemType) {
					continue
				}
				raw, lookupErr := s.repo.FindRawItem(source.ID, item.ExternalID)
				if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
					continue
				}
				if lookupErr != nil {
					trelloClosureLookupErr = lookupErr
					break
				}
				trelloRawByExternalID[raw.ExternalID] = raw
			}
		}
		if trelloClosureLookupErr == nil {
			filtered := make([]ImportItem, 0, len(items))
			for _, item := range items {
				if !isTrelloCardStateItem(item.ItemType) {
					filtered = append(filtered, item)
					continue
				}
				raw := trelloRawByExternalID[item.ExternalID]
				if raw == nil {
					continue
				}
				if item.ItemType == trelloClosedCardItemType && raw.ItemType != trelloClosedCardItemType {
					filtered = append(filtered, item)
				} else if item.ItemType == trelloUnavailableCardItemType && raw.ItemType == "trello_card" {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
	}
	if source.ConnectorKey == trelloConnectorKey && manualSyncJobID != uuid.Nil && request.manualProjectKeyOverride {
		if projectKey := strings.TrimSpace(request.ProjectKey); projectKey != "" {
			for index := range items {
				items[index].ProjectKey = projectKey
			}
		}
	}
	for index, item := range items {
		if err := ctx.Err(); err != nil {
			now := time.Now().UTC()
			job.Status = "cancelled"
			job.Message = "sync cancelled before all source items were processed; the cursor was retained"
			if trelloSlice != nil {
				job.Status = "partial_failure"
				job.Message = "Trello worker stopped before the current page checkpoint; that page will be replayed safely."
			}
			job.CompletedAt = &now
			_, _ = s.repo.UpdateSyncJob(job)
			s.audit(sourceID, "source.sync_cancelled", job.Message)
			return nil, err
		}
		if trelloSlice != nil || trelloCommentRefresh != nil {
			unchanged, checkErr := s.trelloItemAlreadyProcessed(source, item)
			if checkErr != nil {
				recordFailure(item, "existing Trello item checkpoint lookup failed", checkErr)
				continue
			}
			if unchanged {
				continue
			}
		}
		if source.ConnectorKey == trelloConnectorKey && isTrelloCardStateItem(item.ItemType) {
			if trelloClosureLookupErr != nil {
				recordFailure(item, "existing Trello card lookup failed", trelloClosureLookupErr)
				continue
			}
			raw := trelloRawByExternalID[item.ExternalID]
			if raw == nil {
				continue
			}
			closedExtraction, errClose := s.reconcileTrelloCardState(ctx, source, item, raw)
			if errClose != nil {
				recordFailure(item, "card source state could not be applied", errClose)
				continue
			}
			updated++
			if closedExtraction != nil {
				extractions = append(extractions, *closedExtraction)
			}
			continue
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
		if errIndex := s.indexExtractionContext(ctx, extraction); errIndex != nil {
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
		if source.ConnectorKey == trelloConnectorKey {
			if _, errCheckpoint := s.repo.SaveRawItem(raw); errCheckpoint != nil {
				recordFailure(item, "processed source checkpoint could not be committed", errCheckpoint)
				continue
			}
		}
		extractions = append(extractions, *extraction)
		s.storeUsefulMemory(source, extraction)
	}

	commentArchiveFailed := false
	if failed == 0 && trelloCommentRefresh != nil {
		if archiveErr := s.archiveMissingTrelloWebhookComments(ctx, source, trelloCommentRefresh); archiveErr != nil {
			failed++
			commentArchiveFailed = true
			if len(itemErrors) < maxSyncErrorDetails {
				itemErrors = append(itemErrors, "deleted Trello comment history could not be archived; webhook retry required")
			}
		}
	}

	now := time.Now().UTC()
	if trelloSlice != nil {
		job.CursorAfter = source.Cursor
		if failed > 0 {
			job.ItemsFailed += failed
			job.Status = "partial_failure"
			job.Message = fmt.Sprintf("Trello page was not checkpointed because %d source item(s) failed; retry will replay the same page.", failed)
			job.CompletedAt = &now
			job, err = s.repo.UpdateSyncJob(job)
			if err != nil {
				return nil, fmt.Errorf("persist Trello page failure: %w", err)
			}
			return &SyncResult{Job: *job, Extractions: extractions, PursuitOutcomes: pursuitOutcomes, LifeGraphProjections: lifeGraphProjections, Message: job.Message, Errors: itemErrors, Warnings: append(warnings, trelloSyncCapabilityLimitations)}, nil
		}

		job.ItemsSeen += len(items)
		job.ItemsAdded += added
		job.ItemsUpdated += updated
		job.ItemsFailed = 0
		job.CursorAfter = source.Cursor
		trelloPageProgress(job, &trelloSlice.Next, trelloSlice.Complete)
		job.CompletedAt = nil
		if trelloSlice.Complete {
			source.LastSyncedAt = &now
			source.Cursor = trelloSlice.FinalCursor
			job.CursorAfter = source.Cursor
			job.Status = "completed"
			job.Message = "Trello sync completed after card inventory, actions, and durable processing were verified."
			job.CompletedAt = &now
		} else {
			job.Status = "running"
			job.Message = "Trello page committed; the same logical sync will resume from its saved card/action cursors."
		}
		repository, ok := s.repo.(trelloSyncStateRepository)
		if !ok {
			return nil, errors.New("durable Trello sync repository became unavailable before checkpoint commit")
		}
		_, job, _, err = repository.CommitTrelloSyncPage(source, job, &trelloSlice.Current, &trelloSlice.Next, trelloSlice.Page, trelloSlice.Receipts, trelloSlice.SeenCardExternalIDs, trelloSlice.VerifiedBoardCardExternalIDs, trelloSlice.Complete)
		if err != nil {
			return nil, fmt.Errorf("commit Trello page checkpoint: %w", err)
		}
		s.audit(sourceID, "source.trello_page_committed", fmt.Sprintf("phase=%s page=%d records=%d requests=%d bytes=%d", trelloSlice.Page.Phase, job.ProgressPages, trelloSlice.Page.RecordCount, trelloSlice.Page.RequestCount, trelloSlice.Page.ResponseBytes))
		return &SyncResult{Job: *job, Extractions: extractions, PursuitOutcomes: pursuitOutcomes, LifeGraphProjections: lifeGraphProjections, Message: job.Message, Errors: itemErrors, Warnings: append(warnings, trelloSyncCapabilityLimitations)}, nil
	}

	job.ItemsSeen = len(items)
	job.ItemsAdded = added
	job.ItemsUpdated = updated
	job.ItemsFailed = failed
	job.CursorAfter = source.Cursor
	if failed == 0 {
		if source.ConnectorKey == trelloConnectorKey && request.trelloWebhookReceiptID != uuid.Nil {
			// An event-only comment update is evidence, not a complete board scan.
			// Preserve the provider cursor and last full reconciliation timestamp.
			source.LastSyncedAt = originalLastSyncedAt
		} else {
			source.LastSyncedAt = &now
			source.Cursor = firstNonEmpty(adapterCursor, fmt.Sprintf("%s:%d", now.Format(time.RFC3339), len(items)))
		}
		job.Status = "completed"
		job.CursorAfter = source.Cursor
		job.Message = "sync completed with cached extraction and provenance links"
	} else if len(extractions) > 0 {
		job.Status = "partial_failure"
		if commentArchiveFailed {
			job.Message = "Trello comment refresh imported current evidence, but deletion archival failed; the source cursor was retained for webhook retry"
		} else {
			job.Message = fmt.Sprintf("sync partially completed; %d item(s) failed and the cursor was retained for retry", failed)
		}
	} else {
		job.Status = "failed"
		job.Message = fmt.Sprintf("sync failed; %d item(s) failed and the cursor was retained for retry", failed)
	}
	job.CompletedAt = &now
	if completionRepository, ok := s.repo.(sourceSyncCompletionRepository); ok {
		_, job, err = completionRepository.CompleteSourceSync(source, job)
		if err != nil {
			job.Status = "failed"
			job.CursorAfter = job.CursorBefore
			job.ItemsFailed++
			job.Message = "sync result could not be committed; the source cursor was retained"
			job.CompletedAt = &now
			if len(itemErrors) < maxSyncErrorDetails {
				itemErrors = append(itemErrors, "sync history or source cursor commit failed")
			}
			job, persistErr := s.repo.UpdateSyncJob(job)
			if persistErr != nil {
				return nil, fmt.Errorf("sync completion commit failed and failure history could not be persisted: %w", persistErr)
			}
			s.audit(sourceID, "source.sync_partial_failure", job.Message)
			return &SyncResult{Job: *job, Extractions: extractions, PursuitOutcomes: pursuitOutcomes, LifeGraphProjections: lifeGraphProjections, Message: job.Message, Errors: itemErrors, Warnings: warnings}, err
		}
	} else {
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
		job, err = s.repo.UpdateSyncJob(job)
		if err != nil {
			// Repositories without transactional completion support must at least
			// restore the prior cursor if recording the outcome fails.
			source.Cursor = job.CursorBefore
			source.LastSyncedAt = originalLastSyncedAt
			_, _ = s.repo.UpdateSource(source)
			return nil, fmt.Errorf("sync result could not be persisted: %w", err)
		}
	}
	if err == nil {
		action := "source.synced"
		if job.Status != "completed" {
			action = "source.sync_partial_failure"
		}
		s.audit(sourceID, action, job.Message)
	}
	if err == nil && job.Status == "completed" && trelloCommentRefresh != nil {
		s.audit(sourceID, "source.trello_comment_reconciled", fmt.Sprintf(
			"action=%s; action_id=%s; card_id=%s; active_comments=%d; read_only=true",
			trelloCommentRefresh.Receipt.ActionType, trelloCommentRefresh.Receipt.ActionID,
			trelloCommentRefresh.CardID, len(trelloCommentRefresh.ActiveActionIDs),
		))
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
	return s.RunDueScheduledSyncsContext(s.compatibilityContext(), now)
}

func (s *service) RunDueScheduledSyncsContext(ctx context.Context, now time.Time) (*ScheduledSyncRun, error) {
	if ctx == nil {
		return nil, errors.New("source scheduling requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sources, err := s.repo.FindSources(false)
	if err != nil {
		return nil, err
	}
	return s.runDueScheduledSyncs(ctx, now, sources)
}

// RunDueScheduledSyncsForOwner refreshes only sources explicitly owned by the
// authenticated user. Ownerless legacy sources remain readable for local
// compatibility but are never modified by an owner-scoped task request.
func (s *service) RunDueScheduledSyncsForOwner(now time.Time, ownerIdentity string) (*ScheduledSyncRun, error) {
	return s.RunDueScheduledSyncsForOwnerContext(s.compatibilityContext(), now, ownerIdentity)
}

func (s *service) RunDueScheduledSyncsForOwnerContext(ctx context.Context, now time.Time, ownerIdentity string) (*ScheduledSyncRun, error) {
	if ctx == nil {
		return nil, errors.New("source scheduling requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, ErrSourceOwnerRequired
	}
	sources, err := s.repo.FindSourcesVisibleToOwner(ownerIdentity, false)
	if err != nil {
		return nil, err
	}
	owned := make([]models.ConnectedSource, 0, len(sources))
	for _, item := range sources {
		if item.OwnerIdentity == ownerIdentity {
			owned = append(owned, item)
		}
	}
	return s.runDueScheduledSyncs(ctx, now, owned)
}

func (s *service) runDueScheduledSyncs(ctx context.Context, now time.Time, sources []models.ConnectedSource) (*ScheduledSyncRun, error) {
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
			if err := ctx.Err(); err != nil {
				return run, err
			}
			if errors.Is(errSync, context.Canceled) {
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
		if source.ConnectorKey == trelloConnectorKey && result.Job.Status == "running" {
			run.Skipped++
			run.Messages = append(run.Messages, fmt.Sprintf("%s saved Trello progress; the next scheduled worker slice will resume it", source.Name))
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
	if source.RevokedAt != nil || strings.EqualFold(strings.TrimSpace(source.Status), "revoked") {
		return nil, ErrSourceRevoked
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
	limit := request.Limit
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	visibleSourceIDs, err := s.visibleSourceIDsExcluding(request.OwnerIdentity, request.ExcludeConnectorKeys)
	if err != nil {
		return nil, err
	}
	if len(request.ExcludeConnectorKeys) == 0 && s.semanticService != nil && s.semanticService.Enabled() {
		matches, semanticErr := s.semanticService.Search(context.Background(), semantic.SearchRequest{
			OwnerIdentity: request.OwnerIdentity, Query: request.Query, ProjectKey: request.ProjectKey,
			Limit: limit, IncludeSensitive: request.IncludeSensitive,
		})
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
					Extraction:     match.Extraction,
					Score:          match.Similarity,
					Explanation:    "local pgvector cosine similarity; source ownership and sensitivity filters applied",
					RequiresReview: sourceExtractionRequiresReview(match.Extraction),
				})
			}
			if len(ranked) > 0 {
				return &SearchResult{Query: request.Query, ProjectKey: request.ProjectKey, UsedContext: ranked,
					Explanation: fmt.Sprintf("Retrieved %d source-backed records through local pgvector semantic retrieval; owner, source-revocation, project, archive, and sensitivity filters were enforced.", len(ranked))}, nil
			}
		}
		// A semantic failure falls back to the existing bounded keyword search.
		// It is deliberately not attached to an arbitrary source audit record.
	}
	extractions, err := s.repo.FindExtractionsForSources(sourceIDsFromSet(visibleSourceIDs), strings.TrimSpace(request.ProjectKey), false)
	if err != nil {
		return nil, err
	}
	ranked := []RankedExtraction{}
	for _, extraction := range extractions {
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
			Extraction:     extraction,
			Score:          math.Round(score*1000) / 1000,
			Explanation:    explanation,
			RequiresReview: sourceExtractionRequiresReview(extraction),
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

func sourceExtractionRequiresReview(extraction models.SourceExtraction) bool {
	return extraction.Uncertain || strings.EqualFold(strings.TrimSpace(extraction.ContentType), "trello_card")
}

func (s *service) visibleSourceIDs(ownerIdentity string) (map[uuid.UUID]bool, error) {
	return s.visibleSourceIDsExcluding(ownerIdentity, nil)
}

func (s *service) visibleSourceIDsExcluding(ownerIdentity string, excludedConnectorKeys []string) (map[uuid.UUID]bool, error) {
	sources, err := s.repo.FindSourcesVisibleToOwner(ownerIdentity, true)
	if err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
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
		if ownerIdentity == "" || source.OwnerIdentity == "" || source.OwnerIdentity == ownerIdentity {
			visible[source.ID] = true
		}
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

func (s *service) ExtractionPageForOwner(ownerIdentity, projectKey string, includeArchived bool, limit int) (*ExtractionPage, error) {
	visibleSourceIDs, err := s.visibleSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	items, totalCount, err := s.repo.FindExtractionPageForSources(sourceIDsFromSet(visibleSourceIDs), projectKey, includeArchived, limit)
	if err != nil {
		return nil, err
	}
	return &ExtractionPage{Items: items, TotalCount: totalCount, Limit: limit}, nil
}

func (s *service) UpdateExtraction(id uuid.UUID, request models.SourceExtraction) (*models.SourceExtraction, error) {
	extraction, err := s.repo.FindExtraction(id)
	if err != nil {
		return nil, err
	}
	if request.UpdatedAt.IsZero() || !request.UpdatedAt.Equal(extraction.UpdatedAt) {
		return nil, ErrExtractionPatchConflict
	}
	source, err := s.repo.FindSource(extraction.SourceID)
	if err != nil {
		return nil, err
	}
	release, err := s.acquireSourceExtractionLocks(context.Background(), source.ID, source.OwnerIdentity, extraction.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	current, err := s.repo.FindExtraction(id)
	if err != nil {
		return nil, err
	}
	if current.SourceID != extraction.SourceID || !current.UpdatedAt.Equal(extraction.UpdatedAt) || current.Archived {
		return nil, ErrExtractionPatchConflict
	}
	currentSource, err := s.repo.FindSource(current.SourceID)
	if err != nil {
		return nil, err
	}
	if currentSource.ID != source.ID || currentSource.OwnerIdentity != source.OwnerIdentity {
		return nil, ErrExtractionPatchConflict
	}
	source = currentSource
	extraction = current
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
		lessonRequest := extractionCorrectionMemoryRequest(source, &before, updated)
		s.audit(updated.SourceID, "extraction.corrected", memory.SourceExtractionCorrectionAuditMessage(updated.ID, updated.UpdatedAt, lessonRequest.Content))
		if errIndex := s.indexExtraction(updated); errIndex != nil {
			return updated, fmt.Errorf("extraction corrected but index update failed: %w", errIndex)
		}
		if errWorkflow := s.reconcileWorkflowFromExtraction(updated); errWorkflow != nil {
			return updated, fmt.Errorf("extraction corrected but workflow reconciliation failed: %w", errWorkflow)
		}
		if source != nil {
			if _, errProjection := s.projectExtractionToLifeGraph(context.Background(), source, updated); errProjection != nil {
				s.audit(updated.SourceID, "life_graph.projection_failed", compact(safety.RedactSecrets(errProjection.Error()), 300))
			} else if s.lifeOntologyProjector != nil && strings.TrimSpace(source.OwnerIdentity) != "" {
				s.audit(updated.SourceID, "life_graph.projected", "projected corrected extraction "+updated.ID.String()+" as a new immutable graph observation")
			}
		}
		if errMemory := s.rememberExtractionCorrection(&before, updated); errMemory != nil {
			return updated, fmt.Errorf("extraction corrected but correction lesson memory could not be confirmed: %w", errMemory)
		}
	}
	return updated, err
}

func (s *service) ArchiveExtraction(id uuid.UUID, archived bool) (*models.SourceExtraction, error) {
	extraction, err := s.repo.FindExtraction(id)
	if err != nil {
		return nil, err
	}
	source, err := s.repo.FindSource(extraction.SourceID)
	if err != nil {
		return nil, err
	}
	release, err := s.acquireSourceExtractionLocks(context.Background(), source.ID, source.OwnerIdentity, extraction.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	current, err := s.repo.FindExtraction(id)
	if err != nil {
		return nil, err
	}
	if current.SourceID != extraction.SourceID || !current.UpdatedAt.Equal(extraction.UpdatedAt) {
		return nil, ErrExtractionPatchConflict
	}
	currentSource, err := s.repo.FindSource(current.SourceID)
	if err != nil {
		return nil, err
	}
	if currentSource.ID != source.ID || currentSource.OwnerIdentity != source.OwnerIdentity {
		return nil, ErrExtractionPatchConflict
	}
	extraction = current
	source = currentSource
	if archived {
		if err := s.retractWorkflowForExtraction(extraction, "source extraction was archived by the operator"); err != nil {
			return nil, err
		}
	}
	if archived && isTrelloCardStateItem(extraction.ContentType) {
		// Preserve the source state when an operator explicitly archives its evidence.
		if extraction.ContentType == trelloUnavailableCardItemType {
			extraction.ContentType = trelloUnavailableOwnerArchivedItemType
		} else {
			extraction.ContentType = trelloClosedCardOwnerArchivedItemType
		}
	} else if !archived {
		switch extraction.ContentType {
		case trelloClosedCardOwnerArchivedItemType:
			extraction.ContentType = trelloClosedCardItemType
		case trelloUnavailableOwnerArchivedItemType:
			extraction.ContentType = trelloUnavailableCardItemType
		}
	}
	extraction.Archived = archived
	updated, err := s.repo.SaveExtraction(extraction)
	if err == nil {
		s.audit(extraction.SourceID, "extraction.archived", fmt.Sprintf("extraction_id=%s archived=%t", extraction.ID, archived))
		if source != nil {
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
	guardedRepository, ok := s.repo.(transactionalGuardedExtractionDeleteRepository)
	if !ok {
		return ErrGuardedExtractionDeleteUnavailable
	}
	if err := guardedRepository.DeleteExtractionForOwnerGuardedInTransaction(
		extraction,
		source,
		auth.OwnerIdentity,
		func(tx *gorm.DB) (func(bool), error) {
			releaseCommitFence := safety.AcquireExecutionCommitFence()
			retainCommitFence := false
			defer func() {
				if !retainCommitFence {
					releaseCommitFence()
				}
			}()

			// Check before consuming a one-shot approval or producing an external
			// workflow effect. The commit fence keeps a persisted stop mutation
			// ordered either before this check or after the transaction commits.
			if decision := s.evaluateSourceEmergencyStop(); decision.Active {
				return nil, fmt.Errorf(
					"%w: %s",
					ErrSourceEmergencyStopActive,
					safety.RedactSecrets(strings.TrimSpace(decision.Reason)),
				)
			}
			authorizationProjection, err := s.authorizeDestructiveEffectInTransaction(ctx, tx, auth, *source, extraction)
			if err != nil {
				return nil, err
			}

			var workflowProjection workflow.SourceRetractionPostCommitProjection
			if s.workflowService != nil {
				retractor, ok := s.workflowService.(workflow.TransactionalSourceRetraction)
				if !ok {
					return nil, ErrDestructiveTransactionUnavailable
				}
				sourceType := workflowSourceType(source)
				workflowProjection, err = retractor.RetractSourceInTransaction(
					tx,
					auth.OwnerIdentity,
					auth.ActorIdentity,
					sourceType,
					extraction.ID.String(),
					"source extraction was deleted by the operator",
				)
				if err != nil {
					return nil, err
				}
			}
			if decision := s.evaluateSourceEmergencyStop(); decision.Active {
				return nil, fmt.Errorf(
					"%w: %s",
					ErrSourceEmergencyStopActive,
					safety.RedactSecrets(strings.TrimSpace(decision.Reason)),
				)
			}
			if err := guardedRepository.SaveAuditLogInTransaction(tx, &models.SourceAuditLog{
				ID: uuid.New(), SourceID: extraction.SourceID, Action: "extraction.deleted",
				Message: "authenticated operator " + strings.TrimSpace(auth.ActorIdentity) + " deleted extraction " + extraction.ID.String(),
			}); err != nil {
				return nil, fmt.Errorf("record extraction deletion audit: %w", err)
			}
			retainCommitFence = true
			return func(committed bool) {
				if !committed {
					releaseCommitFence()
					return
				}
				releaseCommitFence()
				if authorizationProjection != nil {
					projected := authorizationProjection(ctx)
					if projected.LifeGraphProjectionWarning != "" {
						s.audit(extraction.SourceID, "authorization.life_graph_projection_failed", compact(projected.LifeGraphProjectionWarning, 300))
					}
				}
				if workflowProjection != nil {
					if err := workflowProjection(ctx); err != nil {
						s.audit(extraction.SourceID, "workflow.life_graph_projection_failed", compact(safety.RedactSecrets(err.Error()), 300))
					}
				}
			}, nil
		},
	); err != nil {
		return err
	}
	return nil
}

func (s *service) AuditLogs(sourceID *uuid.UUID) ([]models.SourceAuditLog, error) {
	return s.repo.FindAuditLogs(sourceID)
}

func (s *service) AuditLogsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceAuditLog, error) {
	return s.repo.FindAuditLogsForSources(sourceIDs, limit)
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

func isTrelloCardStateItem(itemType string) bool {
	return itemType == trelloClosedCardItemType || itemType == trelloUnavailableCardItemType
}

func hasTrelloStateItems(items []ImportItem) bool {
	for _, item := range items {
		if isTrelloCardStateItem(item.ItemType) {
			return true
		}
	}
	return false
}

func (s *service) reconcileTrelloCardState(ctx context.Context, source *models.ConnectedSource, item ImportItem, raw *models.SourceRawItem) (*models.SourceExtraction, error) {
	if source == nil || raw == nil || raw.ID == uuid.Nil {
		return nil, fmt.Errorf("an existing Trello source record is required to reconcile state")
	}
	if !isTrelloCardStateItem(item.ItemType) {
		return nil, fmt.Errorf("unsupported Trello source state %q", item.ItemType)
	}
	stateDescription := "no longer present on the configured board"
	auditAction := "source.trello_card_unavailable"
	if item.ItemType == trelloClosedCardItemType {
		stateDescription = "closed or archived at the source"
		auditAction = "source.trello_card_closed"
	}

	extraction, err := s.repo.FindExtractionByRawItem(raw.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		extraction = nil
	}

	if extraction != nil && !extraction.Archived {
		if err := s.retractWorkflowForExtraction(extraction, "Trello card is "+stateDescription+"; retain evidence and stop source-derived work"); err != nil {
			return nil, err
		}
		extraction.ContentType = item.ItemType
		extraction.Archived = true
		extraction, err = s.repo.SaveExtraction(extraction)
		if err != nil {
			return nil, err
		}
	}

	raw.ItemType = item.ItemType
	if _, err := s.repo.SaveRawItem(raw); err != nil {
		return nil, err
	}

	extractionID := "none"
	if extraction != nil {
		extractionID = extraction.ID.String()
	}
	s.audit(source.ID, auditAction, compact(fmt.Sprintf(
		"retained Trello card state=%s external_id=%s raw_item_id=%s extraction_id=%s source_uri=%s event=%s",
		item.ItemType, raw.ExternalID, raw.ID, extractionID, raw.SourceURI, item.Metadata,
	), 700))
	if extraction != nil {
		if _, errProjection := s.projectExtractionToLifeGraph(ctx, source, extraction); errProjection != nil {
			s.audit(source.ID, "life_graph.projection_failed", compact(safety.RedactSecrets(errProjection.Error()), 300))
		} else if s.lifeOntologyProjector != nil && strings.TrimSpace(source.OwnerIdentity) != "" {
			s.audit(source.ID, "life_graph.projected", "projected Trello card source state "+extraction.ID.String()+" as an immutable graph observation")
		}
	}
	return extraction, nil
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
		}
		added = true
	}
	priorContentHash := existing.ContentHash
	contentHash := hashText(item.Title + "|" + item.Content)
	existing.ProjectKey = item.ProjectKey
	existing.ItemType = firstNonEmpty(item.ItemType, source.Category)
	existing.Title = item.Title
	existing.SourceURI = item.SourceURI
	existing.Content = item.Content
	existing.Metadata = item.Metadata
	existing.ContentHash = contentHash
	if source.ConnectorKey == trelloConnectorKey {
		// For Trello the hash is the completed-processing checkpoint. Keep its
		// prior value in storage until extraction, index, and workflow intake pass.
		existing.ContentHash = priorContentHash
	}
	raw, err := s.repo.SaveRawItem(existing)
	if err != nil || raw == nil || source.ConnectorKey != trelloConnectorKey {
		return raw, added, err
	}
	processing := *raw
	processing.ContentHash = contentHash
	return &processing, added, nil
}

func (s *service) trelloItemAlreadyProcessed(source *models.ConnectedSource, item ImportItem) (bool, error) {
	raw, err := s.repo.FindRawItem(source.ID, item.ExternalID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if raw == nil {
		return false, errors.New("Trello raw-item repository returned no record")
	}
	if extraction, lookupErr := s.repo.FindExtractionByRawItem(raw.ID); lookupErr == nil {
		if extraction != nil && extraction.Archived && extraction.ContentType == trelloCommentDeletedItemType {
			return false, nil
		}
	} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return false, lookupErr
	}
	unchanged := raw.ItemType == item.ItemType && raw.ContentHash == hashText(item.Title+"|"+item.Content) &&
		raw.Title == item.Title && raw.SourceURI == item.SourceURI && raw.ProjectKey == item.ProjectKey
	if raw.Metadata != item.Metadata {
		raw.Metadata = item.Metadata
		saved, saveErr := s.repo.SaveRawItem(raw)
		if saveErr != nil {
			return false, saveErr
		}
		if saved == nil {
			return false, errors.New("Trello raw-item repository returned no saved record")
		}
	}
	return unchanged, nil
}

func (s *service) extractAndStore(source *models.ConnectedSource, raw *models.SourceRawItem, text string) (*models.SourceExtraction, error) {
	existing, err := s.repo.FindExtractionByRawItem(raw.ID)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if err == gorm.ErrRecordNotFound {
		existing = &models.SourceExtraction{SourceID: source.ID, RawItemID: raw.ID}
	}
	restoreProviderDeletedComment := source.ConnectorKey == trelloConnectorKey && existing.Archived &&
		existing.ContentType == trelloCommentDeletedItemType && raw.ItemType == "trello_comment"
	if restoreProviderDeletedComment {
		existing.Archived = false
	}
	if source.ConnectorKey == trelloConnectorKey && existing.Archived && isTrelloCardStateItem(existing.ContentType) {
		operatorArchived, errArchive := s.trelloExtractionWasArchivedByOperator(existing)
		if errArchive != nil {
			return nil, fmt.Errorf("could not verify Trello extraction archive provenance: %w", errArchive)
		}
		if !operatorArchived {
			existing.Archived = false
		}
	}
	now := time.Now().UTC()
	clean := normalizeSpaces(text)
	existing.ProjectKey = raw.ProjectKey
	existing.ContentType = raw.ItemType
	existing.Text = clean
	existing.Summary = compact(clean, 420)
	existing.Entities = joinValues(extractEntities(clean))
	existing.Dates = joinValues(extractDates(clean))
	tasks := extractTasks(clean)
	if source.ConnectorKey == trelloConnectorKey {
		tasks = extractTrelloTasks(text)
	}
	existing.Tasks = joinValues(limitValues(tasks, 12))
	existing.Decisions = joinValues(extractDecisions(clean))
	existing.FollowUps = joinValues(extractFollowUps(clean))
	existing.SourceURI = raw.SourceURI
	existing.SourceLabel = raw.Title
	existing.ContentHash = raw.ContentHash
	existing.Sensitive = source.ConnectorKey == "whatsapp-export" || source.ConnectorKey == laroConnectorKey || source.ConnectorKey == workerControlConnectorKey || containsAny(strings.ToLower(clean), "password", "secret", "token", "bank", "invoice", "contract", "legal", "medical", "juridisch", "medisch", "rekening", "factuur")
	existing.Uncertain = source.ConnectorKey == "github" || source.ConnectorKey == trelloConnectorKey || isManualPlanningContextOnlyConnector(source.ConnectorKey) || sourceRawItemRequiresReview(raw) || sourceContentRequiresReview(clean) || len(clean) < 40 || containsAny(strings.ToLower(clean), "maybe", "unclear", "unknown")
	existing.LastIndexedAt = &now
	updated, saveErr := s.repo.SaveExtraction(existing)
	if saveErr == nil && updated != nil && restoreProviderDeletedComment {
		s.audit(source.ID, "source.trello_comment_restored", "authoritative Trello card refresh confirmed a previously provider-deleted comment action is active again")
	}
	return updated, saveErr
}

func (s *service) trelloExtractionWasArchivedByOperator(extraction *models.SourceExtraction) (bool, error) {
	logs, err := s.repo.FindAuditLogs(&extraction.SourceID)
	if err != nil {
		return true, err
	}

	var latestState bool
	var latestAt time.Time
	var hasSpecificState bool
	var conflictingTimestamp bool
	legacyArchiveIsAmbiguous := false
	for _, log := range logs {
		if log.Action != "extraction.archived" {
			continue
		}
		var extractionID, archiveState string
		for _, field := range strings.Fields(log.Message) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "extraction_id":
				extractionID = value
			case "archived":
				archiveState = value
			}
		}
		if archiveState != "true" && archiveState != "false" {
			continue
		}
		if extractionID == "" {
			if archiveState == "true" {
				// Legacy audit rows do not identify which extraction was archived.
				legacyArchiveIsAmbiguous = true
			}
			continue
		}
		if extractionID != extraction.ID.String() {
			continue
		}
		state := archiveState == "true"
		if !hasSpecificState || log.CreatedAt.After(latestAt) {
			latestState = state
			latestAt = log.CreatedAt
			hasSpecificState = true
			conflictingTimestamp = false
		} else if log.CreatedAt.Equal(latestAt) && state != latestState {
			conflictingTimestamp = true
		}
	}

	return legacyArchiveIsAmbiguous || (hasSpecificState && (latestState || conflictingTimestamp)), nil
}

func (s *service) storeUsefulMemory(source *models.ConnectedSource, extraction *models.SourceExtraction) {
	if s.memoryService == nil || source == nil || extraction == nil {
		return
	}
	if isManualPlanningContextOnlyConnector(source.ConnectorKey) || source.ConnectorKey == trelloConnectorKey || source.ConnectorKey == "github" {
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

func (s *service) rememberExtractionCorrection(before, after *models.SourceExtraction) error {
	return s.rememberExtractionCorrectionForOwner(before, after, "")
}

func (s *service) rememberExtractionCorrectionForOwner(before, after *models.SourceExtraction, expectedOwner string) error {
	if before == nil || after == nil || !extractionCorrectionUseful(before, after) {
		return nil
	}
	source, err := s.repo.FindSource(after.SourceID)
	if err != nil {
		s.audit(after.SourceID, "extraction.correction_memory_failed", "source owner could not be verified")
		return fmt.Errorf("source owner could not be verified: %w", err)
	}
	if source == nil || strings.TrimSpace(source.OwnerIdentity) == "" {
		s.audit(after.SourceID, "extraction.correction_memory_failed", "source owner could not be verified")
		return errors.New("source owner could not be verified")
	}
	if strings.TrimSpace(expectedOwner) != "" && strings.TrimSpace(source.OwnerIdentity) != strings.TrimSpace(expectedOwner) {
		s.audit(after.SourceID, "extraction.correction_memory_skipped", "source owner changed before the correction lesson was stored")
		return errors.New("source owner changed before the correction lesson was stored")
	}
	if isManualPlanningContextOnlyConnector(source.ConnectorKey) || source.ConnectorKey == trelloConnectorKey {
		return nil
	}
	if source.ConnectorKey == "github" && (after.Uncertain || after.Sensitive) {
		return nil
	}
	if s.memoryService == nil {
		err := errors.New("memory service is unavailable")
		s.audit(after.SourceID, "extraction.correction_memory_failed", err.Error())
		return err
	}
	request := extractionCorrectionMemoryRequest(source, before, after)
	internalExtractionIdentity := "source-extraction://" + after.ID.String()
	created, err := memory.PersistSourceExtractionLessonForOwner(
		s.memoryService,
		source.OwnerIdentity,
		internalExtractionIdentity,
		request,
	)
	if err != nil {
		s.audit(after.SourceID, "extraction.correction_memory_failed", compact(safety.RedactSecrets(err.Error()), 240))
		return err
	}
	if created == nil {
		err := errors.New("memory store did not confirm the correction lesson")
		s.audit(after.SourceID, "extraction.correction_memory_failed", err.Error())
		return err
	}
	s.autoLinkPursuitMemory(source, after, created)
	s.audit(after.SourceID, "extraction.correction_memory_created", "stored reviewable source correction lesson")
	return nil
}

func extractionCorrectionUseful(before, after *models.SourceExtraction) bool {
	if before.ID == uuid.Nil || after.ID == uuid.Nil || before.ID != after.ID {
		return false
	}
	fields := []struct {
		left  string
		right string
	}{
		{before.Text, after.Text},
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

	if before.Sensitive || after.Sensitive {
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
		extractionCorrectionValue("Previous text", before.Text),
		extractionCorrectionValue("Revised text", after.Text),
		extractionCorrectionValue("Previous summary", before.Summary),
		extractionCorrectionValue("Revised summary", after.Summary),
		extractionCorrectionValue("Previous tasks", before.Tasks),
		extractionCorrectionValue("Revised tasks", after.Tasks),
		extractionCorrectionValue("Previous decisions", before.Decisions),
		extractionCorrectionValue("Revised decisions", after.Decisions),
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
		{"text", before.Text, after.Text},
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
	if source == nil || extraction == nil || extraction.Archived || isManualPlanningContextOnlyConnector(source.ConnectorKey) {
		return nil, nil
	}
	if source.ConnectorKey == "github" && (extraction.Uncertain || extraction.Sensitive) {
		s.audit(source.ID, "github.extraction_review_required", "imported GitHub prose was retained for owner review; workflow intake is deferred")
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
	requiresReview := extraction.Uncertain || extraction.Sensitive || source.ConnectorKey == trelloConnectorKey
	reviewReason := extractionReviewReason(extraction)
	if source.ConnectorKey == trelloConnectorKey {
		if reviewReason != "" {
			reviewReason += "; "
		}
		reviewReason += "Trello source evidence requires owner review"
	}
	sourceType := source.Category
	projectKey := extraction.ProjectKey
	projectKeyHint := ""
	if source.ConnectorKey == trelloConnectorKey {
		// Keep the connector identity and route its configured default separately
		// so it remains visible without becoming a confirmed project match.
		sourceType = trelloConnectorKey
		projectKeyHint = projectKey
		projectKey = ""
	}
	record, err := s.intakeWorkflow(workflow.IntakeRequest{
		OwnerIdentity:  source.OwnerIdentity,
		Input:          input,
		ProjectKey:     projectKey,
		ProjectKeyHint: projectKeyHint,
		SourceType:     sourceType,
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
	if sourceContentRequiresReview(extraction.Text) {
		reasons = append(reasons, "source content contains instruction-like or policy-bypass language")
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
	if extraction == nil || extraction.Archived || firstNonEmpty(extraction.Tasks, extraction.FollowUps) == "" {
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
	return s.workflowService.RetractSource(workflowSourceType(source), extraction.ID.String(), reason)
}

func workflowSourceType(source *models.ConnectedSource) string {
	if source == nil {
		return ""
	}
	if source.ConnectorKey == trelloConnectorKey {
		// Trello workflow intake uses the connector identity, not its broader
		// project_board category. Retraction must use the same source identity.
		return trelloConnectorKey
	}
	return source.Category
}

func (s *service) indexExtraction(extraction *models.SourceExtraction) error {
	return s.indexExtractionContext(context.Background(), extraction)
}

// indexExtractionContext keeps optional semantic enrichment inside the source
// sync deadline. A cancelled durable job must release embedding and database
// work instead of continuing after the source-sync worker has been reclaimed.
// Non-sync correction flows retain the background wrapper above because they
// have no request-scoped cancellation contract.
func (s *service) indexExtractionContext(ctx context.Context, extraction *models.SourceExtraction) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
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
	return compact(safety.RedactSecrets(fmt.Sprintf("%s: %s: %v", label, stage, err)), 320)
}

func (s *service) audit(sourceID uuid.UUID, action, message string) {
	_, _ = s.repo.SaveAuditLog(&models.SourceAuditLog{
		SourceID: sourceID,
		Action:   action,
		Message:  safety.RedactSecrets(message),
	})
}

// sourceOperationalFailureMessage is used for durable job and audit state.
// Preserve explicit policy guidance such as allowlist or consent failures, but
// remove secrets and Windows filesystem locations from connector diagnostics.
func sourceOperationalFailureMessage(err error) string {
	if err == nil {
		return "source synchronization failed"
	}
	message := compact(safety.RedactSecrets(err.Error()), 320)
	message = sourceFailureWindowsPath.ReplaceAllString(message, "${1}[LOCAL_PATH]")
	if strings.TrimSpace(message) == "" {
		return "source synchronization failed; review the source connection and configuration"
	}
	return message
}

type redactedSourceError struct {
	message string
	cause   error
}

func (e redactedSourceError) Error() string { return e.message }

func (e redactedSourceError) Unwrap() error { return e.cause }

func redactSourceError(err error) error {
	if err == nil {
		return nil
	}
	return redactedSourceError{
		message: sourceOperationalFailureMessage(err),
		cause:   err,
	}
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
		{ConnectorKey: trelloConnectorKey, Name: "Trello board (read-only API)", Category: "project_board", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeWebhookSync, ModeIncrementalSync}), RequiredScopes: "read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Trello REST polling and reconciliation are implemented. Signed, idempotent webhook intake is a separate capability and remains unconfigured or unverified until callback signing configuration plus verified delivery/registration evidence are available; webhook registration is operator-managed. Configure TRELLO_ACCOUNT_OWNER_IDENTITY and matching TRELLO_ACCOUNT_MEMBER_ID with a least-privilege TRELLO_API_KEY/TRELLO_READ_TOKEN. Attachment bodies are not downloaded and HAI never writes to Trello."},
		{ConnectorKey: "github", Name: "GitHub repositories and work", Category: "github", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "metadata,read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only GitHub REST polling sync of repositories, issues, pull requests, commits, and workflow runs; public repositories can sync without a token. Webhook sync and a separate historical-backfill mode are not supported. A private-repository GITHUB_SOURCE_TOKEN is sent only to its configured GITHUB_SOURCE_TOKEN_OWNER_IDENTITY."},
		{ConnectorKey: gmailConnectorKey, Name: "Gmail (Google OAuth)", Category: "email", SupportedModes: joinValues([]string{ModeHistoricalBackfill, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "gmail.readonly", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Gmail sync over Google OAuth with provider-native history cursors, bounded message text, attachment metadata, and source provenance"},
		{ConnectorKey: driveConnectorKey, Name: "Google Drive (Google OAuth)", Category: "cloud_document", SupportedModes: joinValues([]string{ModeHistoricalBackfill, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "drive.readonly", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Drive inventory and changes sync over Google OAuth; text-native files are extracted within limits and binary documents remain provenance-linked for governed extraction"},
		{ConnectorKey: contactsConnectorKey, Name: "Google Contacts (Google OAuth)", Category: "contact", SupportedModes: joinValues([]string{ModeHistoricalBackfill, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "contacts.readonly", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Google Contacts sync with provider-native sync tokens; imported people remain review candidates and HAI never writes back to the address book"},
		{ConnectorKey: calendarConnectorKey, Name: "Google Calendar (Google OAuth)", Category: "calendar", SupportedModes: joinValues([]string{ModeHistoricalBackfill, ModeScheduledSync, ModeIncrementalSync}), RequiredScopes: "calendar.readonly", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only primary Google Calendar sync with a bounded initial backfill and provider-native sync tokens; cancellations remain reviewable tombstones and HAI never writes back"},
		{ConnectorKey: "local-folder", Name: "Selected local folders", Category: "local_folder", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeFolderWatcher, ModeIncrementalSync}), RequiredScopes: "selected-folder-read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "manual and scheduled ingestion of an allowlisted local folder"},
		{ConnectorKey: "json-feed", Name: "Allowlisted JSON feed", Category: "generic_feed", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "metadata,read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live scheduled and incremental fetch of a normalized JSON feed over HTTP, with host allowlisting and bounded responses"},
		{ConnectorKey: "whatsapp-export", Name: "WhatsApp exported chats", Category: "chat", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "selected-chat-export-read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "parses local WhatsApp .txt export files into bounded, sensitive, review-gated records; does not connect to WhatsApp"},
		{ConnectorKey: "whisper-audio", Name: "Selected audio folders (whisper.cpp)", Category: "audio", SupportedModes: joinValues([]string{ModeManualImport}), RequiredScopes: "selected-audio-folder-read,explicit-consent", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "operator-triggered local transcription from an explicit selected folder through whisper.cpp; no microphone capture, cloud upload, scheduled scan, or raw-audio retention"},
		{ConnectorKey: doclingDocumentsConnectorKey, Name: "Selected document folders (Docling)", Category: "document", SupportedModes: joinValues([]string{ModeManualImport}), RequiredScopes: "selected-folder-read,explicit-consent", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "operator-triggered local extraction from an explicit selected folder through Docling; no browser uploads, scheduled scan, model download, cloud parser, or source-file retention"},
		{ConnectorKey: "odoo-herp", Name: "Odoo / HERP operations", Category: "herp", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "metadata,read,herp:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterModeled, StatusReason: "generates built-in Odoo app-domain models from manual selection; no live Odoo connection; write-back disabled by default"},
		{ConnectorKey: odooJSON2ConnectorKey, Name: "Odoo JSON-2 (read only)", Category: "herp", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "odoo-api-key,read-only-model-access", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only Odoo JSON-2 sync for a fixed model and field allowlist; requires HAI_ODOO_* configuration and never writes back"},
		{ConnectorKey: shareTConnectorKey, Name: "ShareT links (read only)", Category: "project_board", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "connector:read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "live read-only ShareT link inventory through a scoped connector token; fetches every page within an explicit completeness limit, excludes participant email addresses, and never creates, changes, comments on, or revokes links"},
		{ConnectorKey: cloudQuerySummaryConnectorKey, Name: "CloudQuery sync summaries (local read only)", Category: "cloud_inventory", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "selected-folder-read,cloud_inventory:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads only a fixed, operator-produced local CloudQuery JSONL sync summary; never starts CloudQuery, reads its configuration or credentials, or accesses source/destination data"},
		{ConnectorKey: airbyteInventoryConnectorKey, Name: "Airbyte source and connection inventory (local read only)", Category: "connector_inventory", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "airbyte-api-key,approved-workspace:read", LocalOnlyCapable: true, Enabled: true, AdapterStatus: AdapterLocalOnly, StatusReason: "reads only bounded source and connection metadata from a configured local Airbyte API and fixed workspace allowlist; never reads credentials/configuration/records or creates, changes, starts, stops, or deletes a sync"},
		{ConnectorKey: laroConnectorKey, Name: "LARO legal case intelligence (read only)", Category: "legal_case", SupportedModes: joinValues([]string{ModeManualImport, ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "laro:hai:read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "reads bounded, owner-scoped case summaries and source-linked legal analyses from LARO with a revocable connector credential; source bytes and client contact details are excluded and HAI never writes back"},
		{ConnectorKey: workerControlConnectorKey, Name: "Worker Control commitments (read only)", Category: "operations", SupportedModes: joinValues([]string{ModeScheduledSync, ModeHistoricalBackfill, ModeIncrementalSync}), RequiredScopes: "worker_control:read", LocalOnlyCapable: false, Enabled: true, AdapterStatus: AdapterOperational, StatusReason: "reads bounded owner-scoped commitment and reminder events through a revocable dashboard key; all records remain sensitive and review-gated, and HAI has no write path"},
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

func connectorSupportsMode(connectorKey, mode string) bool {
	for _, connector := range defaultConnectors() {
		if connector.ConnectorKey != strings.TrimSpace(connectorKey) {
			continue
		}
		for _, supported := range strings.Split(connector.SupportedModes, ",") {
			if strings.TrimSpace(supported) == mode {
				return true
			}
		}
		return false
	}
	return false
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
	return sourceUsesLocalFolder(connectorKey) || connectorKey == "json-feed" || connectorKey == "github" || isGoogleOAuthConnector(connectorKey) || connectorKey == trelloConnectorKey || connectorKey == "whatsapp-export" || connectorKey == "whisper-audio" || connectorKey == "odoo-herp" || connectorKey == odooJSON2ConnectorKey || connectorKey == shareTConnectorKey || connectorKey == cloudQuerySummaryConnectorKey || connectorKey == airbyteInventoryConnectorKey || connectorKey == laroConnectorKey || connectorKey == workerControlConnectorKey || connectorKey == openSpecArtifactConnectorKey || isManualPlanningContextOnlyConnector(connectorKey)
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
	// Scheduling must honour the source lifecycle before considering an
	// adapter or frequency. A paused, revoked, or disabled source is retained
	// for audit/history, but must never create recurring failed sync jobs.
	if !source.Enabled {
		return false, "source is disabled"
	}
	switch strings.ToLower(strings.TrimSpace(source.Status)) {
	case "paused":
		return false, "source is paused"
	case "revoked":
		return false, "source access was revoked"
	}
	if source.RevokedAt != nil {
		return false, "source access was revoked"
	}
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
	if sourceHTTPAddressBlocked(target.Hostname()) && !sourceHTTPLoopbackAddressAllowed(sourceHTTPURLAddress(target)) {
		return nil, "", fmt.Errorf("json-feed target uses non-public or reserved address space")
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

const githubSourcePageSize = 100

const githubSourceTokenOwnerIdentityEnv = "GITHUB_SOURCE_TOKEN_OWNER_IDENTITY"

const githubSourceCursorPrefix = "hai-github-cursor-v1:"

const (
	githubSourceWindowOverlap        = time.Second
	githubSourceCursorLookback       = 2 * time.Second
	githubSourceMinSplitWindow       = 4 * time.Second
	githubSourceMaxAdaptiveWindows   = 64
	githubSourceMaxAdaptivePageCalls = 256
	githubSourceMaxPendingRuns       = 16
)

type githubSourceCursor struct {
	Version     int      `json:"v"`
	Updated     string   `json:"u,omitempty"`
	Commits     string   `json:"c,omitempty"`
	Runs        string   `json:"r,omitempty"`
	PendingRuns []string `json:"p,omitempty"`
}

type githubSourceScanBudget struct {
	pages   int
	windows int
}

func fetchGitHubSource(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	return fetchGitHubSourceAt(ctx, source, time.Now().UTC())
}

func fetchGitHubSourceAt(ctx context.Context, source *models.ConnectedSource, scanTime time.Time) ([]ImportItem, string, error) {
	if source == nil {
		return nil, "", fmt.Errorf("source is required")
	}
	cursor, err := parseGitHubSourceCursor(strings.TrimSpace(source.Cursor))
	if err != nil {
		return nil, "", err
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
	if !sourceHTTPHostAllowed(parsedBase.Hostname()) || (sourceHTTPAddressBlocked(parsedBase.Hostname()) && !sourceHTTPLoopbackAddressAllowed(sourceHTTPURLAddress(parsedBase))) {
		return nil, "", fmt.Errorf("github API host %s is not allowlisted", parsedBase.Hostname())
	}
	token := githubSourceTokenForOwner(source.OwnerIdentity)
	if parsedBase.Scheme != "https" && token != "" {
		return nil, "", fmt.Errorf("GitHub deployment credentials require an HTTPS API endpoint")
	}
	scanUpper := scanTime.UTC().Truncate(time.Second)
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
	latest := cursor.Updated
	maxPages := githubSourceMaxPages()
	for _, endpoint := range endpoints {
		if endpoint.kind == "commit" {
			records, err := fetchGitHubCommits(ctx, parsedBase, endpoint.path, cursor.Commits, scanUpper, token, maxPages)
			if err != nil {
				return nil, "", fmt.Errorf("fetch github %s: %w", endpoint.kind, err)
			}
			for _, record := range records {
				item, updated := githubImportItem(record, endpoint.kind, source.DefaultProjectKey, repository)
				if updated == "" {
					updated = githubRecordTimestamp(record, endpoint.kind)
				}
				if item.ExternalID == "" || item.Content == "" {
					continue
				}
				items = append(items, item)
				latest = laterGitHubCursor(latest, updated)
			}
			continue
		}
		if endpoint.kind == "workflow_run" {
			records, err := fetchGitHubWorkflowRuns(ctx, parsedBase, endpoint.path, cursor.Runs, scanUpper, token, maxPages)
			if err != nil {
				return nil, "", fmt.Errorf("fetch github %s: %w", endpoint.kind, err)
			}
			refreshed, err := fetchGitHubPendingWorkflowRuns(ctx, parsedBase, endpoint.path, cursor.PendingRuns, token)
			if err != nil {
				return nil, "", fmt.Errorf("refresh pending github workflow runs: %w", err)
			}
			records, err = mergeGitHubWorkflowRunRecords(records, refreshed)
			if err != nil {
				return nil, "", err
			}
			cursor.PendingRuns, err = pendingGitHubWorkflowRunIDs(records)
			if err != nil {
				return nil, "", err
			}
			for _, record := range records {
				item, updated := githubImportItem(record, endpoint.kind, source.DefaultProjectKey, repository)
				if updated == "" {
					updated = githubRecordTimestamp(record, endpoint.kind)
				}
				if item.ExternalID == "" || item.Content == "" {
					continue
				}
				items = append(items, item)
				latest = laterGitHubCursor(latest, updated)
			}
			continue
		}
		for page := 1; page <= maxPages; page++ {
			if err := ctx.Err(); err != nil {
				return nil, "", newProviderSyncError("GitHub", 0, err)
			}
			value, err := fetchGitHubJSON(ctx, parsedBase, endpoint.path, cursor.Updated, page, token)
			if err != nil {
				return nil, "", fmt.Errorf("fetch github %s page %d: %w", endpoint.kind, page, err)
			}
			pageRecordCount := githubRecordCount(value, endpoint.kind)
			records := githubRecords(value, endpoint.kind)
			for _, record := range records {
				item, updated := githubImportItem(record, endpoint.kind, source.DefaultProjectKey, repository)
				if item.ExternalID == "" || item.Content == "" {
					continue
				}
				items = append(items, item)
				latest = laterGitHubCursor(latest, updated)
			}
			if pageRecordCount < githubSourcePageSize {
				break
			}
			if page == maxPages {
				return nil, "", fmt.Errorf("github %s sync reached the %d-page safety limit; raise GITHUB_SOURCE_MAX_PAGES (maximum 20) and retry to avoid an incomplete import", endpoint.kind, maxPages)
			}
		}
	}

	// Per-stream watermarks advance only after every endpoint and every bounded
	// page/window completed. One raw legacy timestamp remains readable; once an
	// incremental stream has been observed, keep independent commit/run bounds.
	if strings.TrimSpace(source.Cursor) != "" || hasGitHubStreamItem(items) {
		cursor.Version = 1
		cursor.Updated = latest
		cursor.Commits = laterGitHubCursor(cursor.Commits, scanUpper.Format(time.RFC3339))
		cursor.Runs = laterGitHubCursor(cursor.Runs, scanUpper.Format(time.RFC3339))
		encoded, err := encodeGitHubSourceCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		return items, encoded, nil
	}
	return items, latest, nil
}

func parseGitHubSourceCursor(value string) (githubSourceCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return githubSourceCursor{Version: 1}, nil
	}
	if strings.HasPrefix(value, githubSourceCursorPrefix) {
		var cursor githubSourceCursor
		if err := json.Unmarshal([]byte(strings.TrimPrefix(value, githubSourceCursorPrefix)), &cursor); err != nil {
			return githubSourceCursor{}, fmt.Errorf("GitHub source cursor is malformed")
		}
		if cursor.Version != 1 {
			return githubSourceCursor{}, fmt.Errorf("GitHub source cursor version is unsupported")
		}
		for _, marker := range []string{cursor.Updated, cursor.Commits, cursor.Runs} {
			if marker != "" {
				if _, err := time.Parse(time.RFC3339Nano, marker); err != nil {
					return githubSourceCursor{}, fmt.Errorf("GitHub source cursor contains an invalid timestamp")
				}
			}
		}
		if len(cursor.PendingRuns) > githubSourceMaxPendingRuns {
			return githubSourceCursor{}, fmt.Errorf("GitHub source cursor exceeds the pending workflow-run limit")
		}
		seen := make(map[string]struct{}, len(cursor.PendingRuns))
		for _, runID := range cursor.PendingRuns {
			if !canonicalPositiveDecimal(runID) {
				return githubSourceCursor{}, fmt.Errorf("GitHub source cursor contains an invalid pending workflow-run ID")
			}
			if _, exists := seen[runID]; exists {
				return githubSourceCursor{}, fmt.Errorf("GitHub source cursor contains a duplicate pending workflow-run ID")
			}
			seen[runID] = struct{}{}
		}
		return cursor, nil
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return githubSourceCursor{}, fmt.Errorf("GitHub source cursor is neither a legacy timestamp nor a supported versioned cursor")
	}
	return githubSourceCursor{Version: 1, Updated: value, Commits: value, Runs: value}, nil
}

func encodeGitHubSourceCursor(cursor githubSourceCursor) (string, error) {
	if cursor.Version != 1 {
		return "", fmt.Errorf("GitHub source cursor version is unsupported")
	}
	if len(cursor.PendingRuns) > githubSourceMaxPendingRuns {
		return "", fmt.Errorf("GitHub source cursor exceeds the pending workflow-run limit")
	}
	for _, marker := range []*string{&cursor.Updated, &cursor.Commits, &cursor.Runs} {
		if strings.TrimSpace(*marker) == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, *marker)
		if err != nil {
			return "", fmt.Errorf("GitHub source cursor contains an invalid timestamp")
		}
		*marker = parsed.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	seen := make(map[string]struct{}, len(cursor.PendingRuns))
	for _, runID := range cursor.PendingRuns {
		if !canonicalPositiveDecimal(runID) {
			return "", fmt.Errorf("GitHub source cursor contains an invalid pending workflow-run ID")
		}
		if _, exists := seen[runID]; exists {
			return "", fmt.Errorf("GitHub source cursor contains a duplicate pending workflow-run ID")
		}
		seen[runID] = struct{}{}
	}
	sort.Strings(cursor.PendingRuns)
	if cursor.Commits == "" && cursor.Runs == "" && len(cursor.PendingRuns) == 0 {
		return cursor.Updated, nil
	}
	body, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode GitHub source cursor: %w", err)
	}
	encoded := githubSourceCursorPrefix + string(body)
	if len(encoded) > 512 {
		return "", fmt.Errorf("GitHub source cursor exceeds the 512-character storage limit")
	}
	return encoded, nil
}

func laterGitHubCursor(current, candidate string) string {
	current = strings.TrimSpace(current)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return current
	}
	if current == "" {
		return candidate
	}
	currentTime, currentErr := time.Parse(time.RFC3339Nano, current)
	candidateTime, candidateErr := time.Parse(time.RFC3339Nano, candidate)
	if currentErr == nil && candidateErr == nil {
		if candidateTime.After(currentTime) {
			return candidate
		}
		return current
	}
	if candidate > current {
		return candidate
	}
	return current
}

func hasGitHubStreamItem(items []ImportItem) bool {
	for _, item := range items {
		if item.ItemType == "github_commit" || item.ItemType == "github_workflow_run" {
			return true
		}
	}
	return false
}

func githubRecordTimestamp(record map[string]any, kind string) string {
	switch kind {
	case "commit":
		if commit, ok := record["commit"].(map[string]any); ok {
			if committer, ok := commit["committer"].(map[string]any); ok {
				if value := githubString(committer, "date"); value != "" {
					return value
				}
			}
			if author, ok := commit["author"].(map[string]any); ok {
				return githubString(author, "date")
			}
		}
	case "workflow_run":
		return githubString(record, "created_at")
	}
	return ""
}

func githubWindowLowerBound(cursor string) (time.Time, bool, error) {
	if strings.TrimSpace(cursor) == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, cursor)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("GitHub incremental timestamp is invalid")
	}
	// GitHub documents commit `since` as strictly after the timestamp. Two
	// seconds of lookback retains the prior second as well as the exact cursor
	// second; duplicate IDs are removed after any overlapping window scans.
	return parsed.UTC().Truncate(time.Second).Add(-githubSourceCursorLookback), true, nil
}

func fetchGitHubCommits(ctx context.Context, base *url.URL, resourcePath, cursor string, upper time.Time, token string, maxPages int) ([]map[string]any, error) {
	lower, incremental, err := githubWindowLowerBound(cursor)
	if err != nil {
		return nil, err
	}
	if !incremental {
		records, err := fetchGitHubInitialPages(ctx, base, resourcePath, "commit", token, maxPages)
		if err != nil {
			return nil, err
		}
		return dedupeGitHubRecords(records, "commit")
	}
	upper = upper.UTC().Truncate(time.Second)
	if lower.After(upper) {
		return nil, nil
	}
	budget := &githubSourceScanBudget{}
	records, err := fetchGitHubCommitWindow(ctx, base, resourcePath, lower, upper, token, maxPages, budget)
	if err != nil {
		return nil, err
	}
	return dedupeGitHubRecords(records, "commit")
}

func fetchGitHubCommitWindow(ctx context.Context, base *url.URL, resourcePath string, lower, upper time.Time, token string, maxPages int, budget *githubSourceScanBudget) ([]map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, newProviderSyncError("GitHub", 0, err)
	}
	budget.windows++
	if budget.windows > githubSourceMaxAdaptiveWindows {
		return nil, fmt.Errorf("GitHub commit scan exceeded its bounded time-window limit; cursor was not advanced")
	}
	pageRecords := make([]map[string]any, 0)
	truncated := false
	for page := 1; page <= maxPages; page++ {
		if budget.pages >= githubSourceMaxAdaptivePageCalls {
			return nil, fmt.Errorf("GitHub commit scan exceeded its bounded page-call limit; cursor was not advanced")
		}
		budget.pages++
		query := url.Values{}
		query.Set("per_page", strconv.Itoa(githubSourcePageSize))
		query.Set("page", strconv.Itoa(page))
		query.Set("since", lower.UTC().Format(time.RFC3339))
		query.Set("until", upper.UTC().Format(time.RFC3339))
		value, err := fetchGitHubJSONWithQuery(ctx, base, resourcePath, query, token)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}
		list, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("GitHub commit endpoint returned an unexpected response shape")
		}
		pageRecords = append(pageRecords, githubRecordSlice(list)...)
		if len(list) < githubSourcePageSize {
			return pageRecords, nil
		}
		if page == maxPages {
			truncated = true
		}
	}
	if !truncated {
		return pageRecords, nil
	}
	if upper.Sub(lower) < githubSourceMinSplitWindow {
		return nil, fmt.Errorf("GitHub commit timestamp window remains truncated at the provider's one-second precision; cursor was not advanced")
	}
	mid := lower.Add(upper.Sub(lower) / 2).UTC().Truncate(time.Second)
	leftUpper := mid.Add(githubSourceWindowOverlap)
	rightLower := mid.Add(-githubSourceWindowOverlap)
	if !leftUpper.Before(upper) || !rightLower.After(lower) {
		return nil, fmt.Errorf("GitHub commit window cannot be subdivided safely; cursor was not advanced")
	}
	left, err := fetchGitHubCommitWindow(ctx, base, resourcePath, lower, leftUpper, token, maxPages, budget)
	if err != nil {
		return nil, err
	}
	right, err := fetchGitHubCommitWindow(ctx, base, resourcePath, rightLower, upper, token, maxPages, budget)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func fetchGitHubWorkflowRuns(ctx context.Context, base *url.URL, resourcePath, cursor string, upper time.Time, token string, maxPages int) ([]map[string]any, error) {
	lower, incremental, err := githubWindowLowerBound(cursor)
	if err != nil {
		return nil, err
	}
	if !incremental {
		records, err := fetchGitHubInitialPages(ctx, base, resourcePath, "workflow_run", token, maxPages)
		if err != nil {
			return nil, err
		}
		return dedupeGitHubRecords(records, "workflow_run")
	}
	upper = upper.UTC().Truncate(time.Second)
	if lower.After(upper) {
		return nil, nil
	}
	budget := &githubSourceScanBudget{}
	records, err := fetchGitHubWorkflowRunWindow(ctx, base, resourcePath, lower, upper, token, maxPages, budget)
	if err != nil {
		return nil, err
	}
	return dedupeGitHubRecords(records, "workflow_run")
}

func dedupeGitHubRecords(records []map[string]any, kind string) ([]map[string]any, error) {
	seen := make(map[string]struct{}, len(records))
	unique := make([]map[string]any, 0, len(records))
	for _, record := range records {
		id := githubImportRecordIdentifier(record, kind)
		if id == "" {
			return nil, fmt.Errorf("GitHub %s scan returned a record without a stable identifier; cursor was not advanced", kind)
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, record)
	}
	return unique, nil
}

func fetchGitHubPendingWorkflowRuns(ctx context.Context, base *url.URL, resourcePath string, pendingIDs []string, token string) ([]map[string]any, error) {
	if len(pendingIDs) > githubSourceMaxPendingRuns {
		return nil, fmt.Errorf("pending GitHub workflow-run refresh exceeds the %d-run safety limit", githubSourceMaxPendingRuns)
	}
	refreshed := make([]map[string]any, 0, len(pendingIDs))
	seen := make(map[string]struct{}, len(pendingIDs))
	for _, runID := range pendingIDs {
		if !canonicalPositiveDecimal(runID) {
			return nil, fmt.Errorf("pending GitHub workflow-run ID is invalid")
		}
		if _, exists := seen[runID]; exists {
			return nil, fmt.Errorf("pending GitHub workflow-run ID is duplicated")
		}
		seen[runID] = struct{}{}
		value, err := fetchGitHubJSONWithQuery(ctx, base, resourcePath+"/"+runID, url.Values{}, token)
		if err != nil {
			return nil, fmt.Errorf("run %s: %w", runID, err)
		}
		record, ok := value.(map[string]any)
		if !ok || githubImportRecordIdentifier(record, "workflow_run") != runID || githubString(record, "status") == "" {
			return nil, fmt.Errorf("GitHub workflow-run refresh returned an incomplete record for run %s", runID)
		}
		refreshed = append(refreshed, record)
	}
	return refreshed, nil
}

func mergeGitHubWorkflowRunRecords(groups ...[]map[string]any) ([]map[string]any, error) {
	merged := make([]map[string]any, 0)
	positions := make(map[string]int)
	for _, group := range groups {
		for _, record := range group {
			runID := githubImportRecordIdentifier(record, "workflow_run")
			if runID == "" || githubString(record, "status") == "" {
				return nil, fmt.Errorf("GitHub workflow-run scan returned an incomplete record; cursor was not advanced")
			}
			if position, exists := positions[runID]; exists {
				merged[position] = record
				continue
			}
			positions[runID] = len(merged)
			merged = append(merged, record)
		}
	}
	return merged, nil
}

func pendingGitHubWorkflowRunIDs(records []map[string]any) ([]string, error) {
	pending := make([]string, 0)
	seen := make(map[string]struct{})
	for _, record := range records {
		status := strings.ToLower(strings.TrimSpace(githubString(record, "status")))
		if status == "" {
			return nil, fmt.Errorf("GitHub workflow-run record omitted status; cursor was not advanced")
		}
		if status == "completed" {
			continue
		}
		runID := githubImportRecordIdentifier(record, "workflow_run")
		if runID == "" {
			return nil, fmt.Errorf("GitHub workflow-run record omitted a stable identifier; cursor was not advanced")
		}
		if _, exists := seen[runID]; exists {
			continue
		}
		seen[runID] = struct{}{}
		pending = append(pending, runID)
	}
	if len(pending) > githubSourceMaxPendingRuns {
		return nil, fmt.Errorf("GitHub scan found more than %d unfinished runs; cursor was not advanced", githubSourceMaxPendingRuns)
	}
	sort.Strings(pending)
	return pending, nil
}

func fetchGitHubWorkflowRunWindow(ctx context.Context, base *url.URL, resourcePath string, lower, upper time.Time, token string, maxPages int, budget *githubSourceScanBudget) ([]map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, newProviderSyncError("GitHub", 0, err)
	}
	budget.windows++
	if budget.windows > githubSourceMaxAdaptiveWindows {
		return nil, fmt.Errorf("GitHub Actions run scan exceeded its bounded time-window limit; cursor was not advanced")
	}
	createdRange := lower.UTC().Format(time.RFC3339) + ".." + upper.UTC().Format(time.RFC3339)
	pageRecords := make([]map[string]any, 0)
	var totalCount int
	for page := 1; page <= maxPages; page++ {
		if budget.pages >= githubSourceMaxAdaptivePageCalls {
			return nil, fmt.Errorf("GitHub Actions run scan exceeded its bounded page-call limit; cursor was not advanced")
		}
		budget.pages++
		query := url.Values{}
		query.Set("per_page", strconv.Itoa(githubSourcePageSize))
		query.Set("page", strconv.Itoa(page))
		query.Set("created", createdRange)
		value, err := fetchGitHubJSONWithQuery(ctx, base, resourcePath, query, token)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("GitHub Actions endpoint returned an unexpected response shape")
		}
		list, ok := object["workflow_runs"].([]any)
		if !ok {
			return nil, fmt.Errorf("GitHub Actions response omitted workflow_runs; cursor was not advanced")
		}
		count, ok := object["total_count"].(float64)
		if !ok || count < 0 || count != math.Trunc(count) {
			return nil, fmt.Errorf("GitHub Actions response omitted a valid total_count; cursor was not advanced")
		}
		totalCount = int(count)
		pageRecords = append(pageRecords, githubRecordSlice(list)...)
		if totalCount >= 1000 {
			break
		}
		if len(pageRecords) >= totalCount {
			return pageRecords, nil
		}
		if len(list) < githubSourcePageSize {
			break
		}
	}
	if totalCount < 1000 && len(pageRecords) == totalCount {
		return pageRecords, nil
	}
	if upper.Sub(lower) < githubSourceMinSplitWindow {
		return nil, fmt.Errorf("GitHub Actions run window remains incomplete at the provider's one-second precision; cursor was not advanced")
	}
	mid := lower.Add(upper.Sub(lower) / 2).UTC().Truncate(time.Second)
	leftUpper := mid.Add(githubSourceWindowOverlap)
	rightLower := mid.Add(-githubSourceWindowOverlap)
	if !leftUpper.Before(upper) || !rightLower.After(lower) {
		return nil, fmt.Errorf("GitHub Actions run window cannot be subdivided safely; cursor was not advanced")
	}
	left, err := fetchGitHubWorkflowRunWindow(ctx, base, resourcePath, lower, leftUpper, token, maxPages, budget)
	if err != nil {
		return nil, err
	}
	right, err := fetchGitHubWorkflowRunWindow(ctx, base, resourcePath, rightLower, upper, token, maxPages, budget)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func fetchGitHubInitialPages(ctx context.Context, base *url.URL, resourcePath, kind, token string, maxPages int) ([]map[string]any, error) {
	items := make([]map[string]any, 0)
	for page := 1; page <= maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, newProviderSyncError("GitHub", 0, err)
		}
		query := url.Values{}
		query.Set("per_page", strconv.Itoa(githubSourcePageSize))
		query.Set("page", strconv.Itoa(page))
		value, err := fetchGitHubJSONWithQuery(ctx, base, resourcePath, query, token)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}
		if kind == "commit" {
			list, ok := value.([]any)
			if !ok {
				return nil, fmt.Errorf("GitHub commit endpoint returned an unexpected response shape")
			}
			items = append(items, githubRecordSlice(list)...)
			if len(list) < githubSourcePageSize {
				return items, nil
			}
		} else {
			object, ok := value.(map[string]any)
			if !ok {
				if empty, isArray := value.([]any); isArray && len(empty) == 0 {
					return items, nil
				}
				return nil, fmt.Errorf("GitHub Actions endpoint returned an unexpected response shape")
			}
			list, ok := object["workflow_runs"].([]any)
			if !ok {
				return nil, fmt.Errorf("GitHub Actions response omitted workflow_runs")
			}
			items = append(items, githubRecordSlice(list)...)
			count, hasTotal := object["total_count"].(float64)
			if hasTotal && count >= 0 && len(items) >= int(count) {
				return items, nil
			}
			if len(list) < githubSourcePageSize {
				if hasTotal && len(items) < int(count) {
					return nil, fmt.Errorf("GitHub Actions initial listing was incomplete; cursor was not advanced")
				}
				return items, nil
			}
		}
		if page == maxPages {
			return nil, fmt.Errorf("GitHub %s initial listing reached the %d-page safety limit; cursor was not advanced", kind, maxPages)
		}
	}
	return items, nil
}

func fetchGitHubJSONWithQuery(ctx context.Context, base *url.URL, resourcePath string, query url.Values, token string) (any, error) {
	target := *base
	target.Path = strings.TrimRight(base.Path, "/") + resourcePath
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "HAI-connected-source")
	if token = strings.TrimSpace(token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: sourceHTTPTimeout(), Transport: sourceHTTPTransport(), CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, newProviderSyncError("GitHub", 0, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if options, rateLimited := githubRateLimitErrorOptions(response); rateLimited {
			return nil, newProviderSyncErrorWithOptions("GitHub", response.StatusCode, errors.New("provider rate limit"), options)
		}
		return nil, newProviderSyncError("GitHub", response.StatusCode, errors.New("provider rejected request"))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, sourceHTTPMaxBytes()+1))
	if err != nil {
		return nil, newProviderSyncError("GitHub", 0, err)
	}
	if int64(len(body)) > sourceHTTPMaxBytes() {
		retryable := false
		return nil, newProviderSyncErrorWithOptions("GitHub", 0, errors.New("provider response exceeded the configured size limit"), providerSyncErrorOptions{RetryableOverride: &retryable})
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		retryable := false
		return nil, newProviderSyncErrorWithOptions("GitHub", 0, errors.New("provider returned malformed JSON"), providerSyncErrorOptions{RetryableOverride: &retryable})
	}
	return value, nil
}

func fetchGitHubJSON(ctx context.Context, base *url.URL, resourcePath, cursor string, page int, token string) (any, error) {
	query := url.Values{}
	query.Set("per_page", strconv.Itoa(githubSourcePageSize))
	query.Set("page", strconv.Itoa(page))
	query.Set("state", "all")
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	if cursor != "" && resourcePath != "" && resourcePath != "/repos/" {
		if _, err := time.Parse(time.RFC3339, cursor); err == nil && (strings.HasSuffix(resourcePath, "/issues") || strings.HasSuffix(resourcePath, "/pulls")) {
			query.Set("since", cursor)
		}
	}
	return fetchGitHubJSONWithQuery(ctx, base, resourcePath, query, token)
}

func githubRateLimitErrorOptions(response *http.Response) (providerSyncErrorOptions, bool) {
	if response == nil || (response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusTooManyRequests) {
		return providerSyncErrorOptions{}, false
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	bodyText := strings.ToLower(string(body))
	remaining := strings.TrimSpace(response.Header.Get("X-RateLimit-Remaining"))
	retryAfter := strings.TrimSpace(response.Header.Get("Retry-After"))
	secondarySignal := strings.Contains(bodyText, "secondary rate limit") ||
		strings.Contains(bodyText, "rate limit exceeded") ||
		strings.Contains(bodyText, "abuse detection")
	if response.StatusCode != http.StatusTooManyRequests && remaining != "0" && retryAfter == "" && !secondarySignal {
		return providerSyncErrorOptions{}, false
	}

	if retryAfter == "" {
		if reset, err := strconv.ParseInt(strings.TrimSpace(response.Header.Get("X-RateLimit-Reset")), 10, 64); err == nil && reset > 0 {
			retryAfter = time.Unix(reset, 0).UTC().Format(http.TimeFormat)
		} else {
			// GitHub's secondary-limit guidance requires at least one minute when
			// it omits Retry-After. The same conservative delay covers a 429 with
			// incomplete rate-limit headers.
			retryAfter = "60"
		}
	} else if _, valid, _ := parseProviderRetryAfter(retryAfter, time.Now().UTC()); !valid {
		retryAfter = "60"
	}
	retryable := true
	return providerSyncErrorOptions{RetryableOverride: &retryable, RetryAfterHeader: retryAfter}, true
}

func githubSourceTokenForOwner(ownerIdentity string) string {
	token := strings.TrimSpace(os.Getenv("GITHUB_SOURCE_TOKEN"))
	configuredOwner := strings.TrimSpace(os.Getenv(githubSourceTokenOwnerIdentityEnv))
	if token == "" || configuredOwner == "" || ownerIdentity == "" ||
		strings.TrimSpace(ownerIdentity) != ownerIdentity || ownerIdentity != configuredOwner {
		return ""
	}
	return token
}

func githubSourceMaxPages() int {
	pages, err := strconv.Atoi(strings.TrimSpace(os.Getenv("GITHUB_SOURCE_MAX_PAGES")))
	if err != nil || pages < 1 || pages > 20 {
		return 5
	}
	return pages
}

func githubRecords(value any, kind string) []map[string]any {
	if object, ok := value.(map[string]any); ok {
		if kind == "workflow_run" {
			if runs, ok := object["workflow_runs"].([]any); ok {
				return githubRecordSlice(runs)
			}
		}
		if kind == "issue" && isGitHubPullRequest(object) {
			return nil
		}
		return []map[string]any{object}
	}
	if list, ok := value.([]any); ok {
		records := githubRecordSlice(list)
		if kind != "issue" {
			return records
		}
		issues := make([]map[string]any, 0, len(records))
		for _, record := range records {
			if !isGitHubPullRequest(record) {
				issues = append(issues, record)
			}
		}
		return issues
	}
	return nil
}

func githubRecordCount(value any, kind string) int {
	if object, ok := value.(map[string]any); ok {
		if kind == "workflow_run" {
			if runs, ok := object["workflow_runs"].([]any); ok {
				return len(runs)
			}
		}
		return 1
	}
	if list, ok := value.([]any); ok {
		return len(list)
	}
	return 0
}

func isGitHubPullRequest(record map[string]any) bool {
	_, found := record["pull_request"]
	return found
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
	identifier := githubImportRecordIdentifier(record, kind)
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
	observedAt := time.Now().UTC().Format(time.RFC3339Nano)
	contentParts := []string{
		"GitHub " + strings.ReplaceAll(kind, "_", " ") + " from " + repository,
		"Title: " + title,
		"Status: " + githubString(record, "state", "status", "conclusion"),
		body,
	}
	if kind == "workflow_run" {
		contentParts = append(contentParts, "Status last observed at: "+observedAt+"; this is the last successful source observation, not a live status.")
	}
	content := strings.TrimSpace(strings.Join(contentParts, "\n"))
	updated := githubString(record, "updated_at", "created_at", "run_started_at", "timestamp")
	headSHA := ""
	if head, ok := record["head"].(map[string]any); ok {
		headSHA = githubString(head, "sha")
	}
	metadata, err := json.Marshal(githubImportMetadata{
		Source:         "github",
		Repository:     repository,
		Kind:           kind,
		Updated:        updated,
		FetchedAt:      observedAt,
		Status:         githubString(record, "status"),
		Conclusion:     githubString(record, "conclusion"),
		ReviewRequired: true,
		HeadSHA:        firstNonEmpty(headSHA, githubString(record, "head_sha")),
		WorkflowName:   githubRawString(record, "name"),
		WorkflowPath:   githubRawString(record, "path"),
		WorkflowEvent:  githubRawString(record, "event"),
		MergeCommitSHA: githubString(record, "merge_commit_sha"),
		Merged:         githubBoolean(record, "merged") || githubString(record, "merged_at") != "",
	})
	if err != nil {
		return ImportItem{}, ""
	}
	return ImportItem{
		ExternalID: "github:" + kind + ":" + identifier,
		Title:      compact(title, 500),
		Content:    compact(content, 12000),
		SourceURI:  githubString(record, "html_url", "url"),
		ItemType:   "github_" + kind,
		ProjectKey: projectKey,
		Metadata:   string(metadata),
	}, updated
}

func githubBoolean(record map[string]any, key string) bool {
	value, _ := record[key].(bool)
	return value
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

func githubRawString(record map[string]any, key string) string {
	value, _ := record[key].(string)
	return value
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
	host = strings.TrimSpace(host)
	if strings.Contains(host, "%") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return sourceHTTPIPBlocked(ip, false)
}

func sourceHTTPURLAddress(target *url.URL) string {
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(target.Hostname(), port)
}

func sourceHTTPLoopbackAddressAllowed(address string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || !sourceHTTPHostIsLoopback(host) {
		return false
	}
	for _, allowed := range strings.Split(os.Getenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS"), ",") {
		if strings.EqualFold(strings.TrimSpace(allowed), address) {
			return true
		}
	}
	return false
}

func sourceHTTPHostIsLoopback(host string) bool {
	host = strings.TrimSpace(host)
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sourceHTTPIPBlocked(ip net.IP, allowLoopback bool) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		if addr, ok := netip.AddrFromSlice(v4); ok && sourceHTTPInDeniedPrefix(addr, sourceDeniedIPv4Prefixes) {
			return true
		}
	} else if addr, ok := netip.AddrFromSlice(ip); ok {
		if sourceHTTPInDeniedPrefix(addr, sourceDeniedIPv6Prefixes) ||
			(addr.Is6() && !addr.IsLoopback() && sourceHTTPInDeniedPrefix(addr, []netip.Prefix{netip.MustParsePrefix("::/96")})) {
			return true
		}
	}
	if sourceHTTPMetadataIP(ip) || sourceHTTPCarrierGradeNAT(ip) {
		return true
	}
	if ip.IsLoopback() {
		return !allowLoopback
	}
	return ip.IsUnspecified() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func sourceHTTPInDeniedPrefix(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func sourceHTTPMetadataIP(ip net.IP) bool {
	return ip.Equal(net.ParseIP("169.254.169.254")) || ip.Equal(net.ParseIP("168.63.129.16")) || ip.Equal(net.ParseIP("100.100.100.200"))
}

func sourceHTTPCarrierGradeNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 0x40
}

var (
	sourceDeniedIPv4Prefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	}
	sourceDeniedIPv6Prefixes = []netip.Prefix{
		netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("3fff::/20"),
		netip.MustParsePrefix("fec0::/10"),
	}
)

func dialSourceHTTPAddress(
	ctx context.Context,
	network, address string,
	lookup func(context.Context, string) ([]net.IPAddr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid json-feed network address: %w", err)
	}
	allowLoopback := sourceHTTPLoopbackAddressAllowed(address) && sourceHTTPHostIsLoopback(host)
	if ip := net.ParseIP(host); ip != nil {
		if sourceHTTPIPBlocked(ip, allowLoopback) {
			return nil, fmt.Errorf("json-feed host resolved to blocked address space")
		}
		return dial(ctx, network, address)
	}
	resolved, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve json-feed host: %w", err)
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("json-feed host resolved to no addresses")
	}
	for _, candidate := range resolved {
		if sourceHTTPIPBlocked(candidate.IP, allowLoopback && strings.EqualFold(strings.TrimSuffix(host, "."), "localhost")) {
			return nil, fmt.Errorf("json-feed host resolved to blocked address space")
		}
	}
	return dial(ctx, network, net.JoinHostPort(resolved[0].IP.String(), port))
}

func sourceHTTPTransport() *http.Transport {
	key := strings.Join([]string{
		strings.TrimSpace(os.Getenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS")),
		strings.TrimSpace(os.Getenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS")),
		sourceHTTPTimeout().String(),
	}, "|")

	sharedSourceHTTPTransport.Lock()
	defer sharedSourceHTTPTransport.Unlock()
	if sharedSourceHTTPTransport.transport != nil && sharedSourceHTTPTransport.key == key {
		return sharedSourceHTTPTransport.transport
	}
	if sharedSourceHTTPTransport.transport != nil {
		// Do not retain connections created under a previous network policy.
		sharedSourceHTTPTransport.transport.CloseIdleConnections()
	}

	transport := &http.Transport{
		Proxy:                 nil,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: sourceHTTPTimeout(),
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: sourceHTTPTimeout()}
			return dialSourceHTTPAddress(ctx, network, address,
				net.DefaultResolver.LookupIPAddr, dialer.DialContext)
		},
	}
	sharedSourceHTTPTransport.key = key
	sharedSourceHTTPTransport.transport = transport
	return transport
}

func sourceHTTPTimeout() time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CONNECTED_SOURCE_HTTP_TIMEOUT_SECONDS")))
	if err != nil || seconds < 1 || seconds > 120 {
		seconds = 20
	}
	return time.Duration(seconds) * time.Second
}

func sourceSyncTimeout() time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CONNECTED_SOURCE_SYNC_TIMEOUT_SECONDS")))
	if err != nil || seconds < 30 || seconds > 30*60 {
		seconds = 10 * 60
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

// validatedSyncFrequency prevents a source from displaying a scheduled
// interval that the scheduler cannot understand. Manual aliases are persisted
// consistently, while operator-triggered transcription remains manual-only.
func validatedSyncFrequency(connectorKey, value string) (string, error) {
	clean := strings.TrimSpace(strings.ToLower(value))
	switch clean {
	case "", "manual", "off", "disabled", "none":
		return "manual", nil
	}
	if !connectorSupportsMode(connectorKey, ModeScheduledSync) {
		return "", fmt.Errorf("connector %s is operator-triggered only and must use manual sync frequency", connectorKey)
	}
	if _, ok := parseSyncFrequency(clean); !ok {
		return "", fmt.Errorf("sync frequency %q is unsupported; use manual, hourly, daily, weekly, or a duration of at least one minute", value)
	}
	return strings.TrimSpace(value), nil
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
		return "", fmt.Errorf("folder path must stay inside the configured local source root")
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
		return "", fmt.Errorf("folder path must not resolve outside the configured local source root")
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
