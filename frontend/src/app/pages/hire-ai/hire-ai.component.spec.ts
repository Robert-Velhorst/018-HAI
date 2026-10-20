import { ChangeDetectorRef } from '@angular/core'
import { fakeAsync, tick } from '@angular/core/testing'
import { FormBuilder } from '@angular/forms'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { NEVER, of, throwError } from 'rxjs'
import { HttpTimeoutPolicy } from '../../shared/http-timeout-policy'
import { HireAIService } from '../../services/hire-ai.service'
import { HireAIComponent } from './hire-ai.component'

describe('HireAIComponent', () => {
  let component: HireAIComponent
  let service: jasmine.SpyObj<HireAIService>
  let notification: jasmine.SpyObj<NzNotificationService>
  let cdr: jasmine.SpyObj<ChangeDetectorRef>

  beforeEach(() => {
    service = jasmine.createSpyObj<HireAIService>('HireAIService', ['status', 'search'])
    notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success'])
    cdr = jasmine.createSpyObj<ChangeDetectorRef>('ChangeDetectorRef', ['markForCheck'])
    component = new HireAIComponent(new FormBuilder(), service, notification, cdr)
  })

  it('fails closed when connector status cannot be refreshed', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    service.status.and.returnValue(throwError(() => new Error('offline')))

    component.refreshStatus()

    expect(component.status).toBeUndefined()
    expect(component.statusError).toContain('could not read')
    expect(cdr.markForCheck).toHaveBeenCalled()
  })

  it('does not issue searches until the connector is configured', () => {
    component.search()
    expect(service.search).not.toHaveBeenCalled()
  })

  it('waits for the connector maximum timeout and fails within the bounded UI deadline', fakeAsync(() => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    service.search.and.returnValue(NEVER)

    component.search()
    tick(30_001)

    expect(component.searchLoading).toBeTrue()
    expect(component.searchError).toBe('')
    tick(HttpTimeoutPolicy.hireAISearchMs - 30_001)

    expect(component.searchLoading).toBeFalse()
    expect(component.searchError).toContain('search failed')
    expect(notification.success).not.toHaveBeenCalled()
  }))

  it('rejects a response outside the required remote listing policy', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    service.search.and.returnValue(of({
      generatedAt: '2026-10-18T00:00:00Z',
      policy: { remoteOnly: false, listingSafety: 'clear', canonicalListingsOnly: true },
      jobs: [],
      nextCursor: null,
    }))

    component.search()

    expect(component.result).toBeUndefined()
    expect(component.searchError).toContain('did not satisfy')
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('explains when Hire.AI rejects a filter instead of reporting the connector unavailable', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    service.search.and.returnValue(throwError(() => ({ status: 400 })))

    component.search()

    expect(component.searchError).toContain('rejected one or more search filters')
    expect(component.searchError).toContain('currency')
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('maps the default any-type filter to an omitted API value', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    service.search.and.returnValue(of({
      generatedAt: '2026-10-18T00:00:00Z',
      policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
      jobs: [],
      nextCursor: null,
    }))

    component.search()

    expect(service.search).toHaveBeenCalledWith(jasmine.objectContaining({ jobType: undefined }), undefined)
    expect(component.result?.jobs).toEqual([])
    expect(notification.success).toHaveBeenCalled()
  })

  it('requires both salary bounds and a currency before searching', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    component.filters.patchValue({ salaryMin: '50000', salaryCurrency: 'EUR' })

    component.search()

    expect(service.search).not.toHaveBeenCalled()
    expect(component.searchError).toContain('both salary bounds')
    expect(component.searchLoading).toBeFalse()

    component.filters.patchValue({ salaryMax: '80000' })
    service.search.and.returnValue(of({
      generatedAt: '2026-10-18T00:00:00Z',
      policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
      jobs: [],
      nextCursor: null,
    }))
    component.search()

    expect(service.search).toHaveBeenCalledOnceWith(jasmine.objectContaining({ salaryRange: [50000, 80000] }), undefined)
  })

  it('rejects unknown and placeholder ISO currency codes before calling Hire.AI', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    component.filters.patchValue({ salaryCurrency: 'ZZZ' })

    component.search()

    expect(service.search).not.toHaveBeenCalled()
    expect(component.searchError).toContain('recognized ISO 4217 currency')

    component.filters.patchValue({ salaryMin: '50000', salaryMax: '80000', salaryCurrency: 'XXX' })
    component.search()

    expect(service.search).not.toHaveBeenCalled()
    expect(component.searchError).toContain('recognized ISO 4217 currency')
  })

  it('sends validated salary and source-platform filters', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    component.filters.patchValue({ salaryMin: '50000', salaryMax: '80000', salaryCurrency: 'eur', platformId: '7' })
    service.search.and.returnValue(of({
      generatedAt: '2026-10-18T00:00:00Z',
      policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
      jobs: [],
      nextCursor: null,
    }))

    component.search()

    expect(service.search).toHaveBeenCalledWith(jasmine.objectContaining({
      salaryRange: [50000, 80000],
      salaryCurrency: 'EUR',
      platformId: 7,
    }), undefined)
    expect(component.searchError).toBe('')
  })

  it('forwards the SmartRecruiters application-system filter supported by Hire.AI', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    component.filters.patchValue({ applicationProcess: 'smartrecruiters' })
    service.search.and.returnValue(of({
      generatedAt: '2026-10-18T00:00:00Z',
      policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
      jobs: [],
      nextCursor: null,
    }))

    component.search()

    expect(service.search).toHaveBeenCalledWith(jasmine.objectContaining({
      applicationProcess: 'smartrecruiters',
    }), undefined)
  })

  it('keeps the original filters attached to a pagination cursor', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    const cursor = { postedDate: '2026-10-17T00:00:00Z', createdAt: '2026-10-17T12:00:00Z', id: 10 }
    service.search.and.returnValues(
      of({
        generatedAt: '2026-10-18T00:00:00Z',
        policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
        jobs: [],
        nextCursor: cursor,
      }),
      of({
        generatedAt: '2026-10-18T00:00:00Z',
        policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
        jobs: [],
        nextCursor: null,
      }),
    )
    component.filters.patchValue({ query: 'support' })

    component.search()
    component.filters.patchValue({ query: 'design' })
    component.loadMore()

    expect(service.search.calls.count()).toBe(2)
    expect(service.search.calls.argsFor(0)[0].query).toBe('support')
    expect(service.search.calls.argsFor(1)).toEqual([jasmine.objectContaining({ query: 'support' }), cursor])
  })

  it('stops pagination when Hire.AI repeats a cursor', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    const cursor = { postedDate: '2026-10-17T00:00:00Z', createdAt: '2026-10-17T12:00:00Z', id: 10 }
    const page = {
      generatedAt: '2026-10-18T00:00:00Z',
      policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
      jobs: [],
      nextCursor: cursor,
    }
    service.search.and.returnValues(of(page), of(page))

    component.search()
    component.loadMore()
    component.loadMore()

    expect(service.search.calls.count()).toBe(2)
    expect(component.searchError).toContain('repeated a pagination cursor')
  })

  it('deduplicates canonical job IDs when pages overlap and keeps the newest fields', () => {
    component.status = { enabled: true, configured: true, capabilities: [], restrictions: [] }
    const cursor = { postedDate: '2026-10-17T00:00:00Z', createdAt: '2026-10-17T12:00:00Z', id: 10 }
    service.search.and.returnValues(
      of({
        generatedAt: '2026-10-18T00:00:00Z',
        policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
        jobs: [
          { id: 1, title: 'Older title', platformId: 7 },
          { id: 2, title: 'Second job', platformId: 7 },
        ],
        nextCursor: cursor,
      }),
      of({
        generatedAt: '2026-10-18T00:00:00Z',
        policy: { remoteOnly: true, listingSafety: 'clear', canonicalListingsOnly: true },
        jobs: [
          { id: 1, title: 'Updated title', platformId: 7 },
          { id: 3, title: 'Third job', platformId: 7 },
        ],
        nextCursor: null,
      }),
    )

    component.search()
    component.loadMore()

    expect(component.result?.jobs.map(job => job.id)).toEqual([1, 2, 3])
    expect(component.result?.jobs[0].title).toBe('Updated title')
  })
})
