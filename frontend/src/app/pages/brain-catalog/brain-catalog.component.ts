import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import {
  BrainCatalogCollectionDisposition,
  IBrainCatalogAdoptionPlan,
  IBrainCatalogCapabilityRecommendationResponse,
  IBrainCatalogEntry,
  IBrainCatalogOSSInsightDiscovery,
  IBrainCatalogOSSInsightDiscoveryReport,
  IBrainCatalogOSSInsightKnownProfileHit,
  IBrainCatalogOSSInsightReview,
  IBrainCatalogRepositoryDiscoveryMaintenanceReview,
  IBrainCatalogRevalidationRun,
  IBrainCatalogResponse,
  IBrainCatalogUpstreamReview,
} from '../../models/brain-catalog.model.interface'
import { IRAGFlowStatus } from '../../models/ragflow.model.interface'
import { IAnythingLLMStatus } from '../../models/anythingllm.model.interface'
import { IPresidioStatus } from '../../models/presidio.model.interface'
import { ILangfuseExportResult, ILangfuseProbeResult, ILangfuseStatus } from '../../models/langfuse.model.interface'
import { ISerenaStatus } from '../../models/serena.model.interface'
import { IOpenLITExportResult, IOpenLITStatus } from '../../models/openlit.model.interface'
import { IMLflowStatus } from '../../models/mlflow.model.interface'
import { IMiniSWEStatus } from '../../models/miniswe.model.interface'
import { IGitleaksStatus } from '../../models/gitleaks.model.interface'
import { IGosecStatus } from '../../models/gosec.model.interface'
import { ITrivyStatus } from '../../models/trivy.model.interface'
import { IGrypeStatus } from '../../models/grype.model.interface'
import { ISyftStatus } from '../../models/syft.model.interface'
import { IBrowserVerificationStatus } from '../../models/browser-verification.model.interface'
import {
  IAgentFrameworkMigrationPlan,
  IAutoGenCompatibilityEvent,
} from '../../models/autogen-compat.model.interface'
import { BrainCatalogService } from '../../services/brain-catalog.service'
import { LangfuseService } from '../../services/langfuse.service'
import { PresidioService } from '../../services/presidio.service'
import { PursuitService } from '../../services/pursuit.service'
import { RAGFlowService } from '../../services/ragflow.service'
import { AnythingLLMService } from '../../services/anythingllm.service'
import { SerenaService } from '../../services/serena.service'
import { OpenLITService } from '../../services/openlit.service'
import { MLflowService } from '../../services/mlflow.service'
import { MiniSWEService } from '../../services/miniswe.service'
import { GitleaksService } from '../../services/gitleaks.service'
import { GosecService } from '../../services/gosec.service'
import { TrivyService } from '../../services/trivy.service'
import { GrypeService } from '../../services/grype.service'
import { SyftService } from '../../services/syft.service'
import { BrowserVerificationService } from '../../services/browser-verification.service'
import { AutoGenCompatibilityService } from '../../services/autogen-compat.service'

