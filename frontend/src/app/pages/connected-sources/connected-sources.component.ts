import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Inject, OnDestroy, OnInit, QueryList, ViewChildren } from '@angular/core';
import { AbstractControl, FormBuilder, FormGroup, ValidationErrors, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { forkJoin, Observable, of, Subscription, timer } from 'rxjs';
import { catchError, finalize, switchMap, tap, timeout } from 'rxjs/operators';
import {
  IConnectedSource,
  ISourceAuditLog,
  ISourceConnector,
  ISourceConnectionHealth,
  ISourceExtraction,
  ISourceExtractionPage,
  ISourceExtractionCorrectionRecovery,
  ISourceExtractionCorrectionView,
  SourceExtractionCorrectionUiState,
  IKnowledgeGraphResult,
  IKnowledgeGraphSourceRef,
  ISourcePursuitRoutingOutcome,
  ISourceSearchResult,
  ISourceSyncJob,
  ISourceManualSyncJob,
  ISourceSyncResult,
  ISourceLifeGraphProjectionOutcome,
} from '../../models/connected-source.model.interface';
import { CONNECTED_SOURCE_SERVICE_TOKEN } from '../../services/connected-source/connected-source.service.token';
import { IConnectedSourceService, ISourceDestructiveAuthorization } from '../../services/connected-source.service.interface';
import { ThemeMode, ThemeService } from '../../services/theme.service';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component';

type SourceAction =
  | 'connect'
  | 'gmail'
  | 'odoo'
  | 'import'
  | 'folder'
  | 'whatsapp'
  | 'search'
  | 'graph';

type SourceMutation = 'pause' | 'resume' | 'reindex' | 'revoke';

interface SourceActionCard {
  id: SourceAction;
  title: string;
  detail: string;
  icon: string;
  metric: string;
  tone: 'blue' | 'green' | 'gold';
}

const manualSyncValues = new Set(['', 'manual', 'off', 'disabled', 'none']);
const inProgressSyncStatuses = new Set(['queued', 'running', 'pending', 'sync_queued', 'sync_running']);
const syncDurationPart = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g;

function syncFrequencyValidator(control: AbstractControl): ValidationErrors | null {
  const value = String(control.value || '').trim().toLowerCase();
  if (manualSyncValues.has(value) || value === 'hourly' || value === 'daily' || value === 'weekly') {
    return null;
  }
  let totalMilliseconds = 0;
  let consumed = '';
  syncDurationPart.lastIndex = 0;
  for (let match = syncDurationPart.exec(value); match; match = syncDurationPart.exec(value)) {
    const amount = Number(match[1]);
    const multiplier = ({
      ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60000, h: 3600000,
    } as Record<string, number>)[match[2]];
    if (!Number.isFinite(amount) || multiplier === undefined) {
      return { syncFrequency: true };
    }
    totalMilliseconds += amount * multiplier;
    consumed += match[0];
  }
  return consumed === value && totalMilliseconds >= 60000 && Number.isFinite(totalMilliseconds)
    ? null
    : { syncFrequency: true };
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-connected-sources',
    templateUrl: './connected-sources.component.html',
    styleUrls: ['./connected-sources.component.scss'],
    standalone: false
})
export class ConnectedSourcesComponent implements OnInit, OnDestroy {
  readonly moduleId = 'connected-sources';
  @ViewChildren(HaiProgressiveSectionComponent) private disclosures?: QueryList<HaiProgressiveSectionComponent>;
  connectors: ISourceConnector[] = [];
  sources: IConnectedSource[] = [];
  extractions: ISourceExtraction[] = [];
  extractionTotalCount = 0;
  auditLogs: ISourceAuditLog[] = [];
  syncJobs: ISourceSyncJob[] = [];
  sourceExtractionCounts: Record<string, number> = {};
  latestJobsBySource: Record<string, ISourceSyncJob> = {};
  connectionHealth: Record<string, ISourceConnectionHealth> = {};
  connectionHealthUnavailable: Record<string, boolean> = {};
  connectionHealthChecking: Record<string, boolean> = {};
  searchResult?: ISourceSearchResult;
  knowledgeGraph?: IKnowledgeGraphResult;
  graphLoading = false;
  graphIncludeSensitive = false;
  lastSyncResult?: ISourceSyncResult;
  includeDisabled = true;
  includeArchived = false;
  loading = false;
  connecting = false;
  authorizing = false;
  syncing = false;
  manualSyncSubmittingSourceId = '';
  manualSyncRecoveryMessages: Record<string, string> = {};
  manualSyncRecoveryActions: Record<string, 'retry' | 'refresh' | 'review'> = {};
  manualSyncStatusUnavailable: Record<string, boolean> = {};
  sourceMutationsInProgress: Record<string, SourceMutation> = {};
  manualSyncRetryNotBefore: Record<string, number> = {};
  manualSyncJobsBySource: Record<string, ISourceManualSyncJob> = {};
  extractionCorrectionRecoveries: Record<string, ISourceExtractionCorrectionRecovery> = {};
  extractionCorrectionViews: Record<string, ISourceExtractionCorrectionView> = {};
  operationError = '';
  destructiveTarget?: { action: 'revoke' | 'delete'; id: string; label: string };
  destructiveSubmitting = false;
  destructiveError = '';
  private readonly uncertainDestructiveActions = new Set<string>();
  connectorCatalogLoading = false;
  connectorCatalogLoaded = false;
  connectorCatalogUnavailable = false;
  sourcesLoaded = false;
  syncJobsLoaded = false;
  syncJobsUnavailable = false;
  extractionsLoading = false;
  extractionsLoaded = false;
  auditLogsLoading = false;
  auditLogsLoaded = false;
  searchLoading = false;
  searchError = '';
  graphError = '';
  loadWarnings: string[] = [];
  selectedAction: SourceAction = 'connect';
  sourceActions: SourceActionCard[] = [];
  selectedSourceId = '';
  themeMode: ThemeMode = 'light';
  sourceFilterQuery = '';
  sourceEnabledFilter: 'all' | 'enabled' | 'paused' = 'all';
  extractionFilterQuery = '';
  activityFilterQuery = '';
  activityTypeFilter: 'all' | 'sync' | 'audit' | 'failed' = 'all';
  private readonly loadTimeoutMs = 6000;
  private readonly operationTimeoutMs = 15000;
  private readonly manualSyncAcceptanceTimeoutMs = 10000;
  private readonly manualSyncPollIntervalMs = 3000;
  private readonly manualSyncSessionPrefix = 'hai.manual-source-sync.v1.';
  private readonly manualSyncRetrySessionPrefix = 'hai.manual-source-sync.retry-after.v1.';
  private readonly extractionCorrectionSessionKey = 'hai.source-extraction-corrections.v1';
  private readonly manualSyncPolls = new Map<string, Subscription>();
  private readonly manualSyncRetryTimers = new Map<string, Subscription>();
  private readonly extractionCorrectionPolls = new Map<string, Subscription>();
  private readonly extractionCorrectionLocalMessages: Record<string, string> = {};
  private readonly extractionCorrectionPollIntervalMs = 3000;
  private readonly extractionCorrectionMaxStatusChecks = 20;
  private readonly extractionCorrectionMaxPollFailures = 4;
  private readonly manualSyncFallbackKeys = new Map<string, string>();
  private readonly extractionPageLimit = 100;
  private lastSelectedConnectorKey = 'local-folder';
  private refreshSubscription?: Subscription;
  private connectorSubscription?: Subscription;
  private connectorChangeSubscription?: Subscription;
  private connectionHealthSubscription?: Subscription;
  private selectedHealthSubscription?: Subscription;
  private extractionSubscription?: Subscription;
  private auditSubscription?: Subscription;
  private searchSubscription?: Subscription;
  private graphSubscription?: Subscription;
  private themeSubscription?: Subscription;
  private selectedHealthSourceId = '';
  private healthRequestSequence = 0;
  private healthRequestVersions: Record<string, number> = {};
  private destroyed = false;

  sourceForm: FormGroup = this.fb.group({
    connectorKey: ['local-folder', [Validators.required]],
    name: ['Selected local folder', [Validators.required]],
    localOnly: [true],
    syncFrequency: ['manual', [syncFrequencyValidator]],
    syncTarget: [''],
    defaultProjectKey: ['018-HAI'],
    excludePatterns: ['spam,trash'],
  });

  destructiveApprovalForm = this.fb.group({
    taskId: ['', [Validators.required, Validators.pattern(/^[!-~]{1,255}$/)]],
    approvalSourceId: ['', [Validators.required, Validators.pattern(/^[!-~]{1,255}$/)]],
    approvalBindingDigest: ['', [Validators.required, Validators.pattern(/^[a-f0-9]{64}$/)]],
    idempotencyKey: ['', [Validators.required, Validators.pattern(/^[!-~]{16,128}$/)]],
  });

  importForm: FormGroup = this.fb.group({
    sourceId: ['', [Validators.required]],
    mode: ['manual_import'],
    projectKey: ['018-HAI'],
    externalId: ['', [Validators.required]],
    title: ['', [Validators.required]],
    sourceUri: ['', [Validators.required]],
    content: ['', [Validators.required]],
  });

  folderForm: FormGroup = this.fb.group({
    sourceId: ['', [Validators.required]],
    mode: ['incremental_sync'],
    projectKey: ['018-HAI'],
    folderPath: ['.', [Validators.required]],
    limit: [100],
    maxBytes: [1048576],
  });

  whatsappForm: FormGroup = this.fb.group({
    sourceId: ['', [Validators.required]],
    name: ['WhatsApp exports for Robert'],
    projectKey: ['Robert-life-os', [Validators.required]],
    folderPath: ['whatsapp'],
    chatTitle: ['', [Validators.required]],
    pastedExport: ['', [Validators.required]],
    chunkMessages: [40],
    maxBytes: [2097152],
  });

  odooForm: FormGroup = this.fb.group({
    sourceId: [''],
    name: ['Odoo / HERP workspace'],
    baseUrl: ['https://noodzakelijk-online1.odoo.com/odoo'],
    apps: ['CRM, Sales, Invoicing and Accounting, Project, Helpdesk, Documents and Sign, Calendar and Appointments'],
    projectKey: ['Robert-life-os'],
    localOnly: [true],
    syncFrequency: ['manual', [syncFrequencyValidator]],
  });

  searchForm: FormGroup = this.fb.group({
    query: ['connected sources decisions follow up', [Validators.required]],
    projectKey: ['018-HAI'],
    limit: [8],
    includeSensitive: [false],
  });

  constructor(
    private fb: FormBuilder,
    @Inject(CONNECTED_SOURCE_SERVICE_TOKEN)
    private sourceService: IConnectedSourceService,
    private notification: NzNotificationService,
    private router: Router,
    private themeService: ThemeService,
    private viewPreferences: ModuleViewPreferencesService,
    private changeDetector: ChangeDetectorRef,
  ) {}

  get isAdvancedView(): boolean {
    return this.viewPreferences.get(this.moduleId).mode === 'advanced';
  }

  openAdvancedAction(action: SourceAction, targetId: string, sourceId?: string): void {
    this.selectedAction = action;
    if (sourceId && this.selectedSourceId && this.selectedSourceId !== sourceId) this.closeSelectedSourceDisclosures();
    if (sourceId) this.selectedSourceId = sourceId;
    this.viewPreferences.setMode(this.moduleId, 'advanced');
    if (['source-records', 'source-activity'].includes(targetId)) {
      this.viewPreferences.setSection(this.moduleId, targetId, true);
    }

    const scrollToTarget = () => {
      if (typeof document !== 'undefined') {
        window.setTimeout(() => document.getElementById(targetId)?.scrollIntoView({ block: 'start' }), 0);
      }
    };
    const navigateToTarget = () => Promise.resolve(this.router.navigate(['/connected-sources'], {
      fragment: targetId,
      queryParamsHandling: 'preserve',
      replaceUrl: true,
    })).then(scrollToTarget);
    const currentFragment = this.router.url.split('#', 2)[1]?.split('?', 1)[0];

    // Force a NavigationEnd when the same section is reopened after switching
    // back to Basic; the app shell reads the saved module preference there.
    if (currentFragment === targetId) {
      void Promise.resolve(this.router.navigateByUrl(this.router.url.split('#', 1)[0], { replaceUrl: true }))
        .then(() => navigateToTarget());
      return;
    }

    void navigateToTarget();
  }

  onExtractionsSectionOpen(open: boolean): void {
    if (open && !this.extractionsLoaded && !this.extractionsLoading) this.loadExtractions();
  }

  onActivitySectionOpen(open: boolean): void {
    if (open && !this.auditLogsLoaded && !this.auditLogsLoading) this.loadAuditLogs();
  }

  ngOnInit(): void {
    this.themeMode = this.themeService.mode();
    this.themeSubscription = this.themeService.changes$?.subscribe((mode) => this.themeMode = mode);
    this.restoreExtractionCorrections();
    this.closeSelectedSourceDisclosures();
    this.connectorChangeSubscription = this.sourceForm.get('connectorKey')?.valueChanges.subscribe(
      (connectorKey) => this.connectorChanged(String(connectorKey || ''))
    );
    this.updateSourceActions();
    this.refresh();
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    this.refreshSubscription?.unsubscribe();
    this.connectorSubscription?.unsubscribe();
    this.connectorChangeSubscription?.unsubscribe();
    this.connectionHealthSubscription?.unsubscribe();
    this.selectedHealthSubscription?.unsubscribe();
    this.extractionSubscription?.unsubscribe();
    this.auditSubscription?.unsubscribe();
    this.searchSubscription?.unsubscribe();
    this.graphSubscription?.unsubscribe();
    this.themeSubscription?.unsubscribe();
    this.manualSyncPolls.forEach((poll) => poll.unsubscribe());
    this.manualSyncPolls.clear();
    this.manualSyncRetryTimers.forEach((timerSubscription) => timerSubscription.unsubscribe());
    this.manualSyncRetryTimers.clear();
    this.extractionCorrectionPolls.forEach((poll) => poll.unsubscribe());
    this.extractionCorrectionPolls.clear();
  }

