import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import {
  AgentBootContext,
  OperationalGraphNode,
  OperationalGraphSnapshot,
  OperationalNeighborhood,
} from '../../models/operational-graph.model'
import { OperationalGraphService } from '../../services/operational-graph.service'

type NodeFilter = 'attention' | 'all' | string

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-operational-brain',
  templateUrl: './operational-brain.component.html',
  styleUrls: ['./operational-brain.component.scss'],
})
export class OperationalBrainComponent implements OnInit {
  readonly moduleId = 'operational-brain'
  snapshot?: OperationalGraphSnapshot
  selected?: OperationalGraphNode
  neighborhood?: OperationalNeighborhood
  agentBoot?: AgentBootContext
  loading = true
  detailLoading = false
  bootLoading = false
  errorMessage = ''
  searchQuery = ''
  filter: NodeFilter = 'attention'
  inspectorOpen = false

  constructor(private graph: OperationalGraphService) {}

  ngOnInit(): void { this.load() }

  get nodes(): OperationalGraphNode[] { return this.snapshot?.nodes || [] }
  get layers(): Array<{ id: string; count: number }> {
    return Object.entries(this.snapshot?.layerCounts || {})
      .filter(([id]) => id !== 'system')
      .map(([id, count]) => ({ id, count }))
      .sort((left, right) => right.count - left.count || left.id.localeCompare(right.id))
  }
  get attentionNodes(): OperationalGraphNode[] {
    return this.nodes.filter((node) => this.needsAttention(node)).sort((a, b) => b.weight - a.weight)
  }
  get visibleNodes(): OperationalGraphNode[] {
    const query = this.searchQuery.trim().toLowerCase()
    return this.nodes
      .filter((node) => node.id !== this.snapshot?.rootId)
      .filter((node) => this.filter === 'all' || (this.filter === 'attention' ? this.needsAttention(node) : node.layer === this.filter))
      .filter((node) => !query || `${node.label} ${node.summary || ''} ${(node.tags || []).join(' ')}`.toLowerCase().includes(query))
      .sort((a, b) => b.weight - a.weight || a.label.localeCompare(b.label))
      .slice(0, 80)
  }
  get agentNodes(): OperationalGraphNode[] { return this.nodes.filter((node) => node.kind === 'agent') }
  get selectedLinks() { return this.neighborhood?.links || [] }
  get emptyStateIcon(): string { return this.searchQuery.trim() ? 'search' : 'check-circle' }
  get emptyStateTitle(): string {
    if (this.searchQuery.trim()) return 'No matching records'
    return this.filter === 'attention' ? 'No operational exceptions' : 'No records match this view'
  }
  get emptyStateSummary(): string {
    if (this.searchQuery.trim()) return 'Change or clear the search term to return to this view.'
    return this.filter === 'attention'
      ? 'HAI has no blocked, waiting, or unsupported records in the current bounded map.'
      : 'Choose another system layer.'
  }

  load(): void {
    this.loading = true
    this.errorMessage = ''
    this.graph.snapshot().subscribe({
      next: (snapshot) => { this.snapshot = snapshot; this.loading = false },
      error: (error: HttpErrorResponse) => { this.loading = false; this.errorMessage = this.apiError(error, 'The operational graph could not be loaded.') },
    })
  }

  setFilter(filter: NodeFilter): void { this.filter = filter }

  openInspector(node: OperationalGraphNode): void {
    this.selected = node
    this.inspectorOpen = true
    this.neighborhood = undefined
    this.agentBoot = undefined
    this.detailLoading = true
    this.graph.neighborhood(node.id, 1, 100).subscribe({
      next: (result) => { this.neighborhood = result; this.detailLoading = false },
      error: () => { this.detailLoading = false },
    })
    if (node.kind === 'agent') this.loadAgentBoot(node)
  }

  closeInspector(): void {
    this.inspectorOpen = false
    this.selected = undefined
    this.neighborhood = undefined
    this.agentBoot = undefined
  }

  loadAgentBoot(node: OperationalGraphNode): void {
    this.bootLoading = true
    this.graph.agentBoot(node.id.replace(/^agent:/, '')).subscribe({
      next: (boot) => { this.agentBoot = boot; this.bootLoading = false },
      error: () => { this.bootLoading = false },
    })
  }

  relatedLabel(nodeId: string): string {
    return this.neighborhood?.nodes.find((node) => node.id === nodeId)?.label || nodeId
  }

  linkOtherEnd(sourceId: string, targetId: string): string {
    return sourceId === this.selected?.id ? targetId : sourceId
  }

  statusTone(node: OperationalGraphNode): 'good' | 'review' | 'danger' | 'neutral' {
    const status = `${node.status || ''} ${node.verificationStatus || ''}`.toLowerCase()
    if (/block|fail|unsupported|conflict/.test(status)) return 'danger'
    if (/review|uncertain|waiting|draft|unknown/.test(status)) return 'review'
    if (/verified|healthy|active|enabled|complete|ready|supported/.test(status)) return 'good'
    return 'neutral'
  }

  displayStatus(node: OperationalGraphNode): string {
    const verification = (node.verificationStatus || '').toLowerCase()
    if (/needs_review|unsupported|uncertain|conflict|fail/.test(verification)) return verification
    return node.status || node.verificationStatus || 'recorded'
  }

  needsAttention(node: OperationalGraphNode): boolean {
    const status = `${node.status || ''} ${node.verificationStatus || ''}`.toLowerCase()
    return /block|fail|unsupported|conflict|needs_review|uncertain|waiting/.test(status)
  }

  label(value: string): string {
    return value.replace(/[_-]+/g, ' ').replace(/\b\w/g, (character) => character.toUpperCase())
  }

  trackByNode(_: number, node: OperationalGraphNode): string { return node.id }
  trackByLayer(_: number, layer: { id: string }): string { return layer.id }

  private apiError(error: HttpErrorResponse, fallback: string): string {
    return String(error.error?.error || error.error?.message || fallback)
  }
}