interface BoundedCatalogStatus {
  configured: boolean
  endpoint?: string
  scope: string
  restrictions?: string[]
  workspaces?: string[]
  experimentIds?: string[]
  metricKeys?: string[]
  profiles?: Array<unknown>
  configError?: string
}

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-brain-catalog',
  templateUrl: './brain-catalog.component.html',
  styleUrls: ['./brain-catalog.component.scss'],
})
export class BrainCatalogComponent implements OnInit {
  catalog?: IBrainCatalogResponse
  selected?: IBrainCatalogEntry
  loading = false
  loadFailed = false
  reviewingCandidateId = ''
  reviewingDiscoveryRepository = ''
  verifyingDiscoveryRepository = ''
  revalidatingId = ''
  checkingOSSInsight = false
  runningCatalogRevalidation = false
  discoveringOSSInsight = false
  loadingAdoptionPlan = false
  recommendingCapabilities = false
  discoveryDisplayLimit = 30
  knownProfileDisplayLimit = 20
  upstreamReview?: IBrainCatalogUpstreamReview
  ossInsightReview?: IBrainCatalogOSSInsightReview
  catalogRevalidation?: IBrainCatalogRevalidationRun
  collectionMaintenanceHistory: IBrainCatalogOSSInsightReview[] = []
  collectionMaintenanceHistoryUnavailable = false
  repositoryDiscoveryMaintenanceHistory: IBrainCatalogRepositoryDiscoveryMaintenanceReview[] = []
  repositoryDiscoveryMaintenanceHistoryUnavailable = false
  ossInsightDiscovery?: IBrainCatalogOSSInsightDiscoveryReport
  adoptionPlan?: IBrainCatalogAdoptionPlan
  capabilityRecommendation?: IBrainCatalogCapabilityRecommendationResponse
  discoveryReviews: Record<string, IBrainCatalogUpstreamReview> = {}
  ragflowStatus?: IRAGFlowStatus
  loadingRAGFlowStatus = false
  ragflowStatusUnavailable = false
  anythingLLMStatus?: IAnythingLLMStatus
  loadingAnythingLLMStatus = false
  anythingLLMStatusUnavailable = false
  presidioStatus?: IPresidioStatus
  loadingPresidioStatus = false
  presidioStatusUnavailable = false
  langfuseStatus?: ILangfuseStatus
  loadingLangfuseStatus = false
  langfuseStatusUnavailable = false
  probingLangfuse = false
  exportingLangfuse = false
  langfuseProbe?: ILangfuseProbeResult
  langfuseExport?: ILangfuseExportResult
  serenaStatus?: ISerenaStatus
  loadingSerenaStatus = false
  serenaStatusUnavailable = false
  openLITStatus?: IOpenLITStatus
  loadingOpenLITStatus = false
  openLITStatusUnavailable = false
  exportingOpenLIT = false
  openLITExport?: IOpenLITExportResult
  mlflowStatus?: IMLflowStatus
  loadingMLflowStatus = false
  mlflowStatusUnavailable = false
  miniSWEStatus?: IMiniSWEStatus
  loadingMiniSWEStatus = false
  miniSWEStatusUnavailable = false
  gitleaksStatus?: IGitleaksStatus
  loadingGitleaksStatus = false
  gitleaksStatusUnavailable = false
  gosecStatus?: IGosecStatus
  loadingGosecStatus = false
  gosecStatusUnavailable = false
  trivyStatus?: ITrivyStatus
  loadingTrivyStatus = false
  trivyStatusUnavailable = false
  grypeStatus?: IGrypeStatus
  loadingGrypeStatus = false
  grypeStatusUnavailable = false
  syftStatus?: ISyftStatus
  loadingSyftStatus = false
  syftStatusUnavailable = false
  browserVerificationStatus?: IBrowserVerificationStatus
  loadingBrowserVerificationStatus = false
  browserVerificationStatusUnavailable = false
  planningFrameworkMigration = false
  frameworkMigrationPlan?: IAgentFrameworkMigrationPlan
  readonly frameworkMigrationExample = JSON.stringify(
    {
      workloadId: 'legacy-workload',
      events: [
        {
          id: 'event-1',
          type: 'handoff',
          agent: 'triage',
          correlationId: 'review-1',
          summary: 'Prepare a source-backed review.',
        },
      ],
    },
    null,
    2
  )

  constructor(
    private service: BrainCatalogService,
    private pursuitService: PursuitService,
    private ragflowService: RAGFlowService,
    private anythingLLMService: AnythingLLMService,
    private presidioService: PresidioService,
    private langfuseService: LangfuseService,
    private serenaService: SerenaService,
    private openLITService: OpenLITService,
    private mlflowService: MLflowService,
    private miniSWEService: MiniSWEService,
    private gitleaksService: GitleaksService,
    private gosecService: GosecService,
    private trivyService: TrivyService,
    private grypeService: GrypeService,
    private syftService: SyftService,
    private browserVerificationService: BrowserVerificationService,
    private autoGenCompatibilityService: AutoGenCompatibilityService,
    private notification: NzNotificationService,
    private router: Router,
  ) {}

  ngOnInit(): void { this.refresh() }

  refresh(): void {
    this.loading = true
    this.loadFailed = false
    this.service.overview().subscribe({
      next: (catalog) => {
        this.catalog = catalog
        this.select(this.integrated[0] ?? catalog.entries[0])
        this.loadCollectionMaintenanceHistory()
        this.loadRepositoryDiscoveryMaintenanceHistory()
        this.loading = false
      },
      error: () => {
        this.loading = false
        this.loadFailed = true
        this.notification.error('Catalog unavailable', 'HAI could not load the reviewed capability catalog.')
      },
    })
  }

  get integrated(): IBrainCatalogEntry[] {
    return (this.catalog?.entries ?? []).filter((entry) => entry.status === 'integrated_profile')
  }

  get candidates(): IBrainCatalogEntry[] {
    return (this.catalog?.entries ?? []).filter((entry) =>
      entry.status === 'candidate' || entry.status === 'compatibility_only'
    )
  }

  get held(): IBrainCatalogEntry[] {
    return (this.catalog?.entries ?? []).filter((entry) =>
      entry.status === 'reference_only' || entry.status === 'license_review' || entry.status === 'excluded'
    )
  }

