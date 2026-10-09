import { FormBuilder } from '@angular/forms';
import { ChangeDetectorRef } from '@angular/core';
import { fakeAsync, TestBed, tick } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { Subject, of, throwError } from 'rxjs';
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { IConnectedSource, ISourceAuditLog, ISourceConnectionHealth, ISourceConnector, ISourceExtraction, ISourceExtractionCorrectionRecovery, ISourceExtractionCorrectionView, ISourceManualSyncJob, ISourcePursuitRoutingOutcome } from '../../models/connected-source.model.interface';
import { CONNECTED_SOURCE_SERVICE_TOKEN } from '../../services/connected-source/connected-source.service.token';
import { IConnectedSourceService } from '../../services/connected-source.service.interface';
import { ThemeService } from '../../services/theme.service';
import { ConnectedSourcesComponent } from './connected-sources.component';
import { ConnectedSourcesModule } from './connected-sources.module';

afterEach(() => localStorage.removeItem('hai.module-view.v1.connected-sources'));
afterEach(() => sessionStorage.removeItem('hai.source-extraction-corrections.v1'));

describe('ConnectedSourcesComponent pursuit handoff', () => {
  function createComponent(): { component: ConnectedSourcesComponent; router: jasmine.SpyObj<Router>; sourceService: jasmine.SpyObj<IConnectedSourceService>; notification: jasmine.SpyObj<NzNotificationService>; changeDetector: jasmine.SpyObj<ChangeDetectorRef>; preferences: ModuleViewPreferencesService } {
    const router = jasmine.createSpyObj<Router>('Router', ['navigate', 'navigateByUrl']);
    Object.defineProperty(router, 'url', { value: '/connected-sources', configurable: true });
    const notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['info', 'success', 'error', 'warning']);
    const sourceService = jasmine.createSpyObj<IConnectedSourceService>('ConnectedSourceService', [
      'createSource', 'sync', 'submitManualSync', 'manualSyncJob', 'transcribe', 'extractDocuments', 'runDueScheduledSyncs',
      'connectors', 'sources', 'extractions', 'submitExtractionCorrection', 'extractionCorrection', 'auditLogs', 'syncJobs', 'connectionHealth', 'connectionHealths', 'startGoogleOAuth',
      'search', 'knowledgeGraph', 'pause', 'resume', 'reindex', 'revoke', 'deleteExtraction'
    ]);
    const changeDetector = jasmine.createSpyObj<ChangeDetectorRef>('ChangeDetectorRef', ['detectChanges']);
    const preferences = new ModuleViewPreferencesService();
    preferences.reset('connected-sources');
    return {
      component: new ConnectedSourcesComponent(
        new FormBuilder(),
		sourceService,
		notification,
        router,
        { mode: () => 'light' } as any,
        preferences,
        changeDetector,
      ),
      router,
	  sourceService,
      notification,
      changeDetector,
      preferences,
    };
  }

  it('defaults to Basic and stores Advanced independently for connected sources', () => {
    const { component, preferences } = createComponent();
    expect(component.isAdvancedView).toBeFalse();

    preferences.setMode('connected-sources', 'advanced');

    expect(component.isAdvancedView).toBeTrue();
    expect(localStorage.getItem('hai.module-view.v1.connected-sources')).toContain('"mode":"advanced"');
    expect(preferences.get('workflow-engine').mode).toBe('basic');
  });

  it('does not label modeled or local-only intake as a verified live connection', () => {
    const { component } = createComponent();
    component.sourcesLoaded = true;
    for (const [key, status] of [['odoo-herp', 'modeled'], ['local-folder', 'local_only']]) {
      const source = connectedSource(`synthetic-${key}`, key);
      component.sources = [source];
      component.connectors = [sourceConnector(key, key, status)];
      component.connectionHealth[source.id] = { ...connectionHealth(source.id, key), status: 'active', configured: true, authorized: true };
      expect(component.sourceDisplayStatus(source)).toBe(status);
      expect(component.connectedSourceMetric()).toBe('0');
      expect(component.configuredSourceMetric()).toBe('1');
      expect(component.sourceHealthSummary(source)).toContain('No live account connection');
    }
  });

  it('fails closed for unknown adapter capability and per-source setup failure even if the catalog is operational', () => {
    const { component } = createComponent();
    const source = connectedSource('synthetic-setup', 'trello');
    allowSourceSync(component, source);
    component.connectors[0].adapterStatus = undefined;
    expect(component.connectorCanConnect(component.connectors[0])).toBeFalse();
    expect(component.sourceSyncCanRun(source)).toBeFalse();
    component.connectors[0].adapterStatus = 'operational';
    component.connectionHealth[source.id] = { ...connectionHealth(source.id, 'trello'), status: 'configuration_required', reason: 'Board target needs review.' };
    expect(component.sourceSyncCanRun(source)).toBeFalse();
    expect(component.sourceSyncUnavailableReason(source)).toBe('Board target needs review.');
    component.connectionHealthUnavailable[source.id] = true;
    expect(component.sourceSyncUnavailableReason(source)).toContain('Retry the access check');
  });

  it('treats the backend active registration state as configured, not live access verification', () => {
    const { component } = createComponent();
    const source = connectedSource('synthetic-new-account', 'github');
    allowSourceSync(component, source);
    component.connectionHealth[source.id] = { ...connectionHealth(source.id, source.connectorKey), status: 'active', configured: true, authorized: true };
    expect(component.sourceDisplayStatus(source)).toBe('configured');
  });

  it('cannot restart a revoked source or begin Google consent while its source is paused or mutating', () => {
    const { component, sourceService } = createComponent();
    const source = connectedSource('synthetic-paused', 'gmail');
    allowSourceSync(component, source);
    component.connectionHealth[source.id] = { ...connectionHealth(source.id, 'gmail'), configured: true, requiresReconnect: true };
    source.enabled = false;
    component.authorizeGoogleSource(source);
    source.enabled = true;
    component.sourceMutationsInProgress[source.id] = 'pause';
    component.authorizeGoogleSource(source);
    source.status = 'revoked';
    component.resume(source);
    expect(sourceService.startGoogleOAuth).not.toHaveBeenCalled();
    expect(sourceService.resume).not.toHaveBeenCalled();
  });

  it('does not create duplicate Google sources while the first creation or authorization is pending', () => {
    const { component, sourceService } = createComponent();
    component.connectors = [sourceConnector('gmail', 'Gmail', 'operational')];
    sourceService.createSource.and.returnValue(new Subject<IConnectedSource>());
    component.connectGmail();
    component.connectGmail();
    expect(sourceService.createSource).toHaveBeenCalledTimes(1);
    component.ngOnDestroy();
  });

  it('prefers the newer authoritative job over a cached failed manual job', () => {
    const { component } = createComponent();
    const source = connectedSource('synthetic-newer-job', 'trello');
    allowSourceSync(component, source);
    const old = manualJob(source.id, { status: 'failed', createdAt: '2026-09-20T10:00:00Z' });
    component.manualSyncJobsBySource[source.id] = old;
    component.latestJobsBySource[source.id] = { ...old, id: 'newer-job', status: 'running', createdAt: '2026-09-30T10:00:00Z' };
    expect(component.sourceDisplayStatus(source)).toBe('syncing');
    expect(component.sourceSyncCanRun(source)).toBeFalse();
    component.latestJobsBySource[source.id] = { ...component.latestJobsBySource[source.id], status: 'completed' };
    component.connectionHealth[source.id] = { ...connectionHealth(source.id, 'trello'), status: 'operational', configured: true };
    expect(component.sourceDisplayStatus(source)).toBe('connected');
    expect(component.sourceActionLabel(source)).toBe('Sync');
    expect(component.sourceSyncCanRun(source)).toBeTrue();
  });

  it('preserves the retry key on malformed or wrong-source acceptance without announcing fake success', () => {
    const { component, sourceService, notification } = createComponent();
    const source = connectedSource('synthetic-bad-ack', 'trello');
    allowSourceSync(component, source);
    sourceService.submitManualSync.and.returnValues(of(null as any), of(manualJob('different-source', { status: 'completed' })));
    component.syncSource(source);
    const key = sourceService.submitManualSync.calls.argsFor(0)[2];
    component.retryManualSyncSafely(source);
    expect(sourceService.submitManualSync.calls.argsFor(1)[2]).toBe(key);
    expect(component.manualSyncRecoveryActions[source.id]).toBe('retry');
    expect(component.manualSyncStatus(source)).toBeUndefined();
    expect(notification.success).not.toHaveBeenCalled();
    sessionStorage.removeItem(`hai.manual-source-sync.v1.${source.id}`);
  });

  it('uses a refreshed terminal record for the same job instead of leaving an exhausted poll stuck running', () => {
    const { component } = createComponent();
    const source = connectedSource('synthetic-terminal-history', 'trello');
    allowSourceSync(component, source);
    const running = manualJob(source.id, { status: 'running' });
    component.manualSyncJobsBySource[source.id] = running;
    component.latestJobsBySource[source.id] = { ...running, status: 'completed' };
    expect(component.manualSyncStatus(source)?.status).toBe('completed');
    expect(component.sourceSyncCanRun(source)).toBeTrue();
  });

  it('rejects a polling result belonging to another job and keeps status recovery read-only', fakeAsync(() => {
    const { component, sourceService, notification } = createComponent();
    const source = connectedSource('synthetic-bad-poll', 'trello');
    const queued = manualJob(source.id);
    component.manualSyncJobsBySource[source.id] = queued;
    sourceService.manualSyncJob.and.returnValue(of({ ...queued, id: 'different-job', status: 'completed' }));
    component.checkManualSyncStatus(source);
    tick(15000);
    expect(component.manualSyncStatusUnavailable[source.id]).toBeTrue();
    expect(component.manualSyncStatus(source)?.status).toBe('queued');
    expect(notification.success).not.toHaveBeenCalled();
    expect(sourceService.submitManualSync).not.toHaveBeenCalled();
    component.ngOnDestroy();
  }));

  it('opens the existing activity disclosure when recovery links target Advanced history', () => {
    const { component, preferences } = createComponent();
    component.openAdvancedAction('connect', 'source-activity');
    expect(preferences.get('connected-sources').openSections['source-activity']).toBeTrue();
    expect(preferences.get('memory').openSections['source-activity']).toBeUndefined();
  });

  const approval = { taskId: 'synthetic-task', approvalSourceId: 'task-review:synthetic-review', approvalBindingDigest: 'a'.repeat(64), idempotencyKey: 'synthetic-source-action-key' };

  it('requires explicit existing approval references before revocation and prevents duplicate submission', () => {
    const { component, sourceService } = createComponent();
    const source = connectedSource('synthetic-revoke', 'trello');
    sourceService.revoke.and.returnValue(new Subject<IConnectedSource>());
    component.revoke(source);
    component.submitDestructiveApproval();
    expect(sourceService.revoke).not.toHaveBeenCalled();
    expect(component.destructiveCanSubmit()).toBeFalse();
    component.destructiveApprovalForm.patchValue(approval);
    component.submitDestructiveApproval();
    component.submitDestructiveApproval();
    component.closeDestructiveApproval();
    expect(component.destructiveTarget?.id).toBe(source.id);
    expect(sourceService.revoke).toHaveBeenCalledOnceWith(source.id, approval);
    expect(component.destructiveSubmitting).toBeTrue();
    expect(component.sourceMutationInProgress(source, 'revoke')).toBeTrue();
    expect(component.sourceSyncCanRun(source)).toBeFalse();
  });

  it('preserves denied approval and emergency-stop recovery without client override or fake success', () => {
    const { component, sourceService, notification } = createComponent();
    const source = connectedSource('synthetic-approval-denied', 'trello');
    for (const status of [403, 423]) {
      sourceService.revoke.and.returnValue(throwError(() => ({ status, error: { error: 'Denied token=private-secret' } })));
      component.revoke(source);
      component.destructiveApprovalForm.patchValue(approval);
      component.submitDestructiveApproval();
      expect(component.destructiveSubmitting).toBeFalse();
      expect(component.destructiveTarget?.id).toBe(source.id);
      expect(component.destructiveError).not.toContain('private-secret');
      expect(component.destructiveError).toContain(status === 423 ? 'Emergency stop' : 'server-approved');
    }
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('does not resend destructive changes after an ambiguous outcome, including reopening the review', () => {
    const { component, sourceService, notification } = createComponent();
    const source = connectedSource('synthetic-unknown-revoke', 'trello');
    sourceService.revoke.and.returnValue(throwError(() => ({ status: 0 })));
    component.revoke(source);
    component.destructiveApprovalForm.patchValue(approval);
    component.submitDestructiveApproval();
    component.closeDestructiveApproval();
    component.revoke(source);
    component.destructiveApprovalForm.patchValue(approval);
    component.submitDestructiveApproval();
    expect(sourceService.revoke).toHaveBeenCalledTimes(1);
    expect(component.destructiveCanSubmit()).toBeFalse();
    expect(notification.success).not.toHaveBeenCalled();
  });

  it('hands destructive error recovery off to the existing Advanced audit disclosure without resubmitting', () => {
    const { component, sourceService, preferences } = createComponent();
    component.revoke(connectedSource('synthetic-recovery', 'trello'));
    component.reviewDestructiveActivity();
    expect(component.destructiveTarget).toBeUndefined();
    expect(preferences.get('connected-sources').mode).toBe('advanced');
    expect(preferences.get('connected-sources').openSections['source-activity']).toBeTrue();
    expect(sourceService.revoke).not.toHaveBeenCalled();
  });

  it('sends the reviewed extraction ID and approval references, not a naked DELETE', () => {
    const { component, sourceService } = createComponent();
    sourceService.deleteExtraction.and.returnValue(new Subject<void>());
    const extraction = extractionRecord('synthetic-reviewed-record');
    component.delete(extraction);
    expect(sourceService.deleteExtraction).not.toHaveBeenCalled();
    component.destructiveApprovalForm.patchValue(approval);
    component.submitDestructiveApproval();
    expect(sourceService.deleteExtraction).toHaveBeenCalledOnceWith(extraction.id, approval);
  });

  it('renders Basic pause and opens an approval-only revocation dialog without sending until submission', async () => {
    const { sourceService, notification, router, preferences } = createComponent();
    const source = connectedSource('synthetic-ui-revoke', 'trello');
    sourceService.connectors.and.returnValue(of([sourceConnector('trello', 'Trello', 'operational')]));
    sourceService.sources.and.returnValue(of([source]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([{ ...connectionHealth(source.id, source.connectorKey), configured: true, status: 'configuration_ready' }]));
    sourceService.pause.and.returnValue(throwError(() => ({ status: 403 })));
    sourceService.revoke.and.returnValue(throwError(() => ({ status: 423, error: { error: 'Emergency stop active' } })));
    await TestBed.configureTestingModule({
      imports: [ConnectedSourcesModule],
      providers: [
        { provide: CONNECTED_SOURCE_SERVICE_TOKEN, useValue: sourceService },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'light', changes$: of('light') } },
        { provide: Router, useValue: router },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents();
    const fixture = TestBed.createComponent(ConnectedSourcesComponent);
    fixture.detectChanges();
    const pause = Array.from(fixture.nativeElement.querySelectorAll('.basic-source__actions button') as NodeListOf<HTMLButtonElement>).find((button) => button.textContent?.trim() === 'Pause')!;
    expect(pause.title).toContain('current job may still finish');
    pause.click();
    expect(sourceService.pause).toHaveBeenCalledOnceWith(source.id);
    preferences.setMode('connected-sources', 'advanced');
    fixture.detectChanges();
    const danger = fixture.debugElement.queryAll(By.directive(HaiProgressiveSectionComponent))
      .find((section) => section.componentInstance.sectionId === 'source-danger-zone')!;
    danger.componentInstance.setOpen(true);
    fixture.detectChanges();
    const review = Array.from(danger.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((button) => button.textContent?.trim() === 'Review revocation')!;
    review.click();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(sourceService.revoke).not.toHaveBeenCalled();
    const dialog = document.querySelector('.ant-modal-content') as HTMLElement;
    expect(dialog.textContent).toContain('Existing approval only');
    expect(dialog.querySelectorAll('input').length).toBe(4);
    expect(dialog.querySelector<HTMLButtonElement>('button[type="submit"]')?.disabled).toBeTrue();
    fixture.componentInstance.destructiveApprovalForm.patchValue(approval);
    fixture.detectChanges();
    dialog.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
    fixture.detectChanges();
    expect(sourceService.revoke).toHaveBeenCalledOnceWith(source.id, approval);
    expect(dialog.textContent).toContain('Emergency stop remains active');
    fixture.componentInstance.closeDestructiveApproval();
    fixture.detectChanges();
    fixture.destroy();
  });

  it('opens Advanced at the requested existing action and persists the module mode', () => {
    const { component, router, preferences } = createComponent();

    component.openAdvancedAction('connect', 'source-operations', 'source-2');

    expect(preferences.get('connected-sources').mode).toBe('advanced');
    expect(component.selectedAction).toBe('connect');
    expect(component.selectedSourceId).toBe('source-2');
    expect(router.navigate).toHaveBeenCalledWith(['/connected-sources'], {
      fragment: 'source-operations',
      queryParamsHandling: 'preserve',
      replaceUrl: true,
    });
  });

  it('submits a revision-bound correction once with a persisted idempotency key', () => {
    const { component, sourceService } = createComponent();
    const extraction = {
      id: 'extraction-1', sourceId: 'source-1', rawItemId: 'raw-1', contentType: 'note', text: 'Evidence',
      summary: 'Summary', sensitive: true, uncertain: true, archived: false, updatedAt: '2026-09-24T00:00:00.123456789Z',
    } as ISourceExtraction;
    sourceService.submitExtractionCorrection.and.returnValue(of(correctionView({ extractionId: extraction.id })));
    sourceService.extractions.and.returnValue(of({ items: [], totalCount: 0, limit: 100 }));

    component.markCorrected(extraction);

    expect(sourceService.submitExtractionCorrection).toHaveBeenCalledTimes(1);
    const [extractionId, patch, ifMatchRevision, idempotencyKey] = sourceService.submitExtractionCorrection.calls.argsFor(0);
    expect(extractionId).toBe('extraction-1');
    expect(patch).toEqual({ uncertain: false });
    expect(ifMatchRevision).toBe(extraction.updatedAt);
    expect(idempotencyKey).toBeTruthy();
    expect(component.extractionCorrectionRecoveries[extraction.id].state).toBe('queued');
    expect(JSON.parse(sessionStorage.getItem('hai.source-extraction-corrections.v1') || '[]')[0].idempotencyKey).toBe(idempotencyKey);
    component.ngOnDestroy();
  });

  it('does not resend a correction after an indeterminate submission response', () => {
    const { component, sourceService } = createComponent();
    const extraction = {
      id: 'extraction-timeout', sourceId: 'source-1', rawItemId: 'raw-1', contentType: 'note', text: 'Evidence',
      sensitive: false, uncertain: true, archived: false, updatedAt: '2026-09-24T00:00:00Z',
    } as ISourceExtraction;
    sourceService.submitExtractionCorrection.and.returnValue(throwError(() => ({ name: 'TimeoutError' })));

    component.markCorrected(extraction);
    component.markCorrected(extraction);

    expect(sourceService.submitExtractionCorrection).toHaveBeenCalledTimes(1);
    expect(sourceService.extractionCorrection).not.toHaveBeenCalled();
    expect(component.extractionCorrectionRecoveries[extraction.id].state).toBe('failed_may_have_saved');
    expect(component.extractionCorrectionMessage(extraction.id)).toContain('will not send it again');
  });

  it('resumes status polling after reload and never repeats the correction PATCH', fakeAsync(() => {
    const { component, sourceService } = createComponent();
    const recovery: ISourceExtractionCorrectionRecovery = {
      version: 1,
      extractionId: 'extraction-reload',
      correctionId: 'correction-reload',
      idempotencyKey: 'stable-idempotency-key',
      ifMatchRevision: '2026-09-24T00:00:00Z',
      state: 'queued',
      status: 'queued',
      statusChecks: 0,
      pollFailures: 0,
      updatedAt: new Date().toISOString(),
    };
    sessionStorage.setItem('hai.source-extraction-corrections.v1', JSON.stringify([recovery]));
    sourceService.extractionCorrection.and.returnValue(of(correctionView({
      id: recovery.correctionId!, extractionId: recovery.extractionId, status: 'verified', phase: 'complete',
      patchSaved: true, recoveryPending: false, appliedRevision: '2026-09-24T00:00:01Z',
    })));
    sourceService.extractions.and.returnValue(of({ items: [], totalCount: 0, limit: 100 }));

    (component as any).restoreExtractionCorrections();
    tick(0);

    expect(sourceService.extractionCorrection).toHaveBeenCalledOnceWith('correction-reload');
    expect(sourceService.submitExtractionCorrection).not.toHaveBeenCalled();
    expect(component.extractionCorrectionRecoveries[recovery.extractionId].state).toBe('applied');
    component.ngOnDestroy();
  }));

  it('renders a server revision conflict as a review state instead of retrying', () => {
    const { component, sourceService } = createComponent();
    const extraction = {
      id: 'extraction-conflict', sourceId: 'source-1', rawItemId: 'raw-1', contentType: 'note', text: 'Evidence',
      sensitive: false, uncertain: true, archived: false, updatedAt: '2026-09-24T00:00:00Z',
    } as ISourceExtraction;
    sourceService.submitExtractionCorrection.and.returnValue(throwError(() => ({ status: 409 })));
    sourceService.extractions.and.returnValue(of({ items: [], totalCount: 0, limit: 100 }));

    component.markCorrected(extraction);

    expect(component.extractionCorrectionRecoveries[extraction.id].state).toBe('conflict');
    expect(sourceService.submitExtractionCorrection).toHaveBeenCalledTimes(1);
    expect(sourceService.extractionCorrection).not.toHaveBeenCalled();
    expect(sourceService.extractions).toHaveBeenCalled();
  });

  it('keeps source Advanced disclosure state module-scoped and closes record controls when selection changes', () => {
    const { component, sourceService, preferences } = createComponent();
    preferences.setSection('connected-sources', 'source-timeline-cooccurrence', true);
    preferences.setSection('connected-sources', 'source-permissions-filters', true);
    preferences.setSection('connected-sources', 'source-danger-zone', true);
    component.selectedSourceId = 'source-a';
    sourceService.connectionHealth.and.returnValue(of(connectionHealth('source-b', 'local-folder')));

    component.selectSource({ id: 'source-b', connectorKey: 'local-folder' } as IConnectedSource);

    expect(component.selectedSourceId).toBe('source-b');
    expect(preferences.get('connected-sources').openSections['source-timeline-cooccurrence']).toBeTrue();
    expect(preferences.get('connected-sources').openSections['source-permissions-filters']).toBeFalse();
    expect(preferences.get('connected-sources').openSections['source-danger-zone']).toBeFalse();
    expect(preferences.get('memory').openSections).toEqual({});
  });

  it('renders source selection as a focusable native button with an announced selected state', async () => {
    const { sourceService, preferences } = createComponent();
    const source = {
      id: 'source-keyboard', name: 'Local documents', connectorKey: 'local-folder',
      category: 'files', status: 'operational', enabled: true, localOnly: true,
      syncTarget: 'access_token=inspector-secret',
    } as IConnectedSource;
    const unknownStateSource = {
      id: 'source-unknown-state', name: 'Unknown state', connectorKey: 'gmail', enabled: undefined,
    } as unknown as IConnectedSource;
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([source, unknownStateSource]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));
    sourceService.connectionHealth.and.returnValue(of(connectionHealth('source-keyboard', 'local-folder')));
    preferences.setMode('connected-sources', 'advanced');

    await TestBed.configureTestingModule({
      imports: [ConnectedSourcesModule],
      providers: [
        { provide: CONNECTED_SOURCE_SERVICE_TOKEN, useValue: sourceService },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', changes$: of('dark'), label: () => 'Dark mode', icon: () => 'star' } },
        { provide: Router, useValue: jasmine.createSpyObj<Router>('Router', ['navigate', 'navigateByUrl']) },
        { provide: NzNotificationService, useValue: jasmine.createSpyObj('NzNotificationService', ['info', 'success', 'error', 'warning']) },
      ],
    }).compileComponents();

    const fixture = TestBed.createComponent(ConnectedSourcesComponent);
    fixture.componentInstance.selectedAction = 'graph';
    fixture.detectChanges();
    const button = fixture.nativeElement.querySelector('.source-row__select') as HTMLButtonElement;

    expect(button).toBeTruthy();
    expect(button.tagName).toBe('BUTTON');
    expect(button.type).toBe('button');
    expect(button.tabIndex).toBe(0);
    expect(button.getAttribute('aria-pressed')).toBe('true');
    expect(button.getAttribute('aria-label')).toContain('Local documents');
    button.focus();
    expect(document.activeElement).toBe(button);
    button.click();
    expect(fixture.componentInstance.selectedSourceId).toBe('source-keyboard');
    expect(fixture.nativeElement.textContent).not.toContain('inspector-secret');

    const sourceButtons = fixture.nativeElement.querySelectorAll('.source-row__select') as NodeListOf<HTMLButtonElement>;
    const unknownButton = Array.from(sourceButtons)
      .find((element) => (element.getAttribute('aria-label') || '').includes('Unknown state')) as HTMLButtonElement;
    expect(unknownButton.getAttribute('aria-label')).toContain('state unknown');
    expect(unknownButton.getAttribute('aria-label')).not.toContain('paused');
    fixture.destroy();
  });

  it('keeps Add source available in Basic with existing sources and labels the connector selector', async () => {
    const { router, sourceService, notification, preferences } = createComponent();
    const source = connectedSource('existing-source', 'local-folder');
    sourceService.connectors.and.returnValue(of([sourceConnector('local-folder', 'Local folder', 'local_only')]));
    sourceService.sources.and.returnValue(of([source]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([connectionHealth(source.id, source.connectorKey)]));

    await TestBed.configureTestingModule({
      imports: [ConnectedSourcesModule],
      providers: [
        { provide: CONNECTED_SOURCE_SERVICE_TOKEN, useValue: sourceService },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'light', changes$: of('light'), label: () => 'Light mode', icon: () => 'star' } },
        { provide: Router, useValue: router },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents();

    const fixture = TestBed.createComponent(ConnectedSourcesComponent);
    fixture.detectChanges();
    const addButton = fixture.nativeElement.querySelector('[data-testid="configure-source-basic"]') as HTMLButtonElement;

    expect(fixture.componentInstance.isAdvancedView).toBeFalse();
    expect(addButton).toBeTruthy();
    expect(fixture.nativeElement.querySelectorAll('[data-testid="source-row"]').length).toBe(0);
    expect(fixture.nativeElement.querySelectorAll('.basic-source').length).toBe(1);

    addButton.click();
    fixture.detectChanges();

    const connector = fixture.nativeElement.querySelector('nz-select[data-testid="source-connector"]') as HTMLElement;
    const connectorInput = connector?.querySelector('#source-connector-control') as HTMLInputElement;
    expect(fixture.componentInstance.isAdvancedView).toBeTrue();
    expect(router.navigate).toHaveBeenCalledWith(['/connected-sources'], {
      fragment: 'source-operations',
      queryParamsHandling: 'preserve',
      replaceUrl: true,
    });
    expect(connector?.getAttribute('aria-label')).toBe('Connector');
    expect(connectorInput).toBeTruthy();
    expect(connectorInput.labels?.[0]?.textContent).toContain('Connector');
    fixture.destroy();
  });

  it('announces initial loading in Advanced and clears the busy state after source data arrives', async () => {
    const { router, sourceService, notification, preferences } = createComponent();
    const pendingSources = new Subject<IConnectedSource[]>();
    preferences.setMode('connected-sources', 'advanced');
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(pendingSources.asObservable());
    sourceService.syncJobs.and.returnValue(of([]));

    await TestBed.configureTestingModule({
      imports: [ConnectedSourcesModule],
      providers: [
        { provide: CONNECTED_SOURCE_SERVICE_TOKEN, useValue: sourceService },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', changes$: of('dark') } },
        { provide: Router, useValue: router },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents();

    const fixture = TestBed.createComponent(ConnectedSourcesComponent);
    fixture.detectChanges();
    const content = fixture.nativeElement.querySelector('.sources-content') as HTMLElement;
    const progress = fixture.nativeElement.querySelector('[data-testid="source-load-progress"]') as HTMLElement;

    expect(content.getAttribute('aria-busy')).toBe('true');
    expect(progress).toBeTruthy();
    expect(progress.textContent).toContain('Loading source data');

    pendingSources.next([]);
    pendingSources.complete();
    fixture.detectChanges();

    expect(sourceService.sources).toHaveBeenCalledTimes(1);
    expect(sourceService.syncJobs).toHaveBeenCalledTimes(1);
    expect(fixture.componentInstance.syncJobsLoaded).toBeTrue();
    expect(fixture.componentInstance.sourcesLoaded).toBeTrue();
    expect(fixture.componentInstance.loading).toBeFalse();
    expect(content.getAttribute('aria-busy')).toBe('false');
    expect(fixture.nativeElement.querySelector('[data-testid="source-load-progress"]')).toBeNull();
    fixture.destroy();
  });

  it('keeps a failed source load distinct from empty, then retries into a confirmed empty state', async () => {
    const { router, sourceService, notification, preferences } = createComponent();
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValues(
      throwError(() => new Error('sources unavailable')),
      of([]),
    );
    sourceService.syncJobs.and.returnValue(of([]));

    await TestBed.configureTestingModule({
      imports: [ConnectedSourcesModule],
      providers: [
        { provide: CONNECTED_SOURCE_SERVICE_TOKEN, useValue: sourceService },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'light', changes$: of('light') } },
        { provide: Router, useValue: router },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents();

    const fixture = TestBed.createComponent(ConnectedSourcesComponent);
    fixture.detectChanges();
    let basicView = fixture.nativeElement.querySelector('[data-testid="sources-basic-view"]') as HTMLElement;

    expect(basicView.textContent).toContain('Source list unavailable');
    expect(basicView.textContent).not.toContain('No connected sources');

    const retry = basicView.querySelector('.basic-empty--warning button') as HTMLButtonElement;
    retry.click();
    fixture.detectChanges();
    basicView = fixture.nativeElement.querySelector('[data-testid="sources-basic-view"]') as HTMLElement;

    expect(sourceService.sources).toHaveBeenCalledTimes(2);
    expect(basicView.textContent).toContain('No connected sources');
    expect(basicView.textContent).not.toContain('Source list unavailable');
    fixture.destroy();
  });

  it('renders the Odoo safety checkbox without nested labels', async () => {
    const { router, sourceService, notification, preferences } = createComponent();
    preferences.setMode('connected-sources', 'advanced');
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));

    await TestBed.configureTestingModule({
      imports: [ConnectedSourcesModule],
      providers: [
        { provide: CONNECTED_SOURCE_SERVICE_TOKEN, useValue: sourceService },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'dark', changes$: of('dark') } },
        { provide: Router, useValue: router },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents();

    const fixture = TestBed.createComponent(ConnectedSourcesComponent);
    fixture.componentInstance.selectedAction = 'odoo';
    fixture.detectChanges();

    const field = fixture.nativeElement.querySelector('.checkbox-field') as HTMLElement;
    const checkbox = field?.querySelector('label.ant-checkbox-wrapper') as HTMLLabelElement;
    expect(field.tagName).toBe('DIV');
    expect(field.querySelectorAll('label label').length).toBe(0);
    expect(checkbox.textContent).toContain('Local-first source records');
    fixture.destroy();
  });

  it('distinguishes unverified, checking, unavailable, and consent-required Google state', () => {
    const { component } = createComponent();
    const source = { id: 'gmail-1', connectorKey: 'gmail', status: 'operational' } as IConnectedSource;

    expect(component.sourceHealthSummary(source)).toContain('has not been returned');
    component.connectionHealthChecking[source.id] = true;
    expect(component.sourceHealthSummary(source)).toContain('Checking Google');
    component.connectionHealthChecking = {};
    component.connectionHealthUnavailable[source.id] = true;
    expect(component.sourceHealthSummary(source)).toContain('could not be checked');
    component.connectionHealthUnavailable = {};
    component.connectionHealth[source.id] = {
      sourceId: source.id,
      connectorKey: source.connectorKey,
      status: 'operational',
      reason: 'Consent expired.',
      configured: true,
      authorized: false,
      requiresReconnect: true,
    };

    expect(component.googleAuthorizationRequired(source)).toBeTrue();
    expect(component.sourceNeedsAttention(source)).toBeTrue();
    expect(component.sourceHealthSummary(source)).toBe('Consent expired.');
  });

  it('keeps missing Google health in the unverified count instead of calling it an attention failure', () => {
    const { component } = createComponent();
    const source = { id: 'gmail-unknown', connectorKey: 'gmail' } as IConnectedSource;
    component.sources = [source];

    expect(component.sourceNeedsAttention(source)).toBeFalse();
    expect(component.unverifiedSourceCount()).toBe(1);

    component.connectionHealthUnavailable[source.id] = true;
    expect(component.sourceNeedsAttention(source)).toBeTrue();
    expect(component.unverifiedSourceCount()).toBe(0);
  });

  it('uses backend-derived Trello health rather than a stale source status label', () => {
    const { component } = createComponent();
    const source = { id: 'trello-board', connectorKey: 'trello', status: 'operational', enabled: true } as IConnectedSource;
    component.connectionHealth[source.id] = {
      sourceId: source.id,
      connectorKey: 'trello',
      status: 'previously_verified',
      reason: 'Run a fresh read-only sync to verify current board access.',
      configured: true,
      authorized: false,
      requiresReconnect: false,
    };

    expect(component.sourceHealthTone(source)).toBe('watch');
    expect(component.sourceHealthSummary(source)).toBe('Run a fresh read-only sync to verify current board access.');
    expect(component.sourceDisplayStatus(source)).toBe('stale');
    expect(component.sourceHealthSummary(source)).not.toContain('operational');
    expect(component.sourceNeedsAttention(source)).toBeTrue();
  });

  it('maps connected, configured, syncing, failed, stale, disconnected, and unavailable states distinctly', () => {
    const { component } = createComponent();
    const source = connectedSource('source-state', 'local-folder');
    component.sources = [source];
    allowSourceSync(component, source);
    component.connectionHealth[source.id] = {
      ...connectionHealth(source.id, source.connectorKey),
      status: 'operational',
      configured: true,
    };
    expect(component.sourceDisplayStatus(source)).toBe('connected');

    component.connectionHealth[source.id] = {
      ...component.connectionHealth[source.id],
      status: 'configuration_ready',
      configured: true,
    };
    expect(component.sourceDisplayStatus(source)).toBe('configured');

    component.manualSyncJobsBySource[source.id] = {
      id: 'sync-running', sourceId: source.id, mode: 'manual_async_sync', status: 'running',
      itemsSeen: 0, itemsAdded: 0, itemsUpdated: 0, itemsFailed: 0,
      createdAt: new Date().toISOString(), attempt: 1, maxAttempts: 5,
    };
    expect(component.sourceDisplayStatus(source)).toBe('syncing');
    delete component.manualSyncJobsBySource[source.id];

    component.syncJobs = [{
      id: 'sync-failed', sourceId: source.id, mode: 'incremental', status: 'failed',
      itemsSeen: 0, itemsAdded: 0, itemsUpdated: 0, itemsFailed: 1,
    }];
    (component as any).rebuildSourceIndexes();
    expect(component.sourceDisplayStatus(source)).toBe('failed');

    component.syncJobs = [];
    (component as any).rebuildSourceIndexes();
    component.connectionHealth[source.id] = {
      ...component.connectionHealth[source.id], status: 'stale', reason: 'Needs a fresh check.',
    };
    expect(component.sourceDisplayStatus(source)).toBe('stale');

    component.connectionHealth[source.id] = {
      ...component.connectionHealth[source.id], status: 'disconnected', reason: 'Connection is unavailable.',
    };
    expect(component.sourceDisplayStatus(source)).toBe('disconnected');
    expect(component.statusText(component.sourceDisplayStatus(source))).toBe('disconnected');
    expect(component.sourceHealthTone(source)).toBe('bad');
    expect(component.sourceNeedsAttention(source)).toBeTrue();
    expect(component.sourceSyncCanRun(source)).toBeFalse();
    expect(component.sourceSyncUnavailableReason(source)).toBe('Connection is unavailable.');
  });

  it('maps backend sync-health states without relying on the separate job-history endpoint', () => {
    const { component } = createComponent();
    const source = connectedSource('source-health-sync', 'trello');
    component.sources = [source];
    component.syncJobsUnavailable = true;
    allowSourceSync(component, source);

    for (const [backendStatus, expectedStatus, blockedMessage] of [
      ['sync_queued', 'syncing', 'already queued'],
      ['sync_running', 'syncing', 'already running'],
    ]) {
      component.connectionHealth[source.id] = {
        ...connectionHealth(source.id, source.connectorKey),
        status: backendStatus,
        configured: true,
        reason: `Backend reports ${backendStatus}.`,
      };
      expect(component.sourceDisplayStatus(source)).toBe(expectedStatus);
      expect(component.sourceHealthSummary(source)).toContain(`Backend reports ${backendStatus}.`);
      expect(component.sourceSyncCanRun(source)).toBeFalse();
      expect(component.sourceSyncUnavailableReason(source)).toContain(blockedMessage);
    }

    for (const [backendStatus, expectedStatus] of [
      ['sync_failed', 'failed'],
      ['sync_partial_failure', 'partial_failure'],
      ['sync_cancelled', 'failed'],
    ]) {
      component.connectionHealth[source.id] = {
        ...connectionHealth(source.id, source.connectorKey),
        status: backendStatus,
        configured: true,
        reason: `Backend reports ${backendStatus}.`,
      };
      expect(component.sourceDisplayStatus(source)).toBe(expectedStatus);
      expect(component.sourceNeedsAttention(source)).toBeTrue();
      expect(component.sourceHealthSummary(source)).toContain(`Backend reports ${backendStatus}.`);
      expect(component.sourceActionLabel(source)).toBe('Retry sync');
      expect(component.sourceSyncCanRun(source)).toBeTrue();
    }
  });

  it('treats partial sync failures as attention, retryable errors, and failed activity', () => {
    const { component, sourceService, notification } = createComponent();
    const source = connectedSource('source-partial-status', 'local-folder');
    const partialJob = {
      id: 'sync-partial-status', sourceId: source.id, mode: 'manual_async_sync', status: 'partial_failure',
      itemsSeen: 3, itemsAdded: 2, itemsUpdated: 0, itemsFailed: 1, message: 'One record failed.',
    } as ISourceManualSyncJob;
    component.sources = [source];
    component.sourcesLoaded = true;
    component.connectionHealth[source.id] = {
      ...connectionHealth(source.id, source.connectorKey), status: 'operational', configured: true,
    };
    component.syncJobs = [partialJob];
    (component as any).rebuildSourceIndexes();

    expect(component.sourceDisplayStatus(source)).toBe('partial_failure');
    expect(component.sourceHealthTone(source)).toBe('bad');
    expect(component.sourceNeedsAttention(source)).toBeTrue();
    expect(component.attentionSourceCountMetric()).toBe('1');
    expect(component.failedJobCount()).toBe(1);
    expect(component.failedSyncSources()).toEqual([source]);
    expect(component.sourceHealthSummary(source)).toBe('One record failed.');
    expect(component.sourceActionLabel(source)).toBe('Retry sync');
    component.activityTypeFilter = 'failed';
    expect(component.filteredSyncJobs()).toEqual([partialJob]);

    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));
    (component as any).acceptManualSyncJob(partialJob);

    expect(notification.error).toHaveBeenCalledWith('Source sync needs attention', 'One record failed.');
    expect(notification.info).not.toHaveBeenCalledWith('Source sync queued', jasmine.any(String));
    component.ngOnDestroy();
  });

  it('normalizes sync_partial_failure health as a failed attention state', () => {
    const { component } = createComponent();
    const source = connectedSource('source-health-partial', 'local-folder');
    component.sources = [source];
    component.connectionHealth[source.id] = {
      ...connectionHealth(source.id, source.connectorKey),
      status: 'sync_partial_failure',
      reason: 'Some records were not processed.',
      configured: true,
    };

    expect(component.statusTone('sync_partial_failure')).toBe('bad');
    expect(component.sourceDisplayStatus(source)).toBe('partial_failure');
    expect(component.sourceHealthTone(source)).toBe('bad');
    expect(component.sourceNeedsAttention(source)).toBeTrue();
    expect(component.sourceHealthSummary(source)).toBe('Some records were not processed.');

    source.enabled = false;
    expect(component.sourceDisplayStatus(source)).toBe('partial_failure');
    expect(component.sourceNeedsAttention(source)).toBeTrue();
  });

  it('blocks unsupported sync with a visible, sanitized reason and never submits it', () => {
    const { component, sourceService, notification } = createComponent();
    const source = connectedSource('source-unsupported', 'trello');
    component.connectorCatalogLoaded = true;
    component.connectors = [sourceConnector('trello', 'Trello', 'not_implemented')];
    component.connectors[0].statusReason = 'Adapter unavailable; access_token=provider-secret';

    expect(component.sourceDisplayStatus(source)).toBe('unavailable');
    expect(component.sourceSyncCanRun(source)).toBeFalse();
    expect(component.sourceSyncUnavailableReason(source)).toContain('Adapter unavailable');
    expect(component.sourceSyncUnavailableReason(source)).not.toContain('provider-secret');

    component.syncSource(source);

    expect(sourceService.submitManualSync).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalledWith('Sync unavailable', jasmine.stringContaining('Adapter unavailable'));
  });

  it('shows why an unconfigured Trello connector cannot be created without exposing credentials', () => {
    const { component, sourceService } = createComponent();
    const trello = sourceConnector('trello', 'Trello', 'configuration_required');
    trello.statusReason = 'Trello credentials are not configured; api_key=private-value';
    component.connectors = [trello];
    component.sourceForm.patchValue({ connectorKey: 'trello' });
    component.connectorChanged('trello');

    const message = component.selectedConnectorReadinessMessage();
    expect(message).toContain('setup required');
    expect(message).toContain('Trello credentials are not configured');
    expect(message).not.toContain('private-value');
    expect(component.sourceCanConnect()).toBeFalse();

    component.connectSource();
    expect(sourceService.createSource).not.toHaveBeenCalled();
  });

  it('blocks Trello sync when the connector is in error or reconnect-required state', () => {
    const { component, sourceService } = createComponent();
    const source = connectedSource('trello-adapter-state', 'trello');
    component.sources = [source];
    component.connectorCatalogLoaded = true;

    for (const adapterStatus of ['error', 'reconnect_required']) {
      component.connectors = [sourceConnector('trello', 'Trello', adapterStatus)];
      expect(component.sourceDisplayStatus(source)).toBe('unavailable');
      expect(component.sourceSyncCanRun(source)).toBeFalse();
      component.syncSource(source);
    }

    expect(sourceService.submitManualSync).not.toHaveBeenCalled();
  });

  it('honors Trello Retry-After and reuses the same key on a deliberate retry', fakeAsync(() => {
    const { component, sourceService } = createComponent();
    const source = connectedSource('trello-rate-limit', 'trello');
    allowSourceSync(component, source);
    component.connectors[0].name = 'Trello';
    sourceService.submitManualSync.and.returnValues(
      throwError(() => ({
        status: 429,
        headers: { get: (name: string) => name.toLowerCase() === 'retry-after' ? '12' : null },
        error: { error: 'Provider rate limit reached.' },
      })),
      throwError(() => ({ status: 503 })),
    );

    component.syncSource(source);

    const requestKey = sessionStorage.getItem('hai.manual-source-sync.v1.trello-rate-limit');
    expect(requestKey).toBeTruthy();
    expect(component.manualSyncRecoveryMessages[source.id]).toContain('Trello rate-limited this sync request');
    expect(component.manualSyncRecoveryMessages[source.id]).toContain('12 seconds');
    expect(component.manualSyncRateLimited(source)).toBeTrue();
    expect(component.sourceSyncCanRun(source)).toBeFalse();

    component.retryManualSyncSafely(source);
    expect(sourceService.submitManualSync).toHaveBeenCalledTimes(1);

    tick(12000);
    expect(component.manualSyncRateLimited(source)).toBeFalse();
    component.retryManualSyncSafely(source);

    expect(sourceService.submitManualSync).toHaveBeenCalledTimes(2);
    expect(sourceService.submitManualSync.calls.argsFor(0)[2]).toBe(requestKey!);
    expect(sourceService.submitManualSync.calls.argsFor(1)[2]).toBe(requestKey!);
    component.ngOnDestroy();
  }));

  it('keeps Trello Retry-After in force after the page is reloaded', fakeAsync(() => {
    const source = connectedSource('trello-reload-limit', 'trello');
    const retryAtKey = 'hai.manual-source-sync.retry-after.v1.' + source.id;
    const idempotencyKey = 'hai.manual-source-sync.v1.' + source.id;
    sessionStorage.removeItem(retryAtKey);
    sessionStorage.removeItem(idempotencyKey);

    const first = createComponent();
    allowSourceSync(first.component, source);
    first.sourceService.submitManualSync.and.returnValue(throwError(() => ({
      status: 429,
      headers: { get: (name: string) => name.toLowerCase() === 'retry-after' ? '20' : null },
    })));
    first.component.syncSource(source);

    const storedDeadline = sessionStorage.getItem(retryAtKey);
    const storedRequestKey = sessionStorage.getItem(idempotencyKey);
    expect(Number(storedDeadline)).toBeGreaterThan(Date.now());
    expect(storedRequestKey).toBeTruthy();
    first.component.ngOnDestroy();

    const reloaded = createComponent();
    reloaded.sourceService.connectors.and.returnValue(of([sourceConnector('trello', 'Trello', 'operational')]));
    reloaded.sourceService.sources.and.returnValue(of([source]));
    reloaded.sourceService.syncJobs.and.returnValue(of([]));
    reloaded.sourceService.connectionHealths.and.returnValue(of([]));
    reloaded.component.refresh();

    expect(reloaded.component.sourceSyncCanRun(source)).toBeFalse();
    expect(reloaded.component.sourceSyncUnavailableReason(source)).toContain('rate-limited');
    reloaded.component.retryManualSyncSafely(source);
    expect(reloaded.sourceService.submitManualSync).not.toHaveBeenCalled();

    tick(20000);

    expect(reloaded.component.sourceSyncCanRun(source)).toBeTrue();
    expect(sessionStorage.getItem(retryAtKey)).toBeNull();
    expect(sessionStorage.getItem(idempotencyKey)).toBe(storedRequestKey);
    reloaded.component.ngOnDestroy();
    sessionStorage.removeItem(idempotencyKey);
  }));

  it('turns a failed source job into an executable retry action', () => {
    const { component, sourceService } = createComponent();
    const source = connectedSource('source-failed-retry', 'trello');
    allowSourceSync(component, source);
    component.syncJobs = [{
      id: 'sync-failed-retry', sourceId: source.id, mode: 'manual_async_sync', status: 'failed',
      itemsSeen: 1, itemsAdded: 0, itemsUpdated: 0, itemsFailed: 1,
    }];
    (component as any).rebuildSourceIndexes();
    sourceService.submitManualSync.and.returnValue(throwError(() => ({ status: 503 })));

    expect(component.sourceActionLabel(source)).toBe('Retry sync');
    expect(component.sourceSyncCanRun(source)).toBeTrue();
    component.syncSource(source);

    expect(sourceService.submitManualSync).toHaveBeenCalledTimes(1);
    component.ngOnDestroy();
  });

  it('explains catalog and concurrent-operation gates instead of leaving sync silently disabled', () => {
    const { component } = createComponent();
    const source = connectedSource('source-gated', 'trello');
    component.connectorCatalogUnavailable = true;
    expect(component.sourceSyncUnavailableReason(source)).toContain('could not be loaded');

    component.connectorCatalogUnavailable = false;
    component.connectorCatalogLoaded = true;
    component.connectors = [sourceConnector('trello', 'Trello', 'operational')];
    component.syncing = true;
    expect(component.sourceSyncCanRun(source)).toBeFalse();
    expect(component.sourceSyncUnavailableReason(source)).toContain('Another source operation is running');
  });

  it('redacts authorization material from operational text', () => {
    const { component } = createComponent();
    const sanitized = component.safeOperationalText(
      'Authorization: Bearer bearer-secret access_token=oauth-secret code=oauth-code eyJabcdefghijk.abcdefghijk.abcdefghijk'
    );

    expect(sanitized).not.toContain('bearer-secret');
    expect(sanitized).not.toContain('oauth-secret');
    expect(sanitized).not.toContain('oauth-code');
    expect(sanitized).not.toContain('eyJabcdefghijk');
    expect(sanitized).toContain('[redacted]');
  });

  it('redacts provider credentials from sync notifications', () => {
    const { component, notification } = createComponent();
    (component as any).notifySyncResult('Local import', {
      job: { status: 'completed', itemsSeen: 1, itemsFailed: 0 },
      message: 'Imported. access_token=provider-secret',
      warnings: [],
      errors: [],
    });

    expect(notification.success).toHaveBeenCalledWith(
      'Local import',
      '1 seen, 0 failed. Imported. access_token=[redacted]'
    );
    expect(notification.success.calls.mostRecent().args[1]).not.toContain('provider-secret');
  });

  it('shows revoked sources as revoked instead of implying their connection is usable', () => {
    const { component } = createComponent();
    const source = { id: 'revoked-source', connectorKey: 'trello', status: 'revoked', enabled: true } as IConnectedSource;

    expect(component.sourceDisplayStatus(source)).toBe('revoked');
    expect(component.sourceEnabledLabel(source)).toBe('revoked');
    expect(component.sourceHealthSummary(source)).toContain('has been revoked');
  });

  it('does not count a revoked source as enabled when provider status casing differs', () => {
    const { component } = createComponent();
    const source = { ...connectedSource('uppercase-revoked', 'trello'), status: 'REVOKED' };
    component.sources = [source];

    expect(component.sourceDisplayStatus(source)).toBe('revoked');
    expect(component.enabledSources()).toEqual([]);
  });

  it('treats backend-confirmed health revocation as revoked and blocks source actions', () => {
    const { component } = createComponent();
    const source = { ...connectedSource('health-revoked', 'gmail'), status: 'operational', enabled: true };
    component.sources = [source];
    component.connectionHealth[source.id] = {
      ...connectionHealth(source.id, source.connectorKey),
      status: 'revoked',
      configured: true,
      authorized: false,
    };

    expect(component.sourceDisplayStatus(source)).toBe('revoked');
    expect(component.sourceEnabledLabel(source)).toBe('revoked');
    expect(component.enabledSources()).toEqual([]);
    expect(component.syncButtonVisible(source)).toBeFalse();
    expect(component.sourceSyncCanRun(source)).toBeFalse();
    expect(component.googleAuthorizationRequired(source)).toBeFalse();
  });

  it('does not describe unavailable or loading source records as an empty activity or extraction list', () => {
    const { component } = createComponent();
    component.syncJobsUnavailable = true;
    component.auditLogsLoaded = true;
    expect(component.sourceActivityEmptyText()).toContain('could not be loaded');
    expect(component.sourceActivityCountSummary()).toContain('Sync jobs unavailable');

    component.syncJobsUnavailable = false;
    component.syncJobsLoaded = true;
    component.syncJobs = [];
    component.auditLogs = [];
    expect(component.sourceActivityEmptyText()).toBe('No source activity has been recorded.');
    expect(component.sourceActivityCountSummary()).toBe('0 of 0 sync jobs · 0 of 0 audit events');

    component.extractionsLoaded = false;
    component.extractionTotalCount = 0;
    expect(component.extractionsEmptyText()).toContain('not available yet');
    expect(component.extractionScopeSummary()).toBe('Extracted record counts are unavailable.');
    expect(component.extractionCountSummary()).toBe('Extracted record counts unavailable');
    component.extractionsLoading = true;
    expect(component.extractionCountSummary()).toContain('Loading');
    component.extractionsLoading = false;
    component.extractionsLoaded = true;
    expect(component.extractionsEmptyText()).toBe('No extracted records have been recorded.');
    expect(component.extractionScopeSummary()).toContain('API reports 0 total');
    component.extractionTotalCount = 2;
    expect(component.extractionsEmptyText()).toContain('API reports extracted records');
  });

  it('restarts the canceled initial load after source creation and verifies fresh source state', () => {
    const { component, sourceService } = createComponent();
    const connector = sourceConnector('local-folder', 'Local folder', 'local_only');
    const created = { ...connectedSource('created-during-refresh', 'local-folder'), name: 'Documents' };
    const initialSources = new Subject<IConnectedSource[]>();
    sourceService.connectors.and.returnValue(of([connector]));
    sourceService.sources.and.returnValues(initialSources.asObservable(), of([created]));
    sourceService.syncJobs.and.returnValues(of([]), of([]));
    sourceService.connectionHealths.and.returnValue(of([{
      ...connectionHealth(created.id, created.connectorKey),
      status: 'operational',
      configured: true,
    }]));
    sourceService.createSource.and.returnValue(of(created));
    component.sourceForm.patchValue({ syncTarget: 'documents' });

    component.refresh();
    expect(component.loading).toBeTrue();
    expect(component.sourcesLoaded).toBeFalse();
    component.connectSource();

    expect(sourceService.sources).toHaveBeenCalledTimes(2);
    expect(component.loading).toBeFalse();
    expect(component.sourcesLoaded).toBeTrue();
    expect(component.syncJobsLoaded).toBeTrue();
    expect(component.sources.map((source) => source.id)).toEqual([created.id]);
    expect(component.sourceDisplayStatus(created)).toBe('local_only');
    expect(component.connectionHealthChecking[created.id]).toBeUndefined();
    expect(component.sourceCountMetric()).toBe('1');

    initialSources.next([]);
    initialSources.complete();
    expect(component.sources.map((source) => source.id)).toEqual([created.id]);
    component.ngOnDestroy();
  });

  it('prevents duplicate source mutations and reports sanitized source-specific failures', () => {
    const { component, sourceService, notification } = createComponent();
    const source = { ...connectedSource('trello-mutation', 'trello'), name: 'Project board' };
    const pending = new Subject<IConnectedSource>();
    sourceService.pause.and.returnValue(pending.asObservable());

    component.pause(source);
    component.pause(source);

    expect(sourceService.pause).toHaveBeenCalledOnceWith(source.id);
    expect(component.sourceMutationInProgress(source, 'pause')).toBeTrue();
    expect(component.sourceSyncCanRun(source)).toBeFalse();
    expect(component.sourceSyncUnavailableReason(source)).toContain('setting is being updated');

    pending.error({ status: 403, error: { error: 'Trello denied access. access_token=private-value' } });

    expect(component.sourceMutationInProgress(source)).toBeFalse();
    expect(notification.error).toHaveBeenCalledWith(
      'Pause failed',
      jasmine.stringContaining('Project board: Trello denied access. access_token=[redacted]')
    );
    expect(notification.error.calls.mostRecent().args[1]).not.toContain('private-value');
  });

  it('clears stale health while a new batch is pending and leaves missing results unverified', () => {
    const { component, sourceService } = createComponent();
    const source = { id: 'gmail-source', connectorKey: 'gmail' } as IConnectedSource;
    component.sources = [source];
    const pending = new Subject<ISourceConnectionHealth[]>();
    component.connectionHealth[source.id] = {
      sourceId: source.id,
      connectorKey: 'gmail',
      status: 'ready',
      reason: 'Old result',
      configured: true,
      authorized: true,
      requiresReconnect: false,
    };
    sourceService.connectionHealths.and.returnValue(pending.asObservable());

    (component as any).loadConnectionHealth([source]);

    expect(component.sourceHealth(source)).toBeUndefined();
    expect(component.sourceHealthChecking(source)).toBeTrue();

    pending.next([]);
    pending.complete();

    expect(component.sourceHealthChecking(source)).toBeFalse();
    expect(component.sourceHealthUnavailable(source)).toBeFalse();
    expect(component.sourceNeedsAttention(source)).toBeFalse();
    expect(component.unverifiedSourceCount()).toBe(1);
  });

  it('clears old connection health and marks existing sources as checking as soon as refresh starts', () => {
    const { component, sourceService } = createComponent();
    const source = connectedSource('source-refresh-health', 'local-folder');
    const pendingSources = new Subject<IConnectedSource[]>();
    component.sources = [source];
    component.connectionHealth[source.id] = {
      ...connectionHealth(source.id, source.connectorKey), status: 'operational', configured: true,
    };
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(pendingSources.asObservable());
    sourceService.syncJobs.and.returnValue(of([]));

    expect(component.sourceDisplayStatus(source)).toBe('connected');
    component.refresh();

    expect(component.sourceHealth(source)).toBeUndefined();
    expect(component.sourceHealthUnavailable(source)).toBeFalse();
    expect(component.sourceHealthChecking(source)).toBeTrue();
    expect(component.sourceDisplayStatus(source)).toBe('checking');
    component.ngOnDestroy();
  });

  it('does not show the confirmed-empty source message when the source list failed to load', () => {
    const { component, sourceService } = createComponent();
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(throwError(() => new Error('sources unavailable')));
    sourceService.syncJobs.and.returnValue(of([]));

    component.refresh();

    expect(component.sourceRegistryEmptyText()).toContain('could not be loaded');
    expect(component.sourceRegistryEmptyText()).not.toBe('No connected sources yet.');
  });

  it('does not report a partially failed sync as successful', () => {
    const { component, notification } = createComponent();

    (component as any).notifySyncResult('Source sync', {
      job: { status: 'completed', itemsSeen: 2, itemsFailed: 1 },
      extractions: [],
      message: 'One record could not be processed.',
      errors: ['one record failed'],
      warnings: [],
    });

    expect(notification.success).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalled();
  });

  it('prevents duplicate Odoo and WhatsApp sync requests while a source operation is running', () => {
    const { component, sourceService } = createComponent();
    const odooPending = new Subject<any>();
    sourceService.sync.and.returnValue(odooPending.asObservable());
    component.odooForm.patchValue({ sourceId: 'odoo-source' });
    component.whatsappForm.patchValue({
      sourceId: 'whatsapp-source',
      chatTitle: 'Existing chat export',
      pastedExport: '2026-01-01, Robert: Example',
    });

    component.syncOdooApps();
    component.syncOdooApps();
    expect(sourceService.sync).toHaveBeenCalledTimes(1);

    odooPending.error(new Error('finish operation'));
    const whatsappPending = new Subject<any>();
    sourceService.sync.and.returnValue(whatsappPending.asObservable());
    component.syncing = false;
    component.importWhatsAppPaste();
    component.importWhatsAppPaste();
    expect(sourceService.sync).toHaveBeenCalledTimes(2);

    whatsappPending.complete();
    const folderPending = new Subject<any>();
    sourceService.sync.and.returnValue(folderPending.asObservable());
    component.whatsappForm.patchValue({ folderPath: 'whatsapp' });
    component.syncWhatsAppFolder();
    component.syncWhatsAppFolder();
    expect(sourceService.sync).toHaveBeenCalledTimes(3);
  });

  it('cancels an obsolete source search so old results cannot replace the latest query', () => {
    const { component, sourceService } = createComponent();
    const first = new Subject<any>();
    const second = new Subject<any>();
    sourceService.search.and.returnValues(first.asObservable(), second.asObservable());
    component.searchForm.patchValue({ query: 'first query' });

    component.search();
    component.searchForm.patchValue({ query: 'latest query' });
    component.search();
    first.next({ query: 'first query', usedContext: [], explanation: 'stale' });
    second.next({ query: 'latest query', usedContext: [], explanation: 'current' });
    second.complete();

    expect(component.searchResult?.query).toBe('latest query');
    expect(component.searchLoading).toBeFalse();
  });

  it('clears old graph data and reports a failed candidate-view refresh', () => {
    const { component, sourceService } = createComponent();
    component.knowledgeGraph = {
      projectKey: 'old-project', status: 'candidate_only', extractionCount: 1,
      sensitiveExcluded: 0, entities: [], relationships: [], timeline: [], warnings: [],
    };
    sourceService.knowledgeGraph.and.returnValue(throwError(() => new Error('graph unavailable')));

    component.loadKnowledgeGraph();

    expect(component.knowledgeGraph).toBeUndefined();
    expect(component.graphError).toContain('could not load');
    expect(component.graphLoading).toBeFalse();
  });

  it('offers quick Google setup only for configured connectors without an existing source', () => {
    const { component } = createComponent();
    component.connectors = [
      { connectorKey: 'gmail', name: 'Gmail', enabled: true, adapterStatus: 'operational' } as any,
      { connectorKey: 'google-drive', name: 'Drive', enabled: true, adapterStatus: 'operational' } as any,
      { connectorKey: 'google-calendar', name: 'Calendar', enabled: true, adapterStatus: 'configuration_required' } as any,
    ];
    component.sources = [{ id: 'drive-1', connectorKey: 'google-drive' } as IConnectedSource];

    expect(component.quickGoogleConnectors().map((connector) => connector.connectorKey)).toEqual(['gmail']);
  });

  it('filters exact loaded source, extraction, sync, and audit records without changing the underlying lists', () => {
    const { component } = createComponent();
    const unknownStateSource = {
      id: 'source-unknown-state', name: 'Unknown state', connectorKey: 'unknown-connector', enabled: undefined,
    } as unknown as IConnectedSource;
    component.sources = [
      { id: 'source-1', name: 'Gmail archive', connectorKey: 'gmail', enabled: true } as IConnectedSource,
      { id: 'source-2', name: 'Local notes', connectorKey: 'local-folder', enabled: false } as IConnectedSource,
      unknownStateSource,
    ];
    component.sourceFilterQuery = 'gmail';
    expect(component.filteredSources().map((source) => source.id)).toEqual(['source-1']);
    component.sourceFilterQuery = '';
    component.sourceEnabledFilter = 'paused';
    expect(component.filteredSources().map((source) => source.id)).toEqual(['source-2']);
    expect(component.sourceEnabledLabel(unknownStateSource)).toBe('state unknown');
    component.sourceEnabledFilter = 'enabled';
    expect(component.filteredSources().map((source) => source.id)).toEqual(['source-1']);

    component.extractions = [
      { id: 'ext-1', sourceId: 'source-1', text: 'hearing date', summary: 'Hearing' } as any,
      { id: 'ext-2', sourceId: 'source-2', text: 'garden plan', summary: 'Garden' } as any,
    ];
    component.extractionFilterQuery = 'hearing';
    expect(component.filteredExtractions().map((record) => record.id)).toEqual(['ext-1']);

    component.syncJobs = [
      { id: 'job-1', sourceId: 'source-1', mode: 'incremental', status: 'failed', message: 'timeout' } as any,
      { id: 'job-2', sourceId: 'source-2', mode: 'incremental', status: 'completed', message: 'done' } as any,
    ];
    component.auditLogs = [{ id: 'audit-1', action: 'authorize', sourceId: 'source-1', message: 'Consent updated' } as any];
    component.activityTypeFilter = 'failed';
    expect(component.filteredSyncJobs().map((job) => job.id)).toEqual(['job-1']);
    expect(component.filteredAuditLogs()).toEqual([]);
    component.activityTypeFilter = 'audit';
    component.activityFilterQuery = 'consent';
    expect(component.filteredAuditLogs().map((log) => log.id)).toEqual(['audit-1']);
    expect(component.syncJobs.length).toBe(2);
    expect(component.auditLogs.length).toBe(1);
  });

  it('opens the candidate pursuit returned by a source sync', () => {
    const { component, router } = createComponent();
    const outcome: ISourcePursuitRoutingOutcome = {
      extractionId: 'extraction-1',
      pursuitId: '68882979-1333-4b14-bd46-16fd5806c9c5',
      status: 'candidate_pending',
      message: 'Imported source candidate awaits explicit acceptance.',
    };

    component.lastSyncResult = {
      job: {} as any,
      extractions: [],
      pursuitOutcomes: [outcome],
      message: 'sync complete',
    };
    component.openPursuitOutcome(outcome);

    expect(component.pursuitRoutingOutcomes()).toEqual([outcome]);
    expect(component.pursuitRoutingLabel(outcome)).toBe('Decision needed');
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: { selected: outcome.pursuitId } });
  });

  it('opens the pursuits inbox when a routing repair has no pursuit ID', () => {
    const { component, router } = createComponent();
    component.openPursuitOutcome({ status: 'routing_deferred', message: 'Router repair is required.' });

    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: undefined });
  });

  it('opens the private graph inspector for projected source records', () => {
    const { component, router } = createComponent();
    component.lastSyncResult = {
      job: {} as any,
      extractions: [],
      message: 'sync complete',
      lifeGraphProjections: [{
        extractionId: 'extraction-1', documentId: 'life-document-1', linkedEntityIds: ['source-1'],
        relationIds: ['relation-1'], alreadyExisted: false, advisoryOnly: true,
        canExecute: false, grantsAuthority: false,
      }],
    };

    component.openLifeGraph();

    expect(component.lifeGraphProjections().length).toBe(1);
    expect(router.navigate).toHaveBeenCalledWith(['/governance-control']);
  });

  it('recognizes all read-only Google source types', () => {
    const { component } = createComponent();
    expect(component.isGoogleSource({ connectorKey: 'gmail' } as IConnectedSource)).toBeTrue();
    expect(component.isGoogleSource({ connectorKey: 'google-drive' } as IConnectedSource)).toBeTrue();
    expect(component.isGoogleSource({ connectorKey: 'google-contacts' } as IConnectedSource)).toBeTrue();
    expect(component.isGoogleSource({ connectorKey: 'google-calendar' } as IConnectedSource)).toBeTrue();
    expect(component.isGoogleSource({ connectorKey: 'github' } as IConnectedSource)).toBeFalse();
  });

  it('reports Google readiness independently from other live connectors', () => {
    const { component } = createComponent();
    component.connectors = [
      { connectorKey: 'github', enabled: true, adapterStatus: 'operational' } as any,
      { connectorKey: 'gmail', enabled: true, adapterStatus: 'not_implemented' } as any,
      { connectorKey: 'google-drive', enabled: true, adapterStatus: 'not_implemented' } as any,
      { connectorKey: 'google-contacts', enabled: true, adapterStatus: 'not_implemented' } as any,
      { connectorKey: 'google-calendar', enabled: true, adapterStatus: 'not_implemented' } as any,
    ];

    expect(component.googleConnectorMetric()).toBe('setup needed');

    component.connectors[1].adapterStatus = 'operational';
    expect(component.googleConnectorMetric()).toBe('1/4 ready');

    component.connectors[2].adapterStatus = 'operational';
    expect(component.googleConnectorMetric()).toBe('2/4 ready');

    component.connectors[3].adapterStatus = 'operational';
    expect(component.googleConnectorMetric()).toBe('3/4 ready');

    component.connectors[4].adapterStatus = 'operational';
    expect(component.googleConnectorMetric()).toBe('4 ready');
  });

  it('does not offer connector creation before required setup is complete', () => {
		const { component } = createComponent();
		expect(component.connectorCanConnect({ enabled: true, adapterStatus: 'configuration_required' } as any)).toBeFalse();
		expect(component.connectorCanConnect({ enabled: true, adapterStatus: 'not_implemented' } as any)).toBeFalse();
		expect(component.connectorCanConnect({ enabled: true, adapterStatus: 'operational' } as any)).toBeTrue();
		expect(component.adapterStatusLabel('configuration_required')).toBe('setup required');
	});

  it('keeps Google connection controls disabled until that connector is configured', () => {
    const { component } = createComponent();
    component.connectors = [
      { connectorKey: 'gmail', enabled: true, adapterStatus: 'configuration_required' } as any,
      { connectorKey: 'google-drive', enabled: true, adapterStatus: 'operational' } as any,
    ];

    expect(component.googleConnectorCanConnect('gmail')).toBeFalse();
    expect(component.googleConnectorCanConnect('google-drive')).toBeTrue();
  });

  it('uses a reliable same-tab handoff for Google authorization', () => {
    const { component, sourceService } = createComponent();
    const source = { id: 'source-id', connectorKey: 'gmail', status: 'operational', enabled: true } as IConnectedSource;
    component.connectors = [{ connectorKey: 'gmail', enabled: true, adapterStatus: 'operational' } as any];
    component.connectionHealth[source.id] = {
      sourceId: source.id,
      connectorKey: 'gmail',
      status: 'ready',
      reason: 'Read-only consent is needed.',
      configured: true,
      authorized: false,
      requiresReconnect: false,
    };
    sourceService.startGoogleOAuth.and.returnValue(of({ authorizeUrl: 'https://accounts.google.test/authorize' }));
    const navigate = spyOn<any>(component, 'navigateToGoogleAuthorization');

    component.authorizeGoogleSource(source);

    expect(sourceService.startGoogleOAuth).toHaveBeenCalledWith('source-id');
    expect(navigate).toHaveBeenCalledWith('https://accounts.google.test/authorize');
  });

  it('does not start Google OAuth without confirmed health or start duplicate authorization requests', () => {
    const { component, sourceService } = createComponent();
    const source = { id: 'source-id', connectorKey: 'gmail', enabled: true } as IConnectedSource;
    component.connectors = [{ connectorKey: 'gmail', enabled: true, adapterStatus: 'operational' } as any];
    sourceService.startGoogleOAuth.and.returnValue(new Subject<any>().asObservable());

    component.authorizeGoogleSource(source);
    expect(sourceService.startGoogleOAuth).not.toHaveBeenCalled();

    component.connectionHealth[source.id] = {
      sourceId: source.id,
      connectorKey: 'gmail',
      status: 'ready',
      reason: '',
      configured: true,
      authorized: false,
      requiresReconnect: false,
    };
    component.authorizeGoogleSource(source);
    component.authorizeGoogleSource(source);

    expect(sourceService.startGoogleOAuth).toHaveBeenCalledTimes(1);
    expect(component.authorizing).toBeTrue();
  });

  it('keeps health recovery available and continues through the existing Google reconnect endpoint', async () => {
    const { sourceService, notification, router, preferences } = createComponent();
    const source = connectedSource('gmail-recovery', 'gmail');
    sourceService.connectors.and.returnValue(of([
      sourceConnector('gmail', 'Gmail', 'operational'),
    ]));
    sourceService.sources.and.returnValue(of([source]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(throwError(() => new Error('batch health unavailable')));
    sourceService.connectionHealth.and.returnValue(of({
      ...connectionHealth(source.id, source.connectorKey),
      status: 'reconnect_required',
      configured: true,
      authorized: false,
      requiresReconnect: true,
      reason: 'Google rejected the stored authorization.',
    }));
    sourceService.startGoogleOAuth.and.returnValue(of({ authorizeUrl: 'https://accounts.google.test/authorize' }));

    await TestBed.configureTestingModule({
      imports: [ConnectedSourcesModule],
      providers: [
        { provide: CONNECTED_SOURCE_SERVICE_TOKEN, useValue: sourceService },
        { provide: ModuleViewPreferencesService, useValue: preferences },
        { provide: ThemeService, useValue: { mode: () => 'light', changes$: of('light'), label: () => 'Light mode', icon: () => 'star' } },
        { provide: Router, useValue: router },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents();

    const fixture = TestBed.createComponent(ConnectedSourcesComponent);
    fixture.detectChanges();
    const component = fixture.componentInstance;
    expect(component.sourceHealthUnavailable(source)).toBeTrue();

    const buttons = (): HTMLButtonElement[] => Array.from(
      fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>
    );
    const retryHealth = buttons().find((button) => button.textContent?.trim() === 'Retry access check');
    expect(retryHealth).toBeTruthy();
    retryHealth!.click();
    fixture.detectChanges();

    expect(sourceService.connectionHealth).toHaveBeenCalledWith(source.id);
    expect(component.sourceHealth(source)?.requiresReconnect).toBeTrue();
    const reconnect = buttons().find((button) => button.textContent?.trim() === 'Reconnect Google');
    expect(reconnect).toBeTruthy();

    const navigateToAuthorization = spyOn<any>(component, 'navigateToGoogleAuthorization');
    reconnect!.click();
    fixture.detectChanges();
    expect(sourceService.startGoogleOAuth).toHaveBeenCalledWith(source.id);
    expect(navigateToAuthorization).toHaveBeenCalledWith('https://accounts.google.test/authorize');
    fixture.destroy();
  });

  it('does not offer Google authorization when backend health says OAuth configuration is missing', () => {
    const { component } = createComponent();
    const source = connectedSource('gmail-not-configured', 'gmail');
    component.connectionHealth[source.id] = {
      ...connectionHealth(source.id, source.connectorKey),
      status: 'configuration_required',
      configured: false,
      authorized: false,
      requiresReconnect: true,
    };

    expect(component.googleAuthorizationRequired(source)).toBeFalse();
    expect(component.canAuthorizeGoogleSource(source)).toBeFalse();
    expect(component.sourceDisplayStatus(source)).toBe('setup required');
  });

  it('surfaces a failed Google reconnect request and clears its in-progress state', () => {
    const { component, sourceService, notification } = createComponent();
    const source = connectedSource('gmail-reconnect-error', 'gmail');
    component.connectors = [sourceConnector('gmail', 'Gmail', 'operational')];
    component.connectionHealth[source.id] = {
      ...connectionHealth(source.id, source.connectorKey),
      configured: true,
      authorized: false,
      requiresReconnect: true,
    };
    sourceService.startGoogleOAuth.and.returnValue(throwError(() => ({
      status: 503,
      error: { error: 'Authorization service unavailable.' },
    })));

    component.authorizeGoogleSource(source);

    expect(sourceService.startGoogleOAuth).toHaveBeenCalledWith(source.id);
    expect(component.authorizing).toBeFalse();
    expect(component.operationError).toContain('Authorization service unavailable.');
    expect(notification.error).toHaveBeenCalledWith('Authorization failed', component.operationError);
  });

  it('records HAI normalized read permissions before Gmail OAuth starts', () => {
    const { component, sourceService } = createComponent();
    component.connectors = [{ connectorKey: 'gmail', category: 'email', enabled: true, adapterStatus: 'operational' } as any];
    sourceService.createSource.and.returnValue(of(connectedSource('gmail-source', 'gmail')));
    sourceService.startGoogleOAuth.and.returnValue(of({ authorizeUrl: 'https://accounts.google.test/authorize' }));
    spyOn<any>(component, 'navigateToGoogleAuthorization');
    spyOn(component, 'refresh');

    component.connectGmail();

    expect(sourceService.createSource).toHaveBeenCalledWith(jasmine.objectContaining({
      connectorKey: 'gmail',
      permissions: ['metadata:read', 'email:read'],
    }));
  });

  it('uses remote read-only defaults for a Trello board source', () => {
    const { component } = createComponent();

    component.sourceForm.patchValue({ connectorKey: 'trello' });
    component.connectorChanged('trello');

    expect(component.sourceForm.value).toEqual(jasmine.objectContaining({
      name: 'Trello board (read-only)',
      syncFrequency: '1h',
      syncTarget: '',
      defaultProjectKey: '',
      localOnly: false,
    }));
    expect(component.syncTargetPlaceholder()).toContain('Trello board URL or ID');
  });

  it('creates a read-only Trello board source without claiming access is verified', () => {
    const { component, sourceService } = createComponent();
    const trello = sourceConnector('trello', 'Trello', 'operational');
    trello.supportedModes = 'manual,scheduled_sync';
    component.connectors = [trello];
    component.sourceForm.patchValue({ connectorKey: 'trello' });
    component.connectorChanged('trello');
    component.sourceForm.patchValue({ syncTarget: 'https://trello.com/b/abc12345/board-name' });
    const created = { ...connectedSource('trello-created', 'trello'), status: 'operational' };
    sourceService.createSource.and.returnValue(of(created));

    component.connectSource();

    expect(sourceService.createSource).toHaveBeenCalledWith(jasmine.objectContaining({
      connectorKey: 'trello',
      enabled: true,
      localOnly: false,
      syncFrequency: '1h',
      syncTarget: 'https://trello.com/b/abc12345/board-name',
      defaultProjectKey: '',
    }));
    expect(sourceService.createSource.calls.mostRecent().args[0].permissions).toBeUndefined();
    expect(component.sourceDisplayStatus(created)).toBe('unverified');
    expect(component.sourceHealthSummary(created)).toContain('health has not been returned');
  });

  it('uses a cautious status tone for a previously verified Trello connection', () => {
    const { component } = createComponent();
    expect(component.statusText('previously_verified')).toBe('previously verified');
    expect(component.statusTone('previously_verified')).toBe('watch');
  });

  it('keeps unsupported source schedules out of the connect request', () => {
    const { component } = createComponent();

    component.sourceForm.patchValue({ syncFrequency: 'after-lunch' });
    expect(component.sourceForm.invalid).toBeTrue();

    component.sourceForm.patchValue({ syncFrequency: '30s' });
    expect(component.sourceForm.invalid).toBeTrue();

    component.sourceForm.patchValue({ syncFrequency: '1h30m' });
    expect(component.sourceForm.valid).toBeTrue();
  });

  it('keeps manual-only connector schedules out of the connect request', () => {
    const { component } = createComponent();
    component.connectors = [{
      connectorKey: 'docling-documents', enabled: true, adapterStatus: 'local_only',
      supportedModes: 'manual_import', category: 'document',
    } as any];
    component.sourceForm.patchValue({
      connectorKey: 'docling-documents', syncFrequency: 'hourly', syncTarget: 'legal/vivare', localOnly: true,
    });

    expect(component.selectedConnectorSupportsScheduledSync()).toBeFalse();
    expect(component.sourceCanConnect()).toBeFalse();

    component.sourceForm.patchValue({ syncFrequency: 'manual' });
    expect(component.sourceCanConnect()).toBeTrue();
  });

  it('uses the same bounded schedule validation for Odoo source setup', () => {
    const { component } = createComponent();

    component.odooForm.patchValue({ syncFrequency: 'sometimes' });
    expect(component.odooForm.invalid).toBeTrue();

    component.odooForm.patchValue({ syncFrequency: 'daily' });
    expect(component.odooForm.valid).toBeTrue();
  });

  it('requires an explicit folder instead of defaulting local intake to the root', () => {
    const { component } = createComponent();
    component.connectors = [{ connectorKey: 'local-folder', enabled: true, adapterStatus: 'local_only' } as any];

    component.connectorChanged('local-folder');

    expect(component.sourceForm.value.syncTarget).toBe('');
    expect(component.sourceCanConnect()).toBeFalse();
  });

  it('preserves in-progress source inputs when the selected connector emits a duplicate change event', () => {
    const { component } = createComponent();
    component.connectors = [{ connectorKey: 'local-folder', enabled: true, adapterStatus: 'local_only' } as any];
    component.sourceForm.patchValue({
      name: 'E2E local source',
      syncTarget: 'e2e',
    });

    component.connectorChanged('local-folder');

    expect(component.sourceForm.value).toEqual(jasmine.objectContaining({
      name: 'E2E local source',
      syncTarget: 'e2e',
    }));
    expect(component.sourceCanConnect()).toBeTrue();
  });

  it('applies connector defaults through the reactive form control without ngModel events', () => {
    const { component, sourceService } = createComponent();
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.extractions.and.returnValue(of({ items: [], totalCount: 0, limit: 100 }));
    sourceService.auditLogs.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));
    component.ngOnInit();

    component.sourceForm.patchValue({ connectorKey: 'github' });

    expect(component.sourceForm.value).toEqual(jasmine.objectContaining({
      connectorKey: 'github',
      name: 'GitHub repository',
      syncTarget: 'Noodzakelijk-Online/018-HAI',
      localOnly: false,
    }));
    component.ngOnDestroy();
  });

  it('does not prefill a manual import with synthetic source content', () => {
    const { component } = createComponent();

    expect(component.importForm.invalid).toBeTrue();
    expect(component.importForm.value).toEqual(jasmine.objectContaining({
      externalId: '',
      title: '',
      sourceUri: '',
      content: '',
    }));
  });

  it('keeps actionable server errors while removing control characters', () => {
    const { component } = createComponent();

    expect((component as any).operationErrorMessage(
      { error: { error: 'Trello credentials are not configured\nSet the read-only token.' } },
      'fallback'
    )).toBe('Trello credentials are not configured Set the read-only token.');
  });

  it('explains a timed-out operation without exposing a raw transport error', () => {
    const { component } = createComponent();

    expect((component as any).operationErrorMessage({ name: 'TimeoutError' }, 'fallback'))
      .toBe('No response within 15 seconds. Check system status and retry.');
  });

  it('requires a real selected WhatsApp export before it can be imported', () => {
    const { component, sourceService } = createComponent();

    component.importWhatsAppPaste();

    expect(component.whatsappForm.invalid).toBeTrue();
    expect(sourceService.sync).not.toHaveBeenCalled();
  });

	it('marks generic source creation as in progress until the request completes', () => {
		const { component, sourceService } = createComponent();
		component.connectors = [{ connectorKey: 'local-folder', enabled: true, adapterStatus: 'local_only', category: 'local_folder' } as any];
		component.sourceForm.patchValue({ connectorKey: 'local-folder', syncTarget: 'projects/018-hai' });
		const pending = new Subject<any>();
		sourceService.createSource.and.returnValue(pending.asObservable());
		component.connectSource();
		expect(sourceService.createSource).toHaveBeenCalled();
		expect(component.connecting).toBeTrue();
		pending.complete();
		expect(component.connecting).toBeFalse();
	});

  it('keeps a server-confirmed source visible while preserving existing sources', () => {
    const { component, sourceService, changeDetector } = createComponent();
    const created = {
      id: 'created-source',
      connectorKey: 'local-folder',
      name: 'New local source',
    } as IConnectedSource;
    component.sources = [{ id: 'existing-source', name: 'Existing source' } as IConnectedSource];
    component.connectors = [{ connectorKey: 'local-folder', enabled: true, adapterStatus: 'local_only', category: 'local_folder' } as any];
    component.sourceForm.patchValue({ connectorKey: 'local-folder', syncTarget: 'projects/018-hai' });
    sourceService.createSource.and.returnValue(of(created));
    sourceService.connectionHealths.and.returnValue(of([]));

    component.connectSource();

    expect(component.sources.map((source) => source.id)).toEqual(['created-source', 'existing-source']);
    expect(component.selectedSourceId).toBe('created-source');
    expect(changeDetector.detectChanges).toHaveBeenCalled();
  });

  it('does not start duplicate source work while another sync is running', () => {
    const { component, sourceService } = createComponent();
    const pending = new Subject<any>();
    sourceService.sync.and.returnValue(pending.asObservable());
    sourceService.transcribe.and.returnValue(pending.asObservable());
    sourceService.extractDocuments.and.returnValue(pending.asObservable());
    sourceService.runDueScheduledSyncs.and.returnValue(pending.asObservable());
    component.importForm.patchValue({ sourceId: 'import-source' });
    component.folderForm.patchValue({ sourceId: 'folder-source' });
    component.syncing = true;

    component.sync();
    component.syncFolder();
    component.runDueScheduledSyncs();
    component.syncSource({ id: 'github-source', connectorKey: 'github' } as IConnectedSource);
    component.syncSource({ id: 'audio-source', connectorKey: 'whisper-audio' } as IConnectedSource);
    component.syncSource({ id: 'document-source', connectorKey: 'docling-documents' } as IConnectedSource);

    expect(sourceService.sync).not.toHaveBeenCalled();
    expect(sourceService.transcribe).not.toHaveBeenCalled();
    expect(sourceService.extractDocuments).not.toHaveBeenCalled();
    expect(sourceService.runDueScheduledSyncs).not.toHaveBeenCalled();
  });

  it('retries an uncertain acceptance with the same idempotency key', () => {
    const { component, sourceService } = createComponent();
    const source = { id: 'source-retry', connectorKey: 'trello', defaultProjectKey: 'vivare', enabled: true } as IConnectedSource;
    allowSourceSync(component, source);
    const completed: ISourceManualSyncJob = {
      id: 'sync-retry', sourceId: source.id, mode: 'manual_async_sync', status: 'failed',
      itemsSeen: 0, itemsAdded: 0, itemsUpdated: 0, itemsFailed: 1,
      message: 'Provider request failed; the cursor was retained.', createdAt: new Date().toISOString(),
      attempt: 1, maxAttempts: 5,
    };
    sourceService.submitManualSync.and.returnValues(
      throwError(() => ({ status: 0 })),
      of(completed),
    );
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));

    component.syncSource(source);
    const storedKey = sessionStorage.getItem('hai.manual-source-sync.v1.source-retry');
    expect(storedKey).toBeTruthy();
    expect(component.manualSyncRecoveryActions[source.id]).toBe('retry');

    component.retryManualSyncSafely(source);

    expect(sourceService.submitManualSync.calls.argsFor(0)[1]).toEqual({});
    expect(sourceService.submitManualSync.calls.argsFor(1)[1]).toEqual({});
    expect(sourceService.submitManualSync.calls.argsFor(0)[2]).toBe(storedKey!);
    expect(sourceService.submitManualSync.calls.argsFor(1)[2]).toBe(storedKey!);
    expect(sessionStorage.getItem('hai.manual-source-sync.v1.source-retry')).toBeNull();
    expect(component.manualSyncStatus(source)?.status).toBe('failed');
  });

  it('polls an accepted job to completion and reports success only after verified completion', fakeAsync(() => {
    const { component, sourceService, notification } = createComponent();
    const source = { id: 'source-poll', connectorKey: 'trello', enabled: true } as IConnectedSource;
    allowSourceSync(component, source);
    const queued: ISourceManualSyncJob = {
      id: 'sync-poll', sourceId: source.id, mode: 'manual_async_sync', status: 'queued',
      itemsSeen: 0, itemsAdded: 0, itemsUpdated: 0, itemsFailed: 0,
      message: 'Queued for background sync.', createdAt: new Date().toISOString(), attempt: 0, maxAttempts: 5,
    };
    const completed: ISourceManualSyncJob = {
      ...queued, status: 'completed', itemsSeen: 4, itemsAdded: 4,
      message: 'Sync completed.', completedAt: new Date().toISOString(), attempt: 1,
    };
    sourceService.submitManualSync.and.returnValue(of(queued));
    sourceService.manualSyncJob.and.returnValue(of(completed));
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));

    component.syncSource(source);
    expect(component.manualSyncStatus(source)?.status).toBe('queued');
    tick(0);

    expect(sourceService.manualSyncJob).toHaveBeenCalledWith(queued.id);
    expect(component.manualSyncStatus(source)?.status).toBe('completed');
    expect(notification.success).toHaveBeenCalledWith('Source sync completed', 'Sync completed.');
    expect(sourceService.sync).not.toHaveBeenCalled();
    component.ngOnDestroy();
  }));

  it('does not report a completed response with item failures as success', () => {
    const { component, sourceService, notification } = createComponent();
    const source = { id: 'source-partial', connectorKey: 'trello', enabled: true } as IConnectedSource;
    allowSourceSync(component, source);
    sourceService.submitManualSync.and.returnValue(of({
      id: 'sync-partial', sourceId: source.id, mode: 'manual_async_sync', status: 'completed',
      itemsSeen: 3, itemsAdded: 2, itemsUpdated: 0, itemsFailed: 1,
      message: 'One record failed; the cursor was retained.', createdAt: new Date().toISOString(), attempt: 1, maxAttempts: 5,
    } as ISourceManualSyncJob));
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));

    component.syncSource(source);

    expect(notification.success).not.toHaveBeenCalled();
    expect(notification.error).toHaveBeenCalledWith('Source sync needs attention', 'One record failed; the cursor was retained.');
  });

  it('defers extraction and audit reads until their Advanced sections open', () => {
    const { component, sourceService } = createComponent();
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.extractions.and.returnValue(throwError(() => new Error('extractions unavailable')));
    sourceService.auditLogs.and.returnValue(throwError(() => new Error('audit unavailable')));
    sourceService.syncJobs.and.returnValue(throwError(() => new Error('jobs unavailable')));

    component.refresh();

    expect(sourceService.extractions).not.toHaveBeenCalled();
    expect(sourceService.auditLogs).not.toHaveBeenCalled();
    expect(component.loadWarnings).toEqual(['Sync jobs']);

    component.onExtractionsSectionOpen(true);
    component.onActivitySectionOpen(true);

    expect(sourceService.extractions).toHaveBeenCalledTimes(1);
    expect(sourceService.auditLogs).toHaveBeenCalledTimes(1);
    expect(component.loadWarnings).toContain('Extracted records');
    expect(component.loadWarnings).toContain('Audit history');
    expect(component.hasLoadWarnings()).toBeTrue();
  });

  it('loads restored-open Advanced source sections during refresh only in that module', () => {
    const { component, sourceService, preferences } = createComponent();
    preferences.setMode('connected-sources', 'advanced');
    preferences.setSection('connected-sources', 'source-records', true);
    preferences.setSection('connected-sources', 'source-activity', true);
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.extractions.and.returnValue(of({ items: [extractionRecord('record-1')], totalCount: 1, limit: 100 }));
    sourceService.auditLogs.and.returnValue(of([auditLog('audit-1')]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));

    component.refresh();

    expect(sourceService.extractions).toHaveBeenCalledTimes(1);
    expect(sourceService.auditLogs).toHaveBeenCalledTimes(1);
    expect(component.extractionsLoaded).toBeTrue();
    expect(component.auditLogsLoaded).toBeTrue();
    expect(preferences.get('memory').mode).toBe('basic');
  });

  it('loads the connector catalog before unrelated source records finish loading', () => {
    const { component, sourceService } = createComponent();
    const delayedSources = new Subject<IConnectedSource[]>();
    sourceService.connectors.and.returnValue(of([
      sourceConnector('local-folder', 'Local folder', 'local_only'),
    ]));
    sourceService.sources.and.returnValue(delayedSources.asObservable());
    sourceService.extractions.and.returnValue(of({ items: [], totalCount: 0, limit: 100 }));
    sourceService.auditLogs.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));

    component.refresh();
    component.sourceForm.patchValue({ name: 'Local evidence', syncTarget: 'evidence' });

    expect(component.sourceCanConnect()).toBeTrue();
    delayedSources.complete();
  });

  it('cancels an obsolete refresh so stale sources cannot replace the newest view', () => {
    const { component, sourceService } = createComponent();
    const firstSources = new Subject<IConnectedSource[]>();
    const secondSources = new Subject<IConnectedSource[]>();
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValues(firstSources.asObservable(), secondSources.asObservable());
    sourceService.extractions.and.returnValue(of({ items: [], totalCount: 0, limit: 100 }));
    sourceService.auditLogs.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));

    component.refresh();
    component.refresh();

    secondSources.next([{ id: 'new-source', name: 'Newest source' } as IConnectedSource]);
    secondSources.complete();
    firstSources.next([{ id: 'old-source', name: 'Stale source' } as IConnectedSource]);
    firstSources.complete();

    expect(component.sources.map((source) => source.id)).toEqual(['new-source']);
  });

  it('marks an unavailable source health batch instead of treating it as absent health', () => {
    const { component, sourceService } = createComponent();
    const source = { id: 'gmail-source', connectorKey: 'gmail', name: 'Personal Gmail' } as IConnectedSource;
    sourceService.connectionHealths.and.returnValue(throwError(() => new Error('health unavailable')));

    (component as any).loadConnectionHealth([source]);

    expect(sourceService.connectionHealths).toHaveBeenCalledTimes(1);
    expect(component.sourceHealthUnavailable(source)).toBeTrue();
    expect(component.sourceHealth(source)).toBeUndefined();
  });

  it('treats malformed health results as unavailable instead of throwing during display mapping', () => {
    const { component, sourceService } = createComponent();
    const source = { id: 'gmail-source', connectorKey: 'gmail', name: 'Personal Gmail' } as IConnectedSource;
    sourceService.connectionHealths.and.returnValue(of([null] as any));

    (component as any).loadConnectionHealth([source]);

    expect(component.sourceHealthUnavailable(source)).toBeTrue();
    expect(component.sourceHealth(source)).toBeUndefined();
  });

  it('rejects source-health records that identify a different connector', () => {
    const { component, sourceService } = createComponent();
    const source = { id: 'gmail-mismatched-health', connectorKey: 'gmail', name: 'Personal Gmail' } as IConnectedSource;
    sourceService.connectionHealths.and.returnValue(of([{
      ...connectionHealth(source.id, 'google-drive'),
      status: 'ready',
      configured: true,
      authorized: true,
    }]));

    (component as any).loadConnectionHealth([source]);

    expect(component.sourceHealthUnavailable(source)).toBeTrue();
    expect(component.sourceHealthChecking(source)).toBeFalse();
    expect(component.sourceHealth(source)).toBeUndefined();
    expect(component.sourceHealthSummary(source)).toContain('could not be checked');
  });

  it('cancels obsolete source health requests so stale health cannot replace a newer refresh', () => {
    const { component, sourceService } = createComponent();
    const firstHealth = new Subject<ISourceConnectionHealth[]>();
    const secondHealth = new Subject<ISourceConnectionHealth[]>();
    const source = { id: 'gmail-source', connectorKey: 'gmail', name: 'Personal Gmail' } as IConnectedSource;
    sourceService.connectionHealths.and.returnValues(firstHealth.asObservable(), secondHealth.asObservable());

    (component as any).loadConnectionHealth([source]);
    (component as any).loadConnectionHealth([source]);

    secondHealth.next([{
      ...connectionHealth(source.id, source.connectorKey),
      status: 'ready',
      configured: true,
      authorized: true,
    }]);
    secondHealth.complete();
    firstHealth.next([{
      ...connectionHealth(source.id, source.connectorKey),
      status: 'blocked',
      configured: true,
    }]);
    firstHealth.complete();

    expect(component.sourceHealth(source)?.status).toBe('ready');
  });

  it('builds constant-time source record and latest-job lookups', () => {
    const { component } = createComponent();
    const source = { id: 'gmail-source', connectorKey: 'gmail', name: 'Personal Gmail' } as IConnectedSource;
    component.extractions = [
      { id: 'record-1', sourceId: source.id } as any,
      { id: 'record-2', sourceId: source.id } as any,
    ];
    component.syncJobs = [
      { id: 'newest-job', sourceId: source.id, status: 'completed' } as any,
      { id: 'older-job', sourceId: source.id, status: 'failed' } as any,
    ];

    (component as any).rebuildSourceIndexes();

    expect(component.sourceExtractionCount(source)).toBe(2);
    expect(component.latestJobFor(source)?.id).toBe('newest-job');
  });

  it('preserves extracted records when an action-triggered reload fails', () => {
    const { component, sourceService } = createComponent();
    component.extractions = [{ id: 'existing-record' } as any];
    sourceService.extractions.and.returnValue(throwError(() => new Error('records unavailable')));

    (component as any).loadExtractions();

    expect(component.extractions.map((item) => item.id)).toEqual(['existing-record']);
    expect(component.loadWarnings).toContain('Extracted records');
  });

  it('keeps an exact extraction count while loading only the recent page when the Advanced section is open', () => {
    const { component, sourceService, preferences } = createComponent();
    preferences.setMode('connected-sources', 'advanced');
    preferences.setSection('connected-sources', 'source-records', true);
    sourceService.connectors.and.returnValue(of([]));
    sourceService.sources.and.returnValue(of([]));
    sourceService.extractions.and.returnValue(of({ items: [extractionRecord('recent-record')], totalCount: 245, limit: 100 }));
    sourceService.auditLogs.and.returnValue(of([]));
    sourceService.syncJobs.and.returnValue(of([]));
    sourceService.connectionHealths.and.returnValue(of([]));

    component.refresh();

    expect(component.extractions.length).toBe(1);
    expect(component.extractionTotalCount).toBe(245);
    expect(component.extractionPageIsTruncated()).toBeTrue();
    expect(sourceService.extractions).toHaveBeenCalledWith('018-HAI', false, 100);
  });
});

