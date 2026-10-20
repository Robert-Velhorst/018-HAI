import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http'
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { HireAIService } from './hire-ai.service'

describe('HireAIService', () => {
  let service: HireAIService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()],
    })
    service = TestBed.inject(HireAIService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('reads connector status without exposing or configuring credentials', () => {
    let configured = false
    service.status().subscribe((status) => { configured = status.configured })

    const request = http.expectOne('/api/v1/hire-ai/status')
    expect(request.request.method).toBe('GET')
    request.flush({ enabled: true, configured: true, capabilities: [], restrictions: [] })
    expect(configured).toBeTrue()
  })

  it('sends only bounded filters and an optional continuation cursor', () => {
    service.search({ query: 'support', location: 'Europe' }).subscribe()
    const first = http.expectOne('/api/v1/hire-ai/jobs/search')
    expect(first.request.method).toBe('POST')
    expect(first.request.body).toEqual({ filters: { query: 'support', location: 'Europe' }, limit: 10 })
    first.flush({ generatedAt: '2026-10-18T00:00:00Z', policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true }, jobs: [], nextCursor: null })

    const cursor = { createdAt: '2026-10-18T00:00:00Z', id: 42 }
    service.search({ query: 'support' }, cursor).subscribe()
    const next = http.expectOne('/api/v1/hire-ai/jobs/search')
    expect(next.request.body).toEqual({ filters: { query: 'support' }, limit: 10, cursor })
    next.flush({ generatedAt: '2026-10-18T00:00:00Z', policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true }, jobs: [], nextCursor: null })
  })
})
