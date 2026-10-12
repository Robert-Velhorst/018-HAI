import { TestBed } from '@angular/core/testing';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ContextMemoryService } from './context-memory.service';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('ContextMemoryService', () => {
  let service: ContextMemoryService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] });
    service = TestBed.inject(ContextMemoryService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('requests a bounded local semantic reindex', () => {
    service.reindexSemantic(500).subscribe((result) => {
      expect(result.enabled).toBeTrue();
      expect(result.indexed).toBe(2);
    });

    const request = http.expectOne('/api/v1/memory/semantic/reindex?limit=100');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({});
    request.flush({
      enabled: true,
      attempted: 2,
      indexed: 2,
      failed: 0,
      deferred: 0,
      explanation: 'Two visible records were indexed locally.',
    });
  });

  it('requests only the recent memory slice when a limit is supplied', () => {
    service.list(undefined, false, 20).subscribe();

    const request = http.expectOne('/api/v1/memory/?includeArchived=false&limit=20');
    expect(request.request.method).toBe('GET');
    request.flush([]);
  });

  it('uses the source-backed query contract without sending caller-supplied owner identity', () => {
    service.query({ projectKey: ' synthetic-project ', q: ' text & more ', kind: 'lesson', tag: 'source-correction', sort: 'confidence', order: 'asc', page: 2, pageSize: 50, includeArchived: true, ownerIdentity: 'forged-owner' } as any).subscribe((result) => expect(result.total).toBe(51));
    const request = http.expectOne((req) => req.url === '/api/v1/memory/query');
    expect(request.request.method).toBe('GET');
    expect(request.request.params.keys().sort()).toEqual(['includeArchived', 'kind', 'order', 'page', 'pageSize', 'projectKey', 'q', 'sort', 'tag'].sort());
    expect(request.request.params.get('projectKey')).toBe('synthetic-project');
    expect(request.request.params.get('q')).toBe('text & more');
    expect(request.request.params.get('page')).toBe('2');
    expect(request.request.params.get('pageSize')).toBe('50');
    expect(request.request.params.get('includeArchived')).toBe('true');
    request.flush({ items: [], total: 51, page: 2, pageSize: 50, totalPages: 2, sort: 'confidence', order: 'asc' });
  });

  it('bounds finite pagination and safely defaults non-finite values', () => {
    for (const [page, pageSize, expectedPage, expectedSize] of [[-10, 500, '1', '100'], [2.7, 40.9, '2', '40'], [NaN, Infinity, '1', '20']]) {
      service.query({ page: page as number, pageSize: pageSize as number }).subscribe();
      const request = http.expectOne((req) => req.url.endsWith('/memory/query'));
      expect(request.request.params.get('page')).toBe(expectedPage as string);
      expect(request.request.params.get('pageSize')).toBe(expectedSize as string);
      request.flush({ items: [], total: 0, page: 1, pageSize: 20, totalPages: 0, sort: 'updatedAt', order: 'desc' });
    }
  });

  it('normalizes the backend null usedContext to an empty array without inventing hits', () => {
    service.retrieve({ query: 'no matches' }).subscribe((result) => expect(result.usedContext).toEqual([]));
    http.expectOne('/api/v1/memory/retrieve').flush({ query: 'no matches', usedContext: null, explanation: 'No matching records' });
  });

  it('propagates unauthorized query and failed mutation responses instead of substituting success', () => {
    const queryError = jasmine.createSpy('query error');
    const archiveSuccess = jasmine.createSpy('archive success');
    const archiveError = jasmine.createSpy('archive error');
    service.query({}).subscribe({ error: queryError });
    http.expectOne((req) => req.url.endsWith('/memory/query')).flush({ error: 'owner required' }, { status: 401, statusText: 'Unauthorized' });
    expect(queryError).toHaveBeenCalled();
    service.archive('synthetic-memory').subscribe({ next: archiveSuccess, error: archiveError });
    http.expectOne('/api/v1/memory/synthetic-memory/archive').flush({ error: 'not your record' }, { status: 403, statusText: 'Forbidden' });
    expect(archiveSuccess).not.toHaveBeenCalled();
    expect(archiveError).toHaveBeenCalled();
  });

  it('keeps the selected record inside a single URL segment and uses the documented mutation methods', () => {
    const memory = { kind: 'project', content: 'synthetic correction' };
    service.update('fixture/../export', memory).subscribe();
    const update = http.expectOne('/api/v1/memory/fixture%2F..%2Fexport');
    expect(update.request.method).toBe('PATCH');
    expect(update.request.body).toEqual(memory);
    update.flush({ id: 'fixture', ...memory });
    service.restore('fixture').subscribe();
    const restore = http.expectOne('/api/v1/memory/fixture/restore');
    expect(restore.request.method).toBe('POST');
    expect(restore.request.body).toEqual({});
    restore.flush({ id: 'fixture', archived: false });
    service.delete('fixture').subscribe();
    const deletion = http.expectOne('/api/v1/memory/fixture');
    expect(deletion.request.method).toBe('DELETE');
    deletion.flush(null, { status: 204, statusText: 'No Content' });
  });

  it('exports the selected project without pagination or ownership overrides', () => {
    service.exportMemories('synthetic-project').subscribe();
    const request = http.expectOne('/api/v1/memory/export?projectKey=synthetic-project');
    expect(request.request.method).toBe('GET');
    request.flush({ format: '018-hai-context-memory-v1', memories: [] });
  });
});