function manualJob(sourceId: string, overrides: Partial<ISourceManualSyncJob> = {}): ISourceManualSyncJob {
  return {
    id: 'synthetic-sync-job', sourceId, mode: 'manual_async_sync', status: 'queued',
    itemsSeen: 0, itemsAdded: 0, itemsUpdated: 0, itemsFailed: 0,
    createdAt: '2026-09-30T10:00:00Z', attempt: 0, maxAttempts: 5, ...overrides,
  };
}

function connectionHealth(sourceId: string, connectorKey: string): ISourceConnectionHealth {
  return {
    sourceId,
    connectorKey,
    status: 'unknown',
    reason: '',
    configured: false,
    authorized: false,
    requiresReconnect: false,
  };
}

function connectedSource(id: string, connectorKey: string): IConnectedSource {
  return {
    id,
    connectorKey,
    name: 'Test source',
    category: 'email',
    enabled: true,
    localOnly: false,
    syncFrequency: 'manual',
    ingestionModes: 'metadata',
    permissions: 'read',
    excludePatterns: '',
    status: 'operational',
  };
}

function sourceConnector(connectorKey: string, name: string, adapterStatus: string): ISourceConnector {
  return {
    id: `connector-${connectorKey}`,
    connectorKey,
    name,
    category: 'files',
    supportedModes: 'manual',
    requiredScopes: '',
    localOnlyCapable: true,
    enabled: true,
    adapterStatus,
  };
}