  select(entry: IBrainCatalogEntry): void {
    this.selected = entry
    this.upstreamReview = undefined
    this.ragflowStatus = undefined
    this.ragflowStatusUnavailable = false
    this.anythingLLMStatus = undefined
    this.anythingLLMStatusUnavailable = false
    this.presidioStatus = undefined
    this.presidioStatusUnavailable = false
    this.langfuseStatus = undefined
    this.langfuseStatusUnavailable = false
    this.langfuseProbe = undefined
    this.langfuseExport = undefined
    this.serenaStatus = undefined
    this.serenaStatusUnavailable = false
    this.openLITStatus = undefined
    this.openLITStatusUnavailable = false
    this.openLITExport = undefined
    this.mlflowStatus = undefined
    this.mlflowStatusUnavailable = false
    this.miniSWEStatus = undefined
    this.miniSWEStatusUnavailable = false
    this.gitleaksStatus = undefined
    this.gitleaksStatusUnavailable = false
    this.gosecStatus = undefined
    this.gosecStatusUnavailable = false
    this.trivyStatus = undefined
    this.trivyStatusUnavailable = false
    this.grypeStatus = undefined
    this.grypeStatusUnavailable = false
    this.syftStatus = undefined
    this.syftStatusUnavailable = false
    this.browserVerificationStatus = undefined
    this.browserVerificationStatusUnavailable = false
    this.frameworkMigrationPlan = undefined
    if (entry.id === 'ragflow') this.loadRAGFlowStatus()
    if (entry.id === 'anythingllm') this.loadAnythingLLMStatus()
    if (entry.id === 'presidio') this.loadPresidioStatus()
    if (entry.id === 'langfuse') this.loadLangfuseStatus()
    if (entry.id === 'serena') this.loadSerenaStatus()
    if (entry.id === 'openlit') this.loadOpenLITStatus()
    if (entry.id === 'mlflow') this.loadMLflowStatus()
    if (entry.id === 'mini-swe-agent') this.loadMiniSWEStatus()
    if (entry.id === 'gitleaks') this.loadGitleaksStatus()
    if (entry.id === 'gosec') this.loadGosecStatus()
    if (entry.id === 'trivy') this.loadTrivyStatus()
    if (entry.id === 'grype') this.loadGrypeStatus()
    if (entry.id === 'syft') this.loadSyftStatus()
    if (entry.id === 'playwright') this.loadBrowserVerificationStatus()
  }

  loadRAGFlowStatus(): void {
    if (this.loadingRAGFlowStatus) return
    this.loadingRAGFlowStatus = true
    this.ragflowService.status().subscribe({
      next: (status) => {
        this.loadingRAGFlowStatus = false
        this.ragflowStatus = status
      },
      error: () => {
        this.loadingRAGFlowStatus = false
        this.ragflowStatusUnavailable = true
      },
    })
  }

  loadAnythingLLMStatus(): void {
    if (this.loadingAnythingLLMStatus) return
    this.loadingAnythingLLMStatus = true
    this.anythingLLMService.status().subscribe({
      next: (status) => {
        this.loadingAnythingLLMStatus = false
        this.anythingLLMStatus = status
      },
      error: () => {
        this.loadingAnythingLLMStatus = false
        this.anythingLLMStatusUnavailable = true
      },
    })
  }

  loadPresidioStatus(): void {
    if (this.loadingPresidioStatus) return
    this.loadingPresidioStatus = true
    this.presidioService.status().subscribe({
      next: (status) => {
        this.loadingPresidioStatus = false
        this.presidioStatus = status
      },
      error: () => {
        this.loadingPresidioStatus = false
        this.presidioStatusUnavailable = true
      },
    })
  }

  loadLangfuseStatus(): void {
    if (this.loadingLangfuseStatus) return
    this.loadingLangfuseStatus = true
    this.langfuseService.status().subscribe({
      next: (status) => {
        this.loadingLangfuseStatus = false
        this.langfuseStatus = status
      },
      error: () => {
        this.loadingLangfuseStatus = false
        this.langfuseStatusUnavailable = true
      },
    })
  }

  loadSerenaStatus(): void {
    if (this.loadingSerenaStatus) return
    this.loadingSerenaStatus = true
    this.serenaService.status().subscribe({
      next: (status) => {
        this.loadingSerenaStatus = false
        this.serenaStatus = status
      },
      error: () => {
        this.loadingSerenaStatus = false
        this.serenaStatusUnavailable = true
      },
    })
  }

  loadOpenLITStatus(): void {
    if (this.loadingOpenLITStatus) return
    this.loadingOpenLITStatus = true
    this.openLITService.status().subscribe({
      next: (status) => {
        this.loadingOpenLITStatus = false
        this.openLITStatus = status
      },
      error: () => {
        this.loadingOpenLITStatus = false
        this.openLITStatusUnavailable = true
      },
    })
  }

