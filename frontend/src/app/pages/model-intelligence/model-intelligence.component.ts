import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { forkJoin, Observable, Subject } from 'rxjs'
import { finalize, take, takeUntil, throwIfEmpty, timeout } from 'rxjs/operators'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import {
  IHardwareResponse,
  ICalibrationSummary,
  IModelIntelligenceOverview,
  IModelProfile,
  IOperationBudget,
  IPowerPolicy,
  IProviderSummary,
} from '../../models/model-intelligence.model.interface'
import { ModelIntelligenceService } from '../../services/model-intelligence.service'

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-model-intelligence',
    templateUrl: './model-intelligence.component.html',
    styleUrls: ['./model-intelligence.component.scss'],
    standalone: false
})
export class ModelIntelligenceComponent implements OnInit, OnDestroy {
  private readonly moduleId = 'model-intelligence'
  private readonly calibrationSectionId = 'model-calibration'
  private readonly destroyed$ = new Subject<void>()
  private destroyed = false
  private scrollTimer?: ReturnType<typeof setTimeout>

  overview?: IModelIntelligenceOverview
  calibration?: ICalibrationSummary
  profiles: IModelProfile[] = []
  budgets?: IOperationBudget
  hardware?: IHardwareResponse
  power?: IPowerPolicy

  loading = false
  errorMessage = ''
  profilesLoading = false
  profilesLoaded = false
  profilesError = ''
  runtimeLoading = false
  runtimeLoaded = false
  runtimeError = ''
  detectionLoading = false
  detectionError = ''
  benchmarking: Record<string, boolean> = {}
  benchmarkErrors: Record<string, string> = {}