function allowSourceSync(component: ConnectedSourcesComponent, source: IConnectedSource): void {
  component.connectorCatalogLoaded = true;
  component.connectorCatalogUnavailable = false;
  component.connectorCatalogLoading = false;
  component.connectors = [sourceConnector(source.connectorKey, 'Test connector', 'operational')];
}

function extractionRecord(id: string): ISourceExtraction {
  return {
    id,
    sourceId: 'source-1',
    rawItemId: `raw-${id}`,
    contentType: 'note',
    text: 'Evidence record',
    sensitive: false,
    uncertain: true,
    archived: false,
    updatedAt: '2026-09-24T00:00:00Z',
  };
}

function auditLog(id: string): ISourceAuditLog {
  return { id, action: 'source.read', message: 'Read source data', createdAt: '2026-09-24T00:00:00Z' };
}

function correctionView(overrides: Partial<ISourceExtractionCorrectionView> = {}): ISourceExtractionCorrectionView {
  return {
    id: 'correction-1',
    extractionId: 'extraction-1',
    status: 'queued',
    phase: 'accepted',
    intentPersisted: true,
    patchSaved: false,
    recoveryPending: true,
    needsReview: false,
    expectedRevision: '2026-09-24T00:00:00Z',
    appliedRevision: null,
    attempts: 0,
    maxAttempts: 5,
    ...overrides,
  };
}
