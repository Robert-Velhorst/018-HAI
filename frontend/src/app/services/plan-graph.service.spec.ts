import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { IPlanGraph } from '../models/plan-graph.model.interface'
import { PlanGraphService } from './plan-graph.service'
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('PlanGraphService', () => {
  let service: PlanGraphService
  let http: HttpTestingController

  const transportPlan = (overrides: Record<string, unknown> = {}): any => ({
    id: 'plan-1',
    title: 'Evidence response',
    status: 'draft',
    revision: 1,
    digest: 'a'.repeat(64),
    nodes: [{
      id: '001-objective',
      type: 'objective',
      title: 'Prepare a verified response',
      owner: 'hai',
      status: 'ready',
      estimatedMinutes: 30,
      estimatedCostEur: 0,
      risk: 'medium',
      approvalState: 'not_required',
      bindings: {},
    }],
    edges: [],
    createdBy: 'robert',
    createdAt: '2026-08-05T08:00:00Z',
    canExecute: false,
    ...overrides,
  })

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] })
    service = TestBed.inject(PlanGraphService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('lists plans and normalizes omitted collections', () => {
    let result: IPlanGraph[] = []
    service.list().subscribe((plans) => result = plans)

    const request = http.expectOne('/api/v1/plans')
    expect(request.request.method).toBe('GET')
    request.flush({ plans: [{ ...transportPlan(), nodes: undefined }] })

    expect(result.length).toBe(1)
    expect(result[0].nodes).toEqual([])
    expect(result[0].approval).toEqual({ required: false, status: 'not_required' })
  })

  it('loads an encoded plan detail', () => {
    service.get('plan/one').subscribe()
    const request = http.expectOne('/api/v1/plans/plan%2Fone')
    expect(request.request.method).toBe('GET')
    request.flush(transportPlan())
  })

  it('creates a preview without changing the supplied request', () => {
    const body = {
      idempotencyKey: 'preview-key',
      title: 'Evidence response',
      nodes: transportPlan().nodes,
      edges: [],
    }
    service.preview(body).subscribe()
    const request = http.expectOne('/api/v1/plans/preview')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual(body)
    request.flush(transportPlan())
  })

  it('binds accept and replan mutations to the current revision', () => {
    service.accept('plan-1', { expectedRevision: 2, expectedDigest: 'a'.repeat(64) }).subscribe()
    const accept = http.expectOne('/api/v1/plans/plan-1/accept')
    expect(accept.request.method).toBe('POST')
    expect(accept.request.body.expectedDigest).toBe('a'.repeat(64))
    accept.flush(transportPlan({ revision: 2, digest: 'b'.repeat(64), status: 'accepted' }))

    service.replan('plan-1', {
      expectedRevision: 2,
      expectedDigest: 'b'.repeat(64),
      idempotencyKey: 'replan-key',
      title: 'Evidence response',
      nodes: transportPlan().nodes,
      edges: [],
      reason: 'Deadline moved',
      trigger: 'owner_requested',
    }).subscribe()
    const replan = http.expectOne('/api/v1/plans/plan-1/replan')
    expect(replan.request.method).toBe('POST')
    expect(replan.request.body.reason).toBe('Deadline moved')
    replan.flush(transportPlan({ revision: 3, digest: 'c'.repeat(64) }))
  })
})
