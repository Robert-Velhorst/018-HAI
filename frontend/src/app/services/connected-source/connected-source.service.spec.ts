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

  it('rejects an empty audit page that claims more records', () => {
    let error: unknown;
    service.auditLogPage(0, 8).subscribe({ error: failure => { error = failure; } });
    http.expectOne(r => r.url === '/api/v1/sources/audit-logs').flush({
      items: [], total: 1, limit: 8, offset: 0, hasMore: true,
    });
    expect(error).toEqual(jasmine.any(Error));
  });

  it('rejects an empty sync page that claims more records', () => {
    let error: unknown;
    service.syncJobPage(0, 8).subscribe({ error: failure => { error = failure; } });
    http.expectOne(r => r.url === '/api/v1/sources/sync-jobs').flush({
      items: [], total: 1, limit: 8, offset: 0, hasMore: true,
    });
    expect(error).toEqual(jasmine.any(Error));
  });

  it('rejects an empty extraction page that claims more records', () => {
    let error: unknown;
    service.extractions('', false, 8, 0).subscribe({ error: failure => { error = failure; } });
    http.expectOne(r => r.url === '/api/v1/sources/extractions').flush({
      items: [], total: 1, totalCount: 1, limit: 8, offset: 0, hasMore: true,
    });
    expect(error).toEqual(jasmine.any(Error));
  });

  it('requests an offset audit page and refuses an older server returning an unpaged array', () => {
    let error: unknown;
    service.auditLogPage(8).subscribe({ error: (failure) => { error = failure; } });
    const request = http.expectOne((r) => r.url === '/api/v1/sources/audit-logs' && r.params.get('offset') === '8' && r.params.get('limit') === '8');
    request.flush([]);
    expect(error).toEqual(jasmine.any(Error));
  });

  it('preserves owner-scoped audit pagination metadata and source filter', () => {
    const page = { items: [], total: 8, limit: 8, offset: 8, hasMore: false };
    let observed: unknown;
    service.auditLogPage(8, 8, 'source-id').subscribe((value) => { observed = value; });
    const request = http.expectOne((r) => r.url === '/api/v1/sources/audit-logs' && r.params.get('sourceId') === 'source-id');
    request.flush(page);
    expect(observed).toEqual(page);
  });

  it('rejects mismatched audit offsets, limits and continuation metadata', () => {
    for (const patch of [{ offset: 0 }, { limit: 100 }, { hasMore: true }, { total: -1 }, { items: [null] }, { items: [{}] }]) {
      let error: unknown;
      service.auditLogPage(8).subscribe({ error: (failure) => { error = failure; } });
      http.expectOne((r) => r.url === '/api/v1/sources/audit-logs').flush({ items: [], total: 8, limit: 8, offset: 8, hasMore: false, ...patch });
      expect(error).toEqual(jasmine.any(Error));
    }
  });

  it('requests bounded sync history separately from the latest-job array', () => {
    const page = { items: [], total: 0, limit: 6, offset: 6, hasMore: false };
    let observed: unknown;
    service.syncJobPage(6, 6, 'source-id').subscribe((value) => { observed = value; });
    const request = http.expectOne((r) => r.url === '/api/v1/sources/sync-jobs' && r.params.get('offset') === '6' && r.params.get('limit') === '6' && r.params.get('sourceId') === 'source-id');
    request.flush(page);
    expect(observed).toEqual(page);
  });

  it('rejects unpaged, malformed or mismatched sync history without treating it as empty', () => {
    for (const response of [[], { items: [], total: 0, limit: 100, offset: 0, hasMore: false },
      { items: [null], total: 1, limit: 6, offset: 0, hasMore: false },
      { items: [{ id: 'job', sourceId: 'source', mode: 'manual_async_sync', status: 'queued', itemsSeen: -1, itemsAdded: 0, itemsUpdated: 0, itemsFailed: 0 }], total: 1, limit: 6, offset: 0, hasMore: false }]) {
      let error: unknown;
      service.syncJobPage(0).subscribe({ error: (failure) => { error = failure; } });
      http.expectOne((r) => r.url === '/api/v1/sources/sync-jobs').flush(response);
      expect(error).toEqual(jasmine.any(Error));
    }
  });

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

  it('requests an explicit extraction offset and reads the paged history response', () => {
    service.extractions('synthetic-project', false, 8, 8).subscribe((page) => {
      expect(page.items.map(item => item.id)).toEqual(['older-record']);
      expect(page.totalCount).toBe(20);
      expect(page.limit).toBe(8);
    });
    const request = http.expectOne(candidate => candidate.url === '/api/v1/sources/extractions');
    expect(request.request.params.get('offset')).toBe('8');
    expect(request.request.params.get('limit')).toBe('8');
    request.flush({ items: [{ id: 'older-record' }], totalCount: 20, total: 20, limit: 8, offset: 8, hasMore: true }, {
      headers: { 'X-Total-Count': '20', 'X-Result-Limit': '8', 'X-Result-Offset': '8' },
    });
  });

  it('does not silently show the first page when an older backend ignores the offset', () => {
    let failed = false;
    service.extractions('', false, 8, 8).subscribe({ error: () => { failed = true; } });
    const request = http.expectOne(candidate => candidate.url === '/api/v1/sources/extractions');
    request.flush([{ id: 'first-page-record' }]);
    expect(failed).toBeTrue();
  });

  it('rejects a history envelope for a different offset instead of mislabeling the records', () => {
    let failed = false;
    service.extractions('', false, 8, 8).subscribe({ error: () => { failed = true; } });
    const request = http.expectOne(candidate => candidate.url === '/api/v1/sources/extractions');
    request.flush({ items: [], totalCount: 20, limit: 8, offset: 0 });
    expect(failed).toBeTrue();
  });

  for (const test of [
    { name: 'different page limit', patch: { limit: 100 } },
    { name: 'negative total', patch: { totalCount: -1, total: -1 } },
    { name: 'fractional total', patch: { totalCount: 20.5, total: 20.5 } },
    { name: 'contradictory totals', patch: { totalCount: 9 } },
    { name: 'impossible item count', patch: { totalCount: 8, total: 8 } },
    { name: 'incorrect continuation', patch: { hasMore: false } },
    { name: 'null record', patch: { items: [null] } },
    { name: 'conflicting count header', headers: { 'X-Total-Count': '0' } },
    { name: 'conflicting limit header', headers: { 'X-Result-Limit': '100' } },
    { name: 'conflicting offset header', headers: { 'X-Result-Offset': '0' } },
  ] as Array<{ name: string; patch?: Record<string, unknown>; headers?: Record<string, string> }>) {
    it(`rejects inconsistent extraction history: ${test.name}`, () => {
      let failures = 0;
      let successes = 0;
      service.extractions('', false, 8, 8).subscribe({ next: () => successes++, error: () => failures++ });
      http.expectOne(candidate => candidate.url === '/api/v1/sources/extractions').flush({
        items: [{ id: 'older-record' }], totalCount: 20, total: 20, limit: 8, offset: 8, hasMore: true,
        ...test.patch,
      }, { headers: test.headers || {} });
      expect(successes).toBe(0);
      expect(failures).toBe(1);
    });
  }

  it('uses valid extraction envelope metadata when optional response headers are absent', () => {
    let observed: unknown;
    service.extractions('', false, 8, 8).subscribe(page => { observed = page; });
    http.expectOne(candidate => candidate.url === '/api/v1/sources/extractions').flush({
      items: [{ id: 'older-record' }], totalCount: 20, total: 20, limit: 8, offset: 8, hasMore: true,
    });
    expect(observed).toEqual({ items: [{ id: 'older-record' }], totalCount: 20, limit: 8, offset: 8 });
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
