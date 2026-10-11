import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { KnowledgeClaimService } from './knowledge-claim.service'
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('KnowledgeClaimService', () => {
  let service: KnowledgeClaimService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] })
    service = TestBed.inject(KnowledgeClaimService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('loads the owner-scoped bounded review queue', () => {
    service.reviewQueue('018-HAI').subscribe()

    const request = http.expectOne(
      (candidate) => candidate.url === '/api/v1/knowledge/claims/review-queue' &&
        candidate.params.get('workspaceId') === '018-HAI'
    )
    expect(request.request.method).toBe('GET')
    request.flush({ items: [], counts: {}, effectiveAt: '2026-08-04T10:00:00Z', observedBy: '2026-08-04T10:00:00Z', truncated: false })
  })

  it('posts a correction only through the dedicated approval endpoint', () => {
    const payload = {
      workspaceId: '018-HAI',
      requestId: 'correction-1',
      correctedObject: 'new value',
      reason: 'Owner confirmed the correction.',
    }
    service.correct('claim/one', payload).subscribe()

    const request = http.expectOne('/api/v1/knowledge/claims/claim%2Fone/corrections')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual(payload)
    request.flush({ id: 'claim-new' })
  })
})
