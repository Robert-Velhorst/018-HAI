import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { HttpClient, HttpErrorResponse } from '@angular/common/http'
import { Router } from '@angular/router'
import { timeout, TimeoutError } from 'rxjs'
import { finalize } from 'rxjs/operators'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { IAgentRuntimeInfo } from '../../models/agent-runtime.model.interface'
import { IMCPPreflightOverview, IMCPPreflightResult, IMCPPreflightServer } from '../../models/mcp-preflight.model.interface'
import {
  IRuntimeFeature,
  IRuntimeAttempt,
  IRuntimeCapabilityCard,
  IRuntimeCapabilityOverview,
  IRuntimeParityInventory,
  IRuntimeParityOverview,
  IRuntimeProbe,
  IRuntimeSummary,
  RuntimeFeatureDisposition,
  readRuntimeAttempt,
  runtimeAttemptUncertain,
  runtimeAttemptVerified,
} from '../../models/runtime-lab.model.interface'
import { outcomeRecord, safeOutcomeText, safeReceiptSummary } from '../../models/safe-outcome.model.interface'
import { MCPPreflightService } from '../../services/mcp-preflight.service'
import { RuntimeLabService } from '../../services/runtime-lab.service'

const MODULE_ID = 'runtime-lab'
const DEEPSEEK_HARNESS_ID = 'deepseek-harness'

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-runtime-lab',
    templateUrl: './runtime-lab.component.html',
    styleUrls: ['./runtime-lab.component.scss'],
    standalone: false
})
export class RuntimeLabComponent implements OnInit {
  runtimes: IRuntimeSummary[] = []
  agentRuntimeInfo: IAgentRuntimeInfo[] = []
  parityOverview?: IRuntimeParityOverview
  capabilityOverview?: IRuntimeCapabilityOverview
  mcpOverview?: IMCPPreflightOverview
  loading = false
  loadError = false
  agentRuntimeInfoLoading = false
  agentRuntimeInfoError = false
  mcpLoading = false
  parityLoading = false
  capabilityLoading = false
  probeBusy: Record<string, boolean> = {}
  selfTestBusy: Record<string, boolean> = {}
  mcpBusy: Record<string, boolean> = {}
  private readonly operationTimeoutMs = 30000
  private readonly selfTestOutcomes = new Map<string, IRuntimeAttempt>()
  private readonly uncertainRuntimeIds = new Set<string>()
  private readonly runtimeDiagnosticDetails = new Map<string, string>()
  private readonly selfTestRuntimeSnapshots = new Map<string, IRuntimeSummary>()

