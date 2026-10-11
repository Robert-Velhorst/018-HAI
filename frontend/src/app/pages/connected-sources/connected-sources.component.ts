import { ChangeDetectionStrategy, Component, Inject, OnInit } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { forkJoin, of } from 'rxjs';
import { catchError, finalize, timeout } from 'rxjs/operators';
import {
  IConnectedSource,
  ISourceAuditLog,
  ISourceConnector,
  ISourceConnectionHealth,
  ISourceOverview,
  ISourceExtraction,
  IKnowledgeGraphResult,
  IKnowledgeGraphSourceRef,
  ISourcePursuitRoutingOutcome,
  ISourceSearchResult,
  ISourceSyncJob,
  ISourceSyncResult,
  ISourceLifeGraphProjectionOutcome,
} from '../../models/connected-source.model.interface';
import { CONNECTED_SOURCE_SERVICE_TOKEN } from '../../services/connected-source/connected-source.service.token';
import { IConnectedSourceService } from '../../services/connected-source.service.interface';
import { ThemeMode, ThemeService } from '../../services/theme.service';

type SourceAction =
  | 'connect'
  | 'gmail'
  | 'odoo'
  | 'import'
  | 'folder'
  | 'whatsapp'
  | 'search'
  | 'graph';

interface SourceActionCard {
  id: SourceAction;
  title: string;
  detail: string;
  icon: string;
  metric: string;
  tone: 'blue' | 'green' | 'gold';
}

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-connected-sources',
  templateUrl: './connected-sources.component.html',
  styleUrls: ['./connected-sources.component.scss'],
})
export class ConnectedSourcesComponent implements OnInit {
  connectors: ISourceConnector[] = [];
  sources: IConnectedSource[] = [];
  extractions: ISourceExtraction[] = [];
  auditLogs: ISourceAuditLog[] = [];
  syncJobs: ISourceSyncJob[] = [];
  connectionHealth: Record<string, ISourceConnectionHealth> = {};
  overview?: ISourceOverview;
  searchResult?: ISourceSearchResult;
  knowledgeGraph?: IKnowledgeGraphResult;
  graphLoading = false;
  graphIncludeSensitive = false;
  lastSyncResult?: ISourceSyncResult;
  includeDisabled = false;
  includeArchived = false;
  loading = false;
  extractionHistoryOpen = false;
  activityHistoryOpen = false;
  extractionHistoryLoading = false;
  activityHistoryLoading = false;
  extractionHistoryError = '';
  activityHistoryError = '';
  connecting = false;
  syncing = false;
  selectedAction: SourceAction = 'connect';
  sourceActions: SourceActionCard[] = [];
  selectedSourceId = '';
  themeMode: ThemeMode = 'light';
  private readonly loadTimeoutMs = 6000;
  private readonly operationTimeoutMs = 15000;
  private extractionHistoryLoaded = false;
  private activityHistoryLoaded = false;

  sourceForm: FormGroup = this.fb.group({
    connectorKey: ['local-folder', [Validators.required]],
    name: ['Selected local folder', [Validators.required]],
    localOnly: [true],
    syncFrequency: ['manual'],
    syncTarget: ['', [Validators.required]],
    defaultProjectKey: ['018-HAI'],
    excludePatterns: ['spam,trash'],
  });