  refresh(): void {
    this.refreshSubscription?.unsubscribe();
    this.connectorSubscription?.unsubscribe();
    this.connectionHealthSubscription?.unsubscribe();
    this.selectedHealthSubscription?.unsubscribe();
    this.selectedHealthSourceId = '';
    this.connectionHealth = {};
    this.connectionHealthUnavailable = {};
    this.connectionHealthChecking = this.sources.reduce<Record<string, boolean>>((checking, source) => {
      this.healthRequestVersions[source.id] = ++this.healthRequestSequence;
      checking[source.id] = true;
      return checking;
    }, {});
    this.loading = true;
    this.loadWarnings = [];
    this.sourcesLoaded = false;
    this.syncJobsLoaded = false;
    this.syncJobsUnavailable = false;
    this.connectorCatalogLoading = true;
    this.connectorCatalogLoaded = false;
    this.connectorCatalogUnavailable = false;
    const openSections = this.viewPreferences.get(this.moduleId).openSections;
    if (this.isAdvancedView && openSections['source-records']) {
      this.extractionsLoaded = false;
      this.onExtractionsSectionOpen(true);
    }
    if (this.isAdvancedView && openSections['source-activity']) {
      this.auditLogsLoaded = false;
      this.onActivitySectionOpen(true);
    }
    // The connector catalog controls whether the primary source action is safe
    // to enable. It must not wait for unrelated historical panels to finish.
    this.connectorSubscription = this.sourceService.connectors().pipe(
      timeout(this.loadTimeoutMs),
      tap((connectors) => {
        if (!Array.isArray(connectors)) throw new Error('Connector catalog response was not a list.');
        this.connectorCatalogLoaded = true;
      }),
      catchError(() => {
        this.connectorCatalogUnavailable = true;
        this.recordLoadWarning('Connector catalog');
        this.notification.error('Connectors unavailable', 'Connector status did not load in time.');
        return of([] as ISourceConnector[]);
      })
    ).subscribe((connectors) => {
      this.connectorCatalogLoading = false;
      this.connectors = connectors;
      this.updateSourceActions();
    });
    this.refreshSubscription = forkJoin({
      sources: this.sourceService.sources(this.includeDisabled).pipe(
        timeout(this.loadTimeoutMs),
        tap((sources) => {
          if (!Array.isArray(sources)) throw new Error('Connected sources response was not a list.');
          this.sourcesLoaded = true;
        }),
        catchError(() => {
          this.recordLoadWarning('Connected sources');
          this.notification.error('Sources unavailable', 'Connected sources did not load in time.');
          return of([] as IConnectedSource[]);
        })
      ),
      syncJobs: this.sourceService.syncJobs().pipe(
        timeout(this.loadTimeoutMs),
        tap((jobs) => {
          if (!Array.isArray(jobs)) throw new Error('Sync jobs response was not a list.');
          this.syncJobsLoaded = true;
        }),
        catchError(() => {
          this.syncJobsUnavailable = true;
          this.recordLoadWarning('Sync jobs');
          return of([] as ISourceSyncJob[]);
        })
      ),
    })
      .pipe(finalize(() => {
        this.loading = false;
        if (!this.destroyed) this.changeDetector.detectChanges();
      }))
      .subscribe(({ sources, syncJobs }) => {
        this.sources = sources;
        this.syncJobs = syncJobs || [];
        this.rebuildSourceIndexes();
        this.applySourceDefaults(this.sources);
        this.loadConnectionHealth(this.sources);
        this.resumeManualSyncPolling(this.syncJobs);
        this.updateSourceActions();
      });
  }

  hasLoadWarnings(): boolean {
    return this.loadWarnings.length > 0;
  }

  private recordLoadWarning(label: string): void {
    if (!this.loadWarnings.includes(label)) {
      this.loadWarnings = [...this.loadWarnings, label];
    }
  }

  private clearLoadWarning(label: string): void {
    this.loadWarnings = this.loadWarnings.filter((warning) => warning !== label);
  }

  private operationErrorMessage(error: unknown, fallback: string): string {
    const candidate = (error as { error?: { error?: unknown } })?.error?.error;
    if (typeof candidate === 'string' && candidate.trim()) {
      return this.safeOperationalText(candidate).slice(0, 280);
    }
    if ((error as { name?: unknown })?.name === 'TimeoutError') {
      return `No response within ${Math.round(this.operationTimeoutMs / 1000)} seconds. Check system status and retry.`;
    }
    return fallback;
  }

  connectSource(): void {
    if (!this.sourceCanConnect() || this.connecting) {
      return;
    }
    const connector = this.connectors.find(
      (item) => item.connectorKey === this.sourceForm.value.connectorKey
    );
	if (!connector) {
		this.notification.warning('Connector unavailable', 'Refresh the connector catalog and select an available connector.');
		return;
	}
	if (!this.connectorCanConnect(connector)) {
		this.notification.warning(
		  'Connector setup required',
		  this.safeOperationalText(connector.statusReason) || 'This connector is not available until its required configuration is complete.'
		);
		return;
	}
	this.connecting = true;
    this.operationError = '';
    this.sourceService
      .createSource({
        connectorKey: this.sourceForm.value.connectorKey,
        name: this.sourceForm.value.name,
        category: connector?.category,
        enabled: true,
        localOnly: this.sourceForm.value.localOnly,
        syncFrequency: this.sourceForm.value.syncFrequency,
        syncTarget: this.sourceForm.value.syncTarget,
        defaultProjectKey: this.sourceForm.value.defaultProjectKey,
        permissions: this.sourceForm.value.connectorKey === 'whisper-audio'
          ? ['metadata:read', 'audio:read', 'selected-audio-folder-read', 'explicit-consent']
          : this.sourceForm.value.connectorKey === 'docling-documents'
            ? ['metadata:read', 'document:read', 'selected-document-folder-read', 'explicit-consent']
            : undefined,
        excludePatterns: String(this.sourceForm.value.excludePatterns || '')
          .split(',')
          .map((item) => item.trim())
          .filter(Boolean),
      })
		.pipe(
		  timeout(this.operationTimeoutMs),
		  finalize(() => (this.connecting = false))
      )
      .subscribe({
        next: (created) => {
          const reconcileAfterCreate = this.loading || this.sourcesLoaded || this.sourceListUnavailable();
          // The create response is authoritative. Cancel an initial broad refresh so a
          // slower pre-create response cannot overwrite the newly connected source.
          this.refreshSubscription?.unsubscribe();
          this.sources = [created, ...this.sources.filter((source) => source.id !== created.id)];
          this.selectedSourceId = created.id;
          this.rebuildSourceIndexes();
          this.applySourceDefaults(this.sources);
          this.updateSourceActions();
          this.changeDetector.detectChanges();
          this.notification.success('Source registered', 'Connection status is being refreshed. HAI will not treat it as verified until the check completes.');
          // The canceled pre-create request also owned the first source, health, and
          // sync-job load. Start a fresh authoritative read so those states cannot
          // remain permanently unknown after registration.
          if (reconcileAfterCreate) this.refresh();
        },
        error: (error) => {
          this.operationError = this.operationErrorMessage(error, 'Failed to connect source.');
          this.notification.error('Error', this.operationError);
        },
      });
  }

  sync(): void {
    if (this.importForm.invalid || this.syncing) {
      return;
    }
    this.syncing = true;
    this.operationError = '';
    this.sourceService
      .sync(this.importForm.value.sourceId, {
        mode: this.importForm.value.mode,
        items: [
          {
            externalId: this.importForm.value.externalId,
            title: this.importForm.value.title,
            content: this.importForm.value.content,
            sourceUri: this.importForm.value.sourceUri,
            projectKey: this.importForm.value.projectKey,
          },
        ],
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (result) => {
          this.syncing = false;
          this.notifySyncResult('Item sync', result);
          this.refresh();
        },
        error: (error) => {
          this.syncing = false;
          this.operationError = this.operationErrorMessage(error, 'The source could not be synchronized.');
          this.notification.error('Source sync failed', this.operationError);
        },
      });
  }

  setAction(action: SourceAction): void {
    this.selectedAction = action;
    if (action === 'graph' && !this.knowledgeGraph && !this.graphLoading) {
      this.loadKnowledgeGraph();
    }
  }

  private updateSourceActions(): void {
    this.sourceActions = [
      {
        id: 'connect',
        title: 'Connect source',
        detail: 'Register a governed connector.',
        icon: 'plus',
        metric: `${this.enabledSources().length} active`,
        tone: 'blue',
      },
      {
        id: 'folder',
        title: 'Scan folder',
        detail: 'Index allowlisted local files.',
        icon: 'folder-open',
        metric: `${this.localSourceCount()} local`,
        tone: 'green',
      },
      {
        id: 'gmail',
        title: 'Connect Google',
        detail: 'Read-only Gmail, Drive, Contacts, and Calendar sync.',
        icon: 'mail',
        metric: this.googleConnectorMetric(),
        tone: 'blue',
      },
      {
        id: 'whatsapp',
        title: 'Import WhatsApp',
        detail: 'Parse selected chat exports.',
        icon: 'message',
        metric: `${this.whatsappSources().length} sources`,
        tone: 'green',
      },
      {
        id: 'odoo',
        title: 'Model Odoo',
        detail: 'Wire HERP app domains.',
        icon: 'deployment-unit',
        metric: `${this.odooSources().length} sources`,
        tone: 'gold',
      },
      {
        id: 'import',
        title: 'Import item',
        detail: 'Add one source-backed record.',
        icon: 'file-add',
        metric: `${this.extractions.length} records`,
        tone: 'blue',
      },
      {
        id: 'search',
        title: 'Search context',
        detail: 'Find relevant extracted facts.',
        icon: 'search',
        metric: this.searchResult ? `${this.searchResult.usedContext.length} hits` : 'ready',
        tone: 'gold',
      },
      {
        id: 'graph',
        title: 'Inspect connections',
        detail: 'Review source-linked candidates.',
        icon: 'share-alt',
        metric: this.knowledgeGraph ? `${this.knowledgeGraph.entities.length} entities` : 'review',
        tone: 'blue',
      },
    ];
  }

  selectedSource(): IConnectedSource | undefined {
    return this.sources.find((source) => source.id === this.selectedSourceId) || this.sources[0];
  }

  selectSource(source: IConnectedSource): void {
    if (this.selectedSourceId && this.selectedSourceId !== source.id) this.closeSelectedSourceDisclosures();
    this.selectedSourceId = source.id;
    this.refreshConnectionHealth(source);
  }

  private closeSelectedSourceDisclosures(): void {
    const recordSections = ['source-permissions-filters', 'source-danger-zone'];
    recordSections.forEach((sectionId) => this.viewPreferences.setSection(this.moduleId, sectionId, false));
    this.disclosures?.forEach((disclosure) => {
      if (recordSections.includes(disclosure.sectionId)) disclosure.setOpen(false);
    });
  }

  sourceHealth(source?: IConnectedSource): ISourceConnectionHealth | undefined {
    return source ? this.connectionHealth[source.id] : undefined;
  }

  sourceHealthUnavailable(source?: IConnectedSource): boolean {
    return Boolean(source && this.connectionHealthUnavailable[source.id]);
  }

  sourceHealthChecking(source?: IConnectedSource): boolean {
    return Boolean(source && this.connectionHealthChecking[source.id]);
  }

  sourceListUnavailable(): boolean {
    return this.loadWarnings.includes('Connected sources');
  }

  sourceRegistryEmptyText(): string {
    if (this.sourceListUnavailable()) return 'Connected sources could not be loaded. Retry to confirm the current source state.';
    return this.sources.length ? 'No sources match these filters.' : 'No connected sources yet.';
  }

  sourceActivityEmptyText(): string {
    if (this.syncJobsUnavailable || !this.syncJobsLoaded || !this.auditLogsLoaded) {
      return 'Some source activity could not be loaded. Retry before treating this view as empty.';
    }
    return this.syncJobs.length || this.auditLogs.length
      ? 'No loaded activity records match these filters.'
      : 'No source activity has been recorded.';
  }

  sourceActivityCountSummary(): string {
    const jobs = this.syncJobsLoaded && !this.syncJobsUnavailable
      ? `${this.recentSyncJobs().length} of ${this.syncJobs.length} sync jobs`
      : this.loading ? 'Sync jobs loading' : 'Sync jobs unavailable';
    const audit = this.auditLogsLoaded
      ? `${this.recentAuditLogs().length} of ${this.auditLogs.length} audit events`
      : this.auditLogsLoading
        ? 'Audit history loading'
        : this.loadWarnings.includes('Audit history') ? 'Audit history unavailable' : 'Audit history not loaded';
    return `${jobs} · ${audit}`;
  }

  extractionScopeSummary(): string {
    if (this.extractionsLoading) return 'Loading recent extracted records and the API total.';
    if (!this.extractionsLoaded) return 'Extracted record counts are unavailable.';
    return `Showing up to 8 matching records from the ${this.extractions.length} most recent records loaded; the API reports ${this.extractionTotalCount} total.`;
  }

  extractionCountSummary(): string {
    if (this.extractionsLoading) return 'Loading extracted record counts…';
    if (!this.extractionsLoaded) return 'Extracted record counts unavailable';
    return `${this.filteredExtractions().length} shown · ${this.extractions.length} loaded · ${this.extractionTotalCount} total`;
  }

  extractionsEmptyText(): string {
    if (!this.extractionsLoaded) return 'Extracted records are not available yet.';
    if (this.extractions.length) return 'No loaded records match this search.';
    return this.extractionTotalCount > 0
      ? 'The API reports extracted records, but none were returned in the loaded page.'
      : 'No extracted records have been recorded.';
  }

  sourceCountMetric(): string {
    return this.sourcesLoaded && !this.sourceListUnavailable() ? String(this.enabledSources().length) : '—';
  }

  attentionSourceCountMetric(): string {
    if (!this.sourcesLoaded || this.sourceListUnavailable()
      || Object.values(this.connectionHealthChecking).some(Boolean)) return '—';
    return String(this.sourcesNeedingAttention().length);
  }

  unverifiedSourceMetric(): string {
    if (!this.sourcesLoaded || this.sourceListUnavailable()
      || Object.values(this.connectionHealthChecking).some(Boolean)) return '—';
    return String(this.unverifiedSourceCount());
  }

  connectedSourceMetric(): string {
    return this.sourceStateMetric('connected');
  }

  configuredSourceMetric(): string {
    const configured = this.sourceStateMetric('configured');
    if (configured === '—') return configured;
    return String(Number(configured) + Number(this.sourceStateMetric('modeled')) + Number(this.sourceStateMetric('local_only')));
  }

  syncingSourceMetric(): string {
    return this.sourceStateMetric('syncing');
  }

  private sourceStateMetric(status: string): string {
    if (!this.sourcesLoaded || this.sourceListUnavailable()
      || Object.values(this.connectionHealthChecking).some(Boolean)) return '—';
    return String(this.sources.filter((source) => this.sourceDisplayStatus(source) === status).length);
  }

  connectorCountMetric(): string {
    if (!this.connectorCatalogLoaded || this.connectorCatalogUnavailable) return '—';
    return `${this.operationalConnectorCount()} live · ${this.localOnlyConnectorCount()} local · ${this.modeledConnectorCount()} modeled`;
  }

  extractionCountMetric(): string {
    if (this.extractionsLoading) return '…';
    return this.extractionsLoaded ? String(this.extractionTotalCount) : '—';
  }

  sensitiveExtractionMetric(): string {
    return this.extractionsLoaded ? String(this.sensitiveExtractionCount()) : '—';
  }

  failedJobMetric(): string {
    return this.syncJobsLoaded && !this.syncJobsUnavailable ? String(this.failedJobCount()) : '—';
  }

  canAuthorizeGoogleSource(source: IConnectedSource): boolean {
    return source.enabled === true
      && !this.sourceIsRevoked(source)
      && Boolean(this.sourceHealth(source)?.configured)
      && this.googleAuthorizationRequired(source)
      && !this.sourceHealthUnavailable(source)
      && !this.sourceHealthChecking(source)
      && !this.sourceMutationInProgress(source)
      && !this.authorizing
      && this.connectorCanConnect(this.connectorFor(source));
  }

  isGoogleSource(source?: IConnectedSource): boolean {
    return source?.connectorKey === 'gmail'
      || source?.connectorKey === 'google-drive'
      || source?.connectorKey === 'google-contacts'
      || source?.connectorKey === 'google-calendar';
  }

  isTrelloSource(source?: IConnectedSource): boolean {
    return source?.connectorKey === 'trello';
  }

  googleAuthorizationRequired(source: IConnectedSource): boolean {
    const health = this.sourceHealth(source);
    return this.isGoogleSource(source)
      && !this.sourceIsRevoked(source)
      && Boolean(health?.configured && (health.requiresReconnect || !health.authorized));
  }

