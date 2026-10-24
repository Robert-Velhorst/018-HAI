import { fakeAsync, flushMicrotasks, tick } from '@angular/core/testing'
import { EMPTY, NEVER, of, Subject, throwError } from 'rxjs'
import { ModelIntelligenceComponent } from './model-intelligence.component'
import { IModelIntelligenceOverview, IModelProfile } from '../../models/model-intelligence.model.interface'

describe('Model intelligence observation boundaries', () => {
  function fixture() {
    const service = jasmine.createSpyObj('ModelIntelligenceService', [
      'overview', 'profiles', 'tokenBudgets', 'hardware', 'powerPolicy',
      'benchmark', 'detectHardware',
    ])
    service.overview.and.returnValue(of({}))
    service.profiles.and.returnValue(of({ profiles: [] }))
    service.tokenBudgets.and.returnValue(of({}))
    service.hardware.and.returnValue(of({}))
    service.powerPolicy.and.returnValue(of({}))
    const notification = jasmine.createSpyObj('notifications', ['error', 'success', 'warning'])
    const router = jasmine.createSpyObj('router', ['navigate'])
    const preferences = jasmine.createSpyObj('preferences', ['get', 'setMode', 'setSection'])
    return { component: new ModelIntelligenceComponent(service, notification, router, preferences), service, notification, router }
  }

  for (const mode of ['empty', 'stalled'] as const) {
    for (const area of ['overview', 'profiles', 'runtime'] as const) {
      it(`releases ${area} loading with a visible error for a ${mode} observation`, fakeAsync(() => {
        const { component, service } = fixture()
        const response = mode === 'empty' ? EMPTY : NEVER
        if (area === 'overview') { service.overview.and.returnValue(response); component.refresh() }
        if (area === 'profiles') { service.profiles.and.returnValue(response); component.onProfilesOpen(true) }
        if (area === 'runtime') { service.hardware.and.returnValue(response); component.onRuntimeOpen(true) }
        tick(6001)
        expect(area === 'overview' ? component.loading : area === 'profiles' ? component.profilesLoading : component.runtimeLoading).toBeFalse()
        expect(area === 'overview' ? component.errorMessage : area === 'profiles' ? component.profilesError : component.runtimeError).toBeTruthy()
        expect(component.profilesLoaded).toBeFalse()
        expect(component.runtimeLoaded).toBeFalse()
        ;(component as any).ngOnDestroy?.()
      }))
    }
  }

  it('does not reveal raw response bodies or request URLs in model errors', () => {
    const { component, service } = fixture()
    service.overview.and.returnValue(throwError(() => ({
      status: 503,
      error: { error: 'sensitive-body-marker', message: 'sensitive-message-marker' },
      message: 'https://example.invalid?token=sensitive-url-marker',
    })))
    component.refresh()
    expect(component.errorMessage).toContain('Model outcome telemetry is unavailable')
    expect(component.errorMessage).not.toMatch(/sensitive-|example\.invalid|token=/)
  })

  it('ignores a late overview after the module is destroyed', () => {
    const { component, service } = fixture()
    const pending = new Subject<IModelIntelligenceOverview>()
    service.overview.and.returnValue(pending)
    component.refresh()
    ;(component as any).ngOnDestroy?.()
    pending.next({ calibration: { evaluatedRuns: 9 } } as IModelIntelligenceOverview)
    expect(component.overview).toBeUndefined()
    pending.complete()
  })

  it('accepts one overview without waiting for stream completion and retains it after a failed refresh', () => {
    const { component, service } = fixture()
    const pending = new Subject<IModelIntelligenceOverview>()
    const first = { calibration: { evaluatedRuns: 9 } } as IModelIntelligenceOverview
    service.overview.and.returnValue(pending)
    component.refresh()
    pending.next(first)
    expect(component.loading).toBeFalse()
    pending.next({ calibration: { evaluatedRuns: 50 } } as IModelIntelligenceOverview)
    expect(component.overview).toBe(first)
    service.overview.and.returnValue(EMPTY)
    component.refresh()
    expect(component.overview).toBe(first)
    expect(component.errorMessage).toBeTruthy()
    component.ngOnDestroy()
    pending.complete()
  })

  for (const operation of ['benchmark', 'detectHardware'] as const) {
    it(`ignores late ${operation} results and prevents a new dispatch after destruction`, () => {
      const { component, service, notification } = fixture()
      const pending = new Subject<any>()
      service[operation].and.returnValue(pending)
      const profile = { providerId: 'test', modelId: 'model' } as IModelProfile
      const run = () => operation === 'benchmark' ? component.benchmark(profile) : component.detectHardware()
      run()
      component.ngOnDestroy()
      pending.next({ ok: true, tokensPerSecond: 1, selectedServingStack: 'test' })
      expect(notification.success).not.toHaveBeenCalled()
      expect(service.overview).not.toHaveBeenCalled()
      run()
      expect(service[operation]).toHaveBeenCalledTimes(1)
      pending.complete()
    })
  }

  it('reports navigation rejection safely without an unhandled promise rejection', fakeAsync(() => {
    const { component, router, notification } = fixture()
    router.navigate.and.returnValue(Promise.reject(new Error('sensitive-route-marker')))
    component.openOutcomeRegister()
    flushMicrotasks()
    expect(notification.warning).toHaveBeenCalledWith('Panel unavailable', 'The requested model panel could not be opened. Try again.')
    component.ngOnDestroy()
  }))
})