  constructor(
    private http: HttpClient,
    private service: RuntimeLabService,
    private mcpPreflight: MCPPreflightService,
    private notification: NzNotificationService,
    private router: Router,
    private viewPreferences: ModuleViewPreferencesService
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  get isAdvanced(): boolean {
    return this.viewPreferences.get(MODULE_ID).mode === 'advanced'
  }

  get runtimesNeedingAttention(): IRuntimeSummary[] {
    return this.runtimes.filter((runtime) => runtime.status !== 'ready' || this.selfTestNeedsReconciliation(runtime))
  }

  get mcpServersNeedingAttention(): IMCPPreflightServer[] {
    return (this.mcpOverview?.servers ?? []).filter((server) =>
      !server.configured || Boolean(server.lastAttempt && server.lastAttempt.status !== 'ready')
    )
  }

  get runtimeReadinessSummary(): string {
    const ready = this.runtimes.length - this.runtimesNeedingAttention.length
    return `${ready} of ${this.runtimes.length} runtimes ready`
  }

  resetView(): void {
    this.viewPreferences.reset(MODULE_ID)
    document.body.classList.remove('hai-view-advanced')
    void this.router.navigate(['/runtime-lab'], {
      queryParams: { mode: 'basic' },
      replaceUrl: true,
    })
  }

  refresh(): void {
    this.loading = true
    this.loadError = false
    this.refreshAgentRuntimeInfo()
    this.refreshMCPPreflight()
    this.refreshFeatureParity()
    this.refreshCapabilities()
    this.service.overview().subscribe({
      next: (res) => {
        this.runtimes = (res.runtimes ?? []).map((runtime) => {
          // Incomplete legacy diagnostics can explain a block, but cannot prove execution.
          const diagnosticDetail = safeOutcomeText(outcomeRecord(runtime.lastAttempt)?.['detail'])
          if (diagnosticDetail) this.runtimeDiagnosticDetails.set(runtime.info.id, diagnosticDetail)
          else this.runtimeDiagnosticDetails.delete(runtime.info.id)
          const lastAttempt = readRuntimeAttempt(runtime.lastAttempt)
          if (lastAttempt && runtimeAttemptUncertain(lastAttempt)) {
            if (!this.uncertainRuntimeIds.has(runtime.info.id)) this.selfTestOutcomes.set(runtime.info.id, lastAttempt)
            this.uncertainRuntimeIds.add(runtime.info.id)
            this.selfTestRuntimeSnapshots.set(runtime.info.id, runtime)
          }
          return { ...runtime, lastAttempt }
        })
        for (const runtimeId of this.uncertainRuntimeIds) {
          const retained = this.selfTestRuntimeSnapshots.get(runtimeId)
          if (retained && !this.runtimes.some((runtime) => runtime.info.id === runtimeId)) this.runtimes.push(retained)
        }
        this.loadError = false
        this.loading = false
      },
      error: () => {
        this.loadError = true
        this.loading = false
        this.notification.error('Error', 'Failed to load the runtime lab.')
      },
    })
  }

  private refreshAgentRuntimeInfo(): void {
    this.agentRuntimeInfoLoading = true
    this.agentRuntimeInfoError = false
    this.http.get<IAgentRuntimeInfo[]>('/api/v1/agent-runtimes/').subscribe({
      next: (runtimes) => {
        this.agentRuntimeInfo = Array.isArray(runtimes) ? runtimes : []
        this.agentRuntimeInfoLoading = false
      },
      error: () => {
        this.agentRuntimeInfo = []
        this.agentRuntimeInfoLoading = false
        this.agentRuntimeInfoError = true
      },
    })
  }

  agentInfo(runtimeId: string): IAgentRuntimeInfo | undefined {
    return this.agentRuntimeInfo.find((runtime) => runtime.id === runtimeId)
  }

  runtimeAttentionDetail(runtime: IRuntimeSummary): string {
    if (this.selfTestNeedsReconciliation(runtime)) return 'Previous self-test completion is not confirmed; effects require reconciliation before another self-test.'
    if (runtime.info.id === DEEPSEEK_HARNESS_ID) {
      const info = this.agentInfo(DEEPSEEK_HARNESS_ID)
      if (info?.missingConfiguration?.length) {
        return info.missingConfiguration.join('; ')
      }
      if (info?.configured === false) {
        return 'HAI reports this runtime as not configured; no additional blocker detail was returned.'
      }
      if (this.agentRuntimeInfoLoading) {
        return 'Loading the canonical runtime policy; no runtime action has been started.'
      }
      if (this.agentRuntimeInfoError || !info) {
        return 'Canonical runtime details are unavailable. Refresh to retry; no runtime action has been started.'
      }
    }
    return runtime.lastAttempt?.detail || this.runtimeDiagnosticDetails.get(runtime.info.id) ||
      runtime.setupRequirements?.[0]?.detail || runtime.info.description
  }

  runtimeConfiguredLabel(runtimeId: string): string {
    const info = this.agentInfo(runtimeId)
    if (info) return info.configured ? 'Yes' : 'No'
    if (this.agentRuntimeInfoLoading) return 'Checking'
    return 'Unavailable'
  }

  runtimeExecutionLabel(runtimeId: string, fallback: boolean): string {
    const info = this.agentInfo(runtimeId)
    if (info) return info.executionEnabled ? 'Enabled' : 'Blocked'
    return fallback ? 'Enabled' : 'Blocked'
  }

  isDeepSeekExecutionBlocked(runtime: IRuntimeSummary): boolean {
    if (runtime.info.id !== DEEPSEEK_HARNESS_ID) return false
    const info = this.agentInfo(DEEPSEEK_HARNESS_ID)
    return runtime.status === 'blocked' || info?.configured === false || info?.executionEnabled === false
  }

  isDeepSeekSandboxBlocked(runtime: IRuntimeSummary): boolean {
    const info = this.agentInfo(DEEPSEEK_HARNESS_ID)
    return runtime.info.id === DEEPSEEK_HARNESS_ID &&
      runtime.status === 'blocked' &&
      info?.configured === false &&
      Boolean(info.missingConfiguration?.some((reason) => /OS-enforced sandbox/i.test(reason)))
  }

  refreshCapabilities(): void {
    this.capabilityLoading = true
    this.service.capabilities().subscribe({
      next: (overview) => {
        this.capabilityOverview = overview
        this.capabilityLoading = false
      },
      error: () => {
        this.capabilityOverview = undefined
        this.capabilityLoading = false
      },
    })
  }

  cardsForRuntime(runtimeId: string): IRuntimeCapabilityCard[] {
    return (this.capabilityOverview?.cards ?? []).filter((card) => card.runtimeId === runtimeId)
  }

  capabilityState(runtimeId: string): 'loading' | 'unavailable' | 'empty' | 'available' {
    if (this.capabilityLoading) return 'loading'
    if (!this.capabilityOverview) return 'unavailable'
    return this.cardsForRuntime(runtimeId).length ? 'available' : 'empty'
  }

  preparedModelCatalogLabel(discovery?: { gatewayPreparedModelCatalog?: IRuntimeProbe['gatewayPreparedModelCatalog'] }): string | null {
    const catalog = discovery?.gatewayPreparedModelCatalog
    if (!catalog) return null
    return `${catalog.availableModels} available · ${catalog.unavailableModels} unavailable · ${catalog.unknownAvailabilityModels} unknown`
  }

  taskLedgerLabel(discovery?: { gatewayTaskLedger?: IRuntimeProbe['gatewayTaskLedger'] }): string | null {
    const ledger = discovery?.gatewayTaskLedger
    if (!ledger) return null
    return `${ledger.sampledTasks} sampled · ${ledger.truncated ? 'partial sample' : 'complete sample'}`
  }

  capabilityCatalogLabel(discovery?: { gatewayCapabilityCatalog?: IRuntimeProbe['gatewayCapabilityCatalog'] }): string | null {
    const catalog = discovery?.gatewayCapabilityCatalog
    if (!catalog) return null
    const toolCount = Object.values(catalog.toolCountsBySource).reduce((total, count) => total + count, 0)
    return `${catalog.eligibleSkills} of ${catalog.sampledSkills} skills eligible · ${toolCount} tools · ${catalog.sampledCommands} commands`
  }

  agentRosterLabel(discovery?: { gatewayAgentRoster?: IRuntimeProbe['gatewayAgentRoster'] }): string | null {
    const roster = discovery?.gatewayAgentRoster
    if (!roster) return null
    return `${roster.agentCount} agents · ${roster.systemCount} system · ${roster.unknownKindCount} legacy/unknown`
  }

  refreshFeatureParity(): void {
    this.parityLoading = true
    this.service.featureParity().subscribe({
      next: (overview) => {
        this.parityOverview = overview
        this.parityLoading = false
      },
      error: () => {
        this.parityOverview = undefined
        this.parityLoading = false
      },
    })
  }

  dispositionColor(disposition: RuntimeFeatureDisposition): string {
    switch (disposition) {
      case 'already_present':
      case 'consolidated_existing':
      case 'hai_native_reimplementation':
      case 'integrated_directly':
        return 'green'
      case 'adapted_for_hai':
        return 'blue'
      case 'deferred':
      case 'blocked_external':
        return 'gold'
      case 'constrained_unsafe':
      case 'excluded_incompatible_license':
        return 'red'
      default:
        return 'default'
    }
  }

  dispositionLabel(disposition: RuntimeFeatureDisposition): string {
    return disposition.replace(/_/g, ' ')
  }

  implementationCount(inventory: IRuntimeParityInventory, state: string): number {
    return inventory.features.filter((item) => item.implementationStatus === state).length
  }

  backlogCount(inventory: IRuntimeParityInventory): number {
    return inventory.features.filter((item) =>
      item.disposition === 'deferred' || item.disposition === 'blocked_external'
    ).length
  }

  featureTrackBy(_index: number, feature: IRuntimeFeature): string {
    return feature.id
  }

  refreshMCPPreflight(): void {
    this.mcpLoading = true
    this.mcpPreflight.overview().subscribe({
      next: (overview) => {
        this.mcpOverview = overview
        this.mcpLoading = false
      },
      error: () => {
        this.mcpOverview = undefined
        this.mcpLoading = false
      },
    })
  }

  runMCPPreflight(server: IMCPPreflightServer): void {
    if (this.mcpBusy[server.id] || !server.configured) return
    this.mcpBusy[server.id] = true
    this.mcpPreflight.run(server.id).pipe(timeout(this.operationTimeoutMs)).subscribe({
      next: (result) => {
        this.mcpBusy[server.id] = false
        this.notifyMCPPreflight(server, result)
        this.refreshMCPPreflight()
      },
      error: () => {
        this.mcpBusy[server.id] = false
        this.notification.error('MCP readiness check failed', 'HAI could not complete the local handshake. No MCP tool was called or enabled.')
        this.refreshMCPPreflight()
      },
    })
  }

  private notifyMCPPreflight(server: IMCPPreflightServer, result: IMCPPreflightResult): void {
    if (result.status === 'ready') {
      const verification = result.readOnlyVerified
        ? ' Declared tools matched HAI\'s inspection-only context contract.'
        : ''
      this.notification.success(
        'MCP server ready',
        `${server.catalogName || server.id}: ${result.toolCount} declared tool(s) inspected. No tool was called.${verification}`
      )
      return
    }
    this.notification.warning('MCP server not ready', `${server.catalogName || server.id}: ${result.detail}`)
  }

  isReadOnlyContractServer(server: IMCPPreflightServer): boolean {
    return server.catalogId === 'github-mcp-server' || server.catalogId === 'playwright-mcp'
  }

  probe(r: IRuntimeSummary): void {
    const runtimeId = r.info.id
    if (this.probeBusy[runtimeId]) return
    this.probeBusy[runtimeId] = true
    this.service.probe(runtimeId).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => this.probeBusy[runtimeId] = false),
    ).subscribe({
      next: (res) => {
        this.probeBusy[runtimeId] = false
        const title = res.protocolValid ? 'Discovery validated' : 'Discovery did not validate'
        this.notification.info(
          title,
          `${r.info.displayName}: ${res.discoveryState} · ${res.readinessLevel}. Execution remains governed and blocked.`
        )
        this.refresh()
      },
      error: () => {
        this.notification.error('Error', 'Probe failed.')
      },
    })
  }

  selfTest(r: IRuntimeSummary): void {
    const runtimeId = r.info.id
    if (!this.canSelfTest(r)) return
    this.selfTestRuntimeSnapshots.set(runtimeId, r)
    this.selfTestBusy[runtimeId] = true
    let receivedOutcome = false
    this.service.selfTest(runtimeId).pipe(
      timeout(this.operationTimeoutMs),
      finalize(() => this.selfTestBusy[runtimeId] = false),
    ).subscribe({
      next: (attempt) => {
        receivedOutcome = true
        const result = readRuntimeAttempt(attempt)
        if (!result || result.runtimeId !== runtimeId) {
          this.recordUncertainSelfTest(r)
        } else {
          this.selfTestOutcomes.set(runtimeId, result)
          r.lastAttempt = result
          if (runtimeAttemptUncertain(result)) this.uncertainRuntimeIds.add(runtimeId)
          if (runtimeAttemptVerified(result)) {
            this.notification.success('Self-test passed', `${r.info.displayName} verified through the ledger.`)
          } else if (result.status === 'setup_required') {
            this.notification.warning('Setup required', `${r.info.displayName} is not configured - no fake execution.`)
          } else {
            this.notification.warning('Self-test', `${r.info.displayName}: ${this.selfTestAttemptLabel(result)}`)
          }
        }
        this.refresh()
      },
      error: (err) => {
        receivedOutcome = true
        const response = err as HttpErrorResponse
        const attempt = readRuntimeAttempt(response?.error)
        if (attempt?.runtimeId === runtimeId && (attempt.receipt || runtimeAttemptUncertain(attempt))) {
          this.selfTestOutcomes.set(runtimeId, { ...attempt, verificationPassed: false, reconciliationRequired: true })
          this.uncertainRuntimeIds.add(runtimeId)
        } else if (err instanceof TimeoutError || response?.status === 0 || response?.status >= 500 || !(err instanceof HttpErrorResponse)) {
          this.recordUncertainSelfTest(r)
        } else {
          this.notification.error('Self-test not accepted', 'The request was refused. No verified completion was returned.')
          return
        }
        this.notification.warning('Self-test outcome unknown', 'Review the retained attempt and operation ledger. Another self-test is blocked.')
      },
      complete: () => {
        if (!receivedOutcome) this.recordUncertainSelfTest(r)
      },
    })
  }

  private recordUncertainSelfTest(runtime: IRuntimeSummary): void {
    const runtimeId = runtime.info.id
    this.uncertainRuntimeIds.add(runtimeId)
    this.selfTestOutcomes.set(runtimeId, {
      id: '', runtimeId, status: 'inconclusive', verificationPassed: false,
      reconciliationRequired: true, createdAt: '',
      detail: 'The self-test request returned no confirmed outcome. It may have produced effects; refresh does not authorize another test.',
    })
  }

  lastSelfTestAttempt(runtime: IRuntimeSummary): IRuntimeAttempt | undefined {
    const retained = this.selfTestOutcomes.get(runtime.info.id)
    if (this.uncertainRuntimeIds.has(runtime.info.id)) return retained ?? runtime.lastAttempt
    if (!retained || !runtime.lastAttempt) return retained ?? runtime.lastAttempt
    return Date.parse(runtime.lastAttempt.createdAt) > Date.parse(retained.createdAt) ? runtime.lastAttempt : retained
  }

  selfTestNeedsReconciliation(runtime: IRuntimeSummary): boolean {
    const attempt = this.lastSelfTestAttempt(runtime)
    return this.uncertainRuntimeIds.has(runtime.info.id) || (!!attempt && runtimeAttemptUncertain(attempt))
  }

  canSelfTest(runtime: IRuntimeSummary): boolean {
    return !this.selfTestBusy[runtime.info.id] && !this.selfTestNeedsReconciliation(runtime) && !this.isDeepSeekExecutionBlocked(runtime)
  }

  selfTestAttemptLabel(attempt: IRuntimeAttempt): string {
    if (runtimeAttemptUncertain(attempt)) return 'Completion not verified; reconciliation required'
    if (attempt.discoveryRecovered) return 'Read-only discovery recovered; execution not verified'
    if (runtimeAttemptVerified(attempt)) return 'Verified completion'
    return safeOutcomeText(attempt.status, 64)
  }

  selfTestAttemptColor(attempt: IRuntimeAttempt): string {
    return runtimeAttemptUncertain(attempt) ? 'gold' : attempt.discoveryRecovered ? 'blue' : this.attemptColor(attempt.status)
  }

  selfTestOutcomeSummary(runtime: IRuntimeSummary): string {
    const attempt = this.lastSelfTestAttempt(runtime)
    if (!attempt) return ''
    const parts = [runtimeAttemptUncertain(attempt) ? '' : safeOutcomeText(attempt.detail)]
    if (attempt.operationId) parts.push(`Operation: ${safeOutcomeText(attempt.operationId, 128)}.`)
    if (attempt.operationStatus) parts.push(`Returned status: ${safeOutcomeText(attempt.operationStatus, 64)}.`)
    if (attempt.outcomeRecorded === false) parts.push('Completion not confirmed recorded.')
    if (attempt.outcomeRecorded === true) parts.push('Outcome reported recorded; storage durability is not established.')
    const receipt = safeReceiptSummary(attempt)
    if (receipt) parts.push(receipt)
    return parts.filter(Boolean).join(' ')
  }

  reviewSelfTestOperations(): void {
    this.router.navigate(['/background-operations'], { queryParams: { mode: 'advanced' }, fragment: 'operation-ledger' })
  }

  statusColor(status: string): string {
    switch (status) {
      case 'ready':
        return 'green'
      case 'configured':
        return 'blue'
      case 'not_configured':
        return 'default'
      case 'blocked':
        return 'gold'
      case 'unavailable':
      case 'failed':
        return 'red'
      default:
        return 'default'
    }
  }

  attemptColor(status?: string): string {
    switch (status) {
      case 'succeeded':
        return 'green'
      case 'setup_required':
        return 'default'
      case 'inconclusive':
        return 'gold'
      case 'failed':
      case 'blocked':
        return 'red'
      default:
        return 'default'
    }
  }

  goBack(): void {
    this.router.navigate(['/control-center'])
  }
}
