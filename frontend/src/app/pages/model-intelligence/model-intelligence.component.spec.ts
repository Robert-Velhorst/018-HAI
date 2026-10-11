import { CommonModule } from '@angular/common'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { Router } from '@angular/router'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { NzSpinModule } from 'ng-zorro-antd/spin'
import { of, throwError } from 'rxjs'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import {
  ICalibrationSummary,
  IModelIntelligenceOverview,
  IModelProfile,
  IProviderSummary,
} from '../../models/model-intelligence.model.interface'
import { ModelIntelligenceService } from '../../services/model-intelligence.service'
import { ModelIntelligenceComponent } from './model-intelligence.component'

describe('ModelIntelligenceComponent', () => {
  let service: jasmine.SpyObj<ModelIntelligenceService>
  let notification: jasmine.SpyObj<any>
  let component: ModelIntelligenceComponent
  let router: jasmine.SpyObj<Router>
  let preferences: ModuleViewPreferencesService

  const calibration: ICalibrationSummary = {
    totalRuns: 8, evaluatedRuns: 5, acceptedOutputs: 4, rejectedOutputs: 1,
    needsReview: 0, unvalidatedRuns: 3, models: [],
    laneLeaders: [{
      lane: 'triage', providerId: 'local', modelId: 'capable', tokensPerSecond: 12, observedSpeedSamples: 1,
      runs: 5, evaluatedRuns: 5, acceptedOutputs: 4, acceptanceRate: 0.8,
      confidence: 'emerging', averageTokens: 100, averageDurationMs: 800,
      averageCostEur: 0, reason: '4/5 evaluated outputs accepted.',
    }],
    generatedAt: '2026-08-04T10:00:00Z',
    explanation: 'Accepted output evidence ranks before efficiency.',
  }
  const overview: IModelIntelligenceOverview = {
    providers: [], lanes: ['triage'], totalProfiles: 2, activeModels: 1,
    deterministicProfiles: 2, telemetryPersistence: { state: 'durable', message: 'Model-run history is being saved to durable storage.' },
    telemetryRuns: 8, evaluatedRuns: 5, acceptedOutputs: 4, unvalidatedRuns: 3,
    cacheHits: 0, cacheMisses: 0, laneWinners: [], calibration,
  }

  beforeEach(() => {
    service = jasmine.createSpyObj<ModelIntelligenceService>('ModelIntelligenceService', [
      'overview', 'profiles', 'tokenBudgets', 'hardware', 'powerPolicy',
      'benchmark', 'detectHardware',
    ])
    notification = jasmine.createSpyObj('NzNotificationService', ['success', 'warning', 'error'])
    router = jasmine.createSpyObj<Router>('Router', ['navigate'])
    router.navigate.and.returnValue(Promise.resolve(true))
    preferences = new ModuleViewPreferencesService(document)
    localStorage.removeItem('hai.module-view.v1.model-intelligence')
    document.body.classList.remove('hai-view-advanced')
    service.overview.and.returnValue(of(overview))
    service.profiles.and.returnValue(of({ profiles: [] }))
    service.tokenBudgets.and.returnValue(of({
      maximumInputTokens: 4096, maximumOutputTokens: 1024, maximumReasoningEffort: 'medium',
      maximumContextItems: 12, maximumSourceBytes: 1000000, contextStrategy: 'relevant_only',
      cacheStrategy: 'exact', batchEligible: true,
    }))
    service.hardware.and.returnValue(of({
      profile: { operatingSystem: 'windows', windowsVersion: '11', cpuCores: 8, gpuVendor: '',
        npuVendor: '', executionProviders: [], powerMode: 'balanced', batteryStatus: 'unknown' },
      selectedServingStack: 'ollama',
    }))
    service.powerPolicy.and.returnValue(of({
      mode: 'balanced', allowHeavyWorkNow: true, deferHeavyWorkOnBattery: true, nightBatchOnly: false,
    }))
    component = new ModelIntelligenceComponent(service, notification, router, preferences)
  })

  afterEach(() => {
    localStorage.removeItem('hai.module-view.v1.model-intelligence')
    document.body.classList.remove('hai-view-advanced')
  })

  it('loads completion evidence without eagerly loading advanced diagnostics', () => {
    component.ngOnInit()

    expect(component.acceptancePercent()).toBe(80)
    expect(component.unresolvedOutcomes()).toBe(1)
    expect(component.calibration?.laneLeaders[0].reason).toContain('evaluated outputs accepted')
    expect(service.profiles).not.toHaveBeenCalled()
    expect(service.hardware).not.toHaveBeenCalled()
  })

  it('loads profiles and runtime details only when their sections open', () => {
    component.ngOnInit()
    component.onProfilesOpen(true)
    component.onRuntimeOpen(true)

    expect(service.profiles).toHaveBeenCalledTimes(1)
    expect(service.tokenBudgets).toHaveBeenCalledTimes(1)
    expect(service.hardware).toHaveBeenCalledTimes(1)
    expect(service.powerPolicy).toHaveBeenCalledTimes(1)
    expect(component.profilesLoaded).toBeTrue()
    expect(component.runtimeLoaded).toBeTrue()
  })

  it('opens the outcome register in Advanced and persists that module view', () => {
    component.openOutcomeRegister()

    expect(preferences.get('model-intelligence')).toEqual({
      version: 1,
      mode: 'advanced',
      openSections: { 'model-calibration': true },
      navigationMode: 'auto',
    })
    expect(document.body.classList.contains('hai-view-advanced')).toBeTrue()
    expect(router.navigate).toHaveBeenCalledWith(['/model-intelligence'], {
      queryParams: { mode: 'advanced' },
      fragment: 'model-calibration',
    })
    expect(preferences.get('llm-policy').mode).toBe('basic')
  })

  it('keeps calibration, provider internals, profiles, and diagnostics out of the Basic view', async () => {
    await TestBed.configureTestingModule({
      imports: [CommonModule, ControlRoomModule, NzButtonModule, NzIconModule, NzSpinModule],
      declarations: [ModelIntelligenceComponent],
      providers: [
        { provide: ModelIntelligenceService, useValue: service },
        { provide: NzNotificationService, useValue: notification },
        { provide: Router, useValue: router },
        { provide: ModuleViewPreferencesService, useValue: preferences },
      ],
    }).compileComponents()

    const fixture: ComponentFixture<ModelIntelligenceComponent> = TestBed.createComponent(ModelIntelligenceComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement

    expect(root.querySelector('.outcome-summary')?.textContent).toContain('1 model output rejected or flagged for review')
    expect(root.querySelector('.outcome-summary button')?.textContent).toContain('Review outcome evidence')
    expect(root.querySelectorAll('.hai-progressive-section--advanced').length).toBe(0)
    expect(root.querySelector('.leader-row')).toBeNull()
    expect(root.querySelector('.metric-strip')).toBeNull()
    expect(root.textContent).not.toContain('local/capable')
    expect(root.querySelector('.telemetry-persistence-status')).toBeNull()

    service.overview.and.returnValue(of({
      ...overview,
      telemetryPersistence: {
        state: 'degraded',
        message: 'Model-run history could not be saved; model output was withheld.',
      },
    }))
    fixture.componentInstance.refresh()
    expect(service.overview).toHaveBeenCalledTimes(2)
    expect(fixture.componentInstance.loading).toBeFalse()
    expect(fixture.componentInstance.overview?.telemetryPersistence.state).toBe('degraded')
    await fixture.whenStable()
    fixture.detectChanges()
    expect(root.querySelector('.telemetry-persistence-status')?.textContent).withContext(root.innerHTML).toContain('Model inference is paused')
    expect(root.querySelector('.telemetry-persistence-status')?.textContent).toContain('model output was withheld')

    service.profiles.and.returnValue(throwError(() => new Error('profile token=private-detail')))
    fixture.componentInstance.openAdvancedSection('model-profiles')
    fixture.detectChanges()
    expect(fixture.componentInstance.profilesError).toContain('private-detail')
    preferences.setMode('model-intelligence', 'basic')
    document.body.classList.remove('hai-view-advanced')
    fixture.detectChanges()
    const advancedError = root.querySelector('.advanced-error-summary')
    expect(advancedError).withContext(root.innerHTML).not.toBeNull()
    expect(advancedError?.textContent).toContain('Some Advanced model data needs attention')
    expect(root.querySelector('.advanced-error-summary')?.textContent).not.toContain('private-detail')
    root.querySelector<HTMLButtonElement>('.advanced-error-actions button')!.click()
    fixture.detectChanges()
    expect(root.querySelector('#model-profiles .hai-progressive-section__summary')?.getAttribute('aria-expanded')).toBe('true')

    root.querySelector<HTMLButtonElement>('.outcome-summary button')!.click()
    fixture.detectChanges()

    expect(root.querySelector('#model-calibration .hai-progressive-section__summary')?.getAttribute('aria-expanded')).toBe('true')
    expect(root.querySelector('.leader-row')?.textContent).toContain('local/capable')
    expect(root.querySelector('.leader-confidence')?.textContent).toContain('reported token usage')
    expect(preferences.get('model-intelligence').mode).toBe('advanced')
    fixture.destroy()
  })

  it('keeps profile and runtime errors visible with explicit retry paths', () => {
    service.profiles.and.returnValues(
      throwError(() => new Error('profile endpoint unavailable')),
      of({ profiles: [] })
    )
    component.onProfilesOpen(true)
    expect(component.profilesError).toContain('profile endpoint unavailable')
    component.retryProfiles()
    expect(component.profilesError).toBe('')
    expect(component.profilesLoaded).toBeTrue()

    service.hardware.and.returnValues(
      throwError(() => new Error('runtime endpoint unavailable')),
      of({
        profile: { operatingSystem: 'windows', windowsVersion: '11', cpuCores: 8, gpuVendor: '', npuVendor: '', executionProviders: [], powerMode: 'balanced', batteryStatus: 'unknown' },
        selectedServingStack: 'ollama',
      })
    )
    component.onRuntimeOpen(true)
    expect(component.runtimeError).toContain('runtime endpoint unavailable')
    component.retryRuntime()
    expect(component.runtimeError).toBe('')
    expect(component.runtimeLoaded).toBeTrue()
  })

  it('renders model execution claims from deterministic, endpoint, and attestation evidence', async () => {
    const providers: IProviderSummary[] = [
      { id: 'rules', name: 'Built-in rules', status: 'active', claimLevel: 'declared', local: false, endpointLocal: false, localInferenceOperatorAttested: false, billingStatus: 'unknown', models: 1, deterministic: true },
      { id: 'ollama', name: 'Ollama', status: 'active', claimLevel: 'declared', local: true, endpointLocal: true, localInferenceOperatorAttested: false, billingStatus: 'unknown', models: 1, deterministic: false },
      { id: 'attested', name: 'Attested local model', status: 'active', claimLevel: 'declared', local: true, endpointLocal: true, localInferenceOperatorAttested: true, billingStatus: 'unmetered', models: 1, deterministic: false },
    ]
    const profiles: IModelProfile[] = providers.map((provider) => ({
      providerId: provider.id,
      modelId: `${provider.id}-model`,
      displayName: provider.name,
      architectureFamily: 'test',
      lanes: ['triage'],
      contextWindow: 4096,
      local: provider.local,
      endpointLocal: provider.endpointLocal,
      localInferenceOperatorAttested: provider.localInferenceOperatorAttested,
      deterministic: provider.deterministic,
      paid: provider.billingStatus === 'unknown' ? null : provider.billingStatus === 'paid',
      billingStatus: provider.billingStatus,
      status: 'active',
      claimLevel: 'declared',
      observedTokensPerSecond: provider.deterministic ? 0 : provider.localInferenceOperatorAttested ? 24 : 0,
      observedRuns: 0,
      observedFailures: 0,
    }))
    service.overview.and.returnValue(of({ ...overview, providers }))
    service.profiles.and.returnValue(of({ profiles }))
    preferences.setMode('model-intelligence', 'advanced')
    preferences.setSection('model-intelligence', 'providers', true)
    preferences.setSection('model-intelligence', 'model-profiles', true)

    await TestBed.configureTestingModule({
      imports: [CommonModule, ControlRoomModule, NzButtonModule, NzIconModule, NzSpinModule],
      declarations: [ModelIntelligenceComponent],
      providers: [
        { provide: ModelIntelligenceService, useValue: service },
        { provide: NzNotificationService, useValue: notification },
        { provide: Router, useValue: router },
        { provide: ModuleViewPreferencesService, useValue: preferences },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(ModelIntelligenceComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    const providerRows = root.querySelectorAll<HTMLElement>('#providers tbody tr')
    const profileRows = root.querySelectorAll<HTMLElement>('#model-profiles tbody tr')

    expect(providerRows.length).toBe(3)
    expect(providerRows[0].textContent).toContain('Built-in deterministic rules; no model call')
    expect(providerRows[1].textContent).toContain('Local endpoint; inference not attested')
    expect(providerRows[2].textContent).toContain('Local inference (operator attested)')
    expect(profileRows[0].textContent).toContain('Model billing: not applicable')
    expect(profileRows[0].textContent).toContain('Not applicable (no model inference)')
    expect(profileRows[1].textContent).toContain('Billing: not verified')
    expect(profileRows[1].textContent).toContain('Not measured')
    expect(profileRows[2].textContent).toContain('Billing: unmetered')
    expect(profileRows[2].textContent).toContain('24 tok/s observed')
    fixture.destroy()
  })

  it('reports benchmark token provenance without treating unknown values as estimates', () => {
    const profile: IModelProfile = {
      providerId: 'test', modelId: 'model', displayName: 'Test model', architectureFamily: 'test',
      lanes: ['triage'], contextWindow: 4096, local: false, endpointLocal: false,
      localInferenceOperatorAttested: false, deterministic: false, paid: null, billingStatus: 'unknown',
      status: 'active', claimLevel: 'declared', observedTokensPerSecond: 0, observedRuns: 0, observedFailures: 0,
    }
    const cases = [
      ['provider_reported', 'provider-reported token counts'],
      ['provider_reported_partial', 'partly estimated token counts'],
      ['estimated', 'estimated token counts'],
      ['estimated_uncertain', 'estimated token counts; uncertain'],
      ['provider_report_invalid', 'provider usage report invalid; counts may be unreliable'],
      ['', 'usage source unavailable'],
      ['future_value', 'usage source unavailable'],
    ]

    for (const [usageSource, label] of cases) {
      notification.success.calls.reset()
      service.benchmark.and.returnValue(of({
        providerId: profile.providerId, modelId: profile.modelId, ok: true,
        inputTokens: 12, outputTokens: 4, usageSource, durationMs: 800,
        tokensPerSecond: 5, claimLevel: 'benchmarked',
      }))
      component.benchmark(profile)

      expect(notification.success.calls.mostRecent().args[0]).toBe('Benchmarked')
      expect(notification.success.calls.mostRecent().args[1]).toContain(`(${label})`)
    }
  })

  it('shows an inline retryable error when the core model outcome summary is unavailable', () => {
    service.overview.and.returnValue(throwError(() => new Error('outcome endpoint unavailable')))

    component.ngOnInit()

    expect(component.errorMessage).toContain('outcome endpoint unavailable')
    expect(component.loading).toBeFalse()
    component.refresh()
    expect(service.overview).toHaveBeenCalledTimes(2)
  })
})