  loadMLflowStatus(): void {
    if (this.loadingMLflowStatus) return
    this.loadingMLflowStatus = true
    this.mlflowService.status().subscribe({
      next: (status) => {
        this.loadingMLflowStatus = false
        this.mlflowStatus = status
      },
      error: () => {
        this.loadingMLflowStatus = false
        this.mlflowStatusUnavailable = true
      },
    })
  }

  loadMiniSWEStatus(): void {
    if (this.loadingMiniSWEStatus) return
    this.loadingMiniSWEStatus = true
    this.miniSWEService.status().subscribe({
      next: (status) => {
        this.loadingMiniSWEStatus = false
        this.miniSWEStatus = status
      },
      error: () => {
        this.loadingMiniSWEStatus = false
        this.miniSWEStatusUnavailable = true
      },
    })
  }

  loadGitleaksStatus(): void {
    if (this.loadingGitleaksStatus) return
    this.loadingGitleaksStatus = true
    this.gitleaksService.status().subscribe({
      next: (status) => {
        this.loadingGitleaksStatus = false
        this.gitleaksStatus = status
      },
      error: () => {
        this.loadingGitleaksStatus = false
        this.gitleaksStatusUnavailable = true
      },
    })
  }

  loadGosecStatus(): void {
    if (this.loadingGosecStatus) return
    this.loadingGosecStatus = true
    this.gosecService.status().subscribe({
      next: (status) => {
        this.loadingGosecStatus = false
        this.gosecStatus = status
      },
      error: () => {
        this.loadingGosecStatus = false
        this.gosecStatusUnavailable = true
      },
    })
  }

  loadTrivyStatus(): void {
    if (this.loadingTrivyStatus) return
    this.loadingTrivyStatus = true
    this.trivyService.status().subscribe({
      next: (status) => {
        this.loadingTrivyStatus = false
        this.trivyStatus = status
      },
      error: () => {
        this.loadingTrivyStatus = false
        this.trivyStatusUnavailable = true
      },
    })
  }

  loadGrypeStatus(): void {
    if (this.loadingGrypeStatus) return
    this.loadingGrypeStatus = true
    this.grypeService.status().subscribe({
      next: (status) => {
        this.loadingGrypeStatus = false
        this.grypeStatus = status
      },
      error: () => {
        this.loadingGrypeStatus = false
        this.grypeStatusUnavailable = true
      },
    })
  }

  loadSyftStatus(): void {
    if (this.loadingSyftStatus) return
    this.loadingSyftStatus = true
    this.syftService.status().subscribe({
      next: (status) => {
        this.loadingSyftStatus = false
        this.syftStatus = status
      },
      error: () => {
        this.loadingSyftStatus = false
        this.syftStatusUnavailable = true
      },
    })
  }

  loadBrowserVerificationStatus(): void {
    if (this.loadingBrowserVerificationStatus) return
    this.loadingBrowserVerificationStatus = true
    this.browserVerificationService.status().subscribe({
      next: (status) => {
        this.loadingBrowserVerificationStatus = false
        this.browserVerificationStatus = status
      },
      error: () => {
        this.loadingBrowserVerificationStatus = false
        this.browserVerificationStatusUnavailable = true
      },
    })
  }

  boundedStatus(id: string): BoundedCatalogStatus | undefined {
    switch (id) {
      case 'openlit': return this.openLITStatus
      case 'mlflow': return this.mlflowStatus
      case 'mini-swe-agent': return this.miniSWEStatus
      case 'gitleaks': return this.gitleaksStatus
      case 'gosec': return this.gosecStatus
      case 'trivy': return this.trivyStatus
      case 'grype': return this.grypeStatus
      case 'syft': return this.syftStatus
      case 'playwright':
        return this.browserVerificationStatus
          ? {
              configured: this.browserVerificationStatus.configured,
              endpoint: this.browserVerificationStatus.runnerUrl,
              profiles: this.browserVerificationStatus.profiles,
              configError: this.browserVerificationStatus.configError,
              scope: this.browserVerificationStatus.scope,
            }
          : undefined
      default: return undefined
    }
  }

  boundedStatusLoading(id: string): boolean {
    const flags: Record<string, boolean> = {
      openlit: this.loadingOpenLITStatus,
      mlflow: this.loadingMLflowStatus,
      'mini-swe-agent': this.loadingMiniSWEStatus,
      gitleaks: this.loadingGitleaksStatus,
      gosec: this.loadingGosecStatus,
      trivy: this.loadingTrivyStatus,
      grype: this.loadingGrypeStatus,
      syft: this.loadingSyftStatus,
      playwright: this.loadingBrowserVerificationStatus,
    }
    return Boolean(flags[id])
  }

