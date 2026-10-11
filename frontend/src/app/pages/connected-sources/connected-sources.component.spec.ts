import { FormBuilder } from '@angular/forms';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { of } from 'rxjs';
import { IConnectedSource, ISourcePursuitRoutingOutcome } from '../../models/connected-source.model.interface';
import { ConnectedSourcesComponent } from './connected-sources.component';

describe('ConnectedSourcesComponent pursuit handoff', () => {
  function createComponent(): { component: ConnectedSourcesComponent; router: jasmine.SpyObj<Router> } {
    const router = jasmine.createSpyObj<Router>('Router', ['navigate']);
    return {
      component: new ConnectedSourcesComponent(
        new FormBuilder(),
        {} as any,
        {} as NzNotificationService,
        router,
        { mode: () => 'light' } as any,
      ),
      router,
    };
  }

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

  it('requires an explicit local folder instead of defaulting to the whole source root', () => {
    const { component } = createComponent();

    expect(component.sourceForm.controls['syncTarget'].value).toBe('');
    expect(component.sourceForm.invalid).toBeTrue();

    component.sourceForm.controls['syncTarget'].setValue('legal/vivare');
    expect(component.sourceForm.valid).toBeTrue();

    component.connectorChanged('local-folder');
    expect(component.sourceForm.controls['syncTarget'].value).toBe('');
    expect(component.syncTargetPlaceholder()).toContain('Explicit subfolder');
  });
});

describe('ConnectedSourcesComponent progressive evidence loading', () => {
  function createHarness() {
    const service = jasmine.createSpyObj('ConnectedSourceService', [
      'connectors', 'sources', 'overview', 'connectionHealth', 'extractions', 'syncJobs', 'auditLogs',
    ]);
    const sources = [
      { id: 'source-1', connectorKey: 'local-folder', enabled: true, status: 'active' },
      { id: 'source-2', connectorKey: 'github', enabled: true, status: 'active' },
    ] as IConnectedSource[];
    service.connectors.and.returnValue(of([]));
    service.sources.and.returnValue(of(sources));
    service.overview.and.returnValue(of({
      extractionCount: 12,
      sensitiveExtractionCount: 2,
      uncertainExtractionCount: 1,
      failedJobs: 3,
      pendingJobs: 1,
      extractionCountsBySource: { 'source-1': 7, 'source-2': 5 },
      latestJobStatusBySource: { 'source-1': 'failed', 'source-2': 'completed' },
    }));
    service.connectionHealth.and.returnValue(of({ sourceId: 'source-1', status: 'ready' }));
    service.extractions.and.returnValue(of([]));
    service.syncJobs.and.returnValue(of([]));
    service.auditLogs.and.returnValue(of([]));
    const component = new ConnectedSourcesComponent(
      new FormBuilder(),
      service,
      jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['error', 'success', 'warning', 'info']),
      jasmine.createSpyObj<Router>('Router', ['navigate']),
      { mode: () => 'light' } as any,
    );
    return { component, service };
  }

  it('loads only the registry, safety overview, and selected-source health on startup', () => {
    const { component, service } = createHarness();

    component.ngOnInit();

    expect(service.connectors).toHaveBeenCalledTimes(1);
    expect(service.sources).toHaveBeenCalledTimes(1);
    expect(service.overview).toHaveBeenCalledTimes(1);
    expect(service.connectionHealth).toHaveBeenCalledOnceWith('source-1');
    expect(service.extractions).not.toHaveBeenCalled();
    expect(service.syncJobs).not.toHaveBeenCalled();
    expect(service.auditLogs).not.toHaveBeenCalled();
    expect(component.failedJobCount()).toBe(3);
    expect(component.sourceExtractionCount(component.sources[0])).toBe(7);
  });

  it('loads extraction records once when their section is first opened', () => {
    const { component, service } = createHarness();
    component.ngOnInit();

    component.setExtractionHistoryOpen(true);
    component.setExtractionHistoryOpen(false);
    component.setExtractionHistoryOpen(true);

    expect(service.extractions).toHaveBeenCalledTimes(1);
    expect(service.extractions).toHaveBeenCalledWith('018-HAI', false, 8);
  });

  it('loads jobs and audit records once when activity is first opened', () => {
    const { component, service } = createHarness();
    component.ngOnInit();

    component.setActivityHistoryOpen(true);
    component.setActivityHistoryOpen(false);
    component.setActivityHistoryOpen(true);

    expect(service.syncJobs).toHaveBeenCalledTimes(1);
    expect(service.syncJobs).toHaveBeenCalledWith(undefined, 6);
    expect(service.auditLogs).toHaveBeenCalledTimes(1);
    expect(service.auditLogs).toHaveBeenCalledWith(undefined, 8);
  });

  it('refreshes only detail histories that the operator has opened', () => {
    const { component, service } = createHarness();
    component.ngOnInit();
    component.setExtractionHistoryOpen(true);

    component.refresh();

    expect(service.extractions).toHaveBeenCalledTimes(2);
    expect(service.syncJobs).not.toHaveBeenCalled();
    expect(service.auditLogs).not.toHaveBeenCalled();
  });
});