  sourceHealthSummary(source: IConnectedSource): string {
    const state = this.sourceDisplayStatus(source);
    const health = this.sourceHealth(source);
    const latestJob = this.manualSyncStatus(source) || this.latestJobFor(source);
    if (state === 'revoked') return 'This source has been revoked. Reconnect it before syncing again.';
    if (state === 'paused') return 'This source is paused. Resume it before syncing.';
    if (state === 'unavailable') {
      if (this.sourceHealthUnavailable(source)) return 'Connection and authorization status could not be checked. Refresh before changing access.';
      return this.safeOperationalText(health?.reason || this.connectorFor(source)?.statusReason)
        || this.sourceSyncUnavailableReason(source)
        || 'This source or its connector is currently unavailable. Review connector status before retrying.';
    }
    if (state === 'disconnected') {
      return this.safeOperationalText(health?.reason) || 'This source is disconnected. Restore its connection before syncing.';
    }
    if (state === 'checking') return this.isGoogleSource(source) ? 'Checking Google connection state.' : 'Checking the current connection state.';
    if (state === 'syncing') return this.safeOperationalText(latestJob?.message || health?.reason) || 'A sync job is queued or running. HAI will update this status when it finishes.';
    if (state === 'failed') return this.safeOperationalText(latestJob?.message || health?.reason) || 'The latest sync failed. Review its activity record before retrying.';
    if (state === 'partial_failure') return this.safeOperationalText(latestJob?.message || health?.reason) || 'The latest sync completed only partially. Review failed records before relying on its output.';
    if (state === 'stale') return this.safeOperationalText(health?.reason) || 'The last successful connection check is stale. Run a fresh sync to verify current access.';
    if (state === 'authorization required') return this.safeOperationalText(health?.reason) || 'The source is configured, but Google read-only authorization is required.';
    if (state === 'setup required') return this.safeOperationalText(health?.reason || this.connectorFor(source)?.statusReason) || 'Connector setup is incomplete. Review connector availability before connecting.';
    if (state === 'configured') return this.safeOperationalText(health?.reason) || 'Connector setup is present, but a successful connection has not yet been verified.';
    if (state === 'modeled') return this.safeOperationalText(this.connectorFor(source)?.statusReason) || 'Built-in domain modeling only. No live account connection is provided.';
    if (state === 'local_only') return this.safeOperationalText(this.connectorFor(source)?.statusReason) || 'Local intake only. No live account connection is provided.';
    if (state === 'connected') return this.safeOperationalText(health?.reason) || 'The latest connection check passed.';
    if (!health) return 'Connection health has not been returned. Refresh to verify current access; no status is assumed.';
    if (this.isGoogleSource(source)) {
      if (health.requiresReconnect) return this.safeOperationalText(health.reason) || 'Google read-only consent must be renewed.';
      if (!health.authorized) return this.safeOperationalText(health.reason) || 'Google read-only consent is required.';
      return `Authorization: ${this.statusText(health.status)}${health.reason ? ` · ${this.safeOperationalText(health.reason)}` : ''}`;
    }
    if (source.enabled === false) return 'Paused. No scheduled sync will run until this source is resumed.';
    const healthTone = this.statusTone(health.status);
    if (healthTone === 'bad' || healthTone === 'watch') return this.safeOperationalText(health.reason) || `Source status: ${this.statusText(health.status)}.`;
    return this.safeOperationalText(health.reason) || `${source.localOnly ? 'Local-only source' : 'Source'} · ${this.statusText(health.status)}`;
  }

  sourceHealthTone(source: IConnectedSource): string {
    const state = this.sourceDisplayStatus(source);
    if (state === 'failed' || state === 'partial_failure' || state === 'revoked' || state === 'disconnected') return 'bad';
    if (state === 'connected') return 'good';
    if (['checking', 'syncing', 'stale', 'configured', 'modeled', 'local_only', 'unavailable', 'setup required', 'authorization required'].includes(state)) return 'watch';
    return 'neutral';
  }

  sourcesNeedingAttention(): IConnectedSource[] {
    return this.sources.filter((source) => this.sourceNeedsAttention(source));
  }

  sourceNeedsAttention(source: IConnectedSource): boolean {
    return ['failed', 'partial_failure', 'stale', 'unavailable', 'disconnected', 'setup required', 'authorization required', 'revoked']
      .includes(this.sourceDisplayStatus(source));
  }

  unverifiedSourceCount(): number {
    return this.sources.filter((source) => source.enabled !== false
      && this.sourceDisplayStatus(source) === 'unverified').length;
  }

  sourceEnabledLabel(source: IConnectedSource): string {
    if (this.sourceIsRevoked(source)) return 'revoked';
    if (source.enabled === false) return 'paused';
    return source.enabled === true ? 'enabled' : 'state unknown';
  }

  sourceIsRevoked(source: IConnectedSource): boolean {
    return Boolean(source.revokedAt)
      || String(source.status || '').trim().toLowerCase() === 'revoked'
      || String(this.sourceHealth(source)?.status || '').trim().toLowerCase() === 'revoked';
  }

  sourceDisplayStatus(source: IConnectedSource): string {
    if (this.sourceIsRevoked(source)) return 'revoked';
    if (this.sourceHealthUnavailable(source)) return 'unavailable';
    const jobs = [this.currentSyncJob(source)].filter((job): job is ISourceSyncJob => Boolean(job));
    if (jobs.some((job) => this.syncIsInProgress(job.status))) return 'syncing';
    const jobFailures = jobs.map((job) => this.syncFailureStatus(job.status)).filter(Boolean);
    if (jobFailures.includes('partial_failure')) return 'partial_failure';
    if (jobFailures.includes('failed')) return 'failed';
    if (this.sourceHealthChecking(source)) return 'checking';
    const health = this.sourceHealth(source);
    const status = String(health?.status || source.status || '').toLowerCase();
    if (this.syncIsInProgress(status)) return 'syncing';
    const healthFailure = this.syncFailureStatus(status);
    if (healthFailure) return healthFailure;
    if (source.enabled === false) return 'paused';
    const connector = this.connectorFor(source);
    const adapterStatus = String(connector?.adapterStatus || '').toLowerCase();
    if (connector && (!connector.enabled || ['not_implemented', 'reconnect_required', 'unavailable', 'error'].includes(adapterStatus))) return 'unavailable';
    if (adapterStatus === 'configuration_required') return 'setup required';
    if (status === 'stale' || status === 'previously_verified') return 'stale';
    if (status === 'failed') return 'failed';
    if (status === 'disconnected') return 'disconnected';
    if (['not_implemented', 'error', 'unavailable'].includes(status)) return 'unavailable';
    if (['not_configured', 'configuration_required'].includes(status)) return 'setup required';
    if (health && this.isGoogleSource(source)) {
      if (health.requiresReconnect || !health.authorized) return health.configured ? 'authorization required' : 'setup required';
    }
    if (health?.configured && ['modeled', 'local_only'].includes(adapterStatus)) return adapterStatus;
    if (['ready', 'operational'].includes(status)
      && health?.configured
      && (!this.isGoogleSource(source) || health.authorized)) return 'connected';
    if (health?.configured) return 'configured';
    if (['configured', 'configuration_ready'].includes(status)) return 'configured';
    if (health?.status) return this.statusTone(status) === 'bad' ? 'unavailable' : 'unverified';
    return this.statusTone(source.status) === 'bad' ? 'unavailable' : 'unverified';
  }

  authorizationSources(): IConnectedSource[] {
    return this.sources.filter((source) => this.googleAuthorizationRequired(source));
  }

  failedSyncSources(): IConnectedSource[] {
    return this.sources.filter((source) => this.sourceHasSyncFailure(source));
  }

  private syncFailureStatus(status?: string): 'failed' | 'partial_failure' | undefined {
    const normalized = String(status || '').trim().toLowerCase();
    if (normalized === 'failed' || normalized === 'sync_failed' || normalized === 'sync_cancelled' || normalized === 'cancelled') return 'failed';
    if (normalized === 'partial_failure' || normalized === 'sync_partial_failure') return 'partial_failure';
    return undefined;
  }

  private syncIsInProgress(status?: string): boolean {
    return inProgressSyncStatuses.has(String(status || '').trim().toLowerCase());
  }

  private sourceHasSyncFailure(source: IConnectedSource): boolean {
    return Boolean(this.syncFailureStatus(this.currentSyncJob(source)?.status)
      || this.syncFailureStatus(this.sourceHealth(source)?.status));
  }

  scheduledSourceCount(): number {
    return this.enabledSources().filter((source) =>
      !manualSyncValues.has(String(source.syncFrequency || '').trim().toLowerCase())
    ).length;
  }

  quickGoogleConnectors(): ISourceConnector[] {
    const googleKeys = new Set(['gmail', 'google-drive', 'google-contacts', 'google-calendar']);
    const existingKeys = new Set(this.sources.map((source) => source.connectorKey));
    return this.connectors
      .filter((connector) => googleKeys.has(connector.connectorKey)
        && this.connectorCanConnect(connector)
        && !existingKeys.has(connector.connectorKey))
      .slice(0, 2);
  }

  hasBasicActions(): boolean {
    return this.scheduledSourceCount() > 0
      || this.failedSyncSources().some((source) => this.syncButtonVisible(source))
      || this.authorizationSources().length > 0
      || this.quickGoogleConnectors().length > 0
      || this.sourcesNeedingAttention().length > 0;
  }

  hasTrelloSources(): boolean {
    return this.sources.some((source) => this.isTrelloSource(source));
  }

  refreshSourceHealth(source: IConnectedSource): void {
    this.refreshConnectionHealth(source);
  }

  connectGoogleConnector(connectorKey: string): void {
    switch (connectorKey) {
      case 'gmail':
        this.connectGmail();
        break;
      case 'google-drive':
        this.connectGoogleDrive();
        break;
      case 'google-contacts':
        this.connectGoogleContacts();
        break;
      case 'google-calendar':
        this.connectGoogleCalendar();
        break;
    }
  }

  filteredSources(): IConnectedSource[] {
    const query = this.sourceFilterQuery.trim().toLowerCase();
    return this.sources.filter((source) => {
      const enabledMatches = this.sourceEnabledFilter === 'all'
        || (this.sourceEnabledFilter === 'enabled'
          ? source.enabled === true
          : source.enabled === false);
      const queryMatches = !query || [
        source.name, source.connectorKey, source.category, source.status,
        source.syncTarget, source.defaultProjectKey,
      ].some((value) => String(value || '').toLowerCase().includes(query));
      return enabledMatches && queryMatches;
    });
  }

  enabledSources(): IConnectedSource[] {
    return this.sources.filter((source) => source.enabled === true && !this.sourceIsRevoked(source));
  }

  localSourceCount(): number {
    return this.sources.filter((source) => source.localOnly).length;
  }

  // "operational" now means a live remote adapter only (GitHub, JSON feed). The
  // local-file readers and the modeled connector are counted separately, so the
  // dashboard stops presenting a local-folder reader as a live cloud connector.
  operationalConnectorCount(): number {
    return this.connectorCountByStatus('operational');
  }

  localOnlyConnectorCount(): number {
    return this.connectorCountByStatus('local_only');
  }

  modeledConnectorCount(): number {
    return this.connectorCountByStatus('modeled');
  }

  googleConnectorMetric(): string {
    const googleConnectors = this.connectors.filter(
      (connector) => connector.connectorKey === 'gmail'
        || connector.connectorKey === 'google-drive'
        || connector.connectorKey === 'google-contacts'
        || connector.connectorKey === 'google-calendar'
    );
    const ready = googleConnectors.filter(
      (connector) => connector.enabled && connector.adapterStatus === 'operational'
    ).length;
    if (ready === googleConnectors.length && ready > 0) {
      return `${ready} ready`;
    }
    return ready > 0 ? `${ready}/${googleConnectors.length} ready` : 'setup needed';
  }

  private connectorCountByStatus(status: string): number {
    return this.connectors.filter(
      (connector) => connector.enabled && connector.adapterStatus === status
    ).length;
  }

  // Human-readable label for an adapter status, so the UI does not surface raw
  // enum values and does not overstate what a connector does.
  adapterStatusLabel(status?: string): string {
    switch ((status || '').toLowerCase()) {
      case 'operational':
        return 'live';
      case 'local_only':
        return 'local files only';
      case 'modeled':
        return 'built-in model';
	  case 'configuration_required':
		return 'setup required';
      case 'not_implemented':
		return 'unavailable';
      default:
        return this.statusText(status);
    }
  }

  failedJobCount(): number {
    return this.syncJobs.filter((job) => Boolean(this.syncFailureStatus(job.status))).length;
  }

  pendingJobCount(): number {
    return this.syncJobs.filter((job) => job.status === 'pending' || job.status === 'queued' || job.status === 'running').length;
  }

  uncertainExtractionCount(): number {
    return this.extractions.filter((extraction) => extraction.uncertain).length;
  }

  sensitiveExtractionCount(): number {
    return this.extractions.filter((extraction) => extraction.sensitive).length;
  }

  recentExtractions(): ISourceExtraction[] {
    return this.extractions.slice(0, 8);
  }

  filteredExtractions(): ISourceExtraction[] {
    const query = this.extractionFilterQuery.trim().toLowerCase();
    return this.extractions
      .filter((extraction) => !query || [
        extraction.id, extraction.sourceId, extraction.rawItemId, extraction.projectKey,
        extraction.contentType, extraction.text, extraction.summary, extraction.entities,
        extraction.dates, extraction.tasks, extraction.decisions, extraction.followUps,
        extraction.sourceUri, extraction.sourceLabel,
      ].some((value) => String(value || '').toLowerCase().includes(query)))
      .slice(0, 8);
  }

  recentAuditLogs(): ISourceAuditLog[] {
    return this.auditLogs.slice(0, 8);
  }

  recentSyncJobs(): ISourceSyncJob[] {
    return this.syncJobs.slice(0, 6);
  }

  filteredSyncJobs(): ISourceSyncJob[] {
    if (this.activityTypeFilter === 'audit') return [];
    const query = this.activityFilterQuery.trim().toLowerCase();
    return this.recentSyncJobs().filter((job) =>
      (this.activityTypeFilter !== 'failed' || Boolean(this.syncFailureStatus(job.status)))
      && (!query || [job.id, job.sourceId, job.mode, job.status, job.message]
        .some((value) => String(value || '').toLowerCase().includes(query)))
    );
  }

  filteredAuditLogs(): ISourceAuditLog[] {
    if (this.activityTypeFilter === 'sync' || this.activityTypeFilter === 'failed') return [];
    const query = this.activityFilterQuery.trim().toLowerCase();
    return this.recentAuditLogs().filter((log) => !query || [
      log.id, log.sourceId, log.action, log.message,
    ].some((value) => String(value || '').toLowerCase().includes(query)));
  }

  hasFilteredActivity(): boolean {
    return this.filteredSyncJobs().length > 0 || this.filteredAuditLogs().length > 0;
  }

  pursuitRoutingOutcomes(): ISourcePursuitRoutingOutcome[] {
    return this.lastSyncResult?.pursuitOutcomes || [];
  }

  pursuitRoutingLabel(outcome: ISourcePursuitRoutingOutcome): string {
    switch (outcome.status) {
      case 'candidate_pending':
        return 'Decision needed';
      case 'pursuit_linked':
        return 'Pursuit linked';
      case 'pursuit_routed':
        return 'Governed workflow routed';
      case 'routing_deferred':
        return 'Routing needs repair';
      default:
        return 'Workflow created';
    }
  }

  openPursuitOutcome(outcome: ISourcePursuitRoutingOutcome): void {
    this.router.navigate(['/pursuits'], {
      queryParams: outcome.pursuitId ? { selected: outcome.pursuitId } : undefined,
    });
  }

