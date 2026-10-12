import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http'
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { BrainCatalogService } from './brain-catalog.service'

describe('BrainCatalogService Agent Skills inventory', () => {
  it('reads the server-owned metadata endpoint without requesting raw skill bodies', () => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()],
    })
    const service = TestBed.inject(BrainCatalogService)
    const http = TestBed.inject(HttpTestingController)
    const inventory = {
      sourceRepository: 'anthropics/skills',
      sourceCommit: 'abc123',
      refreshedAt: '2026-09-23T00:00:00Z',
      skills: [{ id: 'frontend-design', name: 'Frontend design', description: 'Design guidance', license: 'Apache-2.0', sourcePath: 'skills/frontend-design/SKILL.md', sha256: 'deadbeef', scope: 'advisory', status: 'available', selectedForTaskCount: 0 }],
    }

    let received: unknown
    service.skillInventory().subscribe((value) => { received = value })
    const request = http.expectOne('/api/v1/brain-skills/')
    expect(request.request.method).toBe('GET')
    expect(request.request.body).toBeNull()
    request.flush(inventory)
    expect(received).toEqual(inventory)
    http.verify()
  })
})