  constructor(
    private service: ModelIntelligenceService,
    private notification: NzNotificationService,
    private router: Router,
    private viewPreferences: ModuleViewPreferencesService
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  ngOnDestroy(): void {
    this.destroyed = true
    this.destroyed$.next()
    this.destroyed$.complete()
    if (this.scrollTimer !== undefined) window.clearTimeout(this.scrollTimer)
  }

  private observeRead<T>(response: Observable<T>): Observable<T> {
    return response.pipe(
      take(1),
      timeout(6000),
      throwIfEmpty(() => new Error('Model observation completed without a response')),
      takeUntil(this.destroyed$),
    )
  }

  refresh(): void {
    if (this.destroyed || this.loading) return
    this.loading = true
    this.errorMessage = ''
    this.observeRead(this.service.overview()).pipe(
      finalize(() => { this.loading = false }),
    ).subscribe({
      next: (overview) => {
        this.overview = overview
        this.calibration = overview.calibration
        this.loading = false
        if (this.profilesLoaded) this.loadProfiles(true)
        if (this.runtimeLoaded) this.loadRuntimeDetails(true)
      },
      error: (error: HttpErrorResponse) => {
        this.loading = false
        this.errorMessage = this.errorDetail(error, 'Model outcome telemetry is unavailable.')
      },
    })
  }

  onProfilesOpen(open: boolean): void {
    if (open && !this.profilesLoaded) this.loadProfiles()
  }

  onRuntimeOpen(open: boolean): void {
    if (open && !this.runtimeLoaded) this.loadRuntimeDetails()
  }

  openOutcomeRegister(): void {
    this.openAdvancedSection(this.calibrationSectionId)
  }

  openAdvancedSection(sectionId: 'model-profiles' | 'model-calibration' | 'runtime-budget'): void {
    if (this.destroyed) return
    this.viewPreferences.setMode(this.moduleId, 'advanced')
    this.viewPreferences.setSection(this.moduleId, sectionId, true)
    document.body.classList.add('hai-view-advanced')

    void this.router.navigate(['/model-intelligence'], {
      queryParams: { mode: 'advanced' },
      fragment: sectionId,
    }).then((navigated) => {
      if (!navigated || this.destroyed) return
      if (this.scrollTimer !== undefined) window.clearTimeout(this.scrollTimer)
      this.scrollTimer = window.setTimeout(() => {
        if (!this.destroyed) document.getElementById(sectionId)?.scrollIntoView({ block: 'start' })
      })
    }).catch(() => {
      if (!this.destroyed) this.notification.warning('Panel unavailable', 'The requested model panel could not be opened. Try again.')
    })
  }

  hasAdvancedErrors(): boolean {
    return !!(this.profilesError || this.runtimeError || this.detectionError || this.hasBenchmarkErrors())
  }

  hasBenchmarkErrors(): boolean {
    return Object.values(this.benchmarkErrors).some(Boolean)
  }

  retryProfiles(): void {
    this.loadProfiles(true)
  }

  retryRuntime(): void {
    this.loadRuntimeDetails(true)
  }

  private loadProfiles(force = false): void {
    if (this.destroyed || this.profilesLoading || (this.profilesLoaded && !force)) return
    this.profilesLoading = true
    this.profilesError = ''
    this.observeRead(this.service.profiles()).pipe(
      finalize(() => { this.profilesLoading = false }),
    ).subscribe({
      next: ({ profiles }) => {
        this.profiles = profiles ?? []
        this.profilesLoaded = true
        this.profilesLoading = false
      },
      error: (error: HttpErrorResponse) => {
        this.profilesLoading = false
        this.profilesError = this.errorDetail(error, 'Model profiles could not be loaded.')
      },
    })
  }

  private loadRuntimeDetails(force = false): void {
    if (this.destroyed || this.runtimeLoading || (this.runtimeLoaded && !force)) return
    this.runtimeLoading = true
    this.runtimeError = ''
    forkJoin({
      budgets: this.observeRead(this.service.tokenBudgets()),
      hardware: this.observeRead(this.service.hardware()),
      power: this.observeRead(this.service.powerPolicy()),
    }).pipe(finalize(() => { this.runtimeLoading = false })).subscribe({
      next: ({ budgets, hardware, power }) => {
        this.budgets = budgets
        this.hardware = hardware
        this.power = power
        this.runtimeLoaded = true
        this.runtimeLoading = false
      },
      error: (error: HttpErrorResponse) => {
        this.runtimeLoading = false
        this.runtimeError = this.errorDetail(error, 'Runtime diagnostics could not be loaded.')
      },
    })
  }

  benchmark(p: IModelProfile): void {
    const key = p.providerId + '/' + p.modelId
    if (this.destroyed || this.benchmarking[key]) return
    this.benchmarking[key] = true
    this.benchmarkErrors[key] = ''
    this.service.benchmark(p.providerId, p.modelId).pipe(takeUntil(this.destroyed$)).subscribe({
      next: (res) => {
        this.benchmarking[key] = false
        if (res.ok) {
          const source = this.usageSourceLabel(res.usageSource)
          this.notification.success('Benchmarked', `${key}: ${res.inputTokens} in / ${res.outputTokens} out, ${res.tokensPerSecond.toFixed(0)} tok/s (${source})`)
        } else {
          this.notification.warning('Not benchmarked', res.detail ?? `${key} is not usable`)
        }
        this.refresh()
      },
      error: (error: HttpErrorResponse) => {
        this.benchmarking[key] = false
        this.benchmarkErrors[key] = this.errorDetail(error, 'Benchmark failed.')
        this.notification.error('Benchmark failed', this.benchmarkErrors[key])
      },
    })
  }

  detectHardware(): void {
    if (this.destroyed || this.detectionLoading) return
    this.detectionLoading = true
    this.detectionError = ''
    this.service.detectHardware().pipe(takeUntil(this.destroyed$)).subscribe({
      next: (hw) => {
        this.detectionLoading = false
        this.hardware = hw
        this.notification.success('Detected', `Serving stack: ${hw.selectedServingStack}`)
      },
      error: (error: HttpErrorResponse) => {
        this.detectionLoading = false
        this.detectionError = this.errorDetail(error, 'Hardware detection failed.')
        this.notification.error('Hardware detection failed', this.detectionError)
      },
    })
  }

  statusColor(status: string): string {
    switch (status) {
      case 'active':
        return 'green'
      case 'configured':
        return 'blue'
      case 'not_configured':
        return 'default'
      case 'unavailable':
      case 'failed':
      case 'blocked':
        return 'red'
      default:
        return 'default'
    }
  }

  providerExecutionLabel(provider: IProviderSummary): string {
    if (provider.deterministic) return 'Built-in deterministic rules; no model call'
    if (provider.localInferenceOperatorAttested) return 'Local inference (operator attested)'
    if (provider.endpointLocal) return 'Local endpoint; inference not attested'
    return 'Non-local endpoint; inference location not verified'
  }

  profileExecutionLabel(profile: IModelProfile): string {
    if (profile.deterministic) return 'Built-in deterministic rules; no model call'
    if (profile.localInferenceOperatorAttested) return 'Local inference (operator attested)'
    if (profile.endpointLocal) return 'Local endpoint; inference not attested'
    return 'Non-local endpoint; inference location not verified'
  }

  profileBillingLabel(profile: IModelProfile): string {
    if (profile.deterministic) return 'Model billing: not applicable'
    return this.billingStatusLabel(profile.billingStatus)
  }

  providerBillingLabel(provider: IProviderSummary): string {
    if (provider.deterministic) return 'Model billing: not applicable'
    return this.billingStatusLabel(provider.billingStatus)
  }

  private billingStatusLabel(status: string): string {
    switch (status) {
      case 'paid': return 'Billing: paid'
      case 'unmetered': return 'Billing: unmetered'
      default: return 'Billing: not verified'
    }
  }

  observedSpeedLabel(profile: IModelProfile): string {
    if (profile.deterministic) return 'Not applicable (no model inference)'
    if (profile.observedTokensPerSecond <= 0) return 'Not measured'
    return `${Math.round(profile.observedTokensPerSecond)} tok/s observed`
  }

  usageSourceLabel(source: string): string {
    switch (source) {
      case 'provider_reported': return 'provider-reported token counts'
      case 'provider_reported_partial': return 'partly estimated token counts'
      case 'estimated': return 'estimated token counts'
      case 'estimated_uncertain': return 'estimated token counts; uncertain'
      case 'provider_report_invalid': return 'provider usage report invalid; counts may be unreliable'
      default: return 'usage source unavailable'
    }
  }

  acceptancePercent(): number {
    if (!this.calibration?.evaluatedRuns) return 0
    return Math.round((this.calibration.acceptedOutputs / this.calibration.evaluatedRuns) * 100)
  }

  unresolvedOutcomes(): number {
    return (this.calibration?.rejectedOutputs ?? 0) + (this.calibration?.needsReview ?? 0)
  }

  confidenceColor(confidence: string): string {
    switch (confidence) {
      case 'established': return 'green'
      case 'emerging': return 'blue'
      default: return 'gold'
    }
  }

  trackLane(_: number, item: { lane: string; providerId: string; modelId: string }): string {
    return `${item.lane}/${item.providerId}/${item.modelId}`
  }

  trackModel(_: number, item: { lane?: string; providerId: string; modelId: string }): string {
    return `${item.lane ?? 'profile'}/${item.providerId}/${item.modelId}`
  }

  private errorDetail(error: HttpErrorResponse, fallback: string): string {
    // Response bodies and request URLs can contain provider credentials, even in Advanced.
    const status = Number.isInteger(error?.status) && error.status >= 400 && error.status <= 599
      ? ` HTTP ${error.status}.`
      : ''
    return fallback + status
  }
}
