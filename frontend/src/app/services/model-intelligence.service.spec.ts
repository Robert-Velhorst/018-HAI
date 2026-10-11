import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { ModelIntelligenceService } from './model-intelligence.service'
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('ModelIntelligenceService', () => {
  let service: ModelIntelligenceService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] })
    service = TestBed.inject(ModelIntelligenceService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('loads the durable completion-first calibration summary with the overview', () => {
    service.overview().subscribe((overview) => expect(overview.calibration.unvalidatedRuns).toBe(3))

    const request = http.expectOne('/api/v1/model-intelligence/overview')
    expect(request.request.method).toBe('GET')
    request.flush({
      providers: [], lanes: [], totalProfiles: 0, activeModels: 0, telemetryRuns: 3,
      evaluatedRuns: 0, acceptedOutputs: 0, unvalidatedRuns: 3, cacheHits: 0, cacheMisses: 0,
      laneWinners: [], calibration: {
        totalRuns: 3, evaluatedRuns: 0, acceptedOutputs: 0, rejectedOutputs: 0,
        needsReview: 0, unvalidatedRuns: 3, models: [], laneLeaders: [],
        generatedAt: '2026-08-04T10:00:00Z', explanation: 'Outcome evidence first.',
      },
    })
  })
})
