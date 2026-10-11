import { TestBed } from '@angular/core/testing';
import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { ContextMemoryService } from './context-memory.service';

describe('ContextMemoryService', () => {
  let service: ContextMemoryService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [HttpClientTestingModule] });
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

  it('queries a bounded page with explicit server-side filters', () => {
    service.query({
      projectKey: '018-HAI',
      includeArchived: true,
      q: 'routing budget',
      kind: 'decision',
      tag: 'verified',
      sort: 'confidence',
      order: 'asc',
      page: 2,
      pageSize: 500,
    }).subscribe((result) => {
      expect(result.total).toBe(101);
      expect(result.pageSize).toBe(100);
    });

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/memory/query' &&
      candidate.params.get('projectKey') === '018-HAI' &&
      candidate.params.get('includeArchived') === 'true' &&
      candidate.params.get('q') === 'routing budget' &&
      candidate.params.get('kind') === 'decision' &&
      candidate.params.get('tag') === 'verified' &&
      candidate.params.get('sort') === 'confidence' &&
      candidate.params.get('order') === 'asc' &&
      candidate.params.get('page') === '2' &&
      candidate.params.get('pageSize') === '100'
    );
    expect(request.request.method).toBe('GET');
    request.flush({
      items: [],
      total: 101,
      page: 2,
      pageSize: 100,
      totalPages: 2,
      sort: 'confidence',
      order: 'asc',
      search: 'routing budget',
      kind: 'decision',
      tag: 'verified',
    });
  });
});
