import { TestBed } from '@angular/core/testing'
import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing'
import { RuntimeLabService } from './runtime-lab.service'

describe('RuntimeLabService safe outcomes', () => {
  let service: RuntimeLabService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [HttpClientTestingModule] })
    service = TestBed.inject(RuntimeLabService)
    http = TestBed.inject(HttpTestingController)
  })
  afterEach(() => http.verify())

  const attempt = {
    id: 'attempt', runtimeId: 'local-safe-worker', operationId: 'operation', status: 'inconclusive',
    operationStatus: 'running', detail: 'Completion is not confirmed.', verificationPassed: false,
    interrupted: false, reconciliationRequired: true, outcomeRecorded: false, createdAt: '2026-10-01T00:00:00Z',
    receipt: { output: { artifactHash: 'retained-hash', boundedOutput: 'bounded evidence' } },
  }

  it('retains an HTTP 200 inconclusive self-test receipt without issuing another mutation', () => {
    service.selfTest('local-safe-worker').subscribe((result) => expect(result).toEqual(attempt))
    const request = http.expectOne('/api/v1/runtime-lab/local-safe-worker/self-test')
    expect(request.request.method).toBe('POST')
    request.flush(attempt)
    http.expectNone('/api/v1/runtime-lab/local-safe-worker/self-test')
  })

  it('preserves the same attempt fields in overview and attempt history reads', () => {
    service.overview().subscribe((result) => expect(result.runtimes[0].lastAttempt).toEqual(attempt))
    http.expectOne('/api/v1/runtime-lab/overview').flush({ runtimes: [{ lastAttempt: attempt }] })
    service.attempts('local-safe-worker').subscribe((result) => expect(result.attempts[0]).toEqual(attempt))
    http.expectOne('/api/v1/runtime-lab/local-safe-worker/attempts').flush({ attempts: [attempt] })
  })
})
