import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { CapacitySnapshot, LifeDomain, RecordNeedRequest } from '../models/life-ops.model'
import { LifeOpsService } from './life-ops.service'
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('LifeOpsService', () => {
  let service: LifeOpsService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] })
    service = TestBed.inject(LifeOpsService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('uses the owner-scoped life endpoints for the initial view', () => {
    const domain: LifeDomain = {
      id: 'health_wellbeing',
      name: 'Health and wellbeing',
      description: 'Physical and mental capacity.',
      needClass: 'physiological',
      sensitive: true,
    }
    service.overview().subscribe((overview) => expect(overview.domains).toEqual([domain]))
    const request = http.expectOne('/api/v1/life/overview')
    expect(request.request.method).toBe('GET')
    request.flush({ domains: [domain], needs: [], capacity: null, goals: [], forest: [] })
  })

  it('treats a missing latest capacity snapshot as an explicit empty state', () => {
    let value: CapacitySnapshot | null | undefined
    service.latestCapacity().subscribe((result) => (value = result))

    http.expectOne('/api/v1/life/capacity/latest').flush(
      { error: 'whole-life context record not found' },
      { status: 404, statusText: 'Not Found' }
    )
    expect(value).toBeNull()
  })

  it('posts only public owner-context fields', () => {
    const body = {
      domainId: 'health_wellbeing',
      needLevel: 'health',
      state: 'attention_required',
      currentLevel: 40,
      targetLevel: 80,
      priority: 75,
      confidence: 0.8,
      evidence: ['operator report'],
      sourceLabel: 'operator',
      observedAt: '2026-07-30T10:00:00.000Z',
      needsReview: false,
      ownerIdentity: 'forged-owner',
    }
    service.recordNeed(body as RecordNeedRequest & { ownerIdentity: string }).subscribe()

    const request = http.expectOne('/api/v1/life/needs')
    expect(request.request.method).toBe('POST')
    expect(request.request.body.ownerIdentity).toBeUndefined()
    expect(request.request.body.domainId).toBe('health_wellbeing')
    request.flush({
      ...body,
      id: 'need-1',
      ownerIdentity: 'authenticated-owner',
      gap: 40,
      createdAt: body.observedAt,
    })
  })

  it('encodes entity identifiers and posts priority assessments', () => {
    service.entityDomains('case file', 'V/18?A').subscribe()
    http.expectOne('/api/v1/life/entities/case%20file/V%2F18%3FA/domains')
      .flush({ links: [] })

    service.assessPriority({
      entityType: 'goal',
      entityId: 'goal-1',
      title: 'Prepare evidence',
      factors: Object.fromEntries([
        'importance', 'urgency', 'humanNeedAffected', 'deadlinePressure',
        'costOfDelay', 'expectedValue', 'harmAvoided', 'probabilityOfSuccess',
        'effort', 'duration', 'dependencies', 'reversibility', 'risk',
        'legalObligation', 'relationshipConsequences', 'availableCapacity',
        'energyFit', 'opportunityCost', 'strategicAlignment', 'learningValue',
        'compoundingValue', 'staleness', 'commitmentAge', 'peopleBlocked',
        'delegability',
      ].map((key) => [key, 50])) as any,
    }).subscribe()
    const request = http.expectOne('/api/v1/life/priority/assess')
    expect(request.request.method).toBe('POST')
    expect(request.request.body.title).toBe('Prepare evidence')
    request.flush({
      id: 'assessment-1',
      ownerIdentity: 'authenticated-owner',
      entityType: 'goal',
      entityId: 'goal-1',
      title: 'Prepare evidence',
      score: 50,
      band: 'medium',
      factors: request.request.body.factors,
      contributions: [],
      reasons: [],
      capacityApplied: false,
      algorithmVersion: 'lifeops-mcda-v1',
      assessedAt: '2026-07-30T10:00:00Z',
    })
  })
})
