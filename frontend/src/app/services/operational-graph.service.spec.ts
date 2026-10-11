import { provideHttpClient } from '@angular/common/http'
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { OperationalGraphService } from './operational-graph.service'

describe('OperationalGraphService', () => {
  let service: OperationalGraphService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] })
    service = TestBed.inject(OperationalGraphService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('loads the authenticated bounded snapshot', () => {
    service.snapshot().subscribe()
    const request = http.expectOne('/api/v1/operational-graph/snapshot')
    expect(request.request.method).toBe('GET')
    request.flush({ nodes: [], links: [], layerCounts: {}, quality: {} })
  })

  it('encodes node ids and bounds neighborhood parameters in the request', () => {
    service.neighborhood('knowledge:node/one', 2, 50).subscribe()
    const request = http.expectOne((candidate) => candidate.url === '/api/v1/operational-graph/nodes/knowledge%3Anode%2Fone/neighborhood')
    expect(request.request.params.get('depth')).toBe('2')
    expect(request.request.params.get('limit')).toBe('50')
    request.flush({ nodes: [], links: [] })
  })
})