  boundedStatusUnavailable(id: string): boolean {
    const flags: Record<string, boolean> = {
      openlit: this.openLITStatusUnavailable,
      mlflow: this.mlflowStatusUnavailable,
      'mini-swe-agent': this.miniSWEStatusUnavailable,
      gitleaks: this.gitleaksStatusUnavailable,
      gosec: this.gosecStatusUnavailable,
      trivy: this.trivyStatusUnavailable,
      grype: this.grypeStatusUnavailable,
      syft: this.syftStatusUnavailable,
      playwright: this.browserVerificationStatusUnavailable,
    }
    return Boolean(flags[id])
  }

  isBoundedStatusProfile(id: string): boolean {
    return [
      'openlit',
      'mlflow',
      'mini-swe-agent',
      'gitleaks',
      'gosec',
      'trivy',
      'grype',
      'syft',
      'playwright',
    ].includes(id)
  }

  probeLangfuse(): void {
    if (this.probingLangfuse) return
    this.probingLangfuse = true
    this.langfuseProbe = undefined
    this.langfuseService.probe().subscribe({
      next: (result) => {
        this.probingLangfuse = false
        this.langfuseProbe = result
        this.notification.success('Langfuse ready', 'HAI verified the configured local health and readiness endpoints. No trace was exported.')
      },
      error: () => {
        this.probingLangfuse = false
        this.notification.error('Langfuse not ready', 'HAI could not verify the configured local health and readiness endpoints. No trace was exported.')
      },
    })
  }

  exportLangfuseSnapshot(): void {
    if (this.exportingLangfuse) return
    this.exportingLangfuse = true
    this.langfuseExport = undefined
    this.langfuseService.exportOperationalSnapshot().subscribe({
      next: (result) => {
        this.exportingLangfuse = false
        this.langfuseExport = result
        this.notification.success('Aggregate trace exported', "Langfuse accepted HAI's fixed aggregate operational snapshot. No prompts, sources, or workflow records were exported.")
      },
      error: () => {
        this.exportingLangfuse = false
        this.notification.error('Trace export unavailable', 'Langfuse did not accept the aggregate operational snapshot. No HAI workflow or policy state changed.')
      },
    })
  }

  exportOpenLITSnapshot(): void {
    if (this.exportingOpenLIT) return
    this.exportingOpenLIT = true
    this.openLITExport = undefined
    this.openLITService.exportOperationalSnapshot().subscribe({
      next: (result) => {
        this.exportingOpenLIT = false
        this.openLITExport = result
        this.notification.success(
          'Aggregate trace exported',
          "OpenLIT accepted HAI's fixed aggregate operational snapshot. No prompts, sources, tokens, models, or workflow records were exported."
        )
      },
      error: () => {
        this.exportingOpenLIT = false
        this.notification.error(
          'Trace export unavailable',
          'OpenLIT did not accept the aggregate operational snapshot. No HAI workflow or policy state changed.'
        )
      },
    })
  }

  openMCPToolboxRuntimeLab(): void {
    this.router.navigate(['/runtime-lab'])
  }

  prepareFrameworkMigration(input: string): void {
    if (this.planningFrameworkMigration) return
    let parsed: { workloadId?: unknown; events?: unknown }
    try {
      parsed = JSON.parse(input)
    } catch {
      this.notification.error(
        'Invalid migration sample',
        'Use a redacted JSON object with workloadId and 1-100 supported event envelopes. Nothing was sent to an agent runtime.'
      )
      return
    }
    if (
      typeof parsed.workloadId !== 'string' ||
      !Array.isArray(parsed.events) ||
      parsed.events.length < 1 ||
      parsed.events.length > 100
    ) {
      this.notification.error(
        'Invalid migration sample',
        'A workloadId string and 1-100 event envelopes are required. Nothing was sent to an agent runtime.'
      )
      return
    }
    this.planningFrameworkMigration = true
    this.frameworkMigrationPlan = undefined
    this.autoGenCompatibilityService
      .microsoftAgentFrameworkMigrationPlan(
        parsed.workloadId,
        parsed.events as IAutoGenCompatibilityEvent[]
      )
      .subscribe({
        next: (plan) => {
          this.planningFrameworkMigration = false
          this.frameworkMigrationPlan = plan
          this.notification.success(
            'Migration plan prepared',
            'HAI created a transient control map. Microsoft Agent Framework was not installed or started.'
          )
        },
        error: () => {
          this.planningFrameworkMigration = false
          this.notification.error(
            'Migration plan unavailable',
            'HAI could not validate the redacted sample. No sample was stored and no agent runtime was started.'
          )
        },
      })
  }

  selectById(id: string): void {
    const entry = this.catalog?.entries.find((candidate) => candidate.id === id)
    if (entry) this.select(entry)
  }

  startRoadmapReview(id: string): void {
    const entry = this.catalog?.entries.find((candidate) => candidate.id === id)
    if (entry) this.startAdapterReview(entry)
  }