  latestJobFor(source: IConnectedSource): ISourceSyncJob | undefined {
    return this.latestJobsBySource[source.id];
  }

  statusText(status?: string): string {
    return (status || 'unknown').replace(/_/g, ' ');
  }

  statusTone(status?: string): string {
    switch ((status || '').toLowerCase()) {
      case 'ready':
      case 'active':
      case 'completed':
      case 'operational':
      case 'connected':
        return 'good';
      case 'previously_verified':
      case 'stale':
      case 'configured':
      case 'syncing':
      case 'sync_queued':
      case 'sync_running':
      case 'unavailable':
      case 'setup required':
      case 'authorization required':
        return 'watch';
      case 'paused':
      case 'running':
      case 'pending':
      case 'configuration_ready':
      case 'not_configured':
      case 'authorization_required':
      case 'unauthorized':
      case 'local_only':
      case 'modeled':
        return 'watch';
      case 'configuration_required':
      case 'disconnected':
      case 'reconnect_required':
      case 'not_implemented':
      case 'failed':
      case 'sync_failed':
      case 'sync_cancelled':
      case 'partial_failure':
      case 'sync_partial_failure':
      case 'revoked':
      case 'error':
        return 'bad';
      default:
        return 'neutral';
    }
  }

  syncButtonVisible(source: IConnectedSource): boolean {
    return source.enabled === true && !this.sourceIsRevoked(source);
  }

  sourceSyncUnavailableReason(source: IConnectedSource): string {
    if (this.sourceIsRevoked(source)) return 'This source was revoked and cannot be synced.';
    if (source.enabled !== true) return 'This source is paused. Resume it before syncing.';
    if (this.sourceMutationInProgress(source)) return 'A source setting is being updated. Wait for it to finish before syncing.';
    const retryDelay = this.manualSyncRetryDelaySeconds(source);
    if (retryDelay > 0) return `The connector rate-limited this request. Retry is available in ${retryDelay} second${retryDelay === 1 ? '' : 's'}.`;
    const job = this.currentSyncJob(source);
    if (this.syncIsInProgress(job?.status)) {
      return 'A sync job is already queued or running for this source.';
    }
    if (this.connectorCatalogUnavailable) return 'Connector availability could not be loaded. Refresh before syncing.';
    if (this.connectorCatalogLoading || !this.connectorCatalogLoaded) return 'Connector availability is still being checked. Refresh before syncing.';
    if (this.syncing || this.manualSyncSubmittingSourceId) return 'Another source operation is running. Wait for it to finish before starting another sync.';
    const connector = this.connectorFor(source);
    if (!connector) return 'This connector is missing from the current catalog. Refresh the connector list before syncing.';
    if (!connector.enabled) return this.safeOperationalText(connector.statusReason) || 'This connector is disabled and cannot sync.';
    const adapterStatus = String(connector.adapterStatus || '').trim().toLowerCase();
    if (adapterStatus === 'configuration_required') {
      return this.safeOperationalText(connector.statusReason) || 'Connector setup is required before syncing.';
    }
    if (!['operational', 'local_only', 'modeled'].includes(adapterStatus)) {
      return this.safeOperationalText(connector.statusReason) || 'This connector is not available for sync yet.';
    }
    const health = this.sourceHealth(source);
    const healthStatus = String(health?.status || '').toLowerCase();
    if (this.sourceHealthUnavailable(source)) return 'Connection health could not be checked. Retry the access check before syncing.';
    if (this.sourceHealthChecking(source)) return 'Connection health is still being checked.';
    if (health && (!health.configured || ['not_configured', 'configuration_required'].includes(healthStatus))) {
      return this.safeOperationalText(health.reason) || 'Connector setup is incomplete. Review source details before syncing.';
    }
    if (this.syncIsInProgress(healthStatus)) {
      return healthStatus.includes('queued')
        ? 'A sync job is already queued for this source.'
        : 'A sync job is already running for this source.';
    }
    if (['disconnected', 'not_implemented', 'error', 'unavailable', 'revoked'].includes(healthStatus)) {
      return this.safeOperationalText(health?.reason)
        || (healthStatus === 'disconnected'
          ? 'This source is disconnected. Restore its connection before syncing.'
          : 'The connection is unavailable. Check access before syncing.');
    }
    if (this.isGoogleSource(source)) {
      if (this.sourceHealthUnavailable(source)) return 'Google access could not be checked. Refresh health before syncing.';
      if (this.sourceHealthChecking(source)) return 'Google access is still being checked.';
      if (!health) return 'Google access is unverified. Check access before syncing.';
      if (!health.configured) return this.safeOperationalText(health.reason) || 'Google connector setup is incomplete.';
      if (!health.authorized || health.requiresReconnect) return this.safeOperationalText(health.reason) || 'Authorize Google before syncing.';
    }
    return '';
  }

  sourceSyncCanRun(source: IConnectedSource): boolean {
    return this.sourceSyncUnavailableReason(source) === '' && !this.syncing && !this.manualSyncSubmittingSourceId;
  }

