import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { SystemStatusService } from './system-status.service'

describe('SystemStatusService', () => {
  let service: SystemStatusService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [HttpClientTestingModule] })
    service = TestBed.inject(SystemStatusService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('reads detailed readiness from the authenticated system route', () => {
    service.readiness().subscribe()
    const request = http.expectOne('/api/v1/system/readiness')
    expect(request.request.method).toBe('GET')
    request.flush({ status: 'ready', service: 'backend', summary: { ok: 1, warn: 0, fail: 0 }, checks: [] })
  })

  it('reads authenticated build provenance from the system info route', () => {
    service.info().subscribe()
    const request = http.expectOne('/api/v1/system/info')
    expect(request.request.method).toBe('GET')
    request.flush({
      build: { version: '1.2.3', commit: 'abcdef1234567890', buildTime: '2026-10-11T10:00:00Z', goVersion: 'go1.27.3' },
      runMode: 'production', allowsRealSideEffects: false, languages: ['en', 'nl'],
      readiness: { ready: true, ok: 2, warn: 0, fail: 0 },
    })
  })

  it('reads the verified-owner admin delivery route without a trailing redirect', () => {
    service.eventDelivery().subscribe()
    const request = http.expectOne('/api/v1/automation/events/delivery')
    expect(request.request.method).toBe('GET')
    request.flush({ pending: 0, deadLettered: 0, published: 2, recentFailures: [], checkedAt: '2026-08-09T00:00:00Z' })
  })

  it('reads the authenticated HAI connector status', () => {
    service.connectorStatus().subscribe()
    const request = http.expectOne('/api/v1/a2a/status')
    expect(request.request.method).toBe('GET')
    request.flush({
      enabled: true,
      configured: true,
      provider: 'A2A local planning bridge',
      endpoint: 'http://127.0.0.1:8088/api/v1/a2a',
      capabilities: [],
      restrictions: [],
      scope: 'Planning only',
      transport: 'local',
    })
  })

  it('encodes the dead-letter id when requesting an explicit retry', () => {
    service.retryEventDelivery('event/id').subscribe()
    const request = http.expectOne('/api/v1/automation/events/delivery/event%2Fid/retry')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({})
    request.flush({ status: 'queued', eventId: 'event/id' })
  })
})