  importForm: FormGroup = this.fb.group({
    sourceId: ['', [Validators.required]],
    mode: ['manual_import'],
    projectKey: ['018-HAI'],
    externalId: ['sample-1'],
    title: ['Sample connected source item'],
    sourceUri: ['local://sample/source-item'],
    content: ['Decision: use connected sources as structured context. Follow up: verify extracted context before task planning.', [Validators.required]],
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
    sourceId: [''],
    name: ['WhatsApp exports for Robert'],
    projectKey: ['Robert-life-os'],
    folderPath: ['whatsapp'],
    chatTitle: ['WhatsApp selected chat'],
    pastedExport: [
      '31/05/2026, 09:10 - Robert Velhorst: Kun jij morgen de offerte opvolgen?\n31/05/2026, 09:11 - Contact: Ja, ik moet eerst de documenten controleren.',
    ],
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
    syncFrequency: ['manual'],
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
    private themeService: ThemeService
  ) {}

  ngOnInit(): void {
    this.themeMode = this.themeService.mode();
    this.updateSourceActions();
    this.refresh();
  }

  refresh(): void {
    this.loading = true;
    forkJoin({
      connectors: this.sourceService.connectors().pipe(
        timeout(this.loadTimeoutMs),
        catchError(() => {
          this.notification.error('Connectors unavailable', 'Connector status did not load in time.');
          return of([] as ISourceConnector[]);
        })
      ),
      sources: this.sourceService.sources(this.includeDisabled).pipe(
        timeout(this.loadTimeoutMs),
        catchError(() => {
          this.notification.error('Sources unavailable', 'Connected sources did not load in time.');
          return of([] as IConnectedSource[]);
        })
      ),
      overview: this.sourceService.overview(this.searchForm.value.projectKey, this.includeArchived).pipe(
        timeout(this.loadTimeoutMs),
        catchError(() => {
          this.notification.error('Source overview unavailable', 'Safety and activity totals could not be refreshed.');
          return of(undefined);
        })
      ),
    })
      .pipe(finalize(() => (this.loading = false)))
      .subscribe(({ connectors, sources, overview }) => {
        this.connectors = connectors;
        this.sources = sources;
        this.overview = overview;
        this.applySourceDefaults(sources);
        const selected = this.selectedSource();
        if (selected) {
          this.refreshConnectionHealth(selected);
        } else {
          this.connectionHealth = {};
        }
        if (this.extractionHistoryOpen) {
          this.loadExtractions(true);
        }
        if (this.activityHistoryOpen) {
          this.loadActivityHistory(true);
        }
        this.updateSourceActions();
      });
  }

  connectSource(): void {
    if (this.sourceForm.invalid || this.connecting) {
      return;
    }
    const connector = this.connectors.find(
      (item) => item.connectorKey === this.sourceForm.value.connectorKey
    );
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
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: () => {
          this.notification.success('Source connected', 'The source is ready for controlled sync.');
          this.refresh();
        },
        error: (error) => this.notification.error('Error', error?.error?.error || 'Failed to connect source.'),
      });
  }

  sync(): void {
    if (this.importForm.invalid) {
      return;
    }
    this.syncing = true;
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
        error: () => {
          this.syncing = false;
          this.notification.error('Error', 'Sync failed.');
        },
      });
  }

  setAction(action: SourceAction): void {
    this.selectedAction = action;
    if (action === 'graph' && !this.knowledgeGraph && !this.graphLoading) {
      this.loadKnowledgeGraph();
    }
  }

  setExtractionHistoryOpen(open: boolean): void {
    this.extractionHistoryOpen = open;
    if (open && !this.extractionHistoryLoaded) {
      this.loadExtractions(false);
    }
  }

  setActivityHistoryOpen(open: boolean): void {
    this.activityHistoryOpen = open;
    if (open && !this.activityHistoryLoaded) {
      this.loadActivityHistory(false);
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
        metric: this.overview ? `${this.overview.extractionCount} records` : 'not loaded',
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
    this.selectedSourceId = source.id;
    this.refreshConnectionHealth(source);
  }

  sourceHealth(source?: IConnectedSource): ISourceConnectionHealth | undefined {
    return source ? this.connectionHealth[source.id] : undefined;
  }

  isGoogleSource(source?: IConnectedSource): boolean {
    return source?.connectorKey === 'gmail'
      || source?.connectorKey === 'google-drive'
      || source?.connectorKey === 'google-contacts'
      || source?.connectorKey === 'google-calendar';
  }

  enabledSources(): IConnectedSource[] {
    return this.sources.filter((source) => source.enabled && source.status !== 'revoked');
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
      case 'not_implemented':
        return 'not implemented';
      default:
        return this.statusText(status);
    }
  }

  failedJobCount(): number {
    return this.overview?.failedJobs || 0;
  }

  pendingJobCount(): number {
    return this.overview?.pendingJobs || 0;
  }

  uncertainExtractionCount(): number {
    return this.overview?.uncertainExtractionCount || 0;
  }

  sensitiveExtractionCount(): number {
    return this.overview?.sensitiveExtractionCount || 0;
  }

  recentExtractions(): ISourceExtraction[] {
    return this.extractions.slice(0, 8);
  }

  recentAuditLogs(): ISourceAuditLog[] {
    return this.auditLogs.slice(0, 8);
  }

  recentSyncJobs(): ISourceSyncJob[] {
    return this.syncJobs.slice(0, 6);
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

  latestJobStatusFor(source: IConnectedSource): string {
    return this.syncJobs.find((job) => job.sourceId === source.id)?.status
      || this.overview?.latestJobStatusBySource[source.id]
      || 'no job yet';
  }

  statusText(status?: string): string {
    return (status || 'unknown').replace(/_/g, ' ');
  }

  statusTone(status?: string): string {
    switch ((status || '').toLowerCase()) {
      case 'active':
      case 'completed':
      case 'operational':
        return 'good';
      case 'paused':
      case 'running':
      case 'pending':
      case 'not_configured':
      case 'local_only':
      case 'modeled':
        return 'watch';
      case 'not_implemented':
        return 'bad';
      case 'failed':
      case 'partial_failure':
      case 'revoked':
      case 'error':
        return 'bad';
      default:
        return 'neutral';
    }
  }

  syncButtonVisible(source: IConnectedSource): boolean {
    return source.enabled && source.status !== 'revoked';
  }

  sourceExtractionCount(source: IConnectedSource): number | string {
    return this.overview?.extractionCountsBySource[source.id] ?? (this.overview ? 0 : '—');
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

  connectorChanged(connectorKey: string): void {
    if (connectorKey === 'json-feed') {
      this.sourceForm.patchValue({
        name: 'Local account JSON bridge',
        syncFrequency: '15m',
        syncTarget: 'http://host.docker.internal:8787/feed',
        localOnly: true,
      });
      return;
    }
    if (connectorKey === 'local-folder') {
      this.sourceForm.patchValue({
        name: 'Selected local folder',
        syncFrequency: 'manual',
        syncTarget: '',
        localOnly: true,
      });
      return;
    }
    if (connectorKey === 'email') {
      this.sourceForm.patchValue({
        name: 'Email export folder',
        syncFrequency: 'manual',
        syncTarget: 'email',
        localOnly: true,
        excludePatterns: 'spam,trash',
      });
      return;
    }
    if (connectorKey === 'calendar') {
      this.sourceForm.patchValue({
        name: 'Calendar export folder',
        syncFrequency: 'manual',
        syncTarget: 'calendar',
        localOnly: true,
        excludePatterns: 'cancelled,spam',
      });
      return;
    }
    if (connectorKey === 'cloud-documents') {
      this.sourceForm.patchValue({
        name: 'Synced document folder',
        syncFrequency: '15m',
        syncTarget: 'documents',
        localOnly: true,
        excludePatterns: 'trash,temp,cache',
      });
      return;
    }
    if (connectorKey === 'project-board') {
      this.sourceForm.patchValue({
        name: 'Trello board exports',
        syncFrequency: 'manual',
        syncTarget: 'trello',
        localOnly: true,
        excludePatterns: 'archive,template',
      });
      return;
    }
    if (connectorKey === 'github') {
      this.sourceForm.patchValue({
        name: 'GitHub repository',
        syncFrequency: '1h',
        syncTarget: 'Noodzakelijk-Online/018-HAI',
        localOnly: false,
        excludePatterns: '',
      });
      return;
    }
    if (connectorKey === 'whatsapp-export') {
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
    if (connectorKey === 'whisper-audio') {
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
    if (connectorKey === 'docling-documents') {
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
    if (connectorKey === 'odoo-herp') {
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
    if (this.sourceForm.value.connectorKey === 'odoo-herp') {
      return 'Odoo URL or app list, e.g. https://.../odoo?apps=CRM,Sales';
    }
    return 'Explicit subfolder under the connected-source root, e.g. legal/vivare';
  }

  syncSource(source: IConnectedSource): void {
    if (source.connectorKey === 'whisper-audio') {
      this.transcribeSource(source);
      return;
    }
    if (source.connectorKey === 'docling-documents') {
      this.extractDocuments(source);
      return;
    }
    this.syncing = true;
    this.sourceService
      .sync(source.id, {
        mode: 'incremental_sync',
        items: [],
        projectKey: source.defaultProjectKey,
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (result) => {
          this.syncing = false;
          this.notifySyncResult('Source sync', result);
          this.refresh();
        },
        error: (error) => {
          this.syncing = false;
          this.notification.error('Source sync failed', error?.error?.error || 'The connector could not retrieve records.');
        },
      });
  }

  transcribeSource(source: IConnectedSource): void {
    this.syncing = true;
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
          this.notification.error(
            'Local transcription failed',
            error?.error?.error || 'Check the local runner, reviewed GGML model, and selected audio folder.'
          );
        },
      });
  }

  extractDocuments(source: IConnectedSource): void {
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
          this.notification.error(
            'Local document extraction failed',
            error?.error?.error ||
              'Check the local Docling runner, selected document folder, and pre-provisioned artifacts.'
          );
        },
      });
  }

  sourceActionLabel(source: IConnectedSource): string {
    if (source.connectorKey === 'whisper-audio') {
      return 'Transcribe';
    }
    if (source.connectorKey === 'docling-documents') {
      return 'Extract documents';
    }
    return 'Sync';
  }

  sourceActionTitle(source: IConnectedSource): string {
    if (source.connectorKey === 'whisper-audio') {
      return 'Transcribe the explicit local audio folder. HAI never records a microphone or accepts uploaded audio.';
    }
    if (source.connectorKey === 'docling-documents') {
      return 'Extract text from the explicit local document folder. HAI never chooses cloud services or enables OCR implicitly.';
    }
    return 'Run an incremental sync for this source';
  }

  syncFolder(): void {
    if (this.folderForm.invalid) {
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
        error: () => {
          this.syncing = false;
          this.notification.error('Error', 'Folder sync failed.');
        },
      });
  }

  runDueScheduledSyncs(): void {
    this.syncing = true;
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
      error: () => {
        this.syncing = false;
        this.notification.error('Error', 'Scheduled sync check failed.');
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

  authorizeGoogleSource(source: IConnectedSource): void {
    this.sourceService.startGoogleOAuth(source.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: ({ authorizeUrl }) => {
        this.notification.info('Google authorization opened', 'Approve read-only access, then return here and refresh.');
        window.open(authorizeUrl, '_blank', 'noopener,noreferrer');
      },
      error: (error) =>
        this.notification.error('Authorization failed', error?.error?.error || 'Could not start Google authorization.'),
    });
  }

  private connectGoogleSource(connectorKey: 'gmail' | 'google-drive' | 'google-contacts' | 'google-calendar'): void {
    const connector = this.connectors.find((item) => item.connectorKey === connectorKey);
    const labels: Record<typeof connectorKey, string> = {
      gmail: 'Gmail',
      'google-drive': 'Google Drive',
      'google-contacts': 'Google Contacts',
      'google-calendar': 'Google Calendar',
    };
    const scopes: Record<typeof connectorKey, string> = {
      gmail: 'gmail.readonly',
      'google-drive': 'drive.readonly',
      'google-contacts': 'contacts.readonly',
      'google-calendar': 'calendar.readonly',
    };
    const label = labels[connectorKey];
    if (!connector?.enabled || connector.adapterStatus === 'not_implemented') {
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
        permissions: ['metadata:read', scopes[connectorKey]],
      })
      .pipe(
        timeout(this.operationTimeoutMs),
        finalize(() => (this.connecting = false))
      )
      .subscribe({
        next: (source) => {
          this.authorizeGoogleSource(source);
          this.refresh();
        },
        error: (error) => this.notification.error('Error', error?.error?.error || `Failed to create ${label} source.`),
      });
  }

  connectWhatsAppSource(): void {
    const connector = this.connectors.find((item) => item.connectorKey === 'whatsapp-export');
    if (!connector?.enabled) {
      this.notification.warning('WhatsApp connector unavailable', 'Refresh connectors and verify the backend exposes whatsapp-export as enabled.');
      return;
    }
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
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (source) => {
          this.whatsappForm.patchValue({ sourceId: source.id });
          this.notification.success('WhatsApp source ready', 'Exported chats can now be parsed locally and review-gated.');
          this.refresh();
        },
        error: (error) => this.notification.error('Error', error?.error?.error || 'Failed to connect WhatsApp source.'),
      });
  }

  connectOdooSource(): void {
    const connector = this.connectors.find((item) => item.connectorKey === 'odoo-herp');
    if (!connector?.enabled) {
      this.notification.warning('Odoo connector unavailable', 'Refresh connectors and verify the backend exposes odoo-herp as enabled.');
      return;
    }
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
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (source) => {
          this.odooForm.patchValue({ sourceId: source.id });
          this.notification.success('Odoo / HERP source ready', 'Odoo app domains can now be modeled into governed HAI workflows.');
          this.refresh();
        },
        error: (error) => this.notification.error('Error', error?.error?.error || 'Failed to connect Odoo / HERP source.'),
      });
  }

  syncOdooApps(): void {
    const sourceId = this.selectedOdooSourceId();
    if (!sourceId) {
      this.notification.warning('Odoo source missing', 'Connect or select an Odoo / HERP source first.');
      return;
    }
    this.syncing = true;
    this.sourceService
      .sync(sourceId, {
        mode: 'incremental_sync',
        items: [],
        folderPath: this.odooForm.value.apps || '',
        projectKey: this.odooForm.value.projectKey || 'Robert-life-os',
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (result) => {
          this.syncing = false;
          this.notifySyncResult('Odoo / HERP modeling', result);
          this.refresh();
        },
        error: (error) => {
          this.syncing = false;
          this.notification.error('Odoo modeling failed', error?.error?.error || 'The Odoo app domains could not be modeled.');
        },
      });
  }

  importWhatsAppPaste(): void {
    const sourceId = this.selectedWhatsAppSourceId();
    const content = String(this.whatsappForm.value.pastedExport || '').trim();
    if (!sourceId || !content) {
      this.notification.warning('WhatsApp import missing input', 'Connect a WhatsApp source and paste an exported chat first.');
      return;
    }
    this.syncing = true;
    this.sourceService
      .sync(sourceId, {
        mode: 'manual_import',
        projectKey: this.whatsappForm.value.projectKey,
        limit: Number(this.whatsappForm.value.chunkMessages || 40),
        items: [
          {
            externalId: `whatsapp-manual-${Date.now()}`,
            title: this.whatsappForm.value.chatTitle || 'WhatsApp selected chat',
            content,
            sourceUri: `whatsapp-export://manual/${Date.now()}`,
            itemType: 'whatsapp_export',
            projectKey: this.whatsappForm.value.projectKey,
            metadata: 'source=whatsapp-export;import=manual-paste;privacy=local-review-gated',
          },
        ],
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (result) => {
          this.syncing = false;
          this.notifySyncResult('WhatsApp import', result);
          this.refresh();
        },
        error: (error) => {
          this.syncing = false;
          this.notification.error('WhatsApp import failed', error?.error?.error || 'The pasted export could not be parsed.');
        },
      });
  }

  syncWhatsAppFolder(): void {
    const sourceId = this.selectedWhatsAppSourceId();
    if (!sourceId) {
      this.notification.warning('WhatsApp source missing', 'Connect or select a WhatsApp export source first.');
      return;
    }
    this.syncing = true;
    this.sourceService
      .sync(sourceId, {
        mode: 'incremental_sync',
        items: [],
        folderPath: this.whatsappForm.value.folderPath || 'whatsapp',
        projectKey: this.whatsappForm.value.projectKey || 'Robert-life-os',
        limit: Number(this.whatsappForm.value.chunkMessages || 40),
        maxBytes: Number(this.whatsappForm.value.maxBytes || 2097152),
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: (result) => {
          this.syncing = false;
          this.notifySyncResult('WhatsApp folder scan', result);
          this.refresh();
        },
        error: (error) => {
          this.syncing = false;
          this.notification.error('WhatsApp scan failed', error?.error?.error || 'The export folder could not be scanned.');
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
    const summary = `${result.job.itemsSeen} seen, ${result.job.itemsFailed || 0} failed. ${result.message}`;
    if (result.job.status === 'completed' && !(result.warnings || []).length) {
      this.notification.success(label, summary);
      return;
    }
    const warningCount = (result.warnings || []).length;
    const warningSuffix = warningCount ? ` ${warningCount} graph projection warning(s) are available for review.` : '';
    this.notification.warning(`${label} requires attention`, summary + warningSuffix);
  }

  search(): void {
    if (this.searchForm.invalid) {
      return;
    }
    this.sourceService.search(this.searchForm.value).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (result) => {
        this.searchResult = result;
        this.updateSourceActions();
      },
      error: () => this.notification.error('Error', 'Search failed.'),
    });
  }

  pause(source: IConnectedSource): void {
    this.sourceService.pause(source.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.refresh(),
      error: () => this.notification.error('Pause failed', 'The source could not be paused.'),
    });
  }

  resume(source: IConnectedSource): void {
    this.sourceService.resume(source.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.refresh(),
      error: () => this.notification.error('Resume failed', 'The source could not be resumed.'),
    });
  }

  reindex(source: IConnectedSource): void {
    this.sourceService.reindex(source.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.refresh(),
      error: () => this.notification.error('Re-index failed', 'The source could not be re-indexed.'),
    });
  }

  revoke(source: IConnectedSource): void {
    if (!window.confirm('Revoke this source access?')) {
      return;
    }
    this.sourceService.revoke(source.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.refresh(),
      error: () => this.notification.error('Revoke failed', 'The source access could not be revoked.'),
    });
  }

  archive(extraction: ISourceExtraction): void {
    this.sourceService.archiveExtraction(extraction.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.loadExtractions(),
      error: () => this.notification.error('Archive failed', 'The extracted record could not be archived.'),
    });
  }

  delete(extraction: ISourceExtraction): void {
    if (!window.confirm('Delete this extracted record?')) {
      return;
    }
    this.sourceService.deleteExtraction(extraction.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: () => this.loadExtractions(),
      error: () => this.notification.error('Delete failed', 'The extracted record could not be deleted.'),
    });
  }

  markCorrected(extraction: ISourceExtraction): void {
    this.sourceService
      .updateExtraction(extraction.id, {
        ...extraction,
        uncertain: false,
      })
      .pipe(timeout(this.operationTimeoutMs))
      .subscribe({
        next: () => this.loadExtractions(),
        error: () => this.notification.error('Correction failed', 'The extracted record could not be updated.'),
      });
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }

  loadKnowledgeGraph(): void {
    this.graphLoading = true;
    this.sourceService
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
        error: () =>
          this.notification.error(
            'Knowledge view unavailable',
            'The source-linked candidate view could not load.'
          ),
      });
  }

  knowledgeGraphSourceLabel(refs: IKnowledgeGraphSourceRef[]): string {
    const ref = refs?.[0];
    return ref?.sourceLabel || ref?.sourceUri || 'source linked';
  }

  private loadExtractions(force = true): void {
    if (this.extractionHistoryLoading || (this.extractionHistoryLoaded && !force)) {
      return;
    }
    this.extractionHistoryLoading = true;
    this.extractionHistoryError = '';
    this.sourceService
      .extractions(this.searchForm.value.projectKey, this.includeArchived, 8)
      .pipe(
        timeout(this.loadTimeoutMs),
        finalize(() => (this.extractionHistoryLoading = false))
      )
      .subscribe({
        next: (items) => {
          this.extractions = items;
          this.extractionHistoryLoaded = true;
          this.updateSourceActions();
        },
        error: () => {
          this.extractions = [];
          this.extractionHistoryLoaded = false;
          this.extractionHistoryError = 'Extracted records could not be loaded. Retry when the source service is available.';
          this.updateSourceActions();
        },
      });
  }

  private loadActivityHistory(force = true): void {
    if (this.activityHistoryLoading || (this.activityHistoryLoaded && !force)) {
      return;
    }
    this.activityHistoryLoading = true;
    this.activityHistoryError = '';
    let failed = false;
    forkJoin({
      jobs: this.sourceService.syncJobs(undefined, 6).pipe(
        timeout(this.loadTimeoutMs),
        catchError(() => {
          failed = true;
          return of([] as ISourceSyncJob[]);
        })
      ),
      logs: this.sourceService.auditLogs(undefined, 8).pipe(
        timeout(this.loadTimeoutMs),
        catchError(() => {
          failed = true;
          return of([] as ISourceAuditLog[]);
        })
      ),
    })
      .pipe(finalize(() => (this.activityHistoryLoading = false)))
      .subscribe(({ jobs, logs }) => {
        this.syncJobs = jobs || [];
        this.auditLogs = logs || [];
        this.activityHistoryLoaded = !failed;
        this.activityHistoryError = failed
          ? 'Source activity could not be loaded completely. Retry when the source service is available.'
          : '';
        this.updateSourceActions();
      });
  }

  private refreshConnectionHealth(source: IConnectedSource): void {
    this.sourceService.connectionHealth(source.id).pipe(catchError(() => of(undefined))).subscribe((health) => {
      if (health) {
        this.connectionHealth = { ...this.connectionHealth, [source.id]: health };
      }
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