  safeOperationalText(value?: string): string {
    return String(value || '')
      .replace(/[\r\n\t]+/g, ' ')
      .replace(/https?:\/\/[^/\s:@]+(?::[^/\s@]*)?@/gi, '[redacted-url]@')
      .replace(/\bBearer\s+[A-Za-z0-9._~+/-]+=*/gi, 'Bearer [redacted]')
      .replace(/\b(access_token|refresh_token|id_token|token|api[_-]?key|client[_-]?secret|password|secret|authorization|cookie|set-cookie|code)\b(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&]+)/gi, '$1$2[redacted]')
      .replace(/\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b/g, '[redacted-token]')
      .trim();
  }

  sourceExtractionCount(source: IConnectedSource): number {
    return this.sourceExtractionCounts[source.id] || 0;
  }

  connectorFor(source?: IConnectedSource): ISourceConnector | undefined {
    if (!source) {
      return undefined;
    }
    return this.connectors.find((connector) => connector.connectorKey === source.connectorKey);
  }

  sourcePermissions(source?: IConnectedSource): string[] {
    if (!source?.permissions) {
      return [];
    }
    return String(source.permissions)
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
  }

  sourceExcludePatterns(source?: IConnectedSource): string[] {
    if (!source?.excludePatterns) {
      return [];
    }
    return String(source.excludePatterns)
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
  }

  toggleTheme(): void {
    this.themeMode = this.themeService.toggle();
  }

  themeLabel(): string {
    return this.themeService.label();
  }

  themeIcon(): string {
    return this.themeService.icon();
  }

  connectorLabel(connector: ISourceConnector): string {
    const status = connector.adapterStatus || (connector.enabled ? 'operational' : 'not_implemented');
    return `${connector.name} — ${this.adapterStatusLabel(status)}`;
  }

  connectorsForAction(connectorKey: string): ISourceConnector | undefined {
    return this.connectors.find((connector) => connector.connectorKey === connectorKey);
  }

  connectorActionUnavailableReason(connectorKey: string): string {
    const connector = this.connectorsForAction(connectorKey);
    return this.connectorCanConnect(connector) ? '' : this.safeOperationalText(connector?.statusReason) || 'Connector capability is unavailable. Refresh the catalog and review its setup.';
  }

  connectorCanConnect(connector?: ISourceConnector): boolean {
		if (!connector?.enabled) {
			return false;
		}
		switch ((connector.adapterStatus || '').trim().toLowerCase()) {
		  case 'operational':
		  case 'local_only':
		  case 'modeled':
			return true;
		  default:
			return false;
		}
	}

  selectedConnectorReadinessMessage(): string {
    const connector = this.connectors.find((item) => item.connectorKey === this.sourceForm.value.connectorKey);
    if (!connector || this.connectorCanConnect(connector)) return '';
    const status = this.adapterStatusLabel(connector.adapterStatus || (connector.enabled ? 'operational' : 'not_implemented'));
    const reason = this.safeOperationalText(connector.statusReason);
    return `${connector.name} is ${status}. ${reason ? `${reason} ` : ''}HAI will keep source creation disabled until this connector is ready.`;
  }

  googleConnectorCanConnect(connectorKey: 'gmail' | 'google-drive' | 'google-contacts' | 'google-calendar'): boolean {
    return this.connectorCanConnect(this.connectors.find((connector) => connector.connectorKey === connectorKey));
  }

	selectedConnectorCanConnect(): boolean {
		return this.connectorCanConnect(this.connectors.find(
		  (connector) => connector.connectorKey === this.sourceForm.value.connectorKey
		));
	}

  sourceCanConnect(): boolean {
    const connectorKey = String(this.sourceForm.value.connectorKey || '').trim();
    const syncTarget = String(this.sourceForm.value.syncTarget || '').trim();
    return this.sourceForm.valid
      && this.selectedConnectorCanConnect()
      && (this.syncFrequencyIsManual() || this.selectedConnectorSupportsScheduledSync())
      && (!this.connectorRequiresSelectedFolder(connectorKey) || syncTarget.length > 0);
  }

  selectedConnectorSupportsScheduledSync(): boolean {
    return this.connectorSupportsScheduledSync(
      this.connectors.find((connector) => connector.connectorKey === this.sourceForm.value.connectorKey)
    );
  }

  private connectorSupportsScheduledSync(connector?: ISourceConnector): boolean {
    const supportedModes = String(connector?.supportedModes || '')
      .split(',')
      .map((mode) => mode.trim());
    // Older gateways did not expose supportedModes. Keep the form compatible
    // with them; the API remains the authority and rejects unsafe schedules.
    return supportedModes.length === 1 && !supportedModes[0]
      ? true
      : supportedModes.includes('scheduled_sync');
  }

  private syncFrequencyIsManual(): boolean {
    return ['', 'manual', 'off', 'disabled', 'none'].includes(
      String(this.sourceForm.value.syncFrequency || '').trim().toLowerCase()
    );
  }

  private connectorRequiresSelectedFolder(connectorKey: string): boolean {
    return ['local-folder', 'email', 'calendar', 'cloud-documents', 'project-board'].includes(connectorKey);
  }

  connectorChanged(connectorKey: string): void {
    const selectedConnectorKey = String(connectorKey || '').trim();
    if (!selectedConnectorKey || selectedConnectorKey === this.lastSelectedConnectorKey) {
      return;
    }
    this.lastSelectedConnectorKey = selectedConnectorKey;

    if (selectedConnectorKey === 'json-feed') {
      this.sourceForm.patchValue({
        name: 'Local account JSON bridge',
        syncFrequency: '15m',
        syncTarget: 'http://host.docker.internal:8787/feed',
        localOnly: true,
      });
      return;
    }
    if (selectedConnectorKey === 'local-folder') {
      this.sourceForm.patchValue({
        name: 'Selected local folder',
        syncFrequency: 'manual',
        syncTarget: '',
        localOnly: true,
      });
      return;
    }
    if (selectedConnectorKey === 'email') {
      this.sourceForm.patchValue({
        name: 'Email export folder',
        syncFrequency: 'manual',
        syncTarget: 'email',
        localOnly: true,
        excludePatterns: 'spam,trash',
      });
      return;
    }
    if (selectedConnectorKey === 'calendar') {
      this.sourceForm.patchValue({
        name: 'Calendar export folder',
        syncFrequency: 'manual',
        syncTarget: 'calendar',
        localOnly: true,
        excludePatterns: 'cancelled,spam',
      });
      return;
    }
    if (selectedConnectorKey === 'cloud-documents') {
      this.sourceForm.patchValue({
        name: 'Synced document folder',
        syncFrequency: '15m',
        syncTarget: 'documents',
        localOnly: true,
        excludePatterns: 'trash,temp,cache',
      });
      return;
    }
    if (selectedConnectorKey === 'project-board') {
      this.sourceForm.patchValue({
        name: 'Trello board exports',
        syncFrequency: 'manual',
        syncTarget: 'trello',
        localOnly: true,
        excludePatterns: 'archive,template',
      });
      return;
    }
    if (selectedConnectorKey === 'trello') {
      this.sourceForm.patchValue({
        name: 'Trello board (read-only)',
        syncFrequency: '1h',
        syncTarget: '',
        defaultProjectKey: '',
        localOnly: false,
        excludePatterns: 'archive,template',
      });
      return;
    }
    if (selectedConnectorKey === 'github') {
      this.sourceForm.patchValue({
        name: 'GitHub repository',
        syncFrequency: '1h',
        syncTarget: 'Noodzakelijk-Online/018-HAI',
        localOnly: false,
        excludePatterns: '',
      });
      return;
    }
    if (selectedConnectorKey === 'whatsapp-export') {
      this.sourceForm.patchValue({
        name: 'WhatsApp exported chats',
        syncFrequency: 'manual',
        syncTarget: 'whatsapp',
        defaultProjectKey: 'Robert-life-os',
        localOnly: true,
        excludePatterns: 'media omitted,omitted,spam',
      });
      return;
    }
    if (selectedConnectorKey === 'whisper-audio') {
      this.sourceForm.patchValue({
        name: 'Selected voice-note folder',
        syncFrequency: 'manual',
        syncTarget: 'voice-notes',
        defaultProjectKey: 'Robert-life-os',
        localOnly: true,
        excludePatterns: 'private,do-not-transcribe',
      });
      return;
    }
    if (selectedConnectorKey === 'docling-documents') {
      this.sourceForm.patchValue({
        name: 'Selected document evidence folder',
        syncFrequency: 'manual',
        syncTarget: 'documents',
        defaultProjectKey: 'Robert-life-os',
        localOnly: true,
        excludePatterns: 'private,do-not-extract',
      });
      return;
    }
    if (selectedConnectorKey === 'odoo-herp') {
      this.sourceForm.patchValue({
        name: 'Odoo / HERP workspace',
        syncFrequency: 'manual',
        syncTarget: this.odooSyncTarget(),
        defaultProjectKey: 'Robert-life-os',
        localOnly: true,
        excludePatterns: 'password,secret,token,private',
      });
    }
  }

  syncTargetPlaceholder(): string {
    if (this.sourceForm.value.connectorKey === 'json-feed') {
      return 'Allowlisted HTTP(S) JSON feed URL';
    }
    if (this.sourceForm.value.connectorKey === 'whatsapp-export') {
      return 'Folder under connected-source root, e.g. whatsapp';
    }
    if (this.sourceForm.value.connectorKey === 'whisper-audio') {
      return 'Explicit audio subfolder, e.g. voice-notes/2026-07';
    }
    if (this.sourceForm.value.connectorKey === 'docling-documents') {
      return 'Explicit document subfolder, e.g. legal/vivare';
    }
    if (this.sourceForm.value.connectorKey === 'email') {
      return 'Folder under connected-source root containing .mbox or .eml exports';
    }
    if (this.sourceForm.value.connectorKey === 'calendar') {
      return 'Folder under connected-source root containing .ics exports';
    }
    if (this.sourceForm.value.connectorKey === 'cloud-documents') {
      return 'Synced folder under connected-source root, e.g. documents';
    }
    if (this.sourceForm.value.connectorKey === 'project-board') {
      return 'Folder under connected-source root containing Trello .json exports';
    }
    if (this.sourceForm.value.connectorKey === 'github') {
      return 'GitHub owner/repository, e.g. Noodzakelijk-Online/018-HAI';
    }
    if (this.sourceForm.value.connectorKey === 'trello') {
      return 'Trello board URL or ID, e.g. https://trello.com/b/abc12345/board-name';
    }
    if (this.sourceForm.value.connectorKey === 'odoo-herp') {
      return 'Odoo URL or app list, e.g. https://.../odoo?apps=CRM,Sales';
    }
    return 'Selected folder under connected-source root, e.g. projects/018-hai';
  }

  syncSource(source: IConnectedSource): void {
    if (this.syncing || this.manualSyncSubmittingSourceId) {
      return;
    }
    const unavailableReason = this.sourceSyncUnavailableReason(source);
    if (unavailableReason) {
      this.notification.warning('Sync unavailable', unavailableReason);
      return;
    }
    if (source.connectorKey === 'whisper-audio') {
      this.transcribeSource(source);
      return;
    }
    if (source.connectorKey === 'docling-documents') {
      this.extractDocuments(source);
      return;
    }
    this.syncing = true;
    this.manualSyncSubmittingSourceId = source.id;
    this.operationError = '';
    const idempotencyKey = this.manualSyncIdempotencyKey(source.id);
    this.sourceService
      .submitManualSync(source.id, {}, idempotencyKey)
      .pipe(timeout(this.manualSyncAcceptanceTimeoutMs), tap((job) => {
        if (!this.isManualSyncJob(job, source.id)) throw new Error('Invalid sync acknowledgement');
      }), finalize(() => {
        this.syncing = false;
        this.manualSyncSubmittingSourceId = '';
      }))
      .subscribe({
        next: (job) => {
          this.clearManualSyncIdempotencyKey(source.id);
          delete this.manualSyncRecoveryMessages[source.id];
          delete this.manualSyncRecoveryActions[source.id];
          delete this.manualSyncStatusUnavailable[source.id];
          this.acceptManualSyncJob(job);
        },
        error: (error) => {
          const status = Number((error as { status?: number })?.status || 0);
            const message = this.operationErrorMessage(error, status === 429
              ? 'The connector rate limit was reached.'
              : 'HAI could not confirm whether the request was accepted.');
          if (status === 429) {
            const retryAt = this.retryAfterTimestamp(error);
            if (retryAt && retryAt > Date.now()) this.setManualSyncRetryNotBefore(source.id, retryAt);
            const connectorName = this.safeOperationalText(this.connectorFor(source)?.name) || 'The connector';
            const retryDetail = retryAt && retryAt > Date.now()
              ? ` Retry in ${this.manualSyncRetryDelaySeconds(source)} seconds, after ${new Date(retryAt).toLocaleTimeString()}.`
              : ' Wait briefly before trying again.';
            this.manualSyncRecoveryMessages[source.id] = `${connectorName} rate-limited this sync request. ${message}${retryDetail} HAI kept the request key, so a safe retry will not create a duplicate job if the request was accepted.`;
            this.manualSyncRecoveryActions[source.id] = 'retry';
            this.notification.warning('Source rate-limited', this.manualSyncRecoveryMessages[source.id]);
            return;
          }
          if (status === 0 || status === 408 || status >= 500 || (error as { name?: string })?.name === 'TimeoutError') {
            this.manualSyncRecoveryMessages[source.id] = 'The acknowledgement was not confirmed. Retry safely with the same request key; HAI will return the existing job if it already accepted this sync.';
            this.manualSyncRecoveryActions[source.id] = 'retry';
            this.notification.warning('Sync request not confirmed', this.manualSyncRecoveryMessages[source.id]);
            return;
          }
          this.clearManualSyncIdempotencyKey(source.id);
          this.manualSyncRecoveryMessages[source.id] = message;
          if (status === 409) {
            this.manualSyncRecoveryActions[source.id] = 'refresh';
            this.notification.warning('Sync needs attention', message);
            this.refresh();
          } else {
            this.manualSyncRecoveryActions[source.id] = 'review';
            this.notification.error('Sync could not be queued', message);
          }
        },
      });
  }

  manualSyncRateLimited(source: IConnectedSource): boolean {
    return this.manualSyncRetryDelaySeconds(source) > 0;
  }

  private manualSyncRetryDelaySeconds(source: IConnectedSource): number {
    this.restoreManualSyncRetryNotBefore(source.id);
    const retryAt = this.manualSyncRetryNotBefore[source.id];
    return retryAt ? Math.max(0, Math.ceil((retryAt - Date.now()) / 1000)) : 0;
  }

  private restoreManualSyncRetryNotBefore(sourceId: string): void {
    if (this.manualSyncRetryNotBefore[sourceId]) return;
    const storageKey = this.manualSyncRetrySessionPrefix + sourceId;
    try {
      const stored = sessionStorage.getItem(storageKey);
      if (stored === null) return;
      const retryAt = Number(stored);
      if (!Number.isSafeInteger(retryAt) || retryAt <= Date.now()) {
        sessionStorage.removeItem(storageKey);
        return;
      }
      this.setManualSyncRetryNotBefore(sourceId, retryAt, false);
    } catch {
      // The in-memory gate remains authoritative when browser storage is unavailable.
    }
  }

  private retryAfterTimestamp(error: unknown): number | undefined {
    const headers = (error as { headers?: { get?: (name: string) => string | null } })?.headers;
    const value = headers?.get?.('Retry-After')?.trim();
    if (!value) return undefined;
    const seconds = Number(value);
    if (Number.isFinite(seconds) && seconds >= 0) return Date.now() + seconds * 1000;
    const date = Date.parse(value);
    return Number.isFinite(date) ? date : undefined;
  }

  private setManualSyncRetryNotBefore(sourceId: string, retryAt: number, persist = true): void {
    this.manualSyncRetryTimers.get(sourceId)?.unsubscribe();
    this.manualSyncRetryNotBefore = { ...this.manualSyncRetryNotBefore, [sourceId]: retryAt };
    if (persist) {
      try {
        sessionStorage.setItem(this.manualSyncRetrySessionPrefix + sourceId, String(retryAt));
      } catch {
        // Keep the current-tab retry gate even when session storage is unavailable.
      }
    }
    const scheduleClear = (): void => {
      const delay = Math.max(0, retryAt - Date.now());
      const subscription = timer(Math.min(delay, 2147480000)).subscribe(() => {
        if (Date.now() >= retryAt) {
          if (this.manualSyncRetryNotBefore[sourceId] === retryAt) {
            this.clearManualSyncRetryNotBefore(sourceId);
            this.changeDetector.detectChanges();
          }
          return;
        }
        if (this.manualSyncRetryNotBefore[sourceId] === retryAt) scheduleClear();
      });
      this.manualSyncRetryTimers.set(sourceId, subscription);
    };
    scheduleClear();
  }

  private clearManualSyncRetryNotBefore(sourceId: string): void {
    this.manualSyncRetryTimers.get(sourceId)?.unsubscribe();
    this.manualSyncRetryTimers.delete(sourceId);
    const { [sourceId]: _retryAt, ...remaining } = this.manualSyncRetryNotBefore;
    this.manualSyncRetryNotBefore = remaining;
    try {
      sessionStorage.removeItem(this.manualSyncRetrySessionPrefix + sourceId);
    } catch {
      // A stale stored deadline is discarded on its next read if cleanup fails.
    }
  }

  retryManualSyncSafely(source: IConnectedSource): void {
    this.syncSource(source);
  }

  recoverManualSyncRequest(source: IConnectedSource): void {
    const action = this.manualSyncRecoveryActions[source.id];
    if (action === 'retry') {
      this.retryManualSyncSafely(source);
    } else if (action === 'refresh') {
      this.refresh();
    } else {
      this.openAdvancedAction('connect', 'source-inspector', source.id);
    }
  }

  checkManualSyncStatus(source: IConnectedSource): void {
    const job = this.manualSyncStatus(source);
    if (!job) {
      this.refresh();
      return;
    }
    delete this.manualSyncStatusUnavailable[source.id];
    this.startManualSyncPolling(source.id, job.id, 0, 0);
  }

  manualSyncStatus(source: IConnectedSource): ISourceManualSyncJob | undefined {
    const current = this.currentSyncJob(source);
    return current?.mode === 'manual_async_sync' ? current as ISourceManualSyncJob : undefined;
  }

  private currentSyncJob(source: IConnectedSource): ISourceSyncJob | undefined {
    const current = this.manualSyncJobsBySource[source.id];
    const latest = this.latestJobFor(source);
    if (!current) return latest;
    if (!latest) return current;
    if (current.id === latest.id) {
      // A refreshed terminal history record outranks a stale in-flight poll.
      return this.syncIsInProgress(current.status) && !this.syncIsInProgress(latest.status) ? latest : current;
    }
    const currentTime = Date.parse(current.createdAt || current.startedAt || '');
    const latestTime = Date.parse(latest.createdAt || latest.startedAt || '');
    return Number.isFinite(latestTime) && (!Number.isFinite(currentTime) || latestTime > currentTime) ? latest : current;
  }

  private isManualSyncJob(value: unknown, sourceId: string, jobId?: string): value is ISourceManualSyncJob {
    if (!value || typeof value !== 'object') return false;
    const job = value as ISourceManualSyncJob;
    return typeof job.id === 'string' && Boolean(job.id.trim())
      && (!jobId || job.id === jobId)
      && job.sourceId === sourceId
      && job.mode === 'manual_async_sync'
      && ['queued', 'running', 'completed', 'failed', 'partial_failure', 'cancelled'].includes(job.status)
      && typeof job.createdAt === 'string' && Number.isFinite(Date.parse(job.createdAt))
      && [job.itemsSeen, job.itemsAdded, job.itemsUpdated, job.itemsFailed, job.attempt, job.maxAttempts]
        .every((count) => Number.isSafeInteger(count) && count >= 0)
      && job.maxAttempts > 0;
  }

  private manualSyncIdempotencyKey(sourceId: string): string {
    const storageKey = this.manualSyncSessionPrefix + sourceId;
    try {
      const existing = sessionStorage.getItem(storageKey);
      if (existing) return existing;
      const randomUUID = globalThis.crypto?.randomUUID?.();
      const generated = randomUUID || `hai-${Date.now()}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
      sessionStorage.setItem(storageKey, generated);
      return generated;
    } catch {
      const fallback = this.manualSyncFallbackKeys.get(sourceId);
      if (fallback) return fallback;
      const generated = `hai-${Date.now()}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
      this.manualSyncFallbackKeys.set(sourceId, generated);
      return generated;
    }
  }

  private clearManualSyncIdempotencyKey(sourceId: string): void {
    this.manualSyncFallbackKeys.delete(sourceId);
    try {
      sessionStorage.removeItem(this.manualSyncSessionPrefix + sourceId);
    } catch {
      // Storage can be unavailable in restricted browser contexts; the current
      // request still carries its key, but recovery across a reload is limited.
    }
  }

  private acceptManualSyncJob(job: ISourceManualSyncJob): void {
    this.clearManualSyncRetryNotBefore(job.sourceId);
    this.upsertManualSyncJob(job);
    if (job.status === 'completed' && job.itemsFailed === 0) {
      this.notification.success('Source sync completed', this.safeOperationalText(job.message) || 'HAI completed the source sync.');
      this.refresh();
      return;
    }
    if (this.syncFailureStatus(job.status) || job.status === 'cancelled' || job.status === 'completed') {
      this.notification.error('Source sync needs attention', this.safeOperationalText(job.message) || 'The sync did not complete fully; the source checkpoint was retained.');
      this.refresh();
      return;
    }
    this.notification.info('Source sync queued', 'HAI will continue in the background. You can leave this page and return to review its status.');
    this.startManualSyncPolling(job.sourceId, job.id, 0, 0);
    this.refresh();
  }

  private resumeManualSyncPolling(jobs: ISourceSyncJob[]): void {
    for (const job of jobs) {
      if (job.mode === 'manual_async_sync' && (job.status === 'queued' || job.status === 'running')) {
        this.startManualSyncPolling(job.sourceId, job.id, this.manualSyncPollIntervalMs, 0);
      }
    }
  }

  private startManualSyncPolling(sourceId: string, jobId: string, delayMs: number, failures: number): void {
    if (this.manualSyncPolls.has(sourceId)) return;
    const poll = timer(delayMs).pipe(
      switchMap(() => this.sourceService.manualSyncJob(jobId).pipe(
        timeout(this.loadTimeoutMs),
        tap((job) => { if (!this.isManualSyncJob(job, sourceId, jobId)) throw new Error('Invalid sync status'); }),
      )),
      catchError(() => of(null as ISourceManualSyncJob | null)),
    ).subscribe({
      next: (job) => {
        if (!job) {
          if (failures >= 4) {
            this.manualSyncStatusUnavailable[sourceId] = true;
            this.notification.warning('Sync status unavailable', 'The background job may still be running. Retry the status check; this will not start or cancel another sync.');
            this.manualSyncPolls.delete(sourceId);
            return;
          }
          this.manualSyncPolls.delete(sourceId);
          this.startManualSyncPolling(sourceId, jobId, this.manualSyncPollIntervalMs, failures + 1);
          return;
        }
        this.upsertManualSyncJob(job);
        if (job.status === 'queued' || job.status === 'running') {
          this.manualSyncPolls.delete(sourceId);
          this.startManualSyncPolling(sourceId, jobId, this.manualSyncPollIntervalMs, 0);
          return;
        }
        this.manualSyncPolls.delete(sourceId);
        if (job.status === 'completed' && job.itemsFailed === 0) {
          this.notification.success('Source sync completed', this.safeOperationalText(job.message) || 'HAI completed the source sync.');
        } else {
          this.notification.error('Source sync needs attention', this.safeOperationalText(job.message) || 'The sync did not complete fully; the source checkpoint was retained.');
        }
        this.refresh();
      },
    });
    this.manualSyncPolls.set(sourceId, poll);
  }

  private upsertManualSyncJob(job: ISourceManualSyncJob): void {
    this.manualSyncJobsBySource = { ...this.manualSyncJobsBySource, [job.sourceId]: job };
    this.syncJobs = [job, ...this.syncJobs.filter((existing) => existing.id !== job.id)].slice(0, 250);
    this.rebuildSourceIndexes();
  }

  transcribeSource(source: IConnectedSource): void {
    if (this.syncing) {
      return;
    }
    this.syncing = true;
    this.operationError = '';
    this.sourceService
      .transcribe(source.id)
      .pipe(timeout(Math.max(this.operationTimeoutMs, 300000)))
      .subscribe({
        next: (result) => {
          this.syncing = false;
          this.notifySyncResult('Local transcription', result);
          this.refresh();
        },
        error: (error) => {
          this.syncing = false;
          this.notification.error('Local transcription failed', this.operationErrorMessage(
            error,
            'Check the local runner, reviewed GGML model, and selected audio folder.'
          ));
        },
      });
  }

  extractDocuments(source: IConnectedSource): void {
    if (this.syncing) {
      return;
    }
    this.syncing = true;
    this.sourceService
      .extractDocuments(source.id)
      .pipe(timeout(Math.max(this.operationTimeoutMs, 300000)))
      .subscribe({
        next: (result) => {
          this.syncing = false;
          this.notifySyncResult('Local document extraction', result);
          this.refresh();
        },
        error: (error) => {
          this.syncing = false;
          this.notification.error('Local document extraction failed', this.operationErrorMessage(
            error,
            'Check the local Docling runner, selected document folder, and pre-provisioned artifacts.'
          ));
        },
      });
  }

  sourceActionLabel(source: IConnectedSource): string {
    if (source.connectorKey === 'whisper-audio') {
      return this.sourceHasSyncFailure(source) ? 'Retry transcription' : 'Transcribe';
    }
    if (source.connectorKey === 'docling-documents') {
      return this.sourceHasSyncFailure(source) ? 'Retry extraction' : 'Extract documents';
    }
    return this.sourceHasSyncFailure(source) ? 'Retry sync' : 'Sync';
  }

  sourceActionTitle(source: IConnectedSource): string {
    if (source.connectorKey === 'whisper-audio') {
      return 'Transcribe the explicit local audio folder. HAI never records a microphone or accepts uploaded audio.';
    }
    if (source.connectorKey === 'docling-documents') {
      return 'Extract text from the explicit local document folder. HAI never chooses cloud services or enables OCR implicitly.';
    }
    return this.sourceHasSyncFailure(source)
      ? 'Retry the incomplete or failed incremental sync. HAI keeps the provider checkpoint and records the new attempt.'
      : 'Run an incremental sync for this source';
  }

  syncFolder(): void {
    if (this.folderForm.invalid || this.syncing) {
      return;
    }
    this.syncing = true;
    this.sourceService
      .sync(this.folderForm.value.sourceId, {
        mode: this.folderForm.value.mode,
        items: [],
        folderPath: this.folderForm.value.folderPath,
        projectKey: this.folderForm.value.projectKey,
        limit: Number(this.folderForm.value.limit || 100),
        maxBytes: Number(this.folderForm.value.maxBytes || 1048576),
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (result) => {
          this.syncing = false;
          this.notifySyncResult('Folder sync', result);
          this.refresh();
        },
        error: (error) => {
          this.syncing = false;
          this.notification.error('Folder sync failed', this.operationErrorMessage(error, 'The selected folder could not be synchronized.'));
        },
      });
  }

  runDueScheduledSyncs(): void {
    if (this.syncing) {
      return;
    }
    this.syncing = true;
    this.operationError = '';
    this.sourceService.runDueScheduledSyncs().pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (result) => {
        this.syncing = false;
        const summary = `${result.completed} completed, ${result.failed} failed, ${result.skipped} skipped.`;
        if (result.failed > 0) {
          this.notification.warning('Scheduled sync requires attention', summary);
        } else {
          this.notification.success('Scheduled sync checked', summary);
        }
        this.refresh();
      },
      error: (error) => {
        this.syncing = false;
        this.operationError = this.operationErrorMessage(error, 'Due sources could not be checked.');
        this.notification.error('Scheduled sync check failed', this.operationError);
      },
    });
  }

  // Creates a read-only Google source, then opens Google's consent screen.
  connectGmail(): void {
    this.connectGoogleSource('gmail');
  }

  lifeGraphProjections(): ISourceLifeGraphProjectionOutcome[] {
    return this.lastSyncResult?.lifeGraphProjections || [];
  }

  lifeGraphWarnings(): string[] {
    return this.lastSyncResult?.warnings || [];
  }

  openLifeGraph(): void {
    this.router.navigate(['/governance-control']);
  }

  connectGoogleDrive(): void {
    this.connectGoogleSource('google-drive');
  }

  connectGoogleContacts(): void {
    this.connectGoogleSource('google-contacts');
  }

  connectGoogleCalendar(): void {
    this.connectGoogleSource('google-calendar');
  }

  authorizeGoogleSource(source: IConnectedSource, justCreated = false): void {
    if (this.authorizing) return;
    if (justCreated) {
      if (!this.connectorCanConnect(this.connectorFor(source))) return;
    } else if (!this.canAuthorizeGoogleSource(source)) {
      return;
    }
    this.operationError = '';
    this.authorizing = true;
    this.sourceService.startGoogleOAuth(source.id).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => (this.authorizing = false)),
    ).subscribe({
      next: ({ authorizeUrl }) => {
        this.notification.info('Continue with Google', 'Approve read-only access. HAI will return to this source after Google redirects back.');
        this.navigateToGoogleAuthorization(authorizeUrl);
      },
      error: (error) => {
        this.operationError = this.operationErrorMessage(error, 'Could not start Google authorization.');
        this.notification.error('Authorization failed', this.operationError);
      },
    });
  }

  // OAuth begins after the consent URL is returned, so opening a new window at
  // this point is commonly blocked as an asynchronous popup. A same-tab
  // navigation is deterministic and the signed callback restores this route.
  private navigateToGoogleAuthorization(authorizeUrl: string): void {
    window.location.assign(authorizeUrl);
  }

  private connectGoogleSource(connectorKey: 'gmail' | 'google-drive' | 'google-contacts' | 'google-calendar'): void {
    if (this.connecting || this.authorizing) return;
    const connector = this.connectors.find((item) => item.connectorKey === connectorKey);
    const labels: Record<typeof connectorKey, string> = {
      gmail: 'Gmail',
      'google-drive': 'Google Drive',
      'google-contacts': 'Google Contacts',
      'google-calendar': 'Google Calendar',
    };
    // These are HAI's normalized permissions. The backend owns the provider
    // OAuth scope selection and never trusts client-supplied Google scopes.
    const permissions: Record<typeof connectorKey, string> = {
      gmail: 'email:read',
      'google-drive': 'cloud_document:read',
      'google-contacts': 'contact:read',
      'google-calendar': 'calendar:read',
    };
    const label = labels[connectorKey];
	if (!connector) {
		this.notification.warning(`${label} unavailable`, 'Refresh the connector catalog and try again.');
		return;
	}
	if (!this.connectorCanConnect(connector)) {
      this.notification.warning(
        `${label} not configured`,
        'Configure the Google OAuth client, redirect URL, token encryption key, and state signing key first.'
      );
      return;
    }
    this.connecting = true;
    this.sourceService
      .createSource({
        connectorKey,
        name: `${label} (Google account)`,
        category: connector.category,
        enabled: true,
        localOnly: false,
        syncFrequency: '15m',
        permissions: ['metadata:read', permissions[connectorKey]],
      })
      .pipe(
        timeout(this.operationTimeoutMs),
        finalize(() => (this.connecting = false))
      )
      .subscribe({
        next: (source) => {
          this.authorizeGoogleSource(source, true);
          this.refresh();
        },
        error: (error) => {
          this.operationError = this.operationErrorMessage(error, `Failed to create ${label} source.`);
          this.notification.error('Error', this.operationError);
        },
      });
  }

  connectWhatsAppSource(): void {
	if (this.connecting) {
		return;
	}
    const connector = this.connectors.find((item) => item.connectorKey === 'whatsapp-export');
    if (!this.connectorCanConnect(connector)) {
      this.notification.warning('WhatsApp connector unavailable', 'Refresh connectors and verify the backend exposes whatsapp-export as enabled.');
      return;
    }
	this.connecting = true;
	this.sourceService
      .createSource({
        connectorKey: 'whatsapp-export',
        name: this.whatsappForm.value.name || 'WhatsApp exported chats',
        category: 'chat',
        enabled: true,
        localOnly: true,
        syncFrequency: 'manual',
        syncTarget: this.whatsappForm.value.folderPath || 'whatsapp',
        defaultProjectKey: this.whatsappForm.value.projectKey || 'Robert-life-os',
        permissions: ['metadata:read', 'chat:read', 'selected-chat-export-read'],
        excludePatterns: ['media omitted', 'omitted', 'spam'],
      })
	  .pipe(timeout(this.operationTimeoutMs), finalize(() => (this.connecting = false)))
      .subscribe({
        next: (source) => {
          this.whatsappForm.patchValue({ sourceId: source.id });
          this.notification.success('WhatsApp source ready', 'Exported chats can now be parsed locally and review-gated.');
          this.refresh();
        },
        error: (error) => {
          this.operationError = this.operationErrorMessage(error, 'Failed to connect WhatsApp source.');
          this.notification.error('Error', this.operationError);
        },
      });
  }

  connectOdooSource(): void {
	if (this.odooForm.invalid || this.connecting) {
		this.odooForm.markAllAsTouched();
		return;
	}
    const connector = this.connectors.find((item) => item.connectorKey === 'odoo-herp');
    if (!this.connectorCanConnect(connector)) {
      this.notification.warning('Odoo connector unavailable', 'Refresh connectors and verify the backend exposes odoo-herp as enabled.');
      return;
    }
	this.connecting = true;
	this.sourceService
      .createSource({
        connectorKey: 'odoo-herp',
        name: this.odooForm.value.name || 'Odoo / HERP workspace',
        category: 'herp',
        enabled: true,
        localOnly: Boolean(this.odooForm.value.localOnly),
        syncFrequency: this.odooForm.value.syncFrequency || 'manual',
        syncTarget: this.odooSyncTarget(),
        defaultProjectKey: this.odooForm.value.projectKey || 'Robert-life-os',
        permissions: ['metadata:read', 'herp:read', 'odoo:read'],
        excludePatterns: ['password', 'secret', 'token', 'private'],
      })
	  .pipe(timeout(this.operationTimeoutMs), finalize(() => (this.connecting = false)))
      .subscribe({
        next: (source) => {
          this.odooForm.patchValue({ sourceId: source.id });
          this.notification.success('Odoo / HERP source ready', 'Odoo app domains can now be modeled into governed HAI workflows.');
          this.refresh();
        },
        error: (error) => {
          this.operationError = this.operationErrorMessage(error, 'Failed to connect Odoo / HERP source.');
          this.notification.error('Error', this.operationError);
        },
      });
  }

  syncOdooApps(): void {
    if (this.syncing) return;
    const sourceId = this.selectedOdooSourceId();
    if (!sourceId) {
      this.notification.warning('Odoo source missing', 'Connect or select an Odoo / HERP source first.');
      return;
    }
    this.syncing = true;
    this.operationError = '';
    this.sourceService
      .sync(sourceId, {
        mode: 'incremental_sync',
        items: [],
        folderPath: this.odooForm.value.apps || '',
        projectKey: this.odooForm.value.projectKey || 'Robert-life-os',
      })
      .pipe(timeout(this.operationTimeoutMs), finalize(() => (this.syncing = false)))
      .subscribe({
        next: (result) => {
          this.notifySyncResult('Odoo / HERP modeling', result);
          this.refresh();
        },
        error: (error) => {
          this.operationError = this.operationErrorMessage(error, 'The Odoo app domains could not be modeled.');
          this.notification.error('Odoo modeling failed', this.operationError);
        },
      });
  }

  importWhatsAppPaste(): void {
    if (this.syncing) return;
    const sourceId = this.selectedWhatsAppSourceId();
    const content = String(this.whatsappForm.value.pastedExport || '').trim();
    if (this.whatsappForm.invalid || !sourceId || !content) {
      this.notification.warning('WhatsApp import missing input', 'Select a source, add a chat label and project, then paste an exported chat.');
      return;
    }
    this.syncing = true;
    this.operationError = '';
    const importId = Date.now();
    this.sourceService
      .sync(sourceId, {
        mode: 'manual_import',
        projectKey: this.whatsappForm.value.projectKey,
        limit: Number(this.whatsappForm.value.chunkMessages || 40),
        items: [
          {
            externalId: `whatsapp-manual-${importId}`,
            title: this.whatsappForm.value.chatTitle,
            content,
            sourceUri: `whatsapp-export://manual/${importId}`,
            itemType: 'whatsapp_export',
            projectKey: this.whatsappForm.value.projectKey,
            metadata: 'source=whatsapp-export;import=manual-paste;privacy=local-review-gated',
          },
        ],
      })
      .pipe(timeout(this.operationTimeoutMs), finalize(() => (this.syncing = false)))
      .subscribe({
        next: (result) => {
          this.notifySyncResult('WhatsApp import', result);
          this.refresh();
        },
        error: (error) => {
          this.operationError = this.operationErrorMessage(error, 'The pasted export could not be parsed.');
          this.notification.error('WhatsApp import failed', this.operationError);
        },
      });
  }

  syncWhatsAppFolder(): void {
    if (this.syncing) return;
    const sourceId = this.selectedWhatsAppSourceId();
    if (!sourceId) {
      this.notification.warning('WhatsApp source missing', 'Connect or select a WhatsApp export source first.');
      return;
    }
    this.syncing = true;
    this.operationError = '';
    this.sourceService
      .sync(sourceId, {
        mode: 'incremental_sync',
        items: [],
        folderPath: this.whatsappForm.value.folderPath || 'whatsapp',
        projectKey: this.whatsappForm.value.projectKey || 'Robert-life-os',
        limit: Number(this.whatsappForm.value.chunkMessages || 40),
        maxBytes: Number(this.whatsappForm.value.maxBytes || 2097152),
      })
      .pipe(timeout(this.operationTimeoutMs), finalize(() => (this.syncing = false)))
      .subscribe({
        next: (result) => {
          this.notifySyncResult('WhatsApp folder scan', result);
          this.refresh();
        },
        error: (error) => {
          this.operationError = this.operationErrorMessage(error, 'The export folder could not be scanned.');
          this.notification.error('WhatsApp scan failed', this.operationError);
        },
      });
  }

  whatsappSources(): IConnectedSource[] {
    return this.sources.filter((source) => source.connectorKey === 'whatsapp-export');
  }

  odooSources(): IConnectedSource[] {
    return this.sources.filter((source) => source.connectorKey === 'odoo-herp');
  }

  private selectedWhatsAppSourceId(): string {
    const selected = this.whatsappForm.value.sourceId;
    if (selected) {
      return selected;
    }
    return this.whatsappSources()[0]?.id || '';
  }

  private selectedOdooSourceId(): string {
    const selected = this.odooForm.value.sourceId;
    if (selected) {
      return selected;
    }
    return this.odooSources()[0]?.id || '';
  }

  private odooSyncTarget(): string {
    const baseUrl = String(this.odooForm.value.baseUrl || '').trim();
    const apps = String(this.odooForm.value.apps || '').trim();
    if (!baseUrl) {
      return apps;
    }
    if (!apps) {
      return baseUrl;
    }
    return `${baseUrl}${baseUrl.includes('?') ? '&' : '?'}apps=${encodeURIComponent(apps)}`;
  }

  private notifySyncResult(label: string, result: ISourceSyncResult): void {
    this.lastSyncResult = result;
    const summary = `${result.job.itemsSeen} seen, ${result.job.itemsFailed || 0} failed. ${this.safeOperationalText(result.message)}`;
    const hasPartialFailure = Number(result.job.itemsFailed || 0) > 0 || Boolean((result.errors || []).length);
    if (result.job.status === 'completed' && !hasPartialFailure && !(result.warnings || []).length) {
      this.notification.success(label, summary);
      return;
    }
    const detailCount = (result.errors || []).length + (result.warnings || []).length;
    const detailSuffix = detailCount ? ` ${detailCount} error or warning detail(s) are available for review.` : '';
    this.notification.warning(`${label} requires attention`, summary + detailSuffix);
  }

  search(): void {
    if (this.searchForm.invalid) {
      return;
    }
    this.searchSubscription?.unsubscribe();
    this.searchResult = undefined;
    this.searchError = '';
    this.searchLoading = true;
    this.searchSubscription = this.sourceService.search(this.searchForm.value).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => (this.searchLoading = false)),
    ).subscribe({
      next: (result) => {
        this.searchResult = result;
        this.updateSourceActions();
      },
      error: (error) => {
        this.searchError = this.operationErrorMessage(error, 'Search did not complete.');
        this.notification.error('Search failed', this.searchError);
      },
    });
  }

  sourceMutationInProgress(source: IConnectedSource, mutation?: SourceMutation): boolean {
    const activeMutation = this.sourceMutationsInProgress[source.id]
      || (this.destructiveSubmitting && this.destructiveTarget?.action === 'revoke'
        && this.destructiveTarget.id === source.id ? 'revoke' : undefined);
    return mutation ? activeMutation === mutation : Boolean(activeMutation);
  }

  private runSourceMutation(
    source: IConnectedSource,
    mutation: SourceMutation,
    request: () => Observable<unknown>,
    fallback: string,
  ): void {
    if (this.sourceMutationInProgress(source)) return;
    this.operationError = '';
    this.sourceMutationsInProgress = { ...this.sourceMutationsInProgress, [source.id]: mutation };
    request().pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => {
        const { [source.id]: _activeMutation, ...remaining } = this.sourceMutationsInProgress;
        this.sourceMutationsInProgress = remaining;
      }),
    ).subscribe({
      next: () => this.refresh(),
      error: (error) => {
        const labels: Record<SourceMutation, string> = {
          pause: 'Pause', resume: 'Resume', reindex: 'Re-index', revoke: 'Revoke',
        };
        const sourceName = this.safeOperationalText(source.name) || this.safeOperationalText(source.connectorKey) || 'Source';
        const detail = this.operationErrorMessage(error, fallback);
        this.operationError = `${sourceName}: ${detail}`;
        this.notification.error(`${labels[mutation]} failed`, `${sourceName}: ${detail}`);
      },
    });
  }

  pause(source: IConnectedSource): void {
    this.runSourceMutation(source, 'pause', () => this.sourceService.pause(source.id), 'The source could not be paused.');
  }

  resume(source: IConnectedSource): void {
    if (this.sourceIsRevoked(source)) return;
    this.runSourceMutation(source, 'resume', () => this.sourceService.resume(source.id), 'The source could not be resumed.');
  }

  reindex(source: IConnectedSource): void {
    this.runSourceMutation(source, 'reindex', () => this.sourceService.reindex(source.id), 'The source could not be re-indexed.');
  }

  revoke(source: IConnectedSource): void {
    if (this.sourceMutationInProgress(source) || this.sourceIsRevoked(source)) return;
    this.openDestructiveApproval('revoke', source.id, source.name);
  }

  archive(extraction: ISourceExtraction): void {
    this.sourceService.archiveExtraction(extraction.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.loadExtractions(),
      error: () => this.notification.error('Archive failed', 'The extracted record could not be archived.'),
    });
  }

  delete(extraction: ISourceExtraction): void {
    this.openDestructiveApproval('delete', extraction.id, extraction.summary || extraction.id);
  }

  private openDestructiveApproval(action: 'revoke' | 'delete', id: string, label: string): void {
    if (this.destructiveSubmitting) return;
    this.destructiveTarget = { action, id, label: this.safeOperationalText(label) };
    this.destructiveApprovalForm.reset();
    this.destructiveError = this.uncertainDestructiveActions.has(`${action}:${id}`)
      ? 'The previous outcome is unknown. Refresh source status and inspect audit history; no repeat request will be sent from this page.'
      : '';
  }

  destructiveCanSubmit(): boolean {
    return Boolean(this.destructiveTarget) && this.destructiveApprovalForm.valid && !this.destructiveSubmitting
      && !this.uncertainDestructiveActions.has(`${this.destructiveTarget?.action}:${this.destructiveTarget?.id}`);
  }

  closeDestructiveApproval(): void {
    if (this.destructiveSubmitting) return;
    this.destructiveTarget = undefined;
    this.destructiveApprovalForm.reset();
  }

  reviewDestructiveActivity(): void {
    if (this.destructiveSubmitting) return;
    this.closeDestructiveApproval();
    this.openAdvancedAction('connect', 'source-activity');
  }

  submitDestructiveApproval(): void {
    if (!this.destructiveCanSubmit()) return;
    const target = this.destructiveTarget!;
    const authorization = this.destructiveApprovalForm.value as ISourceDestructiveAuthorization;
    const request: Observable<unknown> = target.action === 'revoke'
      ? this.sourceService.revoke(target.id, authorization)
      : this.sourceService.deleteExtraction(target.id, authorization);
    this.destructiveSubmitting = true;
    this.destructiveError = '';
    request.pipe(
      timeout(this.operationTimeoutMs),
      tap((result) => {
        if (target.action === 'revoke' && (!(result as IConnectedSource)?.id
          || (result as IConnectedSource).id !== target.id || !this.sourceIsRevoked(result as IConnectedSource))) {
          throw new Error('Revoke outcome unconfirmed');
        }
      }),
      finalize(() => { this.destructiveSubmitting = false; }),
    ).subscribe({
      next: () => {
        this.destructiveTarget = undefined;
        this.destructiveApprovalForm.reset();
        this.notification.success(target.action === 'revoke' ? 'Source revoked' : 'Record deleted', target.label);
        if (target.action === 'revoke') this.refresh();
        else this.loadExtractions();
      },
      error: (error) => {
        const status = Number(error?.status || 0);
        if (status === 0 || status === 408 || status >= 500 || error?.name === 'TimeoutError') {
          this.uncertainDestructiveActions.add(`${target.action}:${target.id}`);
          this.destructiveError = 'The outcome is unknown. Refresh source status and inspect audit history before making another change. No automatic retry was sent.';
        } else {
          const recovery = status === 423 ? 'Emergency stop remains active; no client override is available.'
            : status === 403 ? 'An existing server-approved reference matching this exact action is required.'
              : 'Review the current resource and approval before retrying.';
          this.destructiveError = `${this.operationErrorMessage(error, 'The server rejected this change.')} ${recovery}`;
        }
        this.notification.error(target.action === 'revoke' ? 'Revoke not confirmed' : 'Delete not confirmed', this.destructiveError);
      },
    });
  }

  extractionCorrectionRecord(extractionId: string): ISourceExtractionCorrectionRecovery | undefined {
    return this.extractionCorrectionRecoveries[extractionId];
  }

  extractionCorrectionLabel(extractionId: string): string {
    const labels: Record<SourceExtractionCorrectionUiState, string> = {
      submitting: 'Submitting correction',
      queued: 'Queued for verification',
      running: 'Applying and verifying',
      applied: 'Applied and verified',
      conflict: 'Conflict: record changed',
      failed_may_have_saved: 'Outcome unknown; may have saved',
      failed_not_saved: 'Correction not saved',
    };
    const state = this.extractionCorrectionRecoveries[extractionId]?.state;
    return state ? labels[state] : '';
  }

  extractionCorrectionTone(extractionId: string): 'good' | 'watch' | 'bad' | 'neutral' {
    const state = this.extractionCorrectionRecoveries[extractionId]?.state;
    if (state === 'applied') return 'good';
    if (state === 'submitting' || state === 'queued' || state === 'running') return 'watch';
    if (state === 'conflict' || state === 'failed_may_have_saved' || state === 'failed_not_saved') return 'bad';
    return 'neutral';
  }

  extractionCorrectionMessage(extractionId: string): string {
    const localMessage = this.extractionCorrectionLocalMessages[extractionId];
    if (localMessage) return localMessage;
    const record = this.extractionCorrectionRecoveries[extractionId];
    const view = this.extractionCorrectionViews[extractionId];
    if (view?.message) return view.message;
    switch (record?.state) {
      case 'submitting':
        return 'HAI is submitting this correction once.';
      case 'queued':
        return 'The correction is persisted and waiting for background processing.';
      case 'running':
        return 'The correction was saved; HAI is reconciling and verifying its derived records.';
      case 'applied':
        return 'The correction and its follow-up verification completed.';
      case 'conflict':
        return 'The extraction changed since it was loaded. Refresh it and review before submitting a new correction.';
      case 'failed_may_have_saved':
        return 'The request outcome is uncertain. HAI will not send it again automatically.';
      case 'failed_not_saved':
        return 'The correction was not saved. Refresh the extraction before trying again.';
      default:
        return '';
    }
  }

  extractionCorrectionCanCheck(extractionId: string): boolean {
    const record = this.extractionCorrectionRecoveries[extractionId];
    return Boolean(record?.correctionId && record.state !== 'applied' && record.state !== 'failed_not_saved');
  }

  extractionCorrectionCheckInProgress(extractionId: string): boolean {
    return this.extractionCorrectionPolls.has(extractionId);
  }

  correctionSubmissionBlocked(extraction: ISourceExtraction): boolean {
    const state = this.extractionCorrectionRecoveries[extraction.id]?.state;
    return !extraction.uncertain
      || state === 'submitting'
      || state === 'queued'
      || state === 'running'
      || state === 'applied'
      || state === 'failed_may_have_saved';
  }

  checkExtractionCorrection(extractionId: string): void {
    const record = this.extractionCorrectionRecoveries[extractionId];
    if (!record) return;
    if (!record.correctionId) {
      this.extractionCorrectionLocalMessages[extractionId] =
        'No server correction ID was received, so its status cannot be queried. Refresh the extracted record to inspect its current flag; do not submit it again until the outcome is clear.';
      this.refreshExtractions();
      return;
    }
    this.extractionCorrectionPolls.get(extractionId)?.unsubscribe();
    this.extractionCorrectionPolls.delete(extractionId);
    record.statusChecks = 0;
    record.pollFailures = 0;
    this.extractionCorrectionLocalMessages[extractionId] = '';
    this.persistExtractionCorrectionRecoveries();
    this.beginExtractionCorrectionStatusCheck(extractionId, 0);
  }

  markCorrected(extraction: ISourceExtraction): void {
    if (this.correctionSubmissionBlocked(extraction)) return;
    const revision = String(extraction.updatedAt || '');
    if (!this.isRFC3339Nano(revision)) {
      this.setLocalCorrectionFailure(extraction.id, 'The extraction has no valid RFC3339 revision. Refresh the record; no request was sent.');
      return;
    }

    const recovery: ISourceExtractionCorrectionRecovery = {
      version: 1,
      extractionId: extraction.id,
      idempotencyKey: this.newCorrectionIdempotencyKey(),
      ifMatchRevision: revision,
      state: 'submitting',
      statusChecks: 0,
      pollFailures: 0,
      updatedAt: new Date().toISOString(),
    };
    this.extractionCorrectionLocalMessages[extraction.id] = '';
    delete this.extractionCorrectionViews[extraction.id];
    if (!this.saveExtractionCorrectionRecovery(recovery)) {
      recovery.state = 'failed_not_saved';
      this.extractionCorrectionRecoveries[extraction.id] = recovery;
      this.extractionCorrectionLocalMessages[extraction.id] = 'Browser storage is unavailable. The request was not sent because its recovery key could not be saved.';
      return;
    }

    this.sourceService.submitExtractionCorrection(
      extraction.id,
      { uncertain: false },
      revision,
      recovery.idempotencyKey,
    ).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (view) => {
        if (!this.isExtractionCorrectionView(view) || view.extractionId !== extraction.id) {
          this.markCorrectionOutcomeUnknown(extraction.id, 'The server response could not be matched to this extraction. It may have saved; HAI did not resend it.');
          return;
        }
        this.applyExtractionCorrectionView(extraction.id, view, true);
      },
      error: (error) => this.handleExtractionCorrectionSubmitError(extraction.id, error),
    });
  }

  private newCorrectionIdempotencyKey(): string {
    try {
      if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
    } catch {
      // Use the non-secret uniqueness fallback if the browser blocks crypto access.
    }
    return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
  }

  private isRFC3339Nano(value: string): boolean {
    return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)
      && Number.isFinite(Date.parse(value));
  }

  private isExtractionCorrectionView(value: unknown): value is ISourceExtractionCorrectionView {
    if (!value || typeof value !== 'object') return false;
    const view = value as Partial<ISourceExtractionCorrectionView>;
    return typeof view.id === 'string'
      && Boolean(view.id)
      && typeof view.extractionId === 'string'
      && typeof view.status === 'string'
      && typeof view.phase === 'string'
      && typeof view.intentPersisted === 'boolean'
      && typeof view.patchSaved === 'boolean'
      && typeof view.recoveryPending === 'boolean'
      && typeof view.needsReview === 'boolean'
      && typeof view.expectedRevision === 'string'
      && typeof view.attempts === 'number'
      && Number.isFinite(view.attempts)
      && typeof view.maxAttempts === 'number'
      && Number.isFinite(view.maxAttempts);
  }

  private restoreExtractionCorrections(): void {
    let stored: unknown;
    try {
      stored = JSON.parse(sessionStorage.getItem(this.extractionCorrectionSessionKey) || '[]');
    } catch {
      return;
    }
    if (!Array.isArray(stored)) return;

    for (const candidate of stored) {
      if (!this.isExtractionCorrectionRecovery(candidate)) continue;
      const record = { ...candidate };
      if (record.state === 'submitting') {
        record.state = 'failed_may_have_saved';
        this.extractionCorrectionLocalMessages[record.extractionId] =
          'The page ended before a response arrived. The correction may have saved; HAI did not resubmit it.';
      }
      this.extractionCorrectionRecoveries[record.extractionId] = record;
    }

    this.persistExtractionCorrectionRecoveries();
    Object.values(this.extractionCorrectionRecoveries).forEach((record) => {
      if (record.correctionId && (record.state === 'queued' || record.state === 'running' || record.state === 'failed_may_have_saved')) {
        this.beginExtractionCorrectionStatusCheck(record.extractionId, 0);
      }
    });
  }

  private isExtractionCorrectionRecovery(value: unknown): value is ISourceExtractionCorrectionRecovery {
    if (!value || typeof value !== 'object') return false;
    const record = value as Partial<ISourceExtractionCorrectionRecovery>;
    const states: SourceExtractionCorrectionUiState[] = [
      'submitting', 'queued', 'running', 'applied', 'conflict', 'failed_may_have_saved', 'failed_not_saved',
    ];
    return record.version === 1
      && typeof record.extractionId === 'string'
      && Boolean(record.extractionId)
      && typeof record.idempotencyKey === 'string'
      && typeof record.ifMatchRevision === 'string'
      && typeof record.state === 'string'
      && states.includes(record.state as SourceExtractionCorrectionUiState)
      && typeof record.statusChecks === 'number'
      && Number.isFinite(record.statusChecks)
      && typeof record.pollFailures === 'number'
      && Number.isFinite(record.pollFailures)
      && typeof record.updatedAt === 'string'
      && (record.correctionId === undefined || typeof record.correctionId === 'string');
  }

  private persistExtractionCorrectionRecoveries(): boolean {
    try {
      sessionStorage.setItem(this.extractionCorrectionSessionKey, JSON.stringify(Object.values(this.extractionCorrectionRecoveries)));
      return true;
    } catch {
      return false;
    }
  }

  private saveExtractionCorrectionRecovery(record: ISourceExtractionCorrectionRecovery): boolean {
    this.extractionCorrectionRecoveries[record.extractionId] = record;
    return this.persistExtractionCorrectionRecoveries();
  }

  private setLocalCorrectionFailure(extractionId: string, message: string): void {
    const record: ISourceExtractionCorrectionRecovery = {
      version: 1,
      extractionId,
      idempotencyKey: '',
      ifMatchRevision: '',
      state: 'failed_not_saved',
      statusChecks: 0,
      pollFailures: 0,
      updatedAt: new Date().toISOString(),
    };
    this.extractionCorrectionRecoveries[extractionId] = record;
    this.extractionCorrectionLocalMessages[extractionId] = message;
  }

  private handleExtractionCorrectionSubmitError(extractionId: string, error: any): void {
    const responseView = error?.error;
    if (this.isExtractionCorrectionView(responseView) && responseView.extractionId === extractionId) {
      this.applyExtractionCorrectionView(extractionId, responseView, true);
      return;
    }
    const record = this.extractionCorrectionRecoveries[extractionId];
    if (!record) return;
    if (error?.status === 409) {
      record.state = 'conflict';
      record.status = 'conflict';
      record.updatedAt = new Date().toISOString();
      this.extractionCorrectionLocalMessages[extractionId] =
        'The extraction revision no longer matches. Refresh the record and review it before submitting another correction.';
      this.persistExtractionCorrectionRecoveries();
      this.notification.warning('Correction needs review', this.extractionCorrectionLocalMessages[extractionId]);
      this.refreshExtractions();
      return;
    }
    this.markCorrectionOutcomeUnknown(
      extractionId,
      'The request failed without a trustworthy status response. It may have saved; HAI will not send it again automatically.',
    );
  }

  private markCorrectionOutcomeUnknown(extractionId: string, message: string): void {
    const record = this.extractionCorrectionRecoveries[extractionId];
    if (!record) return;
    record.state = 'failed_may_have_saved';
    record.updatedAt = new Date().toISOString();
    this.extractionCorrectionLocalMessages[extractionId] = message;
    this.persistExtractionCorrectionRecoveries();
    this.notification.warning('Correction outcome is uncertain', message);
  }

  private correctionStateFromView(view: ISourceExtractionCorrectionView): SourceExtractionCorrectionUiState {
    const status = view.status.trim().toLowerCase();
    const errorCode = String(view.errorCode || '').toLowerCase();
    if (status.includes('conflict') || errorCode.includes('revision') || errorCode.includes('conflict')) return 'conflict';
    if (['applied', 'verified', 'complete', 'completed', 'succeeded', 'success'].includes(status)
      && view.intentPersisted && view.patchSaved && !view.recoveryPending && !view.needsReview) {
      return 'applied';
    }
    if (['failed', 'dead', 'exhausted', 'cancelled', 'canceled'].includes(status)) {
      return view.patchSaved || view.recoveryPending ? 'failed_may_have_saved' : 'failed_not_saved';
    }
    if (['queued', 'pending', 'accepted'].includes(status)) return 'queued';
    if (['running', 'applying', 'reconciling', 'verifying'].includes(status) || view.recoveryPending) return 'running';
    if (view.intentPersisted) return 'queued';
    return 'failed_may_have_saved';
  }

  private applyExtractionCorrectionView(
    extractionId: string,
    view: ISourceExtractionCorrectionView,
    notify: boolean,
  ): void {
    const record = this.extractionCorrectionRecoveries[extractionId];
    if (!record || view.extractionId !== extractionId) {
      this.markCorrectionOutcomeUnknown(extractionId, 'The returned correction did not match this extraction. Its outcome needs a manual status check.');
      return;
    }
    this.extractionCorrectionViews[extractionId] = view;
    const state = this.correctionStateFromView(view);
    record.correctionId = view.id;
    record.status = view.status;
    record.state = state;
    record.pollFailures = 0;
    record.updatedAt = new Date().toISOString();
    this.extractionCorrectionLocalMessages[extractionId] = '';
    this.persistExtractionCorrectionRecoveries();

    if (state === 'queued' || state === 'running') {
      if (record.statusChecks >= this.extractionCorrectionMaxStatusChecks) {
        this.extractionCorrectionLocalMessages[extractionId] =
          'Automatic status checks are paused. Use Check status to continue; HAI will not resend the correction.';
      } else {
        this.beginExtractionCorrectionStatusCheck(extractionId, this.extractionCorrectionPollIntervalMs);
      }
      if (notify) this.notification.info('Correction queued', this.extractionCorrectionMessage(extractionId));
      return;
    }

    this.extractionCorrectionPolls.get(extractionId)?.unsubscribe();
    this.extractionCorrectionPolls.delete(extractionId);
    if (notify) {
      if (state === 'applied') this.notification.success('Correction verified', this.extractionCorrectionMessage(extractionId));
      else if (state === 'conflict') this.notification.warning('Correction needs review', this.extractionCorrectionMessage(extractionId));
      else this.notification.error('Correction needs attention', this.extractionCorrectionMessage(extractionId));
    }
    if ((state === 'applied' || state === 'conflict') && !this.extractionsLoading) {
      this.extractionsLoaded = false;
      this.loadExtractions();
    }
  }

  private beginExtractionCorrectionStatusCheck(extractionId: string, delayMs: number): void {
    const record = this.extractionCorrectionRecoveries[extractionId];
    if (!record?.correctionId || this.extractionCorrectionPolls.has(extractionId)) return;
    if (record.statusChecks >= this.extractionCorrectionMaxStatusChecks) {
      this.extractionCorrectionLocalMessages[extractionId] =
        'Automatic status checks are paused. Use Check status to continue; HAI will not resend the correction.';
      return;
    }
    record.statusChecks += 1;
    record.updatedAt = new Date().toISOString();
    this.persistExtractionCorrectionRecoveries();
    const poll = timer(Math.max(0, delayMs)).pipe(
      switchMap(() => this.sourceService.extractionCorrection(record.correctionId!).pipe(timeout(this.loadTimeoutMs))),
    ).subscribe({
      next: (view) => {
        this.extractionCorrectionPolls.delete(extractionId);
        if (!this.isExtractionCorrectionView(view) || view.extractionId !== extractionId) {
          this.handleExtractionCorrectionStatusError(extractionId);
          return;
        }
        this.applyExtractionCorrectionView(extractionId, view, false);
      },
      error: () => {
        this.extractionCorrectionPolls.delete(extractionId);
        this.handleExtractionCorrectionStatusError(extractionId);
      },
    });
    this.extractionCorrectionPolls.set(extractionId, poll);
  }

  private handleExtractionCorrectionStatusError(extractionId: string): void {
    const record = this.extractionCorrectionRecoveries[extractionId];
    if (!record) return;
    record.pollFailures += 1;
    record.updatedAt = new Date().toISOString();
    this.extractionCorrectionLocalMessages[extractionId] =
      'The status service did not return a usable result. The correction was not resent; use Check status to try the read again.';
    this.persistExtractionCorrectionRecoveries();
    if (record.pollFailures <= this.extractionCorrectionMaxPollFailures
      && record.statusChecks < this.extractionCorrectionMaxStatusChecks
      && (record.state === 'queued' || record.state === 'running')) {
      const backoff = Math.min(this.extractionCorrectionPollIntervalMs * 2 ** (record.pollFailures - 1), 30000);
      this.beginExtractionCorrectionStatusCheck(extractionId, backoff);
    }
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }

  loadKnowledgeGraph(): void {
    this.graphSubscription?.unsubscribe();
    this.knowledgeGraph = undefined;
    this.graphError = '';
    this.graphLoading = true;
    this.graphSubscription = this.sourceService
      .knowledgeGraph(
        this.searchForm.value.projectKey,
        this.includeArchived,
        this.graphIncludeSensitive
      )
      .pipe(
        timeout(this.loadTimeoutMs),
        finalize(() => (this.graphLoading = false))
      )
      .subscribe({
        next: (graph) => {
          this.knowledgeGraph = graph;
          this.updateSourceActions();
        },
        error: (error) => {
          this.graphError = this.operationErrorMessage(error, 'The source-linked candidate view could not load.');
          this.notification.error('Knowledge view unavailable', this.graphError);
        },
      });
  }

  knowledgeGraphSourceLabel(refs: IKnowledgeGraphSourceRef[]): string {
    const ref = refs?.[0];
    return ref?.sourceLabel || ref?.sourceUri || 'source linked';
  }

  private loadExtractions(): void {
    this.extractionSubscription?.unsubscribe();
    this.extractionsLoading = true;
    this.extractionSubscription = this.sourceService
      .extractions(this.searchForm.value.projectKey, this.includeArchived, this.extractionPageLimit)
      .pipe(
        timeout(this.loadTimeoutMs),
        finalize(() => (this.extractionsLoading = false)),
      )
      .subscribe({
        next: (page) => {
          this.setExtractionPage(page);
          this.extractionsLoaded = true;
          this.clearLoadWarning('Extracted records');
          this.rebuildSourceIndexes();
          this.updateSourceActions();
        },
        error: () => {
          this.extractionsLoaded = false;
          this.recordLoadWarning('Extracted records');
          this.updateSourceActions();
        },
      });
  }

  refreshExtractions(): void {
    if (this.extractionsLoading) return;
    this.extractionsLoaded = false;
    this.loadExtractions();
  }

  extractionPageIsTruncated(): boolean {
    return this.extractionTotalCount > this.extractions.length;
  }

  private setExtractionPage(page: ISourceExtractionPage): void {
    this.extractions = page.items || [];
    this.extractionTotalCount = page.totalCount;
  }

  private loadAuditLogs(): void {
    this.auditSubscription?.unsubscribe();
    this.auditLogsLoading = true;
    this.auditSubscription = this.sourceService.auditLogs().pipe(
      timeout(this.loadTimeoutMs),
      finalize(() => (this.auditLogsLoading = false)),
    ).subscribe({
      next: (logs) => {
        this.auditLogs = logs;
        this.auditLogsLoaded = true;
        this.clearLoadWarning('Audit history');
      },
      error: () => {
        this.auditLogsLoaded = false;
        this.recordLoadWarning('Audit history');
      },
    });
  }

  private loadSyncJobs(): void {
    this.sourceService.syncJobs().pipe(timeout(this.loadTimeoutMs)).subscribe({
      next: (jobs) => {
        this.syncJobs = jobs || [];
        this.rebuildSourceIndexes();
        this.resumeManualSyncPolling(this.syncJobs);
        this.updateSourceActions();
      },
      error: () => {
        this.recordLoadWarning('Sync jobs');
        this.updateSourceActions();
      },
    });
  }

  private rebuildSourceIndexes(): void {
    this.sourceExtractionCounts = this.extractions.reduce<Record<string, number>>((counts, extraction) => {
      counts[extraction.sourceId] = (counts[extraction.sourceId] || 0) + 1;
      return counts;
    }, {});
    this.latestJobsBySource = this.syncJobs.reduce<Record<string, ISourceSyncJob>>((jobs, job) => {
      if (!jobs[job.sourceId]) {
        jobs[job.sourceId] = job;
      }
      return jobs;
    }, {});
  }

  private loadConnectionHealth(sources: IConnectedSource[]): void {
    this.connectionHealthSubscription?.unsubscribe();
    this.selectedHealthSubscription?.unsubscribe();
    this.selectedHealthSourceId = '';
    this.connectionHealth = {};
    this.connectionHealthUnavailable = {};
    this.connectionHealthChecking = {};
    if (!sources.length) {
      return;
    }
    const requestVersions: Record<string, number> = {};
    this.connectionHealthChecking = sources.reduce<Record<string, boolean>>((checking, source) => {
      const version = ++this.healthRequestSequence;
      this.healthRequestVersions[source.id] = version;
      requestVersions[source.id] = version;
      checking[source.id] = true;
      return checking;
    }, {});
    this.connectionHealthSubscription = this.sourceService.connectionHealths().pipe(
      timeout(this.loadTimeoutMs),
      tap((results) => {
        if (!Array.isArray(results)
          || results.some((item) => !item || typeof item.sourceId !== 'string' || !item.sourceId)) {
          throw new Error('Connection health response was not a valid source list.');
        }
      }),
      catchError(() => {
        const unavailable = { ...this.connectionHealthUnavailable };
        sources.forEach((source) => {
          if (this.healthRequestVersions[source.id] === requestVersions[source.id]) unavailable[source.id] = true;
        });
        this.connectionHealthUnavailable = unavailable;
        return of([] as ISourceConnectionHealth[]);
      })
    ).subscribe((results) => {
      const checking = { ...this.connectionHealthChecking };
      const accepted: Record<string, ISourceConnectionHealth> = {};
      const sourcesById = new Map(sources.map((source) => [source.id, source]));
      const unavailable = { ...this.connectionHealthUnavailable };
      results.forEach((item) => {
        const source = sourcesById.get(item.sourceId);
        if (!source || this.healthRequestVersions[item.sourceId] !== requestVersions[item.sourceId]) return;
        if (!this.isConnectionHealthForSource(item, source)) {
          unavailable[source.id] = true;
          return;
        }
        accepted[source.id] = item;
        delete unavailable[source.id];
      });
      sources.forEach((source) => {
        if (this.healthRequestVersions[source.id] === requestVersions[source.id]) delete checking[source.id];
      });
      this.connectionHealthChecking = checking;
      this.connectionHealth = { ...this.connectionHealth, ...accepted };
      this.connectionHealthUnavailable = unavailable;
    });
  }

  private isConnectionHealthForSource(value: unknown, source: IConnectedSource): value is ISourceConnectionHealth {
    if (!value || typeof value !== 'object') return false;
    const health = value as Partial<ISourceConnectionHealth>;
    return health.sourceId === source.id
      && health.connectorKey === source.connectorKey
      && typeof health.status === 'string'
      && health.status.trim().length > 0
      && typeof health.reason === 'string'
      && typeof health.configured === 'boolean'
      && typeof health.authorized === 'boolean'
      && typeof health.requiresReconnect === 'boolean';
  }

  private refreshConnectionHealth(source: IConnectedSource): void {
    this.selectedHealthSubscription?.unsubscribe();
    if (this.selectedHealthSourceId && this.selectedHealthSourceId !== source.id) {
      const { [this.selectedHealthSourceId]: _previous, ...remainingChecking } = this.connectionHealthChecking;
      this.connectionHealthChecking = remainingChecking;
    }
    this.selectedHealthSourceId = source.id;
    const requestVersion = ++this.healthRequestSequence;
    this.healthRequestVersions[source.id] = requestVersion;
    const { [source.id]: _oldHealth, ...remainingHealth } = this.connectionHealth;
    this.connectionHealth = remainingHealth;
    const { [source.id]: _oldUnavailable, ...remainingUnavailable } = this.connectionHealthUnavailable;
    this.connectionHealthUnavailable = remainingUnavailable;
    this.connectionHealthChecking = { ...this.connectionHealthChecking, [source.id]: true };
    this.selectedHealthSubscription = this.sourceService.connectionHealth(source.id).pipe(
      timeout(this.loadTimeoutMs),
      catchError(() => of(undefined))
    ).subscribe((health) => {
      if (this.healthRequestVersions[source.id] !== requestVersion) return;
      const { [source.id]: _checking, ...remainingChecking } = this.connectionHealthChecking;
      this.connectionHealthChecking = remainingChecking;
      if (this.selectedHealthSourceId === source.id) this.selectedHealthSourceId = '';
      if (!this.isConnectionHealthForSource(health, source)) {
        this.connectionHealthUnavailable = { ...this.connectionHealthUnavailable, [source.id]: true };
        return;
      }
      const { [source.id]: _unavailable, ...remainingUnavailable } = this.connectionHealthUnavailable;
      this.connectionHealthUnavailable = remainingUnavailable;
      this.connectionHealth = { ...this.connectionHealth, [source.id]: health };
    });
  }

  private applySourceDefaults(sources: IConnectedSource[]): void {
    if (!sources.length) {
      return;
    }
    if (!this.selectedSourceId) {
      this.selectedSourceId = sources[0].id;
    }
    if (!this.importForm.value.sourceId) {
      this.importForm.patchValue({ sourceId: sources[0].id });
    }
    if (!this.folderForm.value.sourceId) {
      const localFolder = sources.find((source) => source.connectorKey === 'local-folder');
      this.folderForm.patchValue({ sourceId: (localFolder || sources[0]).id });
    }
    const whatsapp = sources.find((source) => source.connectorKey === 'whatsapp-export');
    if (whatsapp && !this.whatsappForm.value.sourceId) {
      this.whatsappForm.patchValue({ sourceId: whatsapp.id });
    }
    const odoo = sources.find((source) => source.connectorKey === 'odoo-herp');
    if (odoo && !this.odooForm.value.sourceId) {
      this.odooForm.patchValue({ sourceId: odoo.id });
    }
  }
}
