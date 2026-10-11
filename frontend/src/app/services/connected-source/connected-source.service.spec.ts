import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { ConnectedSourceService } from './connected-source.service';
import { ISourceExtractionCorrectionView, ISourceManualSyncJob } from '../../models/connected-source.model.interface';

describe('ConnectedSourceService extraction corrections', () => {
  let service: ConnectedSourceService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()],
    });
    service = TestBed.inject(ConnectedSourceService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  const authorization = {
    taskId: 'synthetic-task', approvalSourceId: 'task-review:synthetic-review',
    approvalBindingDigest: 'a'.repeat(64), idempotencyKey: 'synthetic-source-action-key',
  };

  it('sends existing approval references to the exact revocation endpoint without granting client authority', () => {
    service.revoke('source / 1', authorization).subscribe();
    const request = http.expectOne('/api/v1/sources/source%20%2F%201/revoke');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({});
    expect(request.request.headers.get('X-HAI-Task-ID')).toBe(authorization.taskId);
    expect(request.request.headers.get('X-HAI-Approval-Source-ID')).toBe(authorization.approvalSourceId);
    expect(request.request.headers.get('X-HAI-Approval-Binding-Digest')).toBe(authorization.approvalBindingDigest);
    expect(request.request.headers.get('X-HAI-Idempotency-Key')).toBe(authorization.idempotencyKey);
    request.flush({ id: 'source / 1', status: 'revoked' });
  });

  it('sends approval references for record deletion and preserves emergency-stop rejection', () => {
    let observedStatus = 0;
    service.deleteExtraction('record / 1', authorization).subscribe({ error: (error) => { observedStatus = error.status; } });
    const request = http.expectOne('/api/v1/sources/extractions/record%20%2F%201');
    expect(request.request.method).toBe('DELETE');
    expect(request.request.headers.get('X-HAI-Task-ID')).toBe(authorization.taskId);
    expect(request.request.headers.get('X-HAI-Approval-Source-ID')).toBe(authorization.approvalSourceId);
    expect(request.request.headers.get('X-HAI-Approval-Binding-Digest')).toBe(authorization.approvalBindingDigest);
    expect(request.request.headers.get('X-HAI-Idempotency-Key')).toBe(authorization.idempotencyKey);
    request.flush({ error: 'emergency stop is active' }, { status: 423, statusText: 'Locked' });
    expect(observedStatus).toBe(423);
  });

  it('uses the returned page length when total-count headers are missing rather than reporting zero records', () => {
    service.extractions('', false, 100).subscribe((page) => {
      expect(page.totalCount).toBe(2);
      expect(page.limit).toBe(100);
    });
    const request = http.expectOne((candidate) => candidate.url === '/api/v1/sources/extractions');
    expect(request.request.params.get('includeArchived')).toBe('false');
    request.flush([{ id: 'record-1' }, { id: 'record-2' }]);
  });

  it('preserves the backend total and result limit for a truncated extraction page', () => {
    service.extractions('synthetic-project', true, 100).subscribe((page) => {
      expect(page.totalCount).toBe(200);
      expect(page.limit).toBe(50);
    });
    const request = http.expectOne((candidate) => candidate.url === '/api/v1/sources/extractions');
    expect(request.request.params.get('projectKey')).toBe('synthetic-project');
    request.flush([{ id: 'record-1' }], { headers: { 'X-Total-Count': '200', 'X-Result-Limit': '50' } });
  });

  it('wires health recovery, pause, resume, and Google consent to their actual backend paths', () => {
    service.connectionHealth('synthetic-source').subscribe();
    const health = http.expectOne('/api/v1/sources/synthetic-source/health');
    expect(health.request.method).toBe('GET');
    health.flush({ sourceId: 'synthetic-source' });
    service.pause('synthetic-source').subscribe();
    const pause = http.expectOne('/api/v1/sources/synthetic-source/pause');
    expect(pause.request.method).toBe('POST');
    pause.flush({ id: 'synthetic-source', enabled: false });
    service.resume('synthetic-source').subscribe();
    const resume = http.expectOne('/api/v1/sources/synthetic-source/resume');
    expect(resume.request.method).toBe('POST');
    resume.flush({ id: 'synthetic-source', enabled: true });
    service.startGoogleOAuth('synthetic-source').subscribe();
    const oauth = http.expectOne('/api/v1/sources/oauth/google/start?sourceId=synthetic-source');
    expect(oauth.request.method).toBe('GET');
    oauth.flush({ authorizeUrl: 'https://accounts.google.test/authorize' });
  });

  it('submits the patch with the exact If-Match revision and idempotency key headers', () => {
    const revision = '2026-09-24T19:45:12.123456789Z';
    const key = 'correction-key-1';
    const response = correctionView();

    service.submitExtractionCorrection('extraction / 1', { uncertain: false }, revision, key).subscribe();

    const request = http.expectOne('/api/v1/sources/extractions/extraction%20%2F%201');
    expect(request.request.method).toBe('PATCH');
    expect(request.request.body).toEqual({ uncertain: false });
    expect(request.request.body.expectedRevision).toBeUndefined();
    expect(request.request.headers.get('If-Match')).toBe(revision);
    expect(request.request.headers.get('Idempotency-Key')).toBe(key);
    request.flush(response);
  });

  it('fetches correction status from the owner-scoped direct-view endpoint without caching', () => {
    const response = correctionView({ status: 'running', recoveryPending: true });

    service.extractionCorrection('correction-1').subscribe((view) => expect(view).toEqual(response));

    const request = http.expectOne('/api/v1/sources/extraction-corrections/correction-1');
    expect(request.request.method).toBe('GET');
    expect(request.request.headers.get('Cache-Control')).toBe('no-cache');
    request.flush(response);
  });

  it('queues a manual source sync with the request key used for safe retry', () => {
    const job: ISourceManualSyncJob = {
      id: 'sync-1', sourceId: 'source-1', mode: 'manual_async_sync', status: 'queued',
      itemsSeen: 0, itemsAdded: 0, itemsUpdated: 0, itemsFailed: 0,
      createdAt: '2026-09-25T10:00:00Z', attempt: 0, maxAttempts: 5,
    };

    service.submitManualSync('source-1', { projectKey: '018-HAI' }, 'stable-request-key').subscribe((result) => {
      expect(result).toEqual(job);
    });

    const request = http.expectOne('/api/v1/sources/source-1/sync-jobs');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({ projectKey: '018-HAI' });
    expect(request.request.headers.get('Idempotency-Key')).toBe('stable-request-key');
    request.flush(job);
  });

  it('checks the accepted manual sync job without allowing a cached status response', () => {
    const job: ISourceManualSyncJob = {
      id: 'sync-1', sourceId: 'source-1', mode: 'manual_async_sync', status: 'running',
      itemsSeen: 2, itemsAdded: 1, itemsUpdated: 1, itemsFailed: 0,
      createdAt: '2026-09-25T10:00:00Z', attempt: 1, maxAttempts: 5,
    };

    service.manualSyncJob('sync-1').subscribe((result) => expect(result).toEqual(job));

    const request = http.expectOne('/api/v1/sources/sync-jobs/sync-1');
    expect(request.request.method).toBe('GET');
    expect(request.request.headers.get('Cache-Control')).toBe('no-cache');
    request.flush(job);
  });
});

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
    expectedRevision: '2026-09-24T19:45:12.123456789Z',
    appliedRevision: null,
    attempts: 0,
    maxAttempts: 5,
    ...overrides,
  };
}