  loadAdoptionPlan(): void {
    if (this.loadingAdoptionPlan || this.adoptionPlan) return
    this.loadingAdoptionPlan = true
    this.service.adoptionPlan().subscribe({
      next: (plan) => {
        this.loadingAdoptionPlan = false
        this.adoptionPlan = plan
      },
      error: () => {
        this.loadingAdoptionPlan = false
        this.notification.error('Implementation roadmap unavailable', 'HAI could not load the read-only adoption queue. No catalog, runtime, or approval state changed.')
      },
    })
  }

  revalidate(entry: IBrainCatalogEntry): void {
    if (this.revalidatingId) return
    this.revalidatingId = entry.id
    this.upstreamReview = undefined
    this.service.revalidate(entry.id).subscribe({
      next: (review) => {
        this.revalidatingId = ''
        this.upstreamReview = review
        this.notification.success('Upstream rechecked', `${entry.name} was checked without changing its HAI activation state.`)
      },
      error: () => {
        this.revalidatingId = ''
        this.notification.error('Upstream recheck unavailable', 'HAI could not retrieve public GitHub metadata. No catalog decision or runtime state changed.')
      },
    })
  }

  revalidateOSSInsightCollections(): void {
    if (this.checkingOSSInsight) return
    this.checkingOSSInsight = true
    this.ossInsightReview = undefined
    this.service.revalidateOSSInsightCollections().subscribe({
      next: (review) => {
        this.checkingOSSInsight = false
        this.ossInsightReview = review
        const drift = (review.newCollections?.length ?? 0) + (review.missingExpected?.length ?? 0)
        this.notification.success('OSS Insight checked', drift ? 'Collection drift needs catalog review. No project was installed or activated.' : 'The collection snapshot still matches. No project was installed or activated.')
      },
      error: () => {
        this.checkingOSSInsight = false
        this.notification.error('OSS Insight check unavailable', 'HAI could not retrieve the public collection list. No catalog decision or runtime state changed.')
      },
    })
  }

  runDueCatalogRevalidations(): void {
    if (this.runningCatalogRevalidation) return
    this.runningCatalogRevalidation = true
    this.catalogRevalidation = undefined
    this.service.runDueRevalidations().subscribe({
      next: (run) => {
        this.runningCatalogRevalidation = false
        this.catalogRevalidation = run
        this.loadCollectionMaintenanceHistory()
        this.loadRepositoryDiscoveryMaintenanceHistory()
        if (!run.enabled) {
          this.notification.error(
            'Catalog maintenance is disabled',
            'Enable bounded public metadata checks before running maintenance. Catalog and runtime state were not changed.'
          )
          return
        }
        const summary =
          `${run.checked} checked, ${run.reused} still current, ` +
          `${run.failed} unavailable or failed.`
        if (run.failed > 0) {
          this.notification.error('Catalog maintenance needs review', summary)
        } else {
          this.notification.success('Catalog maintenance complete', summary)
        }
      },
      error: () => {
        this.runningCatalogRevalidation = false
        this.notification.error(
          'Catalog maintenance unavailable',
          'HAI could not check the fixed public upstream metadata. Catalog and runtime state were not changed.'
        )
      },
    })
  }

  private loadCollectionMaintenanceHistory(): void {
    this.collectionMaintenanceHistoryUnavailable = false
    this.service.collectionRevalidationHistory().subscribe({
      next: (history) => (this.collectionMaintenanceHistory = history || []),
      error: () => {
        this.collectionMaintenanceHistory = []
        this.collectionMaintenanceHistoryUnavailable = true
      },
    })
  }

  private loadRepositoryDiscoveryMaintenanceHistory(): void {
    this.repositoryDiscoveryMaintenanceHistoryUnavailable = false
    this.service.repositoryDiscoveryRevalidationHistory().subscribe({
      next: (history) => (this.repositoryDiscoveryMaintenanceHistory = history || []),
      error: () => {
        this.repositoryDiscoveryMaintenanceHistory = []
        this.repositoryDiscoveryMaintenanceHistoryUnavailable = true
      },
    })
  }

  discoverOSSInsightRepositories(): void {
    if (this.discoveringOSSInsight) return
    this.discoveringOSSInsight = true
    this.ossInsightDiscovery = undefined
    this.service.discoverOSSInsightRepositories().subscribe({
      next: (report) => {
        this.discoveringOSSInsight = false
        this.ossInsightDiscovery = report
        this.discoveryReviews = {}
        this.discoveryDisplayLimit = 30
        this.knownProfileDisplayLimit = 20
        this.notification.success('Candidate discovery complete', `${report.discoveries?.length ?? 0} unreviewed repositories were found. No catalog entry, credential, or runtime state changed.`)
      },
      error: () => {
        this.discoveringOSSInsight = false
        this.notification.error('Candidate discovery unavailable', 'HAI could not inspect the reviewed OSS Insight categories. No catalog decision or runtime state changed.')
      },
    })
  }

