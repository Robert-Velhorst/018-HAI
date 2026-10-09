import { TestBed } from '@angular/core/testing'
import { provideHttpClient } from '@angular/common/http'
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing'
import { OpenclawUsageComponent } from './openclaw-usage.component'

describe('OpenclawUsageComponent', () => {
  beforeEach(() => TestBed.configureTestingModule({
    imports: [OpenclawUsageComponent],
    providers: [provideHttpClient(), provideHttpClientTesting()],
  }))

  it('loads only on expansion, retains unknown cost and cancels obsolete requests', () => {
    const fixture = TestBed.createComponent(OpenclawUsageComponent)
    const http = TestBed.inject(HttpTestingController)
    fixture.componentRef.setInput('eventId', 'event-1')
    fixture.detectChanges()
    http.expectNone('/api/v1/openclaw-usage/event-1')
    fixture.componentInstance.toggle()
    fixture.componentInstance.load()
    const first = http.expectOne('/api/v1/openclaw-usage/event-1')
    first.flush({ state: 'captured', attempts: 1, snapshot: {
      source: 'openclaw.sessions.usage', scope: 'session-instance', startDate: '2026-09-05', endDate: '2026-09-05', observedAt: '2026-09-05T10:00:00Z', modelsReported: false,
      totals: { input: 12, output: 4, cacheRead: 0, cacheWrite: 0, totalTokens: 16, estimatedCostUsd: null, missingCostEntries: null },
    } })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).toContain('Unavailable')
    expect(fixture.nativeElement.textContent).toContain('16')
    expect(fixture.nativeElement.textContent).toContain('Not reported')
    fixture.componentInstance.load()
    const obsolete = http.expectOne('/api/v1/openclaw-usage/event-1')
    fixture.componentRef.setInput('eventId', 'event-2')
    fixture.detectChanges()
    expect(obsolete.cancelled).toBeTrue()
    expect(fixture.componentInstance.view).toBeUndefined()
    fixture.destroy()
    http.verify()
  })

  it('shows a recoverable read error without rendering server secrets', () => {
    const fixture = TestBed.createComponent(OpenclawUsageComponent)
    const http = TestBed.inject(HttpTestingController)
    fixture.componentRef.setInput('eventId', 'event-1')
    fixture.detectChanges()
    fixture.componentInstance.toggle()
    http.expectOne('/api/v1/openclaw-usage/event-1').flush({ error: 'private token' }, { status: 503, statusText: 'Unavailable' })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).toContain('Usage is temporarily unavailable')
    expect(fixture.nativeElement.textContent).not.toContain('private token')
    fixture.componentInstance.load()
    http.expectOne('/api/v1/openclaw-usage/event-1').flush({ state: 'retry_exhausted', attempts: 8 })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).toContain('Automatic collection stopped')
    fixture.destroy()
    http.verify()
  })
})
