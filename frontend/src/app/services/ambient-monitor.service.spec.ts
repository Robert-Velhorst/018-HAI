import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { AmbientMonitorService } from './ambient-monitor.service'
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('AmbientMonitorService', () => {
  let service: AmbientMonitorService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] })
    service = TestBed.inject(AmbientMonitorService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('loads targets from the encoded outcome monitor route', () => {
    service.getMonitor('life/work', 'health goal').subscribe()
    const request = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/life%2Fwork/outcomes/health%20goal/monitor'
    )
    expect(request.request.method).toBe('GET')
    request.flush({ targets: [] })
  })

  it('registers only allowlisted target fields', () => {
    const payload: any = {
      idempotencyKey: 'monitor-1', targetId: 'target-1', indicatorId: 'open-loops',
      sourceKind: 'workflow_open_loop_count', enabled: true, cadenceSeconds: 900.9,
      firstRunAt: '2026-08-05T10:00:00Z', ownerId: 'other', canExecute: true,
    }
    service.registerTarget('local', 'outcome-1', payload).subscribe()
    const request = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/local/outcomes/outcome-1/monitor'
    )
    expect(request.request.method).toBe('PUT')
    expect(request.request.body).toEqual({
      idempotencyKey: 'monitor-1', targetId: 'target-1', indicatorId: 'open-loops',
      sourceKind: 'workflow_open_loop_count', enabled: true, cadenceSeconds: 900,
      firstRunAt: '2026-08-05T10:00:00Z',
    })
    expect(request.request.body.ownerId).toBeUndefined()
    expect(request.request.body.canExecute).toBeUndefined()
    request.flush({})
  })

  it('changes only enabled state through the governed target route', () => {
    service.setEnabled('local', 'outcome-1', 'target/1', {
      idempotencyKey: 'disable-1', enabled: false,
    }).subscribe()
    const request = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/local/outcomes/outcome-1/monitor/target%2F1/enabled'
    )
    expect(request.request.method).toBe('PATCH')
    expect(request.request.body).toEqual({ idempotencyKey: 'disable-1', enabled: false })
    request.flush({})
  })

  it('loads immutable observations with a bounded limit', () => {
    service.listObservations('local', 'outcome-1', 'target-1', 900).subscribe()
    const request = http.expectOne((candidate) =>
      candidate.url.endsWith('/monitor/target-1/observations') && candidate.params.get('limit') === '500'
    )
    expect(request.request.method).toBe('GET')
    request.flush({ observations: [] })
  })

  it('loads immutable monitor runs', () => {
    service.listRuns('local', 'outcome-1', 'target-1', 25).subscribe()
    const request = http.expectOne((candidate) =>
      candidate.url.endsWith('/monitor/target-1/runs') && candidate.params.get('limit') === '25'
    )
    expect(request.request.method).toBe('GET')
    request.flush({ runs: [] })
  })

  it('loads bounded composition delivery history from the encoded target route', () => {
    service.listCompositions('life/work', 'health goal', 'target/1', 900).subscribe()
    const request = http.expectOne((candidate) =>
      candidate.url.endsWith('/outcomes/health%20goal/monitor/target%2F1/compositions') &&
      candidate.params.get('limit') === '500'
    )
    expect(request.request.method).toBe('GET')
    expect(request.request.body).toBeNull()
    request.flush({ compositions: [] })
  })

  it('loads immutable composition attempts without client authority fields', () => {
    service.listCompositionAttempts('local', 'outcome-1', 'target-1', 'cmp/1', 25).subscribe()
    const request = http.expectOne((candidate) =>
      candidate.url.endsWith('/monitor/target-1/compositions/cmp%2F1/attempts') &&
      candidate.params.get('limit') === '25'
    )
    expect(request.request.method).toBe('GET')
    expect(request.request.body).toBeNull()
    request.flush({ attempts: [] })
  })

  it('runs a bounded workspace batch without client authority fields', () => {
    const payload: any = {
      workerId: 'browser-owner', asOf: '2026-08-05T10:00:00Z', leaseSeconds: 120.8,
      limit: 20.9, authority: 'autonomous', canExecute: true,
    }
    service.runDue('life/work', payload).subscribe()
    const request = http.expectOne(
      '/api/v1/outcome-evaluations/workspaces/life%2Fwork/monitors/run-due'
    )
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({
      workerId: 'browser-owner', asOf: '2026-08-05T10:00:00Z', leaseSeconds: 120, limit: 20,
    })
    expect(request.request.body.authority).toBeUndefined()
    expect(request.request.body.canExecute).toBeUndefined()
    request.flush({ claimed: 0, completions: [], failures: [] })
  })
})