  discoverReviewableOSSInsightRepositories(): void {
    if (this.discoveringOSSInsight) return
    this.discoveringOSSInsight = true
    this.ossInsightDiscovery = undefined
    this.service.discoverReviewableOSSInsightRepositories().subscribe({
      next: (report) => {
        this.discoveringOSSInsight = false
        this.ossInsightDiscovery = report
        this.discoveryReviews = {}
        this.discoveryDisplayLimit = 30
        this.knownProfileDisplayLimit = 20
        this.notification.success('Relevant discovery complete', `${report.discoveries?.length ?? 0} unreviewed repositories were found across candidate and represented categories. No catalog entry, credential, or runtime state changed.`)
      },
      error: () => {
        this.discoveringOSSInsight = false
        this.notification.error('Relevant discovery unavailable', 'HAI could not inspect the reviewed OSS Insight categories. No catalog decision or runtime state changed.')
      },
    })
  }

  verifyDiscovery(discovery: IBrainCatalogOSSInsightDiscovery): void {
    if (this.verifyingDiscoveryRepository) return
    this.verifyingDiscoveryRepository = discovery.repository
    this.service.revalidateOSSInsightDiscovery(discovery.repository, this.ossInsightDiscovery?.scope ?? 'candidate').subscribe({
      next: (review) => {
        this.verifyingDiscoveryRepository = ''
        this.discoveryReviews = { ...this.discoveryReviews, [discovery.repository]: review }
        this.notification.success('Candidate metadata verified', `${discovery.repository} remains unconfigured. Review the metadata before creating a manual adapter review.`)
      },
      error: () => {
        this.verifyingDiscoveryRepository = ''
        this.notification.error('Candidate metadata unavailable', 'HAI could not verify this source-discovered repository. It was not added to the catalog or activated.')
      },
    })
  }

  recommendCapabilities(need: string): void {
    if (this.recommendingCapabilities) return
    const normalized = need.trim()
    if (!normalized) {
      this.notification.error('Describe the capability need', 'Use a concrete need such as local model evaluation or read-only browser verification.')
      return
    }
    this.recommendingCapabilities = true
    this.capabilityRecommendation = undefined
    this.service.recommendCapabilities(normalized).subscribe({
      next: (recommendation) => {
        this.recommendingCapabilities = false
        this.capabilityRecommendation = recommendation
      },
      error: () => {
        this.recommendingCapabilities = false
        this.notification.error('Capability recommendation unavailable', 'HAI could not search its reviewed catalog. No upstream was queried and no runtime state changed.')
      },
    })
  }

  queueDiscoveryReview(discovery: IBrainCatalogOSSInsightDiscovery): void {
    if (this.reviewingDiscoveryRepository) return
    if (discovery.reviewAllowed === false) {
      this.notification.info(
        'Discovery is not an adapter candidate',
        discovery.triageReason ||
          'This repository is recorded as reference or deferred material. HAI did not create a pursuit or activate anything.'
      )
      return
    }
    const review = this.discoveryReviews[discovery.repository]
    if (!review) {
      this.notification.error(
        'Verify metadata first',
        'HAI needs current upstream metadata before creating a manual adapter review.'
      )
      return
    }
    if (!review.available || review.archived) {
      this.notification.error(
        'Upstream is unavailable',
        'HAI cannot create an adapter review for an unavailable or archived repository.'
      )
      return
    }
    this.reviewingDiscoveryRepository = discovery.repository
    this.pursuitService.create({
      title: `Screen ${discovery.repository} for a HAI adapter`,
      description: `Review discovered repository ${discovery.repository} from OSS Insight collection ${discovery.collection}. Source: ${discovery.sourceUrl}. This discovery record does not install, configure, credential, or execute the project.`,
      whyItMatters: discovery.rationale,
      projectKey: '018-HAI',
      domain: 'software',
      desiredOutcome: `A documented go/no-go decision for a narrow ${discovery.repository} adapter, including provenance, licence, maintenance, local deployment, data egress, health, audit, rollback, and approval boundaries.`,
      currentStateSummary: 'Repository was discovered from an already screened OSS Insight candidate category. It is not yet a HAI catalog profile and has no configured runtime.',
      status: 'waiting',
      priorityScore: 60,
      riskLevel: 'high',
      autonomyLevel: 'manual',
      sourceOfCreation: `ossinsight_discovery:${discovery.repository}`,
      nextRecommendedAction: 'Verify the fixed upstream repository metadata and licence, then define a local-only adapter and no-op validation plan before considering implementation.',
      completionDefinition: 'A human-approved acceptance or rejection record exists. Creating this pursuit must not install, enable, credential, or execute the discovered repository.',
    }).subscribe({
      next: (pursuit) => {
        this.reviewingDiscoveryRepository = ''
        this.notification.success('Discovery review created', `${discovery.repository} remains unconfigured. HAI created a manual review record.`)
        this.router.navigate(['/pursuits'], { queryParams: { selected: pursuit.id } })
      },
      error: () => {
        this.reviewingDiscoveryRepository = ''
        this.notification.error('Could not create discovery review', 'No repository was added to the catalog or activated. Check the local pursuit service and try again.')
      },
    })
  }

