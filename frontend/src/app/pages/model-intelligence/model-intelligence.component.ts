import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { forkJoin } from 'rxjs'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import {
  IHardwareResponse,
  ICalibrationSummary,
  IModelIntelligenceOverview,
  IModelProfile,
  IOperationBudget,
  IPowerPolicy,
} from '../../models/model-intelligence.model.interface'
import { ModelIntelligenceService } from '../../services/model-intelligence.service'

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-model-intelligence',
  templateUrl: './model-intelligence.component.html',
  styleUrls: ['./model-intelligence.component.scss'],
})
export class ModelIntelligenceComponent implements OnInit {
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
  runtimeLoading = false
  runtimeLoaded = false
  benchmarking: Record<string, boolean> = {}

  constructor(
    private service: ModelIntelligenceService,
    private notification: NzNotificationService
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  refresh(): void {
    this.loading = true
    this.errorMessage = ''
    forkJoin({
      overview: this.service.overview(),
      calibration: this.service.calibration(),
    }).subscribe({
      next: ({ overview, calibration }) => {
        this.overview = overview
        this.calibration = calibration
        this.loading = false
        if (this.profilesLoaded) this.loadProfiles()
        if (this.runtimeLoaded) this.loadRuntimeDetails()
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

  private loadProfiles(): void {
    this.profilesLoading = true
    this.service.profiles().subscribe({
      next: ({ profiles }) => {
        this.profiles = profiles ?? []
        this.profilesLoaded = true
        this.profilesLoading = false
      },
      error: (error: HttpErrorResponse) => {
        this.profilesLoading = false
        this.notification.error('Profiles unavailable', this.errorDetail(error, 'Model profiles could not be loaded.'))
      },
    })
  }

  private loadRuntimeDetails(): void {
    this.runtimeLoading = true
    forkJoin({
      budgets: this.service.tokenBudgets(),
      hardware: this.service.hardware(),
      power: this.service.powerPolicy(),
    }).subscribe({
      next: ({ budgets, hardware, power }) => {
        this.budgets = budgets
        this.hardware = hardware
        this.power = power
        this.runtimeLoaded = true
        this.runtimeLoading = false
      },
      error: (error: HttpErrorResponse) => {
        this.runtimeLoading = false
        this.notification.error('Runtime details unavailable', this.errorDetail(error, 'Runtime diagnostics could not be loaded.'))
      },
    })
  }

  benchmark(p: IModelProfile): void {
    const key = p.providerId + '/' + p.modelId
    this.benchmarking[key] = true
    this.service.benchmark(p.providerId, p.modelId).subscribe({
      next: (res) => {
        this.benchmarking[key] = false
        if (res.ok) {
          this.notification.success('Benchmarked', `${key}: ${res.tokensPerSecond.toFixed(0)} tok/s`)
        } else {
          this.notification.warning('Not benchmarked', res.detail ?? `${key} is not usable`)
        }
        this.refresh()
      },
      error: () => {
        this.benchmarking[key] = false
        this.notification.error('Error', 'Benchmark failed.')
      },
    })
  }

  detectHardware(): void {
    this.service.detectHardware().subscribe({
      next: (hw) => {
        this.hardware = hw
        this.notification.success('Detected', `Serving stack: ${hw.selectedServingStack}`)
      },
      error: () => this.notification.error('Error', 'Hardware detect failed.'),
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
    return error.error?.error || error.error?.message || error.message || fallback
  }
}
