import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { IMCPPreflightOverview, IMCPPreflightResult, IMCPPreflightServer } from '../../models/mcp-preflight.model.interface'
import {
  IRuntimeFeature,
  IRuntimeCapabilityCard,
  IRuntimeCapabilityOverview,
  IRuntimeParityInventory,
  IRuntimeParityOverview,
  IRuntimeSummary,
  RuntimeFeatureDisposition,
} from '../../models/runtime-lab.model.interface'
import { MCPPreflightService } from '../../services/mcp-preflight.service'
import { RuntimeLabService } from '../../services/runtime-lab.service'

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-runtime-lab',
  templateUrl: './runtime-lab.component.html',
  styleUrls: ['./runtime-lab.component.scss'],
})
export class RuntimeLabComponent implements OnInit {
  runtimes: IRuntimeSummary[] = []
  parityOverview?: IRuntimeParityOverview
  capabilityOverview?: IRuntimeCapabilityOverview
  mcpOverview?: IMCPPreflightOverview
  loading = false
  mcpLoading = false
  parityLoading = false
  capabilityLoading = false
  parityOpen = false
  busy: Record<string, boolean> = {}
  mcpBusy: Record<string, boolean> = {}

  constructor(
    private service: RuntimeLabService,
    private mcpPreflight: MCPPreflightService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  refresh(): void {
    this.loading = true
    this.refreshMCPPreflight()
    if (this.parityOpen) {
      this.refreshFeatureParity(true)
      this.refreshCapabilities(true)
    }
    this.service.overview().subscribe({
      next: (res) => {
        this.runtimes = res.runtimes ?? []
        this.loading = false
      },
      error: () => {
        this.loading = false
        this.notification.error('Error', 'Failed to load the runtime lab.')
      },
    })
  }

  refreshCapabilities(force = false): void {
    if (this.capabilityLoading || (!force && this.capabilityOverview)) {
      return
    }
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

  refreshFeatureParity(force = false): void {
    if (this.parityLoading || (!force && this.parityOverview)) {
      return
    }
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

  onParityToggle(event: Event): void {
    this.parityOpen = (event.target as HTMLDetailsElement).open
    if (!this.parityOpen) {
      return
    }
    this.refreshFeatureParity()
    this.refreshCapabilities()
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
    this.mcpPreflight.run(server.id).subscribe({
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
    this.busy[r.info.id] = true
    this.service.probe(r.info.id).subscribe({
      next: (res) => {
        this.busy[r.info.id] = false
        const title = res.protocolValid ? 'Discovery validated' : 'Discovery did not validate'
        this.notification.info(
          title,
          `${r.info.displayName}: ${res.discoveryState} · ${res.readinessLevel}. Execution remains governed and blocked.`
        )
        this.refresh()
      },
      error: () => {
        this.busy[r.info.id] = false
        this.notification.error('Error', 'Probe failed.')
      },
    })
  }

  selfTest(r: IRuntimeSummary): void {
    this.busy[r.info.id] = true
    this.service.selfTest(r.info.id).subscribe({
      next: (attempt) => {
        this.busy[r.info.id] = false
        if (attempt.status === 'succeeded') {
          this.notification.success('Self-test passed', `${r.info.displayName} verified through the ledger.`)
        } else if (attempt.status === 'setup_required') {
          this.notification.warning('Setup required', `${r.info.displayName} is not configured - no fake execution.`)
        } else {
          this.notification.warning('Self-test', `${r.info.displayName}: ${attempt.status}`)
        }
        this.refresh()
      },
      error: () => {
        this.busy[r.info.id] = false
        this.notification.error('Error', 'Self-test failed.')
      },
    })
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