  canStartReview(entry: IBrainCatalogEntry): boolean {
    return entry.status === 'candidate' || entry.status === 'compatibility_only'
  }

  get visibleDiscoveries(): IBrainCatalogOSSInsightDiscovery[] {
    return (this.ossInsightDiscovery?.discoveries ?? []).slice(0, this.discoveryDisplayLimit)
  }

  get visibleKnownProfiles(): IBrainCatalogOSSInsightKnownProfileHit[] {
    return (this.ossInsightDiscovery?.knownProfiles ?? []).slice(0, this.knownProfileDisplayLimit)
  }

  showMoreDiscoveries(): void {
    const available = this.ossInsightDiscovery?.discoveries?.length ?? 0
    this.discoveryDisplayLimit = Math.min(available, this.discoveryDisplayLimit + 30)
  }

  showMoreKnownProfiles(): void {
    const available = this.ossInsightDiscovery?.knownProfiles?.length ?? 0
    this.knownProfileDisplayLimit = Math.min(available, this.knownProfileDisplayLimit + 20)
  }

  startAdapterReview(entry: IBrainCatalogEntry): void {
    if (!this.canStartReview(entry) || this.reviewingCandidateId) return
    this.reviewingCandidateId = entry.id
    const riskLevel = entry.requiresApproval ? 'high' : 'medium'
    this.pursuitService.create({
      title: `Review ${entry.name} adapter boundary`,
      description: `Review the HAI adapter proposal for ${entry.name}. Upstream: ${entry.upstreamUrl}. Discovery source: ${entry.sourceCatalogUrl}. HAI must not install, enable, or execute ${entry.name} during this review.`,
      whyItMatters: entry.rationale,
      projectKey: '018-HAI',
      domain: 'software',
      desiredOutcome: `A documented go/no-go decision for a narrow ${entry.name} adapter, including local configuration, health checks, audit records, rollback, and approval boundaries.`,
      currentStateSummary: `Catalog candidate recorded as ${entry.status.replace(/_/g, ' ')}. No HAI adapter is configured or live.`,
      status: 'waiting',
      priorityScore: entry.requiresApproval ? 70 : 55,
      riskLevel,
      autonomyLevel: 'manual',
      sourceOfCreation: `brain_catalog:${entry.id}`,
      nextRecommendedAction: 'Review license, deployment model, workspace and network boundaries, allowed tools, health probe, audit trail, rollback, and the no-op validation path before implementation.',
      completionDefinition: 'A human-approved adapter design and validation plan exists, or the candidate is explicitly rejected. No third-party software is installed or activated by creating this pursuit.',
    }).subscribe({
      next: (pursuit) => {
        this.reviewingCandidateId = ''
        this.notification.success('Adapter review created', `${entry.name} remains disabled. HAI created a review record instead of activating the project.`)
        this.router.navigate(['/pursuits'], { queryParams: { selected: pursuit.id } })
      },
      error: () => {
        this.reviewingCandidateId = ''
        this.notification.error('Could not create adapter review', 'No project was installed, configured, or activated. Try again after checking the local pursuit service.')
      },
    })
  }

  statusColor(status: string): string {
    if (status === 'integrated_profile') return 'green'
    if (status === 'candidate') return 'blue'
    if (status === 'compatibility_only' || status === 'reference_only') return 'gold'
    return 'red'
  }

  statusLabel(status: string): string {
    return status.replace(/_/g, ' ')
  }

  readinessLabel(readiness: string): string {
    return readiness.replace(/_/g, ' ')
  }

  collectionDispositionColor(disposition: BrainCatalogCollectionDisposition): string {
    if (disposition === 'represented_in_catalog') return 'green'
    if (disposition === 'review_candidate') return 'blue'
    if (disposition === 'reference_only') return 'gold'
    return 'default'
  }

  collectionDispositionLabel(disposition: BrainCatalogCollectionDisposition): string {
    if (disposition === 'represented_in_catalog') return 'represented'
    if (disposition === 'review_candidate') return 'review candidate'
    if (disposition === 'reference_only') return 'reference only'
    return 'not adopted'
  }
}
